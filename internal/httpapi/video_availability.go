package httpapi

import (
	"fmt"
	"net/http"
)

func (s *Server) handleVideoAvailability(w http.ResponseWriter, r *http.Request, id int64, limit, offset int) {
	season, err := videoSeasonQuery(r)
	if err != nil || season == nil {
		videoError(w, fmt.Errorf("season required"))
		return
	}
	out, err := s.service.VideoAvailability(id, *season, limit, offset)
	if err != nil {
		videoError(w, err)
		return
	}
	writeJSON(w, out)
}
func (s *Server) handleVideoAvailabilityRefresh(w http.ResponseWriter, r *http.Request) {
	id, tail, ok := parseIDTail(r.URL.Path, "/api/videos/works/")
	if !ok || tail != "availability/refresh" {
		http.NotFound(w, r)
		return
	}
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	var in struct {
		Season *int `json:"season"`
	}
	if !videoJSON(w, r, &in) {
		return
	}
	if in.Season == nil || *in.Season < 0 || *in.Season > 999 {
		videoError(w, fmt.Errorf("invalid season"))
		return
	}
	if err := s.service.RefreshVideoAvailability(r.Context(), id, *in.Season); err != nil {
		videoError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
