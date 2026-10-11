package mcp

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVideoToolsRoutesAndBounds(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		path string
	}{
		{"list_video_works", map[string]any{"limit": 500}, "/api/client/video-works?limit=100&offset=0"},
		{"get_video_work", map[string]any{"workId": 7}, "/api/client/video-works/7"},
		{"list_video_seasons", map[string]any{"workId": 7}, "/api/client/video-works/7/seasons?limit=20&offset=0"},
		{"list_video_episodes", map[string]any{"workId": 7, "season": 0}, "/api/client/video-works/7/episodes?limit=20&offset=0&season=0"},
		{"list_video_versions", map[string]any{"workId": 7, "episodeId": 3}, "/api/client/video-works/7/files?episodeId=3&limit=20&offset=0"},
		{"get_video_availability", map[string]any{"workId": 7, "season": 1}, "/api/client/video-works/7/availability?limit=20&offset=0&season=1"},
		{"get_video_playback_state", map[string]any{"videoId": 8, "profileId": 2}, "/api/client/videos/8/playback-state?profileId=2"},
		{"list_video_continue", map[string]any{"profileId": 2}, "/api/client/video-playback?kind=continue&limit=20&offset=0&profileId=2"},
		{"list_video_next_up", map[string]any{}, "/api/client/video-playback?kind=next-up&limit=20&offset=0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New("http://test", "secret")
			calls := 0
			s.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer secret" {
					t.Fatal("bad request")
				}
				if calls == 1 {
					return jsonResponse(`{"capabilities":{"videoLibraryV1":true,"videoSeasonsV1":true,"videoPlaybackStateV1":true,"videoNextUpV1":true,"videoEpisodeAvailabilityV1":true}}`), nil
				}
				if r.URL.RequestURI() != tc.path {
					t.Errorf("got %s want %s", r.URL.RequestURI(), tc.path)
				}
				return jsonResponse(`{"items":[],"total":0}`), nil
			})
			r := s.Handle(context.Background(), toolCall(t, "foliospace."+tc.name, tc.args))
			if r.Error != nil || strings.Contains(mustJSON(t, r.Result), `"isError":true`) || calls != 2 {
				t.Fatalf("%+v calls=%d", r, calls)
			}
		})
	}
}

func TestVideoInvalidArgumentsNeverRequest(t *testing.T) {
	s := New("http://test", "")
	s.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("missing season reached network")
		return nil, nil
	})
	r := s.Handle(context.Background(), toolCall(t, "foliospace.get_video_availability", map[string]any{"workId": 1}))
	if !strings.Contains(mustJSON(t, r.Result), "invalid-arguments") {
		t.Fatal(r)
	}
	for _, a := range []map[string]any{{"profileId": 0}, {"profileId": "2"}, {"profileId": -1}, {"profileId": 1.5}, {"limit": 0}, {"offset": -1}, {"unexpected": true}} {
		s := New("http://test", "")
		s.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("invalid argument reached network")
			return nil, nil
		})
		r := s.Handle(context.Background(), toolCall(t, "foliospace.list_video_continue", a))
		if !strings.Contains(mustJSON(t, r.Result), "invalid-arguments") {
			t.Fatal(r)
		}
	}
}

func TestVideoLegacyFallbackAndAuth(t *testing.T) {
	for _, status := range []int{200, 401, 403, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			s := New("http://test", "secret")
			calls := 0
			s.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				res := jsonResponse(`{"capabilities":{}}`)
				if calls == 1 {
					res.StatusCode = status
				} else if r.URL.Path != "/api/client/videos" {
					t.Fatal(r.URL.Path)
				}
				return res, nil
			})
			r := s.Handle(context.Background(), toolCall(t, "foliospace.list_video_works", nil))
			b := mustJSON(t, r.Result)
			if status == 200 {
				if calls != 2 || !strings.Contains(b, "legacy-files") {
					t.Fatal(b)
				}
			} else if calls != 1 || !strings.Contains(b, `"isError":true`) {
				t.Fatal(b)
			}
		})
	}
}

func TestVideoErrorsPrivacyAndSize(t *testing.T) {
	for _, status := range []int{200, 409, 404} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			s := New("http://test", "")
			calls := 0
			s.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return jsonResponse(`{"capabilities":{"videoPlaybackStateV1":true}}`), nil
				}
				r := jsonResponse(`{"code":"revision-conflict","state":{"revision":4,"filePath":"/private/example"},"error":"secret"}`)
				r.StatusCode = status
				return r, nil
			})
			r := s.Handle(context.Background(), toolCall(t, "foliospace.get_video_playback_state", map[string]any{"videoId": 1}))
			b := mustJSON(t, r.Result)
			if strings.Contains(b, "/private/") || strings.Contains(b, "secret") {
				t.Fatal(b)
			}
			if status >= 400 && (!strings.Contains(b, "revision-conflict") || !strings.Contains(b, "revision")) {
				t.Fatal(b)
			}
		})
	}
	s := New("http://test", "")
	s.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return jsonResponse(strings.Repeat(" ", 3<<20)), nil })
	r := s.Handle(context.Background(), toolCall(t, "foliospace.list_video_works", nil))
	if !strings.Contains(mustJSON(t, r.Result), "response-too-large") {
		t.Fatal(r)
	}
}

func TestVideoToolsBothTransports(t *testing.T) {
	for _, framed := range []bool{false, true} {
		body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
		input := body + "\n"
		if framed {
			input = fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
		}
		var out bytes.Buffer
		if err := New("http://test", "").Serve(context.Background(), strings.NewReader(input), &out); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"foliospace.list_video_works", "foliospace.list_video_next_up", "foliospace.list_games", "readOnlyHint"} {
			if !strings.Contains(out.String(), name) {
				t.Fatal(name)
			}
		}
	}
}

func TestVideoRedirectDoesNotForwardCredential(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("redirect followed") }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer source.Close()
	s := New(source.URL, "secret")
	r := s.Handle(context.Background(), toolCall(t, "foliospace.list_video_works", nil))
	if !strings.Contains(mustJSON(t, r.Result), "http-302") {
		t.Fatal(r)
	}
}

func TestVideoNoFallbackForUnsupportedFiltersOrProfile(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   map[string]any
		caps   string
		status int
		want   string
	}{
		{"list_video_works", map[string]any{"kind": "movie"}, `{}`, 200, "capability-unsupported"},
		{"list_video_works", map[string]any{"libraryId": 1}, `{}`, 200, "capability-unsupported"},
		{"list_video_seasons", map[string]any{"workId": 1}, `{}`, 200, "capability-unsupported"},
		{"list_video_continue", map[string]any{"profileId": 999}, `{"videoPlaybackStateV1":true}`, 404, "not-found"},
	} {
		s := New("http://test", "")
		calls := 0
		s.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return jsonResponse(`{"capabilities":` + tc.caps + "}"), nil
			}
			if calls > 2 || r.URL.Query().Get("profileId") != "999" {
				t.Fatal("profile fallback")
			}
			result := jsonResponse(`{"code":"not-found"}`)
			result.StatusCode = tc.status
			return result, nil
		})
		r := s.Handle(context.Background(), toolCall(t, "foliospace."+tc.name, tc.args))
		if !strings.Contains(mustJSON(t, r.Result), tc.want) {
			t.Fatal(r)
		}
	}
}
