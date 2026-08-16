package httpapi

import (
	"bytes"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/launchcatalog"
	"foliospace-reader/internal/service"
)

type Server struct {
	service           *service.Service
	static            http.Handler
	options           Options
	thumbnailRequests chan struct{}
}

type Options struct {
	APIToken                  string
	DisableGameLaunchResolver bool
}

const authCookieName = "foliospace_api_token"
const serviceVersion = "0.996"

func New(service *service.Service, static http.Handler) *Server {
	return NewWithOptions(service, static, Options{})
}

func NewWithOptions(service *service.Service, static http.Handler, options Options) *Server {
	return &Server{service: service, static: static, options: options, thumbnailRequests: make(chan struct{}, 8)}
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
	mux.HandleFunc("/api/settings/game-catalog", s.handleGameCatalogSettings)
	mux.HandleFunc("/api/client/info", s.handleClientInfo)
	mux.HandleFunc("/api/client/preferences", s.handleClientPreferences)
	mux.HandleFunc("/api/client/home", s.handleClientHome)
	mux.HandleFunc("/api/client/search", s.handleClientSearch)
	mux.HandleFunc("/api/client/manual-collections", s.handleClientManualCollections)
	mux.HandleFunc("/api/client/manual-collections/", s.handleClientManualCollectionAction)
	mux.HandleFunc("/api/client/games/platforms", s.handleClientGamePlatforms)
	mux.HandleFunc("/api/client/games", s.handleClientGames)
	mux.HandleFunc("/api/client/games/played", s.handleClientPlayedGames)
	mux.HandleFunc("/api/client/games/", s.handleClientGameAction)
	mux.HandleFunc("/api/client/videos", s.handleClientVideos)
	mux.HandleFunc("/api/client/videos/", s.handleClientVideoAction)
	mux.HandleFunc("/api/client/books", s.handleClientBooks)
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
	mux.HandleFunc("/api/games/metadata/providers", s.handleGameMetadataProviders)
	mux.HandleFunc("/api/games/gamelist.xml", s.handleGameGamelistExport)
	mux.HandleFunc("/api/games/curation", s.handleGameCuration)
	mux.HandleFunc("/api/games/curation/task", s.handleGameCurationTask)
	mux.HandleFunc("/api/games/curation/analyze", s.handleGameCurationAnalyze)
	mux.HandleFunc("/api/games/curation/checksums", s.handleGameCurationChecksums)
	mux.HandleFunc("/api/games/curation/rebuild", s.handleGameCurationRebuild)
	mux.HandleFunc("/api/games/curation/covers", s.handleGameCurationCovers)
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

func (s *Server) handleGameMetadataProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]any{"providers": s.service.GameMetadataProviders()})
}

func (s *Server) handleGameCatalogSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, s.service.GameCatalogSettings())
	case http.MethodPut:
		var settings domain.GameCatalogSettings
		if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := s.service.SaveGameCatalogSettings(settings); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, s.service.GameCatalogSettings())
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleGameCuration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Query().Get("summary") == "1" {
		summary, err := s.service.GameCurationSummary()
		writeJSONOrError(w, summary, err)
		return
	}
	page, err := s.service.ListGameCurationPage(domain.GameListOptions{
		Limit:       queryInt(r, "limit", 60, 200),
		Offset:      queryInt(r, "offset", 0, 0),
		Query:       r.URL.Query().Get("q"),
		Platform:    r.URL.Query().Get("platform"),
		CatalogRole: r.URL.Query().Get("state"),
		Sort:        r.URL.Query().Get("sort"),
	})
	writeJSONOrError(w, page, err)
}

func (s *Server) handleGameCurationTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, s.service.GameCatalogTaskStatus())
}

func (s *Server) handleGameCurationAnalyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	task, err := s.service.StartGameCatalogAnalysis()
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSONStatus(w, http.StatusAccepted, task)
}

func (s *Server) handleGameCurationChecksums(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Limit  int   `json:"limit"`
		GameID int64 `json:"gameId"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	task, err := s.service.StartGameChecksumBackfill(req.Limit, req.GameID)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSONStatus(w, http.StatusAccepted, task)
}

func (s *Server) handleGameCurationRebuild(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req domain.GameCompatibilityRebuildRequest
	if r.Body != nil && r.ContentLength != 0 {
		r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	task, err := s.service.StartGameCompatibilityRebuild(req)
	if err != nil {
		status := http.StatusBadRequest
		if task.Status == "running" {
			status = http.StatusConflict
		}
		writeError(w, status, err)
		return
	}
	writeJSONStatus(w, http.StatusAccepted, task)
}

func (s *Server) handleGameCurationCovers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		IncludeNetwork bool `json:"includeNetwork"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	task, err := s.service.StartGameCoverMatch(req.IncludeNetwork)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSONStatus(w, http.StatusAccepted, task)
}

func (s *Server) handleGameGamelistExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	data, err := s.service.ExportGameGamelistXML(domain.GameListOptions{
		Query:      r.URL.Query().Get("q"),
		Platform:   r.URL.Query().Get("platform"),
		ROMSetName: r.URL.Query().Get("romSetName"),
		Format:     r.URL.Query().Get("format"),
		BasePath:   r.URL.Query().Get("basePath"),
	})
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="gamelist.xml"`)
	_, _ = w.Write(data)
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
		ServiceName:    "FolioSpace Library",
		ServiceVersion: serviceVersion,
		APIVersion:     "v1",
		SupportedFormats: []string{
			"cbz", "zip", "epub", "pdf", "mp4", "m4v", "mov", "mkv", "avi", "webm",
			"nes", "sfc", "smc", "vb", "vboy", "gba", "gb", "gbc", "nds", "3ds", "cci", "cxi", "cia", "z64", "v64", "n64",
			"gdi", "cdi", "chd", "iso", "bin", "cue", "ccd", "toc", "m3u", "cso", "gcm", "rvz", "7z", "dosz", "exe", "com", "bat",
			"d88", "88d", "d98", "98d", "fdi", "xdf", "hdm", "dup", "2hd", "tfd", "nfd", "hd4", "hd5", "hd9", "fdd",
			"h01", "hdb", "ddb", "dd6", "dcp", "dcu", "flp", "img", "ima", "fim", "thd", "nhd", "hdi", "vhd", "slh", "hdn", "cmd",
			"py1",
		},
		Capabilities: clientCapabilities{
			ClientHome:            true,
			UnifiedManifest:       true,
			ProgressSync:          true,
			EPUBStreaming:         true,
			PDFStreaming:          true,
			PDFPageLayout:         true,
			PDFWebtoonLayout:      true,
			ComicWebtoonLayout:    true,
			WebtoonPositionSync:   true,
			CompactReader:         true,
			PageStreaming:         true,
			PageImageDownsample:   true,
			BookCatalog:           true,
			CollectionCatalog:     true,
			GameShelf:             true,
			GameCatalog:           true,
			GamePlatformCatalog:   true,
			VideoCatalog:          true,
			VideoHLS:              true,
			PrivateState:          true,
			Search:                true,
			Preferences:           true,
			Profiles:              true,
			BearerTokenAuth:       s.authEnabled(),
			SetupWizard:           true,
			ScannerJobEvents:      true,
			ScannerJobControl:     true,
			ScanSettings:          true,
			RecentScan:            true,
			GameSaveSync:          true,
			GamePlayStats:         true,
			GamePlayedCatalog:     true,
			GameMetadataProviders: true,
			GameLaunchResolver:    !s.options.DisableGameLaunchResolver,
			StableRuntimeIdentity: !s.options.DisableGameLaunchResolver,
			DOSArchiveLaunchV1:    true,
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
	limit := queryLimit(r, 12)
	var (
		continueReading []domain.Book
		recentBooks     []domain.Book
		favoriteBooks   []domain.Book
		wantBooks       []domain.Book
		gameShelf       []domain.GameAsset
		videoShelf      []domain.VideoAsset
		collections     []domain.Series
	)
	var err error
	if continueReading, err = s.service.ContinueReadingForProfile(profileID, limit); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if recentBooks, err = s.service.RecentBooksForProfile(profileID, limit); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if favoriteBooks, err = s.service.FavoriteBooksForProfile(profileID, limit); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if wantBooks, err = s.service.BooksByPrivateStatusForProfile(profileID, "want", limit); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if gameShelf, err = s.service.RecentGames(limit); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if videoShelf, err = s.service.RecentVideos(limit); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	includeCollections := !strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("includeCollections")), "false")
	if includeCollections {
		if collections, err = s.service.ListSeriesForProfileLimit(profileID, limit); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
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
	if tail == "file" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		s.streamClientBookFile(w, r, id, s.requestProfileID(r))
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

func (s *Server) streamClientBookFile(w http.ResponseWriter, r *http.Request, bookID int64, profileID int64) {
	book, err := s.service.BookForProfile(bookID, profileID)
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	path := strings.TrimSpace(book.FilePath)
	if path == "" {
		writeError(w, http.StatusNotFound, errors.New("book source file is unavailable"))
		return
	}
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, errors.New("book source file is missing"))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !info.Mode().IsRegular() {
		writeError(w, http.StatusNotFound, errors.New("book source is not a regular file"))
		return
	}
	name := filepath.Base(path)
	if disposition := mime.FormatMediaType("attachment", map[string]string{"filename": name}); disposition != "" {
		w.Header().Set("Content-Disposition", disposition)
	}
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, name, info.ModTime(), file)
}

func (s *Server) handleClientBooks(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeClient(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	page, err := s.service.ListBooksPageForProfile(domain.BookListOptions{
		Limit:     queryInt(r, "limit", 60, 200),
		Offset:    queryInt(r, "offset", 0, 0),
		Query:     r.URL.Query().Get("q"),
		Sort:      r.URL.Query().Get("sort"),
		Direction: r.URL.Query().Get("direction"),
		Format:    r.URL.Query().Get("format"),
	}, s.requestProfileID(r))
	writeJSONOrError(w, clientBookListPage(bookListPageWithThumbnails(page)), err)
}

func (s *Server) handleClientManualCollections(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeClient(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		collections, err := s.service.ListManualCollections()
		writeJSONOrError(w, collections, err)
	case http.MethodPost:
		var req domain.ManualCollection
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		collection, err := s.service.CreateManualCollection(req)
		writeJSONOrError(w, collection, err)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleClientManualCollectionAction(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeClient(w, r) {
		return
	}
	id, tail, ok := parseIDTail(r.URL.Path, "/api/client/manual-collections/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	if tail == "" {
		switch r.Method {
		case http.MethodGet:
			details, err := s.service.ManualCollectionDetails(id)
			writeJSONOrError(w, details, err)
		case http.MethodPut:
			var req domain.ManualCollection
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			collection, err := s.service.UpdateManualCollection(id, req)
			writeJSONOrError(w, collection, err)
		case http.MethodDelete:
			writeJSONOrError(w, map[string]bool{"ok": true}, s.service.DeleteManualCollection(id))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}
	if tail == "items" {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req domain.ManualCollectionItem
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		collection, err := s.service.AddManualCollectionItem(id, req)
		writeJSONOrError(w, collection, err)
		return
	}
	if strings.HasPrefix(tail, "items/") && r.Method == http.MethodDelete {
		parts := strings.Split(strings.TrimPrefix(tail, "items/"), "/")
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		assetID, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		collection, err := s.service.RemoveManualCollectionItem(id, parts[0], assetID)
		writeJSONOrError(w, collection, err)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) handleClientGameAction(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeClient(w, r) {
		return
	}
	if r.URL.Path == "/api/client/games/facets" {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		facets, err := s.service.ListGameFacets(domain.GameListOptions{
			Query:             r.URL.Query().Get("q"),
			Platform:          r.URL.Query().Get("platform"),
			ROMSetName:        r.URL.Query().Get("romSetName"),
			Format:            r.URL.Query().Get("format"),
			ClientVisibleOnly: true,
		})
		writeJSONOrError(w, facets, err)
		return
	}
	id, tail, ok := parseIDTail(r.URL.Path, "/api/client/games/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	if tail == "resolve" {
		if s.options.DisableGameLaunchResolver {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		var req domain.GameLaunchResolveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := service.ValidateGameLaunchResolveRequest(req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		resolution, err := s.service.ResolveGameLaunchProfile(id, req)
		if err != nil {
			var resolveErr *service.GameLaunchResolveError
			if errors.As(err, &resolveErr) {
				logGameLaunchDecision(id, req, "", 0, resolveErr.Code)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(resolveErr)
				return
			}
			var unavailable *service.RuntimeProfileNotAvailableError
			if errors.As(err, &unavailable) {
				logGameLaunchDecision(id, req, "", 0, "launch-profile-missing")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"code": "launch-profile-missing", "message": unavailable.Error(),
				})
				return
			}
			writeJSONOrError(w, nil, err)
			return
		}
		logGameLaunchDecision(id, req, resolution.LaunchProfileID, resolution.ProfileRevision, "")
		w.Header().Set("Cache-Control", "private, no-store")
		writeJSON(w, clientGameLaunchResolution(resolution))
		return
	}
	if tail == "manifest" && r.Method == http.MethodGet {
		game, err := s.service.Game(id)
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		files, err := s.service.GameFiles(id)
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		var dosLaunch *domain.DOSLaunch
		if game.Platform == "dos" {
			launch, launchErr := s.service.DOSLaunch(id)
			if launchErr != nil && !errors.Is(launchErr, sql.ErrNoRows) {
				writeJSONOrError(w, nil, launchErr)
				return
			}
			if launchErr == nil {
				dosLaunch = &launch
			}
		}
		launchDependencies, err := s.service.LegacyGameLaunchDependencies(id)
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
		manifest := clientGameManifest(game, files, dosLaunch)
		writeJSON(w, appendLegacyLaunchDependencies(manifest, launchDependencies))
		return
	}
	if tail == "details" && r.Method == http.MethodGet {
		details, err := s.service.GameDetails(id)
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		writeJSON(w, clientGameDetails(details))
		return
	}
	if tail == "metadata" && r.Method == http.MethodGet {
		details, err := s.service.GameDetails(id)
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		writeJSON(w, clientGameMetadata(details))
		return
	}
	if tail == "private-state" {
		if r.Method != http.MethodPut {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req domain.GamePrivateState
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		game, err := s.service.UpdateGamePrivateStateForProfile(id, s.requestProfileID(r), req)
		writeJSONOrError(w, clientGameItem(game), err)
		return
	}
	if tail == "play-stats" {
		switch r.Method {
		case http.MethodGet:
			stats, err := s.service.GamePlayStatsForProfile(id, s.requestProfileID(r))
			writeJSONOrError(w, stats, err)
		case http.MethodPut:
			var req domain.GamePlaySessionReport
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			if strings.TrimSpace(req.SessionID) == "" || len(strings.TrimSpace(req.SessionID)) > 128 || req.ElapsedSeconds < 0 || req.ElapsedSeconds > 365*24*60*60 {
				writeError(w, http.StatusBadRequest, errors.New("invalid game play session report"))
				return
			}
			result, err := s.service.ReportGamePlaySessionForProfile(id, s.requestProfileID(r), req)
			writeJSONOrError(w, result, err)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}
	if tail == "save-sync/archive" {
		switch r.Method {
		case http.MethodGet:
			stream, err := s.service.OpenGameSaveSyncArchive(id, s.requestProfileID(r))
			if err != nil {
				writeJSONOrError(w, nil, err)
				return
			}
			defer stream.Body.Close()
			w.Header().Set("Content-Type", stream.ContentType)
			_, _ = io.Copy(w, stream.Body)
		case http.MethodPut:
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<20))
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			if len(body) == 0 {
				writeError(w, http.StatusBadRequest, errors.New("save sync archive is empty"))
				return
			}
			if err := s.service.SaveGameSaveSyncArchive(id, s.requestProfileID(r), body); err != nil {
				writeJSONOrError(w, nil, err)
				return
			}
			writeJSON(w, map[string]bool{"ok": true})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}
	if tail == "cover" && r.Method == http.MethodGet {
		s.streamGameCover(w, id)
		return
	}
	if tail == "file" && r.Method == http.MethodGet {
		game, err := s.service.Game(id)
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		stream, err := s.service.OpenGameFile(id)
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		defer stream.Body.Close()
		size := int64(-1)
		name := clientGameFileName(game)
		if game.Platform == "dos" || clientGameStreamsInnerArchiveFile(game) {
			size = game.Size
			if clientGameStreamsInnerArchiveFile(game) {
				if files, filesErr := s.service.GameFiles(id); filesErr == nil && len(files) > 0 {
					size = files[0].Size
					name = files[0].Name
				}
			}
		}
		serveGameStream(w, r, stream, size, name)
		return
	}
	if strings.HasPrefix(tail, "files/") && r.Method == http.MethodGet {
		position, err := strconv.Atoi(strings.TrimPrefix(tail, "files/"))
		if err != nil || position < 0 {
			writeError(w, http.StatusBadRequest, errors.New("invalid game file position"))
			return
		}
		stream, file, err := s.service.OpenGameFilePart(id, position)
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		defer stream.Body.Close()
		serveGameStream(w, r, stream, file.Size, filepathBase(file.Name))
		return
	}
	http.NotFound(w, r)
}

func clientGameStreamsInnerArchiveFile(game domain.GameAsset) bool {
	if !strings.EqualFold(filepath.Ext(game.FilePath), ".zip") {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(game.Platform)) {
	case "3ds", "n64", "pc98", "virtualboy", "snes", "nes", "gb", "gbc", "gba", "nds", "md":
		return true
	default:
		return false
	}
}

func serveGameStream(w http.ResponseWriter, r *http.Request, stream service.PageStream, size int64, name string) {
	w.Header().Set("Content-Type", stream.ContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, strings.ReplaceAll(name, `"`, "")))
	if seeker, ok := stream.Body.(io.ReadSeeker); ok {
		http.ServeContent(w, r, name, time.Time{}, seeker)
		return
	}
	w.Header().Set("Accept-Ranges", "bytes")
	if value := strings.TrimSpace(r.Header.Get("Range")); value != "" && size >= 0 {
		start, end, ok := parseSingleByteRange(value, size)
		if !ok {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if start > 0 {
			if _, err := io.CopyN(io.Discard, stream.Body, start); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
		}
		length := end - start + 1
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
		w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.CopyN(w, stream.Body, length)
		return
	}
	if size >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	}
	_, _ = io.Copy(w, stream.Body)
}

func parseSingleByteRange(value string, size int64) (int64, int64, bool) {
	if size <= 0 || !strings.HasPrefix(value, "bytes=") || strings.Contains(value, ",") {
		return 0, 0, false
	}
	parts := strings.SplitN(strings.TrimSpace(strings.TrimPrefix(value, "bytes=")), "-", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	if parts[0] == "" {
		suffix, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || suffix <= 0 {
			return 0, 0, false
		}
		if suffix > size {
			suffix = size
		}
		return size - suffix, size - 1, true
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, false
	}
	end := size - 1
	if parts[1] != "" {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil || end < start {
			return 0, 0, false
		}
		if end >= size {
			end = size - 1
		}
	}
	return start, end, true
}

func logGameLaunchDecision(gameID int64, req domain.GameLaunchResolveRequest, profileID string, revision int, rejection string) {
	runtimes := make([]string, 0, len(req.Runtimes))
	for _, runtime := range req.Runtimes {
		fingerprint := strings.ToLower(strings.TrimSpace(runtime.CoreSHA256))
		if len(fingerprint) > 12 {
			fingerprint = fingerprint[:12]
		}
		runtimes = append(runtimes, fmt.Sprintf("%s/%s/%s/%s/%s", runtime.ID, runtime.Version, runtime.ContentSet, runtime.CoreID, fingerprint))
	}
	log.Printf("game launch resolve game=%d client=%s/%s/%s/%s runtimes=%q profile=%q revision=%d rejection=%q",
		gameID, req.Client.Name, req.Client.Version, req.Client.Platform, req.Client.Architecture,
		strings.Join(runtimes, ","), profileID, revision, rejection)
}

func (s *Server) handleClientGames(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeClient(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	page, err := s.service.ListGamesPageForProfile(domain.GameListOptions{
		Limit:             queryInt(r, "limit", 50, 200),
		Offset:            queryInt(r, "offset", 0, 0),
		Query:             r.URL.Query().Get("q"),
		Platform:          r.URL.Query().Get("platform"),
		ROMSetName:        r.URL.Query().Get("romSetName"),
		Format:            r.URL.Query().Get("format"),
		Sort:              r.URL.Query().Get("sort"),
		ClientVisibleOnly: true,
	}, s.requestProfileID(r))
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

func (s *Server) handleClientGamePlatforms(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeClient(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	platforms, err := s.service.ListGamePlatforms()
	writeJSONOrError(w, platforms, err)
}

func (s *Server) handleClientPlayedGames(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeClient(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	page, err := s.service.ListPlayedGamesForProfile(domain.PlayedGameListOptions{
		Limit:     queryInt(r, "limit", 50, 200),
		Offset:    queryInt(r, "offset", 0, 0),
		Query:     r.URL.Query().Get("q"),
		Platform:  r.URL.Query().Get("platform"),
		Sort:      r.URL.Query().Get("sort"),
		Direction: r.URL.Query().Get("direction"),
	}, s.requestProfileID(r))
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	items := make([]clientPlayedGame, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, clientPlayedGameItem(item))
	}
	writeJSON(w, clientPlayedGameListResponse{
		Items: items, Total: page.Total, Limit: page.Limit, Offset: page.Offset, HasMore: page.HasMore,
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
	if tail == "" && r.Method == http.MethodGet {
		details, err := s.service.GameDetails(id)
		writeJSONOrError(w, details, err)
		return
	}
	if tail == "cover" && r.Method == http.MethodGet {
		s.streamGameCover(w, id)
		return
	}
	if tail == "metadata/refresh" && r.Method == http.MethodPost {
		if !s.authorizeAPI(w, r) {
			return
		}
		result, err := s.service.RefreshGameMetadata(id)
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		writeJSON(w, gameMetadataActionResponseFromResult(result))
		return
	}
	if tail == "metadata/select-match" && r.Method == http.MethodPost {
		if !s.authorizeAPI(w, r) {
			return
		}
		var req gameMetadataSelectMatchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
			writeJSONOrError(w, nil, err)
			return
		}
		result, err := s.service.SelectGameMetadataMatch(id, req.Source, req.SourceID)
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		writeJSON(w, gameMetadataActionResponseFromResult(result))
		return
	}
	if tail == "metadata" && r.Method == http.MethodPut {
		var req domain.GameMetadata
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		details, err := s.service.UpdateGameMetadata(id, req)
		writeJSONOrError(w, details, err)
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
	if !s.authEnabled() {
		return true
	}
	if token := s.requestToken(r); token != "" {
		if r.URL.Query().Get("access_token") != "" {
			s.setAuthCookie(w, token)
		}
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
		FileURL:           clientBookDownloadURL(book.ID),
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
			PageKey:    page.PageKey,
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
			Name            string   `json:"name"`
			RootPath        string   `json:"rootPath"`
			AssetType       string   `json:"assetType"`
			ExcludePatterns []string `json:"excludePatterns"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		lib, err := s.service.CreateLibraryWithOptions(req.Name, req.RootPath, req.AssetType, req.ExcludePatterns)
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
	if tail == "" && r.Method == http.MethodPut {
		var req struct {
			ExcludePatterns []string `json:"excludePatterns"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		lib, err := s.service.UpdateLibraryExcludePatterns(id, req.ExcludePatterns)
		writeJSONOrError(w, lib, err)
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
	var req service.ScanRequest
	if r.Body != nil {
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&req); err != nil && err != io.EOF {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	var job domain.ScanJob
	var err error
	if strings.EqualFold(strings.TrimSpace(req.Mode), "recent") || req.RecentLimit > 0 {
		job, err = s.service.ScanLibraryRecent(id, req.Path, req.RecentLimit)
	} else if strings.TrimSpace(req.Path) == "" {
		job, err = s.service.ScanLibrary(id)
	} else {
		job, err = s.service.ScanLibraryPath(id, req.Path)
	}
	writeJSONOrError(w, job, err)
}

func (s *Server) handleSeries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if hasCollectionListQuery(r) {
		page, err := s.service.ListSeriesPageForProfile(s.requestProfileID(r), domain.CollectionListOptions{
			Limit:       queryInt(r, "limit", 60, 200),
			Offset:      queryInt(r, "offset", 0, 0),
			PrimaryType: r.URL.Query().Get("primaryType"),
			Sort:        r.URL.Query().Get("sort"),
			Direction:   r.URL.Query().Get("direction"),
			Query:       r.URL.Query().Get("q"),
		})
		writeJSONOrError(w, clientCollectionListPage(page), err)
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
			SeriesID:  id,
			Limit:     queryInt(r, "limit", 60, 200),
			Offset:    queryInt(r, "offset", 0, 0),
			Query:     r.URL.Query().Get("q"),
			Sort:      r.URL.Query().Get("sort"),
			Direction: r.URL.Query().Get("direction"),
			Format:    r.URL.Query().Get("format"),
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
	if strings.HasPrefix(tail, "epub/spine/") && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		index, err := strconv.Atoi(strings.TrimPrefix(tail, "epub/spine/"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		s.streamEPUBSpine(w, r, id, index)
		return
	}
	if strings.HasPrefix(tail, "epub/resources/") && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		s.streamEPUBResource(w, r, id, strings.TrimPrefix(tail, "epub/resources/"))
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
	if tail == "reading-position" && r.Method == http.MethodGet {
		positions, err := s.service.ReadingPositionsForProfile(id, s.requestProfileID(r))
		writeJSONOrError(w, clientReadingPositions{BookID: id, Positions: positions}, err)
		return
	}
	if tail == "reading-position/webtoon" && r.Method == http.MethodPut {
		var req domain.ReadingPosition
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		position, err := s.service.SaveWebtoonReadingPositionForProfile(id, s.requestProfileID(r), req)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, position)
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
	return query.Has("limit") || query.Has("offset") || query.Has("q") || query.Has("sort") || query.Has("direction") || query.Has("format")
}

func hasCollectionListQuery(r *http.Request) bool {
	query := r.URL.Query()
	return query.Has("limit") || query.Has("offset") || query.Has("q") || query.Has("sort") || query.Has("direction") || query.Has("primaryType")
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
	select {
	case s.thumbnailRequests <- struct{}{}:
		defer func() { <-s.thumbnailRequests }()
	default:
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusTooManyRequests, fmt.Errorf("thumbnail request limit reached"))
		return
	}
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
	var status domain.ThumbnailQueueStatus
	var err error
	if action == "cleanup-orphans" || r.URL.Query().Get("detail") == "full" {
		status, err = s.service.ThumbnailWorkerStatus()
	} else {
		status, err = s.service.ThumbnailWorkerQueueStatus()
	}
	writeJSONOrError(w, status, err)
}

func (s *Server) streamEPUBResource(w http.ResponseWriter, r *http.Request, bookID int64, resourcePath string) {
	page, err := s.service.OpenEPUBResource(bookID, resourcePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer page.Body.Close()

	w.Header().Set("Content-Type", page.ContentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", epubResourceCSP())
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, page.Body)
}

func (s *Server) streamEPUBSpine(w http.ResponseWriter, r *http.Request, bookID int64, index int) {
	manifest, err := s.service.EPUBManifest(bookID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if index < 0 || index >= len(manifest.Spine) {
		writeError(w, http.StatusNotFound, fmt.Errorf("epub spine index %d out of range", index))
		return
	}
	href := manifest.Spine[index].Href
	page, err := s.service.OpenEPUBResource(bookID, href)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer page.Body.Close()

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", epubResourceCSP())
	if r.Method == http.MethodHead {
		if strings.Contains(page.ContentType, "html") || strings.Contains(page.ContentType, "xhtml") {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		} else {
			w.Header().Set("Content-Type", page.ContentType)
		}
		return
	}

	if !strings.Contains(page.ContentType, "html") && !strings.Contains(page.ContentType, "xhtml") {
		w.Header().Set("Content-Type", page.ContentType)
		_, _ = io.Copy(w, page.Body)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	body, err := io.ReadAll(page.Body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	_, _ = w.Write(injectEPUBBase(stripXMLDeclaration(body), epubResourceBaseURL(bookID, href)))
}

func epubResourceCSP() string {
	return "default-src 'none'; base-uri 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; font-src 'self' data:; media-src 'self' data: blob:"
}

func epubResourceBaseURL(bookID int64, href string) string {
	dir := ""
	if index := strings.LastIndex(href, "/"); index >= 0 {
		dir = href[:index+1]
	}
	return fmt.Sprintf("/api/books/%d/epub/resources/%s", bookID, encodeEPUBResourcePath(dir))
}

func encodeEPUBResourcePath(path string) string {
	if path == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if part != "" {
			parts[i] = url.PathEscape(part)
		}
	}
	return strings.Join(parts, "/")
}

func injectEPUBBase(body []byte, baseURL string) []byte {
	base := []byte(`<base href="` + html.EscapeString(baseURL) + `">`)
	lower := strings.ToLower(string(body))
	if index := strings.Index(lower, "<head"); index >= 0 {
		if end := strings.Index(lower[index:], ">"); end >= 0 {
			insertAt := index + end + 1
			out := make([]byte, 0, len(body)+len(base))
			out = append(out, body[:insertAt]...)
			out = append(out, base...)
			out = append(out, body[insertAt:]...)
			return out
		}
	}
	out := make([]byte, 0, len(body)+len(base)+13)
	out = append(out, []byte("<head>")...)
	out = append(out, base...)
	out = append(out, []byte("</head>")...)
	out = append(out, body...)
	return out
}

func stripXMLDeclaration(body []byte) []byte {
	trimmed := bytes.TrimLeft(body, "\ufeff \t\r\n")
	if !bytes.HasPrefix(trimmed, []byte("<?xml")) {
		return body
	}
	if end := bytes.Index(trimmed, []byte("?>")); end >= 0 {
		return bytes.TrimLeft(trimmed[end+2:], " \t\r\n")
	}
	return body
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
		if r.URL.Path == "/" || strings.HasSuffix(r.URL.Path, ".html") {
			w.Header().Set("Cache-Control", "no-store")
		}
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

func writeJSONStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
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
	ClientHome            bool `json:"clientHome"`
	UnifiedManifest       bool `json:"unifiedManifest"`
	ProgressSync          bool `json:"progressSync"`
	EPUBStreaming         bool `json:"epubStreaming"`
	PDFStreaming          bool `json:"pdfStreaming"`
	PDFPageLayout         bool `json:"pdfPageLayout"`
	PDFWebtoonLayout      bool `json:"pdfWebtoonLayout"`
	ComicWebtoonLayout    bool `json:"comicWebtoonLayout"`
	WebtoonPositionSync   bool `json:"webtoonPositionSync"`
	CompactReader         bool `json:"compactReader"`
	PageStreaming         bool `json:"pageStreaming"`
	PageImageDownsample   bool `json:"pageImageDownsample"`
	BookCatalog           bool `json:"bookCatalog"`
	CollectionCatalog     bool `json:"collectionCatalog"`
	GameShelf             bool `json:"gameShelf"`
	GameCatalog           bool `json:"gameCatalog"`
	GamePlatformCatalog   bool `json:"gamePlatformCatalog"`
	VideoCatalog          bool `json:"videoCatalog"`
	VideoHLS              bool `json:"videoHls"`
	PrivateState          bool `json:"privateState"`
	Search                bool `json:"search"`
	Preferences           bool `json:"preferences"`
	Profiles              bool `json:"profiles"`
	BearerTokenAuth       bool `json:"bearerTokenAuth"`
	SetupWizard           bool `json:"setupWizard"`
	ScannerJobEvents      bool `json:"scannerJobEvents"`
	ScannerJobControl     bool `json:"scannerJobControl"`
	ScanSettings          bool `json:"scanSettings"`
	RecentScan            bool `json:"recentScan"`
	GameSaveSync          bool `json:"gameSaveSync"`
	GamePlayStats         bool `json:"gamePlayStats"`
	GamePlayedCatalog     bool `json:"gamePlayedCatalog"`
	GameMetadataProviders bool `json:"gameMetadataProviders"`
	GameLaunchResolver    bool `json:"gameLaunchResolver"`
	StableRuntimeIdentity bool `json:"stableRuntimeIdentityV1"`
	DOSArchiveLaunchV1    bool `json:"dosArchiveLaunchV1"`
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
	AddedAt         string `json:"addedAt"`
	ThumbnailStatus string `json:"thumbnailStatus,omitempty"`
	ThumbnailURL    string `json:"thumbnailUrl,omitempty"`
	Favorite        bool   `json:"favorite"`
	Liked           bool   `json:"liked"`
}

type clientCollectionListPageResponse struct {
	Items   []clientCollection `json:"items"`
	Total   int64              `json:"total"`
	Limit   int                `json:"limit"`
	Offset  int                `json:"offset"`
	HasMore bool               `json:"hasMore"`
}

type clientBook struct {
	ID                   int64    `json:"id"`
	CollectionID         int64    `json:"collectionId"`
	SeriesID             int64    `json:"seriesId"`
	CollectionTitle      string   `json:"collectionTitle,omitempty"`
	Title                string   `json:"title"`
	Creator              string   `json:"creator,omitempty"`
	Description          string   `json:"description,omitempty"`
	BookType             string   `json:"bookType"`
	Format               string   `json:"format"`
	PageCount            int      `json:"pageCount"`
	CoverStatus          string   `json:"coverStatus"`
	CoverURL             string   `json:"coverUrl"`
	ThumbnailStatus      string   `json:"thumbnailStatus"`
	ThumbnailURL         string   `json:"thumbnailUrl"`
	ManifestURL          string   `json:"manifestUrl"`
	DownloadURL          string   `json:"downloadUrl"`
	Analyzed             bool     `json:"analyzed"`
	AddedAt              string   `json:"addedAt"`
	UpdatedAt            string   `json:"updatedAt"`
	CurrentPage          int      `json:"currentPage"`
	ProgressFraction     float64  `json:"progressFraction"`
	LastReadAt           string   `json:"lastReadAt"`
	PrivateStatus        string   `json:"privateStatus"`
	Favorite             bool     `json:"favorite"`
	Rating               int      `json:"rating"`
	Tags                 []string `json:"tags"`
	Summary              string   `json:"summary"`
	ContentHash          *string  `json:"contentHash"`
	ContentHashAlgorithm *string  `json:"contentHashAlgorithm"`
	FileSize             *int64   `json:"fileSize"`
	ContentRevision      *string  `json:"contentRevision"`
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
	FileURL           string              `json:"fileUrl"`
	Progress          clientProgress      `json:"progress"`
	ReaderModes       []string            `json:"readerModes"`
	DefaultReaderMode string              `json:"defaultReaderMode"`
	Pages             []clientPageRef     `json:"pages,omitempty"`
	EPUB              *clientEPUBOpenInfo `json:"epub,omitempty"`
}

type clientGame struct {
	ID               int64  `json:"id"`
	AssetType        string `json:"assetType"`
	Title            string `json:"title"`
	Platform         string `json:"platform"`
	ROMSetName       string `json:"romSetName,omitempty"`
	ParentROMSetName string `json:"parentRomSetName,omitempty"`
	Region           string `json:"region,omitempty"`
	Format           string `json:"format"`
	ContentMode      string `json:"contentMode,omitempty"`
	FileName         string `json:"fileName,omitempty"`
	Size             int64  `json:"size"`
	CRC32            string `json:"crc32"`
	SHA1             string `json:"sha1"`
	EmulatorHint     string `json:"emulatorHint"`
	InputProfile     string `json:"inputProfile,omitempty"`
	Compatibility    string `json:"compatibility"`
	CatalogRole      string `json:"catalogRole,omitempty"`
	CoverURL         string `json:"coverUrl,omitempty"`
	ManifestURL      string `json:"manifestUrl"`
	DownloadURL      string `json:"downloadUrl,omitempty"`
	Favorite         bool   `json:"favorite"`
	Liked            bool   `json:"liked"`
}

type clientGameManifestResponse struct {
	Game        clientGame       `json:"game"`
	FileURL     string           `json:"fileUrl"`
	EntryFile   *string          `json:"entryFile"`
	Files       []clientGameFile `json:"files,omitempty"`
	DOSLaunch   *clientDOSLaunch `json:"dosLaunch,omitempty"`
	ContentMode string           `json:"contentMode,omitempty"`
	UpdatedAt   string           `json:"updatedAt,omitempty"`
}

type clientGameLaunchResolutionResponse struct {
	LaunchProfileID string                       `json:"launchProfileId"`
	ProfileRevision int                          `json:"profileRevision"`
	Runtime         domain.GameRuntimeDescriptor `json:"runtime"`
	Manifest        clientGameManifestResponse   `json:"manifest"`
}

type clientGameFile struct {
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	Role      string `json:"role"`
	Label     string `json:"label,omitempty"`
	DiskIndex *int   `json:"diskIndex,omitempty"`
	DriveHint string `json:"driveHint,omitempty"`
	URL       string `json:"url"`
	Checksum  string `json:"checksum,omitempty"`
}

type clientDOSLaunch struct {
	EntrySource      string                      `json:"entrySource"`
	InstallDirectory *string                     `json:"installDirectory"`
	WorkingDirectory *string                     `json:"workingDirectory"`
	DOSBoxConfig     *string                     `json:"dosboxConfig"`
	Arguments        []string                    `json:"arguments"`
	Candidates       []domain.DOSLaunchCandidate `json:"candidates"`
	KeymapHints      map[string]string           `json:"keymapHints,omitempty"`
}

type clientGameMetadataResponse struct {
	MetadataStatus string                     `json:"metadataStatus"`
	Metadata       domain.GameMetadata        `json:"metadata"`
	Sources        []clientGameMetadataSource `json:"sources"`
	Artwork        []clientGameArtwork        `json:"artwork"`
}

type clientGameDetailsResponse struct {
	Game clientGame `json:"game"`
	clientGameMetadataResponse
}

type clientGameMetadataSource struct {
	ID         int64   `json:"id"`
	Source     string  `json:"source"`
	SourceID   string  `json:"sourceId"`
	MatchedBy  string  `json:"matchedBy"`
	Confidence float64 `json:"confidence"`
	UpdatedAt  string  `json:"updatedAt"`
}

type clientGameArtwork struct {
	ID         int64   `json:"id"`
	Source     string  `json:"source"`
	Kind       string  `json:"kind"`
	URL        string  `json:"url,omitempty"`
	Width      int     `json:"width"`
	Height     int     `json:"height"`
	Selected   bool    `json:"selected"`
	Confidence float64 `json:"confidence"`
	UpdatedAt  string  `json:"updatedAt"`
}

type gameMetadataActionResponse struct {
	GameID         int64                               `json:"gameId"`
	Action         string                              `json:"action"`
	Status         string                              `json:"status"`
	Message        string                              `json:"message"`
	MetadataStatus string                              `json:"metadataStatus"`
	Sources        []clientGameMetadataSource          `json:"sources"`
	Providers      []domain.GameMetadataProviderStatus `json:"providers"`
}

type gameMetadataSelectMatchRequest struct {
	Source   string `json:"source"`
	SourceID string `json:"sourceId"`
}

type clientGameListResponse struct {
	Items   []clientGame `json:"items"`
	Total   int64        `json:"total"`
	Limit   int          `json:"limit"`
	Offset  int          `json:"offset"`
	HasMore bool         `json:"hasMore"`
}

type clientPlayedGame struct {
	GameID           int64      `json:"gameId"`
	Title            string     `json:"title"`
	Platform         string     `json:"platform"`
	ROMSetName       string     `json:"romSetName,omitempty"`
	Format           string     `json:"format"`
	CRC32            string     `json:"crc32,omitempty"`
	SHA1             string     `json:"sha1,omitempty"`
	EmulatorHint     string     `json:"emulatorHint,omitempty"`
	CoverURL         string     `json:"coverUrl,omitempty"`
	ManifestURL      string     `json:"manifestUrl"`
	FirstPlayedAt    *time.Time `json:"firstPlayedAt"`
	LastPlayedAt     *time.Time `json:"lastPlayedAt"`
	TotalPlaySeconds int64      `json:"totalPlaySeconds"`
	LaunchCount      int64      `json:"launchCount"`
}

type clientPlayedGameListResponse struct {
	Items   []clientPlayedGame `json:"items"`
	Total   int64              `json:"total"`
	Limit   int                `json:"limit"`
	Offset  int                `json:"offset"`
	HasMore bool               `json:"hasMore"`
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

type clientReadingPositions struct {
	BookID    int64                             `json:"bookId"`
	Positions map[string]domain.ReadingPosition `json:"positions"`
}

type clientPageRef struct {
	Index      int    `json:"index"`
	Name       string `json:"name"`
	PageKey    string `json:"pageKey,omitempty"`
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
			AddedAt:        collection.AddedAt.Format(time.RFC3339),
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

func clientCollectionListPage(page domain.CollectionListPage) clientCollectionListPageResponse {
	return clientCollectionListPageResponse{
		Items:   clientCollections(page.Items),
		Total:   page.Total,
		Limit:   page.Limit,
		Offset:  page.Offset,
		HasMore: page.HasMore,
	}
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

func clientPlayedGameItem(item domain.PlayedGame) clientPlayedGame {
	game := clientGameItem(item.Game)
	return clientPlayedGame{
		GameID: item.Game.ID, Title: game.Title, Platform: game.Platform, ROMSetName: game.ROMSetName,
		Format: game.Format, CRC32: game.CRC32, SHA1: game.SHA1, EmulatorHint: game.EmulatorHint,
		CoverURL: game.CoverURL, ManifestURL: game.ManifestURL, FirstPlayedAt: item.Stats.FirstPlayedAt,
		LastPlayedAt: item.Stats.LastPlayedAt, TotalPlaySeconds: item.Stats.TotalPlaySeconds,
		LaunchCount: item.Stats.LaunchCount,
	}
}

func clientGameItem(game domain.GameAsset) clientGame {
	inputProfile := ""
	if strings.EqualFold(game.Platform, "virtualboy") || strings.EqualFold(game.Platform, "nds") || strings.EqualFold(game.Platform, "3ds") || strings.EqualFold(game.Platform, "3do") || strings.EqualFold(game.Platform, "n64") || strings.EqualFold(game.Platform, "pc98") || strings.EqualFold(game.Platform, "dos") || strings.EqualFold(game.Platform, "psp") || strings.EqualFold(game.Platform, "ngc") || strings.EqualFold(game.Platform, "ps2") || strings.EqualFold(game.Platform, "konami-python1") {
		inputProfile = "standard"
	} else if (strings.EqualFold(game.Platform, "model2") || strings.EqualFold(game.Platform, "naomi2")) && !strings.EqualFold(game.CatalogRole, "dependency") {
		inputProfile = "operatorArcade"
	} else if strings.EqualFold(strings.TrimSpace(game.Title), "srmp7") || pathHasSegment(game.RelPath, "mahjong") {
		inputProfile = "mahjong"
	}
	return clientGame{
		ID:               game.ID,
		AssetType:        "game",
		Title:            game.Title,
		Platform:         game.Platform,
		ROMSetName:       game.ROMSetName,
		ParentROMSetName: launchcatalog.ParentROMSetName(game),
		Region:           game.Region,
		Format:           game.Format,
		ContentMode:      clientGameContentMode(game),
		FileName:         clientGameFileName(game),
		Size:             game.Size,
		CRC32:            game.CRC32,
		SHA1:             game.SHA1,
		EmulatorHint:     game.EmulatorHint,
		InputProfile:     inputProfile,
		Compatibility:    game.Compatibility,
		CatalogRole:      game.CatalogRole,
		CoverURL:         gameCoverURL(game.ID, game.Platform),
		ManifestURL:      fmt.Sprintf("/api/client/games/%d/manifest", game.ID),
		DownloadURL:      fmt.Sprintf("/api/client/games/%d/file", game.ID),
		Favorite:         game.Favorite,
		Liked:            game.Liked,
	}
}

func clientGameFileName(game domain.GameAsset) string {
	name := filepathBase(game.RelPath)
	if !strings.EqualFold(game.Platform, "n64") {
		return name
	}
	format := strings.ToLower(strings.TrimSpace(game.Format))
	if format != "z64" && format != "v64" && format != "n64" {
		return name
	}
	ext := ""
	if dot := strings.LastIndex(name, "."); dot >= 0 {
		ext = name[dot:]
	}
	return strings.TrimSuffix(name, ext) + "." + format
}

func clientGameContentMode(game domain.GameAsset) string {
	if !strings.EqualFold(strings.TrimSpace(game.Platform), "3ds") {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(game.Format), "cia") {
		return "install"
	}
	return "launch"
}

func pathHasSegment(path string, segment string) bool {
	for _, part := range strings.Split(strings.ReplaceAll(path, `\`, "/"), "/") {
		if strings.EqualFold(strings.TrimSpace(part), segment) {
			return true
		}
	}
	return false
}

func gameCoverURL(gameID int64, platform string) string {
	return fmt.Sprintf("/api/games/%d/cover?v=game-cover-refresh-20260714", gameID)
}

func clientGameManifest(game domain.GameAsset, files []domain.GameFile, dosLaunch *domain.DOSLaunch) clientGameManifestResponse {
	manifest := clientGameManifestResponse{
		Game:        clientGameItem(game),
		FileURL:     fmt.Sprintf("/api/client/games/%d/file", game.ID),
		Files:       make([]clientGameFile, 0, len(files)),
		ContentMode: clientGameContentMode(game),
	}
	if !game.UpdatedAt.IsZero() {
		manifest.UpdatedAt = game.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	diskIndex := 0
	for _, file := range files {
		clientFile := clientGameFile{
			Name: file.Name, Size: file.Size, Role: file.Role,
			URL: fmt.Sprintf("/api/client/games/%d/files/%d", game.ID, file.Position),
		}
		if sha1 := strings.ToLower(strings.TrimSpace(file.SHA1)); sha1 != "" {
			clientFile.Checksum = "sha1:" + sha1
		} else if game.Platform == "dos" && file.Position == 0 {
			clientFile.Checksum = game.SHA1
		}
		if game.Platform == "pc98" && file.Role != "font" && isPC98FloppyManifestFile(file.Name) {
			clientFile.Role = "disk"
			if file.Role == "entry" {
				clientFile.Role = "entry"
			}
			if diskIndex == 0 {
				clientFile.DriveHint = "FDD1"
			}
			index := diskIndex
			clientFile.DiskIndex = &index
			clientFile.Label = fmt.Sprintf("Disk %d", diskIndex+1)
			diskIndex++
		}
		manifest.Files = append(manifest.Files, clientFile)
		if file.Role == "entry" && game.Platform != "dos" {
			entry := file.Name
			manifest.EntryFile = &entry
		}
	}
	if dosLaunch != nil {
		if dosLaunch.EntryFile != "" {
			entry := dosLaunch.EntryFile
			manifest.EntryFile = &entry
		}
		manifest.DOSLaunch = &clientDOSLaunch{
			EntrySource: dosLaunch.EntrySource, InstallDirectory: nullableString(dosLaunch.InstallDirectory),
			WorkingDirectory: nullableString(dosLaunch.WorkingDirectory),
			DOSBoxConfig:     nullableString(dosLaunch.DOSBoxConfig), Arguments: dosLaunch.Arguments,
			Candidates: dosLaunch.Candidates, KeymapHints: dosLaunch.KeymapHints,
		}
	}
	return manifest
}

func appendLegacyLaunchDependencies(manifest clientGameManifestResponse, dependencies []domain.GameLaunchResolvedFile) clientGameManifestResponse {
	knownNames := make(map[string]struct{}, len(manifest.Files)+len(dependencies))
	for _, file := range manifest.Files {
		knownNames[strings.ToLower(strings.TrimSpace(file.Name))] = struct{}{}
	}
	for _, dependency := range dependencies {
		if !strings.EqualFold(strings.TrimSpace(dependency.Role), "dependency") || dependency.SourceGameID <= 0 {
			continue
		}
		nameKey := strings.ToLower(strings.TrimSpace(dependency.Name))
		if nameKey == "" {
			continue
		}
		if _, exists := knownNames[nameKey]; exists {
			continue
		}
		file := clientGameFile{
			Name: dependency.Name, Size: dependency.Size, Role: "dependency",
			URL: fmt.Sprintf("/api/client/games/%d/file", dependency.SourceGameID),
		}
		if sha1 := strings.TrimSpace(dependency.SHA1); sha1 != "" {
			file.Checksum = "sha1:" + sha1
		}
		manifest.Files = append(manifest.Files, file)
		knownNames[nameKey] = struct{}{}
	}
	return manifest
}

func clientGameLaunchResolution(resolution domain.GameLaunchResolution) clientGameLaunchResolutionResponse {
	game := clientGameItem(resolution.Game)
	entry := domain.GameLaunchResolvedFile{}
	if len(resolution.Files) > 0 {
		entry = resolution.Files[0]
		for _, file := range resolution.Files {
			if strings.EqualFold(strings.TrimSpace(file.Role), "entry") {
				entry = file
				break
			}
		}
	}
	if resolution.DOSLaunch != nil {
		game.FileName = entry.Name
	} else {
		game.FileName = resolution.EntryFile
	}
	manifest := clientGameManifestResponse{
		Game:      game,
		FileURL:   resolvedGameFileURL(entry),
		EntryFile: &resolution.EntryFile,
		Files:     make([]clientGameFile, 0, len(resolution.Files)),
	}
	if !resolution.Game.UpdatedAt.IsZero() {
		manifest.UpdatedAt = resolution.Game.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	diskIndex := 0
	for _, file := range resolution.Files {
		clientFile := clientGameFile{
			Name: file.Name, Size: file.Size, Role: file.Role,
			URL: resolvedGameFileURL(file),
		}
		if file.SHA1 != "" {
			clientFile.Checksum = "sha1:" + file.SHA1
		}
		if resolution.Game.Platform == "pc98" && file.Role != "font" && isPC98FloppyManifestFile(file.Name) {
			clientFile.Role = "disk"
			if file.Role == "entry" {
				clientFile.Role = "entry"
			}
			if diskIndex == 0 {
				clientFile.DriveHint = "FDD1"
			}
			index := diskIndex
			clientFile.DiskIndex = &index
			clientFile.Label = fmt.Sprintf("Disk %d", diskIndex+1)
			diskIndex++
		}
		manifest.Files = append(manifest.Files, clientFile)
	}
	if resolution.DOSLaunch != nil {
		launch := resolution.DOSLaunch
		manifest.DOSLaunch = &clientDOSLaunch{
			EntrySource: launch.EntrySource, InstallDirectory: nullableString(launch.InstallDirectory),
			WorkingDirectory: nullableString(launch.WorkingDirectory),
			DOSBoxConfig:     nullableString(launch.DOSBoxConfig), Arguments: launch.Arguments,
			Candidates: launch.Candidates, KeymapHints: launch.KeymapHints,
		}
	}
	return clientGameLaunchResolutionResponse{
		LaunchProfileID: resolution.LaunchProfileID,
		ProfileRevision: resolution.ProfileRevision,
		Runtime:         resolution.Runtime,
		Manifest:        manifest,
	}
}

func resolvedGameFileURL(file domain.GameLaunchResolvedFile) string {
	if file.Position != nil {
		return fmt.Sprintf("/api/client/games/%d/files/%d", file.SourceGameID, *file.Position)
	}
	return fmt.Sprintf("/api/client/games/%d/file", file.SourceGameID)
}

func nullableString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	value = strings.TrimSpace(value)
	return &value
}

func isPC98FloppyManifestFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".d88", ".88d", ".d98", ".98d", ".fdi", ".xdf", ".hdm", ".dup", ".2hd", ".tfd", ".nfd",
		".hd4", ".hd5", ".hd9", ".fdd", ".h01", ".hdb", ".ddb", ".dd6", ".dcp", ".dcu", ".flp", ".img",
		".ima", ".bin", ".fim":
		return true
	default:
		return false
	}
}

func filepathBase(path string) string {
	path = strings.ReplaceAll(path, "\\", "/")
	parts := strings.Split(path, "/")
	return parts[len(parts)-1]
}

func clientGameDetails(details domain.GameDetails) clientGameDetailsResponse {
	return clientGameDetailsResponse{
		Game:                       clientGameItem(details.Game),
		clientGameMetadataResponse: clientGameMetadata(details),
	}
}

func clientGameMetadata(details domain.GameDetails) clientGameMetadataResponse {
	return clientGameMetadataResponse{
		MetadataStatus: details.MetadataStatus,
		Metadata:       details.Metadata,
		Sources:        clientGameMetadataSources(details.Sources),
		Artwork:        clientGameArtworkItems(details.Artwork),
	}
}

func clientGameMetadataSources(items []domain.GameMetadataSource) []clientGameMetadataSource {
	out := make([]clientGameMetadataSource, 0, len(items))
	for _, item := range items {
		out = append(out, clientGameMetadataSource{
			ID:         item.ID,
			Source:     item.Source,
			SourceID:   item.SourceID,
			MatchedBy:  item.MatchedBy,
			Confidence: item.Confidence,
			UpdatedAt:  formatClientTime(item.UpdatedAt),
		})
	}
	return out
}

func gameMetadataActionResponseFromResult(result domain.GameMetadataActionResult) gameMetadataActionResponse {
	return gameMetadataActionResponse{
		GameID:         result.GameID,
		Action:         result.Action,
		Status:         result.Status,
		Message:        result.Message,
		MetadataStatus: result.MetadataStatus,
		Sources:        clientGameMetadataSources(result.Sources),
		Providers:      result.Providers,
	}
}

func clientGameArtworkItems(items []domain.GameArtwork) []clientGameArtwork {
	out := make([]clientGameArtwork, 0, len(items))
	for _, item := range items {
		out = append(out, clientGameArtwork{
			ID:         item.ID,
			Source:     item.Source,
			Kind:       item.Kind,
			URL:        item.URL,
			Width:      item.Width,
			Height:     item.Height,
			Selected:   item.Selected,
			Confidence: item.Confidence,
			UpdatedAt:  formatClientTime(item.UpdatedAt),
		})
	}
	return out
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
	var fileSize *int64
	if strings.TrimSpace(book.FilePath) != "" {
		size := book.FileSize
		fileSize = &size
	}
	return clientBook{
		ID:                   book.ID,
		CollectionID:         book.SeriesID,
		SeriesID:             book.SeriesID,
		CollectionTitle:      book.CollectionTitle,
		Title:                book.Title,
		Creator:              book.Creator,
		Description:          book.Description,
		BookType:             book.BookType,
		Format:               book.Format,
		PageCount:            book.PageCount,
		CoverStatus:          book.CoverStatus,
		CoverURL:             clientCoverURL(book.ID),
		ThumbnailStatus:      thumbnailStatus(book),
		ThumbnailURL:         clientThumbnailURL(book.ID, "small"),
		ManifestURL:          fmt.Sprintf("/api/client/books/%d/manifest", book.ID),
		DownloadURL:          clientBookDownloadURL(book.ID),
		Analyzed:             book.Analyzed,
		AddedAt:              formatClientTime(book.AddedAt),
		UpdatedAt:            formatClientTime(book.UpdatedAt),
		CurrentPage:          book.CurrentPage,
		ProgressFraction:     book.ProgressFraction,
		LastReadAt:           formatClientTime(book.LastReadAt),
		PrivateStatus:        book.PrivateStatus,
		Favorite:             book.Favorite,
		Rating:               book.Rating,
		Tags:                 book.Tags,
		Summary:              book.Summary,
		ContentHash:          book.ContentHash,
		ContentHashAlgorithm: book.ContentHashAlgorithm,
		FileSize:             fileSize,
		ContentRevision:      book.ContentRevision,
	}
}

func clientBookDownloadURL(bookID int64) string {
	return fmt.Sprintf("/api/client/books/%d/file", bookID)
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
