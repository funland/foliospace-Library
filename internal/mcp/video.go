package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

type videoSpec struct {
	description, capability, path, id, fields, kind string
	paged                                           bool
}

var videoSpecs = map[string]videoSpec{
	"foliospace.list_video_works":         {"List movies and series; older servers return explicitly labelled legacy files.", "videoLibraryV1", "/api/client/video-works", "", "q kind libraryId sort", "", true},
	"foliospace.get_video_work":           {"Get one movie or series by stable work ID.", "videoLibraryV1", "/api/client/video-works/%d", "workId", "", "", false},
	"foliospace.list_video_seasons":       {"List seasons, including season zero specials.", "videoSeasonsV1", "/api/client/video-works/%d/seasons", "workId", "", "", true},
	"foliospace.list_video_episodes":      {"List episodes with optional season filter.", "videoSeasonsV1", "/api/client/video-works/%d/episodes", "workId", "season", "", true},
	"foliospace.list_video_versions":      {"List playable file versions; each video ID has independent playback state.", "videoLibraryV1", "/api/client/video-works/%d/files", "workId", "season episodeId", "", true},
	"foliospace.get_video_availability":   {"Read local episode availability; unknown expected counts are not zero.", "videoEpisodeAvailabilityV1", "/api/client/video-works/%d/availability", "workId", "season", "", true},
	"foliospace.get_video_playback_state": {"Read playback state for a specific file and optional profile.", "videoPlaybackStateV1", "/api/client/videos/%d/playback-state", "videoId", "profileId", "", false},
	"foliospace.list_video_continue":      {"List continue-watching items for the requested profile.", "videoPlaybackStateV1", "/api/client/video-playback", "", "profileId libraryId", "continue", true},
	"foliospace.list_video_next_up":       {"List next-up episodes for the requested profile.", "videoNextUpV1", "/api/client/video-playback", "", "profileId libraryId", "next-up", true},
}

func videoTools() []Tool {
	names := make([]string, 0, len(videoSpecs))
	for name := range videoSpecs {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]Tool, 0, len(names))
	for _, name := range names {
		s := videoSpecs[name]
		props := map[string]any{}
		var required []string
		fields := strings.Fields(s.fields)
		if s.id != "" {
			fields = append(fields, s.id)
			required = []string{s.id}
		}
		if s.paged {
			fields = append(fields, "limit", "offset")
		}
		if s.capability == "videoEpisodeAvailabilityV1" {
			required = append(required, "season")
		}
		for _, f := range fields {
			if f == "q" || f == "kind" || f == "sort" {
				props[f] = stringSchema(f)
			} else {
				minimum := 1
				if f == "offset" || f == "season" {
					minimum = 0
				}
				props[f] = map[string]any{"type": "integer", "minimum": minimum, "description": f + " (limit defaults to 20, capped at 100)"}
			}
		}
		propsKind, _ := props["kind"].(map[string]any)
		if propsKind != nil {
			propsKind["enum"] = []string{"movie", "series"}
		}
		propsSort, _ := props["sort"].(map[string]any)
		if propsSort != nil {
			propsSort["enum"] = []string{"title", "recent"}
		}
		schema := objectSchema(props, required)
		schema["additionalProperties"] = false
		result = append(result, Tool{Name: name, Description: s.description, InputSchema: schema, Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}})
	}
	return result
}

func videoError(code string, status int, state any) map[string]any {
	d := map[string]any{"code": code}
	if status != 0 {
		d["status"] = status
	}
	if state != nil {
		d["state"] = sanitizeVideo(state)
	}
	r := toolResult(d)
	r["isError"] = true
	return r
}

func (s *Server) callVideoTool(ctx context.Context, spec videoSpec, args map[string]any) map[string]any {
	if spec.capability == "videoEpisodeAvailabilityV1" && args["season"] == nil {
		return videoError("invalid-arguments", 0, nil)
	}
	allowed := map[string]bool{}
	for _, k := range strings.Fields(spec.fields) {
		allowed[k] = true
	}
	if spec.id != "" {
		allowed[spec.id] = true
	}
	if spec.paged {
		allowed["limit"] = true
		allowed["offset"] = true
	}
	query := url.Values{}
	var id int64
	if spec.paged {
		query.Set("limit", "20")
		query.Set("offset", "0")
	}
	if spec.kind != "" {
		query.Set("kind", spec.kind)
	}
	for k, v := range args {
		if !allowed[k] {
			return videoError("invalid-arguments", 0, nil)
		}
		if k == "q" || k == "kind" || k == "sort" {
			text, ok := v.(string)
			if !ok || len(text) > 1024 {
				return videoError("invalid-arguments", 0, nil)
			}
			if k == "kind" && text != "movie" && text != "series" || k == "sort" && text != "title" && text != "recent" {
				return videoError("invalid-arguments", 0, nil)
			}
			query.Set(k, text)
			continue
		}
		n, ok := v.(float64)
		min := float64(1)
		if k == "offset" || k == "season" {
			min = 0
		}
		if !ok || n < min || n > 9007199254740991 || math.Trunc(n) != n || k == "season" && n > 999 || k == "offset" && n > 10000000 {
			return videoError("invalid-arguments", 0, nil)
		}
		if k == "limit" && n > 100 {
			n = 100
		}
		if k == spec.id {
			id = int64(n)
		} else {
			query.Set(k, strconv.FormatInt(int64(n), 10))
		}
	}
	if spec.id != "" && id == 0 {
		return videoError("invalid-arguments", 0, nil)
	}
	info, failure := s.videoGet(ctx, "/api/client/info")
	if failure != nil {
		return failure
	}
	object, valid := info.(map[string]any)
	if !valid {
		return videoError("invalid-response", 0, nil)
	}
	caps, _ := object["capabilities"].(map[string]any)
	if caps[spec.capability] != true {
		if spec.path != "/api/client/video-works" || args["kind"] != nil || args["libraryId"] != nil {
			return videoError("capability-unsupported", 0, nil)
		}
		data, failed := s.videoGet(ctx, "/api/client/videos?"+query.Encode())
		if failed != nil {
			return failed
		}
		return toolResult(map[string]any{"mode": "legacy-files", "capability": "videoLibraryV1", "data": data})
	}
	path := spec.path
	if spec.id != "" {
		path = fmt.Sprintf(path, id)
	}
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	data, failure := s.videoGet(ctx, path)
	if failure != nil {
		return failure
	}
	return toolResult(data)
}

// Keep the old tools' contracts intact; new tools use bounded, non-redirecting GETs.
// Never surface upstream diagnostic bodies (which may contain paths or credentials).
func (s *Server) videoGet(ctx context.Context, path string) (any, map[string]any) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+path, nil)
	if err != nil {
		return nil, videoError("request-failed", 0, nil)
	}
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	client := *s.httpClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		return nil, videoError("request-failed", 0, nil)
	}
	defer res.Body.Close()
	const maxBytes = 2 << 20
	body, err := io.ReadAll(io.LimitReader(res.Body, maxBytes+1))
	if err != nil {
		return nil, videoError("response-read-failed", res.StatusCode, nil)
	}
	if len(body) > maxBytes {
		return nil, videoError("response-too-large", res.StatusCode, nil)
	}
	var data any
	decodeErr := json.Unmarshal(body, &data)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		code := fmt.Sprintf("http-%d", res.StatusCode)
		var state any
		if m, ok := data.(map[string]any); ok {
			if c, ok := m["code"].(string); ok && safeVideoCode(c) {
				code = c
			}
			state = m["state"]
		}
		return nil, videoError(code, res.StatusCode, state)
	}
	if decodeErr != nil {
		return nil, videoError("invalid-response", res.StatusCode, nil)
	}
	return sanitizeVideo(data), nil
}

func safeVideoCode(s string) bool {
	if len(s) == 0 || len(s) > 80 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func sanitizeVideo(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, item := range v {
			lower := strings.ToLower(k)
			if strings.Contains(lower, "token") || strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.HasSuffix(lower, "path") || lower == "error" || lower == "authorization" {
				continue
			}
			out[k] = sanitizeVideo(item)
		}
		return out
	case []any:
		for i, item := range v {
			v[i] = sanitizeVideo(item)
		}
		return v
	default:
		return value
	}
}
