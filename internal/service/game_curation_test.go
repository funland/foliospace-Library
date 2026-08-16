package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/store"
)

func TestGameCatalogSettingsDefaultsAndNormalization(t *testing.T) {
	configDir := t.TempDir()
	conn, err := db.Open(configDir)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	svc := &Service{store: store.New(conn), configDir: configDir}

	defaults := svc.GameCatalogSettings()
	if !defaults.AutoAnalyzeAfterScan || !defaults.EnableLibretroCovers || defaults.MetadataProvider != "local" {
		t.Fatalf("defaults = %#v, want automatic analysis, Libretro covers, and local metadata", defaults)
	}
	if defaults.FBNeoDATPath != filepath.Join(configDir, "policies", "fbneo-arcade.dat") {
		t.Fatalf("FBNeo DAT path = %q", defaults.FBNeoDATPath)
	}
	if defaults.FBNeoTargetsPath != filepath.Join(configDir, "policies", "fbneo-mobile-targets.json") ||
		defaults.MAMETargetsPath != filepath.Join(configDir, "policies", "mame-mobile-targets.json") {
		t.Fatalf("runtime target paths = %#v", defaults)
	}

	if err := svc.SaveGameCatalogSettings(domain.GameCatalogSettings{MetadataProvider: "unsupported"}); err != nil {
		t.Fatal(err)
	}
	normalized := svc.GameCatalogSettings()
	if normalized.MetadataProvider != "local" || normalized.MAMEPlatforms != defaultMAMEPlatforms ||
		normalized.FBNeoTargetsPath != defaults.FBNeoTargetsPath || normalized.MAMETargetsPath != defaults.MAMETargetsPath {
		t.Fatalf("normalized settings = %#v", normalized)
	}
}

func TestScanShouldTriggerCatalogAnalysisOnlyForDirectories(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "SNES")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	rom := filepath.Join(dir, "sf2.zip")
	if err := os.WriteFile(rom, []byte("rom"), 0o600); err != nil {
		t.Fatal(err)
	}
	library := domain.Library{RootPath: root, AssetType: "game"}
	for _, test := range []struct {
		name   string
		target string
		want   bool
	}{
		{name: "full library", target: root, want: true},
		{name: "directory", target: dir, want: true},
		{name: "single ROM", target: rom, want: false},
		{name: "missing target", target: filepath.Join(root, "missing.zip"), want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := scanShouldTriggerCatalogAnalysis(library, domain.ScanJob{TargetPath: test.target}); got != test.want {
				t.Fatalf("scanShouldTriggerCatalogAnalysis(%q) = %v, want %v", test.target, got, test.want)
			}
		})
	}
}

func TestEffectiveTargetsPathUsesLegacyFallback(t *testing.T) {
	configDir := t.TempDir()
	preferred := filepath.Join(configDir, "missing.json")
	legacy := filepath.Join(configDir, "targets.json")
	if err := os.WriteFile(legacy, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := effectiveTargetsPath(preferred, legacy); got != legacy {
		t.Fatalf("effective target path = %q, want legacy %q", got, legacy)
	}
}

func TestGameCatalogTargetsPolicyStatusValidatesSplitPolicies(t *testing.T) {
	dir := t.TempDir()
	fbneoPath := filepath.Join(dir, "fbneo.json")
	mamePath := filepath.Join(dir, "mame.json")
	fullHash := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := os.WriteFile(fbneoPath, []byte(`{"targets":[{"id":"ipad","coreSha256":"`+fullHash+`"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mamePath, []byte(`{"targets":[{"id":"ipad"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	status := gameCatalogTargetsPolicyStatus(fbneoPath, mamePath)
	if !status.Available || status.ID != "targets" {
		t.Fatalf("targets status = %#v, want available aggregate", status)
	}
	if err := os.WriteFile(fbneoPath, []byte(`{"targets":[{"id":"ipad","coreSha256":"0123"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	status = gameCatalogTargetsPolicyStatus(fbneoPath, mamePath)
	if status.Available || status.Message == "" {
		t.Fatalf("invalid targets status = %#v, want unavailable with an error", status)
	}
	if err := os.WriteFile(fbneoPath, []byte(`{"targets":[{"id":"ipad","coreBuildId":"fbneo-source-libretro-ios-arm64-lightgun2p-v1"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	status = gameCatalogTargetsPolicyStatus(fbneoPath, mamePath)
	if !status.Available {
		t.Fatalf("stable build id targets status = %#v, want available", status)
	}
}

func TestRefreshGameMetadataFromHasheousUsesHashAndFillsMissingFields(t *testing.T) {
	type capturedRequest struct {
		method string
		body   struct {
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		err error
	}
	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := capturedRequest{method: r.Method}
		request.err = json.NewDecoder(r.Body).Decode(&request.body)
		requests <- request
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"isError":false,"structuredContent":{"count":1,"games":[{"id":42,"name":"Matched Game","year":"1998","publisher":"Example Studio","platform":"Arcade","countries":{"Japan":true},"languages":"Japanese","score":0.99,"matchedRoms":[{"id":7,"name":"matched.zip"}]}]}}}`))
	}))
	defer server.Close()
	t.Setenv("FOLIOSPACE_HASHEOUS_MCP_URL", server.URL)

	configDir := t.TempDir()
	conn, err := db.Open(configDir)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	library, err := st.CreateLibrary("Games", "/library")
	if err != nil {
		t.Fatal(err)
	}
	game, err := st.UpsertGame(domain.GameAsset{
		LibraryID: library.ID, Title: "matched", Platform: "arcade", Format: "zip",
		FilePath: "/library/matched.zip", RelPath: "matched.zip", MTime: time.Unix(1, 0),
		CRC32: "1234abcd", SHA1: "0123456789abcdef0123456789abcdef01234567", CatalogRole: "needs-curation",
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{store: st, configDir: configDir}
	settings := svc.defaultGameCatalogSettings()
	settings.MetadataProvider = "hasheous"
	if err := svc.SaveGameCatalogSettings(settings); err != nil {
		t.Fatal(err)
	}

	result, err := svc.RefreshGameMetadata(game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" || len(result.Sources) != 1 {
		t.Fatalf("result = %#v", result)
	}
	request := <-requests
	if request.err != nil {
		t.Fatal(request.err)
	}
	if request.method != http.MethodPost {
		t.Fatalf("method = %s, want POST", request.method)
	}
	if request.body.Params.Name != "hasheous_lookup_hashes" || request.body.Params.Arguments["sha1"] != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("request = %#v", request.body)
	}
	details, err := st.GameDetails(game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if details.Metadata.DisplayTitle != "Matched Game" || details.Metadata.ReleaseDate != "1998" || len(details.Metadata.Publishers) != 1 || details.Metadata.Publishers[0] != "Example Studio" {
		t.Fatalf("metadata = %#v", details.Metadata)
	}
}

func TestLocalGameCoverCandidatesIncludeCommonSidecars(t *testing.T) {
	gamePath := filepath.Join("library", "PSP", "Example.iso")
	candidates := localGameCoverCandidates(gamePath)
	wanted := map[string]bool{
		filepath.Join("library", "PSP", "Example.jpg"):                      false,
		filepath.Join("library", "PSP", "covers", "Example.png"):            false,
		filepath.Join("library", "PSP", "media", "Example", "boxFront.jpg"): false,
	}
	for _, candidate := range candidates {
		if _, ok := wanted[candidate]; ok {
			wanted[candidate] = true
		}
	}
	for path, found := range wanted {
		if !found {
			t.Fatalf("candidate %q not found in %#v", path, candidates)
		}
	}
}

func TestLocalGameCoverCandidatesMatchNormalizedSingularBoxart(t *testing.T) {
	root := t.TempDir()
	gamePath := filepath.Join(root, "VirtualBoy", "3-D Tetris (USA).vb")
	coverPath := filepath.Join(root, "VirtualBoy", "boxart", "3D Tetris.png")
	if err := os.MkdirAll(filepath.Dir(coverPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(coverPath, []byte("cover"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, candidate := range localGameCoverCandidates(gamePath) {
		if candidate == coverPath {
			return
		}
	}
	t.Fatalf("normalized boxart candidate %q not found", coverPath)
}

func TestNetworkCoverTaskHonorsLibretroSetting(t *testing.T) {
	configDir := t.TempDir()
	conn, err := db.Open(configDir)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	svc := &Service{store: store.New(conn), configDir: configDir}
	settings := svc.defaultGameCatalogSettings()
	settings.EnableLibretroCovers = false
	if err := svc.SaveGameCatalogSettings(settings); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StartGameCoverMatch(true); err == nil {
		t.Fatal("network cover task started while Libretro matching was disabled")
	}
}
