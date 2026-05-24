package scanner

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"foliospace-reader/internal/db"
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

	errors, err := st.ListFileErrors()
	if err != nil {
		t.Fatal(err)
	}
	if len(errors) != 1 || errors[0].Code != "empty_file" {
		t.Fatalf("errors = %#v, want one empty_file", errors)
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
