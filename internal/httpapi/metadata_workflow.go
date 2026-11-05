package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/store"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// This workflow changes shared library metadata. A profile is a receipt/state
// namespace, never an authorization boundary; require the configured admin token.
func (s *Server) handleVideoMetadataWorkflow(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if !s.authEnabled() || !s.validToken(bearerToken(r.Header.Get("Authorization"))) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="FolioSpace Library"`)
		metadataWorkflowHTTPError(w, http.StatusUnauthorized, "authentication-required")
		return
	}
	id, tail, ok := parseIDTail(r.URL.Path, "/api/client/metadata/videos/")
	if !ok || !metadataWorkflowID(id) {
		metadataWorkflowHTTPError(w, http.StatusNotFound, "not-found")
		return
	}
	if strings.HasPrefix(tail, "jobs/") {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		jobID, err := strconv.ParseInt(strings.TrimPrefix(tail, "jobs/"), 10, 64)
		query := r.URL.Query()
		profileID, profileErr := strconv.ParseInt(query.Get("profileId"), 10, 64)
		if err != nil || profileErr != nil || !metadataWorkflowID(jobID) || !metadataWorkflowID(profileID) || len(query) != 1 || len(query["profileId"]) != 1 {
			metadataWorkflowHTTPError(w, http.StatusBadRequest, "invalid-arguments")
			return
		}
		result, err := s.service.VideoMetadataWorkflowJob(profileID, id, jobID)
		metadataWorkflowHTTPResult(w, result, err)
		return
	}
	if tail != "search" && tail != "preview" && tail != "apply" {
		metadataWorkflowHTTPError(w, http.StatusNotFound, "not-found")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.URL.RawQuery != "" {
		metadataWorkflowHTTPError(w, http.StatusBadRequest, "invalid-arguments")
		return
	}
	switch tail {
	case "search":
		var in struct {
			ProfileID int64  `json:"profileId"`
			Query     string `json:"query"`
			Kind      string `json:"kind"`
		}
		if !metadataWorkflowJSON(w, r, &in) {
			return
		}
		if !metadataWorkflowID(in.ProfileID) || strings.TrimSpace(in.Query) == "" || len(in.Query) > 500 || (in.Kind != "movie" && in.Kind != "series") {
			metadataWorkflowHTTPError(w, http.StatusBadRequest, "invalid-arguments")
			return
		}
		result, err := s.service.SearchVideoMetadataWorkflow(in.ProfileID, id, in.Query, in.Kind)
		metadataWorkflowHTTPResult(w, result, err)
	case "preview":
		var in struct {
			ProfileID      int64  `json:"profileId"`
			JobID          int64  `json:"jobId"`
			CandidateIndex *int   `json:"candidateIndex"`
			Season         *int   `json:"season,omitempty"`
			Episode        *int   `json:"episode,omitempty"`
			Version        string `json:"version,omitempty"`
		}
		if !metadataWorkflowJSON(w, r, &in) {
			return
		}
		if !metadataWorkflowID(in.ProfileID) || !metadataWorkflowID(in.JobID) || in.CandidateIndex == nil || *in.CandidateIndex < 0 || *in.CandidateIndex > 999 || len(in.Version) > 500 || (in.Season == nil) != (in.Episode == nil) || (in.Season != nil && (*in.Season < 0 || *in.Season > 999 || *in.Episode < 1 || *in.Episode > 99999)) {
			metadataWorkflowHTTPError(w, http.StatusBadRequest, "invalid-arguments")
			return
		}
		result, err := s.service.PreviewVideoMetadataWorkflow(in.ProfileID, domain.VideoMetadataPreviewInput{VideoID: id, JobID: in.JobID, CandidateIndex: *in.CandidateIndex, Season: in.Season, Episode: in.Episode, Version: in.Version})
		metadataWorkflowHTTPResult(w, result, err)
	case "apply":
		var in struct {
			ProfileID int64  `json:"profileId"`
			Receipt   string `json:"receipt"`
			Confirm   bool   `json:"confirm"`
		}
		if !metadataWorkflowJSON(w, r, &in) {
			return
		}
		if !metadataWorkflowID(in.ProfileID) || !in.Confirm || len(in.Receipt) == 0 || len(in.Receipt) > 256 {
			metadataWorkflowHTTPError(w, http.StatusBadRequest, "invalid-arguments")
			return
		}
		result, err := s.service.ApplyVideoMetadataWorkflow(in.ProfileID, domain.VideoMetadataApplyInput{VideoID: id, Receipt: in.Receipt, Confirm: in.Confirm})
		metadataWorkflowHTTPResult(w, result, err)
	}
}

func metadataWorkflowID(id int64) bool { return id > 0 && id <= 9007199254740991 }

func metadataWorkflowJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		metadataWorkflowHTTPError(w, http.StatusBadRequest, "invalid-arguments")
		return false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		metadataWorkflowHTTPError(w, http.StatusBadRequest, "invalid-arguments")
		return false
	}
	return true
}

func metadataWorkflowHTTPError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code})
}

func metadataWorkflowHTTPResult(w http.ResponseWriter, value any, err error) {
	if err == nil {
		writeJSON(w, value)
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		metadataWorkflowHTTPError(w, 404, "not-found")
		return
	}
	if errors.Is(err, store.ErrVideoRevision) {
		metadataWorkflowHTTPError(w, 409, "metadata-revision-conflict")
		return
	}
	// Never expose provider diagnostics, database errors, host paths, or credentials.
	code := err.Error()
	switch code {
	case "provider-disabled":
		metadataWorkflowHTTPError(w, 503, code)
	case "metadata-queue-full":
		w.Header().Set("Retry-After", "30")
		metadataWorkflowHTTPError(w, 429, code)
	case "metadata-profile-not-found":
		metadataWorkflowHTTPError(w, 404, code)
	case "metadata-receipt-expired":
		metadataWorkflowHTTPError(w, 410, code)
	case "metadata-receipt-capacity":
		metadataWorkflowHTTPError(w, 429, code)
	case "metadata-preview-stale", "metadata-receipt-invalid", "metadata-job-not-ready", "metadata-job-stale", "metadata-job-conflict", "metadata-shared-work-conflict":
		metadataWorkflowHTTPError(w, 409, code)
	case "metadata-invalid-arguments", "metadata-confirm-required":
		metadataWorkflowHTTPError(w, 400, code)
	default:
		metadataWorkflowHTTPError(w, 500, "metadata-unavailable")
	}
}
