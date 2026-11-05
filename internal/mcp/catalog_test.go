package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func catalogResultObject(t *testing.T, response Response) (map[string]any, bool) {
	t.Helper()
	if response.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %+v", response.Error)
	}
	var result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal([]byte(mustJSON(t, response.Result)), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 || result.Content[0].Type != "text" {
		t.Fatalf("unexpected tool content: %#v", result)
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &data); err != nil {
		t.Fatalf("invalid tool JSON: %v: %s", err, result.Content[0].Text)
	}
	return data, result.IsError
}

func TestCatalogToolsAdvertiseReadOnlyStrictSchemas(t *testing.T) {
	response := New("http://unused.test", "").Handle(context.Background(), Request{Method: "tools/list"})
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	var listed struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal([]byte(mustJSON(t, response.Result)), &listed); err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"foliospace.list_books":     {"q", "format", "sort", "limit", "offset", "profileId"},
		"foliospace.browse_comics":  {"q", "libraryId", "path", "limit", "offset", "profileId"},
		"foliospace.search_library": {"q", "format", "limit", "cursor", "profileId"},
	}
	seen := map[string]bool{}
	for _, tool := range listed.Tools {
		fields, ok := want[tool.Name]
		if !ok {
			continue
		}
		if seen[tool.Name] {
			t.Errorf("catalog tool registered twice: %s", tool.Name)
		}
		seen[tool.Name] = true
		if tool.Annotations["readOnlyHint"] != true || tool.Annotations["destructiveHint"] != false || tool.Annotations["idempotentHint"] != true || tool.Annotations["openWorldHint"] != false {
			t.Errorf("%s annotations = %#v", tool.Name, tool.Annotations)
		}
		if tool.InputSchema["additionalProperties"] != false {
			t.Errorf("%s permits undocumented arguments", tool.Name)
		}
		props, ok := tool.InputSchema["properties"].(map[string]any)
		if !ok || len(props) != len(fields) {
			t.Fatalf("%s properties = %#v", tool.Name, props)
		}
		for _, field := range fields {
			if props[field] == nil {
				t.Errorf("%s missing %s schema", tool.Name, field)
			}
		}
		limit := props["limit"].(map[string]any)
		profile := props["profileId"].(map[string]any)
		if limit["type"] != "integer" || limit["minimum"] != float64(1) || limit["maximum"] != float64(100) || profile["minimum"] != float64(1) {
			t.Errorf("%s invalid bounds: limit=%v profile=%v", tool.Name, limit, profile)
		}
	}
	if len(seen) != len(want) {
		t.Fatalf("registered catalog tools = %v, want %v", seen, want)
	}
}

func TestCatalogInvalidArgumentsNeverMakeHTTPRequests(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"capabilities":{"catalogSearchV1":true}}`)
	}))
	defer upstream.Close()
	common := []map[string]any{
		{"unknown": true}, {"profileId": 0}, {"profileId": -1}, {"profileId": "2"}, {"profileId": 1.5}, {"profileId": 9007199254740992.0},
		{"limit": 0}, {"limit": -1}, {"limit": 101}, {"limit": 2.5}, {"limit": "10"}, {"limit": nil},
		{"q": true}, {"q": strings.Repeat("x", 513)},
	}
	for _, name := range []string{"list_books", "browse_comics", "search_library"} {
		cases := append([]map[string]any{}, common...)
		switch name {
		case "list_books":
			cases = append(cases, map[string]any{"sort": "relevance"}, map[string]any{"sort": ""}, map[string]any{"offset": -1}, map[string]any{"offset": 10000001}, map[string]any{"offset": 0.5}, map[string]any{"cursor": "ignored"}, map[string]any{"format": strings.Repeat("x", 33)}, map[string]any{"format": 1})
		case "browse_comics":
			cases = append(cases, map[string]any{"libraryId": -1}, map[string]any{"libraryId": "2"}, map[string]any{"libraryId": 0.5}, map[string]any{"path": strings.Repeat("x", 4097)}, map[string]any{"path": nil}, map[string]any{"offset": -1}, map[string]any{"format": "cbz"})
		case "search_library":
			cases = append(cases, map[string]any{"offset": 1}, map[string]any{"sort": "title"}, map[string]any{"cursor": strings.Repeat("x", 2049)}, map[string]any{"cursor": 1}, map[string]any{"format": strings.Repeat("x", 33)})
		}
		for i, args := range cases {
			t.Run(fmt.Sprintf("%s/%d", name, i), func(t *testing.T) {
				data, isError := catalogResultObject(t, New(upstream.URL, "").Handle(context.Background(), toolCall(t, "foliospace."+name, args)))
				if !isError || data["code"] != "invalid-arguments" {
					t.Errorf("args=%v: got error=%v, result=%v", args, isError, data)
				}
				if got := calls.Load(); got != 0 {
					t.Fatalf("invalid args triggered %d HTTP requests", got)
				}
			})
		}
	}
}

func TestCatalogCapabilityMissingDoesNotFallBack(t *testing.T) {
	for _, name := range []string{"list_books", "browse_comics", "search_library"} {
		for _, caps := range []string{`{}`, `{"capabilities":{}}`, `{"capabilities":{"catalogSearchV1":false}}`, `{"capabilities":{"catalogSearchV1":"true"}}`} {
			t.Run(name+"/"+caps, func(t *testing.T) {
				var calls atomic.Int64
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					if r.URL.Path != "/api/client/info" {
						t.Errorf("unsupported catalog capability reached %s", r.URL.Path)
					}
					fmt.Fprint(w, caps)
				}))
				defer upstream.Close()
				data, isError := catalogResultObject(t, New(upstream.URL, "").Handle(context.Background(), toolCall(t, "foliospace."+name, nil)))
				if !isError || data["code"] != "capability-unsupported" || calls.Load() != 1 {
					t.Fatalf("error=%v data=%v calls=%d", isError, data, calls.Load())
				}
			})
		}
	}
}

func TestCatalogToolsForwardAuthenticatedGETAndPagination(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		args map[string]any
		want url.Values
	}{
		{"list_books", "/api/client/catalog/books", map[string]any{"q": "A & B 漫画", "format": "cbz", "sort": "title", "limit": 100, "offset": 200, "profileId": 2}, url.Values{"q": {"A & B 漫画"}, "format": {"cbz"}, "sort": {"title"}, "limit": {"100"}, "offset": {"200"}, "profileId": {"2"}}},
		{"browse_comics", "/api/client/catalog/comics", map[string]any{"q": "hero", "libraryId": 7, "path": "Manga/A & B", "limit": 37, "offset": 74, "profileId": 3}, url.Values{"q": {"hero"}, "libraryId": {"7"}, "path": {"Manga/A & B"}, "limit": {"37"}, "offset": {"74"}, "profileId": {"3"}}},
		{"search_library", "/api/client/catalog/search", map[string]any{"q": "space", "format": "zip", "limit": 97, "cursor": "opaque_-cursor", "profileId": 4}, url.Values{"q": {"space"}, "format": {"zip"}, "limit": {"97"}, "cursor": {"opaque_-cursor"}, "profileId": {"4"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer catalog-test-token" {
					t.Errorf("unexpected method/auth: %s %q", r.Method, r.Header.Get("Authorization"))
				}
				if n == 1 {
					if r.URL.Path != "/api/client/info" || len(r.URL.Query()) != 0 {
						t.Errorf("first request must negotiate capabilities: %s", r.URL)
					}
					fmt.Fprint(w, `{"capabilities":{"catalogSearchV1":true}}`)
					return
				}
				if n != 2 || r.URL.Path != tc.path || !reflect.DeepEqual(r.URL.Query(), tc.want) {
					t.Errorf("request #%d = %s, expected %s?%s", n, r.URL, tc.path, tc.want.Encode())
				}
				fmt.Fprint(w, `{"items":[{"id":8,"title":"Visible title"}],"total":345,"limit":97,"hasMore":true,"nextCursor":"next_opaque","profileId":4,"scope":"shared-catalog"}`)
			}))
			defer upstream.Close()
			data, isError := catalogResultObject(t, New(upstream.URL, "catalog-test-token").Handle(context.Background(), toolCall(t, "foliospace."+tc.name, tc.args)))
			if isError || calls.Load() != 2 || data["total"] != float64(345) || data["hasMore"] != true || data["nextCursor"] != "next_opaque" {
				t.Fatalf("error=%v data=%v calls=%d", isError, data, calls.Load())
			}
		})
	}
}

func TestCatalogComicPathsRemainUsableForFolderTraversal(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/client/info" {
			fmt.Fprint(w, `{"capabilities":{"catalogSearchV1":true}}`)
			return
		}
		fmt.Fprint(w, `{"items":[{"id":1,"kind":"folder","path":"Manga/Space","displayTitle":"Space","filePath":"/private/library/Space.cbz"}],"path":"Manga","total":1,"limit":20,"hasMore":false,"token":"secret-token"}`)
	}))
	defer upstream.Close()
	response := New(upstream.URL, "").Handle(context.Background(), toolCall(t, "foliospace.browse_comics", map[string]any{"libraryId": 1, "path": "Manga"}))
	data, isError := catalogResultObject(t, response)
	items, ok := data["items"].([]any)
	if isError || !ok || len(items) != 1 {
		t.Fatalf("error=%v data=%v", isError, data)
	}
	item, ok := items[0].(map[string]any)
	if !ok || data["path"] != "Manga" || item["path"] != "Manga/Space" {
		t.Errorf("relative comic paths needed for traversal were lost: %v", data)
	}
	if output := mustJSON(t, response.Result); strings.Contains(output, "/private/") || strings.Contains(output, "secret-token") {
		t.Errorf("sensitive upstream values leaked: %s", output)
	}
}

func TestCatalogHTTPAndResponseFailuresAreSurfaced(t *testing.T) {
	for _, tc := range []struct {
		name   string
		phase  string
		status int
		body   string
		code   string
	}{
		{"info unauthorized", "info", 401, `{"error":"private diagnostic"}`, "http-401"},
		{"info forbidden", "info", 403, `{"error":"private diagnostic"}`, "http-403"},
		{"info malformed", "info", 200, `{`, "invalid-response"},
		{"info non-object", "info", 200, `[]`, "invalid-response"},
		{"info null", "info", 200, `null`, "invalid-response"},
		{"info too large", "info", 200, strings.Repeat(" ", (2<<20)+1), "response-too-large"},
		{"catalog unauthorized", "catalog", 401, `{"error":"private diagnostic"}`, "http-401"},
		{"catalog forbidden", "catalog", 403, `{"error":"private diagnostic"}`, "http-403"},
		{"catalog missing profile", "catalog", 404, `{"error":"private diagnostic"}`, "http-404"},
		{"catalog unavailable", "catalog", 500, `{"error":"private diagnostic","filePath":"/private/db"}`, "http-500"},
		{"catalog invalid cursor", "catalog", 400, `{"code":"invalid-arguments","error":"private diagnostic"}`, "invalid-arguments"},
		{"catalog malformed", "catalog", 200, `{`, "invalid-response"},
		{"catalog non-object", "catalog", 200, `[]`, "invalid-response"},
		{"catalog null", "catalog", 200, `null`, "invalid-response"},
		{"catalog trailing JSON", "catalog", 200, `{} {}`, "invalid-response"},
		{"catalog too large", "catalog", 200, strings.Repeat(" ", (2<<20)+1), "response-too-large"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if tc.phase == "catalog" && r.URL.Path == "/api/client/info" {
					fmt.Fprint(w, `{"capabilities":{"catalogSearchV1":true}}`)
					return
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer upstream.Close()
			response := New(upstream.URL, "").Handle(context.Background(), toolCall(t, "foliospace.search_library", nil))
			data, isError := catalogResultObject(t, response)
			wantCalls := int64(1)
			if tc.phase == "catalog" {
				wantCalls = 2
			}
			if !isError || data["code"] != tc.code || calls.Load() != wantCalls {
				t.Errorf("error=%v data=%v calls=%d, expected %s after %d requests", isError, data, calls.Load(), tc.code, wantCalls)
			}
			if output := mustJSON(t, response.Result); strings.Contains(output, "private diagnostic") || strings.Contains(output, "/private/db") {
				t.Errorf("upstream diagnostic leaked: %s", output)
			}
		})
	}
}

func TestCatalogReadAndConnectionFailures(t *testing.T) {
	t.Run("truncated response", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "1000")
			fmt.Fprint(w, `{}`)
		}))
		defer upstream.Close()
		data, isError := catalogResultObject(t, New(upstream.URL, "").Handle(context.Background(), toolCall(t, "foliospace.list_books", nil)))
		if !isError || data["code"] != "response-read-failed" {
			t.Fatalf("error=%v data=%v", isError, data)
		}
	})
	t.Run("connection closed", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		upstream.Close()
		data, isError := catalogResultObject(t, New(upstream.URL, "").Handle(context.Background(), toolCall(t, "foliospace.list_books", nil)))
		if !isError || data["code"] != "request-failed" {
			t.Fatalf("error=%v data=%v", isError, data)
		}
	})
}
