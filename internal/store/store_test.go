package store

import (
	"testing"
	"time"

	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
)

func TestStorePersistsLibraryBookProgressAndErrors(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	s := New(conn)
	lib, err := s.CreateLibrary("Comics", "/library")
	if err != nil {
		t.Fatal(err)
	}
	series, err := s.UpsertSeries(lib.ID, "Series A", "Series A")
	if err != nil {
		t.Fatal(err)
	}
	book, err := s.UpsertBook(series.ID, "Book 1", "cbz")
	if err != nil {
		t.Fatal(err)
	}
	file, err := s.UpsertFile(book.ID, lib.ID, "/library/Series A/Book 1.cbz", "Series A/Book 1.cbz", 100, time.Unix(10, 0), ".cbz")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.ReplacePages(book.ID, []domain.Page{{Index: 0, Name: "001.jpg"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveProgressDetail(book.ID, 4, "", 0.4); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordFileError(domain.FileErrorInput{
		LibraryID: lib.ID,
		BookID:    book.ID,
		FileID:    file.ID,
		Path:      file.AbsPath,
		Code:      domain.ErrorEmptyFile,
		Message:   "empty file",
	}); err != nil {
		t.Fatal(err)
	}

	libraries, err := s.ListLibraries()
	if err != nil {
		t.Fatal(err)
	}
	if len(libraries) != 1 {
		t.Fatalf("libraries len = %d, want 1", len(libraries))
	}
	seriesList, err := s.ListSeries()
	if err != nil {
		t.Fatal(err)
	}
	if len(seriesList) != 1 || seriesList[0].DirectoryPath != "Series A" || seriesList[0].CollectionType != "directory" {
		t.Fatalf("series list = %#v, want directory collection at Series A", seriesList)
	}

	progress, err := s.Progress(book.ID)
	if err != nil {
		t.Fatal(err)
	}
	if progress.PageIndex != 4 {
		t.Fatalf("progress = %d, want 4", progress.PageIndex)
	}
	continueBooks, err := s.ListContinueReading(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(continueBooks) != 1 || continueBooks[0].CurrentPage != 4 || continueBooks[0].ProgressFraction != 0.4 {
		t.Fatalf("continue books = %#v, want saved progress", continueBooks)
	}
	recentBooks, err := s.ListRecentBooks(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recentBooks) != 1 || recentBooks[0].CollectionTitle != "Series A" || recentBooks[0].AddedAt.IsZero() {
		t.Fatalf("recent books = %#v, want collection title and added time", recentBooks)
	}

	errors, err := s.ListFileErrors()
	if err != nil {
		t.Fatal(err)
	}
	if len(errors) != 1 || errors[0].Code != domain.ErrorEmptyFile {
		t.Fatalf("errors = %#v, want one empty_file", errors)
	}
}

func TestStorePersistsClientPreferences(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	s := New(conn)
	defaults, err := s.ClientPreferences()
	if err != nil {
		t.Fatal(err)
	}
	if defaults.Locale != "zh" || defaults.ReaderPageMode != "single" || defaults.EPUBTheme != "light" || defaults.EPUBFontSize != 18 {
		t.Fatalf("default preferences = %#v, want zh single light 18", defaults)
	}

	want := domain.ClientPreferences{
		Locale:         "ko",
		ReaderPageMode: "double",
		EPUBPageMode:   "double",
		EPUBTheme:      "dark",
		EPUBFontSize:   24,
	}
	if err := s.SaveClientPreferences(want); err != nil {
		t.Fatal(err)
	}

	got, err := s.ClientPreferences()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("preferences = %#v, want %#v", got, want)
	}
}

func TestStoreCreatesDefaultProfileAndIsolatesProfileState(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	s := New(conn)
	defaultProfile, err := s.DefaultProfile()
	if err != nil {
		t.Fatal(err)
	}
	if defaultProfile.ID == 0 || !defaultProfile.IsDefault || defaultProfile.Name == "" {
		t.Fatalf("default profile = %#v, want named default profile", defaultProfile)
	}
	guestProfile, err := s.CreateProfile("Guest", "comic", "amber")
	if err != nil {
		t.Fatal(err)
	}
	if guestProfile.ID == defaultProfile.ID || guestProfile.IsDefault {
		t.Fatalf("guest profile = %#v, want non-default profile distinct from %#v", guestProfile, defaultProfile)
	}

	lib, err := s.CreateLibrary("Comics", "/library")
	if err != nil {
		t.Fatal(err)
	}
	series, err := s.UpsertSeries(lib.ID, "Series A", "Series A")
	if err != nil {
		t.Fatal(err)
	}
	book, err := s.UpsertBook(series.ID, "Book 1", "cbz")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.SaveProgressDetailForProfile(book.ID, defaultProfile.ID, 3, "page:3", 0.3); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveProgressDetailForProfile(book.ID, guestProfile.ID, 8, "page:8", 0.8); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateBookPrivateStateForProfile(book.ID, defaultProfile.ID, domain.BookPrivateState{Status: "reading", Favorite: true, Rating: 4, Tags: []string{"me"}, Summary: "mine"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateBookPrivateStateForProfile(book.ID, guestProfile.ID, domain.BookPrivateState{Status: "want", Favorite: false, Rating: 2, Tags: []string{"guest"}, Summary: "guest note"}); err != nil {
		t.Fatal(err)
	}

	defaultProgress, err := s.ProgressForProfile(book.ID, defaultProfile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if defaultProgress.PageIndex != 3 || defaultProgress.Locator != "page:3" || defaultProgress.ProgressFraction != 0.3 {
		t.Fatalf("default progress = %#v, want profile-specific progress", defaultProgress)
	}
	guestProgress, err := s.ProgressForProfile(book.ID, guestProfile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if guestProgress.PageIndex != 8 || guestProgress.Locator != "page:8" || guestProgress.ProgressFraction != 0.8 {
		t.Fatalf("guest progress = %#v, want separate profile-specific progress", guestProgress)
	}

	defaultBook, err := s.BookByIDForProfile(book.ID, defaultProfile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if defaultBook.PrivateStatus != "reading" || !defaultBook.Favorite || defaultBook.Rating != 4 || defaultBook.Summary != "mine" || defaultBook.CurrentPage != 3 {
		t.Fatalf("default book = %#v, want default profile state", defaultBook)
	}
	guestBook, err := s.BookByIDForProfile(book.ID, guestProfile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if guestBook.PrivateStatus != "want" || guestBook.Favorite || guestBook.Rating != 2 || guestBook.Summary != "guest note" || guestBook.CurrentPage != 8 {
		t.Fatalf("guest book = %#v, want guest profile state", guestBook)
	}

	defaultFavorites, err := s.ListFavoriteBooksForProfile(defaultProfile.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(defaultFavorites) != 1 || defaultFavorites[0].ID != book.ID {
		t.Fatalf("default favorites = %#v, want favorite book", defaultFavorites)
	}
	guestFavorites, err := s.ListFavoriteBooksForProfile(guestProfile.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(guestFavorites) != 0 {
		t.Fatalf("guest favorites = %#v, want no favorites", guestFavorites)
	}
}

func TestStoreScopesClientPreferencesByProfile(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	s := New(conn)
	defaultProfile, err := s.DefaultProfile()
	if err != nil {
		t.Fatal(err)
	}
	guestProfile, err := s.CreateProfile("Guest", "comic", "amber")
	if err != nil {
		t.Fatal(err)
	}

	defaultPrefs := domain.ClientPreferences{Locale: "en", ReaderPageMode: "single", EPUBPageMode: "single", EPUBTheme: "light", EPUBFontSize: 18}
	guestPrefs := domain.ClientPreferences{Locale: "ja", ReaderPageMode: "webtoon", EPUBPageMode: "double", EPUBTheme: "dark", EPUBFontSize: 22}
	if err := s.SaveClientPreferencesForProfile(defaultProfile.ID, defaultPrefs); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveClientPreferencesForProfile(guestProfile.ID, guestPrefs); err != nil {
		t.Fatal(err)
	}

	gotDefault, err := s.ClientPreferencesForProfile(defaultProfile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotDefault != defaultPrefs {
		t.Fatalf("default prefs = %#v, want %#v", gotDefault, defaultPrefs)
	}
	gotGuest, err := s.ClientPreferencesForProfile(guestProfile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotGuest != guestPrefs {
		t.Fatalf("guest prefs = %#v, want %#v", gotGuest, guestPrefs)
	}
}

func TestStoreRequestsScanJobControl(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	s := New(conn)
	lib, err := s.CreateLibrary("Comics", "/library")
	if err != nil {
		t.Fatal(err)
	}
	job, err := s.StartScanJob(lib.ID)
	if err != nil {
		t.Fatal(err)
	}

	paused, err := s.RequestScanJobPause(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if paused.Status != "pause_requested" {
		t.Fatalf("pause status = %q, want pause_requested", paused.Status)
	}

	cancelled, err := s.RequestScanJobCancel(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != "cancel_requested" {
		t.Fatalf("cancel status = %q, want cancel_requested", cancelled.Status)
	}
}

func TestStoreCancelsInterruptedScanJobs(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	s := New(conn)
	lib, err := s.CreateLibrary("Comics", "/library")
	if err != nil {
		t.Fatal(err)
	}
	running, err := s.StartScanJob(lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	cancelRequested, err := s.StartScanJob(lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequestScanJobCancel(cancelRequested.ID); err != nil {
		t.Fatal(err)
	}
	completed, err := s.StartScanJob(lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	completed.Status = "completed"
	completed.FinishedAt = time.Now()
	if err := s.UpdateScanJob(completed); err != nil {
		t.Fatal(err)
	}

	count, err := s.CancelInterruptedScanJobs()
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	for _, id := range []int64{running.ID, cancelRequested.ID} {
		job, err := s.ScanJobByID(id)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status != "cancelled" || job.FinishedAt.IsZero() {
			t.Fatalf("job %d = %#v, want cancelled with finished_at", id, job)
		}
		events, err := s.ListJobEvents(id)
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 1 || events[0].Level != "warn" {
			t.Fatalf("events for job %d = %#v, want cleanup warning", id, events)
		}
	}
	gotCompleted, err := s.ScanJobByID(completed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotCompleted.Status != "completed" {
		t.Fatalf("completed job status = %q, want completed", gotCompleted.Status)
	}
}

func TestStorePersistsAndListsGameAssets(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	s := New(conn)
	lib, err := s.CreateLibrary("Games", "/library")
	if err != nil {
		t.Fatal(err)
	}
	game, err := s.UpsertGame(domain.GameAsset{
		LibraryID:     lib.ID,
		Title:         "Super Mario World",
		Platform:      "snes",
		Format:        "sfc",
		FilePath:      "/library/SNES/Super Mario World.sfc",
		RelPath:       "SNES/Super Mario World.sfc",
		Size:          1024,
		MTime:         time.Unix(20, 0),
		CRC32:         "b19ed489",
		SHA1:          "0123456789abcdef0123456789abcdef01234567",
		Region:        "USA",
		ROMSetName:    "No-Intro",
		EmulatorHint:  "snes",
		Compatibility: "unknown",
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.GameByID(game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Super Mario World" || got.Platform != "snes" || got.CRC32 != "b19ed489" || got.SHA1 == "" {
		t.Fatalf("game = %#v, want persisted game metadata", got)
	}

	recent, err := s.ListRecentGames(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 || recent[0].ID != game.ID || recent[0].FilePath == "" {
		t.Fatalf("recent games = %#v, want indexed game with internal path", recent)
	}
}

func TestStoreListsGamesPageWithFiltersAndSort(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	s := New(conn)
	lib, err := s.CreateLibrary("Games", "/library")
	if err != nil {
		t.Fatal(err)
	}
	seedGames := []domain.GameAsset{
		{LibraryID: lib.ID, Title: "Super Contra", Platform: "nes", ROMSetName: "NES", Region: "Japan", Format: "nes", FilePath: "/library/nes/super-contra.nes", RelPath: "nes/super-contra.nes", Size: 262160, MTime: time.Unix(30, 0), CRC32: "9bb6059e", SHA1: "5de393e3ad83e6e185e6d338684d7a4475b7d2ce", EmulatorHint: "nes", Compatibility: "unknown"},
		{LibraryID: lib.ID, Title: "Advance Wars", Platform: "gba", ROMSetName: "GBA", Region: "USA", Format: "gba", FilePath: "/library/gba/advance-wars.gba", RelPath: "gba/advance-wars.gba", Size: 1024, MTime: time.Unix(31, 0), CRC32: "11111111", SHA1: "1111111111111111111111111111111111111111", EmulatorHint: "gba", Compatibility: "unknown"},
		{LibraryID: lib.ID, Title: "Metal Slug", Platform: "arcade", ROMSetName: "MAME", Region: "World", Format: "zip", FilePath: "/library/arcade/mslug.zip", RelPath: "arcade/mslug.zip", Size: 2048, MTime: time.Unix(32, 0), CRC32: "22222222", SHA1: "2222222222222222222222222222222222222222", EmulatorHint: "arcade", Compatibility: "unknown"},
	}
	for _, game := range seedGames {
		if _, err := s.UpsertGame(game); err != nil {
			t.Fatal(err)
		}
	}

	page, err := s.ListGamesPage(domain.GameListOptions{Limit: 2, Offset: 0, Sort: "title"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Total != 3 || !page.HasMore || page.Items[0].Title != "Advance Wars" || page.Limit != 2 {
		t.Fatalf("page = %#v, want title-sorted first page with total and hasMore", page)
	}

	filtered, err := s.ListGamesPage(domain.GameListOptions{Limit: 50, Query: "japan", Platform: "nes", Format: "nes"})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Items) != 1 || filtered.Items[0].Title != "Super Contra" || filtered.HasMore {
		t.Fatalf("filtered page = %#v, want Super Contra only", filtered)
	}
}

func TestStorePersistsAndListsVideoAssets(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	s := New(conn)
	lib, err := s.CreateLibraryWithType("Movies", "/library", "video")
	if err != nil {
		t.Fatal(err)
	}
	video, err := s.UpsertVideo(domain.VideoAsset{
		LibraryID:       lib.ID,
		Title:           "Demo Movie",
		Format:          "mp4",
		FilePath:        "/library/Movies/Demo Movie.mp4",
		RelPath:         "Movies/Demo Movie.mp4",
		Size:            4096,
		MTime:           time.Unix(40, 0),
		DurationSeconds: 120.5,
		Width:           1920,
		Height:          1080,
		VideoCodec:      "h264",
		AudioCodec:      "aac",
		ThumbnailStatus: "placeholder",
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.VideoByID(video.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Demo Movie" || got.Format != "mp4" || got.Width != 1920 || got.FilePath == "" {
		t.Fatalf("video = %#v, want persisted video metadata", got)
	}

	recent, err := s.ListRecentVideos(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 || recent[0].ID != video.ID {
		t.Fatalf("recent videos = %#v, want indexed video", recent)
	}

	page, err := s.ListVideosPage(domain.VideoListOptions{Limit: 1, Query: "demo", Format: "mp4", Sort: "title"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Total != 1 || page.HasMore {
		t.Fatalf("video page = %#v, want one matching video", page)
	}

	hevcMP4, err := s.UpsertVideo(domain.VideoAsset{
		LibraryID:       lib.ID,
		Title:           "Escape from the 21st Century 2024 2160p WEB-DL H265 HQ AAC",
		Format:          "mp4",
		FilePath:        "/library/Movies/Escape from the 21st Century 2024 2160p WEB-DL H265 HQ AAC.mp4",
		RelPath:         "Movies/Escape from the 21st Century 2024 2160p WEB-DL H265 HQ AAC.mp4",
		Size:            8192,
		MTime:           time.Unix(41, 0),
		ThumbnailStatus: "placeholder",
	})
	if err != nil {
		t.Fatal(err)
	}
	hevcMP4, err = s.VideoByID(hevcMP4.ID)
	if err != nil {
		t.Fatal(err)
	}
	if hevcMP4.DirectPlayable || hevcMP4.PlaybackMode != "hls" {
		t.Fatalf("hevc-named mp4 playback = directPlayable %v mode %q, want hls", hevcMP4.DirectPlayable, hevcMP4.PlaybackMode)
	}
}

func TestStoreListsBooksPageWithSearchAndSort(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	s := New(conn)
	lib, err := s.CreateLibrary("Comics", "/library")
	if err != nil {
		t.Fatal(err)
	}
	series, err := s.UpsertSeries(lib.ID, "Series A", "Series A")
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"Alpha", "Beta", "Gamma", "Alphabet"} {
		book, err := s.UpsertBook(series.ID, title, "cbz")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.UpsertFile(book.ID, lib.ID, "/library/Series A/"+title+".cbz", "Series A/"+title+".cbz", 100, time.Now(), ".cbz"); err != nil {
			t.Fatal(err)
		}
	}

	page, err := s.ListBooksPage(domain.BookListOptions{
		SeriesID: series.ID,
		Limit:    2,
		Offset:   1,
		Query:    "alpha",
		Sort:     "title",
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || page.Limit != 2 || page.Offset != 1 || page.HasMore {
		t.Fatalf("page metadata = %#v, want total 2 offset 1 limit 2 hasMore false", page)
	}
	if len(page.Items) != 1 || page.Items[0].Title != "Alphabet" {
		t.Fatalf("page items = %#v, want Alphabet as second alpha match", page.Items)
	}

	recent, err := s.ListBooksPage(domain.BookListOptions{
		SeriesID: series.ID,
		Limit:    2,
		Sort:     "recently_added",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(recent.Items) != 2 || recent.Items[0].Title != "Alphabet" || recent.Items[1].Title != "Gamma" {
		t.Fatalf("recent items = %#v, want newest books first", recent.Items)
	}
	if recent.Total != 4 || !recent.HasMore {
		t.Fatalf("recent metadata = %#v, want total 4 and hasMore", recent)
	}

	empty, err := s.ListBooksPage(domain.BookListOptions{
		SeriesID: series.ID,
		Limit:    2,
		Query:    "missing",
	})
	if err != nil {
		t.Fatal(err)
	}
	if empty.Items == nil || len(empty.Items) != 0 || empty.Total != 0 {
		t.Fatalf("empty page = %#v, want empty non-nil items", empty)
	}
}

func TestStoreSearchesBooksAndPersistsPrivateState(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	s := New(conn)
	lib, err := s.CreateLibrary("Comics", "/library")
	if err != nil {
		t.Fatal(err)
	}
	seriesA, err := s.UpsertSeries(lib.ID, "Cyberpunk", "Cyberpunk")
	if err != nil {
		t.Fatal(err)
	}
	seriesB, err := s.UpsertSeries(lib.ID, "Quiet Drama", "Quiet Drama")
	if err != nil {
		t.Fatal(err)
	}
	bookA, err := s.UpsertBook(seriesA.ID, "Neon City", "cbz")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertBook(seriesB.ID, "Winter Notes", "epub"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertFile(bookA.ID, lib.ID, "/library/Cyberpunk/Neon City.cbz", "Cyberpunk/Neon City.cbz", 100, time.Now(), ".cbz"); err != nil {
		t.Fatal(err)
	}

	state := domain.BookPrivateState{
		Status:   "reading",
		Favorite: true,
		Rating:   5,
		Tags:     []string{"noir", "vision"},
		Summary:  "Private note",
	}
	if err := s.UpdateBookPrivateState(bookA.ID, state); err != nil {
		t.Fatal(err)
	}

	book, err := s.BookByID(bookA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if book.PrivateStatus != "reading" || !book.Favorite || book.Rating != 5 || book.Summary != "Private note" {
		t.Fatalf("book private state = %#v, want persisted state", book)
	}
	if len(book.Tags) != 2 || book.Tags[0] != "noir" || book.Tags[1] != "vision" {
		t.Fatalf("book tags = %#v, want stored tags", book.Tags)
	}

	tagResults, err := s.SearchBooks("vision", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(tagResults) != 1 || tagResults[0].ID != bookA.ID {
		t.Fatalf("tag search = %#v, want Neon City", tagResults)
	}

	collectionResults, err := s.SearchBooks("quiet", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(collectionResults) != 1 || collectionResults[0].Title != "Winter Notes" {
		t.Fatalf("collection search = %#v, want Winter Notes", collectionResults)
	}
}

func TestStoreListsPrivateShelves(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	s := New(conn)
	lib, err := s.CreateLibrary("Comics", "/library")
	if err != nil {
		t.Fatal(err)
	}
	series, err := s.UpsertSeries(lib.ID, "Series A", "Series A")
	if err != nil {
		t.Fatal(err)
	}
	wantBook, err := s.UpsertBook(series.ID, "Want Book", "cbz")
	if err != nil {
		t.Fatal(err)
	}
	favoriteBook, err := s.UpsertBook(series.ID, "Favorite Book", "epub")
	if err != nil {
		t.Fatal(err)
	}
	finishedBook, err := s.UpsertBook(series.ID, "Finished Book", "cbz")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateBookPrivateState(wantBook.ID, domain.BookPrivateState{Status: "want"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateBookPrivateState(favoriteBook.ID, domain.BookPrivateState{Status: "reading", Favorite: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateBookPrivateState(finishedBook.ID, domain.BookPrivateState{Status: "finished"}); err != nil {
		t.Fatal(err)
	}

	favorites, err := s.ListFavoriteBooks(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(favorites) != 1 || favorites[0].ID != favoriteBook.ID || !favorites[0].Favorite {
		t.Fatalf("favorites = %#v, want favorite book", favorites)
	}

	wantBooks, err := s.ListBooksByPrivateStatus("want", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(wantBooks) != 1 || wantBooks[0].ID != wantBook.ID || wantBooks[0].PrivateStatus != "want" {
		t.Fatalf("want books = %#v, want wanted book", wantBooks)
	}

	finishedBooks, err := s.ListBooksByPrivateStatus("finished", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(finishedBooks) != 1 || finishedBooks[0].ID != finishedBook.ID {
		t.Fatalf("finished books = %#v, want finished book", finishedBooks)
	}
}
