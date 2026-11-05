package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const diagnosticBookResponse = `{"bookId":7,"profileId":2,"source":"stored","filesystemVerified":false,"health":{"status":"unknown"},"index":{"analyzed":true,"declaredPages":3,"indexedPages":3,"status":"indexed"},"issues":[]}`
const diagnosticGameResponse = `{"gameId":7,"profileId":2,"source":"stored","filesystemVerified":false,"catalogRole":"game","files":{"count":2,"checksummed":1,"checksumPending":1},"launch":{"status":"unknown","readyProfiles":0},"metadataStatus":"unmatched","artworkStatus":"missing","issues":[]}`
const diagnosticCapabilities = `{"capabilities":{"storedDiagnosticsV1":true}}`

func diagnosticResult(t *testing.T, r Response) (map[string]any, bool) {
	t.Helper()
	if r.Error != nil {
		t.Fatalf("unexpected protocol error: %+v", r.Error)
	}
	var result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal([]byte(mustJSON(t, r.Result)), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 || result.Content[0].Type != "text" {
		t.Fatalf("unexpected tool content: %+v", result)
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &data); err != nil {
		t.Fatal(err)
	}
	return data, result.IsError
}

func TestDiagnosticToolsRoutesAndStoredResponses(t *testing.T) {
	for _, tc := range []struct {
		name, idKey, path, body string
	}{
		{"foliospace.diagnose_book", "bookId", "/api/client/diagnostics/books/7", diagnosticBookResponse},
		{"foliospace.diagnose_game", "gameId", "/api/client/diagnostics/games/7", diagnosticGameResponse},
	} {
		for _, explicitProfile := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/profile=%t", tc.name, explicitProfile), func(t *testing.T) {
				calls := 0
				s := New("http://test", "diagnostic-token")
				s.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer diagnostic-token" || r.Body != nil {
						t.Fatalf("diagnosis must issue authenticated GET only: %+v", r)
					}
					if calls == 1 {
						if r.URL.RequestURI() != "/api/client/info" {
							t.Fatalf("first request %s", r.URL.RequestURI())
						}
						return jsonResponse(diagnosticCapabilities), nil
					}
					want := tc.path
					if explicitProfile {
						want += "?profileId=2"
					}
					if calls != 2 || r.URL.RequestURI() != want {
						t.Fatalf("unexpected request %d: %s, want %s", calls, r.URL.RequestURI(), want)
					}
					return jsonResponse(tc.body), nil
				})
				args := map[string]any{tc.idKey: 7}
				if explicitProfile {
					args["profileId"] = 2
				}
				data, failed := diagnosticResult(t, s.Handle(context.Background(), toolCall(t, tc.name, args)))
				if failed || calls != 2 || data[tc.idKey] != float64(7) || data["profileId"] != float64(2) || data["source"] != "stored" || data["filesystemVerified"] != false {
					t.Fatalf("invalid stored response: %v, failed=%t, requests=%d", data, failed, calls)
				}
			})
		}
	}
}

func TestDiagnosticInvalidArgumentsNeverRequest(t *testing.T) {
	for _, tc := range []struct{ name, idKey string }{{"foliospace.diagnose_book", "bookId"}, {"foliospace.diagnose_game", "gameId"}} {
		cases := []map[string]any{{}, {tc.idKey: 7, "unknown": true}, {tc.idKey: 7, "profileId": nil}}
		for _, invalid := range []any{nil, 0, -1, 1.5, "7", true, 9007199254740992.0} {
			cases = append(cases, map[string]any{tc.idKey: invalid}, map[string]any{tc.idKey: 7, "profileId": invalid})
		}
		for i, args := range cases {
			t.Run(fmt.Sprintf("%s/%d", tc.name, i), func(t *testing.T) {
				s := New("http://test", "")
				s.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
					t.Fatal("invalid arguments reached the network")
					return nil, nil
				})
				data, failed := diagnosticResult(t, s.Handle(context.Background(), toolCall(t, tc.name, args)))
				if !failed || data["code"] != "invalid-arguments" {
					t.Fatalf("unexpected invalid-argument result: %v, failed=%t", data, failed)
				}
			})
		}
	}
}

func TestDiagnosticToolsReadOnlySchema(t *testing.T) {
	r := New("http://test", "").Handle(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	if r.Error != nil {
		t.Fatal(r.Error)
	}
	var listing struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal([]byte(mustJSON(t, r.Result)), &listing); err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, tool := range listing.Tools {
		if tool.Name != "foliospace.diagnose_book" && tool.Name != "foliospace.diagnose_game" {
			continue
		}
		if found[tool.Name] {
			t.Fatalf("duplicate tool %s", tool.Name)
		}
		found[tool.Name] = true
		if tool.Annotations["readOnlyHint"] != true || tool.Annotations["destructiveHint"] != false || tool.Annotations["idempotentHint"] != true || tool.Annotations["openWorldHint"] != false {
			t.Fatalf("unsafe annotations for %s: %v", tool.Name, tool.Annotations)
		}
		if tool.InputSchema["additionalProperties"] != false {
			t.Fatalf("unknown properties allowed for %s", tool.Name)
		}
		required, ok := tool.InputSchema["required"].([]any)
		if !ok || len(required) != 1 {
			t.Fatalf("missing required asset identifier for %s", tool.Name)
		}
		wantID := "bookId"
		if tool.Name == "foliospace.diagnose_game" {
			wantID = "gameId"
		}
		if required[0] != wantID {
			t.Fatalf("unexpected required identifier for %s: %v", tool.Name, required)
		}
	}
	if len(found) != 2 {
		t.Fatalf("diagnostic tools missing: %v", found)
	}
}

func TestDiagnosticCapabilityRequiredWithoutFallback(t *testing.T) {
	for _, body := range []string{`{}`, `{"capabilities":{}}`, `{"capabilities":{"storedDiagnosticsV1":false}}`, `{"capabilities":{"storedDiagnosticsV1":"true"}}`, `[]`, `null`} {
		s := New("http://test", "")
		calls := 0
		s.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			if calls != 1 || r.URL.Path != "/api/client/info" {
				t.Fatal("unsupported diagnostics fell back to another endpoint")
			}
			return jsonResponse(body), nil
		})
		data, failed := diagnosticResult(t, s.Handle(context.Background(), toolCall(t, "foliospace.diagnose_book", map[string]any{"bookId": 7})))
		if !failed || calls != 1 || data["code"] == nil {
			t.Fatalf("unsupported capability accepted: body=%s data=%v", body, data)
		}
	}
}

func TestDiagnosticHTTPFailuresDoNotLeakOrRetry(t *testing.T) {
	for _, stage := range []string{"info", "diagnostic"} {
		for _, status := range []int{401, 403, 404, 500} {
			t.Run(fmt.Sprintf("%s/%d", stage, status), func(t *testing.T) {
				calls := 0
				s := New("http://test", "")
				s.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					if stage == "diagnostic" && calls == 1 {
						return jsonResponse(diagnosticCapabilities), nil
					}
					if stage == "diagnostic" && r.URL.Query().Get("profileId") != "999" {
						t.Fatal("explicit unknown profile silently fell back")
					}
					res := jsonResponse(`{"code":"not-found","state":{"summary":"PRIVATE-DIAGNOSTIC-NOTE","filePath":"/PRIVATE-DIAGNOSTIC-MOUNT/example"},"message":"PRIVATE-DIAGNOSTIC-MESSAGE","error":"PRIVATE-DIAGNOSTIC-ERROR"}`)
					res.StatusCode = status
					return res, nil
				})
				r := s.Handle(context.Background(), toolCall(t, "foliospace.diagnose_game", map[string]any{"gameId": 7, "profileId": 999}))
				data, failed := diagnosticResult(t, r)
				wantCalls := 1
				if stage == "diagnostic" {
					wantCalls = 2
				}
				if !failed || calls != wantCalls || data["status"] != float64(status) || strings.Contains(mustJSON(t, r), "PRIVATE-DIAGNOSTIC") {
					t.Fatalf("unsafe failure result: %+v calls=%d", r, calls)
				}
				if _, ok := data["state"]; ok {
					t.Fatalf("upstream state leaked: %v", data)
				}
			})
		}
	}
}

func TestDiagnosticSuccessAllowsOnlyPublicFields(t *testing.T) {
	for _, tc := range []struct{ name, idKey, body string }{
		{"foliospace.diagnose_book", "bookId", diagnosticBookResponse},
		{"foliospace.diagnose_game", "gameId", diagnosticGameResponse},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body map[string]any
			if err := json.Unmarshal([]byte(tc.body), &body); err != nil {
				t.Fatal(err)
			}
			body["summary"] = "PRIVATE-DIAGNOSTIC-NOTE"
			body["title"] = "/PRIVATE-DIAGNOSTIC-MOUNT/example"
			body["privateState"] = map[string]any{"favorite": true}
			for _, key := range []string{"health", "index", "files", "launch"} {
				if object, ok := body[key].(map[string]any); ok {
					object["summary"] = "PRIVATE-DIAGNOSTIC-NESTED"
				}
			}
			calls := 0
			s := New("http://test", "")
			s.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return jsonResponse(diagnosticCapabilities), nil
				}
				return jsonResponse(mustJSON(t, body)), nil
			})
			r := s.Handle(context.Background(), toolCall(t, tc.name, map[string]any{tc.idKey: 7}))
			data, failed := diagnosticResult(t, r)
			if failed || data[tc.idKey] != float64(7) || strings.Contains(mustJSON(t, r), "PRIVATE-DIAGNOSTIC") || strings.Contains(mustJSON(t, r), "privateState") {
				t.Fatalf("unsafe successful response: %+v", r)
			}
		})
	}
}

func TestDiagnosticResponseBoundsAndMalformedBodies(t *testing.T) {
	for _, tc := range []struct{ name, body, code string }{
		{"oversized", strings.Repeat(" ", (2<<20)+1), "response-too-large"},
		{"broken-json", `{"bookId":`, "invalid-response"},
		{"array", `[]`, "invalid-response"},
		{"null", `null`, "invalid-response"},
	} {
		for _, stage := range []string{"info", "diagnostic"} {
			t.Run(tc.name+"/"+stage, func(t *testing.T) {
				calls := 0
				s := New("http://test", "")
				s.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
					calls++
					if calls == 1 && stage == "diagnostic" {
						return jsonResponse(diagnosticCapabilities), nil
					}
					return jsonResponse(tc.body), nil
				})
				data, failed := diagnosticResult(t, s.Handle(context.Background(), toolCall(t, "foliospace.diagnose_book", map[string]any{"bookId": 7})))
				if !failed || data["code"] != tc.code {
					t.Fatalf("bad upstream accepted: %v failed=%t", data, failed)
				}
			})
		}
	}
}

func TestDiagnosticAllowedStringFieldsDoNotLeakUpstreamText(t *testing.T) {
	for _, tc := range []struct {
		name, idKey, body string
		fields            []string
	}{
		{"foliospace.diagnose_book", "bookId", diagnosticBookResponse, []string{"source", "health.status", "health.code", "index.status", "issues"}},
		{"foliospace.diagnose_game", "gameId", diagnosticGameResponse, []string{"source", "catalogRole", "launch.status", "metadataStatus", "artworkStatus", "issues"}},
	} {
		for _, field := range tc.fields {
			t.Run(tc.name+"/"+field, func(t *testing.T) {
				var body map[string]any
				if err := json.Unmarshal([]byte(tc.body), &body); err != nil {
					t.Fatal(err)
				}
				const privateValue = "PRIVATE-DIAGNOSTIC-TEXT:/unmounted/private/library"
				parts := strings.Split(field, ".")
				if len(parts) == 2 {
					body[parts[0]].(map[string]any)[parts[1]] = privateValue
				} else if field == "issues" {
					body[field] = []string{privateValue}
				} else {
					body[field] = privateValue
				}
				calls := 0
				s := New("http://test", "")
				s.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
					calls++
					if calls == 1 {
						return jsonResponse(diagnosticCapabilities), nil
					}
					return jsonResponse(mustJSON(t, body)), nil
				})
				r := s.Handle(context.Background(), toolCall(t, tc.name, map[string]any{tc.idKey: 7}))
				diagnosticResult(t, r)
				if strings.Contains(mustJSON(t, r), "PRIVATE-DIAGNOSTIC-TEXT") {
					t.Fatalf("unvalidated diagnostic field %s exposed upstream text: %+v", field, r)
				}
			})
		}
	}
}

func TestDiagnosticResponseContextMustMatchRequest(t *testing.T) {
	for _, tc := range []struct{ name, idKey, body string }{
		{"foliospace.diagnose_book", "bookId", diagnosticBookResponse},
		{"foliospace.diagnose_game", "gameId", diagnosticGameResponse},
	} {
		for _, mutation := range []string{"wrong-asset", "wrong-profile", "missing-profile", "verified-filesystem"} {
			t.Run(tc.name+"/"+mutation, func(t *testing.T) {
				var body map[string]any
				if err := json.Unmarshal([]byte(tc.body), &body); err != nil {
					t.Fatal(err)
				}
				switch mutation {
				case "wrong-asset":
					body[tc.idKey] = 8
				case "wrong-profile":
					body["profileId"] = 1
				case "missing-profile":
					delete(body, "profileId")
				case "verified-filesystem":
					body["filesystemVerified"] = true
				}
				calls := 0
				s := New("http://test", "")
				s.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
					calls++
					if calls == 1 {
						return jsonResponse(diagnosticCapabilities), nil
					}
					return jsonResponse(mustJSON(t, body)), nil
				})
				data, failed := diagnosticResult(t, s.Handle(context.Background(), toolCall(t, tc.name, map[string]any{tc.idKey: 7, "profileId": 2})))
				if !failed || data["code"] != "invalid-response" || calls != 2 {
					t.Fatalf("response context mismatch accepted: %v, calls=%d", data, calls)
				}
			})
		}
	}
}

func TestDiagnosticRedirectDoesNotForwardCredentials(t *testing.T) {
	for _, stage := range []string{"info", "diagnostic"} {
		t.Run(stage, func(t *testing.T) {
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Error("diagnostic redirect was followed")
			}))
			defer target.Close()
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer diagnostic-token" {
					t.Error("missing API token at intended server")
				}
				if stage == "diagnostic" && r.URL.Path == "/api/client/info" {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, diagnosticCapabilities)
					return
				}
				http.Redirect(w, r, target.URL, http.StatusFound)
			}))
			defer source.Close()
			data, failed := diagnosticResult(t, New(source.URL, "diagnostic-token").Handle(context.Background(), toolCall(t, "foliospace.diagnose_book", map[string]any{"bookId": 7})))
			if !failed || data["code"] != "http-302" {
				t.Fatalf("redirect did not fail closed: %v", data)
			}
		})
	}
}
