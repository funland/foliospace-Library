package scanner

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/store"
)

func TestScanLibraryIndexesValidArchivesAndRecordsEmptyFile(t *testing.T) {
	root := t.TempDir()
	makeZip(t, filepath.Join(root, "Series A", "book1.cbz"), map[string]string{"001.jpg": "image"})
	makeZip(t, filepath.Join(root, "root-book.zip"), map[string]string{"001.png": "image"})
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
	if job.IndexedFiles != 2 {
		t.Fatalf("indexed files = %d, want 2", job.IndexedFiles)
	}
	if job.ErrorCount != 1 {
		t.Fatalf("error count = %d, want 1", job.ErrorCount)
	}

	series, err := st.ListSeries()
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 2 {
		t.Fatalf("series len = %d, want 2", len(series))
	}
	titles := map[string]bool{}
	for _, item := range series {
		titles[item.Title] = true
	}
	if !titles["Series A"] {
		t.Fatalf("series titles = %#v, want Series A", titles)
	}
	rootSeries := filepath.Base(root)
	if !titles[rootSeries] {
		t.Fatalf("series titles = %#v, want root series %q", titles, rootSeries)
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
	if secondJob.SkippedFiles != 2 {
		t.Fatalf("second scan skipped files = %d, want 2", secondJob.SkippedFiles)
	}
	if secondJob.IndexedFiles != 0 {
		t.Fatalf("second scan indexed files = %d, want 0", secondJob.IndexedFiles)
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
	legacySeries, err := st.UpsertSeries(lib.ID, "Unsorted")
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
