package httpapi

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/store"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func videoJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 70000)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		videoError(w, fmt.Errorf("invalid JSON"))
		return false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		videoError(w, fmt.Errorf("expected one JSON object"))
		return false
	}
	return true
}
func videoError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	code := "invalid-request"
	if errors.Is(err, sql.ErrNoRows) {
		status = 404
		code = "not-found"
	}
	if errors.Is(err, store.ErrVideoRevision) || err.Error() == "video-metadata-revision-conflict" {
		status = 409
		code = "metadata-revision-conflict"
	}
	if err.Error() == "provider-disabled" {
		status = 503
		code = "provider-disabled"
	}
	if err.Error() == "metadata-queue-full" {
		status = 429
		code = "metadata-queue-full"
		w.Header().Set("Retry-After", "30")
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "error": code})
}
func (s *Server) handleVideoWorks(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/details") {
		id, tail, ok := parseIDTail(r.URL.Path, "/api/client/video-works/")
		if !ok || tail != "details" {
			http.NotFound(w, r)
			return
		}
		var details domain.VideoWorkDetails
		var err error
		if r.Method == "GET" {
			details, err = s.service.VideoWorkDetails(id)
		} else if r.Method == "POST" {
			details, err = s.service.RefreshVideoWorkDetails(r.Context(), id)
		} else {
			w.Header().Set("Allow", "GET, POST")
			w.WriteHeader(405)
			return
		}
		if err != nil {
			videoError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
		writeJSON(w, details)
		return
	}
	if r.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if r.URL.Path == "/api/client/video-works" {
		library, err := videoPositiveQuery(r, "libraryId")
		if err != nil {
			videoError(w, err)
			return
		}
		sort := r.URL.Query().Get("sort")
		kind := r.URL.Query().Get("kind")
		if (sort != "" && sort != "title" && sort != "recent") || (kind != "" && kind != "movie" && kind != "series") {
			videoError(w, fmt.Errorf("invalid catalog filter"))
			return
		}
		p, err := s.service.VideoWorksFiltered(r.URL.Query().Get("q"), kind, library, sort, limit, offset)
		if err != nil {
			videoError(w, err)
			return
		}
		writeJSON(w, p)
		return
	}
	id, tail, ok := parseIDTail(r.URL.Path, "/api/client/video-works/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	if tail == "" {
		p, err := s.service.VideoWork(id)
		if err != nil {
			videoError(w, err)
			return
		}
		writeJSON(w, p)
		return
	}
	if tail == "seasons" {
		p, err := s.service.VideoSeasons(id, limit, offset)
		if err != nil {
			videoError(w, err)
			return
		}
		writeJSON(w, p)
		return
	}
	if tail == "episodes" {
		season, err := videoSeasonQuery(r)
		if err != nil {
			videoError(w, err)
			return
		}
		p, err := s.service.VideoEpisodes(id, season, limit, offset)
		if err != nil {
			videoError(w, err)
			return
		}
		writeJSON(w, p)
		return
	}
	if tail == "availability" {
		s.handleVideoAvailability(w, r, id, limit, offset)
		return
	}
	if tail == "files" {
		var season *int
		if raw := r.URL.Query().Get("season"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 0 {
				videoError(w, fmt.Errorf("invalid season"))
				return
			}
			season = &n
		}
		episodeID, err := videoPositiveQuery(r, "episodeId")
		if err != nil {
			videoError(w, err)
			return
		}
		p, err := s.service.VideoWorkEpisodeFiles(id, season, episodeID, limit, offset)
		if err != nil {
			videoError(w, err)
			return
		}
		writeJSON(w, p)
		return
	}
	http.NotFound(w, r)
}
func (s *Server) handleVideoProvider(w http.ResponseWriter, r *http.Request) {
	if r.Method == "DELETE" {
		if err := s.service.PurgeVideoProviderCache(true); err != nil {
			videoError(w, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
		return
	}
	if r.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	writeJSON(w, s.service.VideoProviderStatus())
}
func (s *Server) handleVideoMetadataJobs(w http.ResponseWriter, r *http.Request) {
	id, tail, ok := parseIDTail(r.URL.Path, "/api/video-metadata-jobs/")
	if !ok || tail != "" {
		http.NotFound(w, r)
		return
	}
	j, err := s.service.VideoMetadataJob(id)
	if err != nil {
		videoError(w, err)
		return
	}
	if r.Method == "DELETE" {
		if err = s.service.CancelVideoMetadataJob(id); err != nil {
			videoError(w, err)
			return
		}
		j, _ = s.service.VideoMetadataJob(id)
	} else if r.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	writeJSON(w, j)
}
func (s *Server) handleVideoLibraryAction(w http.ResponseWriter, r *http.Request, id int64, tail string) bool {
	switch tail {
	case "metadata/refresh":
		if r.Method != "POST" {
			w.WriteHeader(405)
			return true
		}
		var in struct {
			Query string `json:"query"`
			Kind  string `json:"kind"`
		}
		if !videoJSON(w, r, &in) {
			return true
		}
		if in.Kind != "movie" && in.Kind != "series" {
			videoError(w, fmt.Errorf("invalid kind"))
			return true
		}
		j, err := s.service.QueueVideoMetadata(id, in.Query, in.Kind)
		if err != nil {
			videoError(w, err)
			return true
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(202)
		_ = json.NewEncoder(w).Encode(j)
		return true
	case "metadata/candidates":
		if r.Method != "GET" {
			w.WriteHeader(405)
			return true
		}
		j, err := s.service.LatestVideoMetadataJob(id)
		if err != nil {
			videoError(w, err)
			return true
		}
		writeJSON(w, j)
		return true
	case "metadata/match", "metadata/select-match", "metadata/unlink":
		if r.Method != "POST" {
			w.WriteHeader(405)
			return true
		}
		var in struct {
			domain.VideoMatchInput
			JobID     int64 `json:"jobId"`
			Candidate int   `json:"candidate"`
		}
		if !videoJSON(w, r, &in) {
			return true
		}
		var err error
		switch tail {
		case "metadata/match":
			err = s.service.MatchVideo(id, in.VideoMatchInput)
		case "metadata/unlink":
			err = s.service.UnlinkVideo(id, in.Generation)
		default:
			err = s.service.SelectVideoCandidate(id, in.JobID, in.Candidate, in.VideoMatchInput)
		}
		if err != nil {
			videoError(w, err)
			return true
		}
		m, err := s.service.VideoMetadata(id)
		writeJSONOrError(w, m, err)
		return true
	case "artwork/select":
		if r.Method != "POST" {
			w.WriteHeader(405)
			return true
		}
		var in struct {
			JobID     int64  `json:"jobId"`
			Candidate int    `json:"candidate"`
			Role      string `json:"role"`
		}
		if !videoJSON(w, r, &in) {
			return true
		}
		j, err := s.service.QueueCandidateArtwork(id, in.JobID, in.Candidate, in.Role)
		if err != nil {
			videoError(w, err)
			return true
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(202)
		json.NewEncoder(w).Encode(j)
		return true
	}
	if !strings.HasPrefix(tail, "artwork/") {
		return false
	}
	role := strings.TrimPrefix(tail, "artwork/")
	if role != "poster" && role != "backdrop" {
		http.NotFound(w, r)
		return true
	}
	if r.Method == "GET" || r.Method == "HEAD" {
		b, mime, revision, err := s.service.VideoArtwork(id, role)
		if err != nil {
			videoError(w, err)
			return true
		}
		w.Header().Set("Content-Type", mime)
		w.Header().Set("ETag", `"`+revision+`"`)
		w.Header().Set("Cache-Control", "private, no-cache")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, "artwork", time.Time{}, bytes.NewReader(b))
		return true
	}
	if r.Method == "DELETE" {
		if err := s.service.ResetVideoArtwork(id, role); err != nil {
			videoError(w, err)
		} else {
			writeJSON(w, map[string]bool{"ok": true})
		}
		return true
	}
	if r.Method == "PUT" {
		g, err := strconv.ParseInt(r.URL.Query().Get("generation"), 10, 64)
		if err != nil {
			videoError(w, fmt.Errorf("generation required"))
			return true
		}
		b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 10<<20))
		if err == nil {
			err = s.service.UploadVideoArtwork(id, role, b, g)
		}
		if err != nil {
			videoError(w, err)
		} else {
			writeJSON(w, map[string]bool{"ok": true})
		}
		return true
	}
	w.WriteHeader(405)
	return true
}
