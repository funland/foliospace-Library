package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

func (s *Server) handleVideoMetadata(w http.ResponseWriter, r *http.Request, id int64) {
	if !s.authorizeAPI(w, r) {
		return
	}
	if r.Method == http.MethodGet {
		m, err := s.service.VideoMetadata(id)
		writeJSONOrError(w, m, err)
		return
	}
	if r.Method != http.MethodPut && r.Method != http.MethodDelete {
		w.Header().Set("Allow", "GET, PUT, DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Field string          `json:"field"`
		Value json.RawMessage `json:"value"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 70000)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		writeError(w, http.StatusBadRequest, errors.New("expected one JSON object"))
		return
	}
	if input.Field != "title" && input.Field != "overview" && input.Field != "year" {
		writeError(w, http.StatusBadRequest, errors.New("unsupported metadata field"))
		return
	}
	var err error
	if r.Method == http.MethodPut {
		err = s.service.SetVideoMetadataOverride(id, input.Field, string(input.Value))
	} else {
		err = s.service.ResetVideoMetadataOverride(id, input.Field)
	}
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeError(w, status, err)
		return
	}
	m, err := s.service.VideoMetadata(id)
	writeJSONOrError(w, m, err)
}
