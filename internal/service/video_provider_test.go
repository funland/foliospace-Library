package service

import (
	"bytes"
	"context"
	"errors"
	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/store"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTMDBAdapterSanitizedErrorsAndCandidates(t *testing.T) {
	for _, code := range []int{200, 401, 404, 429, 500} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-only" || r.URL.Path != "/search/tv" || r.URL.Query().Get("query") != "Show" {
					t.Error("wrong request")
				}
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(code)
				if code == 200 {
					w.Write([]byte(`{"results":[{"id":4,"name":"Show","first_air_date":"2001-01-01","overview":"Plot","poster_path":"/abc.jpg"}]}`))
				} else {
					w.Write([]byte("secret response must not leak"))
				}
			}))
			defer ts.Close()
			p := &tmdbProvider{base: ts.URL, token: "test-only", language: "en-US", client: ts.Client()}
			out, e := p.Search(context.Background(), "Show", "series")
			if code == 200 {
				if e != nil || len(out) != 1 || out[0].Year != 2001 || out[0].Kind != "series" {
					t.Fatalf("%+v %v", out, e)
				}
			} else {
				var pe *videoProviderError
				if !errors.As(e, &pe) || strings.Contains(e.Error(), "secret") {
					t.Fatal(e)
				}
				if code == 429 && pe.RetryAfter != time.Minute {
					t.Fatal("Retry-After ignored")
				}
			}
		})
	}
}
func TestVideoArtworkLocalManualAndStale(t *testing.T) {
	t.Setenv("FOLIOSPACE_VIDEO_METADATA_PROVIDER", "")
	config := t.TempDir()
	c, e := db.Open(config)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	st := store.New(c)
	root := t.TempDir()
	lib, _ := st.CreateLibraryWithType("Videos", root, "video")
	v, _ := st.UpsertVideo(domain.VideoAsset{LibraryID: lib.ID, FilePath: filepath.Join(root, "Movie.mkv"), Title: "Movie", MTime: time.Now()})
	s := NewWithConfig(st, config)
	b, mime, _, e := s.VideoArtwork(v.ID, "poster")
	if e != nil || mime != "image/svg+xml" || len(b) == 0 {
		t.Fatal("missing fallback")
	}
	var pngBytes bytes.Buffer
	png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 3, 4)))
	os.WriteFile(filepath.Join(root, "Movie-poster.png"), pngBytes.Bytes(), 0600)
	b, mime, rev, e := s.VideoArtwork(v.ID, "poster")
	if e != nil || mime != "image/png" || !bytes.Equal(b, pngBytes.Bytes()) || rev == "" {
		t.Fatal("local artwork")
	}
	m, _ := st.VideoMetadata(v.ID)
	if e = s.UploadVideoArtwork(v.ID, "poster", pngBytes.Bytes(), m.Generation); e != nil {
		t.Fatal(e)
	}
	if e = s.UploadVideoArtwork(v.ID, "poster", pngBytes.Bytes(), m.Generation); !errors.Is(e, store.ErrVideoRevision) {
		t.Fatal("stale artwork write accepted")
	}
	if e = s.UploadVideoArtwork(v.ID, "poster", []byte("not image"), m.Generation); e == nil {
		t.Fatal("bad image accepted")
	}
	for _, p := range []string{"//evil/x.jpg", "/../../etc/passwd", "https://evil/x.jpg", "/x.svg", "/x.jpg?token=x"} {
		if tmdbImagePath.MatchString(p) {
			t.Fatal("unsafe image path", p)
		}
	}
	if s.VideoProviderStatus()["enabled"] != false {
		t.Fatal("provider enabled by default")
	}
}
