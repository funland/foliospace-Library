package scanner

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/naomi2catalog"
	"foliospace-reader/internal/store"
	"golang.org/x/text/encoding/japanese"
)

func TestIdentifyProjectJusticeNaomiROMSet(t *testing.T) {
	revA := projectJusticeNaomiROMSetCatalog[0]
	members := cloneNaomiArchiveMembers(revA.members)

	got, detected, err := identifyProjectJusticeNaomiROMSet(members)
	if err != nil {
		t.Fatal(err)
	}
	if !detected || got.romSetName != "pjustica" || got.parent != "pjustic" {
		t.Fatalf("identity = %#v, detected = %v", got, detected)
	}

	delete(members, "mpr-23547.ic11")
	if _, detected, err := identifyProjectJusticeNaomiROMSet(members); !detected || err == nil {
		t.Fatalf("missing MPR detected = %v, err = %v", detected, err)
	}
}

func TestIdentifyProjectJusticeNaomiROMSetRejectsMixedOrDamagedRevision(t *testing.T) {
	revA := projectJusticeNaomiROMSetCatalog[0]
	members := cloneNaomiArchiveMembers(revA.members)
	members["epr-23548b.ic22"] = projectJusticeNaomiROMSetCatalog[1].members["epr-23548b.ic22"]
	if _, detected, err := identifyProjectJusticeNaomiROMSet(members); !detected || err == nil {
		t.Fatalf("mixed revisions detected = %v, err = %v", detected, err)
	}

	members = cloneNaomiArchiveMembers(revA.members)
	members[revA.bootMember] = naomiArchiveMember{size: 0x0400000, crc32: 0xd0dbdf40}
	if _, detected, err := identifyProjectJusticeNaomiROMSet(members); !detected || err == nil {
		t.Fatalf("damaged boot ROM detected = %v, err = %v", detected, err)
	}

	if _, detected, err := identifyProjectJusticeNaomiROMSet(map[string]naomiArchiveMember{"unrelated.bin": {size: 1, crc32: 1}}); detected || err != nil {
		t.Fatalf("unrelated archive detected = %v, err = %v", detected, err)
	}
}

func TestProjectJusticeArchiveNameTriggersAuditOutsideNaomiDirectory(t *testing.T) {
	for _, path := range []string{"/games/NAOMI/pjustic.zip", "/games/Arcade/PJUSTICA.ZIP"} {
		if !isProjectJusticeArchiveName(path) {
			t.Fatalf("isProjectJusticeArchiveName(%q) = false", path)
		}
	}
	if isProjectJusticeArchiveName("/games/Arcade/unrelated.zip") {
		t.Fatal("unrelated archive unexpectedly triggers Project Justice audit")
	}
}

func cloneNaomiArchiveMembers(source map[string]naomiArchiveMember) map[string]naomiArchiveMember {
	result := make(map[string]naomiArchiveMember, len(source))
	for name, member := range source {
		result[name] = member
	}
	return result
}

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

func TestScanLibraryUsesPDFMetadataForTitleCollectionAndBookDetails(t *testing.T) {
	root := t.TempDir()
	pdfPath := filepath.Join(root, "Manuals", "ugly-file-name.pdf")
	if err := os.MkdirAll(filepath.Dir(pdfPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pdfPath, samplePDFWithInfo("PDF Metadata Title", "PDF Author", "PDF description."), 0o644); err != nil {
		t.Fatal(err)
	}

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
		t.Fatalf("job = %#v, want one indexed pdf", job)
	}

	series, err := st.ListSeries()
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 || series[0].Title != "PDF Author" {
		t.Fatalf("series = %#v, want PDF author collection", series)
	}
	books, err := st.ListBooks(series[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 1 || books[0].Title != "PDF Metadata Title" || books[0].Creator != "PDF Author" || books[0].Description != "PDF description." {
		t.Fatalf("books = %#v, want PDF metadata details", books)
	}
}

func TestPDFMetadataSkipsWindowFallbackWhenInfoObjectIsOutsideMetadataWindow(t *testing.T) {
	root := t.TempDir()
	pdfPath := filepath.Join(root, "fallback.pdf")
	head := []byte("%PDF-1.4\n1 0 obj << /Type /Outlines /Title (Bookmark Title) >> endobj\n")
	middlePad := bytes.Repeat([]byte("x"), 3<<20)
	infoObject := []byte("5 0 obj << /Title (Real Title) /Author (Real Author) >> endobj\n")
	tailPad := bytes.Repeat([]byte("y"), 3<<20)
	tail := []byte("trailer << /Root 1 0 R /Info 5 0 R >>\n%%EOF\n")
	data := append([]byte{}, head...)
	data = append(data, middlePad...)
	data = append(data, infoObject...)
	data = append(data, tailPad...)
	data = append(data, tail...)
	if err := os.WriteFile(pdfPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	metadata, err := readPDFMetadata(pdfPath, bookMetadata{Title: "fallback"})
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Title != "fallback" || metadata.Creator != "" {
		t.Fatalf("metadata = %#v, want fallback metadata when Info object is outside read window", metadata)
	}
}

func TestScanLibrarySkipsThumbnailAndMediaDirectories(t *testing.T) {
	root := t.TempDir()
	makeZip(t, filepath.Join(root, "Series", "book.cbz"), map[string]string{"001.jpg": "image"})
	makeZip(t, filepath.Join(root, "Series", "thumbnails", "thumb.cbz"), map[string]string{"001.jpg": "image"})
	makeZip(t, filepath.Join(root, "Series", "media", "cover.cbz"), map[string]string{"001.jpg": "image"})
	makeZip(t, filepath.Join(root, "_maintenance", "backup.cbz"), map[string]string{"001.jpg": "image"})

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
	if job.Status != "completed" || job.IndexedFiles != 1 {
		t.Fatalf("job = %#v, want only real book indexed", job)
	}
	series, err := st.ListSeries()
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 || series[0].Title != "Series" {
		t.Fatalf("series = %#v, want only Series collection", series)
	}
	books, err := st.ListBooks(series[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 1 || books[0].Title != "book" {
		t.Fatalf("books = %#v, want only main archive", books)
	}
}

func TestScanLibraryUsesEmbeddedComicJSONMetadata(t *testing.T) {
	root := t.TempDir()
	makeZip(t, filepath.Join(root, "Comics", "ugly-file-name.cbz"), map[string]string{
		"metadata.json": `{
			"name": "Archive Metadata Title",
			"description": "Archive description.",
			"author": ["mignon"],
			"tags": ["C106", "中文", "巨乳"]
		}`,
		"chapter/001.jpg": "image",
	})

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
	if job.Status != "completed" || job.IndexedFiles != 1 {
		t.Fatalf("job = %#v, want one indexed comic", job)
	}

	series, err := st.ListSeries()
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 || series[0].Title != "mignon" {
		t.Fatalf("series = %#v, want creator collection", series)
	}
	books, err := st.ListBooks(series[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 1 || books[0].Title != "Archive Metadata Title" || books[0].Creator != "mignon" || books[0].Description != "Archive description." {
		t.Fatalf("books = %#v, want embedded JSON metadata details", books)
	}
	if strings.Join(books[0].Tags, ",") != "C106,中文,巨乳" {
		t.Fatalf("book tags = %#v, want embedded JSON tags", books[0].Tags)
	}

	results, err := st.SearchBooks("巨乳", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ID != books[0].ID {
		t.Fatalf("tag search = %#v, want embedded JSON tagged book", results)
	}
}

func TestScanLibraryDoesNotReopenUnchangedEPUBWhenMetadataExists(t *testing.T) {
	root := t.TempDir()
	epubPath := filepath.Join(root, "Books", "cached.epub")
	makeZip(t, epubPath, sampleEPUBEntriesWithMetadata("Cached Title", "Cached Author", "Cached description."))

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

	firstJob, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if firstJob.Status != "completed" || firstJob.ErrorCount != 0 || firstJob.IndexedFiles != 1 {
		t.Fatalf("first job = %#v, want one clean indexed epub", firstJob)
	}

	info, err := os.Stat(epubPath)
	if err != nil {
		t.Fatal(err)
	}
	broken := make([]byte, info.Size())
	copy(broken, []byte("not an epub"))
	if err := os.WriteFile(epubPath, broken, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(epubPath, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}

	secondJob, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if secondJob.Status != "completed" || secondJob.ErrorCount != 0 || secondJob.SkippedFiles != 1 {
		t.Fatalf("second job = %#v, want unchanged EPUB skipped without reopening metadata", secondJob)
	}
}

func TestScanLibrarySkipsUnchangedComicWithoutReclassification(t *testing.T) {
	root := t.TempDir()
	comicPath := filepath.Join(root, "Publisher", "Series A", "book1.cbz")
	makeZip(t, comicPath, map[string]string{"001.jpg": "image"})

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

	firstJob, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if firstJob.Status != "completed" || firstJob.IndexedFiles != 1 {
		t.Fatalf("first job = %#v, want one indexed comic", firstJob)
	}

	legacySeries, err := st.UpsertSeries(lib.ID, "Legacy Series", "Legacy Series")
	if err != nil {
		t.Fatal(err)
	}
	series, err := st.ListSeries()
	if err != nil {
		t.Fatal(err)
	}
	var originalSeriesID int64
	for _, item := range series {
		if item.Title == "Publisher/Series A" {
			originalSeriesID = item.ID
			break
		}
	}
	if originalSeriesID == 0 {
		t.Fatalf("series = %#v, want Publisher/Series A", series)
	}
	books, err := st.ListBooks(originalSeriesID)
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 1 {
		t.Fatalf("books = %#v, want one book before legacy move", books)
	}
	if _, err := st.UpdateBookIdentity(books[0].ID, legacySeries.ID, books[0].Title, books[0].Format); err != nil {
		t.Fatal(err)
	}

	secondJob, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if secondJob.Status != "completed" || secondJob.SkippedFiles != 1 || secondJob.ReclassifiedFiles != 0 {
		t.Fatalf("second job = %#v, want unchanged comic fast skipped without reclassification", secondJob)
	}
}

func TestScanLibrarySkipsUnchangedComicWithoutPageIndex(t *testing.T) {
	root := t.TempDir()
	comicPath := filepath.Join(root, "Series A", "book1.cbz")
	makeZip(t, comicPath, map[string]string{"001.jpg": "image"})

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
	series, err := st.UpsertSeries(lib.ID, "Series A", "Series A")
	if err != nil {
		t.Fatal(err)
	}
	book, err := st.UpsertBook(series.ID, "book1", "cbz")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(comicPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertFile(book.ID, lib.ID, comicPath, "Series A/book1.cbz", info.Size(), info.ModTime(), ".cbz"); err != nil {
		t.Fatal(err)
	}

	broken := make([]byte, info.Size())
	copy(broken, []byte("not a zip"))
	if err := os.WriteFile(comicPath, broken, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(comicPath, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}

	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "completed" || job.ErrorCount != 0 || job.SkippedFiles != 1 || job.IndexedFiles != 0 {
		t.Fatalf("job = %#v, want unchanged comic skipped without opening archive", job)
	}
}

func TestScanLibraryDisambiguatesDuplicateEPUBMetadataTitles(t *testing.T) {
	root := t.TempDir()
	makeZip(t, filepath.Join(root, "Author", "Duplicate Book (160)", "first.epub"), sampleEPUBEntriesWithMetadata("Duplicate Book", "Author", "First copy."))
	makeZip(t, filepath.Join(root, "Author", "Duplicate Book (161)", "second.epub"), sampleEPUBEntriesWithMetadata("Duplicate Book", "Author", "Second copy."))

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
	if job.Status != "completed" || job.ErrorCount != 0 || job.IndexedFiles != 2 {
		t.Fatalf("job = %#v, want two duplicate-title EPUBs indexed without errors", job)
	}

	series, err := st.ListSeries()
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 || series[0].Title != "Author" {
		t.Fatalf("series = %#v, want one author collection", series)
	}
	books, err := st.ListBooks(series[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 2 {
		t.Fatalf("books = %#v, want both duplicate-title books retained", books)
	}
	titles := map[string]bool{}
	for _, book := range books {
		titles[book.Title] = true
	}
	if !titles["Duplicate Book (160)"] || !titles["Duplicate Book (161)"] {
		t.Fatalf("titles = %#v, want Calibre ids appended for duplicate metadata titles", titles)
	}
}

func TestScanLibraryUsesConfiguredWorkerPool(t *testing.T) {
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

	job, err := NewWithWorkerCount(st, func() int { return 2 }).ScanLibrary(lib)
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

func TestScanLibraryConcurrentWorkerPoolHandlesLargeDirectories(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 150; i++ {
		makeZip(t, filepath.Join(root, "Bulk", "book-"+strconv.Itoa(i)+".cbz"), map[string]string{"001.jpg": "image"})
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

	job, err := NewWithWorkerCount(st, func() int { return 4 }).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "completed" || job.DiscoveredFiles != 150 || job.IndexedFiles != 150 || job.ErrorCount != 0 {
		t.Fatalf("job = %#v, want all large-directory files discovered and indexed", job)
	}

	secondJob, err := NewWithWorkerCount(st, func() int { return 4 }).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if secondJob.Status != "completed" || secondJob.DiscoveredFiles != 150 || secondJob.SkippedFiles != 150 || secondJob.IndexedFiles != 0 {
		t.Fatalf("second job = %#v, want unchanged large-directory files skipped", secondJob)
	}
}

func TestScanLibraryPathIndexesSingleFile(t *testing.T) {
	root := t.TempDir()
	targetPath := filepath.Join(root, "Series A", "target.cbz")
	makeZip(t, targetPath, map[string]string{"001.jpg": "image"})
	makeZip(t, filepath.Join(root, "Series B", "other.cbz"), map[string]string{"001.jpg": "image"})

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

	job, err := NewWithWorkerCount(st, func() int { return 4 }).ScanLibraryPath(lib, targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "completed" || job.DiscoveredFiles != 1 || job.IndexedFiles != 1 {
		t.Fatalf("job = %#v, want one targeted file indexed", job)
	}

	series, err := st.ListSeries()
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 || series[0].Title != "Series A" {
		t.Fatalf("series = %#v, want only targeted file series", series)
	}
	books, err := st.ListBooks(series[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 1 || books[0].PageCount != 0 || books[0].Analyzed {
		t.Fatalf("books = %#v, want targeted scan to defer page indexing", books)
	}
}

func TestScanLibraryRecentIndexesNewestChangedFiles(t *testing.T) {
	root := t.TempDir()
	oldPath := filepath.Join(root, "Series", "old.cbz")
	midPath := filepath.Join(root, "Series", "mid.cbz")
	newPath := filepath.Join(root, "Series", "new.cbz")
	makeZip(t, oldPath, map[string]string{"001.jpg": "image"})
	makeZip(t, midPath, map[string]string{"001.jpg": "image"})
	makeZip(t, newPath, map[string]string{"001.jpg": "image"})
	base := time.Now().Add(-1 * time.Hour)
	for i, path := range []string{oldPath, midPath, newPath} {
		when := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
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

	job, err := New(st).RunRecentScanJobPath(lib, mustStartScanJob(t, st, lib.ID), root, 2)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "completed" || job.DiscoveredFiles != 2 || job.IndexedFiles != 2 {
		t.Fatalf("job = %#v, want two newest files indexed", job)
	}

	if _, err := st.FileIndexByPath(oldPath); err == nil {
		t.Fatalf("old file was indexed, want recent limit to skip it")
	}
	if _, err := st.FileIndexByPath(midPath); err != nil {
		t.Fatalf("mid file not indexed: %v", err)
	}
	if _, err := st.FileIndexByPath(newPath); err != nil {
		t.Fatalf("new file not indexed: %v", err)
	}
}

func TestScanLibraryRecentReportsDiscoveryProgress(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 12; i++ {
		makeZip(t, filepath.Join(root, "Series", "book-"+strconv.Itoa(i)+".cbz"), map[string]string{"001.jpg": "image"})
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

	job, err := New(st).RunRecentScanJobPath(lib, mustStartScanJob(t, st, lib.ID), root, 3)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "completed" || job.DiscoveredFiles != 3 || job.IndexedFiles != 3 {
		t.Fatalf("job = %#v, want three recent files indexed", job)
	}
	events, err := st.ListJobEvents(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if strings.HasPrefix(event.Message, "recent scan progress: ") {
			return
		}
	}
	t.Fatalf("events = %#v, want recent scan progress event", events)
}

func TestScanLibraryRecentPrunesUnchangedCachedDirectories(t *testing.T) {
	root := t.TempDir()
	quietPath := filepath.Join(root, "Quiet", "old.cbz")
	changedPath := filepath.Join(root, "Changed", "new.cbz")
	makeZip(t, quietPath, map[string]string{"001.jpg": "image"})
	makeZip(t, changedPath, map[string]string{"001.jpg": "image"})

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

	firstJob, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if firstJob.Status != "completed" || firstJob.IndexedFiles != 2 {
		t.Fatalf("first job = %#v, want both files indexed", firstJob)
	}

	quietInfo, err := os.Stat(filepath.Join(root, "Quiet"))
	if err != nil {
		t.Fatal(err)
	}
	makeZip(t, filepath.Join(root, "Changed", "newer.cbz"), map[string]string{"001.jpg": "image"})

	secondJob, err := New(st).RunRecentScanJobPath(lib, mustStartScanJob(t, st, lib.ID), root, 20)
	if err != nil {
		t.Fatal(err)
	}
	if secondJob.Status != "completed" || secondJob.IndexedFiles != 1 {
		t.Fatalf("second job = %#v, want only changed directory new file indexed", secondJob)
	}
	if secondJob.CurrentPath != "" {
		t.Fatalf("current path = %q, want cleared after completion", secondJob.CurrentPath)
	}
	events, err := st.ListJobEvents(secondJob.ID)
	if err != nil {
		t.Fatal(err)
	}
	var pruned bool
	for _, event := range events {
		if strings.Contains(event.Message, "pruned unchanged directories: ") {
			pruned = true
			break
		}
	}
	if !pruned {
		t.Fatalf("events = %#v, want unchanged directory prune event", events)
	}
	afterQuietInfo, err := os.Stat(filepath.Join(root, "Quiet"))
	if err != nil {
		t.Fatal(err)
	}
	if !afterQuietInfo.ModTime().Equal(quietInfo.ModTime()) {
		t.Fatalf("quiet dir mtime changed during test setup")
	}
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

func TestScanLibraryIndexesPSPGameCubeAndPS2DiscImages(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"PSP/Crisis Core.cso":            "psp-image",
		"NGC/Metroid Prime.rvz":          "ngc-image",
		"PS2/Shadow of the Colossus.iso": "ps2-image",
		"PS2/track01.bin":                "not-a-standalone-ps2-game",
	}
	for relativePath, body := range files {
		path := filepath.Join(root, filepath.FromSlash(relativePath))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("Games", root, "game")
	if err != nil {
		t.Fatal(err)
	}

	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "completed" || job.IndexedFiles != 3 || job.ErrorCount != 0 {
		t.Fatalf("job = %#v, want three launchable disc images", job)
	}

	games, err := st.ListRecentGames(10)
	if err != nil {
		t.Fatal(err)
	}
	byPlatform := make(map[string]domain.GameAsset, len(games))
	for _, game := range games {
		byPlatform[game.Platform] = game
	}
	for platform, want := range map[string]struct {
		format string
		set    string
		hint   string
	}{
		"psp": {format: "cso", set: "PSP", hint: "ppsspp"},
		"ngc": {format: "rvz", set: "NGC", hint: "dolphin"},
		"ps2": {format: "iso", set: "PS2", hint: "pcsx2"},
	} {
		game, ok := byPlatform[platform]
		if !ok || game.Format != want.format || game.ROMSetName != want.set || game.EmulatorHint != want.hint {
			t.Fatalf("game for %s = %#v, want format=%s set=%s hint=%s", platform, game, want.format, want.set, want.hint)
		}
		gameFiles, err := st.GameFiles(game.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(gameFiles) != 1 || gameFiles[0].Role != "entry" {
			t.Fatalf("files for %s = %#v, want one entry image", platform, gameFiles)
		}
	}

	secondJob, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if secondJob.IndexedFiles != 0 || secondJob.SkippedFiles != 3 {
		t.Fatalf("second job = %#v, want three unchanged images skipped", secondJob)
	}
}

func TestScanLibraryIndexesN64RawAndZIPROMs(t *testing.T) {
	tests := []struct {
		name       string
		fileName   string
		entries    map[string]string
		body       string
		wantFormat string
		wantTitle  string
		wantError  bool
	}{
		{
			name:       "raw z64",
			fileName:   "Super Mario 64.z64",
			body:       string([]byte{0x80, 0x37, 0x12, 0x40}) + "raw-z64",
			wantFormat: "z64",
			wantTitle:  "Super Mario 64",
		},
		{
			name:       "extension header mismatch uses detected format",
			fileName:   "Mismatch.v64",
			body:       string([]byte{0x80, 0x37, 0x12, 0x40}) + "valid-z64",
			wantFormat: "z64",
			wantTitle:  "Mismatch",
		},
		{
			name:      "invalid header",
			fileName:  "Invalid.n64",
			body:      "not-an-n64-rom",
			wantError: true,
		},
		{
			name:     "zip with one raw entry",
			fileName: "Mario Kart 64.zip",
			entries: map[string]string{
				"README.txt":                "notes",
				"__MACOSX/._Mario Kart.v64": "ignored",
				"ROM/Mario Kart 64.v64":     string([]byte{0x37, 0x80, 0x40, 0x12}) + "raw-v64",
			},
			wantFormat: "v64",
			wantTitle:  "Mario Kart 64",
		},
		{
			name:     "zip with multiple raw entries",
			fileName: "Multiple.zip",
			entries: map[string]string{
				"One.z64": string([]byte{0x80, 0x37, 0x12, 0x40}) + "one",
				"Two.n64": string([]byte{0x40, 0x12, 0x37, 0x80}) + "two",
			},
			wantError: true,
		},
		{
			name:     "zip path traversal",
			fileName: "Traversal.zip",
			entries: map[string]string{
				"../Escape.z64": string([]byte{0x80, 0x37, 0x12, 0x40}) + "escape",
			},
			wantError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "N64", test.fileName)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if test.entries != nil {
				makeZip(t, path, test.entries)
			} else if err := os.WriteFile(path, []byte(test.body), 0o644); err != nil {
				t.Fatal(err)
			}

			conn, err := db.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			st := store.New(conn)
			lib, err := st.CreateLibraryWithType("Games", root, "game")
			if err != nil {
				t.Fatal(err)
			}
			job, err := New(st).ScanLibrary(lib)
			if err != nil {
				t.Fatal(err)
			}
			games, err := st.ListRecentGames(10)
			if err != nil {
				t.Fatal(err)
			}
			if test.wantError {
				if job.ErrorCount != 1 || len(games) != 0 {
					t.Fatalf("job = %#v, games = %#v, want one scan error and no game", job, games)
				}
				return
			}
			if job.ErrorCount != 0 || job.IndexedFiles != 1 || len(games) != 1 {
				t.Fatalf("job = %#v, games = %#v, want one indexed N64 game", job, games)
			}
			game := games[0]
			if game.Title != test.wantTitle || game.Platform != "n64" || game.ROMSetName != "Nintendo 64" || game.EmulatorHint != "mupen64plus" || game.Format != test.wantFormat || game.Compatibility != "untested" {
				t.Fatalf("game = %#v, want canonical N64 metadata", game)
			}
			if game.CRC32 == "" || game.SHA1 == "" {
				t.Fatalf("game checksums = %q/%q, want raw ROM checksums", game.CRC32, game.SHA1)
			}
			files, err := st.GameFiles(game.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != 1 || files[0].Role != "entry" || files[0].Name != test.wantTitle+"."+test.wantFormat || files[0].Size != game.Size {
				t.Fatalf("game files = %#v, want one raw ROM entry", files)
			}
		})
	}
}

func TestScanLibraryIndexesValidatedNintendo3DSImages(t *testing.T) {
	root := t.TempDir()
	threeDSDir := filepath.Join(root, "3DS")
	if err := os.MkdirAll(threeDSDir, 0o755); err != nil {
		t.Fatal(err)
	}
	directPath := filepath.Join(threeDSDir, "Direct Game.3ds")
	if err := os.WriteFile(directPath, makeNintendo3DSImage("NCSD"), 0o644); err != nil {
		t.Fatal(err)
	}
	cxiPath := filepath.Join(threeDSDir, "Executable.cxi")
	if err := os.WriteFile(cxiPath, makeNintendo3DSImage("NCCH"), 0o644); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(threeDSDir, "Archive Game.zip")
	makeZip(t, zipPath, map[string]string{
		"README.txt":      "notes",
		"ROM/Archive.3ds": string(makeNintendo3DSImage("NCSD")),
	})
	ciaPath := filepath.Join(threeDSDir, "Install Package.cia")
	if err := os.WriteFile(ciaPath, syntheticCIAForTest(), 0o644); err != nil {
		t.Fatal(err)
	}

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("Games", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.ErrorCount != 0 || job.IndexedFiles != 4 {
		t.Fatalf("job = %#v, want four indexed Nintendo 3DS files", job)
	}

	tests := []struct {
		path        string
		format      string
		fileName    string
		catalogRole string
	}{
		{path: directPath, format: "3ds", fileName: "Direct Game.3ds", catalogRole: "game"},
		{path: cxiPath, format: "cxi", fileName: "Executable.cxi", catalogRole: "game"},
		{path: zipPath, format: "zip", fileName: "Archive.3ds", catalogRole: "game"},
		{path: ciaPath, format: "cia", fileName: "Install Package.cia", catalogRole: "needs-curation"},
	}
	for _, test := range tests {
		game, err := st.GameByPath(test.path)
		if err != nil {
			t.Fatal(err)
		}
		if game.Platform != "3ds" || game.ROMSetName != "Nintendo 3DS" || game.EmulatorHint != "spatialemu-3ds-companion" || game.Format != test.format || game.Compatibility != "untested" || game.CatalogRole != test.catalogRole {
			t.Fatalf("game = %#v, want canonical Nintendo 3DS metadata", game)
		}
		files, err := st.GameFiles(game.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != 1 || files[0].Name != test.fileName || files[0].Role != "entry" || files[0].Size != game.Size || files[0].SHA1 == "" {
			t.Fatalf("files = %#v, want one logical checksummed entry", files)
		}
	}

	second, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if second.IndexedFiles != 0 || second.SkippedFiles != 4 {
		t.Fatalf("second job = %#v, want four unchanged files skipped", second)
	}
}

func TestInspectNintendo3DSZIPRejectsAmbiguousAndUnsafeArchives(t *testing.T) {
	tests := []struct {
		name    string
		entries map[string]string
	}{
		{name: "multiple images", entries: map[string]string{
			"One.3ds": string(makeNintendo3DSImage("NCSD")),
			"Two.cxi": string(makeNintendo3DSImage("NCCH")),
		}},
		{name: "path traversal", entries: map[string]string{
			"../Escape.3ds": string(makeNintendo3DSImage("NCSD")),
		}},
		{name: "invalid header", entries: map[string]string{
			"Invalid.3ds": string(makeNintendo3DSImage("NCCH")),
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "game.zip")
			makeZip(t, path, test.entries)
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := inspectThreeDSImage(path, info, ".zip"); err == nil {
				t.Fatal("inspectThreeDSImage succeeded, want validation error")
			}
		})
	}
}

func makeNintendo3DSImage(magic string) []byte {
	body := make([]byte, 0x240)
	for index := range body {
		body[index] = byte(index % 251)
	}
	copy(body[0x100:], magic)
	return body
}

func TestScanLibraryIndexesValidatedPC98MediaAndExcludesSupportFiles(t *testing.T) {
	root := t.TempDir()
	pc98Dir := filepath.Join(root, "PC98")
	rawPath := filepath.Join(pc98Dir, "Love Escalator AI汉化版", "Love Escalator_CN.hdi")
	zipPath := filepath.Join(pc98Dir, "GAME.PC98", "Archive Game.zip")
	if err := os.MkdirAll(filepath.Dir(rawPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(zipPath), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := syntheticPC98AnexImage(512, 4, 2, 10)
	if err := os.WriteFile(rawPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string][]byte{
		"FONT.bmp":     []byte("font"),
		"np21x64w.exe": []byte("emulator"),
		"BIOS.ROM":     []byte("firmware"),
	} {
		if err := os.WriteFile(filepath.Join(filepath.Dir(rawPath), name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	zipMedia := syntheticPC98AnexImage(1024, 2, 2, 4)
	makeZip(t, zipPath, map[string]string{
		"Archive Game.fdi": string(zipMedia),
		"README.txt":       "notes",
		"__MACOSX/._Game":  "ignored",
	})
	dosPath := filepath.Join(pc98Dir, "GAME.DOS", "DOS Game.zip")
	if err := os.MkdirAll(filepath.Dir(dosPath), 0o755); err != nil {
		t.Fatal(err)
	}
	makeZip(t, dosPath, map[string]string{"Not PC98.hdi": string(raw)})

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("Games", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.ErrorCount != 0 || job.IndexedFiles != 2 {
		t.Fatalf("job = %#v, want two validated PC-98 games", job)
	}
	page, err := st.ListGamesPage(domain.GameListOptions{Platform: "pc98", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("PC-98 page = %#v, want two games", page)
	}
	for _, game := range page.Items {
		if game.Platform != "pc98" || game.ROMSetName != "PC-98" || game.EmulatorHint != "np2kai" || game.Compatibility != "untested" || game.CRC32 == "" || game.SHA1 == "" {
			t.Fatalf("game = %#v, want canonical PC-98 metadata", game)
		}
		files, err := st.GameFiles(game.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != 1 || files[0].Role != "entry" || files[0].Size != game.Size {
			t.Fatalf("files = %#v, want one raw PC-98 entry", files)
		}
	}
	secondJob, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if secondJob.IndexedFiles != 0 || secondJob.SkippedFiles != 2 {
		t.Fatalf("second job = %#v, want unchanged PC-98 media skipped", secondJob)
	}
}

func TestScanLibraryPackagesPC98UserDiskWithCUEAsOneGame(t *testing.T) {
	root := t.TempDir()
	gameDir := filepath.Join(root, "PC98", "Policenauts (1994)(Konami)")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	userPath := filepath.Join(gameDir, "USER.FDI")
	systemPath := filepath.Join(gameDir, "SYSTEM.FDI")
	if err := os.WriteFile(userPath, syntheticPC98AnexImage(512, 4, 2, 10), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(systemPath, syntheticPC98AnexImage(512, 4, 2, 11), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gameDir, "USER.NFD"), []byte("unsupported installer disk"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gameDir, "P_NAUTS.cue"), []byte("FILE \"P_NAUTS.BIN\" BINARY\n  TRACK 01 MODE1/2352\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gameDir, "P_NAUTS.BIN"), []byte("cd payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gameDir, "KAMON.GIF"), []byte("artwork payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("Games", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.ErrorCount != 0 || job.IndexedFiles != 1 {
		t.Fatalf("job = %#v, want one PC-98 mixed-media package", job)
	}
	page, err := st.ListGamesPage(domain.GameListOptions{Platform: "pc98", Limit: 10})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("games = %#v err=%v, want one game", page.Items, err)
	}
	game := page.Items[0]
	if game.Title != "Policenauts" || game.FilePath != userPath || game.Format != "fdi" {
		t.Fatalf("game = %#v, want USER.FDI launch entry", game)
	}
	files, err := st.GameFiles(game.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantNames := []string{"USER.FDI", "P_NAUTS.cue", "P_NAUTS.BIN", "KAMON.GIF"}
	if len(files) != len(wantNames) {
		t.Fatalf("files = %#v, want complete user-disk and CD package", files)
	}
	for index, wantName := range wantNames {
		wantRole := "dependency"
		if index == 0 {
			wantRole = "entry"
		}
		if files[index].Name != wantName || files[index].Role != wantRole || files[index].SHA1 == "" {
			t.Fatalf("file %d = %#v, want %s role=%s with checksum", index, files[index], wantName, wantRole)
		}
	}
	if game.Size != gameFilesSize(files) {
		t.Fatalf("game size = %d, want package size %d", game.Size, gameFilesSize(files))
	}
	if _, err := st.GameByPath(systemPath); err == nil {
		t.Fatal("SYSTEM.FDI must not remain as a separately launchable game")
	}

	second, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if second.IndexedFiles != 0 || second.SkippedFiles != 1 || second.ErrorCount != 0 {
		t.Fatalf("second scan = %#v, want unchanged package skipped", second)
	}
}

func TestScanLibraryPackagesValidatedPC98FontAndTracksSidecarChanges(t *testing.T) {
	root := t.TempDir()
	gameDir := filepath.Join(root, "PC98", "Love Escalator AI汉化版")
	mediaPath := filepath.Join(gameDir, "Love Escalator_CN.hdi")
	fontPath := filepath.Join(gameDir, "PC98_CN.bmp")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mediaPath, syntheticPC98AnexImage(512, 4, 2, 10), 0o644); err != nil {
		t.Fatal(err)
	}
	font := syntheticPC98FontBitmap(0x00)
	if err := os.WriteFile(fontPath, font, 0o644); err != nil {
		t.Fatal(err)
	}

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("PC-98", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	first, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if first.ErrorCount != 0 || first.IndexedFiles != 1 {
		t.Fatalf("first scan = %#v, want one indexed game", first)
	}
	page, err := st.ListGamesPage(domain.GameListOptions{Platform: "pc98", Limit: 10})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("games = %#v err=%v", page.Items, err)
	}
	game := page.Items[0]
	files, err := st.GameFiles(game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Role != "entry" || files[1].Role != "font" || files[1].Name != "FONT.bmp" {
		t.Fatalf("files = %#v, want entry plus validated font", files)
	}
	if game.Size != files[0].Size+files[1].Size {
		t.Fatalf("game size = %d, want package size %d", game.Size, files[0].Size+files[1].Size)
	}

	second, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if second.IndexedFiles != 0 || second.SkippedFiles != 1 {
		t.Fatalf("second scan = %#v, want unchanged package skipped", second)
	}

	font = syntheticPC98FontBitmap(0xff)
	if err := os.WriteFile(fontPath, font, 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(fontPath, future, future); err != nil {
		t.Fatal(err)
	}
	changed, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if changed.IndexedFiles != 1 {
		t.Fatalf("changed scan = %#v, want media package refreshed", changed)
	}

	if err := os.Remove(fontPath); err != nil {
		t.Fatal(err)
	}
	removed, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if removed.IndexedFiles != 1 {
		t.Fatalf("removed scan = %#v, want stale font removed", removed)
	}
	files, err = st.GameFiles(game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Role != "entry" {
		t.Fatalf("files after removal = %#v, want entry only", files)
	}
}

func TestScanLibraryMarksNonBootablePC98HDIBroken(t *testing.T) {
	root := t.TempDir()
	gameDir := filepath.Join(root, "PC98", "Install Target")
	mediaPath := filepath.Join(gameDir, "Install Target.hdi")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	image := syntheticPC98AnexImage(512, 4, 2, 10)
	headerSize := int(binary.LittleEndian.Uint32(image[8:12]))
	sectorSize := int(binary.LittleEndian.Uint32(image[16:20]))
	image[headerSize+sectorSize] = 0
	if err := os.WriteFile(mediaPath, image, 0o644); err != nil {
		t.Fatal(err)
	}

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("PC-98", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.ErrorCount != 0 || job.IndexedFiles != 1 {
		t.Fatalf("scan = %#v, want one retained non-bootable HDI", job)
	}
	page, err := st.ListGamesPage(domain.GameListOptions{Platform: "pc98", Limit: 10})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("games = %#v err=%v", page.Items, err)
	}
	if page.Items[0].Compatibility != "broken" {
		t.Fatalf("compatibility = %q, want broken", page.Items[0].Compatibility)
	}
	sources, err := st.GameSources(page.Items[0].ID)
	if err != nil || len(sources) != 1 || !sources[0].BootabilityChecked || sources[0].Compatibility != "broken" {
		t.Fatalf("sources = %#v err=%v, want checked broken source", sources, err)
	}
	second, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if second.IndexedFiles != 0 || second.SkippedFiles != 1 {
		t.Fatalf("second scan = %#v, want checked HDI to use unchanged fast path", second)
	}
	if _, err := conn.Exec(`UPDATE game_sources SET bootability_checked = 0, compatibility = 'untested' WHERE file_path = ?`, mediaPath); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`UPDATE games SET compatibility = 'untested' WHERE id = ?`, page.Items[0].ID); err != nil {
		t.Fatal(err)
	}
	legacy, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.IndexedFiles != 1 || legacy.SkippedFiles != 0 {
		t.Fatalf("legacy scan = %#v, want unchecked HDI to be reinspected once", legacy)
	}
	updated, err := st.GameByID(page.Items[0].ID)
	if err != nil || updated.Compatibility != "broken" {
		t.Fatalf("updated game = %#v err=%v, want broken compatibility restored", updated, err)
	}
}

func TestScanLibraryBlocksYuNoSpecialDiskAsStandaloneGame(t *testing.T) {
	root := t.TempDir()
	gameDir := filepath.Join(root, "PC98", "Yu-No - Special Disk (Elf)")
	mediaPath := filepath.Join(gameDir, "YU-NO_A.D88")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mediaPath, syntheticPC98D88Image(0x7a), 0o644); err != nil {
		t.Fatal(err)
	}

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("PC-98", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.ErrorCount != 1 || job.IndexedFiles != 0 {
		t.Fatalf("scan = %#v, want standalone special disk blocked", job)
	}
	page, err := st.ListGamesPage(domain.GameListOptions{Platform: "pc98", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 0 {
		t.Fatalf("games = %#v, want no published special-disk record", page.Items)
	}
}

func TestScanLibraryBuildsDragonKnightSpecialDiskRuntimePackage(t *testing.T) {
	root := t.TempDir()
	mainDir := filepath.Join(root, "PC98", "Dragon Knight 4 (1991)(Elf)")
	specialDir := filepath.Join(root, "PC98", "Dragon Knight 4 Special Disk (1991)(Elf)")
	if err := os.MkdirAll(mainDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(specialDir, 0o755); err != nil {
		t.Fatal(err)
	}
	base := syntheticPC98AnexImage(512, 4, 2, 10)
	for i := 0; i < 12; i++ {
		image := append([]byte(nil), base...)
		image[len(image)-1] = byte(i + 1)
		name := fmt.Sprintf("DragonKnight4_%c.fdi", 'a'+rune(i))
		if err := os.WriteFile(filepath.Join(mainDir, name), image, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		image := append([]byte(nil), base...)
		image[len(image)-1] = byte(100 + i)
		name := fmt.Sprintf("DragonKnight4SpecialDisk_%c.fdi", 'a'+rune(i))
		if err := os.WriteFile(filepath.Join(specialDir, name), image, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("PC-98", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.ErrorCount != 0 || job.IndexedFiles != 14 {
		t.Fatalf("scan = %#v, want fourteen source files", job)
	}
	page, err := st.ListGamesPage(domain.GameListOptions{Platform: "pc98", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var special domain.GameAsset
	for _, game := range page.Items {
		if game.Title == "Dragon Knight 4 Special Disk" {
			special = game
		}
	}
	if special.ID == 0 {
		t.Fatalf("games = %#v, missing special disk", page.Items)
	}
	sources, err := st.GameSources(special.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 {
		t.Fatalf("special sources = %d, want only its own A/B sources", len(sources))
	}
	files, err := st.GameFiles(special.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 12 {
		t.Fatalf("special files = %#v, want own A/B plus main C-L", files)
	}
	if !strings.Contains(strings.ToLower(files[0].Name), "specialdisk_a") || !strings.Contains(strings.ToLower(files[1].Name), "specialdisk_b") ||
		!strings.Contains(strings.ToLower(files[2].Name), "_c.fdi") || !strings.Contains(strings.ToLower(files[11].Name), "_l.fdi") {
		t.Fatalf("special file order = %#v", files)
	}
	second, err := New(st).ScanLibraryPath(lib, specialDir)
	if err != nil {
		t.Fatal(err)
	}
	if second.IndexedFiles != 0 || second.SkippedFiles != 2 {
		t.Fatalf("second special scan = %#v, want completed package skipped", second)
	}
}

func TestScanLibraryRejectsAmbiguousPC98ZIP(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "PC-98", "Multiple.zip")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	image := syntheticPC98AnexImage(512, 4, 2, 4)
	makeZip(t, path, map[string]string{"Disk A.hdi": string(image), "Disk B.hdi": string(image)})
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("Games", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	games, err := st.ListRecentGames(10)
	if err != nil {
		t.Fatal(err)
	}
	if job.ErrorCount != 1 || len(games) != 0 {
		t.Fatalf("job = %#v, games = %#v, want manual-review error and no game", job, games)
	}
}

func TestScanLibraryNormalizesPC98NamesDeduplicatesSourcesAndGroupsDisks(t *testing.T) {
	root := t.TempDir()
	pc98Dir := filepath.Join(root, "PC98")
	arcushuImage := syntheticPC98D88Image(0)
	diskOneImage := append([]byte(nil), arcushuImage...)
	diskOneImage[len(diskOneImage)-1] = 1
	diskTwoImage := append([]byte(nil), arcushuImage...)
	diskTwoImage[len(diskTwoImage)-1] = 2
	prismImage := append([]byte(nil), arcushuImage...)
	prismImage[len(prismImage)-1] = 3

	cp932Name, err := japanese.ShiftJIS.NewEncoder().Bytes([]byte("アークシュ.D88"))
	if err != nil {
		t.Fatal(err)
	}
	arcushuPath := filepath.Join(pc98Dir, "Wolf Team", "Arcushu.zip")
	arcushuCopyPath := filepath.Join(pc98Dir, "Wolf Team Mirror", "Arcushu Copy.zip")
	diskOnePath := filepath.Join(pc98Dir, "Space Quest", "Space Quest Disk 1.zip")
	diskTwoPath := filepath.Join(pc98Dir, "Space Quest", "Space Quest Disk 2.zip")
	makeRawNameZip(t, arcushuPath, string(cp932Name), arcushuImage)
	makeRawNameZip(t, arcushuCopyPath, string(cp932Name), arcushuImage)
	makeZip(t, diskOnePath, map[string]string{"1.D88": string(diskOneImage)})
	makeZip(t, diskTwoPath, map[string]string{"2.D88": string(diskTwoImage)})
	makeZip(t, filepath.Join(pc98Dir, "Telenet Japan", "プリズム98.zip"), map[string]string{"1.D88": string(prismImage)})

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("Games", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	// Seed the old one-file-per-record shape to verify migration consolidates
	// existing IDs instead of only grouping a clean database correctly.
	for _, legacy := range []struct {
		path  string
		title string
		image []byte
	}{
		{arcushuPath, "Arcushu", arcushuImage},
		{arcushuCopyPath, "Arcushu Copy", arcushuImage},
		{diskOnePath, "Space Quest Disk 1", diskOneImage},
		{diskTwoPath, "Space Quest Disk 2", diskTwoImage},
	} {
		info, statErr := os.Stat(legacy.path)
		if statErr != nil {
			t.Fatal(statErr)
		}
		legacyChecksums, checksumErr := validateAndChecksumPC98Media(bytes.NewReader(legacy.image), uint64(len(legacy.image)), ".d88")
		if checksumErr != nil {
			t.Fatal(checksumErr)
		}
		if _, upsertErr := st.UpsertGame(domain.GameAsset{
			LibraryID: lib.ID, Title: legacy.title, Platform: "pc98", ROMSetName: "PC-98", Format: "zip",
			FilePath: legacy.path, RelPath: filepath.ToSlash(strings.TrimPrefix(legacy.path, root+string(filepath.Separator))),
			Size: info.Size(), MTime: info.ModTime(), CRC32: legacyChecksums.crc32, SHA1: legacyChecksums.sha1,
			EmulatorHint: "np2kai", Compatibility: "untested",
		}); upsertErr != nil {
			t.Fatal(upsertErr)
		}
	}
	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.ErrorCount != 0 {
		errors, listErr := st.ListFileErrorsByJob(job.ID)
		if listErr != nil {
			t.Fatal(listErr)
		}
		t.Fatalf("job = %#v, errors = %#v, want clean PC-98 scan", job, errors)
	}
	page, err := st.ListGamesPage(domain.GameListOptions{Platform: "pc98", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	// Arcushu's mirrored image is one record, the two Space Quest disks are one record,
	// and Prism 98 keeps the container title instead of the generic internal name "1".
	if page.Total != 3 {
		t.Fatalf("games = %#v, want three content/group records", page.Items)
	}
	var arcushu domain.GameAsset
	var spaceQuest domain.GameAsset
	var prism domain.GameAsset
	for _, game := range page.Items {
		if strings.ContainsRune(game.Title, '\uFFFD') || game.Title == "1" {
			t.Fatalf("game title = %q, want decoded container title", game.Title)
		}
		switch game.Title {
		case "Arcushu Copy", "Arcushu":
			arcushu = game
		case "Space Quest":
			spaceQuest = game
		case "プリズム98":
			prism = game
		}
	}
	if arcushu.ID == 0 || spaceQuest.ID == 0 || prism.ID == 0 {
		t.Fatalf("games = %#v, want Arcushu, Space Quest, and Prism 98", page.Items)
	}
	arcushuSources, err := st.GameSources(arcushu.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(arcushuSources) != 2 {
		t.Fatalf("Arcushu sources = %#v, want two retained source paths", arcushuSources)
	}
	arcushuFiles, err := st.GameFiles(arcushu.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(arcushuFiles) != 1 || arcushuFiles[0].Name != "アークシュ.D88" {
		t.Fatalf("Arcushu files = %#v, want decoded CP932 entry", arcushuFiles)
	}
	spaceFiles, err := st.GameFiles(spaceQuest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(spaceFiles) != 2 || spaceFiles[0].Role != "entry" || spaceFiles[1].Role != "dependency" || spaceFiles[0].Name != "1.D88" || spaceFiles[1].Name != "2.D88" {
		t.Fatalf("Space Quest files = %#v, want ordered two-disk manifest", spaceFiles)
	}
	spaceSources, err := st.GameSources(spaceQuest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(spaceSources) != 2 || spaceSources[0].DiskOrder != 1 || spaceSources[1].DiskOrder != 2 {
		t.Fatalf("Space Quest sources = %#v, want parsed disk order 1, 2", spaceSources)
	}
}

func TestPC98SourceIdentityGroupsBareAlphaDisksByParentDirectory(t *testing.T) {
	root := filepath.Join("games", "PC98")
	mainDir := filepath.Join(root, "Dragon Knight 4 (1991)(Elf)")
	specialDir := filepath.Join(root, "Dragon Knight 4 Special Disk (1991)(Elf)")

	mainTitleA, mainKeyA, mainOrderA := pc98SourceIdentity(root, filepath.Join(mainDir, "DragonKnight4_a.fdi"), "DragonKnight4_a.fdi")
	mainTitleL, mainKeyL, mainOrderL := pc98SourceIdentity(root, filepath.Join(mainDir, "DragonKnight4_l.fdi"), "DragonKnight4_l.fdi")
	specialTitle, specialKey, specialOrder := pc98SourceIdentity(root, filepath.Join(specialDir, "DragonKnight4SpecialDisk_a.fdi"), "DragonKnight4SpecialDisk_a.fdi")

	if mainTitleA != "Dragon Knight 4" || mainTitleL != "Dragon Knight 4" {
		t.Fatalf("main titles = %q, %q, want parent directory title", mainTitleA, mainTitleL)
	}
	if mainKeyA != mainKeyL || mainOrderA != 1 || mainOrderL != 12 {
		t.Fatalf("main keys/orders = %q/%d, %q/%d, want one 12-disk group", mainKeyA, mainOrderA, mainKeyL, mainOrderL)
	}
	if specialTitle != "Dragon Knight 4 Special Disk" || specialOrder != 1 {
		t.Fatalf("special title/order = %q/%d", specialTitle, specialOrder)
	}
	if specialKey == mainKeyA {
		t.Fatal("special disk must remain separate from the main game")
	}
}

func TestValidatePC98MediaHeaderAcceptsRawSizedFDI(t *testing.T) {
	prefix := []byte("\xeb\x10\x90 ELFDOS FORMAT")
	if err := validatePC98MediaHeader(prefix, 1261568, ".fdi"); err != nil {
		t.Fatalf("raw-sized FDI rejected: %v", err)
	}
	if err := validatePC98MediaHeader(prefix, 1234567, ".fdi"); err == nil {
		t.Fatal("arbitrary-sized FDI should remain rejected")
	}
}

func syntheticPC98AnexImage(sectorSize, sectors, surfaces, cylinders uint32) []byte {
	const headerSize = uint32(4096)
	dataSize := sectorSize * sectors * surfaces * cylinders
	image := make([]byte, int(headerSize+dataSize))
	binary.LittleEndian.PutUint32(image[8:12], headerSize)
	binary.LittleEndian.PutUint32(image[12:16], dataSize)
	binary.LittleEndian.PutUint32(image[16:20], sectorSize)
	binary.LittleEndian.PutUint32(image[20:24], sectors)
	binary.LittleEndian.PutUint32(image[24:28], surfaces)
	binary.LittleEndian.PutUint32(image[28:32], cylinders)
	for index := int(headerSize); index < len(image); index++ {
		image[index] = byte(index)
	}
	partitionOffset := int(headerSize + sectorSize)
	image[partitionOffset] = 0x80
	binary.LittleEndian.PutUint16(image[partitionOffset+6:partitionOffset+8], 1)
	copy(image[partitionOffset+16:partitionOffset+32], []byte("MS-DOS"))
	bootOffset := int(headerSize + sectorSize*sectors*surfaces)
	binary.LittleEndian.PutUint16(image[bootOffset+11:bootOffset+13], uint16(sectorSize))
	binary.LittleEndian.PutUint16(image[bootOffset+14:bootOffset+16], 1)
	image[bootOffset+16] = 2
	binary.LittleEndian.PutUint16(image[bootOffset+17:bootOffset+19], 16)
	binary.LittleEndian.PutUint16(image[bootOffset+22:bootOffset+24], 1)
	rootOffset := bootOffset + int(3*sectorSize)
	copy(image[rootOffset:rootOffset+11], []byte("IO      SYS"))
	copy(image[rootOffset+32:rootOffset+43], []byte("MSDOS   SYS"))
	copy(image[rootOffset+64:rootOffset+75], []byte("COMMAND COM"))
	return image
}

func syntheticPC98D88Image(marker byte) []byte {
	const headerSize = 0x2b0
	image := make([]byte, headerSize+512)
	binary.LittleEndian.PutUint32(image[0x1c:0x20], uint32(len(image)))
	binary.LittleEndian.PutUint32(image[0x20:0x24], headerSize)
	image[len(image)-1] = marker
	return image
}

func syntheticPC98FontBitmap(fill byte) []byte {
	const pixelOffset = 62
	const pixelBytes = 2048 * 2048 / 8
	data := make([]byte, pixelOffset+pixelBytes)
	copy(data[:2], []byte("BM"))
	binary.LittleEndian.PutUint32(data[2:6], uint32(len(data)))
	binary.LittleEndian.PutUint32(data[10:14], pixelOffset)
	binary.LittleEndian.PutUint32(data[14:18], 40)
	binary.LittleEndian.PutUint32(data[18:22], 2048)
	binary.LittleEndian.PutUint32(data[22:26], 2048)
	binary.LittleEndian.PutUint16(data[26:28], 1)
	binary.LittleEndian.PutUint16(data[28:30], 1)
	binary.LittleEndian.PutUint32(data[34:38], pixelBytes)
	for i := pixelOffset; i < len(data); i++ {
		data[i] = fill
	}
	return data
}

func TestScanLibraryImportsGamelistMetadata(t *testing.T) {
	root := t.TempDir()
	platformDir := filepath.Join(root, "SNES")
	romPath := filepath.Join(platformDir, "Super Mario World (USA).sfc")
	coverPath := filepath.Join(platformDir, "media", "covers", "Super Mario World.png")
	manualPath := filepath.Join(platformDir, "manuals", "Super Mario World.pdf")
	outsidePath := filepath.Join(filepath.Dir(root), "outside.png")
	if err := os.MkdirAll(filepath.Dir(coverPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(manualPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(romPath, []byte("rom-body"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(coverPath, []byte("cover"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manualPath, []byte("manual"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outsidePath, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	gamelist := `<?xml version="1.0"?>
<gameList>
  <game>
    <path>./Super Mario World (USA).sfc</path>
    <name>Super Mario World</name>
    <desc>Dinosaur Land platform adventure.</desc>
    <releasedate>19901121T000000</releasedate>
    <developer>Nintendo EAD</developer>
    <publisher>Nintendo</publisher>
    <genre>Platform</genre>
    <players>1-2</players>
    <image>./media/covers/Super Mario World.png</image>
    <manual>./manuals/Super Mario World.pdf</manual>
    <screenshot>../../outside.png</screenshot>
  </game>
</gameList>`
	if err := os.WriteFile(filepath.Join(platformDir, "gamelist.xml"), []byte(gamelist), 0o644); err != nil {
		t.Fatal(err)
	}

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("Games", root, "game")
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
	details, err := st.GameDetails(games[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if details.MetadataStatus != "matched" {
		t.Fatalf("metadata status = %q, want matched", details.MetadataStatus)
	}
	if details.Metadata.DisplayTitle != "Super Mario World" ||
		details.Metadata.Summary != "Dinosaur Land platform adventure." ||
		details.Metadata.ReleaseDate != "1990-11-21" ||
		details.Metadata.Players != "1-2" {
		t.Fatalf("metadata = %#v, want gamelist fields", details.Metadata)
	}
	if len(details.Metadata.Genres) != 1 || details.Metadata.Genres[0] != "Platform" {
		t.Fatalf("genres = %#v, want Platform", details.Metadata.Genres)
	}
	if len(details.Metadata.Developers) != 1 || details.Metadata.Developers[0] != "Nintendo EAD" {
		t.Fatalf("developers = %#v, want Nintendo EAD", details.Metadata.Developers)
	}
	if len(details.Metadata.Publishers) != 1 || details.Metadata.Publishers[0] != "Nintendo" {
		t.Fatalf("publishers = %#v, want Nintendo", details.Metadata.Publishers)
	}
	if len(details.Sources) != 1 ||
		details.Sources[0].Source != "gamelist" ||
		details.Sources[0].MatchedBy != "path" ||
		details.Sources[0].Confidence != 1 {
		t.Fatalf("sources = %#v, want gamelist path match", details.Sources)
	}

	artworkByKind := map[string]domain.GameArtwork{}
	for _, artwork := range details.Artwork {
		artworkByKind[artwork.Kind] = artwork
	}
	cover := artworkByKind["cover"]
	if cover.Source != "gamelist" || cover.CachePath != coverPath || !cover.Selected {
		t.Fatalf("cover artwork = %#v, want selected local cover", cover)
	}
	manual := artworkByKind["manual"]
	if manual.Source != "gamelist" || manual.CachePath != manualPath {
		t.Fatalf("manual artwork = %#v, want local manual", manual)
	}
	if screenshot, ok := artworkByKind["screenshot"]; ok {
		t.Fatalf("screenshot artwork = %#v, want outside-library path ignored", screenshot)
	}
}

func TestScanLibraryImportsRootGamelistMetadata(t *testing.T) {
	root := t.TempDir()
	romPath := filepath.Join(root, "SNES", "Chrono Trigger.sfc")
	coverPath := filepath.Join(root, "media", "Chrono Trigger.png")
	if err := os.MkdirAll(filepath.Dir(romPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(coverPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(romPath, []byte("rom-body"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(coverPath, []byte("cover"), 0o644); err != nil {
		t.Fatal(err)
	}
	gamelist := `<?xml version="1.0"?>
<gameList>
  <game>
    <path>./SNES/Chrono Trigger.sfc</path>
    <name>Chrono Trigger</name>
    <desc>Time travel RPG.</desc>
    <releasedate>19950311T000000</releasedate>
    <developer>Square</developer>
    <publisher>Square</publisher>
    <genre>RPG</genre>
    <players>1</players>
    <image>./media/Chrono Trigger.png</image>
  </game>
</gameList>`
	if err := os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(gamelist), 0o644); err != nil {
		t.Fatal(err)
	}

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("Games", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(st).ScanLibrary(lib); err != nil {
		t.Fatal(err)
	}
	games, err := st.ListRecentGames(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 {
		t.Fatalf("games len = %d, want 1", len(games))
	}
	details, err := st.GameDetails(games[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if details.Metadata.DisplayTitle != "Chrono Trigger" || details.Metadata.ReleaseDate != "1995-03-11" {
		t.Fatalf("metadata = %#v, want root gamelist fields", details.Metadata)
	}
	if len(details.Artwork) != 1 || details.Artwork[0].Kind != "cover" || details.Artwork[0].CachePath != coverPath {
		t.Fatalf("artwork = %#v, want root gamelist cover", details.Artwork)
	}
}

func TestScanLibraryIndexesVideoMetadata(t *testing.T) {
	root := t.TempDir()
	videoPath := filepath.Join(root, "Movies", "Demo.Movie.mp4")
	if err := os.MkdirAll(filepath.Dir(videoPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(videoPath, []byte("video-body"), 0o644); err != nil {
		t.Fatal(err)
	}

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("Movies", root, "video")
	if err != nil {
		t.Fatal(err)
	}

	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "completed" || job.IndexedFiles != 1 || job.ErrorCount != 0 {
		t.Fatalf("job = %#v, want one indexed video and no errors", job)
	}

	videos, err := st.ListRecentVideos(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(videos) != 1 {
		t.Fatalf("videos len = %d, want 1", len(videos))
	}
	video := videos[0]
	if video.Title != "Demo Movie" || video.Format != "mp4" || video.Size != int64(len("video-body")) || video.ThumbnailStatus != "placeholder" {
		t.Fatalf("video = %#v, want inferred video metadata", video)
	}
	if video.FilePath == "" {
		t.Fatalf("video file path is empty, scanner should keep internal path")
	}

	secondJob, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if secondJob.SkippedFiles != 1 || secondJob.IndexedFiles != 0 {
		t.Fatalf("second job = %#v, want unchanged video skipped", secondJob)
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

	game, err := st.GameByPath(filepath.Join(root, "Arcade", "mslug.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if game.Title != "mslug" || game.Format != "zip" || game.Platform != "arcade" || game.ROMSetName != "Arcade" || game.CatalogRole != "needs-curation" {
		t.Fatalf("game = %#v, want indexed but quarantined Arcade zip ROM set", game)
	}

	series, err := st.ListSeries()
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 0 {
		t.Fatalf("series = %#v, want no comic series for game zip", series)
	}
}

func TestScanLibraryIndexesDreamcastGDIAsOneLaunchableGame(t *testing.T) {
	root := t.TempDir()
	gameDir := filepath.Join(root, "DC", "Example Game")
	if err := os.MkdirAll(filepath.Join(gameDir, "__MACOSX"), 0o755); err != nil {
		t.Fatal(err)
	}
	gdi := "3\n1 0 4 2352 track01.bin 0\n2 45000 0 2352 track03.bin 0\n3 45150 4 2352 track05.bin 0\n"
	for name, data := range map[string]string{
		"Example Game.gdi":       gdi,
		"track01.bin":            "track-one",
		"track03.bin":            "track-three",
		"track05.bin":            "track-five",
		"._track01.bin":          "apple-double",
		"._Example Game.gdi":     gdi,
		".DS_Store":              "finder",
		"__MACOSX/._track03.bin": "apple-double",
	} {
		if err := os.WriteFile(filepath.Join(gameDir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("Games", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	trackInfo, err := os.Stat(filepath.Join(gameDir, "track01.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertGame(domain.GameAsset{
		LibraryID: lib.ID, Title: "track01", Platform: "disc", ROMSetName: "DC", Format: "bin",
		FilePath: filepath.Join(gameDir, "track01.bin"), RelPath: "DC/Example Game/track01.bin",
		Size: trackInfo.Size(), MTime: trackInfo.ModTime(), EmulatorHint: "disc", Compatibility: "unknown",
	}); err != nil {
		t.Fatal(err)
	}

	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.ErrorCount != 0 {
		t.Fatalf("job = %#v, want no Dreamcast scan errors", job)
	}
	games, err := st.ListRecentGames(20)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 {
		t.Fatalf("games = %#v, want one GDI game and no track records", games)
	}
	game := games[0]
	if game.Platform != "dreamcast" || game.ROMSetName != "DC" || game.EmulatorHint != "dreamcast" || game.Format != "gdi" {
		t.Fatalf("game = %#v, want canonical Dreamcast metadata", game)
	}
	files, err := st.GameFiles(game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 4 || files[0].Role != "entry" || files[0].Name != "Example Game.gdi" || files[3].Name != "track05.bin" {
		t.Fatalf("game files = %#v, want entry plus three ordered tracks", files)
	}

	secondJob, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if secondJob.IndexedFiles != 0 || secondJob.SkippedFiles != 1 || secondJob.ErrorCount != 0 {
		t.Fatalf("second job = %#v, want one unchanged launchable game skipped", secondJob)
	}
}

func TestScanLibraryIndexesKonamiPython1DescriptorsAsThreeGames(t *testing.T) {
	root := t.TempDir()
	pythonRoot := filepath.Join(root, "KONAMI", "konami_python_1_collection", "kpython1")
	if err := os.MkdirAll(pythonRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	type sample struct {
		stem  string
		title string
		chd   string
	}
	samples := []sample{
		{stem: "pesta", title: "Pro Evolution Soccer the Arcade", chd: "c18eaa03.chd"},
		{stem: "wswe", title: "World Soccer Winning Eleven Arcade Game", chd: "c18jaa03.chd"},
		{stem: "wswe2k3", title: "World Soccer Winning Eleven Arcade Game 2003", chd: "c27jaa03.chd"},
	}
	for _, sample := range samples {
		gameDir := filepath.Join(pythonRoot, sample.stem)
		if err := os.MkdirAll(gameDir, 0o755); err != nil {
			t.Fatal(err)
		}
		dependencies := []string{sample.chd, "m48t58y.u48", "b22a01.u42", "d72872gc.crom", "ds2430.u3", "kn00002.ps2", "kn00002.id"}
		for _, name := range dependencies {
			if err := os.WriteFile(filepath.Join(gameDir, name), []byte(sample.stem+"-"+name), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		descriptor := fmt.Sprintf(`[Game]
Name=%s
CfImagePath=%s/%s
BbsRamPath=%s/m48t58y.u48
IoBootRomPath=%s/b22a01.u42
IoConfigRomPath=%s/d72872gc.crom
InternalDonglePath=%s/ds2430.u3
MemoryCardDonglePath=%s/kn00002.ps2
MemoryCardIdPath=%s/kn00002.id
`, sample.title, sample.stem, sample.chd, sample.stem, sample.stem, sample.stem, sample.stem, sample.stem, sample.stem)
		if err := os.WriteFile(filepath.Join(pythonRoot, sample.stem+".py1"), []byte(descriptor), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range map[string]string{
		"coh.bin": "client-bios", "keys/global.key": "client-key", "@eaDir/cache.bin": "metadata",
	} {
		path := filepath.Join(pythonRoot, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("GameROMS", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	job, err := NewWithWorkerCount(st, func() int { return 2 }).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.IndexedFiles != 3 || job.ErrorCount != 0 {
		t.Fatalf("job = %#v, want three Python 1 games and no errors", job)
	}
	page, err := st.ListGamesPage(domain.GameListOptions{Platform: "konami-python1", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || len(page.Items) != 3 {
		t.Fatalf("games = %#v, want exactly three Python 1 entries", page)
	}
	byTitle := make(map[string]domain.GameAsset, len(page.Items))
	for _, game := range page.Items {
		byTitle[game.Title] = game
		if game.Platform != "konami-python1" || game.ROMSetName != "KONAMI-PYTHON1" || game.Format != "py1" || game.EmulatorHint != "pcsx2-reliquary" || game.CatalogRole != "game" {
			t.Fatalf("game = %#v, want canonical Python 1 metadata", game)
		}
		files, err := st.GameFiles(game.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != 8 || files[0].Role != "entry" || filepath.Ext(files[0].Name) != ".py1" {
			t.Fatalf("files = %#v, want one descriptor plus seven dependencies", files)
		}
		for index, file := range files {
			if len(file.SHA1) != 40 || file.Size <= 0 || file.Position != index {
				t.Fatalf("file = %#v, want contiguous checksummed manifest", file)
			}
			if strings.Contains(strings.ToLower(file.Name), "keys/") || strings.Contains(strings.ToLower(file.Name), "bios") {
				t.Fatalf("manifest leaked client runtime file: %#v", file)
			}
		}
	}
	if _, ok := byTitle["World Soccer Winning Eleven Arcade Game 2003"]; !ok {
		t.Fatalf("titles = %#v, descriptor title was not imported", byTitle)
	}
	secondJob, err := NewWithWorkerCount(st, func() int { return 2 }).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if secondJob.IndexedFiles != 0 || secondJob.SkippedFiles != 3 || secondJob.ErrorCount != 0 {
		t.Fatalf("second job = %#v, want three unchanged descriptors skipped", secondJob)
	}
}

func TestKonamiPython1DescriptorRejectsDependencyPathEscape(t *testing.T) {
	data := []byte(`[Game]
Name=Unsafe
CfImagePath=../bios/system.chd
BbsRamPath=game/m48t58y.u48
IoBootRomPath=game/b22a01.u42
IoConfigRomPath=game/d72872gc.crom
InternalDonglePath=game/ds2430.u3
MemoryCardDonglePath=game/kn00002.ps2
MemoryCardIdPath=game/kn00002.id
`)
	if _, err := parseKonamiPython1Descriptor(data); err == nil || !strings.Contains(err.Error(), "escapes game directory") {
		t.Fatalf("parse error = %v, want path escape rejection", err)
	}
}

func TestScanLibraryIndexesSaturnCUEAsOneLaunchableGame(t *testing.T) {
	t.Setenv("FOLIOSPACE_SCAN_WORKERS", "2")
	root := t.TempDir()
	gameDir := filepath.Join(root, "Guardian Heroes")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cue := `FILE "C:\SATURN\Guardian Heroes (Track 01).BIN" BINARY
  TRACK 01 MODE1/2352
FILE "Guardian Heroes (Track 02).WAV" WAVE
  TRACK 02 AUDIO
`
	for name, data := range map[string]string{
		"Guardian Heroes.cue":            cue,
		"Guardian Heroes (Track 01).bin": "data-track",
		"Guardian Heroes (Track 02).wav": "audio-track",
		"._Guardian Heroes.cue":          "apple-double",
		".DS_Store":                      "finder",
	} {
		if err := os.WriteFile(filepath.Join(gameDir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("Saturn", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	trackPath := filepath.Join(gameDir, "Guardian Heroes (Track 01).bin")
	trackInfo, err := os.Stat(trackPath)
	if err != nil {
		t.Fatal(err)
	}
	if !isDiscTrackDependency(trackPath) {
		t.Fatal("CUE data track was not recognized as a dependency")
	}
	if _, err := st.UpsertGame(domain.GameAsset{
		LibraryID: lib.ID, Title: "Guardian Heroes Track 01", Platform: "disc", ROMSetName: "SS", Format: "bin",
		FilePath: trackPath, RelPath: "Guardian Heroes/Guardian Heroes (Track 01).bin",
		Size: trackInfo.Size(), MTime: trackInfo.ModTime(), EmulatorHint: "disc", Compatibility: "unknown",
	}); err != nil {
		t.Fatal(err)
	}

	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.IndexedFiles != 1 || job.ErrorCount != 0 {
		t.Fatalf("job = %#v, want one Saturn disc indexed without track records", job)
	}
	games, err := st.ListRecentGames(20)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 {
		t.Fatalf("games = %#v, want one CUE game and no independent tracks", games)
	}
	game := games[0]
	if game.Platform != "saturn" || game.ROMSetName != "SS" || game.EmulatorHint != "saturn" || game.Format != "cue" {
		t.Fatalf("game = %#v, want canonical Saturn metadata", game)
	}
	files, err := st.GameFiles(game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 || files[0].Name != "Guardian Heroes.cue" || files[0].Role != "entry" || files[1].Name != "Guardian Heroes (Track 01).BIN" || files[1].Role != "dependency" || files[2].Name != "Guardian Heroes (Track 02).WAV" {
		t.Fatalf("game files = %#v, want cue plus two ordered dependencies", files)
	}
	normalized, err := NormalizeCUEFileReferences([]byte(cue))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(normalized), `C:\SATURN`) || !strings.Contains(string(normalized), `FILE "Guardian Heroes (Track 01).BIN" BINARY`) {
		t.Fatalf("normalized CUE = %q, want safe relative FILE references", normalized)
	}
	facets, err := st.ListGameFacets(domain.GameListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if facets.Total != 1 || len(facets.Platforms) != 1 || facets.Platforms[0].Platform != "saturn" || facets.Platforms[0].Count != 1 {
		t.Fatalf("facets = %#v, want one launchable Saturn disc", facets)
	}

	secondJob, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if secondJob.IndexedFiles != 0 || secondJob.SkippedFiles != 1 || secondJob.ErrorCount != 0 {
		t.Fatalf("second job = %#v, want one unchanged Saturn disc skipped", secondJob)
	}
}

func TestScanLibraryIndexes3DOCUEAndSingleFileImages(t *testing.T) {
	t.Setenv("FOLIOSPACE_SCAN_WORKERS", "2")
	root := t.TempDir()
	gameDir := filepath.Join(root, "3DO", "Gex")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"Gex.cue":     "FILE \"GEX.BIN\" BINARY\n  TRACK 01 MODE1/2352\n    INDEX 01 00:00:00\n",
		"gex.bin":     "3do-track",
		"panafz1.bin": "3do-bios",
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(gameDir, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name := range map[string]struct{}{"Super Street Fighter II Turbo.iso": {}, "Yu Yu Hakusho.chd": {}} {
		if err := os.WriteFile(filepath.Join(root, "3DO", name), []byte("disc-image"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("Games", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.IndexedFiles != 3 || job.ErrorCount != 0 {
		t.Fatalf("job = %#v, want three 3DO games without BIOS entries", job)
	}
	games, err := st.ListRecentGames(20)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 3 {
		t.Fatalf("games = %#v, want CUE, ISO, and CHD only", games)
	}
	for _, game := range games {
		if game.Platform != "3do" || game.ROMSetName != "3DO" || game.EmulatorHint != "opera" || game.CatalogRole != "game" {
			t.Fatalf("game = %#v, want canonical 3DO metadata", game)
		}
		if game.Title == "panafz1" {
			t.Fatal("3DO BIOS was published as a game")
		}
		if game.Format == "cue" {
			indexed, err := st.GameFiles(game.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(indexed) != 2 || indexed[0].Name != "Gex.cue" || indexed[0].Role != "entry" || indexed[1].Name != "GEX.BIN" || indexed[1].Role != "dependency" {
				t.Fatalf("3DO CUE files = %#v", indexed)
			}
		}
	}
}

func TestGameTitlePreservingDiscLabel(t *testing.T) {
	for _, test := range []struct {
		name string
		want string
	}{
		{"D no Shokutaku (Japan) (Disc 1).cue", "D no Shokutaku (Disc 1)"},
		{"D no Shokutaku (Japan) (Disc 2).cue", "D no Shokutaku (Disc 2)"},
		{"Gex (Europe).cue", "Gex"},
	} {
		if got := gameTitlePreservingDiscLabel(test.name); got != test.want {
			t.Fatalf("gameTitlePreservingDiscLabel(%q) = %q, want %q", test.name, got, test.want)
		}
	}
}

func TestScanLibraryIndexesNDSAsSingleFileGame(t *testing.T) {
	root := t.TempDir()
	ndsDir := filepath.Join(root, "Nintendo DS")
	if err := os.MkdirAll(ndsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string]string{
		"Mario Kart DS.nds": "nds-rom",
		"bios7.bin":         "bios",
		"bios9.bin":         "bios",
		"firmware.bin":      "firmware",
		"nand.dsi":          "dsi-nand",
	} {
		if err := os.WriteFile(filepath.Join(ndsDir, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("Games", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.IndexedFiles != 1 || job.ErrorCount != 0 {
		t.Fatalf("job = %#v, want one NDS game and no firmware entries", job)
	}
	games, err := st.ListRecentGames(20)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 {
		t.Fatalf("games = %#v, want one NDS game", games)
	}
	game := games[0]
	if game.Platform != "nds" || game.ROMSetName != "Nintendo DS" || game.EmulatorHint != "melonds-ds" || game.Format != "nds" {
		t.Fatalf("game = %#v, want canonical NDS metadata", game)
	}
	indexed, err := st.GameFiles(game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(indexed) != 1 || indexed[0].Name != "Mario Kart DS.nds" || indexed[0].Role != "entry" {
		t.Fatalf("NDS files = %#v", indexed)
	}
}

func TestScanLibraryIndexesSingleNDSROMInsideZIP(t *testing.T) {
	root := t.TempDir()
	ndsDir := filepath.Join(root, "NDS")
	archivePath := filepath.Join(ndsDir, "(NDS) 0001 - Electroplankton (J)(Trashman).zip")
	romName := "0001 - Electroplankton (J)(Trashman).nds"
	romBody := "nds-rom-body"
	makeZip(t, archivePath, map[string]string{
		romName:                  romBody,
		"__MACOSX/._ignored.nds": "resource-fork",
	})

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("Games", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.IndexedFiles != 1 || job.ErrorCount != 0 {
		t.Fatalf("job = %#v, want one zipped NDS game", job)
	}
	games, err := st.ListRecentGames(20)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 {
		t.Fatalf("games = %#v, want one zipped NDS game", games)
	}
	game := games[0]
	if game.Title != "Electroplankton" || game.Platform != "nds" || game.Region != "Japan" || game.Format != "nds" || game.Size != int64(len(romBody)) || game.FilePath != archivePath {
		t.Fatalf("game = %#v, want normalized zipped NDS metadata", game)
	}
	files, err := st.GameFiles(game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name != romName || files[0].FilePath != archivePath || files[0].Role != "entry" || files[0].Size != int64(len(romBody)) {
		t.Fatalf("files = %#v, want inner NDS entry", files)
	}
}

func TestScanLibraryIndexesModel2CatalogAndHidesDependency(t *testing.T) {
	root := t.TempDir()
	model2Dir := filepath.Join(root, "Model2")
	makeZip(t, filepath.Join(model2Dir, "vf2.zip"), map[string]string{"vf2.bin": "virtua-fighter"})
	makeZip(t, filepath.Join(model2Dir, "daytona.zip"), map[string]string{"daytona.bin": "daytona"})
	makeZip(t, filepath.Join(model2Dir, "segabill.zip"), map[string]string{"epr-18022.ic2": "firmware"})

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("Games", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.IndexedFiles != 3 || job.ErrorCount != 0 {
		t.Fatalf("job = %#v, want two games and one dependency indexed", job)
	}

	page, err := st.ListGamesPage(domain.GameListOptions{Platform: "model2", Limit: 20, Sort: "title"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("page = %#v, want two visible Model 2 games", page)
	}
	byTitle := map[string]domain.GameAsset{}
	for _, game := range page.Items {
		byTitle[game.Title] = game
		fileName := filepath.Base(game.FilePath)
		if game.Platform != "model2" || game.ROMSetName != strings.ToLower(strings.TrimSuffix(fileName, filepath.Ext(fileName))) || game.EmulatorHint != "model2" || game.Format != "zip" || game.CatalogRole != "needs-curation" {
			t.Fatalf("game = %#v, want canonical Model 2 metadata", game)
		}
	}
	if byTitle["Virtua Fighter 2"].Compatibility != "untested" || byTitle["Daytona USA"].Compatibility != "broken" {
		t.Fatalf("games = %#v, want contract compatibility states", page.Items)
	}
	for _, query := range []string{"vf2", "Virtua Fighter 2"} {
		search, err := st.ListGamesPage(domain.GameListOptions{Query: query, Platform: "model2", Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if search.Total != 1 || len(search.Items) != 1 || search.Items[0].Title != "Virtua Fighter 2" {
			t.Fatalf("search %q = %#v, want vf2", query, search)
		}
	}

	dependency, err := st.ListGamesPage(domain.GameListOptions{Query: "segabill", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if dependency.Total != 1 || len(dependency.Items) != 1 || dependency.Items[0].CatalogRole != "dependency" || dependency.Items[0].Compatibility != "unknown" {
		t.Fatalf("dependency = %#v, want searchable hidden segabill package", dependency)
	}
	facets, err := st.ListGameFacets(domain.GameListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if facets.Total != 2 || len(facets.Platforms) != 1 || facets.Platforms[0].Platform != "model2" || facets.Platforms[0].Count != 2 {
		t.Fatalf("facets = %#v, want dependencies excluded from Model 2 count", facets)
	}
}

func TestScanLibraryKeepsModel3EntryZIPStemAsROMSetName(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "Model3ROMs", "SpikeOut_Final_Edition.zip")
	makeZip(t, archivePath, map[string]string{"epr-21653.ic17": "model3-rom"})

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("Games", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.IndexedFiles != 1 || job.ErrorCount != 0 {
		t.Fatalf("job = %#v, want one Model 3 game", job)
	}

	page, err := st.ListGamesPage(domain.GameListOptions{Platform: "model3", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("page = %#v, want one Model 3 game", page)
	}
	game := page.Items[0]
	if game.ROMSetName != "spikeout_final_edition" || filepath.Base(game.FilePath) != "SpikeOut_Final_Edition.zip" {
		t.Fatalf("game = %#v, want romSetName equal to the entry ZIP stem", game)
	}

	game.ROMSetName = "Model3ROMs"
	if _, err := st.UpsertGame(game); err != nil {
		t.Fatal(err)
	}
	job, err = New(st).ScanLibraryPath(lib, filepath.Join(root, "Model3ROMs"))
	if err != nil {
		t.Fatal(err)
	}
	if job.IndexedFiles != 1 || job.SkippedFiles != 0 || job.ErrorCount != 0 {
		t.Fatalf("migration job = %#v, want the unchanged legacy record reindexed", job)
	}
	page, err = st.ListGamesPage(domain.GameListOptions{Platform: "model3", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ROMSetName != "spikeout_final_edition" {
		t.Fatalf("migrated page = %#v, want entry ZIP stem restored", page)
	}
}

func TestScanLibraryIndexesNaomi2CatalogAndHidesDependencies(t *testing.T) {
	root := t.TempDir()
	naomi2Dir := filepath.Join(root, "NAOMI 2")
	vf4Dir := filepath.Join(naomi2Dir, "vf4")
	makeZip(t, filepath.Join(naomi2Dir, "naomi2.zip"), map[string]string{"epr-23605.ic27": "bios"})
	makeZip(t, filepath.Join(vf4Dir, "vf4.zip"), map[string]string{"317-0314-com.pic": "vf4-pic"})
	if err := os.MkdirAll(filepath.Join(vf4Dir, "vf4"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vf4Dir, "vf4", "gds-0012c.chd"), []byte("gd-rom"), 0o644); err != nil {
		t.Fatal(err)
	}
	makeZip(t, filepath.Join(vf4Dir, "vf4_folder.zip"), map[string]string{"wrapper.txt": "ignore"})
	// Some dumps retain parent-set ZIPs next to the child game directory. A
	// known child short name is still not launchable unless its own directory
	// (or the NAOMI 2 root) owns the descriptor.
	makeZip(t, filepath.Join(naomi2Dir, "clubkcyco", "clubkcyc.zip"), map[string]string{"wrapper.txt": "ignore"})

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("GameROMS", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.IndexedFiles != 2 || job.ErrorCount != 0 {
		t.Fatalf("job = %#v, want descriptor and BIOS indexed without wrapper or CHD tasks", job)
	}

	page, err := st.ListGamesPage(domain.GameListOptions{Platform: "naomi2", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("page = %#v, want one visible NAOMI 2 game", page)
	}
	game := page.Items[0]
	if game.Title != "Virtua Fighter 4 (Ver. C)" || game.Platform != "naomi2" || game.ROMSetName != "vf4" || game.EmulatorHint != "flycast" || game.Format != "zip" || game.Compatibility != "playable" || game.CatalogRole != "game" {
		t.Fatalf("game = %#v, want canonical NAOMI 2 VF4 metadata", game)
	}
	files, err := st.GameFiles(game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Name != "vf4.zip" || files[0].Role != "entry" || files[1].Name != "vf4/gds-0012c.chd" || files[1].Role != "dependency" {
		t.Fatalf("files = %#v, want ZIP entry and GD-ROM dependency", files)
	}

	for _, query := range []string{"gds-0012c", "naomi2.zip"} {
		search, err := st.ListGamesPage(domain.GameListOptions{Query: query, Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if search.Total != 1 || len(search.Items) != 1 || search.Items[0].CatalogRole != "dependency" {
			t.Fatalf("search %q = %#v, want searchable hidden dependency", query, search)
		}
	}
	if _, err := st.GameByPath(filepath.Join(vf4Dir, "vf4_folder.zip")); err == nil {
		t.Fatal("wrapper ZIP was indexed as a game")
	}
	if _, err := st.GameByPath(filepath.Join(naomi2Dir, "clubkcyco", "clubkcyc.zip")); err == nil {
		t.Fatal("parent-set ZIP was indexed as a game")
	}
	if _, err := st.GameByPath(filepath.Join(vf4Dir, "vf4", "gds-0012c.chd")); err != nil {
		t.Fatalf("GD-ROM dependency = %v, want indexed hidden dependency", err)
	}
	facets, err := st.ListGameFacets(domain.GameListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if facets.Total != 1 || len(facets.Platforms) != 1 || facets.Platforms[0].Platform != "naomi2" || facets.Platforms[0].Count != 1 {
		t.Fatalf("facets = %#v, want one visible NAOMI 2 game", facets)
	}

	secondJob, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if secondJob.IndexedFiles != 0 || secondJob.SkippedFiles != 2 || secondJob.ErrorCount != 0 {
		t.Fatalf("second job = %#v, want unchanged descriptor and BIOS skipped", secondJob)
	}
}

func TestScanLibraryIndexesNaomi2SplitCartridgeWithParentROMSet(t *testing.T) {
	root := t.TempDir()
	makeZip(t, filepath.Join(root, "clubkrt.zip"), map[string]string{"parent.ic1": "parent-rom"})
	makeZip(t, filepath.Join(root, "clubkrto.zip"), map[string]string{"clone.ic1": "clone-rom"})
	parentInfo, err := os.Stat(filepath.Join(root, "clubkrt.zip"))
	if err != nil {
		t.Fatal(err)
	}
	cloneInfo, err := os.Stat(filepath.Join(root, "clubkrto.zip"))
	if err != nil {
		t.Fatal(err)
	}

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("NAOMI 2", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.IndexedFiles != 2 || job.ErrorCount != 0 {
		t.Fatalf("job = %#v, want parent and clone indexed", job)
	}

	clone, err := st.GameByPath(filepath.Join(root, "clubkrto.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if clone.Size != parentInfo.Size()+cloneInfo.Size() || clone.ROMSetName != "clubkrto" {
		t.Fatalf("clone = %#v, want aggregate split-set size and clone ROM-set identity", clone)
	}
	files, err := st.GameFiles(clone.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Name != "clubkrto.zip" || files[0].Role != "entry" || files[1].Name != "clubkrt.zip" || files[1].Role != "dependency" || files[1].SHA1 == "" {
		t.Fatalf("files = %#v, want clone entry plus checksummed parent dependency", files)
	}
	parent, err := st.GameByPath(filepath.Join(root, "clubkrt.zip"))
	if err != nil {
		t.Fatalf("parent game was removed: %v", err)
	}
	if parent.CatalogRole != "game" || parent.ROMSetName != "clubkrt" {
		t.Fatalf("parent = %#v, want independently visible parent game", parent)
	}
}

func TestNaomi2SplitCatalogBuildsAllParentClosures(t *testing.T) {
	tests := map[string]string{
		"clubkrto":  "clubkrt",
		"clubkrta":  "clubkrt",
		"clubkrtc":  "clubkrt",
		"kingrt66p": "kingrt66",
		"vstrik3co": "vstrik3c",
	}
	for clone, parent := range tests {
		t.Run(clone, func(t *testing.T) {
			root := t.TempDir()
			clonePath := filepath.Join(root, clone+".zip")
			makeZip(t, clonePath, map[string]string{"clone.ic1": "clone-rom"})
			makeZip(t, filepath.Join(root, parent+".zip"), map[string]string{"parent.ic1": "parent-rom"})
			cloneInfo, err := os.Stat(clonePath)
			if err != nil {
				t.Fatal(err)
			}
			entry, ok := naomi2catalog.Lookup(clone)
			if !ok {
				t.Fatalf("catalog entry %q not found", clone)
			}
			files, totalSize, parentPath, err := naomi2IndexedGameFiles(clonePath, cloneInfo, clone, entry)
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != 2 || files[0].Name != clone+".zip" || files[0].Role != "entry" ||
				files[1].Name != parent+".zip" || files[1].Role != "dependency" ||
				parentPath != filepath.Join(root, parent+".zip") || totalSize != files[0].Size+files[1].Size {
				t.Fatalf("files=%#v total=%d parentPath=%q", files, totalSize, parentPath)
			}
		})
	}
}

func TestNaomi2SplitCatalogResolvesParentFromProductionLayouts(t *testing.T) {
	for _, test := range []struct {
		name       string
		parentPath func(string) string
	}{
		{
			name: "parent copy beside clone",
			parentPath: func(root string) string {
				return filepath.Join(root, "clubkrto", "clubkrt.zip")
			},
		},
		{
			name: "sibling parent game directory",
			parentPath: func(root string) string {
				return filepath.Join(root, "clubkrt", "clubkrt.zip")
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			clonePath := filepath.Join(root, "clubkrto", "clubkrto.zip")
			parentPath := test.parentPath(root)
			makeZip(t, clonePath, map[string]string{"clone.ic1": "clone-rom"})
			makeZip(t, parentPath, map[string]string{"parent.ic1": "parent-rom"})
			cloneInfo, err := os.Stat(clonePath)
			if err != nil {
				t.Fatal(err)
			}
			entry, ok := naomi2catalog.Lookup("clubkrto")
			if !ok {
				t.Fatal("clubkrto catalog entry not found")
			}
			files, totalSize, resolvedParent, err := naomi2IndexedGameFiles(clonePath, cloneInfo, "clubkrto", entry)
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != 2 || files[0].Name != "clubkrto.zip" || files[1].Name != "clubkrt.zip" ||
				files[1].Role != "dependency" || resolvedParent != parentPath || totalSize != files[0].Size+files[1].Size {
				t.Fatalf("files=%#v total=%d parent=%q, want clone plus exact parent %q", files, totalSize, resolvedParent, parentPath)
			}
		})
	}
}

func TestScanLibraryRejectsNaomi2SplitCartridgeWithoutParentROMSet(t *testing.T) {
	root := t.TempDir()
	makeZip(t, filepath.Join(root, "clubkrto.zip"), map[string]string{"clone.ic1": "clone-rom"})

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("NAOMI 2", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.IndexedFiles != 0 || job.ErrorCount != 1 {
		t.Fatalf("job = %#v, want missing parent to block clone indexing", job)
	}
	if _, err := st.GameByPath(filepath.Join(root, "clubkrto.zip")); err == nil {
		t.Fatal("split clone without parent was indexed")
	}
	errors, err := st.ListFileErrorsByJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(errors) != 1 || !strings.Contains(errors[0].Message, "clubkrt.zip") {
		t.Fatalf("errors = %#v, want exact missing parent name", errors)
	}
}

func TestSaturnCUERejectsDependencyPathEscape(t *testing.T) {
	root := t.TempDir()
	gameDir := filepath.Join(root, "Saturn")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(gameDir, "unsafe.cue")
	if err := os.WriteFile(path, []byte(`FILE "../outside.bin" BINARY`), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := indexedGameFiles(path, info, ".cue"); err == nil || !strings.Contains(err.Error(), "escapes game directory") {
		t.Fatalf("indexedGameFiles escape error = %v, want rejected path", err)
	}
}

func TestScanLibraryIndexesPCFXCueSetsAndMergesDiscDirectories(t *testing.T) {
	t.Setenv("FOLIOSPACE_SCAN_WORKERS", "2")
	root := t.TempDir()
	collection := filepath.Join(root, "PC-FX官方日版游戏全集（61个）")
	writeDisc := func(dirName string, cueName string, cueBIN string, diskBIN string) {
		dir := filepath.Join(collection, dirName)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, cueName), []byte(`FILE "`+cueBIN+`" BINARY
  TRACK 01 MODE1/2352
`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, diskBIN), []byte("disc-data-"+dirName), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeDisc("19960913 史莱姆汽泡姊妹(ACT)", "Chip Chan Kick.cue", "chip chan kick.BIN", "Chip Chan Kick.bin")
	writeDisc("19970314 王子最终传承(RPG) CD1", "Last Imperial Prince - Disc A.cue", "Last Imperial Prince - Disc A.bin", "Last Imperial Prince - Disc A.bin")
	writeDisc("19970314 王子最终传承(RPG) CD2", "Last Imperial Prince - Disc B.cue", "Last Imperial Prince - Disc B.bin", "Last Imperial Prince - Disc B.bin")
	writeDisc("游戏镜像", "duplicate.cue", "duplicate.bin", "duplicate.bin")
	writeDisc("出版物附属盘、非卖品", "sample.cue", "sample.bin", "sample.bin")
	if err := os.WriteFile(filepath.Join(collection, "pcfx.rom"), []byte("bios"), 0o644); err != nil {
		t.Fatal(err)
	}

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("Games", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.IndexedFiles != 2 || job.ErrorCount != 0 {
		t.Fatalf("job = %#v, want two PC-FX games and no excluded entries", job)
	}
	page, err := st.ListGamesPage(domain.GameListOptions{Platform: "pc-fx", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("games = %#v, want two launchable PC-FX games", page)
	}
	var multi domain.GameAsset
	for _, game := range page.Items {
		if game.Platform != "pc-fx" || game.ROMSetName != "PC-FX" || game.EmulatorHint != "pcfx" {
			t.Fatalf("game = %#v, want canonical PC-FX metadata", game)
		}
		if game.Title == "王子最终传承" {
			multi = game
		}
	}
	if multi.ID == 0 || multi.Format != "m3u" {
		t.Fatalf("multi-disc game = %#v, want one virtual M3U", multi)
	}
	files, err := st.GameFiles(multi.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 5 || files[0].Role != "entry" || filepath.Ext(files[0].Name) != ".m3u" || files[1].Name != "Last Imperial Prince - Disc A.cue" || files[3].Name != "Last Imperial Prince - Disc B.cue" {
		t.Fatalf("files = %#v, want M3U, two CUEs, and two BINs", files)
	}
	if multi.Size != files[0].Size+files[1].Size+files[2].Size+files[3].Size+files[4].Size {
		t.Fatalf("multi size = %d, want complete package size", multi.Size)
	}
	facets, err := st.ListGameFacets(domain.GameListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(facets.Platforms) != 1 || facets.Platforms[0].Platform != "pc-fx" || facets.Platforms[0].Count != 2 || facets.Platforms[0].ROMSetName != "PC-FX" || facets.Platforms[0].EmulatorHint != "pcfx" {
		t.Fatalf("facets = %#v, want one canonical PC-FX facet", facets)
	}

	secondJob, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if secondJob.IndexedFiles != 0 || secondJob.SkippedFiles != 2 || secondJob.ErrorCount != 0 {
		t.Fatalf("second job = %#v, want complete PC-FX sets skipped", secondJob)
	}
}

func TestScanLibraryReadsBOMPegasusMetadataForPCFX(t *testing.T) {
	root := t.TempDir()
	cuePath := filepath.Join(root, "Chip Chan Kick.cue")
	if err := os.WriteFile(cuePath, []byte(`FILE "chip chan kick.BIN" BINARY`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Chip Chan Kick.bin"), []byte("disc"), 0o644); err != nil {
		t.Fatal(err)
	}
	metadata := "\ufeffcollection: PC-FX\r\nignore-file: ignored.cue\r\n\r\ngame: 史莱姆汽泡姊妹\r\nfile: Chip Chan Kick.cue\r\ndescription: PC-FX description\r\ndeveloper: NEC\r\n"
	if err := os.WriteFile(filepath.Join(root, "metadata.pegasus.txt"), []byte(metadata), 0o644); err != nil {
		t.Fatal(err)
	}

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("PC-FX", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(st).ScanLibrary(lib); err != nil {
		t.Fatal(err)
	}
	page, err := st.ListGamesPage(domain.GameListOptions{Platform: "pc-fx", Limit: 10})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("page = %#v err=%v", page, err)
	}
	if page.Items[0].Title != "史莱姆汽泡姊妹" {
		t.Fatalf("title = %q, want Pegasus title", page.Items[0].Title)
	}
	details, err := st.GameDetails(page.Items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if details.Metadata.Summary != "PC-FX description" || len(details.Metadata.Developers) != 1 || details.Metadata.Developers[0] != "NEC" {
		t.Fatalf("metadata = %#v, want Pegasus description and developer", details.Metadata)
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

func TestScanLibraryIndexesPDFAsBook(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Manuals", "guide.pdf")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("%PDF-1.4\n% foliospace test\n"), 0o644); err != nil {
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
	if job.DiscoveredFiles != 1 || job.IndexedFiles != 1 || job.ErrorCount != 0 {
		t.Fatalf("job = %#v, want one pdf indexed", job)
	}

	series, err := st.ListSeries()
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 || series[0].Title != "Manuals" || series[0].PrimaryType != "book" {
		t.Fatalf("series = %#v, want pdf book collection", series)
	}
	books, err := st.ListBooks(series[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 1 || books[0].Title != "guide" || books[0].Format != "pdf" || books[0].PageCount != 1 {
		t.Fatalf("books = %#v, want single-page pdf entry", books)
	}
}

func TestScanLibraryPrunesSkippedDirectoryIndexes(t *testing.T) {
	root := t.TempDir()
	activePath := filepath.Join(root, "Active", "keep.cbz")
	recyclePath := filepath.Join(root, "#recycle", "old.cbz")
	if err := os.MkdirAll(filepath.Dir(activePath), 0o755); err != nil {
		t.Fatal(err)
	}
	makeZip(t, activePath, map[string]string{"001.jpg": "keep"})

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
	activeSeries, err := st.UpsertSeries(lib.ID, "Active", "Active")
	if err != nil {
		t.Fatal(err)
	}
	oldSeries, err := st.UpsertSeries(lib.ID, "#recycle", "#recycle")
	if err != nil {
		t.Fatal(err)
	}
	oldBook, err := st.UpsertBook(oldSeries.ID, "old", "cbz")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertFile(oldBook.ID, lib.ID, recyclePath, "#recycle/old.cbz", 10, time.Unix(10, 0), ".cbz"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertBook(activeSeries.ID, "placeholder", "cbz"); err != nil {
		t.Fatal(err)
	}

	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "completed" || job.ErrorCount != 0 {
		t.Fatalf("job = %#v, want clean completed scan", job)
	}
	series, err := st.ListSeries()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range series {
		if strings.Contains(item.DirectoryPath, "#recycle") || strings.Contains(item.Title, "#recycle") {
			t.Fatalf("series = %#v, want recycle collection pruned", series)
		}
	}
	books, err := st.ListBooks(activeSeries.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, book := range books {
		if book.Title == "old" {
			t.Fatalf("books = %#v, want recycle book pruned", books)
		}
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

	game, err := st.GameByPath(filepath.Join(root, "Arcade", "romset.7z"))
	if err != nil {
		t.Fatal(err)
	}
	if game.Title != "romset" || game.Format != "7z" || game.Platform != "arcade" || game.ROMSetName != "Arcade" || game.CatalogRole != "needs-curation" {
		t.Fatalf("game = %#v, want indexed but quarantined Arcade 7z ROM set", game)
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
		{relPath: "SFC/chrono-trigger.zip", want: "snes"},
		{relPath: "VirtualBoy/3-D Tetris (USA).zip", want: "virtualboy"},
		{relPath: "VirtualBoy/Red Alarm (USA).vb", want: "virtualboy"},
		{relPath: "FBNeo/arcade/mslug.zip", want: "neogeo"},
		{relPath: "FBNeo/arcade/shinobi3.zip", want: "md"},
		{relPath: "FBNeo/arcade/wof.zip", want: "arcade"},
		{relPath: "CPS1 CPS2 CPS3/sf2.zip", want: "mame"},
		{relPath: "CPS1 CPS2 CPS3/sfa.zip", want: "mame"},
		{relPath: "CPS1 CPS2 CPS3/sfiii.zip", want: "mame"},
		{relPath: "CPS1 CPS2 CPS3/sf2hack.zip", want: "mame"},
		{relPath: "FBNeo/arcade/hypreact.zip", want: "mame"},
		{relPath: "FBNeo/arcade/hypreac2.zip", want: "mame"},
		{relPath: "FBNeo/arcade/fromanc4.zip", want: "mame"},
		{relPath: "FBNeo/arcade/mcnpshnt.zip", want: "mame"},
		{relPath: "Mahjong/hypreact.zip", want: "mame"},
		{relPath: "Model2/vf2.zip", want: "model2"},
		{relPath: "NAOMI 2/vf4/vf4.zip", want: "naomi2"},
		{relPath: "Model2ROMs/daytona.zip", want: "model2"},
		{relPath: "Model3ROMs/spikeout.zip", want: "model3"},
		{relPath: "SEGA 32X/doom32x.zip", want: "32x"},
		{relPath: "PS/Alundra.pbp", want: "ps1"},
		{relPath: "PS/xenogears.PBP", want: "ps1"},
		{relPath: "PS/01-动作游戏/人猿泰山.img", want: "ps1"},
		{relPath: "Dreamcast/Crazy Taxi.chd", want: "dreamcast"},
		{relPath: "3DS/Mario Kart 7.3ds", want: "3ds"},
		{relPath: "Nintendo-3DS/Animal Crossing.cxi", want: "3ds"},
		{relPath: "CTR/Install.cia", want: "3ds"},
		{relPath: "Crazy Taxi.cdi", want: "dreamcast"},
		{relPath: "Crazy Taxi.gdi", want: "dreamcast"},
		{relPath: "Saturn/Guardian Heroes.cue", want: "saturn"},
		{relPath: "Saturn/Guardian Heroes.iso", want: "saturn"},
		{relPath: "PS/Ridge Racer.cue", want: "ps1"},
	}
	for _, test := range tests {
		if got := inferGamePlatform(filepath.Ext(test.relPath), test.relPath); got != test.want {
			t.Fatalf("inferGamePlatform(%q) = %q, want %q", test.relPath, got, test.want)
		}
	}
	dcLibrary := domain.Library{Name: "DC", RootPath: "/games/DC"}
	if got := inferLibraryGamePlatform(dcLibrary, ".chd", "Crazy Taxi.chd"); got != "dreamcast" {
		t.Fatalf("inferLibraryGamePlatform(DC root CHD) = %q, want dreamcast", got)
	}
	saturnLibrary := domain.Library{Name: "SS", RootPath: "/games/SS"}
	if got := inferLibraryGamePlatform(saturnLibrary, ".cue", "Guardian Heroes.cue"); got != "saturn" {
		t.Fatalf("inferLibraryGamePlatform(SS root CUE) = %q, want saturn", got)
	}
	if got := inferLibraryGamePlatform(saturnLibrary, ".iso", "Guardian Heroes.iso"); got != "saturn" {
		t.Fatalf("inferLibraryGamePlatform(SS root ISO) = %q, want saturn", got)
	}
	model2Library := domain.Library{Name: "Model2", RootPath: "/games/Model2"}
	if got := inferLibraryGamePlatform(model2Library, ".zip", "vf2.zip"); got != "model2" {
		t.Fatalf("inferLibraryGamePlatform(Model2 root ZIP) = %q, want model2", got)
	}
	naomi2Library := domain.Library{Name: "NAOMI 2", RootPath: "/games/NAOMI 2"}
	if got := inferLibraryGamePlatform(naomi2Library, ".zip", "vf4/vf4.zip"); got != "naomi2" {
		t.Fatalf("inferLibraryGamePlatform(NAOMI 2 ZIP) = %q, want naomi2", got)
	}
	sharedGameLibrary := domain.Library{Name: "GameROMS", RootPath: "/games"}
	if got := inferLibraryGamePlatform(sharedGameLibrary, ".zip", "NAOMI 2/vf4/vf4.zip"); got != "naomi2" {
		t.Fatalf("inferLibraryGamePlatform(GameROMS NAOMI 2 ZIP) = %q, want naomi2", got)
	}
	threeDSLibrary := domain.Library{Name: "3DS", RootPath: "/games/3DS"}
	if got := inferLibraryGamePlatform(threeDSLibrary, ".zip", "Mario Kart 7.zip"); got != "3ds" {
		t.Fatalf("inferLibraryGamePlatform(3DS root ZIP) = %q, want 3ds", got)
	}
	virtualBoyLibrary := domain.Library{Name: "VirtualBoy", RootPath: "/games/VirtualBoy"}
	if got := inferLibraryGamePlatform(virtualBoyLibrary, ".zip", "3-D Tetris (USA).zip"); got != "virtualboy" {
		t.Fatalf("inferLibraryGamePlatform(VirtualBoy ZIP) = %q, want virtualboy", got)
	}
	arcadeLibrary := domain.Library{Name: "Arcade", RootPath: "/games/Arcade"}
	if got := inferLibraryGamePlatform(arcadeLibrary, ".chd", "kinst.chd"); got != "disc" {
		t.Fatalf("inferLibraryGamePlatform(Arcade root CHD) = %q, want disc", got)
	}
}

func TestScanLibraryUsesZIPContentBeforeArcadeShortName(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "FBNeo", "snes", "sf2.zip")
	romName := "Street Fighter II (U)(1992)(Capcom).sfc"
	romBody := "snes-rom-body"
	makeZip(t, path, map[string]string{
		romName:              romBody,
		"__MACOSX/._sf2.sfc": "ignored",
		"README.txt":         "metadata",
	})

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("GameROMS", root, "game")
	if err != nil {
		t.Fatal(err)
	}

	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.IndexedFiles != 1 || job.ErrorCount != 0 {
		t.Fatalf("job = %#v, want one SNES archive indexed", job)
	}
	game, err := st.GameByPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if game.Platform != "snes" || game.Format != "sfc" || game.EmulatorHint != "snes9x" || game.ROMSetName != "SNES" || game.CatalogRole != "game" || game.Size != int64(len(romBody)) {
		t.Fatalf("game = %#v, want content-classified SNES ROM", game)
	}
	if game.Title != "Street Fighter II" {
		t.Fatalf("title = %q, want normalized inner ROM title", game.Title)
	}
	files, err := st.GameFiles(game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name != romName || files[0].Size != int64(len(romBody)) || files[0].SHA1 == "" {
		t.Fatalf("files = %#v, want one checksummed inner ROM", files)
	}

	stale := game
	stale.Platform = "cps1"
	stale.Format = "zip"
	stale.ROMSetName = "sf2"
	stale.EmulatorHint = "mame"
	stale.CatalogRole = "needs-curation"
	if _, err := st.UpsertGame(stale); err != nil {
		t.Fatal(err)
	}
	secondJob, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if secondJob.IndexedFiles != 1 || secondJob.SkippedFiles != 0 || secondJob.ErrorCount != 0 {
		t.Fatalf("second job = %#v, want unchanged stale record reclassified", secondJob)
	}
	repaired, err := st.GameByPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if repaired.ID != game.ID || repaired.Platform != "snes" || repaired.Format != "sfc" || repaired.EmulatorHint != "snes9x" {
		t.Fatalf("repaired = %#v, want same ID with SNES metadata", repaired)
	}
}

func TestScanLibraryIndexesVirtualBoyArchive(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "VirtualBoy", "3-D Tetris (USA).zip")
	romName := "3-D Tetris (USA).vb"
	romBody := "virtual-boy-rom"
	makeZip(t, path, map[string]string{romName: romBody})

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("GameROMS", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.IndexedFiles != 1 || job.ErrorCount != 0 {
		t.Fatalf("job = %#v, want one Virtual Boy archive indexed", job)
	}
	game, err := st.GameByPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if game.Title != "3-D Tetris" || game.Platform != "virtualboy" || game.Format != "vb" || game.ROMSetName != "Virtual Boy" || game.EmulatorHint != "virtualfriend" || game.CatalogRole != "game" {
		t.Fatalf("game = %#v, want canonical Virtual Boy metadata", game)
	}
	files, err := st.GameFiles(game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name != romName || files[0].Role != "entry" || files[0].SHA1 == "" {
		t.Fatalf("files = %#v, want one checksummed Virtual Boy entry", files)
	}
	facets, err := st.ListGameFacets(domain.GameListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if facets.Total != 1 || len(facets.Platforms) != 1 || facets.Platforms[0].Platform != "virtualboy" || facets.Platforms[0].Title != "Virtual Boy" || facets.Platforms[0].Count != 1 {
		t.Fatalf("facets = %#v, want one Virtual Boy title", facets)
	}
}

func TestInspectConsoleROMZIPRecognizesCartridgeFormats(t *testing.T) {
	tests := []struct {
		name       string
		entry      string
		body       []byte
		platform   string
		format     string
		emulator   string
		romSetName string
	}{
		{name: "SNES", entry: "game.smc", body: []byte("rom"), platform: "snes", format: "smc", emulator: "snes9x", romSetName: "SNES"},
		{name: "Virtual Boy", entry: "game.vb", body: []byte("rom"), platform: "virtualboy", format: "vb", emulator: "virtualfriend", romSetName: "Virtual Boy"},
		{name: "NES", entry: "game.nes", body: []byte("rom"), platform: "nes", format: "nes", emulator: "nestopia", romSetName: "NES"},
		{name: "Game Boy", entry: "game.gb", body: []byte("rom"), platform: "gb", format: "gb", emulator: "gambatte", romSetName: "GB"},
		{name: "Game Boy Color", entry: "game.gbc", body: []byte("rom"), platform: "gbc", format: "gbc", emulator: "gambatte", romSetName: "GBC"},
		{name: "Game Boy Advance", entry: "game.gba", body: []byte("rom"), platform: "gba", format: "gba", emulator: "mgba", romSetName: "GBA"},
		{name: "Nintendo DS", entry: "game.nds", body: []byte("rom"), platform: "nds", format: "nds", emulator: "melonds-ds", romSetName: "Nintendo DS"},
		{name: "Mega Drive MD", entry: "game.md", body: []byte("rom"), platform: "md", format: "md", emulator: "genesisplusgx", romSetName: "Mega Drive"},
		{name: "Mega Drive GEN", entry: "game.gen", body: []byte("rom"), platform: "md", format: "gen", emulator: "genesisplusgx", romSetName: "Mega Drive"},
	}
	megaDriveBIN := make([]byte, 0x200)
	copy(megaDriveBIN[0x100:], []byte("SEGA"))
	tests = append(tests, struct {
		name       string
		entry      string
		body       []byte
		platform   string
		format     string
		emulator   string
		romSetName string
	}{name: "Mega Drive BIN", entry: "game.bin", body: megaDriveBIN, platform: "md", format: "bin", emulator: "genesisplusgx", romSetName: "Mega Drive"})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "rom.zip")
			makeZip(t, path, map[string]string{test.entry: string(test.body)})
			rom, detected, err := inspectConsoleROMZIP(path, true)
			if err != nil {
				t.Fatal(err)
			}
			romSetName, emulator := consolePlatformMetadata(rom.platform)
			if !detected || !rom.definitive || rom.platform != test.platform || rom.format != test.format || emulator != test.emulator || romSetName != test.romSetName || rom.checksums.sha1 == "" {
				t.Fatalf("rom = %#v detected=%v metadata=%q/%q", rom, detected, romSetName, emulator)
			}
		})
	}
}

func TestScanLibraryCanonicalizesCPSAndMAMECatalog(t *testing.T) {
	root := t.TempDir()
	cpsDir := filepath.Join(root, "CPS1 CPS2 CPS3")
	mahjongDir := filepath.Join(root, "Mahjong")
	paths := map[string]string{
		"sf2":        filepath.Join(cpsDir, "sf2.zip"),
		"sfa":        filepath.Join(cpsDir, "sfa.zip"),
		"sfiii":      filepath.Join(cpsDir, "sfiii.zip"),
		"hypreact":   filepath.Join(mahjongDir, "hypreact.zip"),
		"dependency": filepath.Join(mahjongDir, "ym2413_instruments.zip"),
	}
	for name, path := range paths {
		makeZip(t, path, map[string]string{name + ".bin": name})
	}

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("GameROMS", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	job, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if job.IndexedFiles != 5 || job.ErrorCount != 0 {
		t.Fatalf("job = %#v, want five catalog assets indexed", job)
	}

	wants := map[string]struct {
		platform string
		romSet   string
		emulator string
		role     string
	}{
		"sf2":        {platform: "mame", romSet: "sf2", emulator: "mame", role: "needs-curation"},
		"sfa":        {platform: "mame", romSet: "sfa", emulator: "mame", role: "needs-curation"},
		"sfiii":      {platform: "mame", romSet: "sfiii", emulator: "mame", role: "needs-curation"},
		"hypreact":   {platform: "mame", romSet: "hypreact", emulator: "mame", role: "needs-curation"},
		"dependency": {platform: "mame", romSet: "ym2413_instruments", emulator: "mame", role: "dependency"},
	}
	games := map[string]domain.GameAsset{}
	for name, want := range wants {
		game, err := st.GameByPath(paths[name])
		if err != nil {
			t.Fatal(err)
		}
		games[name] = game
		if game.Platform != want.platform || game.ROMSetName != want.romSet || game.EmulatorHint != want.emulator || game.CatalogRole != want.role {
			t.Fatalf("%s = %#v, want platform=%s romSet=%s emulator=%s role=%s", name, game, want.platform, want.romSet, want.emulator, want.role)
		}
	}

	facets, err := st.ListGameFacets(domain.GameListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int64{}
	for _, facet := range facets.Platforms {
		counts[facet.Platform] = facet.Count
	}
	if facets.Total != 4 || counts["mame"] != 4 {
		t.Fatalf("facets = %#v, want four unaudited MAME candidates", facets)
	}

	legacyCPS := games["sf2"]
	legacyCPS.Platform = "arcade"
	legacyCPS.ROMSetName = "FBNeo"
	legacyCPS.EmulatorHint = "arcade"
	if _, err := st.UpsertGame(legacyCPS); err != nil {
		t.Fatal(err)
	}
	legacyMAME := games["hypreact"]
	legacyMAME.ROMSetName = "MAME"
	if _, err := st.UpsertGame(legacyMAME); err != nil {
		t.Fatal(err)
	}
	legacyDependency := games["dependency"]
	legacyDependency.CatalogRole = "game"
	if _, err := st.UpsertGame(legacyDependency); err != nil {
		t.Fatal(err)
	}

	secondJob, err := New(st).ScanLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if secondJob.IndexedFiles != 3 || secondJob.SkippedFiles != 2 || secondJob.ErrorCount != 0 {
		t.Fatalf("second job = %#v, want three stale catalog records repaired and two unchanged files skipped", secondJob)
	}
	for name, want := range wants {
		game, err := st.GameByPath(paths[name])
		if err != nil {
			t.Fatal(err)
		}
		if game.Platform != want.platform || game.ROMSetName != want.romSet || game.EmulatorHint != want.emulator || game.CatalogRole != want.role {
			t.Fatalf("repaired %s = %#v", name, game)
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

func makeRawNameZip(t *testing.T, path string, rawName string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	header := &zip.FileHeader{Name: rawName, Method: zip.Deflate, NonUTF8: true}
	entry, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(body); err != nil {
		t.Fatal(err)
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

func mustStartScanJob(t *testing.T, st *store.Store, libraryID int64) domain.ScanJob {
	t.Helper()
	job, err := st.StartScanJob(libraryID)
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func sampleEPUBEntriesWithTitle(title string) map[string]string {
	return sampleEPUBEntriesWithMetadata(title, "", "")
}

func samplePDFWithInfo(title string, author string, subject string) []byte {
	return []byte(`%PDF-1.4
1 0 obj
<< /Type /Catalog /Pages 2 0 R >>
endobj
2 0 obj
<< /Type /Pages /Count 0 >>
endobj
3 0 obj
<< /Title (` + title + `) /Author (` + author + `) /Subject (` + subject + `) >>
endobj
trailer
<< /Root 1 0 R /Info 3 0 R >>
%%EOF
`)
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
