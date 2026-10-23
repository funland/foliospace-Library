package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVideoDetailsBoundedConfirmedIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		wantError  bool
	}{
		{"movie", `{"id":42,"original_title":"Original","runtime":123,"genres":[{"name":"科幻"}],"vote_average":8.5,"vote_count":10,"credits":{"cast":[{"name":"Actor"},{"name":"Actor"}],"crew":[{"name":"Director","job":"Director"}]}}`, false},
		{"mismatch", `{"id":41}`, true},
		{"bad-json", `{`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/movie/42" || r.URL.Query().Get("append_to_response") != "credits" || r.URL.Query().Get("language") != "zh-CN" || r.Header.Get("Authorization") != "Bearer test" {
					t.Error("request identity")
				}
				fmt.Fprint(w, tc.body)
			}))
			defer ts.Close()
			p := &tmdbProvider{base: ts.URL, language: "zh-CN", token: "test", client: ts.Client()}
			d, err := p.Details(context.Background(), "42", "movie")
			if (err != nil) != tc.wantError {
				t.Fatalf("error %v", err)
			}
			if !tc.wantError && (d.OriginalTitle != "Original" || d.RuntimeMinutes != 123 || len(d.Cast) != 1 || len(d.Directors) != 1 || d.Rating != 8.5) {
				t.Fatalf("details %+v", d)
			}
			if _, err = p.Details(context.Background(), "42/../43", "movie"); err == nil || calls != 1 {
				t.Fatal("invalid ID requested")
			}
		})
	}
}
func TestVideoDetailsProviderBackoff(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Retry-After", "120"); w.WriteHeader(429) }))
	defer ts.Close()
	p := &tmdbProvider{base: ts.URL, client: ts.Client()}
	_, err := p.Details(context.Background(), "42", "series")
	e, ok := err.(*videoProviderError)
	if !ok || e.RetryAfter.Seconds() != 120 {
		t.Fatalf("backoff %v", err)
	}
}
