package httpapi

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

// These diagnostic categories deliberately exclude IDs, URLs, query strings,
// headers, user agents and content. Streams/downloads are not wrapped.
func requestTimingCategory(r *http.Request) string {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return ""
	}
	switch r.URL.Path {
	case "/api/client/home":
		return "home"
	case "/api/collections", "/api/series":
		return "collections"
	case "/api/client/video-works":
		return "video-works"
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) == 5 && parts[0] == "api" && parts[1] == "videos" && parts[3] == "artwork" && parts[4] == "poster" {
		return "poster"
	}
	if len(parts) == 6 && parts[0] == "api" && parts[1] == "books" && parts[3] == "epub" && parts[4] == "spine" {
		return "epub-spine"
	}
	return ""
}

type requestTimingSample struct {
	count    int
	lastSlow time.Time
}

func boundedRequestTimings(next http.Handler, printf func(string, ...any)) http.Handler {
	var mu sync.Mutex
	samples := make(map[string]requestTimingSample)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		category := requestTimingCategory(r)
		if category == "" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		recorder := &timingResponseWriter{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		elapsed := time.Since(start)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		// First 16 completions per category per server, then at most one slow,
		// failed or cancelled completion per minute per category. Not an access log.
		mu.Lock()
		sample := samples[category]
		emit := sample.count < 16
		if !emit && (elapsed >= time.Second || status >= 500 || r.Context().Err() != nil) && time.Since(sample.lastSlow) >= time.Minute {
			emit = true
			sample.lastSlow = time.Now()
		}
		if sample.count < 16 {
			sample.count++
		}
		samples[category] = sample
		mu.Unlock()
		if emit {
			printf("request_timing category=%s method=%s status=%d duration_ms=%.2f bytes=%d cancelled=%t", category, r.Method, status, float64(elapsed)/float64(time.Millisecond), recorder.bytes, r.Context().Err() != nil)
		}
	})
}

type timingResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *timingResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *timingResponseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	if status >= 200 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *timingResponseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytes += int64(n)
	return n, err
}
