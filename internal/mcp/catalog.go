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
	"unicode/utf8"
)

type catalogSpec struct{ description, path, fields string }

var catalogSpecs = map[string]catalogSpec{
	"foliospace.list_books":     {"List all indexed book/comic assets with real SQL pagination and exact format filtering. Shared metadata only; no private reader state. Stable title/id or recent/id order while catalog is unchanged.", "/api/client/catalog/books", "q format sort limit offset profileId"},
	"foliospace.browse_comics":  {"Browse comic folders and collections by library and relative path, or search collection titles. Shared metadata only; offset pagination.", "/api/client/catalog/comics", "q libraryId path limit offset profileId"},
	"foliospace.search_library": {"Search books (including comics), all indexed game roles and video files. Global keyset pagination ordered by book/game/video then id, not relevance. Follow nextCursor with identical q, format and profileId to cover every matching row beyond default batches. Later inserts are excluded; restart after metadata edits. Format filters all media. Shared metadata only.", "/api/client/catalog/search", "q format limit cursor profileId"},
}

func catalogTools() []Tool {
	names := make([]string, 0, len(catalogSpecs))
	for n := range catalogSpecs {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]Tool, 0, len(names))
	for _, name := range names {
		spec := catalogSpecs[name]
		props := map[string]any{}
		for _, f := range strings.Fields(spec.fields) {
			switch f {
			case "limit", "offset", "profileId", "libraryId":
				min, max := 0, 9007199254740991
				if f == "limit" {
					min, max = 1, 100
				}
				if f == "profileId" {
					min = 1
				}
				if f == "offset" {
					max = 10000000
				}
				props[f] = map[string]any{"type": "integer", "minimum": min, "maximum": max}
			default:
				props[f] = stringSchema(f)
			}
		}
		if props["sort"] != nil {
			props["sort"] = map[string]any{"type": "string", "enum": []string{"title", "recent"}}
		}
		schema := objectSchema(props, nil)
		schema["additionalProperties"] = false
		out = append(out, Tool{Name: name, Description: spec.description, InputSchema: schema, Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}})
	}
	return out
}

func (s *Server) callCatalogTool(ctx context.Context, spec catalogSpec, args map[string]any) map[string]any {
	allowed := map[string]bool{}
	for _, f := range strings.Fields(spec.fields) {
		allowed[f] = true
	}
	query := url.Values{}
	for key, value := range args {
		if !allowed[key] {
			return videoError("invalid-arguments", 0, nil)
		}
		switch key {
		case "limit", "offset", "profileId", "libraryId":
			n, ok := value.(float64)
			min, max := float64(0), float64(9007199254740991)
			if key == "limit" {
				min, max = 1, 100
			}
			if key == "profileId" {
				min = 1
			}
			if key == "offset" {
				max = 10000000
			}
			if !ok || n < min || n > max || math.Trunc(n) != n {
				return videoError("invalid-arguments", 0, nil)
			}
			query.Set(key, strconv.FormatInt(int64(n), 10))
		default:
			v, ok := value.(string)
			max := 512
			if key == "path" {
				max = 4096
			}
			if key == "cursor" {
				max = 2048
			}
			if key == "format" {
				max = 32
			}
			if !ok || len(v) > max || (key == "sort" && v != "title" && v != "recent") {
				return videoError("invalid-arguments", 0, nil)
			}
			query.Set(key, v)
		}
	}
	info, failure := s.videoGet(ctx, "/api/client/info")
	if failure != nil {
		return failure
	}
	object, ok := info.(map[string]any)
	if !ok {
		return videoError("invalid-response", 0, nil)
	}
	caps, _ := object["capabilities"].(map[string]any)
	if caps["catalogSearchV1"] != true {
		return videoError("capability-unsupported", 0, nil)
	}
	data, failure := s.catalogGet(ctx, spec.path+"?"+query.Encode(), spec.path == "/api/client/catalog/comics")
	if failure != nil {
		return failure
	}
	return toolResult(data)
}

// Comic navigation needs safe relative directory paths. The legacy video
// sanitizer intentionally removes every path, so restore only these two
// documented locations after validation, never arbitrary filesystem fields.
func (s *Server) catalogGet(ctx context.Context, path string, comics bool) (any, map[string]any) {
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
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBytes+1))
	if err != nil {
		return nil, videoError("response-read-failed", res.StatusCode, nil)
	}
	if len(raw) > maxBytes {
		return nil, videoError("response-too-large", res.StatusCode, nil)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		code := fmt.Sprintf("http-%d", res.StatusCode)
		var failure map[string]any
		if json.Unmarshal(raw, &failure) == nil {
			if upstream, ok := failure["code"].(string); ok && safeVideoCode(upstream) {
				code = upstream
			}
		}
		return nil, videoError(code, res.StatusCode, nil)
	}
	var original map[string]any
	if json.Unmarshal(raw, &original) != nil || original == nil {
		return nil, videoError("invalid-response", res.StatusCode, nil)
	}
	// sanitizeVideo rewrites array elements in place; retain the original item
	// maps before sanitizing so their validated navigation paths remain available.
	originalItems, _ := original["items"].([]any)
	oldItems := append([]any(nil), originalItems...)
	clean, _ := sanitizeVideo(original).(map[string]any)
	if comics {
		restoreComicPath(clean, original)
		newItems, _ := clean["items"].([]any)
		for i, item := range oldItems {
			if i < len(newItems) {
				old, _ := item.(map[string]any)
				next, _ := newItems[i].(map[string]any)
				restoreComicPath(next, old)
			}
		}
	}
	return clean, nil
}

func restoreComicPath(dst, src map[string]any) {
	if dst == nil || src == nil {
		return
	}
	p, ok := src["path"].(string)
	if !ok || len(p) > 4096 || !utf8.ValidString(p) || strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\\x00:") {
		return
	}
	if p != "" {
		for _, part := range strings.Split(p, "/") {
			if part == "" || part == "." || part == ".." {
				return
			}
		}
	}
	dst["path"] = p
}
