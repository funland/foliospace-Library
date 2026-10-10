package httpapi

import (
	"errors"
	"fmt"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/store"
	"net/http"
	"strconv"
	"strings"
)

func videoPositiveQuery(r *http.Request, key string) (int64, error) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return 0, nil
	}
	n, e := strconv.ParseInt(raw, 10, 64)
	if e != nil || n <= 0 {
		return 0, fmt.Errorf("invalid %s", key)
	}
	return n, nil
}
func videoSeasonQuery(r *http.Request) (*int, error) {
	raw := r.URL.Query().Get("season")
	if raw == "" {
		return nil, nil
	}
	n, e := strconv.Atoi(raw)
	if e != nil || n < 0 || n > 999 {
		return nil, fmt.Errorf("invalid season")
	}
	return &n, nil
}
func (s *Server) videoProfile(r *http.Request) (int64, error) {
	raw := strings.TrimSpace(r.Header.Get("X-FolioSpace-Profile-Id"))
	if raw == "" {
		raw = strings.TrimSpace(r.URL.Query().Get("profileId"))
	}
	if raw == "" {
		return s.service.VideoProfileID(0)
	}
	n, e := strconv.ParseInt(raw, 10, 64)
	if e != nil || n <= 0 {
		return 0, fmt.Errorf("invalid profile")
	}
	return s.service.VideoProfileID(n)
}
func playbackError(w http.ResponseWriter, err error) {
	var c *store.VideoPlaybackConflict
	if errors.As(err, &c) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(409)
		writeJSON(w, map[string]any{"code": c.Code, "state": c.State})
		return
	}
	videoError(w, err)
}
func (s *Server) handleVideoPlayback(w http.ResponseWriter, r *http.Request, id int64, tail string) bool {
	if tail != "playback-state" && tail != "playback-events" {
		return false
	}
	w.Header().Set("Cache-Control", "private, no-store")
	profile, err := s.videoProfile(r)
	if err != nil {
		videoError(w, err)
		return true
	}
	if tail == "playback-state" && r.Method == "GET" {
		state, e := s.service.VideoPlaybackState(id, profile)
		if e != nil {
			playbackError(w, e)
		} else {
			writeJSON(w, state)
		}
		return true
	}
	if tail == "playback-events" && r.Method == "POST" {
		var in domain.VideoPlaybackEvent
		if !videoJSON(w, r, &in) {
			return true
		}
		state, e := s.service.ApplyVideoPlayback(id, profile, in)
		if e != nil {
			playbackError(w, e)
		} else {
			writeJSON(w, state)
		}
		return true
	}
	w.WriteHeader(405)
	return true
}
func (s *Server) handleVideoPlaybackFeed(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	profile, err := s.videoProfile(r)
	if err != nil {
		videoError(w, err)
		return
	}
	library, err := videoPositiveQuery(r, "libraryId")
	if err != nil {
		videoError(w, err)
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		kind = "continue"
	}
	page, err := s.service.VideoPlaybackFeed(profile, library, kind, queryInt(r, "limit", 30, 100), queryInt(r, "offset", 0, 10000000))
	if err != nil {
		videoError(w, err)
		return
	}
	writeJSON(w, page)
}
