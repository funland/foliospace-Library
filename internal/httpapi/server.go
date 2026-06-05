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
	"time"

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
const serviceVersion = "0.91"

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
	mux.HandleFunc("/api/setup/status", s.handleSetupStatus)
	mux.HandleFunc("/api/setup/initialize", s.handleSetupInitialize)
	mux.HandleFunc("/api/config/directory-roots", s.handleDirectoryRoots)
	mux.HandleFunc("/api/profiles", s.handleProfiles)
	mux.HandleFunc("/api/profiles/", s.handleProfileAction)
	mux.HandleFunc("/api/settings/scan", s.handleScanSettings)
	mux.HandleFunc("/api/client/info", s.handleClientInfo)
	mux.HandleFunc("/api/client/preferences", s.handleClientPreferences)
	mux.HandleFunc("/api/client/home", s.handleClientHome)
	mux.HandleFunc("/api/client/search", s.handleClientSearch)
	mux.HandleFunc("/api/client/games", s.handleClientGames)
	mux.HandleFunc("/api/client/games/", s.handleClientGameAction)
	mux.HandleFunc("/api/client/videos", s.handleClientVideos)
	mux.HandleFunc("/api/client/videos/", s.handleClientVideoAction)
	mux.HandleFunc("/api/client/books/favorites", s.handleClientFavoriteBooks)
	mux.HandleFunc("/api/client/books/private-status/", s.handleClientPrivateStatusBooks)
	mux.HandleFunc("/api/client/books/", s.handleClientBookAction)
	mux.HandleFunc("/api/libraries", s.handleLibraries)
	mux.HandleFunc("/api/libraries/", s.handleLibraryAction)
	mux.HandleFunc("/api/fs/directories", s.handleDirectories)
	mux.HandleFunc("/api/collections", s.handleSeries)
	mux.HandleFunc("/api/collections/", s.handleCollectionAction)
	mux.HandleFunc("/api/series", s.handleSeries)
	mux.HandleFunc("/api/series/", s.handleSeriesAction)
	mux.HandleFunc("/api/books/continue-reading", s.handleContinueReading)
	mux.HandleFunc("/api/books/recent", s.handleRecentBooks)
	mux.HandleFunc("/api/books/favorites", s.handleFavoriteBooks)
	mux.HandleFunc("/api/books/private-status/", s.handlePrivateStatusBooks)
	mux.HandleFunc("/api/books/", s.handleBookAction)
	mux.HandleFunc("/api/games/", s.handleGameAction)
	mux.HandleFunc("/api/games/recent", s.handleRecentGames)
	mux.HandleFunc("/api/videos/", s.handleVideoAction)
	mux.HandleFunc("/api/videos/recent", s.handleRecentVideos)
	mux.HandleFunc("/api/search", s.handleSearch)
	mux.HandleFunc("/api/thumbnail-worker/", s.handleThumbnailWorkerAction)
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
	return path == "/api/auth/status" ||
		path == "/api/auth/check" ||
		path == "/api/auth/logout" ||
		path == "/api/setup/status" ||
		path == "/api/setup/initialize" ||
		path == "/api/config/directory-roots"
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]bool{"enabled": s.authEnabled()})
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	status, err := s.service.SetupStatus(s.envTokenConfigured())
	writeJSONOrError(w, status, err)
}

func (s *Server) handleSetupInitialize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	status, err := s.service.SetupStatus(s.envTokenConfigured())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if status.Initialized {
		writeError(w, http.StatusConflict, errors.New("setup is already initialized"))
		return
	}
	if status.TokenConfigured && !s.requestAuthorized(r) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="FolioSpace Library"`)
		writeError(w, http.StatusUnauthorized, errors.New("missing or invalid bearer token"))
		return
	}
	var req service.SetupInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	submittedToken := strings.TrimSpace(req.Token)
	if s.envTokenConfigured() {
		req.Token = ""
	}
	lib, err := s.service.InitializeSetup(req, status.TokenConfigured)
	if err != nil {
		writeJSONOrError(w, lib, err)
		return
	}
	if token := s.setupCookieToken(r, submittedToken); token != "" {
		s.setAuthCookie(w, token)
	}
	writeJSON(w, lib)
}

func (s *Server) handleDirectoryRoots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	roots, err := s.service.DirectoryRoots()
	writeJSONOrError(w, map[string]any{"roots": roots}, err)
}

func (s *Server) handleProfiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		profiles, err := s.service.ListProfiles()
		writeJSONOrError(w, profiles, err)
	case http.MethodPost:
		var req struct {
			Name   string `json:"name"`
			Avatar string `json:"avatar"`
			Color  string `json:"color"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		profile, err := s.service.CreateProfile(req.Name, req.Avatar, req.Color)
		writeJSONOrError(w, profile, err)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleProfileAction(w http.ResponseWriter, r *http.Request) {
	id, tail, ok := parseIDTail(r.URL.Path, "/api/profiles/")
	if !ok || tail != "" {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodPut:
		var req struct {
			Name   string `json:"name"`
			Avatar string `json:"avatar"`
			Color  string `json:"color"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		profile, err := s.service.UpdateProfile(id, req.Name, req.Avatar, req.Color)
		writeJSONOrError(w, profile, err)
	case http.MethodDelete:
		writeJSONOrError(w, map[string]bool{"ok": true}, s.service.DeleteProfile(id))
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleScanSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, s.service.ScanSettings())
	case http.MethodPut:
		var req service.ScanSettings
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := s.service.SaveScanSettings(req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, s.service.ScanSettings())
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
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
		ServiceName:      "FolioSpace Library",
		ServiceVersion:   serviceVersion,
		APIVersion:       "v1",
		SupportedFormats: []string{"cbz", "zip", "epub", "pdf", "mp4", "m4v", "mov", "mkv", "avi", "webm", "nes", "sfc", "smc", "gba", "gb", "gbc", "nds", "3ds", "cia", "chd", "iso", "bin", "cue", "7z"},
		Capabilities: clientCapabilities{
			ClientHome:         true,
			UnifiedManifest:    true,
			ProgressSync:       true,
			EPUBStreaming:      true,
			PDFStreaming:       true,
			PDFPageLayout:      true,
			PDFWebtoonLayout:   true,
			ComicWebtoonLayout: true,
			CompactReader:      true,
			PageStreaming:      true,
			GameShelf:          true,
			GameCatalog:        true,
			VideoCatalog:       true,
			VideoHLS:           true,
			PrivateState:       true,
			Search:             true,
			Preferences:        true,
			Profiles:           true,
			BearerTokenAuth:    s.authEnabled(),
			SetupWizard:        true,
			ScannerJobEvents:   true,
			ScannerJobControl:  true,
			ScanSettings:       true,
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
	profileID := s.requestProfileID(r)
	continueReading, err := s.service.ContinueReadingForProfile(profileID, queryLimit(r, 12))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	recentBooks, err := s.service.RecentBooksForProfile(profileID, queryLimit(r, 12))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	favoriteBooks, err := s.service.FavoriteBooksForProfile(profileID, queryLimit(r, 12))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	wantBooks, err := s.service.BooksByPrivateStatusForProfile(profileID, "want", queryLimit(r, 12))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	gameShelf, err := s.service.RecentGames(queryLimit(r, 12))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	videoShelf, err := s.service.RecentVideos(queryLimit(r, 12))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	collections, err := s.service.ListSeriesForProfile(profileID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, clientHomeResponse{
		ContinueReading: clientBooks(continueReading),
		RecentBooks:     clientBooks(recentBooks),
		FavoriteBooks:   clientBooks(favoriteBooks),
		WantToRead:      clientBooks(wantBooks),
		GameShelf:       clientGames(gameShelf),
		VideoShelf:      clientVideos(videoShelf),
		Collections:     clientCollections(collections),
	})
}

func (s *Server) handleClientPreferences(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeClient(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		prefs, err := s.service.ClientPreferencesForProfile(s.requestProfileID(r))
		writeJSONOrError(w, prefs, err)
	case http.MethodPut:
		var req domain.ClientPreferences
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		prefs, err := s.service.SaveClientPreferencesForProfile(s.requestProfileID(r), req)
		writeJSONOrError(w, prefs, err)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleClientBookAction(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeClient(w, r) {
		return
	}
	id, tail, ok := parseIDTail(r.URL.Path, "/api/client/books/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	if tail == "manifest" && r.Method == http.MethodGet {
		manifest, err := s.clientBookManifest(id, s.requestProfileID(r))
		writeJSONOrError(w, manifest, err)
		return
	}
	if tail == "private-state" && r.Method == http.MethodGet {
		response, err := s.clientBookPrivateState(id, s.requestProfileID(r))
		writeJSONOrError(w, response, err)
		return
	}
	if tail == "private-state" && r.Method == http.MethodPut {
		var req domain.BookPrivateState
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		book, err := s.service.UpdateBookPrivateStateForProfile(id, s.requestProfileID(r), req)
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		writeJSON(w, clientPrivateStateResponse{
			Book:         clientBookItem(book),
			PrivateState: privateStateFromBook(book),
		})
		return
	}
	http.NotFound(w, r)
}

func (s *Server) handleClientGameAction(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeClient(w, r) {
		return
	}
	id, tail, ok := parseIDTail(r.URL.Path, "/api/client/games/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	if tail == "manifest" && r.Method == http.MethodGet {
		game, err := s.service.Game(id)
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		writeJSON(w, clientGameManifest(game))
		return
	}
	if tail == "cover" && r.Method == http.MethodGet {
		s.streamGameCover(w, id)
		return
	}
	if tail == "file" && r.Method == http.MethodGet {
		stream, err := s.service.OpenGameFile(id)
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		defer stream.Body.Close()
		w.Header().Set("Content-Type", stream.ContentType)
		_, _ = io.Copy(w, stream.Body)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) handleClientGames(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeClient(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	page, err := s.service.ListGamesPage(domain.GameListOptions{
		Limit:    queryInt(r, "limit", 50, 200),
		Offset:   queryInt(r, "offset", 0, 0),
		Query:    r.URL.Query().Get("q"),
		Platform: r.URL.Query().Get("platform"),
		Format:   r.URL.Query().Get("format"),
		Sort:     r.URL.Query().Get("sort"),
	})
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	writeJSON(w, clientGameListResponse{
		Items:   clientGames(page.Items),
		Total:   page.Total,
		Limit:   page.Limit,
		Offset:  page.Offset,
		HasMore: page.HasMore,
	})
}

func (s *Server) handleClientVideoAction(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeClient(w, r) {
		return
	}
	if r.URL.Path == "/api/client/videos/transcode/status" && r.Method == http.MethodGet {
		status, err := s.service.VideoTranscodeQueueStatus()
		writeJSONOrError(w, status, err)
		return
	}
	id, tail, ok := parseIDTail(r.URL.Path, "/api/client/videos/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	if tail == "manifest" && r.Method == http.MethodGet {
		video, err := s.service.Video(id)
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		writeJSON(w, clientVideoManifest(video))
		return
	}
	if tail == "transcode/status" && r.Method == http.MethodGet {
		status, err := s.service.VideoTranscodeStatus(id)
		writeJSONOrError(w, status, err)
		return
	}
	if tail == "file" && r.Method == http.MethodGet {
		path, err := s.service.VideoFilePath(id)
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		http.ServeFile(w, r, path)
		return
	}
	if strings.HasPrefix(tail, "hls/") && r.Method == http.MethodGet {
		name := strings.TrimPrefix(tail, "hls/")
		var path string
		var err error
		if name == "index.m3u8" {
			path, err = s.service.EnsureVideoHLS(id)
		} else {
			path, err = s.service.VideoHLSFilePath(id, name)
		}
		if err != nil {
			if service.IsVideoTranscodeBusy(err) {
				writeError(w, http.StatusConflict, err)
				return
			}
			writeJSONOrError(w, nil, err)
			return
		}
		if strings.HasSuffix(name, ".m3u8") {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		}
		http.ServeFile(w, r, path)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) handleClientVideos(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeClient(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	page, err := s.service.ListVideosPage(domain.VideoListOptions{
		Limit:  queryInt(r, "limit", 50, 200),
		Offset: queryInt(r, "offset", 0, 0),
		Query:  r.URL.Query().Get("q"),
		Format: r.URL.Query().Get("format"),
		Sort:   r.URL.Query().Get("sort"),
	})
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	writeJSON(w, clientVideoListResponse{
		Items:   clientVideos(page.Items),
		Total:   page.Total,
		Limit:   page.Limit,
		Offset:  page.Offset,
		HasMore: page.HasMore,
	})
}

func (s *Server) handleGameAction(w http.ResponseWriter, r *http.Request) {
	id, tail, ok := parseIDTail(r.URL.Path, "/api/games/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	if tail == "cover" && r.Method == http.MethodGet {
		s.streamGameCover(w, id)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) handleVideoAction(w http.ResponseWriter, r *http.Request) {
	id, tail, ok := parseIDTail(r.URL.Path, "/api/videos/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	if tail == "thumbnail" && r.Method == http.MethodGet {
		s.streamVideoThumbnail(w, id)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) handleClientFavoriteBooks(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeClient(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	items, err := s.service.FavoriteBooksForProfile(s.requestProfileID(r), queryLimit(r, 12))
	writeJSONOrError(w, clientBooks(items), err)
}

func (s *Server) handleClientPrivateStatusBooks(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeClient(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	status := strings.TrimPrefix(r.URL.Path, "/api/client/books/private-status/")
	if status == "" || strings.Contains(status, "/") {
		http.NotFound(w, r)
		return
	}
	items, err := s.service.BooksByPrivateStatusForProfile(s.requestProfileID(r), status, queryLimit(r, 12))
	writeJSONOrError(w, clientBooks(items), err)
}

func (s *Server) handleClientSearch(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeClient(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	books, err := s.service.SearchBooksForProfile(query, s.requestProfileID(r), queryInt(r, "limit", 20, 100))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, clientSearchResponse{
		Query: query,
		Books: clientBooks(books),
	})
}

func (s *Server) authorizeClient(w http.ResponseWriter, r *http.Request) bool {
	return s.authorizeAPI(w, r)
}

func (s *Server) authorizeAPI(w http.ResponseWriter, r *http.Request) bool {
	if !s.authEnabled() || s.requestAuthorized(r) {
		return true
	}
	w.Header().Set("WWW-Authenticate", `Bearer realm="FolioSpace Library"`)
	writeError(w, http.StatusUnauthorized, errors.New("missing or invalid bearer token"))
	return false
}

func (s *Server) requestAuthorized(r *http.Request) bool {
	return s.validToken(bearerToken(r.Header.Get("Authorization"))) || s.validToken(r.URL.Query().Get("access_token")) || s.validCookie(r)
}

func (s *Server) requestToken(r *http.Request) string {
	if token := bearerToken(r.Header.Get("Authorization")); s.validToken(token) {
		return token
	}
	if token := r.URL.Query().Get("access_token"); s.validToken(token) {
		return token
	}
	cookie, err := r.Cookie(authCookieName)
	if err == nil && s.validToken(cookie.Value) {
		return cookie.Value
	}
	return ""
}

func (s *Server) setupCookieToken(r *http.Request, submittedToken string) string {
	if s.validToken(submittedToken) {
		return submittedToken
	}
	return s.requestToken(r)
}

func (s *Server) authEnabled() bool {
	return s.envTokenConfigured() || s.service.AdminTokenConfigured()
}

func (s *Server) envTokenConfigured() bool {
	return strings.TrimSpace(s.options.APIToken) != ""
}

func (s *Server) validToken(value string) bool {
	token := strings.TrimSpace(s.options.APIToken)
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if token != "" {
		return subtle.ConstantTimeCompare([]byte(value), []byte(token)) == 1
	}
	return s.service.VerifyAdminToken(value)
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
	if !s.authEnabled() {
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

func (s *Server) clientBookManifest(bookID int64, profileID int64) (clientBookManifestResponse, error) {
	book, err := s.service.BookForProfile(bookID, profileID)
	if err != nil {
		return clientBookManifestResponse{}, err
	}
	progress, err := s.clientProgress(bookID, profileID)
	if err != nil {
		return clientBookManifestResponse{}, err
	}

	out := clientBookManifestResponse{
		Book:              clientBookItem(book),
		Format:            book.Format,
		CoverURL:          clientCoverURL(book.ID),
		Progress:          progress,
		ReaderModes:       readerModesForBookFormat(book.Format),
		DefaultReaderMode: defaultReaderModeForBookFormat(book.Format),
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
		pageURL := fmt.Sprintf("/api/books/%d/pages/%d", book.ID, page.Index)
		out.Pages = append(out.Pages, clientPageRef{
			Index:      page.Index,
			Name:       page.Name,
			URL:        pageURL,
			DisplayURL: pageURL + "?maxWidth=1200",
		})
	}
	return out, nil
}

func readerModesForBookFormat(format string) []string {
	switch strings.ToLower(format) {
	case "epub":
		return []string{"single"}
	case "cbz", "zip", "pdf":
		return []string{"single", "double", "webtoon"}
	default:
		return []string{"single"}
	}
}

func defaultReaderModeForBookFormat(format string) string {
	return "single"
}

func (s *Server) clientBookPrivateState(bookID int64, profileID int64) (clientPrivateStateResponse, error) {
	book, err := s.service.BookForProfile(bookID, profileID)
	if err != nil {
		return clientPrivateStateResponse{}, err
	}
	return clientPrivateStateResponse{
		Book:         clientBookItem(book),
		PrivateState: privateStateFromBook(book),
	}, nil
}

func (s *Server) clientProgress(bookID int64, profileID int64) (clientProgress, error) {
	progress, err := s.service.ProgressForProfile(bookID, profileID)
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
			Name      string `json:"name"`
			RootPath  string `json:"rootPath"`
			AssetType string `json:"assetType"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		lib, err := s.service.CreateLibraryWithType(req.Name, req.RootPath, req.AssetType)
		writeJSONOrError(w, lib, err)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDirectories(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	listing, err := s.service.ListDirectories(r.URL.Query().Get("path"))
	writeJSONOrError(w, listing, err)
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
	items, err := s.service.ListSeriesForProfile(s.requestProfileID(r))
	writeJSONOrError(w, clientCollections(items), err)
}

func (s *Server) handleSeriesAction(w http.ResponseWriter, r *http.Request) {
	id, action, ok := parseIDAction(r.URL.Path, "/api/series/")
	if !ok || (action != "books" && action != "cover") {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if action == "cover" {
		s.streamSeriesCover(w, id)
		return
	}
	items, err := s.service.ListBooksForProfile(id, s.requestProfileID(r))
	writeJSONOrError(w, booksWithThumbnails(items), err)
}

func (s *Server) handleCollectionAction(w http.ResponseWriter, r *http.Request) {
	id, action, ok := parseIDAction(r.URL.Path, "/api/collections/")
	if !ok || (action != "volumes" && action != "assets" && action != "private-state") {
		http.NotFound(w, r)
		return
	}
	if action == "private-state" {
		if r.Method != http.MethodPut {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req domain.CollectionPrivateState
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		collection, err := s.service.UpdateCollectionPrivateStateForProfile(id, s.requestProfileID(r), req)
		writeJSONOrError(w, collection, err)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if action == "assets" {
		assets, err := s.service.CollectionAssetsForProfile(id, s.requestProfileID(r))
		assets.Books = booksWithThumbnails(assets.Books)
		writeJSONOrError(w, map[string]any{
			"books": assets.Books,
			"games": games(assets.Games),
		}, err)
		return
	}
	profileID := s.requestProfileID(r)
	if hasBookListQuery(r) {
		page, err := s.service.ListBooksPageForProfile(domain.BookListOptions{
			SeriesID: id,
			Limit:    queryInt(r, "limit", 60, 200),
			Offset:   queryInt(r, "offset", 0, 0),
			Query:    r.URL.Query().Get("q"),
			Sort:     r.URL.Query().Get("sort"),
		}, profileID)
		writeJSONOrError(w, bookListPageWithThumbnails(page), err)
		return
	}
	items, err := s.service.ListBooksForProfile(id, profileID)
	writeJSONOrError(w, booksWithThumbnails(items), err)
}

func (s *Server) handleContinueReading(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	items, err := s.service.ContinueReadingForProfile(s.requestProfileID(r), queryLimit(r, 12))
	writeJSONOrError(w, booksWithThumbnails(items), err)
}

func (s *Server) handleRecentBooks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	items, err := s.service.RecentBooksForProfile(s.requestProfileID(r), queryLimit(r, 12))
	writeJSONOrError(w, booksWithThumbnails(items), err)
}

func (s *Server) handleRecentGames(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	items, err := s.service.RecentGames(queryLimit(r, 12))
	writeJSONOrError(w, games(items), err)
}

func (s *Server) handleRecentVideos(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	items, err := s.service.RecentVideos(queryLimit(r, 12))
	writeJSONOrError(w, clientVideos(items), err)
}

func (s *Server) streamGameCover(w http.ResponseWriter, gameID int64) {
	stream, err := s.service.OpenGameCover(gameID)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	defer stream.Body.Close()
	w.Header().Set("Content-Type", stream.ContentType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = io.Copy(w, stream.Body)
}

func (s *Server) streamVideoThumbnail(w http.ResponseWriter, videoID int64) {
	video, err := s.service.Video(videoID)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if stream, err := s.service.OpenVideoThumbnail(videoID); err == nil {
		defer stream.Body.Close()
		w.Header().Set("Content-Type", stream.ContentType)
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = io.Copy(w, stream.Body)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, videoThumbnailPlaceholder(video))
}

func (s *Server) handleFavoriteBooks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	items, err := s.service.FavoriteBooksForProfile(s.requestProfileID(r), queryLimit(r, 12))
	writeJSONOrError(w, booksWithThumbnails(items), err)
}

func (s *Server) handlePrivateStatusBooks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	status := strings.TrimPrefix(r.URL.Path, "/api/books/private-status/")
	if status == "" || strings.Contains(status, "/") {
		http.NotFound(w, r)
		return
	}
	items, err := s.service.BooksByPrivateStatusForProfile(s.requestProfileID(r), status, queryLimit(r, 12))
	writeJSONOrError(w, booksWithThumbnails(items), err)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	books, err := s.service.SearchBooksForProfile(query, s.requestProfileID(r), queryInt(r, "limit", 20, 100))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, searchResponse{
		Query: query,
		Books: booksWithThumbnails(books),
	})
}

func (s *Server) handleBookAction(w http.ResponseWriter, r *http.Request) {
	id, tail, ok := parseIDTail(r.URL.Path, "/api/books/")
	if !ok {
		http.NotFound(w, r)
		return
	}

	if tail == "" && r.Method == http.MethodGet {
		book, err := s.service.BookForProfile(id, s.requestProfileID(r))
		writeJSONOrError(w, bookWithThumbnail(book), err)
		return
	}
	if tail == "cover" && r.Method == http.MethodGet {
		s.streamCover(w, id)
		return
	}
	if tail == "thumbnail" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		s.streamBookThumbnail(w, r, id, r.URL.Query().Get("size"))
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
		s.streamPage(w, r, id, pageIndex)
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
		writeJSONOrError(w, map[string]bool{"ok": true}, s.service.SaveProgressDetailForProfile(id, s.requestProfileID(r), req.PageIndex, req.Locator, req.ProgressFraction))
		return
	}
	if tail == "progress" && r.Method == http.MethodGet {
		progress, err := s.service.ProgressForProfile(id, s.requestProfileID(r))
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONOrError(w, domainDefaultProgress(id), nil)
			return
		}
		writeJSONOrError(w, progress, err)
		return
	}
	if tail == "private-state" && r.Method == http.MethodPut {
		var req domain.BookPrivateState
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		book, err := s.service.UpdateBookPrivateStateForProfile(id, s.requestProfileID(r), req)
		writeJSONOrError(w, bookWithThumbnail(book), err)
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

func (s *Server) requestProfileID(r *http.Request) int64 {
	raw := strings.TrimSpace(r.Header.Get("X-FolioSpace-Profile-Id"))
	if raw == "" {
		raw = strings.TrimSpace(r.URL.Query().Get("profileId"))
	}
	if raw == "" {
		return 0
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0
	}
	profileID, err := s.service.ResolveProfileID(value)
	if err != nil {
		return 0
	}
	return profileID
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

func (s *Server) streamPage(w http.ResponseWriter, r *http.Request, bookID int64, pageIndex int) {
	book, err := s.service.Book(bookID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if book.Format == "pdf" {
		if pageIndex != 0 {
			writeError(w, http.StatusInternalServerError, fmt.Errorf("page index %d out of range", pageIndex))
			return
		}
		http.ServeFile(w, r, book.FilePath)
		return
	}
	page, err := s.service.OpenPageWithOptions(bookID, pageIndex, service.PageImageOptions{
		MaxWidth: parsePageMaxWidth(r),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer page.Body.Close()

	w.Header().Set("Content-Type", page.ContentType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = io.Copy(w, page.Body)
}

func parsePageMaxWidth(r *http.Request) int {
	raw := r.URL.Query().Get("maxWidth")
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0
	}
	if value < 320 {
		return 320
	}
	if value > 2400 {
		return 2400
	}
	return value
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

func (s *Server) streamBookThumbnail(w http.ResponseWriter, r *http.Request, bookID int64, size string) {
	stream, err := s.service.OpenBookThumbnail(bookID, size)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer stream.Body.Close()

	w.Header().Set("Content-Type", stream.ContentType)
	fallbackKind := thumbnailFallbackKind(stream)
	if stream.CacheHit && fallbackKind == "" {
		w.Header().Set("Cache-Control", "private, max-age=2592000")
		if stream.ETag != "" {
			w.Header().Set("ETag", `"`+stream.ETag+`"`)
		}
	} else {
		w.Header().Set("Cache-Control", "no-store")
		if fallbackKind != "" {
			w.Header().Set("X-FolioSpace-Thumbnail-Fallback", fallbackKind)
		}
	}
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, stream.Body)
}

func thumbnailFallbackKind(stream service.ThumbnailStream) string {
	if stream.StaleFallback {
		return "stale"
	}
	if stream.SourceFallback {
		return "source"
	}
	if stream.GenericFallback {
		return "generic"
	}
	return ""
}

func (s *Server) streamSeriesCover(w http.ResponseWriter, seriesID int64) {
	page, err := s.service.OpenSeriesCover(seriesID)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	defer page.Body.Close()

	w.Header().Set("Content-Type", page.ContentType)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = io.Copy(w, page.Body)
}

func (s *Server) handleThumbnailWorkerAction(w http.ResponseWriter, r *http.Request) {
	action := strings.TrimPrefix(r.URL.Path, "/api/thumbnail-worker/")
	switch action {
	case "status":
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
	case "pause", "resume", "cancel", "cleanup-orphans":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
	default:
		http.NotFound(w, r)
		return
	}

	switch action {
	case "pause":
		s.service.PauseThumbnailWorker()
	case "resume":
		s.service.ResumeThumbnailWorker()
	case "cancel":
		if _, err := s.service.CancelThumbnailJobs(); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	case "cleanup-orphans":
		if _, err := s.service.CleanupThumbnailOrphanCache(); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	status, err := s.service.ThumbnailWorkerStatus()
	writeJSONOrError(w, status, err)
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
	if !ok {
		http.NotFound(w, r)
		return
	}
	switch action {
	case "events":
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		items, err := s.service.JobEvents(id)
		writeJSONOrError(w, items, err)
	case "pause":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		job, err := s.service.PauseScanJob(id)
		writeJSONOrError(w, job, err)
	case "cancel":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		job, err := s.service.CancelScanJob(id)
		writeJSONOrError(w, job, err)
	case "resume":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		job, err := s.service.ResumeScanJob(id)
		writeJSONOrError(w, job, err)
	default:
		http.NotFound(w, r)
	}
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
	_, _ = w.Write([]byte("FolioSpace Library"))
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
	ServiceVersion   string             `json:"serviceVersion"`
	APIVersion       string             `json:"apiVersion"`
	SupportedFormats []string           `json:"supportedFormats"`
	Capabilities     clientCapabilities `json:"capabilities"`
}

type clientCapabilities struct {
	ClientHome         bool `json:"clientHome"`
	UnifiedManifest    bool `json:"unifiedManifest"`
	ProgressSync       bool `json:"progressSync"`
	EPUBStreaming      bool `json:"epubStreaming"`
	PDFStreaming       bool `json:"pdfStreaming"`
	PDFPageLayout      bool `json:"pdfPageLayout"`
	PDFWebtoonLayout   bool `json:"pdfWebtoonLayout"`
	ComicWebtoonLayout bool `json:"comicWebtoonLayout"`
	CompactReader      bool `json:"compactReader"`
	PageStreaming      bool `json:"pageStreaming"`
	GameShelf          bool `json:"gameShelf"`
	GameCatalog        bool `json:"gameCatalog"`
	VideoCatalog       bool `json:"videoCatalog"`
	VideoHLS           bool `json:"videoHls"`
	PrivateState       bool `json:"privateState"`
	Search             bool `json:"search"`
	Preferences        bool `json:"preferences"`
	Profiles           bool `json:"profiles"`
	BearerTokenAuth    bool `json:"bearerTokenAuth"`
	SetupWizard        bool `json:"setupWizard"`
	ScannerJobEvents   bool `json:"scannerJobEvents"`
	ScannerJobControl  bool `json:"scannerJobControl"`
	ScanSettings       bool `json:"scanSettings"`
}

type clientHomeResponse struct {
	ContinueReading []clientBook       `json:"continueReading"`
	RecentBooks     []clientBook       `json:"recentBooks"`
	FavoriteBooks   []clientBook       `json:"favoriteBooks"`
	WantToRead      []clientBook       `json:"wantToRead"`
	GameShelf       []clientGame       `json:"gameShelf"`
	VideoShelf      []clientVideo      `json:"videoShelf"`
	Collections     []clientCollection `json:"collections"`
}

type searchResponse struct {
	Query string        `json:"query"`
	Books []domain.Book `json:"books"`
}

type clientSearchResponse struct {
	Query string       `json:"query"`
	Books []clientBook `json:"books"`
}

type clientCollection struct {
	ID              int64  `json:"id"`
	LibraryID       int64  `json:"libraryId"`
	Title           string `json:"title"`
	DirectoryPath   string `json:"directoryPath"`
	CollectionType  string `json:"collectionType"`
	PrimaryType     string `json:"primaryType"`
	BookCount       int64  `json:"bookCount"`
	CoverBookID     int64  `json:"coverBookId,omitempty"`
	ThumbnailStatus string `json:"thumbnailStatus,omitempty"`
	ThumbnailURL    string `json:"thumbnailUrl,omitempty"`
	Favorite        bool   `json:"favorite"`
	Liked           bool   `json:"liked"`
}

type clientBook struct {
	ID               int64    `json:"id"`
	CollectionID     int64    `json:"collectionId"`
	SeriesID         int64    `json:"seriesId"`
	CollectionTitle  string   `json:"collectionTitle,omitempty"`
	Title            string   `json:"title"`
	Creator          string   `json:"creator,omitempty"`
	Description      string   `json:"description,omitempty"`
	BookType         string   `json:"bookType"`
	Format           string   `json:"format"`
	PageCount        int      `json:"pageCount"`
	CoverStatus      string   `json:"coverStatus"`
	CoverURL         string   `json:"coverUrl"`
	ThumbnailStatus  string   `json:"thumbnailStatus"`
	ThumbnailURL     string   `json:"thumbnailUrl"`
	Analyzed         bool     `json:"analyzed"`
	AddedAt          string   `json:"addedAt"`
	UpdatedAt        string   `json:"updatedAt"`
	CurrentPage      int      `json:"currentPage"`
	ProgressFraction float64  `json:"progressFraction"`
	LastReadAt       string   `json:"lastReadAt"`
	PrivateStatus    string   `json:"privateStatus"`
	Favorite         bool     `json:"favorite"`
	Rating           int      `json:"rating"`
	Tags             []string `json:"tags"`
	Summary          string   `json:"summary"`
}

type clientBookListPageResponse struct {
	Items   []clientBook `json:"items"`
	Total   int64        `json:"total"`
	Limit   int          `json:"limit"`
	Offset  int          `json:"offset"`
	HasMore bool         `json:"hasMore"`
}

type clientBookManifestResponse struct {
	Book              clientBook          `json:"book"`
	Format            string              `json:"format"`
	CoverURL          string              `json:"coverUrl"`
	Progress          clientProgress      `json:"progress"`
	ReaderModes       []string            `json:"readerModes"`
	DefaultReaderMode string              `json:"defaultReaderMode"`
	Pages             []clientPageRef     `json:"pages,omitempty"`
	EPUB              *clientEPUBOpenInfo `json:"epub,omitempty"`
}

type clientGame struct {
	ID            int64  `json:"id"`
	AssetType     string `json:"assetType"`
	Title         string `json:"title"`
	Platform      string `json:"platform"`
	ROMSetName    string `json:"romSetName,omitempty"`
	Region        string `json:"region,omitempty"`
	Format        string `json:"format"`
	Size          int64  `json:"size"`
	CRC32         string `json:"crc32"`
	SHA1          string `json:"sha1"`
	EmulatorHint  string `json:"emulatorHint"`
	Compatibility string `json:"compatibility"`
	CoverURL      string `json:"coverUrl,omitempty"`
	ManifestURL   string `json:"manifestUrl"`
}

type clientGameManifestResponse struct {
	Game    clientGame `json:"game"`
	FileURL string     `json:"fileUrl"`
}

type clientGameListResponse struct {
	Items   []clientGame `json:"items"`
	Total   int64        `json:"total"`
	Limit   int          `json:"limit"`
	Offset  int          `json:"offset"`
	HasMore bool         `json:"hasMore"`
}

type clientVideo struct {
	ID                 int64   `json:"id"`
	AssetType          string  `json:"assetType"`
	Title              string  `json:"title"`
	Format             string  `json:"format"`
	Size               int64   `json:"size"`
	DurationSeconds    float64 `json:"durationSeconds"`
	Width              int     `json:"width"`
	Height             int     `json:"height"`
	VideoCodec         string  `json:"videoCodec,omitempty"`
	AudioCodec         string  `json:"audioCodec,omitempty"`
	ThumbnailStatus    string  `json:"thumbnailStatus"`
	ThumbnailURL       string  `json:"thumbnailUrl"`
	ManifestURL        string  `json:"manifestUrl"`
	DirectPlayable     bool    `json:"directPlayable"`
	PlaybackMode       string  `json:"playbackMode"`
	PlaybackReason     string  `json:"playbackReason,omitempty"`
	FileURL            string  `json:"fileUrl,omitempty"`
	HLSURL             string  `json:"hlsUrl,omitempty"`
	TranscodeStatusURL string  `json:"transcodeStatusUrl,omitempty"`
}

type clientVideoManifestResponse struct {
	Video              clientVideo `json:"video"`
	FileURL            string      `json:"fileUrl"`
	HLSURL             string      `json:"hlsUrl,omitempty"`
	TranscodeStatusURL string      `json:"transcodeStatusUrl,omitempty"`
}

type clientVideoListResponse struct {
	Items   []clientVideo `json:"items"`
	Total   int64         `json:"total"`
	Limit   int           `json:"limit"`
	Offset  int           `json:"offset"`
	HasMore bool          `json:"hasMore"`
}

type clientPrivateStateResponse struct {
	Book         clientBook              `json:"book"`
	PrivateState domain.BookPrivateState `json:"privateState"`
}

type clientProgress struct {
	BookID           int64   `json:"bookId"`
	PageIndex        int     `json:"pageIndex"`
	Locator          string  `json:"locator"`
	ProgressFraction float64 `json:"progressFraction"`
}

type clientPageRef struct {
	Index      int    `json:"index"`
	Name       string `json:"name"`
	URL        string `json:"url"`
	DisplayURL string `json:"displayUrl,omitempty"`
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
		item := clientCollection{
			ID:             collection.ID,
			LibraryID:      collection.LibraryID,
			Title:          collection.Title,
			DirectoryPath:  collection.DirectoryPath,
			CollectionType: collection.CollectionType,
			PrimaryType:    collection.PrimaryType,
			BookCount:      collection.BookCount,
			CoverBookID:    collection.CoverBookID,
			Favorite:       collection.Favorite,
			Liked:          collection.Liked,
		}
		if collection.CoverBookID > 0 {
			item.ThumbnailStatus = "pending"
			item.ThumbnailURL = clientThumbnailURL(collection.CoverBookID, "small")
		}
		out = append(out, item)
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

func booksWithThumbnails(books []domain.Book) []domain.Book {
	out := make([]domain.Book, 0, len(books))
	for _, book := range books {
		out = append(out, bookWithThumbnail(book))
	}
	return out
}

func bookListPageWithThumbnails(page domain.BookListPage) domain.BookListPage {
	page.Items = booksWithThumbnails(page.Items)
	return page
}

func bookWithThumbnail(book domain.Book) domain.Book {
	book.ThumbnailStatus = thumbnailStatus(book)
	book.ThumbnailURL = clientThumbnailURL(book.ID, "small")
	return book
}

func clientBookListPage(page domain.BookListPage) clientBookListPageResponse {
	return clientBookListPageResponse{
		Items:   clientBooks(page.Items),
		Total:   page.Total,
		Limit:   page.Limit,
		Offset:  page.Offset,
		HasMore: page.HasMore,
	}
}

func games(items []domain.GameAsset) []domain.GameAsset {
	out := make([]domain.GameAsset, 0, len(items))
	for _, item := range items {
		item.FilePath = ""
		item.RelPath = ""
		item.CoverURL = gameCoverURL(item.ID, item.Platform)
		out = append(out, item)
	}
	return out
}

func clientGames(items []domain.GameAsset) []clientGame {
	out := make([]clientGame, 0, len(items))
	for _, item := range items {
		out = append(out, clientGameItem(item))
	}
	return out
}

func clientGameItem(game domain.GameAsset) clientGame {
	return clientGame{
		ID:            game.ID,
		AssetType:     "game",
		Title:         game.Title,
		Platform:      game.Platform,
		ROMSetName:    game.ROMSetName,
		Region:        game.Region,
		Format:        game.Format,
		Size:          game.Size,
		CRC32:         game.CRC32,
		SHA1:          game.SHA1,
		EmulatorHint:  game.EmulatorHint,
		Compatibility: game.Compatibility,
		CoverURL:      gameCoverURL(game.ID, game.Platform),
		ManifestURL:   fmt.Sprintf("/api/client/games/%d/manifest", game.ID),
	}
}

func gameCoverURL(gameID int64, platform string) string {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "nes", "snes", "gb", "gbc", "gba", "genesis", "mega-drive", "megadrive":
		return fmt.Sprintf("/api/games/%d/cover", gameID)
	default:
		return ""
	}
}

func clientGameManifest(game domain.GameAsset) clientGameManifestResponse {
	return clientGameManifestResponse{
		Game:    clientGameItem(game),
		FileURL: fmt.Sprintf("/api/client/games/%d/file", game.ID),
	}
}

func clientVideos(items []domain.VideoAsset) []clientVideo {
	out := make([]clientVideo, 0, len(items))
	for _, item := range items {
		out = append(out, clientVideoItem(item))
	}
	return out
}

func clientVideoItem(video domain.VideoAsset) clientVideo {
	fileURL := fmt.Sprintf("/api/client/videos/%d/file", video.ID)
	hlsURL := fmt.Sprintf("/api/client/videos/%d/hls/index.m3u8", video.ID)
	return clientVideo{
		ID:                 video.ID,
		AssetType:          "video",
		Title:              video.Title,
		Format:             video.Format,
		Size:               video.Size,
		DurationSeconds:    video.DurationSeconds,
		Width:              video.Width,
		Height:             video.Height,
		VideoCodec:         video.VideoCodec,
		AudioCodec:         video.AudioCodec,
		ThumbnailStatus:    video.ThumbnailStatus,
		ThumbnailURL:       fmt.Sprintf("/api/videos/%d/thumbnail?v=%d", video.ID, video.MTime.UnixNano()),
		ManifestURL:        fmt.Sprintf("/api/client/videos/%d/manifest", video.ID),
		DirectPlayable:     video.DirectPlayable,
		PlaybackMode:       video.PlaybackMode,
		PlaybackReason:     video.PlaybackReason,
		FileURL:            fileURL,
		HLSURL:             hlsURL,
		TranscodeStatusURL: fmt.Sprintf("/api/client/videos/%d/transcode/status", video.ID),
	}
}

func clientVideoManifest(video domain.VideoAsset) clientVideoManifestResponse {
	fileURL := fmt.Sprintf("/api/client/videos/%d/file", video.ID)
	hlsURL := fmt.Sprintf("/api/client/videos/%d/hls/index.m3u8", video.ID)
	return clientVideoManifestResponse{
		Video:              clientVideoItem(video),
		FileURL:            fileURL,
		HLSURL:             hlsURL,
		TranscodeStatusURL: fmt.Sprintf("/api/client/videos/%d/transcode/status", video.ID),
	}
}

func videoThumbnailPlaceholder(video domain.VideoAsset) string {
	title := htmlEscape(strings.TrimSpace(video.Title))
	if title == "" {
		title = "Video"
	}
	format := strings.ToUpper(htmlEscape(strings.TrimSpace(video.Format)))
	if format == "" {
		format = "VIDEO"
	}
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 320 180">
<defs><linearGradient id="g" x1="0" x2="1" y1="0" y2="1"><stop stop-color="#172326"/><stop offset="1" stop-color="#33565c"/></linearGradient></defs>
<rect width="320" height="180" rx="14" fill="url(#g)"/>
<circle cx="160" cy="82" r="32" fill="rgba(255,255,255,.16)"/>
<path d="M151 66v32l28-16z" fill="#fff"/>
<text x="20" y="142" fill="#f3fbfb" font-family="Arial, sans-serif" font-size="20" font-weight="700">%s</text>
<text x="20" y="164" fill="#b8d2d5" font-family="Arial, sans-serif" font-size="13">%s</text>
</svg>`, title, format)
}

func htmlEscape(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	value = strings.ReplaceAll(value, ">", "&gt;")
	value = strings.ReplaceAll(value, `"`, "&quot;")
	return value
}

func clientBookItem(book domain.Book) clientBook {
	return clientBook{
		ID:               book.ID,
		CollectionID:     book.SeriesID,
		SeriesID:         book.SeriesID,
		CollectionTitle:  book.CollectionTitle,
		Title:            book.Title,
		Creator:          book.Creator,
		Description:      book.Description,
		BookType:         book.BookType,
		Format:           book.Format,
		PageCount:        book.PageCount,
		CoverStatus:      book.CoverStatus,
		CoverURL:         clientCoverURL(book.ID),
		ThumbnailStatus:  thumbnailStatus(book),
		ThumbnailURL:     clientThumbnailURL(book.ID, "small"),
		Analyzed:         book.Analyzed,
		AddedAt:          formatClientTime(book.AddedAt),
		UpdatedAt:        formatClientTime(book.UpdatedAt),
		CurrentPage:      book.CurrentPage,
		ProgressFraction: book.ProgressFraction,
		LastReadAt:       formatClientTime(book.LastReadAt),
		PrivateStatus:    book.PrivateStatus,
		Favorite:         book.Favorite,
		Rating:           book.Rating,
		Tags:             book.Tags,
		Summary:          book.Summary,
	}
}

func clientCoverURL(bookID int64) string {
	return fmt.Sprintf("/api/books/%d/cover?v=%s", bookID, service.ThumbnailClientCacheVersion())
}

func clientThumbnailURL(bookID int64, size string) string {
	return fmt.Sprintf("/api/books/%d/thumbnail?size=%s&v=%s", bookID, size, service.ThumbnailClientCacheVersion())
}

func thumbnailStatus(book domain.Book) string {
	if strings.TrimSpace(book.ThumbnailStatus) != "" {
		return book.ThumbnailStatus
	}
	return "pending"
}

func formatClientTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339Nano)
}

func privateStateFromBook(book domain.Book) domain.BookPrivateState {
	return domain.BookPrivateState{
		Status:   book.PrivateStatus,
		Favorite: book.Favorite,
		Rating:   book.Rating,
		Tags:     book.Tags,
		Summary:  book.Summary,
	}
}
