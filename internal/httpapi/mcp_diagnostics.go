package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"foliospace-reader/internal/service"
)

func (s *Server) registerMCPDiagnosticRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/client/diagnostics/books/", s.handleMCPDiagnostic)
	mux.HandleFunc("/api/client/diagnostics/games/", s.handleMCPDiagnostic)
}

func diagnosticHTTPError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	writeJSON(w, map[string]string{"code": code})
}

func diagnosticPositiveID(raw string) (int64, error) {
	if raw == "" {
		return 0, service.ErrDiagnosticInvalidArgument
	}
	for _, digit := range raw {
		if digit < '0' || digit > '9' {
			return 0, service.ErrDiagnosticInvalidArgument
		}
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 || n > 9007199254740991 {
		return 0, service.ErrDiagnosticInvalidArgument
	}
	return n, nil
}

// A supplied profile must never fall back to a different person's profile.
func diagnosticRequestProfile(r *http.Request) (int64, error) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return 0, service.ErrDiagnosticInvalidArgument
	}
	var profileID int64
	for key, values := range query {
		if key != "profileId" && key != "access_token" || len(values) != 1 {
			return 0, service.ErrDiagnosticInvalidArgument
		}
	}
	if values, present := query["profileId"]; present {
		profileID, err = diagnosticPositiveID(values[0])
		if err != nil {
			return 0, err
		}
	}
	if values, present := r.Header[http.CanonicalHeaderKey("X-FolioSpace-Profile-Id")]; present {
		if len(values) != 1 {
			return 0, service.ErrDiagnosticInvalidArgument
		}
		headerID, err := diagnosticPositiveID(values[0])
		if err != nil || profileID != 0 && profileID != headerID {
			return 0, service.ErrDiagnosticInvalidArgument
		}
		profileID = headerID
	}
	return profileID, nil
}

func (s *Server) handleMCPDiagnostic(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if !s.authorizeAPI(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		diagnosticHTTPError(w, http.StatusMethodNotAllowed, "method-not-allowed")
		return
	}
	isBook := strings.HasPrefix(r.URL.Path, "/api/client/diagnostics/books/")
	prefix := "/api/client/diagnostics/games/"
	if isBook {
		prefix = "/api/client/diagnostics/books/"
	}
	id, err := diagnosticPositiveID(strings.TrimPrefix(r.URL.Path, prefix))
	if err != nil {
		diagnosticHTTPError(w, http.StatusBadRequest, "invalid-arguments")
		return
	}
	profileID, err := diagnosticRequestProfile(r)
	if err != nil {
		diagnosticHTTPError(w, http.StatusBadRequest, "invalid-arguments")
		return
	}
	var result any
	if isBook {
		result, err = s.service.BookDiagnostics(id, profileID)
	} else {
		result, err = s.service.GameDiagnostics(id, profileID)
	}
	if err != nil {
		switch {
		case errors.Is(err, service.ErrDiagnosticProfileNotFound):
			diagnosticHTTPError(w, http.StatusNotFound, "profile-not-found")
		case errors.Is(err, sql.ErrNoRows):
			diagnosticHTTPError(w, http.StatusNotFound, "asset-not-found")
		case errors.Is(err, service.ErrDiagnosticInvalidArgument):
			diagnosticHTTPError(w, http.StatusBadRequest, "invalid-arguments")
		default:
			diagnosticHTTPError(w, http.StatusInternalServerError, "diagnostic-unavailable")
		}
		return
	}
	writeJSON(w, result)
}
