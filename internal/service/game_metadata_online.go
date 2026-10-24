package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"foliospace-reader/internal/domain"
)

const defaultHasheousMCPEndpoint = "https://hasheous.org/api/v1/Mcp"

var (
	hasheousSHA1Pattern = regexp.MustCompile(`(?i)^[0-9a-f]{40}$`)
	hasheousCRCPattern  = regexp.MustCompile(`(?i)^[0-9a-f]{8}$`)
)

type hasheousLookupResponse struct {
	JSONRPC string `json:"jsonrpc"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Result struct {
		IsError           bool                     `json:"isError"`
		StructuredContent hasheousStructuredResult `json:"structuredContent"`
	} `json:"result"`
}

type hasheousStructuredResult struct {
	Error string              `json:"error"`
	Count int                 `json:"count"`
	Games []hasheousGameMatch `json:"games"`
}

type hasheousGameMatch struct {
	ID         int64           `json:"id"`
	Name       string          `json:"name"`
	Year       string          `json:"year"`
	Publisher  string          `json:"publisher"`
	Platform   string          `json:"platform"`
	Countries  json.RawMessage `json:"countries"`
	Languages  json.RawMessage `json:"languages"`
	Score      float64         `json:"score"`
	MatchedROM []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"matchedRoms"`
}

func (s *Service) refreshGameMetadataFromHasheous(details domain.GameDetails) (domain.GameMetadataActionResult, error) {
	matches, err := lookupHasheousGame(details.Game)
	if err != nil {
		return domain.GameMetadataActionResult{}, err
	}
	for _, match := range matches {
		raw, marshalErr := json.Marshal(match)
		if marshalErr != nil {
			return domain.GameMetadataActionResult{}, marshalErr
		}
		confidence := match.Score
		if confidence <= 0 || confidence > 1 {
			confidence = 1
		}
		if _, err := s.store.UpsertGameMetadataSource(domain.GameMetadataSource{
			GameID: details.Game.ID, Source: "hasheous", SourceID: fmt.Sprintf("%d", match.ID),
			MatchedBy: "hash", Confidence: confidence, RawJSON: string(raw),
		}); err != nil {
			return domain.GameMetadataActionResult{}, err
		}
	}
	status := "completed"
	message := "Hasheous did not find a hash match."
	if len(matches) == 1 {
		raw, _ := json.Marshal(matches[0])
		if err := s.applyHasheousMetadata(details, string(raw)); err != nil {
			return domain.GameMetadataActionResult{}, err
		}
		message = "Hasheous matched the game by hash and filled missing metadata fields."
	} else if len(matches) > 1 {
		status = "needs-selection"
		message = fmt.Sprintf("Hasheous returned %d possible matches. Choose the correct source before applying it.", len(matches))
	}
	updated, err := s.store.GameDetails(details.Game.ID)
	if err != nil {
		return domain.GameMetadataActionResult{}, err
	}
	return domain.GameMetadataActionResult{
		GameID: details.Game.ID, Action: "refresh", Status: status, Message: message,
		MetadataStatus: updated.MetadataStatus, Sources: updated.Sources, Providers: s.GameMetadataProviders(),
	}, nil
}

func lookupHasheousGame(game domain.GameAsset) ([]hasheousGameMatch, error) {
	arguments := map[string]any{"limit": 10}
	sha1 := strings.ToLower(strings.TrimSpace(game.SHA1))
	crc := strings.ToLower(strings.TrimSpace(game.CRC32))
	if hasheousSHA1Pattern.MatchString(sha1) {
		arguments["sha1"] = sha1
	}
	if hasheousCRCPattern.MatchString(crc) {
		arguments["crc"] = crc
	}
	if len(arguments) == 1 {
		return nil, fmt.Errorf("game %d has no valid SHA-1 or CRC32 for online metadata matching", game.ID)
	}
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "hasheous_lookup_hashes",
			"arguments": arguments,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimSpace(os.Getenv("FOLIOSPACE_HASHEOUS_MCP_URL"))
	if endpoint == "" {
		endpoint = defaultHasheousMCPEndpoint
	}
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 20 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("Hasheous request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		limited, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("Hasheous returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(limited)))
	}
	var result hasheousLookupResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&result); err != nil {
		return nil, fmt.Errorf("invalid Hasheous response: %w", err)
	}
	if result.Error != nil {
		return nil, fmt.Errorf("Hasheous JSON-RPC error %d: %s", result.Error.Code, result.Error.Message)
	}
	if result.Result.IsError || result.Result.StructuredContent.Error != "" {
		return nil, fmt.Errorf("Hasheous lookup failed: %s", result.Result.StructuredContent.Error)
	}
	return result.Result.StructuredContent.Games, nil
}

func (s *Service) applyHasheousMetadata(details domain.GameDetails, raw string) error {
	var match hasheousGameMatch
	if err := json.Unmarshal([]byte(raw), &match); err != nil {
		return fmt.Errorf("invalid Hasheous metadata source: %w", err)
	}
	metadata := details.Metadata
	metadata.GameID = details.Game.ID
	if strings.TrimSpace(metadata.DisplayTitle) == "" {
		metadata.DisplayTitle = strings.TrimSpace(match.Name)
	}
	if strings.TrimSpace(metadata.ReleaseDate) == "" {
		metadata.ReleaseDate = strings.TrimSpace(match.Year)
	}
	if len(metadata.Publishers) == 0 && strings.TrimSpace(match.Publisher) != "" {
		metadata.Publishers = []string{strings.TrimSpace(match.Publisher)}
	}
	return s.store.UpsertImportedGameMetadata(metadata)
}
