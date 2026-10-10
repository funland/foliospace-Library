package httpapi

import (
	"encoding/json"
	"fmt"
	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/service"
	"foliospace-reader/internal/store"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestVideoMetadataHTTPProtection(t *testing.T) {
	c, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	st := store.New(c)
	lib, err := st.CreateLibraryWithType("Movies", t.TempDir(), "video")
	if err != nil {
		t.Fatal(err)
	}
	v, err := st.UpsertVideo(domain.VideoAsset{LibraryID: lib.ID, FilePath: lib.RootPath + "/private.mkv", Title: "Original", Format: "mkv", MTime: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(NewWithOptions(service.New(st), nil, Options{APIToken: "secret"}).Routes())
	defer ts.Close()
	path := fmt.Sprintf("/api/videos/%d/metadata", v.ID)
	for _, tc := range []struct {
		method, path, body, token string
		status                    int
	}{
		{"GET", path, "", "", 401},
		{"PUT", path, `{"field":"title","value":"Manual"}`, "", 401},
		{"PUT", path, `{"field":"title","value":"Manual"}`, "secret", 200},
		{"PUT", path, `{"field":"filePath","value":"other"}`, "secret", 400},
		{"PUT", path, `{"field":"year","value":null}`, "secret", 400},
		{"PUT", path, `{"field":"title","value":"bad"} {}`, "secret", 400},
		{"GET", fmt.Sprintf("/api/client/videos/%d/metadata", v.ID), "", "secret", 200},
	} {
		req, _ := http.NewRequest(tc.method, ts.URL+tc.path, strings.NewReader(tc.body))
		if tc.token != "" {
			req.Header.Set("Authorization", "Bearer "+tc.token)
		}
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != tc.status {
			t.Fatalf("%s: %d %s", tc.path, resp.StatusCode, b)
		}
		if strings.Contains(string(b), lib.RootPath) {
			t.Fatal("host path leaked")
		}
		if tc.method == "GET" && tc.status == 200 {
			var m domain.VideoMetadata
			if e = json.Unmarshal(b, &m); e != nil || m.Title != "Manual" {
				t.Fatalf("manual metadata lost %s", b)
			}
		}
	}
}
