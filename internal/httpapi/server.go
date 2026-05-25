package httpapi

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/service"
)

type Server struct {
	service *service.Service
	static  http.Handler
	options Options
}

type Options struct {
	APIToken string
}

const authCookieName = "foliospace_api_token"

func New(service *service.Service, static http.Handler) *Server {
	return NewWithOptions(service, static, Options{})
}

func NewWithOptions(service *service.Service, static http.Handler, options Options) *Server {
	return &Server{service: service, static: static, options: options}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/status", s.handleAuthStatus)
	mux.HandleFunc("/api/auth/check", s.handleAuthCheck)
	mux.HandleFunc("/api/auth/logout", s.handleAuthLogout)
	mux.HandleFunc("/api/client/info", s.handleClientInfo)
	mux.HandleFunc("/api/client/home", s.handleClientHome)
	mux.HandleFunc("/api/client/books/", s.handleClientBookAction)
	mux.HandleFunc("/api/libraries", s.handleLibraries)
	mux.HandleFunc("/api/libraries/", s.handleLibraryAction)
	mux.HandleFunc("/api/collections", s.handleSeries)
	mux.HandleFunc("/api/collections/", s.handleCollectionAction)
	mux.HandleFunc("/api/series", s.handleSeries)
	mux.HandleFunc("/api/series/", s.handleSeriesAction)
	mux.HandleFunc("/api/books/continue-reading", s.handleContinueReading)
	mux.HandleFunc("/api/books/recent", s.handleRecentBooks)
	mux.HandleFunc("/api/books/", s.handleBookAction)
	mux.HandleFunc("/api/jobs", s.handleJobs)
	mux.HandleFunc("/api/jobs/", s.handleJobAction)
	mux.HandleFunc("/api/errors", s.handleErrors)
	mux.HandleFunc("/favicon.ico", s.handleFavicon)
	mux.HandleFunc("/", s.handleStatic)
	return s.authMiddleware(mux)
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") || s.isPublicAuthPath(r.URL.Path) || s.authorizeAPI(w, r) {
			next.ServeHTTP(w, r)
		}
	})
}

func (s *Server) isPublicAuthPath(path string) bool {
	return path == "/api/auth/status" || path == "/api/auth/check" || path == "/api/auth/logout"
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]bool{"enabled": strings.TrimSpace(s.options.APIToken) != ""})
}

func (s *Server) handleAuthCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if s.validToken(req.Token) {
		s.setAuthCookie(w, req.Token)
		writeJSON(w, map[string]bool{"ok": true})
		return
	}
	writeError(w, http.StatusUnauthorized, errors.New("invalid access token"))
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleClientInfo(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeClient(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, clientInfoResponse{
		ServiceName:      "FolioSpace Reader",
		APIVersion:       "v1",
		SupportedFormats: []string{"cbz", "zip", "epub"},
		Capabilities: clientCapabilities{
			ClientHome:       true,
			UnifiedManifest:  true,
			ProgressSync:     true,
			EPUBStreaming:    true,
			PageStreaming:    true,
			BearerTokenAuth:  s.options.APIToken != "",
			ScannerJobEvents: true,
		},
	})
}

func (s *Server) handleClientHome(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeClient(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	continueReading, err := s.service.ContinueReading(queryLimit(r, 12))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	recentBooks, err := s.service.RecentBooks(queryLimit(r, 12))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	collections, err := s.service.ListSeries()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, clientHomeResponse{
		ContinueReading: clientBooks(continueReading),
		RecentBooks:     clientBooks(recentBooks),
		Collections:     clientCollections(collections),
	})
}

func (s *Server) handleClientBookAction(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeClient(w, r) {
		return
	}
	id, tail, ok := parseIDTail(r.URL.Path, "/api/client/books/")
	if !ok || tail != "manifest" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	manifest, err := s.clientBookManifest(id)
	writeJSONOrError(w, manifest, err)
}

func (s *Server) authorizeClient(w http.ResponseWriter, r *http.Request) bool {
	return s.authorizeAPI(w, r)
}

func (s *Server) authorizeAPI(w http.ResponseWriter, r *http.Request) bool {
	if s.validToken(bearerToken(r.Header.Get("Authorization"))) || s.validCookie(r) {
		return true
	}
	w.Header().Set("WWW-Authenticate", `Bearer realm="FolioSpace Reader"`)
	writeError(w, http.StatusUnauthorized, errors.New("missing or invalid bearer token"))
	return false
}

func (s *Server) validToken(value string) bool {
	token := strings.TrimSpace(s.options.APIToken)
	if token == "" {
		return true
	}
	return subtle.ConstantTimeCompare([]byte(value), []byte(token)) == 1
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}

func (s *Server) validCookie(r *http.Request) bool {
	cookie, err := r.Cookie(authCookieName)
	if err != nil {
		return false
	}
	return s.validToken(cookie.Value)
}

func (s *Server) setAuthCookie(w http.ResponseWriter, token string) {
	if strings.TrimSpace(s.options.APIToken) == "" {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   60 * 60 * 24 * 365,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clientBookManifest(bookID int64) (clientBookManifestResponse, error) {
	book, err := s.service.Book(bookID)
	if err != nil {
		return clientBookManifestResponse{}, err
	}
	progress, err := s.clientProgress(bookID)
	if err != nil {
		return clientBookManifestResponse{}, err
	}

	out := clientBookManifestResponse{
		Book:     clientBookItem(book),
		Format:   book.Format,
		CoverURL: clientCoverURL(book.ID),
		Progress: progress,
	}
	if book.Format == "epub" {
		manifest, err := s.service.EPUBManifest(bookID)
		if err != nil {
			return clientBookManifestResponse{}, err
		}
		out.EPUB = &clientEPUBOpenInfo{
			Title:           manifest.Title,
			Creator:         manifest.Creator,
			CoverHref:       manifest.CoverHref,
			Spine:           manifest.Spine,
			TOC:             manifest.TOC,
			ResourceBaseURL: fmt.Sprintf("/api/books/%d/epub/resources/", book.ID),
			CoverURL:        clientCoverURL(book.ID),
		}
		return out, nil
	}

	pages, err := s.service.Pages(bookID)
	if err != nil {
		return clientBookManifestResponse{}, err
	}
	out.Pages = make([]clientPageRef, 0, len(pages))
	for _, page := range pages {
		out.Pages = append(out.Pages, clientPageRef{
			Index: page.Index,
			Name:  page.Name,
			URL:   fmt.Sprintf("/api/books/%d/pages/%d", book.ID, page.Index),
		})
	}
	return out, nil
}

func (s *Server) clientProgress(bookID int64) (clientProgress, error) {
	progress, err := s.service.Progress(bookID)
	if errors.Is(err, sql.ErrNoRows) {
		return clientProgress{BookID: bookID, PageIndex: 0, Locator: "", ProgressFraction: 0}, nil
	}
	if err != nil {
		return clientProgress{}, err
	}
	return clientProgress{
		BookID:           progress.BookID,
		PageIndex:        progress.PageIndex,
		Locator:          progress.Locator,
		ProgressFraction: progress.ProgressFraction,
	}, nil
}

func (s *Server) handleLibraries(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.service.ListLibraries()
		writeJSONOrError(w, items, err)
	case http.MethodPost:
		var req struct {
			Name     string `json:"name"`
			RootPath string `json:"rootPath"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		lib, err := s.service.CreateLibrary(req.Name, req.RootPath)
		writeJSONOrError(w, lib, err)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleLibraryAction(w http.ResponseWriter, r *http.Request) {
	id, tail, ok := parseIDTail(r.URL.Path, "/api/libraries/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	if tail == "" && r.Method == http.MethodDelete {
		writeJSONOrError(w, map[string]bool{"ok": true}, s.service.DeleteLibrary(id))
		return
	}
	if tail != "scan" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	job, err := s.service.ScanLibrary(id)
	writeJSONOrError(w, job, err)
}

func (s *Server) handleSeries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	items, err := s.service.ListSeries()
	writeJSONOrError(w, items, err)
}

func (s *Server) handleSeriesAction(w http.ResponseWriter, r *http.Request) {
	id, action, ok := parseIDAction(r.URL.Path, "/api/series/")
	if !ok || action != "books" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	items, err := s.service.ListBooks(id)
	writeJSONOrError(w, items, err)
}

func (s *Server) handleCollectionAction(w http.ResponseWriter, r *http.Request) {
	id, action, ok := parseIDAction(r.URL.Path, "/api/collections/")
	if !ok || action != "volumes" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if hasBookListQuery(r) {
		page, err := s.service.ListBooksPage(domain.BookListOptions{
			SeriesID: id,
			Limit:    queryInt(r, "limit", 60, 200),
			Offset:   queryInt(r, "offset", 0, 0),
			Query:    r.URL.Query().Get("q"),
			Sort:     r.URL.Query().Get("sort"),
		})
		writeJSONOrError(w, page, err)
		return
	}
	items, err := s.service.ListBooks(id)
	writeJSONOrError(w, items, err)
}

func (s *Server) handleContinueReading(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	items, err := s.service.ContinueReading(queryLimit(r, 12))
	writeJSONOrError(w, items, err)
}

func (s *Server) handleRecentBooks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	items, err := s.service.RecentBooks(queryLimit(r, 12))
	writeJSONOrError(w, items, err)
}

func (s *Server) handleBookAction(w http.ResponseWriter, r *http.Request) {
	id, tail, ok := parseIDTail(r.URL.Path, "/api/books/")
	if !ok {
		http.NotFound(w, r)
		return
	}

	if tail == "" && r.Method == http.MethodGet {
		book, err := s.service.Book(id)
		writeJSONOrError(w, book, err)
		return
	}
	if tail == "cover" && r.Method == http.MethodGet {
		s.streamCover(w, id)
		return
	}
	if tail == "epub/manifest" && r.Method == http.MethodGet {
		manifest, err := s.service.EPUBManifest(id)
		writeJSONOrError(w, manifest, err)
		return
	}
	if strings.HasPrefix(tail, "epub/resources/") && r.Method == http.MethodGet {
		s.streamEPUBResource(w, id, strings.TrimPrefix(tail, "epub/resources/"))
		return
	}
	if tail == "pages" && r.Method == http.MethodGet {
		pages, err := s.service.Pages(id)
		writeJSONOrError(w, pages, err)
		return
	}
	if strings.HasPrefix(tail, "pages/") && r.Method == http.MethodGet {
		pageIndex, err := strconv.Atoi(strings.TrimPrefix(tail, "pages/"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		s.streamPage(w, id, pageIndex)
		return
	}
	if tail == "progress" && r.Method == http.MethodPut {
		var req struct {
			PageIndex        int     `json:"pageIndex"`
			Locator          string  `json:"locator"`
			ProgressFraction float64 `json:"progressFraction"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSONOrError(w, map[string]bool{"ok": true}, s.service.SaveProgressDetail(id, req.PageIndex, req.Locator, req.ProgressFraction))
		return
	}
	if tail == "progress" && r.Method == http.MethodGet {
		progress, err := s.service.Progress(id)
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONOrError(w, domainDefaultProgress(id), nil)
			return
		}
		writeJSONOrError(w, progress, err)
		return
	}
	if tail == "analyze" && r.Method == http.MethodPost {
		pages, err := s.service.AnalyzeBook(id)
		writeJSONOrError(w, pages, err)
		return
	}

	http.NotFound(w, r)
}

func domainDefaultProgress(bookID int64) map[string]any {
	return map[string]any{
		"bookId":           bookID,
		"pageIndex":        0,
		"locator":          "",
		"progressFraction": 0,
	}
}

func queryLimit(r *http.Request, fallback int) int {
	return queryInt(r, "limit", fallback, 50)
}

func queryInt(r *http.Request, key string, fallback int, max int) int {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return fallback
	}
	if key == "limit" && parsed <= 0 {
		return fallback
	}
	if max > 0 && parsed > max {
		return max
	}
	return parsed
}

func hasBookListQuery(r *http.Request) bool {
	query := r.URL.Query()
	return query.Has("limit") || query.Has("offset") || query.Has("q") || query.Has("sort")
}

func (s *Server) streamPage(w http.ResponseWriter, bookID int64, pageIndex int) {
	page, err := s.service.OpenPage(bookID, pageIndex)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer page.Body.Close()

	w.Header().Set("Content-Type", page.ContentType)
	_, _ = io.Copy(w, page.Body)
}

func (s *Server) streamCover(w http.ResponseWriter, bookID int64) {
	page, err := s.service.OpenCover(bookID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer page.Body.Close()

	w.Header().Set("Content-Type", page.ContentType)
	_, _ = io.Copy(w, page.Body)
}

func (s *Server) streamEPUBResource(w http.ResponseWriter, bookID int64, resourcePath string) {
	page, err := s.service.OpenEPUBResource(bookID, resourcePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer page.Body.Close()

	w.Header().Set("Content-Type", page.ContentType)
	_, _ = io.Copy(w, page.Body)
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	items, err := s.service.ListJobs()
	writeJSONOrError(w, items, err)
}

func (s *Server) handleJobAction(w http.ResponseWriter, r *http.Request) {
	id, action, ok := parseIDAction(r.URL.Path, "/api/jobs/")
	if !ok || action != "events" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	items, err := s.service.JobEvents(id)
	writeJSONOrError(w, items, err)
}

func (s *Server) handleErrors(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var jobID int64
	if value := r.URL.Query().Get("jobId"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		jobID = parsed
	}
	var items any
	var err error
	if jobID > 0 {
		items, err = s.service.ListErrorsByJob(jobID)
	} else {
		items, err = s.service.ListErrors()
	}
	writeJSONOrError(w, items, err)
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if s.static != nil {
		s.static.ServeHTTP(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("FolioSpace Reader"))
}

func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func parseIDAction(path string, prefix string) (int64, string, bool) {
	id, tail, ok := parseIDTail(path, prefix)
	if !ok || tail == "" || strings.Contains(tail, "/") {
		return 0, "", false
	}
	return id, tail, true
}

func parseIDTail(path string, prefix string) (int64, string, bool) {
	rest := strings.TrimPrefix(path, prefix)
	if rest == path || rest == "" {
		return 0, "", false
	}
	parts := strings.SplitN(rest, "/", 2)
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, "", false
	}
	tail := ""
	if len(parts) == 2 {
		tail = parts[1]
	}
	return id, tail, true
}

func writeJSONOrError(w http.ResponseWriter, value any, err error) {
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, value)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

type clientInfoResponse struct {
	ServiceName      string             `json:"serviceName"`
	APIVersion       string             `json:"apiVersion"`
	SupportedFormats []string           `json:"supportedFormats"`
	Capabilities     clientCapabilities `json:"capabilities"`
}

type clientCapabilities struct {
	ClientHome       bool `json:"clientHome"`
	UnifiedManifest  bool `json:"unifiedManifest"`
	ProgressSync     bool `json:"progressSync"`
	EPUBStreaming    bool `json:"epubStreaming"`
	PageStreaming    bool `json:"pageStreaming"`
	BearerTokenAuth  bool `json:"bearerTokenAuth"`
	ScannerJobEvents bool `json:"scannerJobEvents"`
}

type clientHomeResponse struct {
	ContinueReading []clientBook       `json:"continueReading"`
	RecentBooks     []clientBook       `json:"recentBooks"`
	Collections     []clientCollection `json:"collections"`
}

type clientCollection struct {
	ID             int64  `json:"id"`
	Title          string `json:"title"`
	CollectionType string `json:"collectionType"`
	BookCount      int64  `json:"bookCount"`
}

type clientBook struct {
	ID               int64   `json:"id"`
	CollectionID     int64   `json:"collectionId"`
	CollectionTitle  string  `json:"collectionTitle,omitempty"`
	Title            string  `json:"title"`
	BookType         string  `json:"bookType"`
	Format           string  `json:"format"`
	PageCount        int     `json:"pageCount"`
	CoverStatus      string  `json:"coverStatus"`
	CoverURL         string  `json:"coverUrl"`
	CurrentPage      int     `json:"currentPage"`
	ProgressFraction float64 `json:"progressFraction"`
}

type clientBookManifestResponse struct {
	Book     clientBook          `json:"book"`
	Format   string              `json:"format"`
	CoverURL string              `json:"coverUrl"`
	Progress clientProgress      `json:"progress"`
	Pages    []clientPageRef     `json:"pages,omitempty"`
	EPUB     *clientEPUBOpenInfo `json:"epub,omitempty"`
}

type clientProgress struct {
	BookID           int64   `json:"bookId"`
	PageIndex        int     `json:"pageIndex"`
	Locator          string  `json:"locator"`
	ProgressFraction float64 `json:"progressFraction"`
}

type clientPageRef struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
	URL   string `json:"url"`
}

type clientEPUBOpenInfo struct {
	Title           string                 `json:"title"`
	Creator         string                 `json:"creator"`
	CoverHref       string                 `json:"coverHref"`
	Spine           []domain.EPUBSpineItem `json:"spine"`
	TOC             []domain.EPUBTOCItem   `json:"toc"`
	ResourceBaseURL string                 `json:"resourceBaseUrl"`
	CoverURL        string                 `json:"coverUrl"`
}

func clientCollections(collections []domain.Series) []clientCollection {
	out := make([]clientCollection, 0, len(collections))
	for _, collection := range collections {
		out = append(out, clientCollection{
			ID:             collection.ID,
			Title:          collection.Title,
			CollectionType: collection.CollectionType,
			BookCount:      collection.BookCount,
		})
	}
	return out
}

func clientBooks(books []domain.Book) []clientBook {
	out := make([]clientBook, 0, len(books))
	for _, book := range books {
		out = append(out, clientBookItem(book))
	}
	return out
}

func clientBookItem(book domain.Book) clientBook {
	return clientBook{
		ID:               book.ID,
		CollectionID:     book.SeriesID,
		CollectionTitle:  book.CollectionTitle,
		Title:            book.Title,
		BookType:         book.BookType,
		Format:           book.Format,
		PageCount:        book.PageCount,
		CoverStatus:      book.CoverStatus,
		CoverURL:         clientCoverURL(book.ID),
		CurrentPage:      book.CurrentPage,
		ProgressFraction: book.ProgressFraction,
	}
}

func clientCoverURL(bookID int64) string {
	return fmt.Sprintf("/api/books/%d/cover", bookID)
}
