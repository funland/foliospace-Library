package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"foliospace-reader/internal/store"
)

func (s *Server) registerComicRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/client/comic-browser", s.handleComicBrowser)
	mux.HandleFunc("/api/comic-title-rules", s.handleComicTitleRules)
}

func comicQueryInteger(r *http.Request, key string, defaultValue int64) (int64, error) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%w: invalid %s", store.ErrInvalidComicRequest, key)
	}
	return value, nil
}

func comicAPIError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, errors.New("comic library not found"))
	case errors.Is(err, store.ErrInvalidComicRequest):
		writeError(w, http.StatusBadRequest, err)
	default:
		writeError(w, http.StatusInternalServerError, errors.New("comic browser unavailable"))
	}
}

func (s *Server) handleComicBrowser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	libraryID, err := comicQueryInteger(r, "libraryId", 0)
	if err != nil {
		comicAPIError(w, err)
		return
	}
	limit, err := comicQueryInteger(r, "limit", 60)
	if err != nil {
		comicAPIError(w, err)
		return
	}
	offset, err := comicQueryInteger(r, "offset", 0)
	if err != nil {
		comicAPIError(w, err)
		return
	}
	page, err := s.service.ComicBrowser(s.requestProfileID(r), store.ComicBrowserOptions{LibraryID: libraryID, Path: r.URL.Query().Get("path"), Query: r.URL.Query().Get("q"), Limit: int(limit), Offset: int(offset)})
	if err != nil {
		comicAPIError(w, err)
		return
	}
	ids := make([]int64, 0, len(page.Items))
	for _, item := range page.Items {
		if item.CoverBookID > 0 {
			ids = append(ids, item.CoverBookID)
		}
	}
	revisions := s.service.BookArtworkRevisions(ids)
	for i := range page.Items {
		if id := page.Items[i].CoverBookID; id > 0 {
			page.Items[i].ThumbnailStatus = "pending"
			page.Items[i].ThumbnailURL = clientThumbnailURL(id, "small") + revisions[id]
		}
	}
	writeJSON(w, page)
}

func (s *Server) handleComicTitleRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPut && r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	libraryID, err := comicQueryInteger(r, "libraryId", 0)
	if err != nil || libraryID == 0 {
		comicAPIError(w, fmt.Errorf("%w: libraryId must be positive", store.ErrInvalidComicRequest))
		return
	}
	if r.Method == http.MethodGet {
		result, err := s.service.ComicTitleRules(libraryID)
		if err != nil {
			comicAPIError(w, err)
			return
		}
		writeJSON(w, result)
		return
	}
	var input struct {
		Rules *[]store.ComicTitleRule `json:"rules"`
		Reset bool                    `json:"reset"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var extra any
	if err := decoder.Decode(&input); err != nil || decoder.Decode(&extra) != io.EOF || (input.Rules == nil && !input.Reset) || (input.Rules != nil && input.Reset) {
		comicAPIError(w, fmt.Errorf("%w: provide rules or reset", store.ErrInvalidComicRequest))
		return
	}
	rules := store.DefaultComicTitleRules()
	if input.Rules != nil {
		rules = *input.Rules
	}
	var result store.ComicTitleRules
	if r.Method == http.MethodPost {
		result, err = s.service.PreviewComicTitleRules(libraryID, rules)
	} else {
		result, err = s.service.SaveComicTitleRules(libraryID, rules)
	}
	if err != nil {
		comicAPIError(w, err)
		return
	}
	writeJSON(w, result)
}
