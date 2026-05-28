package scanner

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/store"
)

func TestScanLibraryIndexesValidArchivesAndRecordsEmptyFile(t *testing.T) {
	root := t.TempDir()
	makeZip(t, filepath.Join(root, "Series A", "book1.cbz"), map[string]string{"001.jpg": "image"})
	makeZip(t, filepath.Join(root, "Publisher", "Series A", "book2.cbz"), map[string]string{"001.jpg": "image"})
	makeZip(t, filepath.Join(root, "Books", "novel.epub"), sampleEPUBEntries())
	makeZip(t, filepath.Join(root, "root-book.zip"), map[string]string{"001.png": "image"})
	makeZip(t, filepath.Join(root, "#recycle", "deleted.cbz"), map[string]string{"001.jpg": "image"})
	makeZip(t, filepath.Join(root, "@eaDir", "thumbnail.cbz"), map[string]string{"001.jpg": "image"})
	makeZip(t, filepath.Join(root, ".calnotes", "notes.cbz"), map[string]string{"001.jpg": "image"})
	if err := os.WriteFile(filepath.Join(root, "Series A", "empty.cbz"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	st := store.New(conn)
	lib, err := st.CreateLibrary("Test", root)
	if err != nil {
		t.Fatal(err)
	}

	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "completed" {
		t.Fatalf("job status = %q, want completed", job.Status)
	}
	if job.IndexedFiles != 4 {
		t.Fatalf("indexed files = %d, want 4", job.IndexedFiles)
	}
	if job.ErrorCount != 1 {
		t.Fatalf("error count = %d, want 1", job.ErrorCount)
	}

	series, err := st.ListSeries()
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 4 {
		t.Fatalf("series len = %d, want 4", len(series))
	}
	titles := map[string]bool{}
	for _, item := range series {
		titles[item.Title] = true
	}
	if !titles["Series A"] {
		t.Fatalf("series titles = %#v, want Series A", titles)
	}
	if !titles["Publisher/Series A"] {
		t.Fatalf("series titles = %#v, want Publisher/Series A", titles)
	}
	if !titles["Books"] {
		t.Fatalf("series titles = %#v, want Books", titles)
	}
	rootSeries := filepath.Base(root)
	if !titles[rootSeries] {
		t.Fatalf("series titles = %#v, want root series %q", titles, rootSeries)
	}
	for _, item := range series {
		if item.CollectionType != "directory" {
			t.Fatalf("collection type for %q = %q, want directory", item.Title, item.CollectionType)
		}
		if item.Title == "Series A" && item.DirectoryPath != "Series A" {
			t.Fatalf("directory path for Series A = %q, want Series A", item.DirectoryPath)
		}
		if item.Title == "Publisher/Series A" && item.DirectoryPath != "Publisher/Series A" {
			t.Fatalf("directory path for Publisher/Series A = %q, want Publisher/Series A", item.DirectoryPath)
		}
		if item.Title == rootSeries && item.DirectoryPath != "." {
			t.Fatalf("directory path for root series = %q, want .", item.DirectoryPath)
		}
	}

	errors, err := st.ListFileErrors()
	if err != nil {
		t.Fatal(err)
	}
	if len(errors) != 1 || errors[0].Code != "empty_file" {
		t.Fatalf("errors = %#v, want one empty_file", errors)
	}

	secondJob, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if secondJob.SkippedFiles != 4 {
		t.Fatalf("second scan skipped files = %d, want 4", secondJob.SkippedFiles)
	}
	if secondJob.IndexedFiles != 0 {
		t.Fatalf("second scan indexed files = %d, want 0", secondJob.IndexedFiles)
	}
}

func TestScanLibraryUsesEPUBMetadataForTitleCollectionAndBookDetails(t *testing.T) {
	root := t.TempDir()
	makeZip(t, filepath.Join(root, "Books", "ugly-file-name.epub"), sampleEPUBEntriesWithMetadata("Metadata Book Title", "Metadata Author", "Metadata description."))

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	st := store.New(conn)
	lib, err := st.CreateLibrary("Books", root)
	if err != nil {
		t.Fatal(err)
	}

	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "completed" || job.IndexedFiles != 1 {
		t.Fatalf("job = %#v, want one indexed epub", job)
	}

	series, err := st.ListSeries()
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 || series[0].Title != "Metadata Author" {
		t.Fatalf("series = %#v, want creator collection", series)
	}
	books, err := st.ListBooks(series[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 1 || books[0].Title != "Metadata Book Title" || books[0].Creator != "Metadata Author" || books[0].Description != "Metadata description." {
		t.Fatalf("books = %#v, want EPUB metadata details", books)
	}

	makeZip(t, filepath.Join(root, "Books", "ugly-file-name.epub"), sampleEPUBEntriesWithMetadata("Renamed Book Title", "Second Author", "Updated description."))
	secondJob, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if secondJob.MetadataUpdatedFiles != 1 || secondJob.ReclassifiedFiles != 1 {
		t.Fatalf("second job = %#v, want metadata and collection change counts", secondJob)
	}
}

func TestScanLibraryUsesConfiguredWorkerPool(t *testing.T) {
	t.Setenv("FOLIOSPACE_SCAN_WORKERS", "2")
	root := t.TempDir()
	for i := 0; i < 6; i++ {
		makeZip(t, filepath.Join(root, "Series A", "book"+string(rune('A'+i))+".cbz"), map[string]string{"001.jpg": "image"})
	}

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	st := store.New(conn)
	lib, err := st.CreateLibrary("Test", root)
	if err != nil {
		t.Fatal(err)
	}

	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "completed" || job.IndexedFiles != 6 || job.ErrorCount != 0 {
		t.Fatalf("job = %#v, want six indexed files with no errors", job)
	}
	events, err := st.ListJobEvents(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Message == "scan workers: 2" {
			return
		}
	}
	t.Fatalf("events = %#v, want scan workers event", events)
}

func TestRunScanJobHonorsPauseRequest(t *testing.T) {
	root := t.TempDir()
	makeZip(t, filepath.Join(root, "Series A", "book1.cbz"), map[string]string{"001.jpg": "image"})

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	st := store.New(conn)
	lib, err := st.CreateLibrary("Test", root)
	if err != nil {
		t.Fatal(err)
	}
	job, err := st.StartScanJob(lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.RequestScanJobPause(job.ID); err != nil {
		t.Fatal(err)
	}

	paused, err := New(st).RunScanJob(lib, job)
	if err != nil {
		t.Fatal(err)
	}
	if paused.Status != "paused" || paused.IndexedFiles != 0 || paused.CurrentPath != "" {
		t.Fatalf("job = %#v, want paused before indexing", paused)
	}
}

func TestRunScanJobHonorsCancelRequest(t *testing.T) {
	root := t.TempDir()
	makeZip(t, filepath.Join(root, "Series A", "book1.cbz"), map[string]string{"001.jpg": "image"})

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	st := store.New(conn)
	lib, err := st.CreateLibrary("Test", root)
	if err != nil {
		t.Fatal(err)
	}
	job, err := st.StartScanJob(lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.RequestScanJobCancel(job.ID); err != nil {
		t.Fatal(err)
	}

	cancelled, err := New(st).RunScanJob(lib, job)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != "cancelled" || cancelled.IndexedFiles != 0 || cancelled.CurrentPath != "" {
		t.Fatalf("job = %#v, want cancelled before indexing", cancelled)
	}
}

func TestScanLibraryIndexesGameROMMetadata(t *testing.T) {
	root := t.TempDir()
	romPath := filepath.Join(root, "SNES", "Super Mario World (USA).sfc")
	if err := os.MkdirAll(filepath.Dir(romPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(romPath, []byte("rom-body"), 0o644); err != nil {
		t.Fatal(err)
	}

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	st := store.New(conn)
	lib, err := st.CreateLibrary("Games", root)
	if err != nil {
		t.Fatal(err)
	}

	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "completed" || job.IndexedFiles != 1 || job.ErrorCount != 0 {
		t.Fatalf("job = %#v, want one indexed ROM and no errors", job)
	}

	games, err := st.ListRecentGames(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 {
		t.Fatalf("games len = %d, want 1", len(games))
	}
	game := games[0]
	if game.Title != "Super Mario World" || game.Platform != "snes" || game.Format != "sfc" || game.Size != int64(len("rom-body")) {
		t.Fatalf("game = %#v, want inferred SNES ROM metadata", game)
	}
	if game.CRC32 == "" || game.SHA1 == "" {
		t.Fatalf("game checksums crc32=%q sha1=%q, want populated checksums", game.CRC32, game.SHA1)
	}
	if game.FilePath == "" {
		t.Fatalf("game file path is empty, scanner should keep internal path")
	}

	secondJob, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if secondJob.SkippedFiles != 1 || secondJob.IndexedFiles != 0 {
		t.Fatalf("second job = %#v, want unchanged ROM skipped", secondJob)
	}
}

func TestScanLibraryTreatsZipAsGameWhenLibraryIsGameTyped(t *testing.T) {
	root := t.TempDir()
	makeZip(t, filepath.Join(root, "Arcade", "mslug.zip"), map[string]string{"mslug.rom": "rom"})

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("Arcade", root, "game")
	if err != nil {
		t.Fatal(err)
	}

	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.IndexedFiles != 1 || job.ErrorCount != 0 {
		t.Fatalf("job = %#v, want zip indexed as game ROM set", job)
	}

	games, err := st.ListRecentGames(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 || games[0].Title != "mslug" || games[0].Format != "zip" || games[0].Platform != "arcade" {
		t.Fatalf("games = %#v, want arcade zip ROM set", games)
	}

	series, err := st.ListSeries()
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 0 {
		t.Fatalf("series = %#v, want no comic series for game zip", series)
	}
}

func TestScanLibraryTreats7zAsBookUnlessLibraryIsGameTyped(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Comics", "archive.7z")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("7z-body"), 0o644); err != nil {
		t.Fatal(err)
	}

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	st := store.New(conn)
	lib, err := st.CreateLibrary("Comics", root)
	if err != nil {
		t.Fatal(err)
	}

	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.DiscoveredFiles != 1 || job.IndexedFiles != 0 || job.ErrorCount != 1 {
		t.Fatalf("job = %#v, want 7z discovered as book with archive error", job)
	}

	games, err := st.ListRecentGames(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 0 {
		t.Fatalf("games = %#v, want default 7z excluded from games", games)
	}

	series, err := st.ListSeries()
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 || series[0].Title != "Comics" || series[0].BookCount != 1 {
		t.Fatalf("series = %#v, want 7z retained under comic collection", series)
	}
	books, err := st.ListBooks(series[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 1 || books[0].Title != "archive" || books[0].Format != "7z" {
		t.Fatalf("books = %#v, want 7z comic book metadata", books)
	}

	if _, err := st.UpsertGame(domain.GameAsset{
		LibraryID:     lib.ID,
		Title:         "archive",
		Platform:      "arcade",
		ROMSetName:    "Comics",
		Format:        "7z",
		FilePath:      path,
		RelPath:       "Comics/archive.7z",
		Size:          7,
		MTime:         time.Now(),
		CRC32:         "00000000",
		SHA1:          "0000000000000000000000000000000000000000",
		EmulatorHint:  "arcade",
		Compatibility: "unknown",
	}); err != nil {
		t.Fatal(err)
	}
	cleanupJob, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if cleanupJob.DiscoveredFiles != 1 || cleanupJob.ErrorCount != 1 {
		t.Fatalf("cleanup job = %#v, want 7z rescanned as book", cleanupJob)
	}
	games, err = st.ListRecentGames(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 0 {
		t.Fatalf("games after cleanup = %#v, want stale 7z game removed", games)
	}
}

func TestScanLibraryTreats7zAsGameWhenLibraryIsGameTyped(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Arcade", "romset.7z")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("rom-body"), 0o644); err != nil {
		t.Fatal(err)
	}

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("Arcade", root, "game")
	if err != nil {
		t.Fatal(err)
	}

	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.IndexedFiles != 1 || job.ErrorCount != 0 {
		t.Fatalf("job = %#v, want game 7z ROM set indexed", job)
	}

	games, err := st.ListRecentGames(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 || games[0].Title != "romset" || games[0].Format != "7z" || games[0].Platform != "arcade" {
		t.Fatalf("games = %#v, want arcade 7z ROM set", games)
	}
}

func TestInferGamePlatformUsesFBNeoSystemDirectories(t *testing.T) {
	tests := []struct {
		relPath string
		want    string
	}{
		{relPath: "FBNeo/megadrive/shinobi3.zip", want: "md"},
		{relPath: "FBNeo/snes/contra3.zip", want: "snes"},
		{relPath: "FBNeo/nes/battlecity.zip", want: "nes"},
		{relPath: "FBNeo/arcade/mslug.zip", want: "neogeo"},
		{relPath: "FBNeo/arcade/shinobi3.zip", want: "md"},
		{relPath: "FBNeo/arcade/wof.zip", want: "arcade"},
		{relPath: "Model3ROMs/spikeout.zip", want: "model3"},
		{relPath: "SEGA 32X/doom32x.zip", want: "32x"},
	}
	for _, test := range tests {
		if got := inferGamePlatform(filepath.Ext(test.relPath), test.relPath); got != test.want {
			t.Fatalf("inferGamePlatform(%q) = %q, want %q", test.relPath, got, test.want)
		}
	}
}

func TestScanLibraryMovesLegacyRootFileToLibrarySeries(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "root-book.cbz")
	makeZip(t, path, map[string]string{"001.jpg": "image"})

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	st := store.New(conn)
	lib, err := st.CreateLibrary("Test", root)
	if err != nil {
		t.Fatal(err)
	}
	legacySeries, err := st.UpsertSeries(lib.ID, "Unsorted", ".")
	if err != nil {
		t.Fatal(err)
	}
	legacyBook, err := st.UpsertBook(legacySeries.ID, "root-book", "cbz")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertFile(legacyBook.ID, lib.ID, path, "root-book.cbz", info.Size(), info.ModTime(), ".cbz"); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplacePages(legacyBook.ID, []domain.Page{{Index: 0, Name: "001.jpg"}}); err != nil {
		t.Fatal(err)
	}

	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.SkippedFiles != 1 {
		t.Fatalf("skipped files = %d, want 1", job.SkippedFiles)
	}

	series, err := st.ListSeries()
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 {
		t.Fatalf("series = %#v, want only library root series", series)
	}
	if series[0].Title != filepath.Base(root) {
		t.Fatalf("series title = %q, want %q", series[0].Title, filepath.Base(root))
	}
	books, err := st.ListBooks(series[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 1 || books[0].ID != legacyBook.ID {
		t.Fatalf("books = %#v, want migrated legacy book id %d", books, legacyBook.ID)
	}
}

func makeZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, body := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func sampleEPUBEntries() map[string]string {
	return sampleEPUBEntriesWithMetadata("Sample EPUB", "", "")
}

func sampleEPUBEntriesWithTitle(title string) map[string]string {
	return sampleEPUBEntriesWithMetadata(title, "", "")
}

func sampleEPUBEntriesWithMetadata(title string, creator string, description string) map[string]string {
	creatorXML := ""
	if creator != "" {
		creatorXML = "\n    <dc:creator>" + creator + "</dc:creator>"
	}
	descriptionXML := ""
	if description != "" {
		descriptionXML = "\n    <dc:description>" + description + "</dc:description>"
	}
	return map[string]string{
		"META-INF/container.xml": `<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OPS/package.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`,
		"OPS/package.opf": `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>` + title + `</dc:title>` + creatorXML + descriptionXML + `
  </metadata>
  <manifest>
    <item id="chapter1" href="text/chapter1.xhtml" media-type="application/xhtml+xml"/>
    <item id="cover" href="images/cover.jpg" media-type="image/jpeg" properties="cover-image"/>
  </manifest>
  <spine>
    <itemref idref="chapter1"/>
  </spine>
</package>`,
		"OPS/text/chapter1.xhtml": `<html xmlns="http://www.w3.org/1999/xhtml"><body><h1>Chapter</h1></body></html>`,
		"OPS/images/cover.jpg":    "cover",
	}
}
