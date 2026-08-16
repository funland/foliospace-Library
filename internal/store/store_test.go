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

func TestReplaceGameLaunchProfilesPreservesGameRoleAcrossPolicies(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	s := New(conn)
	lib, err := s.CreateLibrary("Games", "/games")
	if err != nil {
		t.Fatal(err)
	}
	game, err := s.UpsertGame(domain.GameAsset{
		LibraryID: lib.ID, Title: "Audited Game", Platform: "arcade", ROMSetName: "game",
		Format: "zip", FilePath: "/games/game.zip", RelPath: "game.zip", Size: 1,
		MTime: time.Unix(1, 0), SHA1: "0123456789abcdef0123456789abcdef01234567",
		EmulatorHint: "mame", Compatibility: "unknown", CatalogRole: "needs-curation",
	})
	if err != nil {
		t.Fatal(err)
	}
	profile := domain.GameLaunchProfile{
		GameID: game.ID, ID: "game-windows-mame", Revision: 1, Priority: 1, Status: "ready",
		ClientName: "SpatialEMU.Windows", MinClientVersion: "1.302", ClientPlatform: "windows-x64", Architecture: "x64",
		Runtime:   domain.GameRuntimeDescriptor{ID: "mame", Version: "0.288", ContentSet: "mame-0.288"},
		EntryFile: "game.zip", CanonicalSet: "game",
	}
	readyUpdate := domain.GameLaunchCatalogUpdate{
		GameID: game.ID, Platform: "arcade", ROMSetName: "game", EmulatorHint: "mame", CatalogRole: "game",
	}
	if _, err := s.ReplaceGameLaunchProfiles("mame-0.288-listxml", []domain.GameLaunchProfile{profile}, []domain.GameLaunchCatalogUpdate{readyUpdate}); err != nil {
		t.Fatal(err)
	}

	rejectedUpdate := readyUpdate
	rejectedUpdate.CatalogRole = "needs-curation"
	if _, err := s.ReplaceGameLaunchProfiles("mame-0.287-listxml", nil, []domain.GameLaunchCatalogUpdate{rejectedUpdate}); err != nil {
		t.Fatal(err)
	}
	stored, err := s.GameByID(game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.CatalogRole != "game" {
		t.Fatalf("catalog role=%q, want game while another ready policy remains", stored.CatalogRole)
	}

	if _, err := s.ReplaceGameLaunchProfiles("mame-0.288-listxml", nil, nil); err != nil {
		t.Fatal(err)
	}
	stored, err = s.GameByID(game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.CatalogRole != "needs-curation" {
		t.Fatalf("catalog role=%q, want needs-curation after the last ready policy is removed", stored.CatalogRole)
	}
}

func TestReplaceGameLaunchProfilesNeverPromotesDependencyToGame(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	s := New(conn)
	lib, err := s.CreateLibrary("Games", "/games")
	if err != nil {
		t.Fatal(err)
	}
	device, err := s.UpsertGame(domain.GameAsset{
		LibraryID: lib.ID, Title: "Namco C75", Platform: "arcade", ROMSetName: "namcoc75",
		Format: "zip", FilePath: "/games/namcoc75.zip", RelPath: "namcoc75.zip", Size: 8709,
		MTime: time.Unix(1, 0), SHA1: "0649e27b7d605add7fc4215ee628b71e3c835328",
		EmulatorHint: "fbneo", Compatibility: "unknown", CatalogRole: "dependency",
	})
	if err != nil {
		t.Fatal(err)
	}
	profile := domain.GameLaunchProfile{
		GameID: device.ID, ID: "legacy-device-profile", Revision: 1, Priority: 1, Status: "ready",
		ClientName: "SpatialEMU.Windows", MinClientVersion: "1.302", ClientPlatform: "windows-x64", Architecture: "x64",
		Runtime:   domain.GameRuntimeDescriptor{ID: "mame", Version: "0.288", ContentSet: "mame-0.288"},
		EntryFile: "namcoc75.zip", CanonicalSet: "namcoc75",
	}
	update := domain.GameLaunchCatalogUpdate{
		GameID: device.ID, Platform: "arcade", ROMSetName: "namcoc75", EmulatorHint: "fbneo", CatalogRole: "dependency",
	}
	if _, err := s.ReplaceGameLaunchProfiles("legacy-device", []domain.GameLaunchProfile{profile}, []domain.GameLaunchCatalogUpdate{update}); err != nil {
		t.Fatal(err)
	}
	stored, err := s.GameByID(device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.CatalogRole != "dependency" {
		t.Fatalf("catalog role=%q, want dependency despite a ready profile", stored.CatalogRole)
	}
	if _, err := s.ReplaceGameLaunchProfiles("legacy-device", nil, nil); err != nil {
		t.Fatal(err)
	}
	stored, err = s.GameByID(device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.CatalogRole != "dependency" {
		t.Fatalf("catalog role after policy deletion=%q, want dependency", stored.CatalogRole)
	}
}

func TestReplaceGameLaunchProfilesForGamePreservesOtherGames(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	s := New(conn)
	lib, err := s.CreateLibrary("Games", "/games")
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.UpsertGame(domain.GameAsset{
		LibraryID: lib.ID, Title: "First", Platform: "mame", ROMSetName: "first",
		Format: "zip", FilePath: "/games/first.zip", RelPath: "first.zip", Size: 1,
		MTime: time.Unix(1, 0), SHA1: "0123456789abcdef0123456789abcdef01234567",
		EmulatorHint: "mame", Compatibility: "unknown", CatalogRole: "needs-curation",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.UpsertGame(domain.GameAsset{
		LibraryID: lib.ID, Title: "Second", Platform: "mame", ROMSetName: "second",
		Format: "zip", FilePath: "/games/second.zip", RelPath: "second.zip", Size: 1,
		MTime: time.Unix(1, 0), SHA1: "1123456789abcdef0123456789abcdef01234567",
		EmulatorHint: "mame", Compatibility: "unknown", CatalogRole: "needs-curation",
	})
	if err != nil {
		t.Fatal(err)
	}
	profile := func(game domain.GameAsset, id, client, platform string) domain.GameLaunchProfile {
		return domain.GameLaunchProfile{
			GameID: game.ID, ID: id, Revision: 287, Priority: 200, Status: "ready",
			ClientName: client, MinClientVersion: "1.300", ClientPlatform: platform, Architecture: "arm64",
			Runtime:   domain.GameRuntimeDescriptor{ID: "mame", Version: "0.287", ContentSet: "mame-0.287"},
			EntryFile: game.ROMSetName + ".zip", CanonicalSet: game.ROMSetName,
		}
	}
	update := func(game domain.GameAsset) domain.GameLaunchCatalogUpdate {
		return domain.GameLaunchCatalogUpdate{
			GameID: game.ID, Platform: "mame", ROMSetName: game.ROMSetName, EmulatorHint: "mame", CatalogRole: "game",
		}
	}
	policy := "mame-0.287-listxml"
	if _, err := s.ReplaceGameLaunchProfiles(policy,
		[]domain.GameLaunchProfile{
			profile(first, "first-ipados", "SpatialEMU.iPadOS", "ipados-arm64"),
			profile(second, "second-ipados", "SpatialEMU.iPadOS", "ipados-arm64"),
		},
		[]domain.GameLaunchCatalogUpdate{update(first), update(second)}); err != nil {
		t.Fatal(err)
	}

	if _, err := s.ReplaceGameLaunchProfilesForGame(policy, first.ID,
		[]domain.GameLaunchProfile{profile(first, "first-macos", "SpatialEMU.macOS", "macos-arm64")},
		[]domain.GameLaunchCatalogUpdate{update(first)}); err != nil {
		t.Fatal(err)
	}
	firstProfiles, err := s.GameLaunchProfiles(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstProfiles) != 1 || firstProfiles[0].ID != "first-macos" {
		t.Fatalf("first profiles = %#v", firstProfiles)
	}
	secondProfiles, err := s.GameLaunchProfiles(second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondProfiles) != 1 || secondProfiles[0].ID != "second-ipados" {
		t.Fatalf("second profiles = %#v", secondProfiles)
	}
}

func TestReplaceGameFilesReusesChecksumOnlyForUnchangedSourceIdentity(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	s := New(conn)
	lib, err := s.CreateLibrary("Games", "/games")
	if err != nil {
		t.Fatal(err)
	}
	game, err := s.UpsertGame(domain.GameAsset{
		LibraryID: lib.ID, Title: "Checksum Game", Platform: "psp", ROMSetName: "PSP",
		Format: "iso", FilePath: "/games/game.iso", RelPath: "game.iso", Size: 4,
		MTime: time.Unix(1, 0), SHA1: "0123456789abcdef0123456789abcdef01234567",
		EmulatorHint: "ppsspp", Compatibility: "unknown", CatalogRole: "game",
	})
	if err != nil {
		t.Fatal(err)
	}
	checksum := "89abcdef0123456789abcdef0123456789abcdef"
	file := domain.GameFile{
		GameID: game.ID, Name: "game.iso", FilePath: "/games/game.iso", Size: 4,
		MTime: time.Unix(1, 0), SHA1: checksum, Role: "entry", Position: 0,
	}
	if err := s.ReplaceGameFiles(game.ID, []domain.GameFile{file}); err != nil {
		t.Fatal(err)
	}

	file.SHA1 = ""
	if err := s.ReplaceGameFiles(game.ID, []domain.GameFile{file}); err != nil {
		t.Fatal(err)
	}
	stored, err := s.GameFiles(game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].SHA1 != checksum {
		t.Fatalf("unchanged checksum=%q, want %q", stored[0].SHA1, checksum)
	}

	file.MTime = time.Unix(2, 0)
	if err := s.ReplaceGameFiles(game.ID, []domain.GameFile{file}); err != nil {
		t.Fatal(err)
	}
	stored, err = s.GameFiles(game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].SHA1 != "" {
		t.Fatalf("changed source reused checksum %q", stored[0].SHA1)
	}
	pending, err := s.GameFilesMissingSHA1ForGame(game.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].GameID != game.ID || pending[0].Position != 0 {
		t.Fatalf("targeted pending files = %#v, want the changed game file", pending)
	}
	platformTotal, platformChecksummed, err := s.GameFileChecksumCountsForPlatform("PSP")
	if err != nil {
		t.Fatal(err)
	}
	if platformTotal != 1 || platformChecksummed != 0 {
		t.Fatalf("PSP checksum counts = %d/%d, want 0/1 complete", platformChecksummed, platformTotal)
	}
	platformPending, err := s.GameFilesMissingSHA1ForScope(0, "psp", 0, 32)
	if err != nil {
		t.Fatal(err)
	}
	if len(platformPending) != 1 || platformPending[0].ID != stored[0].ID {
		t.Fatalf("platform pending files = %#v, want the PSP file", platformPending)
	}
	none, err := s.GameFilesMissingSHA1ForScope(0, "snes", 0, 32)
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("unexpected non-PSP pending files = %#v", none)
	}
	if _, err := s.GameFilesMissingSHA1ForGame(0, 10); err == nil {
		t.Fatal("targeted checksum query accepted an invalid game ID")
	}
	if _, err := s.UpdateGameFileSHA1(stored[0], "not-a-sha1"); err == nil {
		t.Fatal("invalid SHA-1 update succeeded")
	}
}

func TestStorePersistsWebtoonReadingPositionPerProfile(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	s := New(conn)
	guestProfile, err := s.CreateProfile("Guest", "", "")
	if err != nil {
		t.Fatal(err)
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

	defaultPosition := domain.ReadingPosition{
		Schema:              domain.WebtoonPositionSchema,
		PageIndex:           12,
		PageKey:             "archive:0012.webp",
		PageYOffsetRatio:    0.25,
		ViewportAnchorRatio: 0.28,
		DocumentProgress:    0.33,
		PageCount:           80,
	}
	guestPosition := domain.ReadingPosition{
		Schema:              domain.WebtoonPositionSchema,
		PageIndex:           40,
		PageKey:             "archive:0040.webp",
		PageYOffsetRatio:    0.75,
		ViewportAnchorRatio: 0.28,
		DocumentProgress:    0.66,
		PageCount:           80,
	}
	if _, err := s.SaveReadingPositionForProfile(book.ID, defaultProfileID, "webtoon", defaultPosition); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveReadingPositionForProfile(book.ID, guestProfile.ID, "webtoon", guestPosition); err != nil {
		t.Fatal(err)
	}

	defaultPositions, err := s.ReadingPositionsForProfile(book.ID, defaultProfileID)
	if err != nil {
		t.Fatal(err)
	}
	guestPositions, err := s.ReadingPositionsForProfile(book.ID, guestProfile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if defaultPositions["webtoon"].PageKey != "archive:0012.webp" || defaultPositions["webtoon"].PageYOffsetRatio != 0.25 {
		t.Fatalf("default positions = %#v, want default webtoon anchor", defaultPositions)
	}
	if guestPositions["webtoon"].PageKey != "archive:0040.webp" || guestPositions["webtoon"].PageYOffsetRatio != 0.75 {
		t.Fatalf("guest positions = %#v, want guest webtoon anchor", guestPositions)
	}

	progress, err := s.ProgressForProfile(book.ID, guestProfile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if progress.PageIndex != 40 || progress.ProgressFraction != 0.66 || progress.Locator != "webtoon:0.66" {
		t.Fatalf("legacy progress = %#v, want page/document progress sync with webtoon locator fallback", progress)
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

func TestStoreManagesThumbnailJobs(t *testing.T) {
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

	first, err := s.EnqueueThumbnailJob(domain.ThumbnailJobInput{
		BookID:   book.ID,
		Size:     "small",
		CacheKey: "book-1-v1-small",
		Priority: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := s.EnqueueThumbnailJob(domain.ThumbnailJobInput{
		BookID:   book.ID,
		Size:     "small",
		CacheKey: "book-1-v1-small",
		Priority: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.ID != first.ID {
		t.Fatalf("duplicate job id = %d, want %d", duplicate.ID, first.ID)
	}
	if err := s.EnqueueThumbnailJobs([]domain.ThumbnailJobInput{
		{BookID: book.ID, Size: "small", CacheKey: "book-1-v1-small", Priority: 1},
		{BookID: book.ID, Size: "medium", CacheKey: "book-1-v1-medium", Priority: 100},
	}); err != nil {
		t.Fatal(err)
	}
	high, err := s.EnqueueThumbnailJob(domain.ThumbnailJobInput{
		BookID:   book.ID,
		Size:     "medium",
		CacheKey: "book-1-v1-medium",
		Priority: 100,
	})
	if err != nil {
		t.Fatal(err)
	}

	status, err := s.ThumbnailQueueStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status.Queued != 2 || status.Running != 0 || status.Ready != 0 || status.Failed != 0 {
		t.Fatalf("status after enqueue = %#v, want two queued jobs", status)
	}

	claimed, ok, err := s.ClaimNextThumbnailJob()
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("claim returned no job")
	}
	if claimed.ID != high.ID || claimed.Status != "running" {
		t.Fatalf("claimed = %#v, want high priority running job %#v", claimed, high)
	}
	if err := s.CompleteThumbnailJob(claimed.ID, "/cache/high.jpg", "image/jpeg", 320, 440, 12345); err != nil {
		t.Fatal(err)
	}
	requeued, err := s.EnqueueThumbnailJob(domain.ThumbnailJobInput{
		BookID:   book.ID,
		Size:     "medium",
		CacheKey: "book-1-v1-medium",
		Priority: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if requeued.ID != high.ID || requeued.Status != "queued" {
		t.Fatalf("requeued ready job = %#v, want same job queued again", requeued)
	}
	if err := s.CompleteThumbnailJob(requeued.ID, "/cache/high.jpg", "image/jpeg", 320, 440, 12345); err != nil {
		t.Fatal(err)
	}

	claimed, ok, err = s.ClaimNextThumbnailJob()
	if err != nil {
		t.Fatal(err)
	}
	if !ok || claimed.ID != first.ID {
		t.Fatalf("second claimed = %#v ok=%v, want first job", claimed, ok)
	}
	if err := s.FailThumbnailJob(claimed.ID, "decode failed"); err != nil {
		t.Fatal(err)
	}

	cancelled, err := s.CancelQueuedThumbnailJobs()
	if err != nil {
		t.Fatal(err)
	}
	if cancelled != 0 {
		t.Fatalf("cancelled = %d, want no queued jobs", cancelled)
	}
	status, err = s.ThumbnailQueueStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status.Ready != 1 || status.Failed != 1 || status.Queued != 0 || status.Running != 0 {
		t.Fatalf("final status = %#v, want ready=1 failed=1", status)
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

func TestStoreScopesCollectionPrivateStateByProfile(t *testing.T) {
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
	lib, err := s.CreateLibrary("Comics", "/library")
	if err != nil {
		t.Fatal(err)
	}
	series, err := s.UpsertSeries(lib.ID, "Series A", "Series A")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateCollectionPrivateStateForProfile(series.ID, defaultProfile.ID, domain.CollectionPrivateState{Favorite: true, Liked: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateCollectionPrivateStateForProfile(series.ID, guestProfile.ID, domain.CollectionPrivateState{Favorite: false, Liked: true}); err != nil {
		t.Fatal(err)
	}

	defaultSeries, err := s.SeriesByIDForProfile(series.ID, defaultProfile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !defaultSeries.Favorite || !defaultSeries.Liked {
		t.Fatalf("default series = %#v, want favorite and liked", defaultSeries)
	}
	guestSeries, err := s.SeriesByIDForProfile(series.ID, guestProfile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if guestSeries.Favorite || !guestSeries.Liked {
		t.Fatalf("guest series = %#v, want liked only", guestSeries)
	}
}

func TestUpdateBookIdentityCarriesCollectionPrivateState(t *testing.T) {
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
	guestProfile, err := s.CreateProfile("Guest", "comic", "teal")
	if err != nil {
		t.Fatal(err)
	}
	lib, err := s.CreateLibrary("Comics", "/library")
	if err != nil {
		t.Fatal(err)
	}
	oldSeries, err := s.UpsertSeries(lib.ID, "Old Series", "Old Series")
	if err != nil {
		t.Fatal(err)
	}
	newSeries, err := s.UpsertSeries(lib.ID, "New Series", "New Series")
	if err != nil {
		t.Fatal(err)
	}
	book, err := s.UpsertBook(oldSeries.ID, "Book A", "cbz")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateCollectionPrivateStateForProfile(oldSeries.ID, defaultProfile.ID, domain.CollectionPrivateState{Favorite: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateCollectionPrivateStateForProfile(oldSeries.ID, guestProfile.ID, domain.CollectionPrivateState{Liked: true}); err != nil {
		t.Fatal(err)
	}

	if _, err := s.UpdateBookIdentity(book.ID, newSeries.ID, book.Title, book.Format); err != nil {
		t.Fatal(err)
	}

	defaultSeries, err := s.SeriesByIDForProfile(newSeries.ID, defaultProfile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !defaultSeries.Favorite || defaultSeries.Liked {
		t.Fatalf("default new series = %#v, want carried favorite only", defaultSeries)
	}
	guestSeries, err := s.SeriesByIDForProfile(newSeries.ID, guestProfile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if guestSeries.Favorite || !guestSeries.Liked {
		t.Fatalf("guest new series = %#v, want carried liked only", guestSeries)
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

func TestStorePersistsGameMetadataDetails(t *testing.T) {
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

	if err := s.UpsertGameMetadata(domain.GameMetadata{
		GameID:       game.ID,
		DisplayTitle: "Super Mario World",
		Summary:      "Dinosaur Land platform adventure.",
		ReleaseDate:  "1990-11-21",
		Genres:       []string{"Platform"},
		Developers:   []string{"Nintendo EAD"},
		Publishers:   []string{"Nintendo"},
		Players:      "1-2",
		Rating:       9.3,
		ExternalLinks: []string{
			"https://example.invalid/smw",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertGameMetadataSource(domain.GameMetadataSource{
		GameID:     game.ID,
		Source:     "gamelist",
		SourceID:   "snes/smw",
		MatchedBy:  "manual",
		Confidence: 1,
		RawJSON:    `{"name":"Super Mario World"}`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertGameArtwork(domain.GameArtwork{
		GameID:     game.ID,
		Source:     "gamelist",
		Kind:       "cover",
		URL:        "/api/games/1/cover",
		CachePath:  "cache/game-covers/1.png",
		Width:      600,
		Height:     800,
		Selected:   true,
		Confidence: 1,
	}); err != nil {
		t.Fatal(err)
	}

	details, err := s.GameDetails(game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if details.MetadataStatus != "matched" || details.Metadata.DisplayTitle != "Super Mario World" || details.Metadata.Genres[0] != "Platform" {
		t.Fatalf("details metadata = %#v, want matched metadata", details)
	}
	if len(details.Sources) != 1 || details.Sources[0].Source != "gamelist" || details.Sources[0].RawJSON == "" {
		t.Fatalf("details sources = %#v, want persisted gamelist source", details.Sources)
	}
	if len(details.Artwork) != 1 || !details.Artwork[0].Selected || details.Artwork[0].Kind != "cover" {
		t.Fatalf("details artwork = %#v, want selected cover artwork", details.Artwork)
	}
}

func TestStoreGameDetailsReportsUnmatchedWhenMetadataIsEmpty(t *testing.T) {
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
		Title:         "Unknown Game",
		Platform:      "gba",
		Format:        "gba",
		FilePath:      "/library/GBA/Unknown Game.gba",
		RelPath:       "GBA/Unknown Game.gba",
		Size:          1024,
		MTime:         time.Unix(21, 0),
		CRC32:         "11111111",
		SHA1:          "1111111111111111111111111111111111111111",
		EmulatorHint:  "gba",
		Compatibility: "unknown",
	})
	if err != nil {
		t.Fatal(err)
	}

	details, err := s.GameDetails(game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if details.MetadataStatus != "unmatched" || details.Metadata.GameID != game.ID || len(details.Sources) != 0 || len(details.Artwork) != 0 {
		t.Fatalf("details = %#v, want unmatched empty metadata state", details)
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
		{LibraryID: lib.ID, Title: "Metal Slug", Platform: "arcade", ROMSetName: "MAME", Region: "World", Format: "zip", FilePath: "/library/arcade/mslug.zip", RelPath: "arcade/mslug.zip", Size: 2048, MTime: time.Unix(32, 0), CRC32: "22222222", SHA1: "2222222222222222222222222222222222222222", EmulatorHint: "arcade", Compatibility: "unknown", CatalogRole: "needs-curation"},
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

	romSet, err := s.ListGamesPage(domain.GameListOptions{Limit: 50, ROMSetName: "GBA"})
	if err != nil {
		t.Fatal(err)
	}
	if len(romSet.Items) != 1 || romSet.Items[0].Title != "Advance Wars" {
		t.Fatalf("rom set page = %#v, want Advance Wars only", romSet)
	}

	curation, err := s.ListGamesPage(domain.GameListOptions{Limit: 50, CatalogRole: "needs-curation"})
	if err != nil {
		t.Fatal(err)
	}
	if len(curation.Items) != 1 || curation.Items[0].Title != "Metal Slug" {
		t.Fatalf("curation page = %#v, want Metal Slug only", curation)
	}

	allAdministrative, err := s.ListGamesPage(domain.GameListOptions{Limit: 50, IncludeDependencies: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(allAdministrative.Items) != 3 || allAdministrative.Total != 3 {
		t.Fatalf("administrative page = %#v, want all catalog roles", allAdministrative)
	}
}

func TestStoreSearchPrefersReadyGameOverNeedsCurationDuplicate(t *testing.T) {
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
	for _, game := range []domain.GameAsset{
		{LibraryID: lib.ID, Title: "sf2", Platform: "snes", Format: "sfc", FilePath: "/library/snes/sf2.zip", RelPath: "snes/sf2.sfc", MTime: time.Unix(1, 0), CatalogRole: "needs-curation"},
		{LibraryID: lib.ID, Title: "sf2", Platform: "cps1", Format: "zip", FilePath: "/library/arcade/sf2.zip", RelPath: "arcade/sf2.zip", MTime: time.Unix(2, 0), CatalogRole: "game"},
	} {
		if _, err := s.UpsertGame(game); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.ListGamesPage(domain.GameListOptions{Query: "sf2", Sort: "title", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].CatalogRole != "game" || page.Items[0].Platform != "cps1" || page.Items[1].CatalogRole != "needs-curation" {
		t.Fatalf("search page = %#v, want audited game before needs-curation duplicate", page)
	}
}

func TestStoreClientCatalogSeparatesDiscoveryFromLaunchReadiness(t *testing.T) {
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
	for _, game := range []domain.GameAsset{
		{LibraryID: lib.ID, Title: "Ready", Platform: "nes", Format: "nes", FilePath: "/library/ready.nes", RelPath: "ready.nes", MTime: time.Unix(1, 0), CatalogRole: "game"},
		{LibraryID: lib.ID, Title: "BIOS", Platform: "neogeo", Format: "zip", FilePath: "/library/neogeo.zip", RelPath: "neogeo.zip", MTime: time.Unix(2, 0), CatalogRole: "dependency"},
		{LibraryID: lib.ID, Title: "Needs Curation", Platform: "arcade", Format: "zip", FilePath: "/library/unknown.zip", RelPath: "unknown.zip", MTime: time.Unix(3, 0), CatalogRole: "needs-curation"},
	} {
		if _, err := s.UpsertGame(game); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.ListGamesPage(domain.GameListOptions{Limit: 20, ClientVisibleOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("client page = %#v, want ready and needs-curation games", page)
	}
	dependency, err := s.ListGamesPage(domain.GameListOptions{Limit: 20, Query: "BIOS"})
	if err != nil {
		t.Fatal(err)
	}
	if dependency.Total != 1 || len(dependency.Items) != 1 || dependency.Items[0].CatalogRole != "dependency" {
		t.Fatalf("dependency search = %#v, want searchable BIOS", dependency)
	}
	clientDependency, err := s.ListGamesPage(domain.GameListOptions{Limit: 20, Query: "BIOS", ClientVisibleOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if clientDependency.Total != 0 {
		t.Fatalf("client dependency search = %#v, want dependency hidden from client catalog", clientDependency)
	}
	uncurated, err := s.ListGamesPage(domain.GameListOptions{Limit: 20, Query: "Needs Curation", ClientVisibleOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if uncurated.Total != 1 || len(uncurated.Items) != 1 || uncurated.Items[0].CatalogRole != "needs-curation" {
		t.Fatalf("uncurated search = %#v, want discoverable needs-curation game", uncurated)
	}
	facets, err := s.ListGameFacets(domain.GameListOptions{ClientVisibleOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if facets.Total != 2 || len(facets.Platforms) != 2 {
		t.Fatalf("client facets = %#v, want NES and arcade discovery counts", facets)
	}
	recent, err := s.ListRecentGames(20)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 2 {
		t.Fatalf("recent games = %#v, want ready and needs-curation games in the admin shelf", recent)
	}
	collections, err := s.ListGamePlatformCollections()
	if err != nil {
		t.Fatal(err)
	}
	if len(collections) != 2 {
		t.Fatalf("platform collections = %#v, want NES and arcade admin collections", collections)
	}
}

func TestStoreListsPlayedGamesByProfileAndPlaytime(t *testing.T) {
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
	first, err := s.UpsertGame(domain.GameAsset{LibraryID: lib.ID, Title: "Alpha", Platform: "snes", Format: "zip", FilePath: "/library/alpha.zip", RelPath: "alpha.zip", MTime: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.UpsertGame(domain.GameAsset{LibraryID: lib.ID, Title: "Beta", Platform: "neogeo", Format: "zip", FilePath: "/library/beta.zip", RelPath: "beta.zip", MTime: time.Unix(2, 0)})
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := s.UpsertGame(domain.GameAsset{LibraryID: lib.ID, Title: "Hidden", Platform: "arcade", Format: "zip", FilePath: "/library/hidden.zip", RelPath: "hidden.zip", MTime: time.Unix(3, 0), CatalogRole: "needs-curation"})
	if err != nil {
		t.Fatal(err)
	}
	guest, err := s.CreateProfile("Guest", "game", "violet")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReportGamePlaySessionForProfile(first.ID, 0, domain.GamePlaySessionReport{SessionID: "alpha-1", ElapsedSeconds: 30}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReportGamePlaySessionForProfile(second.ID, 0, domain.GamePlaySessionReport{SessionID: "beta-1", ElapsedSeconds: 120}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReportGamePlaySessionForProfile(hidden.ID, 0, domain.GamePlaySessionReport{SessionID: "hidden-1", ElapsedSeconds: 600}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReportGamePlaySessionForProfile(first.ID, guest.ID, domain.GamePlaySessionReport{SessionID: "guest-1", ElapsedSeconds: 300}); err != nil {
		t.Fatal(err)
	}

	page, err := s.ListPlayedGamesForProfile(domain.PlayedGameListOptions{Limit: 1, Sort: "playtime", Direction: "desc"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Items) != 1 || page.Items[0].Game.ID != second.ID || page.Items[0].Stats.TotalPlaySeconds != 120 || !page.HasMore {
		t.Fatalf("default played page = %#v", page)
	}
	guestPage, err := s.ListPlayedGamesForProfile(domain.PlayedGameListOptions{Limit: 20}, guest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if guestPage.Total != 1 || len(guestPage.Items) != 1 || guestPage.Items[0].Game.ID != first.ID || guestPage.Items[0].Stats.TotalPlaySeconds != 300 {
		t.Fatalf("guest played page = %#v", guestPage)
	}
}

func TestStoreLoadsGameCurationStatsInBulk(t *testing.T) {
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
	ready, err := s.UpsertGame(domain.GameAsset{LibraryID: lib.ID, Title: "Ready", Platform: "snes", Format: "zip", FilePath: "/library/ready.zip", RelPath: "ready.zip", MTime: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := s.UpsertGame(domain.GameAsset{LibraryID: lib.ID, Title: "Pending", Platform: "arcade", Format: "zip", FilePath: "/library/pending.zip", RelPath: "pending.zip", MTime: time.Unix(2, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertGameMetadata(domain.GameMetadata{GameID: ready.ID, DisplayTitle: "Ready Game"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertGameArtwork(domain.GameArtwork{GameID: ready.ID, Source: "local", Kind: "cover", CachePath: "/covers/ready.jpg", Selected: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceGameFiles(ready.ID, []domain.GameFile{{GameID: ready.ID, Name: "ready.zip", FilePath: "/library/ready.zip", Size: 100, MTime: time.Unix(1, 0), SHA1: "0123456789abcdef0123456789abcdef01234567", Role: "entry"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceGameFiles(pending.ID, []domain.GameFile{{GameID: pending.ID, Name: "pending.zip", FilePath: "/library/pending.zip", Size: 200, MTime: time.Unix(2, 0), Role: "entry"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReplaceGameLaunchProfiles("test", []domain.GameLaunchProfile{{
		GameID: ready.ID, ID: "ready-profile", Revision: 1, Policy: "test", ClientName: "GameEMU", ClientPlatform: "ios", Architecture: "arm64",
		Runtime: domain.GameRuntimeDescriptor{ID: "snes9x"}, EntryFile: "ready.zip", Status: "ready",
	}}, nil); err != nil {
		t.Fatal(err)
	}

	stats, err := s.GameCurationStats([]int64{ready.ID, pending.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got := stats[ready.ID]; got.MetadataStatus != "matched" || got.ArtworkStatus != "ready" || got.ReadyProfiles != 1 || got.FileCount != 1 || got.Checksummed != 1 {
		t.Fatalf("ready stats = %#v", got)
	}
	if got := stats[pending.ID]; got.MetadataStatus != "unmatched" || got.ArtworkStatus != "missing" || got.ReadyProfiles != 0 || got.FileCount != 1 || got.Checksummed != 0 {
		t.Fatalf("pending stats = %#v", got)
	}
}

func TestStoreGameCatalogRoleCountsEmpty(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	total, ready, needsCuration, dependencies, err := New(conn).GameCatalogRoleCounts()
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 || ready != 0 || needsCuration != 0 || dependencies != 0 {
		t.Fatalf("empty role counts = (%d, %d, %d, %d), want all zero", total, ready, needsCuration, dependencies)
	}
}

func TestStoreScopesGamePrivateStateByProfile(t *testing.T) {
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
	guestProfile, err := s.CreateProfile("Guest", "game", "amber")
	if err != nil {
		t.Fatal(err)
	}
	lib, err := s.CreateLibrary("Games", "/library")
	if err != nil {
		t.Fatal(err)
	}
	game, err := s.UpsertGame(domain.GameAsset{
		LibraryID:     lib.ID,
		Title:         "Advance Wars",
		Platform:      "gba",
		ROMSetName:    "GBA",
		Region:        "USA",
		Format:        "gba",
		FilePath:      "/library/gba/advance-wars.gba",
		RelPath:       "gba/advance-wars.gba",
		Size:          1024,
		MTime:         time.Unix(31, 0),
		CRC32:         "11111111",
		SHA1:          "1111111111111111111111111111111111111111",
		EmulatorHint:  "gba",
		Compatibility: "unknown",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateGamePrivateStateForProfile(game.ID, defaultProfile.ID, domain.GamePrivateState{Favorite: true, Liked: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateGamePrivateStateForProfile(game.ID, guestProfile.ID, domain.GamePrivateState{Favorite: false, Liked: true}); err != nil {
		t.Fatal(err)
	}

	defaultGame, err := s.GameByIDForProfile(game.ID, defaultProfile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !defaultGame.Favorite || !defaultGame.Liked {
		t.Fatalf("default game = %#v, want favorite and liked", defaultGame)
	}
	guestPage, err := s.ListGamesPageForProfile(domain.GameListOptions{Limit: 20, Sort: "platform"}, guestProfile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(guestPage.Items) != 1 || guestPage.Items[0].Favorite || !guestPage.Items[0].Liked {
		t.Fatalf("guest page = %#v, want liked only", guestPage)
	}
}

func TestStoreManualCollectionsSpanAssetTypes(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	s := New(conn)
	bookLib, err := s.CreateLibraryWithType("Books", "/books", "book")
	if err != nil {
		t.Fatal(err)
	}
	series, err := s.UpsertSeries(bookLib.ID, "Guides", "Guides")
	if err != nil {
		t.Fatal(err)
	}
	book, err := s.UpsertBook(series.ID, "Arcade Guide", "pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertFile(book.ID, bookLib.ID, "/books/Guides/Arcade Guide.pdf", "Guides/Arcade Guide.pdf", 2048, time.Unix(10, 0), ".pdf"); err != nil {
		t.Fatal(err)
	}
	gameLib, err := s.CreateLibraryWithType("Games", "/games", "game")
	if err != nil {
		t.Fatal(err)
	}
	game, err := s.UpsertGame(domain.GameAsset{LibraryID: gameLib.ID, Title: "Metal Slug", Platform: "arcade", ROMSetName: "MAME", Format: "zip", FilePath: "/games/arcade/mslug.zip", RelPath: "arcade/mslug.zip", Size: 1024, MTime: time.Unix(11, 0), CRC32: "22222222", SHA1: "2222222222222222222222222222222222222222", EmulatorHint: "arcade", Compatibility: "unknown"})
	if err != nil {
		t.Fatal(err)
	}
	videoLib, err := s.CreateLibraryWithType("Videos", "/videos", "video")
	if err != nil {
		t.Fatal(err)
	}
	video, err := s.UpsertVideo(domain.VideoAsset{LibraryID: videoLib.ID, Title: "Cabinet Tour", Format: "mp4", FilePath: "/videos/Cabinet Tour.mp4", RelPath: "Cabinet Tour.mp4", Size: 4096, MTime: time.Unix(12, 0), ThumbnailStatus: "placeholder"})
	if err != nil {
		t.Fatal(err)
	}

	collection, err := s.CreateManualCollection(domain.ManualCollection{Name: "Arcade Night", Description: "Cross-media picks"})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []domain.ManualCollectionItem{
		{AssetType: "book", AssetID: book.ID},
		{AssetType: "game", AssetID: game.ID},
		{AssetType: "video", AssetID: video.ID},
	} {
		if err := s.AddManualCollectionItem(collection.ID, item); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AddManualCollectionItem(collection.ID, domain.ManualCollectionItem{AssetType: "game", AssetID: game.ID}); err != nil {
		t.Fatal(err)
	}

	items, err := s.ListManualCollectionItems(collection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || items[0].AssetType != "book" || items[1].AssetType != "game" || items[2].AssetType != "video" {
		t.Fatalf("manual collection items = %#v, want book/game/video in insertion order without duplicates", items)
	}
	collections, err := s.ListManualCollections()
	if err != nil {
		t.Fatal(err)
	}
	if len(collections) != 1 || collections[0].ItemCount != 3 {
		t.Fatalf("manual collections = %#v, want item count", collections)
	}
	if err := s.RemoveManualCollectionItem(collection.ID, "game", game.ID); err != nil {
		t.Fatal(err)
	}
	items, err = s.ListManualCollectionItems(collection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("manual collection items after remove = %#v, want two", items)
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

	otherSeries, err := s.UpsertSeries(lib.ID, "Series B", "Series B")
	if err != nil {
		t.Fatal(err)
	}
	pdfBook, err := s.UpsertBook(otherSeries.ID, "Zeta Manual", "pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertFile(pdfBook.ID, lib.ID, "/library/Series B/Zeta Manual.pdf", "Series B/Zeta Manual.pdf", 100, time.Now(), ".pdf"); err != nil {
		t.Fatal(err)
	}
	allBooks, err := s.ListBooksPage(domain.BookListOptions{
		Limit:     3,
		Offset:    0,
		Sort:      "title",
		Direction: "desc",
		Format:    "all",
	})
	if err != nil {
		t.Fatal(err)
	}
	if allBooks.Total != 5 || len(allBooks.Items) != 3 || !allBooks.HasMore {
		t.Fatalf("all books page = %#v, want first 3 of 5 books", allBooks)
	}
	if allBooks.Items[0].Title != "Zeta Manual" || allBooks.Items[1].Title != "Gamma" {
		t.Fatalf("all books order = %#v, want title desc across collections", allBooks.Items)
	}
	pdfOnly, err := s.ListBooksPage(domain.BookListOptions{
		Limit:  10,
		Format: "pdf",
	})
	if err != nil {
		t.Fatal(err)
	}
	if pdfOnly.Total != 1 || len(pdfOnly.Items) != 1 || pdfOnly.Items[0].Title != "Zeta Manual" {
		t.Fatalf("pdf page = %#v, want only PDF book", pdfOnly)
	}
}

func TestStoreListsSeriesPageWithPrimaryTypeAndSort(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	s := New(conn)
	comicLib, err := s.CreateLibraryWithType("Comics", "/library", "comic")
	if err != nil {
		t.Fatal(err)
	}
	bookLib, err := s.CreateLibraryWithType("Books", "/books", "book")
	if err != nil {
		t.Fatal(err)
	}
	comicA, err := s.UpsertSeries(comicLib.ID, "Alpha Comic", "Alpha Comic")
	if err != nil {
		t.Fatal(err)
	}
	comicB, err := s.UpsertSeries(comicLib.ID, "Beta Comic", "Beta Comic")
	if err != nil {
		t.Fatal(err)
	}
	bookSeries, err := s.UpsertSeries(bookLib.ID, "Novel Shelf", "Novel Shelf")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		seriesID int64
		libID    int64
		title    string
		format   string
	}{
		{seriesID: comicA.ID, libID: comicLib.ID, title: "Alpha 01", format: "cbz"},
		{seriesID: comicB.ID, libID: comicLib.ID, title: "Beta 01", format: "zip"},
		{seriesID: bookSeries.ID, libID: bookLib.ID, title: "Novel 01", format: "epub"},
	} {
		book, err := s.UpsertBook(item.seriesID, item.title, item.format)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.UpsertFile(book.ID, item.libID, "/tmp/"+item.title+"."+item.format, item.title+"."+item.format, 100, time.Now(), "."+item.format); err != nil {
			t.Fatal(err)
		}
	}
	comics, err := s.ListSeriesPageForProfile(defaultProfileID, domain.CollectionListOptions{
		PrimaryType: "comic",
		Limit:       1,
		Sort:        "title",
		Direction:   "desc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if comics.Total != 2 || len(comics.Items) != 1 || !comics.HasMore || comics.Items[0].Title != "Beta Comic" {
		t.Fatalf("comic page = %#v, want first descending comic collection", comics)
	}
	books, err := s.ListSeriesPageForProfile(defaultProfileID, domain.CollectionListOptions{
		PrimaryType: "book",
		Limit:       60,
	})
	if err != nil {
		t.Fatal(err)
	}
	if books.Total != 1 || len(books.Items) != 1 || books.Items[0].Title != "Novel Shelf" || books.Items[0].PrimaryType != "book" {
		t.Fatalf("book page = %#v, want one book collection", books)
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
