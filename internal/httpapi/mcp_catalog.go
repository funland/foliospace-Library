package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"foliospace-reader/internal/store"
)

func (s *Server) registerMCPCatalogRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/client/catalog/books", s.handleMCPCatalog)
	mux.HandleFunc("/api/client/catalog/search", s.handleMCPCatalog)
	mux.HandleFunc("/api/client/catalog/comics", s.handleMCPCatalog)
}

func catalogRequestNumber(r *http.Request, key string, fallback, max int64) (int64, error) {
	values, exists := r.URL.Query()[key]
	if !exists {
		return fallback, nil
	}
	if len(values) != 1 || values[0] == "" {
		return 0, store.ErrCatalogArguments
	}
	n, err := strconv.ParseInt(values[0], 10, 64)
	if err != nil || n < 0 || n > max {
		return 0, store.ErrCatalogArguments
	}
	return n, nil
}

func (s *Server) handleMCPCatalog(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if !s.authorizeAPI(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, err := url.ParseQuery(r.URL.RawQuery); err != nil {
		writeError(w, http.StatusBadRequest, store.ErrCatalogArguments)
		return
	}
	allowed := map[string]bool{"q": true, "limit": true, "profileId": true}
	switch r.URL.Path {
	case "/api/client/catalog/books":
		allowed["format"] = true
		allowed["sort"] = true
		allowed["offset"] = true
	case "/api/client/catalog/search":
		allowed["format"] = true
		allowed["cursor"] = true
	case "/api/client/catalog/comics":
		allowed["libraryId"] = true
		allowed["path"] = true
		allowed["offset"] = true
	}
	for key, values := range r.URL.Query() {
		if !allowed[key] || len(values) != 1 {
			writeError(w, http.StatusBadRequest, store.ErrCatalogArguments)
			return
		}
	}
	profile, err := catalogRequestNumber(r, "profileId", 0, 9007199254740991)
	if _, exists := r.URL.Query()["profileId"]; exists && profile == 0 {
		err = store.ErrCatalogArguments
	}
	if values, present := r.Header[http.CanonicalHeaderKey("X-FolioSpace-Profile-Id")]; present {
		if len(values) != 1 || strings.TrimSpace(values[0]) == "" {
			writeError(w, http.StatusBadRequest, store.ErrCatalogArguments)
			return
		}
		header := strings.TrimSpace(values[0])
		n, e := strconv.ParseInt(header, 10, 64)
		if e != nil || n < 1 || n > 9007199254740991 || (profile != 0 && profile != n) {
			err = store.ErrCatalogArguments
		} else {
			profile = n
		}
	}
	limit, e1 := catalogRequestNumber(r, "limit", 20, 100)
	offset, e2 := catalogRequestNumber(r, "offset", 0, 10000000)
	if err != nil || e1 != nil || e2 != nil || limit == 0 {
		writeError(w, http.StatusBadRequest, store.ErrCatalogArguments)
		return
	}
	o := store.CatalogOptions{Query: r.URL.Query().Get("q"), Format: r.URL.Query().Get("format"), Sort: r.URL.Query().Get("sort"), Cursor: r.URL.Query().Get("cursor"), Limit: int(limit), Offset: int(offset), ProfileID: profile}
	var data any
	switch r.URL.Path {
	case "/api/client/catalog/books":
		data, err = s.service.CatalogBooks(r.Context(), o)
	case "/api/client/catalog/search":
		data, err = s.service.SearchCatalog(r.Context(), o)
	case "/api/client/catalog/comics":
		library, e := catalogRequestNumber(r, "libraryId", 0, 9007199254740991)
		if e != nil {
			writeError(w, http.StatusBadRequest, store.ErrCatalogArguments)
			return
		}
		var page store.ComicBrowserPage
		var resolved int64
		page, resolved, err = s.service.ComicCatalog(r.Context(), profile, store.ComicBrowserOptions{LibraryID: library, Path: r.URL.Query().Get("path"), Query: o.Query, Limit: o.Limit, Offset: o.Offset})
		items := make([]map[string]any, 0, len(page.Items))
		for _, item := range page.Items {
			items = append(items, map[string]any{"id": item.ID, "libraryId": item.LibraryID, "kind": item.Kind, "path": item.Path, "displayTitle": item.DisplayTitle, "bookCount": item.BookCount, "collectionCount": item.CollectionCount})
		}
		data = map[string]any{"items": items, "total": page.Total, "limit": page.Limit, "offset": page.Offset, "hasMore": page.HasMore, "libraryId": page.LibraryID, "path": page.Path, "profileId": resolved, "scope": "shared-catalog", "consistency": "offset pagination; restart after catalog or title-rule changes"}
	}
	if err != nil {
		switch {
		case errors.Is(err, store.ErrCatalogArguments), errors.Is(err, store.ErrInvalidComicRequest):
			writeError(w, http.StatusBadRequest, errors.New("invalid catalog arguments"))
		case errors.Is(err, store.ErrCatalogProfile):
			writeError(w, http.StatusNotFound, store.ErrCatalogProfile)
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, errors.New("catalog entry not found"))
		default:
			writeError(w, http.StatusInternalServerError, errors.New("catalog unavailable"))
		}
		return
	}
	writeJSON(w, data)
}
