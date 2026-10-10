package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTMDBSeasonContractAndLimits(t *testing.T) {
	for _, body := range []string{`{"season_number":1,"episodes":[{"season_number":1,"episode_number":2,"name":"Next","air_date":null}]}`, `{"season_number":2,"episodes":[]}`, `{"season_number":1}`, strings.Repeat("x", (2<<20)+1)} {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/tv/123/season/1" || r.Header.Get("Authorization") != "Bearer test" {
				t.Error("bad contract")
			}
			w.Write([]byte(body))
		}))
		p := &tmdbProvider{base: ts.URL, token: "test", language: "en-US", client: ts.Client()}
		items, e := p.Season(context.Background(), "123", 1)
		if strings.Contains(body, `"Next"`) {
			if e != nil || len(items) != 1 || items[0].AirDate != "" {
				t.Fatal(items, e)
			}
		} else if e == nil {
			t.Fatal("invalid response accepted")
		}
		ts.Close()
	}
}
