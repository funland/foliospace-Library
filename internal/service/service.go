package service

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"foliospace-reader/internal/archive"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/scanner"
	"foliospace-reader/internal/store"
)

type Service struct {
	store                    *store.Store
	scanner                  *scanner.Scanner
	configDir                string
	thumbnailWorker          *thumbnailWorker
	thumbnailCacheStatusMu   sync.Mutex
	thumbnailCacheStatusSnap domain.ThumbnailCacheStatus
	thumbnailCacheStatusTime time.Time
}

const adminTokenHashSetting = "admin_token_sha256"
const scanWorkersSetting = "scan_workers"

var errVideoTranscodeBusy = errors.New("another video transcode is running")

var videoTranscodeState = struct {
	sync.Mutex
	videoID int64
	dir     string
}{}

var videoThumbnailState = struct {
	sync.Mutex
	cond   *sync.Cond
	active bool
}{}

type VideoTranscodeStatus struct {
	VideoID      int64  `json:"videoId"`
	Status       string `json:"status"`
	Message      string `json:"message,omitempty"`
	SegmentCount int    `json:"segmentCount"`
}

type VideoTranscodeQueueStatus struct {
	Status        string `json:"status"`
	ActiveVideoID int64  `json:"activeVideoId,omitempty"`
	ActiveTitle   string `json:"activeTitle,omitempty"`
	SegmentCount  int    `json:"segmentCount"`
	Message       string `json:"message,omitempty"`
}

func IsVideoTranscodeBusy(err error) bool {
	return errors.Is(err, errVideoTranscodeBusy)
}

type SetupStatus struct {
	Initialized     bool                    `json:"initialized"`
	AuthEnabled     bool                    `json:"authEnabled"`
	HasLibraries    bool                    `json:"hasLibraries"`
	TokenConfigured bool                    `json:"tokenConfigured"`
	DirectoryRoots  []domain.DirectoryEntry `json:"directoryRoots"`
	ScanWorkers     int                     `json:"scanWorkers"`
}

type SetupInput struct {
	Token       string `json:"token"`
	Name        string `json:"name"`
	RootPath    string `json:"rootPath"`
	AssetType   string `json:"assetType"`
	ScanWorkers int    `json:"scanWorkers"`
}

type ScanSettings struct {
	ScanWorkers int `json:"scanWorkers"`
}

func New(store *store.Store) *Service {
	return NewWithConfig(store, "")
}

func NewWithConfig(store *store.Store, configDir string) *Service {
	svc := &Service{
		store: store,
		scanner: scanner.NewWithWorkerCount(store, func() int {
			return scanWorkerCountFromStore(store)
		}),
		configDir: strings.TrimSpace(configDir),
	}
	_, _ = store.ResetRunningThumbnailJobs()
	svc.thumbnailWorker = newThumbnailWorker(svc)
	svc.thumbnailWorker.start()
	return svc
}

func (s *Service) CreateLibrary(name string, rootPath string) (domain.Library, error) {
	return s.CreateLibraryWithType(name, rootPath, "mixed")
}

func (s *Service) SetupStatus(envTokenConfigured bool) (SetupStatus, error) {
	libraries, err := s.ListLibraries()
	if err != nil {
		return SetupStatus{}, err
	}
	roots, err := s.DirectoryRoots()
	if err != nil {
		return SetupStatus{}, err
	}
	tokenConfigured := envTokenConfigured || s.AdminTokenConfigured()
	hasLibraries := len(libraries) > 0
	return SetupStatus{
		Initialized:     tokenConfigured && hasLibraries,
		AuthEnabled:     tokenConfigured,
		HasLibraries:    hasLibraries,
		TokenConfigured: tokenConfigured,
		DirectoryRoots:  roots,
		ScanWorkers:     s.ScanWorkerCount(),
	}, nil
}

func (s *Service) InitializeSetup(input SetupInput, tokenAlreadyConfigured bool) (domain.Library, error) {
	token := strings.TrimSpace(input.Token)
	if token == "" && !tokenAlreadyConfigured {
		return domain.Library{}, fmt.Errorf("access token is required")
	}
	if token != "" {
		if err := s.SetAdminToken(token); err != nil {
			return domain.Library{}, err
		}
	}
	if input.ScanWorkers > 0 {
		if err := s.SaveScanSettings(ScanSettings{ScanWorkers: input.ScanWorkers}); err != nil {
			return domain.Library{}, err
		}
	}
	if strings.TrimSpace(input.RootPath) == "" {
		libraries, err := s.ListLibraries()
		if err != nil {
			return domain.Library{}, err
		}
		if len(libraries) > 0 {
			return libraries[0], nil
		}
	}
	return s.CreateLibraryWithType(input.Name, input.RootPath, input.AssetType)
}

func (s *Service) ScanSettings() ScanSettings {
	return ScanSettings{ScanWorkers: s.ScanWorkerCount()}
}

func (s *Service) SaveScanSettings(settings ScanSettings) error {
	workers := scanner.NormalizeWorkerCount(fmt.Sprintf("%d", settings.ScanWorkers))
	return s.store.UpsertSetting(scanWorkersSetting, fmt.Sprintf("%d", workers))
}

func (s *Service) ScanWorkerCount() int {
	return scanWorkerCountFromStore(s.store)
}

func scanWorkerCountFromStore(st *store.Store) int {
	if value, err := st.Setting(scanWorkersSetting); err == nil && strings.TrimSpace(value) != "" {
		return scanner.NormalizeWorkerCount(value)
	}
	return scanner.NormalizeWorkerCount(os.Getenv("FOLIOSPACE_SCAN_WORKERS"))
}

func (s *Service) AdminTokenConfigured() bool {
	hash, err := s.store.Setting(adminTokenHashSetting)
	return err == nil && strings.TrimSpace(hash) != ""
}

func (s *Service) SetAdminToken(token string) error {
	token = strings.TrimSpace(token)
	if len(token) < 8 {
		return fmt.Errorf("access token must be at least 8 characters")
	}
	sum := sha256.Sum256([]byte(token))
	return s.store.UpsertSetting(adminTokenHashSetting, hex.EncodeToString(sum[:]))
}

func (s *Service) VerifyAdminToken(token string) bool {
	hash, err := s.store.Setting(adminTokenHashSetting)
	if err != nil || strings.TrimSpace(hash) == "" {
		return false
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return subtle.ConstantTimeCompare([]byte(strings.TrimSpace(hash)), []byte(hex.EncodeToString(sum[:]))) == 1
}

func (s *Service) CreateLibraryWithType(name string, rootPath string, assetType string) (domain.Library, error) {
	name = strings.TrimSpace(name)
	rootPath = strings.TrimSpace(rootPath)
	if rootPath == "" {
		return domain.Library{}, fmt.Errorf("library root path is required")
	}
	if name == "" {
		name = rootPath
	}
	return s.store.CreateLibraryWithType(name, rootPath, normalizeLibraryAssetType(assetType))
}

func (s *Service) ListDirectories(path string) (domain.DirectoryListing, error) {
	path = strings.TrimSpace(path)
	roots, err := s.DirectoryRoots()
	if err != nil {
		return domain.DirectoryListing{}, err
	}
	if path == "" || path == string(filepath.Separator) {
		return domain.DirectoryListing{
			Path:    string(filepath.Separator),
			Entries: roots,
		}, nil
	}
	if !filepath.IsAbs(path) {
		path = string(filepath.Separator) + path
	}
	path = filepath.Clean(path)
	if !pathWithinDirectoryRoots(path, roots) {
		return domain.DirectoryListing{}, fmt.Errorf("directory %s is outside allowed roots", path)
	}

	info, err := os.Stat(path)
	if err != nil {
		return domain.DirectoryListing{}, err
	}
	if !info.IsDir() {
		return domain.DirectoryListing{}, fmt.Errorf("%s is not a directory", path)
	}
	items, err := os.ReadDir(path)
	if err != nil {
		return domain.DirectoryListing{}, err
	}
	entries := make([]domain.DirectoryEntry, 0)
	for _, item := range items {
		if !item.IsDir() {
			continue
		}
		entries = append(entries, domain.DirectoryEntry{
			Name: item.Name(),
			Path: filepath.Join(path, item.Name()),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	parent := ""
	if directoryPathIsRoot(path, roots) {
		parent = string(filepath.Separator)
	} else if parentPath := filepath.Dir(path); parentPath != path && pathWithinDirectoryRoots(parentPath, roots) {
		parent = parentPath
	}
	return domain.DirectoryListing{Path: path, Parent: parent, Entries: entries}, nil
}

func (s *Service) DirectoryRoots() ([]domain.DirectoryEntry, error) {
	candidates := []string{os.Getenv("FOLIOSPACE_LIBRARY_DIR")}
	candidates = append(candidates, strings.Split(os.Getenv("FOLIOSPACE_DIRECTORY_ROOTS"), ",")...)
	candidates = append(candidates, "/library", "/games")
	libraries, err := s.store.ListLibraries()
	if err != nil {
		return nil, err
	}
	for _, library := range libraries {
		candidates = append(candidates, library.RootPath)
	}

	seen := map[string]bool{}
	roots := make([]domain.DirectoryEntry, 0)
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if !filepath.IsAbs(candidate) {
			candidate = string(filepath.Separator) + candidate
		}
		candidate = filepath.Clean(candidate)
		info, err := os.Stat(candidate)
		if err != nil || !info.IsDir() || seen[candidate] {
			continue
		}
		seen[candidate] = true
		roots = append(roots, domain.DirectoryEntry{
			Name: directoryRootName(candidate),
			Path: candidate,
		})
	}
	sort.Slice(roots, func(i, j int) bool {
		return strings.ToLower(roots[i].Path) < strings.ToLower(roots[j].Path)
	})
	return roots, nil
}

func (s *Service) ListProfiles() ([]domain.Profile, error) {
	return s.store.ListProfiles()
}

func (s *Service) CreateProfile(name string, avatar string, color string) (domain.Profile, error) {
	return s.store.CreateProfile(name, avatar, color)
}

func (s *Service) RenameProfile(profileID int64, name string) (domain.Profile, error) {
	return s.store.RenameProfile(profileID, name)
}

func (s *Service) UpdateProfile(profileID int64, name string, avatar string, color string) (domain.Profile, error) {
	return s.store.UpdateProfile(profileID, name, avatar, color)
}

func (s *Service) DeleteProfile(profileID int64) error {
	return s.store.DeleteProfile(profileID)
}

func (s *Service) ResolveProfileID(profileID int64) (int64, error) {
	return s.store.ResolveProfileID(profileID)
}

func pathWithinDirectoryRoots(path string, roots []domain.DirectoryEntry) bool {
	path = filepath.Clean(path)
	for _, root := range roots {
		rootPath := filepath.Clean(root.Path)
		if path == rootPath {
			return true
		}
		prefix := rootPath
		if !strings.HasSuffix(prefix, string(filepath.Separator)) {
			prefix += string(filepath.Separator)
		}
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func directoryPathIsRoot(path string, roots []domain.DirectoryEntry) bool {
	path = filepath.Clean(path)
	for _, root := range roots {
		if path == filepath.Clean(root.Path) {
			return true
		}
	}
	return false
}

func directoryRootName(path string) string {
	if path == string(filepath.Separator) {
		return path
	}
	name := filepath.Base(path)
	if name == "." || name == string(filepath.Separator) || name == "" {
		return path
	}
	return name
}

func normalizeLibraryAssetType(value string) string {
	if oneOf(value, "mixed", "book", "comic", "game", "video") {
		return value
	}
	return "mixed"
}

func (s *Service) ListLibraries() ([]domain.Library, error) {
	return s.store.ListLibraries()
}

func (s *Service) DeleteLibrary(id int64) error {
	return s.store.DeleteLibrary(id)
}

func (s *Service) ScanLibrary(id int64) (domain.ScanJob, error) {
	lib, err := s.store.LibraryByID(id)
	if err != nil {
		return domain.ScanJob{}, err
	}
	return s.scanner.StartScanJob(lib)
}

func (s *Service) ListSeries() ([]domain.Series, error) {
	series, err := s.store.ListSeries()
	if err != nil {
		return nil, err
	}
	gameCollections, err := s.store.ListGamePlatformCollections()
	if err != nil {
		return nil, err
	}
	return append(series, gameCollections...), nil
}

func (s *Service) ListBooks(seriesID int64) ([]domain.Book, error) {
	return s.store.ListBooks(seriesID)
}

func (s *Service) ListBooksForProfile(seriesID int64, profileID int64) ([]domain.Book, error) {
	return s.store.ListBooksForProfile(seriesID, profileID)
}

func (s *Service) OpenSeriesCover(seriesID int64) (PageStream, error) {
	page, err := s.store.ListBooksPage(domain.BookListOptions{
		SeriesID: seriesID,
		Limit:    1,
		Sort:     "title",
	})
	if err != nil {
		return PageStream{}, err
	}
	if len(page.Items) == 0 {
		return PageStream{}, fmt.Errorf("series has no books")
	}
	return s.OpenCover(page.Items[0].ID)
}

func (s *Service) CollectionAssets(seriesID int64) (domain.CollectionAssets, error) {
	return s.CollectionAssetsForProfile(seriesID, 0)
}

func (s *Service) CollectionAssetsForProfile(seriesID int64, profileID int64) (domain.CollectionAssets, error) {
	if platform := store.PlatformFromGamePlatformCollectionID(seriesID); platform != "" {
		games, err := s.store.ListGamesByPlatform(platform)
		if err != nil {
			return domain.CollectionAssets{}, err
		}
		return domain.CollectionAssets{Books: []domain.Book{}, Games: games}, nil
	}
	series, err := s.store.SeriesByID(seriesID)
	if err != nil {
		return domain.CollectionAssets{}, err
	}
	books, err := s.store.ListBooksForProfile(seriesID, profileID)
	if err != nil {
		return domain.CollectionAssets{}, err
	}
	games, err := s.store.ListGamesByROMSet(series.Title)
	if err != nil {
		return domain.CollectionAssets{}, err
	}
	return domain.CollectionAssets{Books: books, Games: games}, nil
}

func (s *Service) ListBooksPage(options domain.BookListOptions) (domain.BookListPage, error) {
	return s.store.ListBooksPage(options)
}

func (s *Service) ListBooksPageForProfile(options domain.BookListOptions, profileID int64) (domain.BookListPage, error) {
	return s.store.ListBooksPageForProfile(options, profileID)
}

func (s *Service) SearchBooks(query string, limit int) ([]domain.Book, error) {
	return s.store.SearchBooks(query, limit)
}

func (s *Service) SearchBooksForProfile(query string, profileID int64, limit int) ([]domain.Book, error) {
	return s.store.SearchBooksForProfile(query, profileID, limit)
}

func (s *Service) UpdateBookPrivateState(bookID int64, state domain.BookPrivateState) (domain.Book, error) {
	return s.UpdateBookPrivateStateForProfile(bookID, 0, state)
}

func (s *Service) UpdateBookPrivateStateForProfile(bookID int64, profileID int64, state domain.BookPrivateState) (domain.Book, error) {
	state.Status = strings.TrimSpace(state.Status)
	state.Summary = strings.TrimSpace(state.Summary)
	if state.Rating < 0 {
		state.Rating = 0
	}
	if state.Rating > 5 {
		state.Rating = 5
	}
	profileID, err := s.store.ResolveProfileID(profileID)
	if err != nil {
		return domain.Book{}, err
	}
	if err := s.store.UpdateBookPrivateStateForProfile(bookID, profileID, state); err != nil {
		return domain.Book{}, err
	}
	return s.store.BookByIDForProfile(bookID, profileID)
}

func (s *Service) ClientPreferences() (domain.ClientPreferences, error) {
	return s.store.ClientPreferences()
}

func (s *Service) ClientPreferencesForProfile(profileID int64) (domain.ClientPreferences, error) {
	return s.store.ClientPreferencesForProfile(profileID)
}

func (s *Service) SaveClientPreferences(prefs domain.ClientPreferences) (domain.ClientPreferences, error) {
	return s.SaveClientPreferencesForProfile(0, prefs)
}

func (s *Service) SaveClientPreferencesForProfile(profileID int64, prefs domain.ClientPreferences) (domain.ClientPreferences, error) {
	prefs = normalizeClientPreferences(prefs)
	profileID, err := s.store.ResolveProfileID(profileID)
	if err != nil {
		return domain.ClientPreferences{}, err
	}
	if err := s.store.SaveClientPreferencesForProfile(profileID, prefs); err != nil {
		return domain.ClientPreferences{}, err
	}
	return s.store.ClientPreferencesForProfile(profileID)
}

func normalizeClientPreferences(prefs domain.ClientPreferences) domain.ClientPreferences {
	if !oneOf(prefs.Locale, "zh", "zht", "en", "ja", "ko") {
		prefs.Locale = "zh"
	}
	if !oneOf(prefs.ReaderPageMode, "single", "double", "webtoon") {
		prefs.ReaderPageMode = "single"
	}
	if !oneOf(prefs.EPUBPageMode, "single", "double") {
		prefs.EPUBPageMode = "single"
	}
	if !oneOf(prefs.EPUBTheme, "light", "sepia", "dark") {
		prefs.EPUBTheme = "light"
	}
	if prefs.EPUBFontSize == 0 {
		prefs.EPUBFontSize = 18
	}
	if prefs.EPUBFontSize < 14 {
		prefs.EPUBFontSize = 14
	}
	if prefs.EPUBFontSize > 26 {
		prefs.EPUBFontSize = 26
	}
	return prefs
}

func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func (s *Service) ContinueReading(limit int) ([]domain.Book, error) {
	return s.store.ListContinueReading(limit)
}

func (s *Service) ContinueReadingForProfile(profileID int64, limit int) ([]domain.Book, error) {
	return s.store.ListContinueReadingForProfile(profileID, limit)
}

func (s *Service) RecentBooks(limit int) ([]domain.Book, error) {
	return s.store.ListRecentBooks(limit)
}

func (s *Service) RecentBooksForProfile(profileID int64, limit int) ([]domain.Book, error) {
	return s.store.ListRecentBooksForProfile(profileID, limit)
}

func (s *Service) RecentGames(limit int) ([]domain.GameAsset, error) {
	return s.store.ListRecentGames(limit)
}

func (s *Service) RecentVideos(limit int) ([]domain.VideoAsset, error) {
	return s.store.ListRecentVideos(limit)
}

func (s *Service) ListGamesPage(options domain.GameListOptions) (domain.GameListPage, error) {
	return s.store.ListGamesPage(options)
}

func (s *Service) ListVideosPage(options domain.VideoListOptions) (domain.VideoListPage, error) {
	return s.store.ListVideosPage(options)
}

func (s *Service) Game(id int64) (domain.GameAsset, error) {
	return s.store.GameByID(id)
}

func (s *Service) Video(id int64) (domain.VideoAsset, error) {
	return s.store.VideoByID(id)
}

func (s *Service) OpenVideoThumbnail(id int64) (PageStream, error) {
	video, err := s.store.VideoByID(id)
	if err != nil {
		return PageStream{}, err
	}
	if video.FilePath == "" {
		return PageStream{}, fmt.Errorf("video has no indexed file")
	}
	for _, candidate := range localVideoThumbnailCandidates(video.FilePath) {
		if file, contentType, err := openImageFile(candidate); err == nil {
			return PageStream{Body: file, ContentType: contentType}, nil
		}
	}
	cachePath, err := s.videoThumbnailCachePath(video)
	if err != nil {
		return PageStream{}, err
	}
	if file, err := os.Open(cachePath); err == nil {
		return PageStream{Body: file, ContentType: "image/jpeg"}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if !claimVideoThumbnail(ctx) {
		return PageStream{}, fmt.Errorf("video thumbnail extraction is busy")
	}
	defer releaseVideoThumbnail()
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return PageStream{}, err
	}
	if err := extractVideoThumbnail(video.FilePath, cachePath, video.DurationSeconds); err != nil {
		_ = os.Remove(cachePath)
		return PageStream{}, err
	}
	file, err := os.Open(cachePath)
	if err != nil {
		return PageStream{}, err
	}
	return PageStream{Body: file, ContentType: "image/jpeg"}, nil
}

func (s *Service) OpenGameCover(id int64) (PageStream, error) {
	game, err := s.store.GameByID(id)
	if err != nil {
		return PageStream{}, err
	}
	urls := libretroBoxartCandidates(game)
	if len(urls) == 0 {
		return PageStream{}, fmt.Errorf("game cover source not available")
	}
	cachePath, err := s.gameCoverCachePath(id)
	if err != nil {
		return PageStream{}, err
	}
	if file, err := os.Open(cachePath); err == nil {
		return PageStream{Body: file, ContentType: "image/png"}, nil
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return PageStream{}, err
	}
	for _, sourceURL := range urls {
		if err := downloadGameCover(sourceURL, cachePath); err == nil {
			file, err := os.Open(cachePath)
			if err != nil {
				return PageStream{}, err
			}
			return PageStream{Body: file, ContentType: "image/png"}, nil
		}
	}
	return PageStream{}, fmt.Errorf("game cover not found")
}

func (s *Service) gameCoverCachePath(id int64) (string, error) {
	base := s.configDir
	if base == "" {
		base = filepath.Join(os.TempDir(), "foliospace-reader")
	}
	return filepath.Join(base, "cache", "game-covers", fmt.Sprintf("%d.png", id)), nil
}

func (s *Service) videoThumbnailCachePath(video domain.VideoAsset) (string, error) {
	base := s.configDir
	if base == "" {
		base = filepath.Join(os.TempDir(), "foliospace-reader")
	}
	keySource := fmt.Sprintf("%d|%s|%d|%s", video.ID, video.FilePath, video.Size, video.MTime.Format(time.RFC3339Nano))
	sum := sha256.Sum256([]byte(keySource))
	return filepath.Join(base, "cache", "video-thumbnails", fmt.Sprintf("%d-%s.jpg", video.ID, hex.EncodeToString(sum[:])[:12])), nil
}

func (s *Service) OpenGameFile(id int64) (PageStream, error) {
	game, err := s.store.GameByID(id)
	if err != nil {
		return PageStream{}, err
	}
	if game.FilePath == "" {
		return PageStream{}, fmt.Errorf("game has no indexed file")
	}
	body, err := os.Open(game.FilePath)
	if err != nil {
		return PageStream{}, err
	}
	return PageStream{Body: body, ContentType: "application/octet-stream"}, nil
}

func (s *Service) VideoFilePath(id int64) (string, error) {
	video, err := s.store.VideoByID(id)
	if err != nil {
		return "", err
	}
	if video.FilePath == "" {
		return "", fmt.Errorf("video has no indexed file")
	}
	return video.FilePath, nil
}

func (s *Service) EnsureVideoHLS(id int64) (string, error) {
	video, err := s.store.VideoByID(id)
	if err != nil {
		return "", err
	}
	if video.FilePath == "" {
		return "", fmt.Errorf("video has no indexed file")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return "", fmt.Errorf("ffmpeg is not installed")
	}
	dir, err := s.videoTranscodeCacheDir(video)
	if err != nil {
		return "", err
	}
	playlist := filepath.Join(dir, "index.m3u8")
	if fileExists(playlist) {
		return playlist, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	alreadyRunning := currentVideoTranscode(id, dir)
	if !claimVideoTranscode(id, dir) {
		return "", errVideoTranscodeBusy
	}
	startedPath := filepath.Join(dir, ".started")
	if !alreadyRunning {
		if err := os.WriteFile(startedPath, []byte(time.Now().Format(time.RFC3339)), 0o644); err != nil {
			releaseVideoTranscode(id, dir)
			return "", err
		}
		if err := startVideoHLSTranscode(id, video.FilePath, dir); err != nil {
			releaseVideoTranscode(id, dir)
			return "", err
		}
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if fileExists(playlist) {
			return playlist, nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return "", fmt.Errorf("video transcode is starting")
}

func (s *Service) VideoTranscodeStatus(id int64) (VideoTranscodeStatus, error) {
	video, err := s.store.VideoByID(id)
	if err != nil {
		return VideoTranscodeStatus{}, err
	}
	dir, err := s.videoTranscodeCacheDir(video)
	if err != nil {
		return VideoTranscodeStatus{}, err
	}
	status := VideoTranscodeStatus{VideoID: id}
	playlist := filepath.Join(dir, "index.m3u8")
	status.SegmentCount = countHLSSegments(dir)
	switch {
	case fileExists(playlist):
		status.Status = "ready"
		status.Message = "HLS cache is ready"
	case currentVideoTranscode(id, dir):
		status.Status = "running"
		status.Message = "Transcoding to browser-compatible HLS"
	case otherVideoTranscodeActive():
		status.Status = "queued"
		status.Message = "Waiting for the current video transcode to finish"
	case transcodeLogLooksFailed(filepath.Join(dir, "ffmpeg.log")):
		status.Status = "failed"
		status.Message = "Transcode failed; see ffmpeg.log"
	case fileExists(filepath.Join(dir, ".started")):
		status.Status = "starting"
		status.Message = "Transcode is starting"
	default:
		status.Status = "idle"
		status.Message = "Transcode has not started"
	}
	return status, nil
}

func (s *Service) VideoTranscodeQueueStatus() (VideoTranscodeQueueStatus, error) {
	videoID, dir := activeVideoTranscode()
	if videoID == 0 || dir == "" {
		return VideoTranscodeQueueStatus{Status: "idle", Message: "No active video transcode"}, nil
	}
	status := VideoTranscodeQueueStatus{
		Status:        "running",
		ActiveVideoID: videoID,
		SegmentCount:  countHLSSegments(dir),
		Message:       "Transcoding to browser-compatible HLS",
	}
	video, err := s.store.VideoByID(videoID)
	if err == nil {
		status.ActiveTitle = video.Title
	}
	return status, nil
}

func (s *Service) VideoHLSFilePath(id int64, name string) (string, error) {
	video, err := s.store.VideoByID(id)
	if err != nil {
		return "", err
	}
	dir, err := s.videoTranscodeCacheDir(video)
	if err != nil {
		return "", err
	}
	cleanName := filepath.Base(strings.TrimSpace(name))
	if cleanName != name || cleanName == "." || cleanName == string(filepath.Separator) {
		return "", fmt.Errorf("invalid hls segment path")
	}
	switch filepath.Ext(cleanName) {
	case ".m3u8", ".ts":
	default:
		return "", fmt.Errorf("invalid hls segment extension")
	}
	path := filepath.Join(dir, cleanName)
	if !strings.HasPrefix(path, dir+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid hls segment path")
	}
	return path, nil
}

func (s *Service) videoTranscodeCacheDir(video domain.VideoAsset) (string, error) {
	base := s.configDir
	if base == "" {
		base = os.TempDir()
	}
	keySource := fmt.Sprintf("%d|%s|%d|%s", video.ID, video.FilePath, video.Size, video.MTime.Format(time.RFC3339Nano))
	sum := sha256.Sum256([]byte(keySource))
	return filepath.Join(base, "cache", "video-transcodes", fmt.Sprintf("%d-%s", video.ID, hex.EncodeToString(sum[:])[:12])), nil
}

func startVideoHLSTranscode(videoID int64, inputPath string, outputDir string) error {
	logPath := filepath.Join(outputDir, "ffmpeg.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	args := []string{
		"-hide_banner", "-nostdin", "-y",
		"-i", inputPath,
		"-map", "0:v:0", "-map", "0:a:0?",
		"-vf", "scale=w=min(1920\\,iw):h=-2",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "23",
		"-c:a", "aac", "-ac", "2",
		"-f", "hls",
		"-hls_time", "6",
		"-hls_list_size", "0",
		"-hls_segment_filename", filepath.Join(outputDir, "segment_%05d.ts"),
		filepath.Join(outputDir, "index.m3u8"),
	}
	cmd := exec.CommandContext(context.Background(), "ffmpeg", args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return err
	}
	go func() {
		_ = cmd.Wait()
		_ = logFile.Close()
		releaseVideoTranscode(videoID, outputDir)
	}()
	return nil
}

func claimVideoTranscode(videoID int64, dir string) bool {
	videoTranscodeState.Lock()
	defer videoTranscodeState.Unlock()
	if videoTranscodeState.dir != "" && videoTranscodeState.dir != dir {
		return false
	}
	videoTranscodeState.videoID = videoID
	videoTranscodeState.dir = dir
	return true
}

func releaseVideoTranscode(videoID int64, dir string) {
	videoTranscodeState.Lock()
	defer videoTranscodeState.Unlock()
	if videoTranscodeState.videoID == videoID && videoTranscodeState.dir == dir {
		videoTranscodeState.videoID = 0
		videoTranscodeState.dir = ""
	}
}

func currentVideoTranscode(videoID int64, dir string) bool {
	videoTranscodeState.Lock()
	defer videoTranscodeState.Unlock()
	return videoTranscodeState.videoID == videoID && videoTranscodeState.dir == dir
}

func otherVideoTranscodeActive() bool {
	videoTranscodeState.Lock()
	defer videoTranscodeState.Unlock()
	return videoTranscodeState.dir != ""
}

func activeVideoTranscode() (int64, string) {
	videoTranscodeState.Lock()
	defer videoTranscodeState.Unlock()
	return videoTranscodeState.videoID, videoTranscodeState.dir
}

func countHLSSegments(dir string) int {
	matches, err := filepath.Glob(filepath.Join(dir, "segment_*.ts"))
	if err != nil {
		return 0
	}
	return len(matches)
}

func transcodeLogLooksFailed(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	text := strings.ToLower(string(data))
	return strings.Contains(text, "conversion failed") || strings.Contains(text, "error while") || strings.Contains(text, "invalid data")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func recentMarker(path string, ttl time.Duration) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) < ttl
}

func (s *Service) FavoriteBooks(limit int) ([]domain.Book, error) {
	return s.store.ListFavoriteBooks(limit)
}

func (s *Service) FavoriteBooksForProfile(profileID int64, limit int) ([]domain.Book, error) {
	return s.store.ListFavoriteBooksForProfile(profileID, limit)
}

func (s *Service) BooksByPrivateStatus(status string, limit int) ([]domain.Book, error) {
	return s.store.ListBooksByPrivateStatus(status, limit)
}

func (s *Service) BooksByPrivateStatusForProfile(profileID int64, status string, limit int) ([]domain.Book, error) {
	return s.store.ListBooksByPrivateStatusForProfile(profileID, status, limit)
}

func libretroBoxartCandidates(game domain.GameAsset) []string {
	playlist, ok := libretroPlaylist(game.Platform)
	if !ok {
		return nil
	}
	title := strings.TrimSpace(game.Title)
	if title == "" {
		return nil
	}
	names := []string{title}
	if game.Region != "" {
		names = append(names, fmt.Sprintf("%s (%s)", title, strings.TrimSpace(game.Region)))
	}
	if game.Region == "" {
		names = append(names, title+" (USA)", title+" (World)", title+" (Japan)")
	}
	out := make([]string, 0, len(names))
	seen := make(map[string]bool)
	for _, name := range names {
		name = sanitizeLibretroName(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, fmt.Sprintf("https://thumbnails.libretro.com/%s/Named_Boxarts/%s.png", urlPathEscape(playlist), urlPathEscape(name)))
	}
	return out
}

func libretroPlaylist(platform string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "nes":
		return "Nintendo - Nintendo Entertainment System", true
	case "snes":
		return "Nintendo - Super Nintendo Entertainment System", true
	case "gb":
		return "Nintendo - Game Boy", true
	case "gbc":
		return "Nintendo - Game Boy Color", true
	case "gba":
		return "Nintendo - Game Boy Advance", true
	case "genesis", "mega-drive", "megadrive":
		return "Sega - Mega Drive - Genesis", true
	default:
		return "", false
	}
}

func sanitizeLibretroName(name string) string {
	replacer := strings.NewReplacer("&", "_", "*", "_", ":", "_", "`", "_", "<", "_", ">", "_", "?", "_", `\`, "_", "|", "_", `"`, "_")
	return strings.TrimSpace(replacer.Replace(name))
}

func urlPathEscape(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

func downloadGameCover(sourceURL string, cachePath string) error {
	client := http.Client{Timeout: 8 * time.Second}
	response, err := client.Get(sourceURL)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("cover source returned %d", response.StatusCode)
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "" && !strings.HasPrefix(contentType, "image/") {
		return fmt.Errorf("cover source returned %s", contentType)
	}
	tmpPath := cachePath + ".tmp"
	file, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, io.LimitReader(response.Body, 8<<20))
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return closeErr
	}
	return os.Rename(tmpPath, cachePath)
}

func localVideoThumbnailCandidates(videoPath string) []string {
	dir := filepath.Dir(videoPath)
	ext := filepath.Ext(videoPath)
	base := strings.TrimSuffix(filepath.Base(videoPath), ext)
	names := []string{
		base + ".jpg",
		base + ".jpeg",
		base + ".png",
		base + ".webp",
		base + ".poster.jpg",
		base + ".poster.jpeg",
		base + ".poster.png",
		base + ".cover.jpg",
		base + ".cover.jpeg",
		base + ".cover.png",
		"poster.jpg",
		"poster.jpeg",
		"poster.png",
		"cover.jpg",
		"cover.jpeg",
		"cover.png",
	}
	candidates := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		path := filepath.Join(dir, name)
		if !seen[path] {
			seen[path] = true
			candidates = append(candidates, path)
		}
	}
	return candidates
}

func openImageFile(path string) (*os.File, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	header := make([]byte, 512)
	n, readErr := file.Read(header)
	if readErr != nil && readErr != io.EOF {
		_ = file.Close()
		return nil, "", readErr
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		_ = file.Close()
		return nil, "", err
	}
	contentType := http.DetectContentType(header[:n])
	if !strings.HasPrefix(contentType, "image/") {
		_ = file.Close()
		return nil, "", fmt.Errorf("%s is not an image", path)
	}
	return file, contentType, nil
}

func extractVideoThumbnail(inputPath string, outputPath string, durationSeconds float64) error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("ffmpeg is not installed")
	}
	seekSeconds := 60
	if durationSeconds > 0 {
		seekSeconds = int(durationSeconds * 0.1)
		if seekSeconds < 10 {
			seekSeconds = 10
		}
		if seekSeconds > 300 {
			seekSeconds = 300
		}
	}
	tmpPath := outputPath + ".tmp.jpg"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	args := []string{
		"-hide_banner", "-nostdin", "-y",
		"-ss", strconv.Itoa(seekSeconds),
		"-i", inputPath,
		"-frames:v", "1",
		"-vf", "scale=w=640:h=-2",
		"-q:v", "4",
		tmpPath,
	}
	output, err := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput()
	if err != nil {
		_ = os.Remove(tmpPath)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("ffmpeg thumbnail failed: %s", strings.TrimSpace(string(output)))
	}
	return os.Rename(tmpPath, outputPath)
}

func claimVideoThumbnail(ctx context.Context) bool {
	videoThumbnailState.Lock()
	defer videoThumbnailState.Unlock()
	if videoThumbnailState.cond == nil {
		videoThumbnailState.cond = sync.NewCond(&videoThumbnailState.Mutex)
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			videoThumbnailState.Lock()
			videoThumbnailState.cond.Broadcast()
			videoThumbnailState.Unlock()
		case <-done:
		}
	}()
	defer close(done)
	if videoThumbnailState.active {
		for videoThumbnailState.active && ctx.Err() == nil {
			videoThumbnailState.cond.Wait()
		}
		if ctx.Err() != nil {
			return false
		}
	}
	videoThumbnailState.active = true
	return true
}

func releaseVideoThumbnail() {
	videoThumbnailState.Lock()
	defer videoThumbnailState.Unlock()
	videoThumbnailState.active = false
	if videoThumbnailState.cond != nil {
		videoThumbnailState.cond.Broadcast()
	}
}

func (s *Service) Book(id int64) (domain.Book, error) {
	return s.store.BookByID(id)
}

func (s *Service) BookForProfile(id int64, profileID int64) (domain.Book, error) {
	return s.store.BookByIDForProfile(id, profileID)
}

func (s *Service) AnalyzeBook(id int64) ([]domain.Page, error) {
	book, err := s.store.BookByID(id)
	if err != nil {
		return nil, err
	}
	if book.FilePath == "" {
		return nil, fmt.Errorf("book has no indexed file")
	}
	var pages []domain.Page
	if book.Format == "epub" {
		pages, err = archive.ListEPUBSpine(book.FilePath)
	} else if book.Format == "pdf" {
		pages = []domain.Page{{Index: 0, Name: filepath.Base(book.FilePath)}}
	} else {
		pages, err = archive.ListPages(book.FilePath)
	}
	if err != nil {
		return nil, err
	}
	if err := s.store.ReplacePages(id, pages); err != nil {
		return nil, err
	}
	return pages, nil
}

func (s *Service) Pages(bookID int64) ([]domain.Page, error) {
	pages, err := s.store.ListPages(bookID)
	if err != nil {
		return nil, err
	}
	if len(pages) > 0 {
		return pages, nil
	}
	return s.AnalyzeBook(bookID)
}

func (s *Service) OpenPage(bookID int64, pageIndex int) (PageStream, error) {
	book, err := s.store.BookByID(bookID)
	if err != nil {
		return PageStream{}, err
	}
	if book.FilePath == "" {
		return PageStream{}, fmt.Errorf("book has no indexed file")
	}
	if book.Format == "pdf" {
		if pageIndex != 0 {
			return PageStream{}, fmt.Errorf("page index %d out of range", pageIndex)
		}
		body, err := os.Open(book.FilePath)
		if err != nil {
			return PageStream{}, err
		}
		return PageStream{Body: body, ContentType: "application/pdf"}, nil
	}
	if book.Format == "epub" {
		pages, err := s.Pages(bookID)
		if err != nil {
			return PageStream{}, err
		}
		if pageIndex < 0 || pageIndex >= len(pages) {
			return PageStream{}, fmt.Errorf("page index %d out of range", pageIndex)
		}
		body, contentType, err := archive.OpenEPUBResource(book.FilePath, pages[pageIndex].Name)
		if err != nil {
			return PageStream{}, err
		}
		return PageStream{Body: body, ContentType: contentType}, nil
	}
	body, contentType, err := archive.OpenPage(book.FilePath, pageIndex)
	if err != nil {
		return PageStream{}, err
	}
	return PageStream{Body: body, ContentType: contentType}, nil
}

func (s *Service) OpenCover(bookID int64) (PageStream, error) {
	book, err := s.store.BookByID(bookID)
	if err != nil {
		return PageStream{}, err
	}
	if book.FilePath == "" {
		return PageStream{}, fmt.Errorf("book has no indexed file")
	}
	if book.Format == "epub" {
		body, contentType, err := archive.OpenEPUBCover(book.FilePath)
		if err != nil {
			return PageStream{}, err
		}
		return PageStream{Body: body, ContentType: contentType}, nil
	}
	if book.Format == "pdf" {
		if cover, err := renderPDFCover(book.FilePath, pdfCoverRenderDPI); err == nil {
			return cover, nil
		}
		return PageStream{
			Body:        io.NopCloser(strings.NewReader(pdfCoverPlaceholder())),
			ContentType: "image/svg+xml; charset=utf-8",
		}, nil
	}
	body, contentType, err := archive.OpenCover(book.FilePath)
	if err != nil {
		return PageStream{}, err
	}
	return PageStream{Body: body, ContentType: contentType}, nil
}

const (
	pdfCoverRenderDPI      = 144
	pdfThumbnailRenderDPI  = 96
	pdfCoverRenderTimeout  = 10 * time.Second
	pdfThumbnailSourceMark = "pdf-first-page:pdftoppm:v2"
	pdfPlaceholderMark     = "pdf-placeholder:v1"
)

type cleanupReadCloser struct {
	io.ReadCloser
	cleanup func()
}

func (reader cleanupReadCloser) Close() error {
	err := reader.ReadCloser.Close()
	if reader.cleanup != nil {
		reader.cleanup()
	}
	return err
}

func renderPDFCover(path string, dpi int) (PageStream, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return PageStream{}, fmt.Errorf("pdf path is required")
	}
	binary, err := exec.LookPath("pdftoppm")
	if err != nil {
		return PageStream{}, err
	}
	tmpDir, err := os.MkdirTemp("", "foliospace-pdf-cover-*")
	if err != nil {
		return PageStream{}, err
	}
	cleanup := func() {
		_ = os.RemoveAll(tmpDir)
	}
	outputPrefix := filepath.Join(tmpDir, "cover")
	ctx, cancel := context.WithTimeout(context.Background(), pdfCoverRenderTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-f", "1", "-singlefile", "-jpeg", "-r", strconv.Itoa(dpi), path, outputPrefix)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		cleanup()
		if ctx.Err() != nil {
			return PageStream{}, ctx.Err()
		}
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return PageStream{}, fmt.Errorf("render pdf cover: %w: %s", err, message)
		}
		return PageStream{}, fmt.Errorf("render pdf cover: %w", err)
	}
	file, err := os.Open(outputPrefix + ".jpg")
	if err != nil {
		cleanup()
		return PageStream{}, err
	}
	return PageStream{
		Body:        cleanupReadCloser{ReadCloser: file, cleanup: cleanup},
		ContentType: "image/jpeg",
	}, nil
}

func pdfThumbnailSourceCacheMarker() string {
	if _, err := exec.LookPath("pdftoppm"); err == nil {
		return pdfThumbnailSourceMark
	}
	return pdfPlaceholderMark
}

func pdfCoverPlaceholder() string {
	return `<svg xmlns="http://www.w3.org/2000/svg" width="420" height="600" viewBox="0 0 420 600">
		<rect width="420" height="600" fill="#f5f1e8"/>
		<rect x="34" y="34" width="352" height="532" rx="18" fill="#fffaf0" stroke="#d2c7b4" stroke-width="4"/>
		<text x="210" y="260" text-anchor="middle" font-family="Arial, sans-serif" font-size="42" font-weight="700" fill="#1f2a2e">PDF</text>
		<text x="210" y="316" text-anchor="middle" font-family="Arial, sans-serif" font-size="24" letter-spacing="3" fill="#6d6255">NOW PRINTING</text>
	</svg>`
}

func (s *Service) EPUBManifest(bookID int64) (domain.EPUBManifest, error) {
	book, err := s.store.BookByID(bookID)
	if err != nil {
		return domain.EPUBManifest{}, err
	}
	if book.Format != "epub" {
		return domain.EPUBManifest{}, fmt.Errorf("book is not an epub")
	}
	if book.FilePath == "" {
		return domain.EPUBManifest{}, fmt.Errorf("book has no indexed file")
	}
	return archive.ReadEPUBManifest(book.FilePath)
}

func (s *Service) OpenEPUBResource(bookID int64, resourcePath string) (PageStream, error) {
	book, err := s.store.BookByID(bookID)
	if err != nil {
		return PageStream{}, err
	}
	if book.Format != "epub" {
		return PageStream{}, fmt.Errorf("book is not an epub")
	}
	if book.FilePath == "" {
		return PageStream{}, fmt.Errorf("book has no indexed file")
	}
	body, contentType, err := archive.OpenEPUBResource(book.FilePath, resourcePath)
	if err != nil {
		return PageStream{}, err
	}
	return PageStream{Body: body, ContentType: contentType}, nil
}

func (s *Service) SaveProgress(bookID int64, pageIndex int) error {
	return s.store.SaveProgress(bookID, pageIndex)
}

func (s *Service) SaveProgressDetail(bookID int64, pageIndex int, locator string, progressFraction float64) error {
	return s.SaveProgressDetailForProfile(bookID, 0, pageIndex, locator, progressFraction)
}

func (s *Service) SaveProgressDetailForProfile(bookID int64, profileID int64, pageIndex int, locator string, progressFraction float64) error {
	if progressFraction < 0 {
		progressFraction = 0
	}
	if progressFraction > 1 {
		progressFraction = 1
	}
	return s.store.SaveProgressDetailForProfile(bookID, profileID, pageIndex, locator, progressFraction)
}

func (s *Service) Progress(bookID int64) (domain.ReadProgress, error) {
	return s.store.Progress(bookID)
}

func (s *Service) ProgressForProfile(bookID int64, profileID int64) (domain.ReadProgress, error) {
	return s.store.ProgressForProfile(bookID, profileID)
}

func (s *Service) ListJobs() ([]domain.ScanJob, error) {
	return s.store.ListScanJobs()
}

func (s *Service) PauseScanJob(jobID int64) (domain.ScanJob, error) {
	job, err := s.store.RequestScanJobPause(jobID)
	if err != nil {
		return domain.ScanJob{}, err
	}
	_ = s.store.AddJobEvent(job.ID, "info", "pause requested")
	return job, nil
}

func (s *Service) CancelScanJob(jobID int64) (domain.ScanJob, error) {
	job, err := s.store.RequestScanJobCancel(jobID)
	if err != nil {
		return domain.ScanJob{}, err
	}
	_ = s.store.AddJobEvent(job.ID, "info", "cancel requested")
	return job, nil
}

func (s *Service) ResumeScanJob(jobID int64) (domain.ScanJob, error) {
	job, err := s.store.ScanJobByID(jobID)
	if err != nil {
		return domain.ScanJob{}, err
	}
	if job.Status != "paused" {
		return domain.ScanJob{}, fmt.Errorf("scan job %d is not paused", jobID)
	}
	lib, err := s.store.LibraryByID(job.LibraryID)
	if err != nil {
		return domain.ScanJob{}, err
	}
	resumed, err := s.scanner.StartScanJob(lib)
	if err != nil {
		return domain.ScanJob{}, err
	}
	_ = s.store.AddJobEvent(job.ID, "info", fmt.Sprintf("resumed as job %d", resumed.ID))
	_ = s.store.AddJobEvent(resumed.ID, "info", fmt.Sprintf("resumed from job %d", job.ID))
	return resumed, nil
}

func (s *Service) JobEvents(jobID int64) ([]domain.JobEvent, error) {
	return s.store.ListJobEvents(jobID)
}

func (s *Service) ListErrors() ([]domain.FileError, error) {
	return s.store.ListFileErrors()
}

func (s *Service) ListErrorsByJob(jobID int64) ([]domain.FileError, error) {
	return s.store.ListFileErrorsByJob(jobID)
}

type PageStream struct {
	Body        io.ReadCloser
	ContentType string
}
