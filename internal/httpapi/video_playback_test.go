package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/service"
	"foliospace-reader/internal/store"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestVideoP1P2HTTPAuthStateAndLegacy(t *testing.T) {
	t.Setenv("FOLIOSPACE_VIDEO_METADATA_PROVIDER", "")
	c, e := db.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	st := store.New(c)
	lib, _ := st.CreateLibraryWithType("V", t.TempDir(), "video")
	v, _ := st.UpsertVideo(domain.VideoAsset{LibraryID: lib.ID, FilePath: filepath.Join(lib.RootPath, "a.mp4"), Title: "Show", Format: "mp4", MTime: time.Now()})
	sn, en := 1, 1
	st.SaveVideoLocalMetadata(v.ID, "x", domain.VideoLocalMetadata{Title: "Episode", ShowTitle: "Show", Kind: "episode", Season: &sn, Episode: &en})
	m, _ := st.VideoMetadata(v.ID)
	h := NewWithOptions(service.NewWithConfig(st, t.TempDir()), nil, Options{APIToken: "test-only"}).Routes()
	call := func(method, path, profile string, body any, auth bool, want int) *httptest.ResponseRecorder {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(raw))
		if auth {
			r.Header.Set("Authorization", "Bearer test-only")
		}
		if profile != "" {
			r.Header.Set("X-FolioSpace-Profile-Id", profile)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body)
		}
		return w
	}
	statePath := fmt.Sprintf("/api/client/videos/%d/playback-state", v.ID)
	eventPath := fmt.Sprintf("/api/client/videos/%d/playback-events", v.ID)
	for _, path := range []string{statePath, eventPath, "/api/client/video-playback", fmt.Sprintf("/api/client/video-works/%d/seasons", m.WorkID)} {
		call("GET", path, "", nil, false, 401)
	}
	w := call("GET", statePath, "", nil, true, 200)
	var initial domain.VideoPlaybackState
	if e = json.Unmarshal(w.Body.Bytes(), &initial); e != nil {
		t.Fatal(e)
	}
	in := domain.VideoPlaybackEvent{EventID: "one", SessionID: "test", ClientSequence: 1, MediaKey: initial.MediaKey, EventKind: "progress", PositionSeconds: 20, DurationSeconds: 100}
	call("POST", eventPath, "", in, true, 200)
	call("POST", eventPath, "", in, true, 200)
	in.EventID = "two"
	in.ClientSequence = 2
	w = call("POST", eventPath, "", in, true, 409)
	var conflict map[string]json.RawMessage
	json.Unmarshal(w.Body.Bytes(), &conflict)
	if string(conflict["code"]) != `"playback-revision-conflict"` || len(conflict["state"]) == 0 {
		t.Fatal(w.Body)
	}
	call("GET", statePath, "invalid", nil, true, 400)
	call("GET", statePath, "999999", nil, true, 404)
	call("POST", eventPath, "", map[string]any{"profileId": 99}, true, 400)
	for _, tail := range []string{"seasons", "episodes?season=1", "files?season=1", "availability?season=1"} {
		call("GET", fmt.Sprintf("/api/client/video-works/%d/%s", m.WorkID, tail), "", nil, true, 200)
	}
	call("GET", "/api/client/video-playback?kind=continue", "", nil, true, 200)
	call("GET", "/api/client/video-playback?kind=next-up", "", nil, true, 200)
	call("GET", "/api/client/video-works?sort=recent&libraryId=9999", "", nil, true, 200)
	call("GET", "/api/client/video-works?sort=madeup", "", nil, true, 400)
	call("GET", fmt.Sprintf("/api/client/videos/%d/manifest", v.ID), "", nil, true, 200)
	call("POST", fmt.Sprintf("/api/videos/works/%d/availability/refresh", m.WorkID), "", map[string]int{"season": 1}, true, 503)
	w = call("GET", "/api/client/info", "", nil, true, 200)
	var info clientInfoResponse
	json.Unmarshal(w.Body.Bytes(), &info)
	if !info.Capabilities.VideoSeasonsV1 || !info.Capabilities.VideoPlaybackStateV1 || !info.Capabilities.VideoLibraryV1 {
		t.Fatal(info)
	}
}
