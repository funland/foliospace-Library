package httpapi

import (
	"fmt"
	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/service"
	"foliospace-reader/internal/store"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVideoLibraryHTTPAndLegacyRange(t *testing.T) {
	t.Setenv("FOLIOSPACE_VIDEO_METADATA_PROVIDER", "")
	c, e := db.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	st := store.New(c)
	root := t.TempDir()
	l, _ := st.CreateLibraryWithType("Movies", root, "video")
	path := filepath.Join(root, "Example.mp4")
	os.WriteFile(path, []byte("0123456789"), 0600)
	v, e := st.UpsertVideo(domain.VideoAsset{LibraryID: l.ID, Title: "Example", FilePath: path, Format: "mp4", Size: 10, MTime: time.Now()})
	if e != nil {
		t.Fatal(e)
	}
	if e = st.SaveVideoLocalMetadata(v.ID, "revision1", domain.VideoLocalMetadata{Title: "Example", Kind: "movie", Year: 2001}); e != nil {
		t.Fatal(e)
	}
	m, _ := st.VideoMetadata(v.ID)
	ts := httptest.NewServer(NewWithOptions(service.NewWithConfig(st, t.TempDir()), nil, Options{APIToken: "secret"}).Routes())
	defer ts.Close()
	cases := []struct {
		method, path, body, rangeHeader string
		want                            int
	}{
		{"GET", "/api/client/video-works?limit=1", "", "", 200},
		{"GET", fmt.Sprintf("/api/client/video-works/%d/files", m.WorkID), "", "", 200},
		{"GET", fmt.Sprintf("/api/client/videos/%d/manifest", v.ID), "", "", 200},
		{"GET", fmt.Sprintf("/api/client/videos/%d/file", v.ID), "", "bytes=2-5", 206},
		{"HEAD", fmt.Sprintf("/api/client/videos/%d/file", v.ID), "", "", 200},
		{"GET", fmt.Sprintf("/api/client/videos/%d/file", v.ID), "", "bytes=20-30", 416},
		{"GET", fmt.Sprintf("/api/videos/%d/artwork/poster", v.ID), "", "", 200},
		{"POST", fmt.Sprintf("/api/videos/%d/metadata/refresh", v.ID), `{"query":"Movie","kind":"movie"}`, "", 503},
		{"POST", fmt.Sprintf("/api/videos/%d/metadata/unlink", v.ID), `{"generation":999}`, "", 409},
		{"POST", fmt.Sprintf("/api/videos/%d/metadata/match", v.ID), fmt.Sprintf(`{"generation":%d,"title":"Confirmed HTTP movie","kind":"movie","year":2001,"version":"Original"}`, m.Generation), "", 200},
		{"GET", "/api/videos/metadata/provider", "", "", 200},
	}
	for _, tc := range cases {
		req, _ := http.NewRequest(tc.method, ts.URL+tc.path, strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer secret")
		if tc.rangeHeader != "" {
			req.Header.Set("Range", tc.rangeHeader)
		}
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, resp.StatusCode, b)
		}
		if strings.Contains(string(b), root) {
			t.Fatal("private path leak")
		}
		if tc.want == 206 && (string(b) != "2345" || resp.Header.Get("Content-Range") != "bytes 2-5/10" || resp.ContentLength != 4) {
			t.Fatal("Range regression")
		}
		if tc.method == "HEAD" && (len(b) != 0 || resp.ContentLength != 10) {
			t.Fatal("HEAD body/length regression")
		}
	}
	for _, path := range []string{"/api/client/video-works", fmt.Sprintf("/api/videos/%d/artwork/poster", v.ID), "/api/videos/metadata/provider"} {
		resp, e := http.Get(ts.URL + path)
		if e != nil {
			t.Fatal(e)
		}
		resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Fatal("unauthenticated access", path)
		}
	}
}
