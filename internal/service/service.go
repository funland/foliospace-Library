package service

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"foliospace-reader/internal/archive"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/scanner"
	"foliospace-reader/internal/store"
)

type Service struct {
	store     *store.Store
	scanner   *scanner.Scanner
	configDir string
}

func New(store *store.Store) *Service {
	return NewWithConfig(store, "")
}

func NewWithConfig(store *store.Store, configDir string) *Service {
	return &Service{
		store:     store,
		scanner:   scanner.New(store),
		configDir: strings.TrimSpace(configDir),
	}
}

func (s *Service) CreateLibrary(name string, rootPath string) (domain.Library, error) {
	return s.CreateLibraryWithType(name, rootPath, "mixed")
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
	roots, err := s.directoryRoots()
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

func (s *Service) directoryRoots() ([]domain.DirectoryEntry, error) {
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
	if oneOf(value, "mixed", "book", "comic", "game") {
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
	books, err := s.store.ListBooks(seriesID)
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

func (s *Service) SearchBooks(query string, limit int) ([]domain.Book, error) {
	return s.store.SearchBooks(query, limit)
}

func (s *Service) UpdateBookPrivateState(bookID int64, state domain.BookPrivateState) (domain.Book, error) {
	state.Status = strings.TrimSpace(state.Status)
	state.Summary = strings.TrimSpace(state.Summary)
	if state.Rating < 0 {
		state.Rating = 0
	}
	if state.Rating > 5 {
		state.Rating = 5
	}
	if err := s.store.UpdateBookPrivateState(bookID, state); err != nil {
		return domain.Book{}, err
	}
	return s.store.BookByID(bookID)
}

func (s *Service) ClientPreferences() (domain.ClientPreferences, error) {
	return s.store.ClientPreferences()
}

func (s *Service) SaveClientPreferences(prefs domain.ClientPreferences) (domain.ClientPreferences, error) {
	prefs = normalizeClientPreferences(prefs)
	if err := s.store.SaveClientPreferences(prefs); err != nil {
		return domain.ClientPreferences{}, err
	}
	return s.store.ClientPreferences()
}

func normalizeClientPreferences(prefs domain.ClientPreferences) domain.ClientPreferences {
	if !oneOf(prefs.Locale, "zh", "zht", "en", "ja", "ko") {
		prefs.Locale = "zh"
	}
	if !oneOf(prefs.ReaderPageMode, "single", "double") {
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

func (s *Service) RecentBooks(limit int) ([]domain.Book, error) {
	return s.store.ListRecentBooks(limit)
}

func (s *Service) RecentGames(limit int) ([]domain.GameAsset, error) {
	return s.store.ListRecentGames(limit)
}

func (s *Service) ListGamesPage(options domain.GameListOptions) (domain.GameListPage, error) {
	return s.store.ListGamesPage(options)
}

func (s *Service) Game(id int64) (domain.GameAsset, error) {
	return s.store.GameByID(id)
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

func (s *Service) FavoriteBooks(limit int) ([]domain.Book, error) {
	return s.store.ListFavoriteBooks(limit)
}

func (s *Service) BooksByPrivateStatus(status string, limit int) ([]domain.Book, error) {
	return s.store.ListBooksByPrivateStatus(status, limit)
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

func (s *Service) Book(id int64) (domain.Book, error) {
	return s.store.BookByID(id)
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
	return s.OpenPage(bookID, 0)
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
	if progressFraction < 0 {
		progressFraction = 0
	}
	if progressFraction > 1 {
		progressFraction = 1
	}
	return s.store.SaveProgressDetail(bookID, pageIndex, locator, progressFraction)
}

func (s *Service) Progress(bookID int64) (domain.ReadProgress, error) {
	return s.store.Progress(bookID)
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
