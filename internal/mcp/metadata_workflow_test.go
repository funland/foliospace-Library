package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestMetadataWorkflowToolsRoutingAndPayload(t *testing.T) {
	for _, tc := range []struct {
		name, path, method string
		args               map[string]any
	}{
		{"search_video_metadata", "/api/client/metadata/videos/7/search", "POST", map[string]any{"videoId": 7, "profileId": 2, "query": "Synthetic", "kind": "movie"}},
		{"get_video_metadata_job", "/api/client/metadata/videos/7/jobs/9?profileId=2", "GET", map[string]any{"videoId": 7, "profileId": 2, "jobId": 9}},
		{"preview_video_metadata", "/api/client/metadata/videos/7/preview", "POST", map[string]any{"videoId": 7, "profileId": 2, "jobId": 9, "candidateIndex": 0, "season": 0, "episode": 1, "version": "1080p"}},
		{"apply_video_metadata", "/api/client/metadata/videos/7/apply", "POST", map[string]any{"videoId": 7, "profileId": 2, "receipt": "opaque-receipt", "confirm": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := New("http://synthetic", "admin-secret")
			calls := 0
			s.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("Authorization") != "Bearer admin-secret" {
					t.Fatal("missing auth")
				}
				if calls == 1 {
					return jsonResponse(`{"capabilities":{"videoMetadataWorkflowV1":true}}`), nil
				}
				if r.Method != tc.method || r.URL.RequestURI() != tc.path {
					t.Fatalf("%s %s", r.Method, r.URL.RequestURI())
				}
				if r.Method == "POST" {
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					if body["videoId"] != nil || body["profileId"] != float64(2) || len(body) != len(tc.args)-1 {
						t.Fatalf("unexpected body %+v", body)
					}
					for k, v := range tc.args {
						if k != "videoId" && fmt.Sprint(body[k]) != fmt.Sprint(v) {
							t.Fatalf("%s changed: %+v", k, body)
						}
					}
				}
				return jsonResponse(`{"receipt":"opaque-receipt","scope":"shared-library","filePath":"/private/data","providerToken":"secret"}`), nil
			})
			result := s.Handle(context.Background(), toolCall(t, "foliospace."+tc.name, tc.args))
			encoded := mustJSON(t, result)
			if result.Error != nil || calls != 2 || strings.Contains(encoded, `"isError":true`) || !strings.Contains(encoded, "opaque-receipt") || strings.Contains(encoded, "/private/") || strings.Contains(encoded, "secret") {
				t.Fatal(encoded, calls)
			}
		})
	}
}

func TestMetadataWorkflowInvalidArgumentsNeverRequest(t *testing.T) {
	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"search_video_metadata", map[string]any{"videoId": 1, "profileId": 1, "query": "x", "kind": "poster"}},
		{"search_video_metadata", map[string]any{"videoId": 1, "profileId": 1, "query": " ", "kind": "movie"}},
		{"search_video_metadata", map[string]any{"videoId": 1, "profileId": 1, "query": strings.Repeat("x", 501), "kind": "movie"}},
		{"get_video_metadata_job", map[string]any{"videoId": 1, "jobId": 1}},
		{"get_video_metadata_job", map[string]any{"videoId": 1, "profileId": 0, "jobId": 1}},
		{"get_video_metadata_job", map[string]any{"videoId": 1.2, "profileId": 1, "jobId": 1}},
		{"get_video_metadata_job", map[string]any{"videoId": 1, "profileId": 1, "jobId": "1"}},
		{"preview_video_metadata", map[string]any{"videoId": 1, "profileId": 1, "jobId": 1}},
		{"preview_video_metadata", map[string]any{"videoId": 1, "profileId": 1, "jobId": 1, "candidateIndex": -1}},
		{"preview_video_metadata", map[string]any{"videoId": 1, "profileId": 1, "jobId": 1, "candidateIndex": 0, "season": 1}},
		{"preview_video_metadata", map[string]any{"videoId": 1, "profileId": 1, "jobId": 1, "candidateIndex": 0, "title": "Client metadata"}},
		{"apply_video_metadata", map[string]any{"videoId": 1, "profileId": 1, "receipt": "opaque"}},
		{"apply_video_metadata", map[string]any{"videoId": 1, "profileId": 1, "receipt": "opaque", "confirm": false}},
		{"apply_video_metadata", map[string]any{"videoId": 1, "profileId": 1, "receipt": "opaque", "confirm": "true"}},
		{"apply_video_metadata", map[string]any{"videoId": 1, "profileId": 1, "receipt": "opaque", "confirm": true, "generation": 4}},
		{"apply_video_metadata", map[string]any{"videoId": 1, "profileId": 1, "receipt": "", "confirm": true}},
	} {
		s := New("http://synthetic", "secret")
		s.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("invalid argument reached network")
			return nil, nil
		})
		result := s.Handle(context.Background(), toolCall(t, "foliospace."+tc.name, tc.args))
		if !strings.Contains(mustJSON(t, result.Result), "invalid-arguments") {
			t.Fatalf("%s: %+v", tc.name, result)
		}
	}
}

func TestMetadataWorkflowCapabilityAndSafeFailure(t *testing.T) {
	for _, tc := range []struct {
		capability bool
		status     int
		body, want string
	}{
		{false, 200, `{}`, "capability-unsupported"},
		{true, 409, `{"code":"metadata-preview-stale","error":"/private/admin-secret"}`, "metadata-preview-stale"},
		{true, 500, `{"code":"/private/admin-secret","error":"/private/admin-secret"}`, "http-500"},
		{true, 200, `{invalid-json`, "invalid-response"},
		{true, 200, strings.Repeat(" ", (2<<20)+1), "response-too-large"},
	} {
		s := New("http://synthetic", "secret")
		calls := 0
		s.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return jsonResponse(fmt.Sprintf(`{"capabilities":{"videoMetadataWorkflowV1":%t}}`, tc.capability)), nil
			}
			res := jsonResponse(tc.body)
			res.StatusCode = tc.status
			return res, nil
		})
		result := s.Handle(context.Background(), toolCall(t, "foliospace.apply_video_metadata", map[string]any{"videoId": 1, "profileId": 1, "receipt": "opaque", "confirm": true}))
		encoded := mustJSON(t, result.Result)
		if !strings.Contains(encoded, tc.want) || strings.Contains(encoded, "/private/") || strings.Contains(encoded, "admin-secret") {
			t.Fatal(encoded)
		}
		if !tc.capability && calls != 1 {
			t.Fatal("unsupported capability applied")
		}
	}
}

func TestMetadataWorkflowPostRedirectDoesNotForwardCredentials(t *testing.T) {
	s := New("http://synthetic", "secret")
	calls := 0
	s.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "synthetic" || calls > 2 {
			t.Fatal("redirect followed with credentials")
		}
		if calls == 1 {
			return jsonResponse(`{"capabilities":{"videoMetadataWorkflowV1":true}}`), nil
		}
		res := jsonResponse(`{}`)
		res.StatusCode = http.StatusTemporaryRedirect
		res.Header.Set("Location", "http://redirected.example/collect")
		return res, nil
	})
	result := s.Handle(context.Background(), toolCall(t, "foliospace.apply_video_metadata", map[string]any{"videoId": 1, "profileId": 1, "receipt": "opaque", "confirm": true}))
	if !strings.Contains(mustJSON(t, result.Result), "http-307") {
		t.Fatal(result)
	}
}

func TestMetadataWorkflowToolSchemasAndFraming(t *testing.T) {
	for _, tool := range metadataWorkflowTools() {
		if tool.InputSchema["additionalProperties"] != false {
			t.Fatal(tool.Name)
		}
		if tool.Name == "foliospace.search_video_metadata" && (tool.Annotations["readOnlyHint"] != false || tool.Annotations["openWorldHint"] != true) {
			t.Fatal("search annotations hide effects")
		}
		if tool.Name == "foliospace.apply_video_metadata" && (tool.Annotations["readOnlyHint"] != false || tool.Annotations["idempotentHint"] != false || tool.Annotations["destructiveHint"] != true) {
			t.Fatal("apply annotations hide effects")
		}
		if tool.Name == "foliospace.preview_video_metadata" && (tool.Annotations["readOnlyHint"] != true || tool.Annotations["openWorldHint"] != false) {
			t.Fatal("preview annotations incorrect")
		}
	}
	for _, framed := range []bool{false, true} {
		body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
		input := body + "\n"
		if framed {
			input = fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
		}
		var out bytes.Buffer
		if err := New("http://synthetic", "").Serve(context.Background(), strings.NewReader(input), &out); err != nil {
			t.Fatal(err)
		}
		for name := range metadataWorkflowSpecs {
			if !strings.Contains(out.String(), name) {
				t.Fatal("missing tool", name)
			}
		}
	}
}
