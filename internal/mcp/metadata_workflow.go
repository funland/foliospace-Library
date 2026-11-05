package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

type metadataWorkflowSpec struct {
	action, description, fields, required string
	readOnly, openWorld, idempotent       bool
}

var metadataWorkflowSpecs = map[string]metadataWorkflowSpec{
	"foliospace.search_video_metadata":  {"search", "Queue a TMDB candidate search for one video. Writes a metadata job and contacts TMDB; never applies a match. A profile is a state namespace; metadata changes affect the shared library.", "query kind", "query kind", false, true, false},
	"foliospace.get_video_metadata_job": {"jobs", "Read a single video's metadata search job and candidates without modifying provider caches.", "jobId", "jobId", true, false, true},
	"foliospace.preview_video_metadata": {"preview", "Preview one server-stored candidate for one video and issue an expiring receipt. Reports exact shared-library changes while preserving manual overrides and private state. Series require season and episode.", "jobId candidateIndex season episode version", "jobId candidateIndex", true, false, false},
	"foliospace.apply_video_metadata":   {"apply", "Apply one reviewed metadata receipt with explicit confirm:true. Changes shared-library association for one file; preserves manual overrides and profile private state. Stale, expired, or replayed receipts fail.", "receipt confirm", "receipt confirm", false, false, false},
}

func metadataWorkflowTools() []Tool {
	names := make([]string, 0, len(metadataWorkflowSpecs))
	for name := range metadataWorkflowSpecs {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]Tool, 0, len(names))
	for _, name := range names {
		spec := metadataWorkflowSpecs[name]
		props := map[string]any{}
		for _, field := range strings.Fields("profileId videoId " + spec.fields) {
			switch field {
			case "query", "kind", "version", "receipt":
				p := stringSchema(field)
				if field == "kind" {
					p["enum"] = []string{"movie", "series"}
				}
				if field == "query" || field == "version" {
					p["maxLength"] = 500
				}
				if field == "receipt" {
					p["minLength"] = 1
					p["maxLength"] = 256
				}
				props[field] = p
			case "confirm":
				props[field] = map[string]any{"type": "boolean", "const": true, "description": "Explicit approval of the reviewed receipt."}
			default:
				minimum, maximum := int64(1), int64(9007199254740991)
				if field == "candidateIndex" || field == "season" {
					minimum, maximum = 0, 999
				}
				if field == "episode" {
					minimum, maximum = 1, 99999
				}
				props[field] = map[string]any{"type": "integer", "minimum": minimum, "maximum": maximum}
			}
		}
		schema := objectSchema(props, strings.Fields("profileId videoId "+spec.required))
		schema["additionalProperties"] = false
		result = append(result, Tool{Name: name, Description: spec.description, InputSchema: schema, Annotations: map[string]any{"readOnlyHint": spec.readOnly, "destructiveHint": spec.action == "apply", "idempotentHint": spec.idempotent, "openWorldHint": spec.openWorld}})
	}
	return result
}

func (s *Server) callMetadataWorkflowTool(ctx context.Context, spec metadataWorkflowSpec, args map[string]any) map[string]any {
	allowed := map[string]bool{}
	for _, field := range strings.Fields("profileId videoId " + spec.fields) {
		allowed[field] = true
	}
	for _, field := range strings.Fields("profileId videoId " + spec.required) {
		if args[field] == nil {
			return videoError("invalid-arguments", 0, nil)
		}
	}
	for field, value := range args {
		if !allowed[field] {
			return videoError("invalid-arguments", 0, nil)
		}
		switch field {
		case "query", "kind", "version", "receipt":
			v, ok := value.(string)
			if !ok || len(v) > 500 || (field == "query" && strings.TrimSpace(v) == "") || (field == "receipt" && (len(v) == 0 || len(v) > 256)) || (field == "kind" && v != "movie" && v != "series") {
				return videoError("invalid-arguments", 0, nil)
			}
		case "confirm":
			v, ok := value.(bool)
			if !ok || !v {
				return videoError("invalid-arguments", 0, nil)
			}
		default:
			n, ok := value.(float64)
			minimum, maximum := float64(1), float64(9007199254740991)
			if field == "candidateIndex" || field == "season" {
				minimum, maximum = 0, 999
			}
			if field == "episode" {
				minimum, maximum = 1, 99999
			}
			if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n < minimum || n > maximum || math.Trunc(n) != n {
				return videoError("invalid-arguments", 0, nil)
			}
		}
	}
	if (args["season"] == nil) != (args["episode"] == nil) {
		return videoError("invalid-arguments", 0, nil)
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
	if caps["videoMetadataWorkflowV1"] != true {
		return videoError("capability-unsupported", 0, nil)
	}
	id := int64(args["videoId"].(float64))
	path := fmt.Sprintf("/api/client/metadata/videos/%d/%s", id, spec.action)
	if spec.action == "jobs" {
		path += "/" + strconv.FormatInt(int64(args["jobId"].(float64)), 10) + "?profileId=" + strconv.FormatInt(int64(args["profileId"].(float64)), 10)
		data, failure := s.videoGet(ctx, path)
		if failure != nil {
			return failure
		}
		return toolResult(data)
	}
	body := make(map[string]any, len(args)-1)
	for field, value := range args {
		if field != "videoId" {
			body[field] = value
		}
	}
	data, failure := s.metadataWorkflowPost(ctx, path, body)
	if failure != nil {
		return failure
	}
	return toolResult(data)
}

// Match the bounded, non-redirecting video GET transport for write/preview calls.
func (s *Server) metadataWorkflowPost(ctx context.Context, path string, input any) (any, map[string]any) {
	body, err := json.Marshal(input)
	if err != nil {
		return nil, videoError("invalid-arguments", 0, nil)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, videoError("request-failed", 0, nil)
	}
	req.Header.Set("Content-Type", "application/json")
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
	var data any
	decodeErr := json.Unmarshal(raw, &data)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		code := fmt.Sprintf("http-%d", res.StatusCode)
		if m, ok := data.(map[string]any); ok {
			if c, ok := m["code"].(string); ok && safeVideoCode(c) {
				code = c
			}
		}
		return nil, videoError(code, res.StatusCode, nil)
	}
	if decodeErr != nil {
		return nil, videoError("invalid-response", res.StatusCode, nil)
	}
	return sanitizeVideo(data), nil
}
