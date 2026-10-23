package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRequestTimingsPrivacyAndBound(t *testing.T) {
	var lines []string
	h := boundedRequestTimings(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("private-body")) }), func(f string, args ...any) { lines = append(lines, fmt.Sprintf(f, args...)) })
	for i := 0; i < 40; i++ {
		r := httptest.NewRequest("GET", "/api/videos/private-id/artwork/poster?token=private-token&title=private-title", nil)
		r.Header.Set("Authorization", "Bearer private-secret")
		h.ServeHTTP(httptest.NewRecorder(), r)
	}
	if len(lines) != 16 {
		t.Fatalf("samples: %d", len(lines))
	}
	for _, line := range lines {
		if strings.Contains(line, "private") || !strings.Contains(line, "category=poster method=GET status=200") {
			t.Fatal(line)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for i := 0; i < 10; i++ {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/videos/1/artwork/poster", nil).WithContext(ctx))
	}
	if len(lines) != 17 || !strings.Contains(lines[16], "cancelled=true") {
		t.Fatalf("cancel bound: %+v", lines)
	}
	for _, path := range []string{"/api/videos/1/stream", "/api/books/1/file", "/api/events", "/api/client/video-works/1"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
	}
	if len(lines) != 17 {
		t.Fatal("wrapped unrelated streams")
	}
}

func TestRequestTimingsPreserveHTTP(t *testing.T) {
	file := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"fixture"`)
		w.Header().Set("Cache-Control", "private, no-cache")
		http.ServeContent(w, r, "fixture.jpg", time.Unix(1, 0), strings.NewReader("0123456789"))
	})
	var lines []string
	h := boundedRequestTimings(file, func(f string, args ...any) { lines = append(lines, fmt.Sprintf(f, args...)) })
	for _, tc := range []struct {
		method, header, value string
		status                int
		body                  string
	}{
		{"GET", "", "", 200, "0123456789"}, {"HEAD", "", "", 200, ""},
		{"GET", "Range", "bytes=2-4", 206, "234"}, {"GET", "If-None-Match", `"fixture"`, 304, ""},
	} {
		r := httptest.NewRequest(tc.method, "/api/videos/1/artwork/poster", nil)
		if tc.header != "" {
			r.Header.Set(tc.header, tc.value)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status || w.Body.String() != tc.body || w.Header().Get("ETag") != `"fixture"` {
			t.Fatalf("%+v: %d %q", tc, w.Code, w.Body.String())
		}
		if !strings.Contains(lines[len(lines)-1], fmt.Sprintf("status=%d", tc.status)) {
			t.Fatal(lines)
		}
	}
	w := httptest.NewRecorder()
	if (&timingResponseWriter{ResponseWriter: w}).Unwrap() != w {
		t.Fatal("unwrap")
	}
}
