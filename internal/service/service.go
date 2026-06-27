package service

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	Token           string   `json:"token"`
	Name            string   `json:"name"`
	RootPath        string   `json:"rootPath"`
	AssetType       string   `json:"assetType"`
	ExcludePatterns []string `json:"excludePatterns"`
	ScanWorkers     int      `json:"scanWorkers"`
}

type ScanSettings struct {
	ScanWorkers int `json:"scanWorkers"`
}

type ScanRequest struct {
	Path        string `json:"path"`
	Mode        string `json:"mode"`
	RecentLimit int    `json:"recentLimit"`
}

type PageImageOptions struct {
	MaxWidth int
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
	return s.CreateLibraryWithOptions(input.Name, input.RootPath, input.AssetType, input.ExcludePatterns)
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
	return s.CreateLibraryWithOptions(name, rootPath, assetType, nil)
}

func (s *Service) CreateLibraryWithOptions(name string, rootPath string, assetType string, excludePatterns []string) (domain.Library, error) {
	name = strings.TrimSpace(name)
	rootPath = strings.TrimSpace(rootPath)
	if rootPath == "" {
		return domain.Library{}, fmt.Errorf("library root path is required")
	}
	if name == "" {
		name = rootPath
	}
	return s.store.CreateLibraryWithOptions(name, rootPath, normalizeLibraryAssetType(assetType), excludePatterns)
}

func (s *Service) UpdateLibraryExcludePatterns(id int64, excludePatterns []string) (domain.Library, error) {
	return s.store.UpdateLibraryExcludePatterns(id, excludePatterns)
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
	targetPath := filepath.Clean(lib.RootPath)
	if existing, err := s.store.RunningScanJobByLibraryTarget(lib.ID, targetPath); err == nil {
		return existing, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return domain.ScanJob{}, err
	}
	return s.scanner.StartScanJob(lib)
}

func (s *Service) ScanLibraryPath(id int64, targetPath string) (domain.ScanJob, error) {
	lib, err := s.store.LibraryByID(id)
	if err != nil {
		return domain.ScanJob{}, err
	}
	targetPath, err = normalizeScanTargetPath(lib, targetPath)
	if err != nil {
		return domain.ScanJob{}, err
	}
	if existing, err := s.store.RunningScanJobByLibraryTarget(lib.ID, targetPath); err == nil {
		return existing, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return domain.ScanJob{}, err
	}
	return s.scanner.StartScanJobPath(lib, targetPath)
}

func (s *Service) ScanLibraryRecent(id int64, targetPath string, limit int) (domain.ScanJob, error) {
	lib, err := s.store.LibraryByID(id)
	if err != nil {
		return domain.ScanJob{}, err
	}
	targetPath, err = normalizeScanTargetPath(lib, targetPath)
	if err != nil {
		return domain.ScanJob{}, err
	}
	limit = scanner.NormalizeRecentScanLimit(limit)
	targetLabel := fmt.Sprintf("%s [recent:%d]", filepath.Clean(targetPath), limit)
	if existing, err := s.store.RunningScanJobByLibraryTarget(lib.ID, targetLabel); err == nil {
		return existing, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return domain.ScanJob{}, err
	}
	return s.scanner.StartRecentScanJobPath(lib, targetPath, limit)
}

func normalizeScanTargetPath(library domain.Library, targetPath string) (string, error) {
	targetPath = strings.TrimSpace(targetPath)
	if targetPath == "" {
		return filepath.Clean(library.RootPath), nil
	}
	if !filepath.IsAbs(targetPath) {
		targetPath = filepath.Join(library.RootPath, targetPath)
	}
	targetPath = filepath.Clean(targetPath)
	rootPath := filepath.Clean(library.RootPath)
	relPath, err := filepath.Rel(rootPath, targetPath)
	if err != nil {
		return "", fmt.Errorf("resolve target path: %w", err)
	}
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) || filepath.IsAbs(relPath) {
		return "", fmt.Errorf("target path is outside library root: %s", targetPath)
	}
	if _, err := os.Stat(targetPath); err != nil {
		return "", err
	}
	return targetPath, nil
}

func (s *Service) ListSeries() ([]domain.Series, error) {
	return s.ListSeriesForProfile(0)
}

func (s *Service) ListSeriesForProfile(profileID int64) ([]domain.Series, error) {
	series, err := s.store.ListSeriesForProfile(profileID)
	if err != nil {
		return nil, err
	}
	gameCollections, err := s.store.ListGamePlatformCollections()
	if err != nil {
		return nil, err
	}
	return append(series, gameCollections...), nil
}

func (s *Service) ListSeriesForProfileLimit(profileID int64, limit int) ([]domain.Series, error) {
	series, err := s.store.ListSeriesForProfileLimit(profileID, limit)
	if err != nil {
		return nil, err
	}
	gameCollections, err := s.store.ListGamePlatformCollections()
	if err != nil {
		return nil, err
	}
	return append(series, gameCollections...), nil
}

func (s *Service) ListSeriesPageForProfile(profileID int64, options domain.CollectionListOptions) (domain.CollectionListPage, error) {
	return s.store.ListSeriesPageForProfile(profileID, options)
}

func (s *Service) UpdateCollectionPrivateStateForProfile(seriesID int64, profileID int64, state domain.CollectionPrivateState) (domain.Series, error) {
	profileID, err := s.store.ResolveProfileID(profileID)
	if err != nil {
		return domain.Series{}, err
	}
	if err := s.store.UpdateCollectionPrivateStateForProfile(seriesID, profileID, state); err != nil {
		return domain.Series{}, err
	}
	return s.store.SeriesByIDForProfile(seriesID, profileID)
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

func (s *Service) ListManualCollections() ([]domain.ManualCollection, error) {
	return s.store.ListManualCollections()
}

func (s *Service) CreateManualCollection(collection domain.ManualCollection) (domain.ManualCollection, error) {
	return s.store.CreateManualCollection(collection)
}

func (s *Service) UpdateManualCollection(collectionID int64, collection domain.ManualCollection) (domain.ManualCollection, error) {
	return s.store.UpdateManualCollection(collectionID, collection)
}

func (s *Service) DeleteManualCollection(collectionID int64) error {
	return s.store.DeleteManualCollection(collectionID)
}

func (s *Service) ManualCollectionDetails(collectionID int64) (domain.ManualCollectionDetails, error) {
	collection, err := s.store.ManualCollectionByID(collectionID)
	if err != nil {
		return domain.ManualCollectionDetails{}, err
	}
	items, err := s.store.ListManualCollectionItems(collectionID)
	if err != nil {
		return domain.ManualCollectionDetails{}, err
	}
	resolved := make([]domain.ManualCollectionItem, 0, len(items))
	for _, item := range items {
		resolvedItem, err := s.resolveManualCollectionItem(item)
		if err != nil {
			continue
		}
		resolved = append(resolved, resolvedItem)
	}
	collection.ItemCount = int64(len(resolved))
	return domain.ManualCollectionDetails{Collection: collection, Items: resolved}, nil
}

func (s *Service) AddManualCollectionItem(collectionID int64, item domain.ManualCollectionItem) (domain.ManualCollection, error) {
	item.AssetType = normalizeManualCollectionAssetType(item.AssetType)
	if item.AssetType == "" {
		return domain.ManualCollection{}, fmt.Errorf("unsupported manual collection asset type")
	}
	if err := s.ensureManualCollectionAssetExists(item); err != nil {
		return domain.ManualCollection{}, err
	}
	if err := s.store.AddManualCollectionItem(collectionID, item); err != nil {
		return domain.ManualCollection{}, err
	}
	return s.store.ManualCollectionByID(collectionID)
}

func (s *Service) RemoveManualCollectionItem(collectionID int64, assetType string, assetID int64) (domain.ManualCollection, error) {
	assetType = normalizeManualCollectionAssetType(assetType)
	if assetType == "" {
		return domain.ManualCollection{}, fmt.Errorf("unsupported manual collection asset type")
	}
	if err := s.store.RemoveManualCollectionItem(collectionID, assetType, assetID); err != nil {
		return domain.ManualCollection{}, err
	}
	return s.store.ManualCollectionByID(collectionID)
}

func (s *Service) ensureManualCollectionAssetExists(item domain.ManualCollectionItem) error {
	switch item.AssetType {
	case "book":
		_, err := s.store.BookByID(item.AssetID)
		return err
	case "game":
		_, err := s.store.GameByID(item.AssetID)
		return err
	case "video":
		_, err := s.store.VideoByID(item.AssetID)
		return err
	default:
		return fmt.Errorf("unsupported manual collection asset type")
	}
}

func (s *Service) resolveManualCollectionItem(item domain.ManualCollectionItem) (domain.ManualCollectionItem, error) {
	item.AssetType = normalizeManualCollectionAssetType(item.AssetType)
	switch item.AssetType {
	case "book":
		book, err := s.store.BookByID(item.AssetID)
		if err != nil {
			return domain.ManualCollectionItem{}, err
		}
		item.Title = book.Title
		item.Subtitle = strings.TrimSpace(strings.Join([]string{book.CollectionTitle, strings.ToUpper(book.Format)}, " · "))
		item.CoverURL = fmt.Sprintf("/api/books/%d/thumbnail?size=small&v=%s", book.ID, ThumbnailClientCacheVersion())
		item.ManifestURL = fmt.Sprintf("/api/client/books/%d/manifest", book.ID)
	case "game":
		game, err := s.store.GameByID(item.AssetID)
		if err != nil {
			return domain.ManualCollectionItem{}, err
		}
		item.Title = game.Title
		item.Subtitle = strings.TrimSpace(strings.Join(nonEmptyStrings(game.Platform, game.ROMSetName, strings.ToUpper(game.Format)), " · "))
		item.CoverURL = fmt.Sprintf("/api/games/%d/cover", game.ID)
		item.ManifestURL = fmt.Sprintf("/api/client/games/%d/manifest", game.ID)
	case "video":
		video, err := s.store.VideoByID(item.AssetID)
		if err != nil {
			return domain.ManualCollectionItem{}, err
		}
		item.Title = video.Title
		item.Subtitle = strings.ToUpper(video.Format)
		item.CoverURL = fmt.Sprintf("/api/videos/%d/thumbnail?v=%d", video.ID, video.MTime.UnixNano())
		item.ManifestURL = fmt.Sprintf("/api/client/videos/%d/manifest", video.ID)
	default:
		return domain.ManualCollectionItem{}, fmt.Errorf("unsupported manual collection asset type")
	}
	return item, nil
}

func normalizeManualCollectionAssetType(assetType string) string {
	switch strings.ToLower(strings.TrimSpace(assetType)) {
	case "book", "comic":
		return "book"
	case "game":
		return "game"
	case "video":
		return "video"
	default:
		return ""
	}
}

func nonEmptyStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
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
	books, err := s.store.ListContinueReading(shelfFetchLimit(limit))
	if err != nil {
		return nil, err
	}
	return filterCurrentBookFiles(books, limit), nil
}

func (s *Service) ContinueReadingForProfile(profileID int64, limit int) ([]domain.Book, error) {
	books, err := s.store.ListContinueReadingForProfile(profileID, shelfFetchLimit(limit))
	if err != nil {
		return nil, err
	}
	return filterCurrentBookFiles(books, limit), nil
}

func (s *Service) RecentBooks(limit int) ([]domain.Book, error) {
	books, err := s.store.ListRecentBooks(shelfFetchLimit(limit))
	if err != nil {
		return nil, err
	}
	return filterCurrentBookFiles(books, limit), nil
}

func (s *Service) RecentBooksForProfile(profileID int64, limit int) ([]domain.Book, error) {
	books, err := s.store.ListRecentBooksForProfile(profileID, shelfFetchLimit(limit))
	if err != nil {
		return nil, err
	}
	return filterCurrentBookFiles(books, limit), nil
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

func (s *Service) ListGamesPageForProfile(options domain.GameListOptions, profileID int64) (domain.GameListPage, error) {
	return s.store.ListGamesPageForProfile(options, profileID)
}

func (s *Service) ExportGameGamelistXML(options domain.GameListOptions) ([]byte, error) {
	options.Limit = 200
	options.Offset = 0
	options.Sort = "title"

	var out bytes.Buffer
	out.WriteString(xml.Header)
	out.WriteString("<gameList>\n")
	for {
		page, err := s.store.ListGamesPage(options)
		if err != nil {
			return nil, err
		}
		for _, game := range page.Items {
			writeGamelistGame(&out, game, options.BasePath)
		}
		if !page.HasMore || len(page.Items) == 0 {
			break
		}
		options.Offset += len(page.Items)
	}
	out.WriteString("</gameList>\n")
	return out.Bytes(), nil
}

func writeGamelistGame(out *bytes.Buffer, game domain.GameAsset, basePath string) {
	out.WriteString("  <game>\n")
	writeGamelistElement(out, "path", gamelistDraftPath(game, basePath))
	writeGamelistElement(out, "name", game.Title)
	out.WriteString("  </game>\n")
}

func writeGamelistElement(out *bytes.Buffer, tag string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	out.WriteString("    <")
	out.WriteString(tag)
	out.WriteString(">")
	_ = xml.EscapeText(out, []byte(value))
	out.WriteString("</")
	out.WriteString(tag)
	out.WriteString(">\n")
}

func gamelistDraftPath(game domain.GameAsset, basePath string) string {
	relPath := strings.TrimSpace(game.RelPath)
	if relPath == "" {
		relPath = filepath.Base(game.FilePath)
	}
	relPath = strings.TrimPrefix(filepath.ToSlash(relPath), "./")
	basePath = strings.Trim(strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(basePath)), "./"), "/")
	if basePath != "" {
		prefix := basePath + "/"
		if strings.HasPrefix(strings.ToLower(relPath), strings.ToLower(prefix)) {
			relPath = relPath[len(prefix):]
		}
	}
	return "./" + relPath
}

func (s *Service) ListVideosPage(options domain.VideoListOptions) (domain.VideoListPage, error) {
	return s.store.ListVideosPage(options)
}

func (s *Service) Game(id int64) (domain.GameAsset, error) {
	return s.store.GameByID(id)
}

func (s *Service) GameForProfile(id int64, profileID int64) (domain.GameAsset, error) {
	return s.store.GameByIDForProfile(id, profileID)
}

func (s *Service) UpdateGamePrivateStateForProfile(gameID int64, profileID int64, state domain.GamePrivateState) (domain.GameAsset, error) {
	if err := s.store.UpdateGamePrivateStateForProfile(gameID, profileID, state); err != nil {
		return domain.GameAsset{}, err
	}
	return s.store.GameByIDForProfile(gameID, profileID)
}

func (s *Service) GameDetails(id int64) (domain.GameDetails, error) {
	return s.store.GameDetails(id)
}

func (s *Service) GameMetadataProviders() []domain.GameMetadataProviderStatus {
	return []domain.GameMetadataProviderStatus{
		{
			ID:           "gamelist",
			Name:         "ES-DE/Batocera gamelist.xml",
			Enabled:      true,
			Configured:   true,
			Free:         true,
			Network:      false,
			Capabilities: []string{"metadata", "artwork", "manuals"},
			Message:      "Imported from local gamelist.xml files during scans.",
		},
		{
			ID:           "libretro",
			Name:         "Libretro Thumbnails",
			Enabled:      true,
			Configured:   true,
			Free:         true,
			Network:      true,
			Capabilities: []string{"artwork"},
			Message:      "Used for artwork fallback when local covers are unavailable.",
		},
		credentialedGameMetadataProvider("hasheous", "Hasheous", false, "", []string{"hash-match", "metadata", "artwork"}, "Free hash metadata lookup; no API key is required for basic use."),
		credentialedGameMetadataProvider("igdb", "IGDB", true, "FOLIOSPACE_IGDB_CLIENT_ID", []string{"metadata", "artwork"}, "Requires Twitch/IGDB client credentials before it can be enabled."),
		credentialedGameMetadataProvider("rawg", "RAWG", true, "FOLIOSPACE_RAWG_API_KEY", []string{"metadata", "artwork"}, "Requires a RAWG API key and upstream usage compliance."),
		credentialedGameMetadataProvider("screenscraper", "ScreenScraper", true, "FOLIOSPACE_SCREENSCRAPER_USER", []string{"metadata", "artwork", "manuals"}, "Requires ScreenScraper account credentials before it can be enabled."),
		credentialedGameMetadataProvider("steamgriddb", "SteamGridDB", true, "FOLIOSPACE_STEAMGRIDDB_API_KEY", []string{"artwork"}, "Requires a SteamGridDB API key before it can be enabled."),
		{
			ID:                  "mobygames",
			Name:                "MobyGames",
			Enabled:             false,
			Configured:          false,
			RequiresCredentials: true,
			Free:                false,
			Network:             true,
			Capabilities:        []string{"metadata", "artwork"},
			Message:             "Not enabled by default because the upstream API is paid.",
		},
	}
}

func credentialedGameMetadataProvider(id string, name string, requiresCredentials bool, envKey string, capabilities []string, message string) domain.GameMetadataProviderStatus {
	configured := true
	if envKey != "" {
		configured = strings.TrimSpace(os.Getenv(envKey)) != ""
	}
	return domain.GameMetadataProviderStatus{
		ID:                  id,
		Name:                name,
		Enabled:             !requiresCredentials || configured,
		Configured:          configured,
		RequiresCredentials: requiresCredentials,
		Free:                true,
		Network:             true,
		Capabilities:        capabilities,
		Message:             message,
	}
}

func (s *Service) RefreshGameMetadata(id int64) (domain.GameMetadataActionResult, error) {
	details, err := s.store.GameDetails(id)
	if err != nil {
		return domain.GameMetadataActionResult{}, err
	}
	return domain.GameMetadataActionResult{
		GameID:         id,
		Action:         "refresh",
		Status:         "completed",
		Message:        "Built-in local providers are available; credentialed network providers are reported but not called unless configured.",
		MetadataStatus: details.MetadataStatus,
		Sources:        details.Sources,
		Providers:      s.GameMetadataProviders(),
	}, nil
}

func (s *Service) SelectGameMetadataMatch(id int64, source string, sourceID string) (domain.GameMetadataActionResult, error) {
	source = strings.TrimSpace(source)
	sourceID = strings.TrimSpace(sourceID)
	if source == "" || sourceID == "" {
		return domain.GameMetadataActionResult{}, fmt.Errorf("source and sourceId are required")
	}
	details, err := s.store.GameDetails(id)
	if err != nil {
		return domain.GameMetadataActionResult{}, err
	}
	var selected domain.GameMetadataSource
	for _, item := range details.Sources {
		if item.Source == source && item.SourceID == sourceID {
			selected = item
			break
		}
	}
	if selected.GameID == 0 {
		return domain.GameMetadataActionResult{}, fmt.Errorf("metadata source %s/%s not found for game %d", source, sourceID, id)
	}
	selected.MatchedBy = "manual"
	selected.Confidence = 1
	if _, err := s.store.UpsertGameMetadataSource(selected); err != nil {
		return domain.GameMetadataActionResult{}, err
	}
	details, err = s.store.GameDetails(id)
	if err != nil {
		return domain.GameMetadataActionResult{}, err
	}
	return domain.GameMetadataActionResult{
		GameID:         id,
		Action:         "select-match",
		Status:         "completed",
		Message:        "Selected existing metadata source as the manual match.",
		MetadataStatus: details.MetadataStatus,
		Sources:        details.Sources,
		Providers:      s.GameMetadataProviders(),
	}, nil
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
	if stream, ok := s.openSelectedGameCover(id); ok {
		return stream, nil
	}
	for _, candidate := range localGameCoverCandidates(game.FilePath) {
		file, err := os.Open(candidate)
		if err == nil {
			return PageStream{Body: file, ContentType: localImageContentType(candidate)}, nil
		}
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
		if err := gameCoverDownloader(sourceURL, cachePath); err == nil {
			_, _ = s.store.UpsertGameArtwork(domain.GameArtwork{
				GameID:     game.ID,
				Source:     "libretro",
				Kind:       "cover",
				URL:        sourceURL,
				CachePath:  cachePath,
				Selected:   true,
				Confidence: 1,
			})
			file, err := os.Open(cachePath)
			if err != nil {
				return PageStream{}, err
			}
			return PageStream{Body: file, ContentType: "image/png"}, nil
		}
	}
	return PageStream{}, fmt.Errorf("game cover not found")
}

func (s *Service) openSelectedGameCover(id int64) (PageStream, bool) {
	details, err := s.store.GameDetails(id)
	if err != nil {
		return PageStream{}, false
	}
	for _, artwork := range details.Artwork {
		if !artwork.Selected || artwork.Kind != "cover" || strings.TrimSpace(artwork.CachePath) == "" {
			continue
		}
		file, contentType, err := openImageFile(artwork.CachePath)
		if err == nil {
			return PageStream{Body: file, ContentType: contentType}, true
		}
	}
	return PageStream{}, false
}

func localGameCoverCandidates(gamePath string) []string {
	if strings.TrimSpace(gamePath) == "" {
		return nil
	}
	dir := filepath.Dir(gamePath)
	base := strings.TrimSuffix(filepath.Base(gamePath), filepath.Ext(gamePath))
	names := []string{
		"boxFront.jpg",
		"boxFront.jpeg",
		"boxFront.png",
		"boxFront.webp",
		"boxfront.jpg",
		"boxfront.jpeg",
		"boxfront.png",
		"boxfront.webp",
		"BoxFront.jpg",
		"BoxFront.jpeg",
		"BoxFront.png",
		"BoxFront.webp",
	}
	mediaBases := gameCoverMediaBaseCandidates(base)
	candidates := make([]string, 0, len(names)*len(mediaBases))
	seen := map[string]bool{}
	for _, mediaBase := range mediaBases {
		for _, name := range names {
			path := filepath.Join(dir, "media", mediaBase, name)
			if !seen[path] {
				seen[path] = true
				candidates = append(candidates, path)
			}
		}
	}
	return candidates
}

func gameCoverMediaBaseCandidates(base string) []string {
	candidates := []string{base}
	if len(base) > 1 {
		switch base[len(base)-1] {
		case 'A', 'B', 'C', 'D':
			candidates = append(candidates, base[:len(base)-1])
		}
	}
	return candidates
}

func localImageContentType(path string) string {
	if value := mime.TypeByExtension(strings.ToLower(filepath.Ext(path))); strings.HasPrefix(value, "image/") {
		return value
	}
	return "image/jpeg"
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
	books, err := s.store.ListFavoriteBooks(shelfFetchLimit(limit))
	if err != nil {
		return nil, err
	}
	return filterCurrentBookFiles(books, limit), nil
}

func (s *Service) FavoriteBooksForProfile(profileID int64, limit int) ([]domain.Book, error) {
	books, err := s.store.ListFavoriteBooksForProfile(profileID, shelfFetchLimit(limit))
	if err != nil {
		return nil, err
	}
	return filterCurrentBookFiles(books, limit), nil
}

func (s *Service) BooksByPrivateStatus(status string, limit int) ([]domain.Book, error) {
	books, err := s.store.ListBooksByPrivateStatus(status, shelfFetchLimit(limit))
	if err != nil {
		return nil, err
	}
	return filterCurrentBookFiles(books, limit), nil
}

func (s *Service) BooksByPrivateStatusForProfile(profileID int64, status string, limit int) ([]domain.Book, error) {
	books, err := s.store.ListBooksByPrivateStatusForProfile(profileID, status, shelfFetchLimit(limit))
	if err != nil {
		return nil, err
	}
	return filterCurrentBookFiles(books, limit), nil
}

func shelfFetchLimit(limit int) int {
	normalized := shelfLimit(limit)
	fetch := normalized * 4
	if fetch < 24 {
		return 24
	}
	if fetch > 100 {
		return 100
	}
	return fetch
}

func shelfLimit(limit int) int {
	if limit <= 0 {
		return 12
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func filterCurrentBookFiles(books []domain.Book, limit int) []domain.Book {
	maxItems := shelfLimit(limit)
	out := make([]domain.Book, 0, min(maxItems, len(books)))
	for _, book := range books {
		if !bookFileMatchesIndex(book) {
			continue
		}
		out = append(out, book)
		if len(out) >= maxItems {
			break
		}
	}
	return out
}

func bookFileMatchesIndex(book domain.Book) bool {
	if strings.TrimSpace(book.FilePath) == "" || book.FileSize <= 0 || book.FileMTime.IsZero() {
		return false
	}
	info, err := os.Stat(book.FilePath)
	if err != nil || info.IsDir() {
		return false
	}
	if info.Size() != book.FileSize {
		return false
	}
	return absDuration(info.ModTime().Sub(book.FileMTime)) <= time.Second
}

func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
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
	listing, err := cachedLibretroListing(playlist, "Named_Boxarts")
	if err == nil && len(listing) > 0 {
		if out := libretroBoxartCandidatesFromListing(game, listing); len(out) > 0 {
			return out
		}
	}
	return libretroFallbackBoxartCandidates(game, playlist)
}

func libretroBoxartCandidatesFromListing(game domain.GameAsset, listing []string) []string {
	return libretroArtworkCandidatesFromListing(game, "Named_Boxarts", listing)
}

func libretroArtworkCandidatesFromListing(game domain.GameAsset, artFolder string, listing []string) []string {
	playlist, ok := libretroPlaylist(game.Platform)
	if !ok || strings.TrimSpace(game.Title) == "" {
		return nil
	}
	match := findLibretroArtworkMatch(libretroArtworkSearchNames(game), listing)
	if match == "" {
		return nil
	}
	return []string{libretroArtworkURL(playlist, artFolder, match)}
}

func libretroFallbackBoxartCandidates(game domain.GameAsset, playlist string) []string {
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
		out = append(out, libretroArtworkURL(playlist, "Named_Boxarts", name+".png"))
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
	case "nds", "ds":
		return "Nintendo - Nintendo DS", true
	case "genesis", "mega-drive", "megadrive":
		return "Sega - Mega Drive - Genesis", true
	case "ps1", "psx", "playstation":
		return "Sony - PlayStation", true
	case "psp":
		return "Sony - PlayStation Portable", true
	default:
		return "", false
	}
}

func libretroArtworkSearchNames(game domain.GameAsset) []string {
	title := strings.TrimSpace(game.Title)
	if title == "" {
		return nil
	}
	names := []string{}
	names = append(names, libretroArchiveSearchNames(game)...)
	if game.Region != "" {
		names = append(names, fmt.Sprintf("%s (%s)", title, strings.TrimSpace(game.Region)))
	}
	names = append(names, title)
	if game.Region == "" {
		names = append(names, title+" (USA)", title+" (World)", title+" (Japan)")
	}
	out := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		name = sanitizeLibretroName(name)
		key := strings.ToLower(name)
		if name != "" && !seen[key] {
			seen[key] = true
			out = append(out, name)
		}
	}
	return out
}

func libretroArchiveSearchNames(game domain.GameAsset) []string {
	if strings.ToLower(strings.TrimSpace(game.Format)) != "zip" || strings.TrimSpace(game.FilePath) == "" {
		return nil
	}
	reader, err := zip.OpenReader(game.FilePath)
	if err != nil {
		return nil
	}
	defer reader.Close()

	out := []string{}
	for _, file := range reader.File {
		name := strings.TrimSpace(file.Name)
		if name == "" || file.FileInfo().IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		switch ext {
		case ".sfc", ".smc", ".fig", ".swc", ".nes", ".gb", ".gbc", ".gba", ".md", ".gen", ".bin":
			base := strings.TrimSpace(strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)))
			out = append(out, libretroExpandedRegionNames(base)...)
		}
		if len(out) > 0 {
			break
		}
	}
	return out
}

func libretroExpandedRegionNames(name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	candidates := []string{name}
	regionNames := map[string]string{
		"J":  "Japan",
		"U":  "USA",
		"E":  "Europe",
		"W":  "World",
		"A":  "Asia",
		"K":  "Korea",
		"F":  "France",
		"G":  "Germany",
		"S":  "Spain",
		"I":  "Italy",
		"JU": "Japan, USA",
		"UE": "USA, Europe",
	}
	for code, region := range regionNames {
		short := "(" + code + ")"
		if strings.Contains(name, short) {
			candidates = append(candidates, strings.ReplaceAll(name, short, "("+region+")"))
		}
	}
	clean := regexp.MustCompile(`\s*\[[^]]*]`).ReplaceAllString(name, "")
	if clean != name {
		candidates = append(candidates, clean)
		for code, region := range regionNames {
			short := "(" + code + ")"
			if strings.Contains(clean, short) {
				candidates = append(candidates, strings.ReplaceAll(clean, short, "("+region+")"))
			}
		}
	}

	if base, regions := libretroGoodSetNameParts(clean); base != "" {
		for _, region := range regions {
			candidates = append(candidates, fmt.Sprintf("%s (%s)", base, region))
		}
		candidates = append(candidates, base)
	}
	return candidates
}

func libretroGoodSetNameParts(name string) (string, []string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil
	}
	first := strings.Index(name, "(")
	if first <= 0 {
		return "", nil
	}
	base := strings.TrimSpace(name[:first])
	tags := regexp.MustCompile(`\(([^)]*)\)`).FindAllStringSubmatch(name[first:], -1)
	if base == "" || len(tags) == 0 {
		return "", nil
	}
	regionNames := map[string]string{
		"J":       "Japan",
		"U":       "USA",
		"E":       "Europe",
		"W":       "World",
		"A":       "Asia",
		"K":       "Korea",
		"F":       "France",
		"G":       "Germany",
		"S":       "Spain",
		"I":       "Italy",
		"JU":      "Japan, USA",
		"UE":      "USA, Europe",
		"Japan":   "Japan",
		"USA":     "USA",
		"Europe":  "Europe",
		"World":   "World",
		"Asia":    "Asia",
		"Korea":   "Korea",
		"France":  "France",
		"Germany": "Germany",
		"Spain":   "Spain",
		"Italy":   "Italy",
	}
	regions := []string{}
	seen := map[string]bool{}
	for _, tag := range tags {
		parts := strings.Split(tag[1], ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if region := regionNames[part]; region != "" && !seen[region] {
				seen[region] = true
				regions = append(regions, region)
			}
		}
	}
	return base, regions
}

func findLibretroArtworkMatch(targets []string, listing []string) string {
	for _, target := range targets {
		target = strings.TrimSuffix(target, filepath.Ext(target))
		for _, filename := range listing {
			if strings.EqualFold(strings.TrimSuffix(filename, filepath.Ext(filename)), target) {
				return filename
			}
		}
	}
	for _, target := range targets {
		targetTokens := libretroMatchTokens(target)
		if len(targetTokens) == 0 {
			continue
		}
		best := ""
		bestScore := 0.0
		for _, filename := range listing {
			score := libretroTokenScore(targetTokens, libretroMatchTokens(filename))
			if score > bestScore {
				bestScore = score
				best = filename
			}
		}
		if bestScore >= 0.85 {
			return best
		}
	}
	return ""
}

func libretroTokenScore(target map[string]bool, candidate map[string]bool) float64 {
	if len(target) == 0 || len(candidate) == 0 {
		return 0
	}
	matches := 0
	for token := range target {
		if candidate[token] {
			matches++
		}
	}
	coverage := float64(matches) / float64(len(target))
	if coverage < 1 {
		return coverage * 0.8
	}
	extra := len(candidate) - matches
	if extra <= 0 {
		return 1
	}
	return 1 - (float64(extra) * 0.03)
}

func libretroMatchTokens(name string) map[string]bool {
	name = strings.TrimSuffix(name, filepath.Ext(name))
	name = stripBracketedTags(name)
	name = strings.ToLower(name)
	replacer := strings.NewReplacer("&", " ", "_", " ", "-", " ", ",", " ", ".", " ", ":", " ", "'", " ", `"`, " ", "/", " ")
	name = replacer.Replace(name)
	out := map[string]bool{}
	for _, token := range strings.Fields(name) {
		if token != "" {
			out[token] = true
		}
	}
	return out
}

func stripBracketedTags(value string) string {
	var out strings.Builder
	depth := 0
	for _, r := range value {
		switch r {
		case '(', '[':
			depth++
		case ')', ']':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				out.WriteRune(r)
			}
		}
	}
	return strings.TrimSpace(out.String())
}

func libretroArtworkURL(playlist string, artFolder string, filename string) string {
	return fmt.Sprintf("https://thumbnails.libretro.com/%s/%s/%s", urlPathEscape(playlist), urlPathEscape(artFolder), urlPathEscape(filename))
}

func sanitizeLibretroName(name string) string {
	replacer := strings.NewReplacer("&", "_", "*", "_", ":", "_", "`", "_", "<", "_", ">", "_", "?", "_", `\`, "_", "|", "_", `"`, "_")
	return strings.TrimSpace(replacer.Replace(name))
}

func urlPathEscape(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

var libretroListingHrefPattern = regexp.MustCompile(`(?i)href="([^"]+)"`)

type libretroListingCacheEntry struct {
	filenames []string
	expires   time.Time
}

var libretroListingCache = struct {
	sync.Mutex
	items map[string]libretroListingCacheEntry
}{items: map[string]libretroListingCacheEntry{}}

var libretroListingFetcher = fetchLibretroListing

func cachedLibretroListing(playlist string, artFolder string) ([]string, error) {
	key := playlist + "\x00" + artFolder
	now := time.Now()
	libretroListingCache.Lock()
	if entry, ok := libretroListingCache.items[key]; ok && now.Before(entry.expires) {
		out := append([]string(nil), entry.filenames...)
		libretroListingCache.Unlock()
		return out, nil
	}
	libretroListingCache.Unlock()

	filenames, err := libretroListingFetcher(playlist, artFolder)
	if err != nil {
		return nil, err
	}
	libretroListingCache.Lock()
	libretroListingCache.items[key] = libretroListingCacheEntry{
		filenames: append([]string(nil), filenames...),
		expires:   now.Add(6 * time.Hour),
	}
	libretroListingCache.Unlock()
	return filenames, nil
}

func fetchLibretroListing(playlist string, artFolder string) ([]string, error) {
	sourceURL := fmt.Sprintf("https://thumbnails.libretro.com/%s/%s/", urlPathEscape(playlist), urlPathEscape(artFolder))
	client := http.Client{Timeout: 8 * time.Second}
	response, err := client.Get(sourceURL)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("listing source returned %d", response.StatusCode)
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	return parseLibretroListingFilenames(string(data)), nil
}

func parseLibretroListingFilenames(html string) []string {
	matches := libretroListingHrefPattern.FindAllStringSubmatch(html, -1)
	out := make([]string, 0, len(matches))
	seen := map[string]bool{}
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		href := strings.TrimSpace(match[1])
		if href == "" || strings.HasSuffix(href, "/") || strings.HasPrefix(href, "../") {
			continue
		}
		filename, err := url.PathUnescape(href)
		if err != nil {
			filename = href
		}
		ext := strings.ToLower(filepath.Ext(filename))
		switch ext {
		case ".png", ".jpg", ".jpeg", ".webp":
		default:
			continue
		}
		if !seen[filename] {
			seen[filename] = true
			out = append(out, filename)
		}
	}
	return out
}

var gameCoverDownloader = downloadGameCover

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
	return s.store.ListPages(id)
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
	return s.OpenPageWithOptions(bookID, pageIndex, PageImageOptions{})
}

func (s *Service) OpenPageWithOptions(bookID int64, pageIndex int, options PageImageOptions) (PageStream, error) {
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
	if options.MaxWidth <= 0 || !strings.HasPrefix(contentType, "image/") {
		return PageStream{Body: body, ContentType: contentType}, nil
	}
	return downsamplePageStream(body, contentType, options.MaxWidth)
}

func downsamplePageStream(body io.ReadCloser, contentType string, maxWidth int) (PageStream, error) {
	defer body.Close()

	data, err := io.ReadAll(body)
	if err != nil {
		return PageStream{}, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return PageStream{Body: io.NopCloser(bytes.NewReader(data)), ContentType: contentType}, nil
	}
	bounds := img.Bounds()
	srcWidth := bounds.Dx()
	srcHeight := bounds.Dy()
	if srcWidth <= 0 || srcHeight <= 0 || srcWidth <= maxWidth {
		return PageStream{Body: io.NopCloser(bytes.NewReader(data)), ContentType: contentType}, nil
	}

	dstWidth := maxWidth
	dstHeight := max(1, int(float64(srcHeight)*float64(dstWidth)/float64(srcWidth)))
	dst := image.NewRGBA(image.Rect(0, 0, dstWidth, dstHeight))
	for y := 0; y < dstHeight; y++ {
		srcY := bounds.Min.Y + y*srcHeight/dstHeight
		for x := 0; x < dstWidth; x++ {
			srcX := bounds.Min.X + x*srcWidth/dstWidth
			dst.Set(x, y, blendOverWhite(img.At(srcX, srcY)))
		}
	}

	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 86}); err != nil {
		return PageStream{}, err
	}
	return PageStream{Body: io.NopCloser(bytes.NewReader(out.Bytes())), ContentType: "image/jpeg"}, nil
}

func blendOverWhite(c color.Color) color.RGBA {
	r, g, b, a := c.RGBA()
	if a == 0xffff {
		return color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 0xff}
	}
	alpha := float64(a) / 65535
	return color.RGBA{
		R: uint8(float64(r>>8)*alpha + 255*(1-alpha)),
		G: uint8(float64(g>>8)*alpha + 255*(1-alpha)),
		B: uint8(float64(b>>8)*alpha + 255*(1-alpha)),
		A: 0xff,
	}
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

func (s *Service) SaveWebtoonReadingPositionForProfile(bookID int64, profileID int64, position domain.ReadingPosition) (domain.ReadingPosition, error) {
	position.Schema = strings.TrimSpace(position.Schema)
	if position.Schema == "" {
		position.Schema = domain.WebtoonPositionSchema
	}
	if position.Schema != domain.WebtoonPositionSchema {
		return domain.ReadingPosition{}, fmt.Errorf("unsupported reading position schema %q", position.Schema)
	}
	if position.PageIndex < 0 {
		position.PageIndex = 0
	}
	position.PageKey = strings.TrimSpace(position.PageKey)
	position.PageYOffsetRatio = clampUnit(position.PageYOffsetRatio)
	position.DocumentProgress = clampUnit(position.DocumentProgress)
	if position.ViewportAnchorRatio <= 0 {
		position.ViewportAnchorRatio = 0.28
	}
	position.ViewportAnchorRatio = clampUnit(position.ViewportAnchorRatio)
	if position.PageCount < 0 {
		position.PageCount = 0
	}
	return s.store.SaveReadingPositionForProfile(bookID, profileID, "webtoon", position)
}

func (s *Service) ReadingPositionsForProfile(bookID int64, profileID int64) (map[string]domain.ReadingPosition, error) {
	return s.store.ReadingPositionsForProfile(bookID, profileID)
}

func (s *Service) Progress(bookID int64) (domain.ReadProgress, error) {
	return s.store.Progress(bookID)
}

func (s *Service) ProgressForProfile(bookID int64, profileID int64) (domain.ReadProgress, error) {
	return s.store.ProgressForProfile(bookID, profileID)
}

func clampUnit(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
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
