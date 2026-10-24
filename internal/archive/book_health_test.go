package archive

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestBookHealthDistinguishesEmptyZeroTruncatedAndOperationalErrors(t *testing.T) {
	for _, tc := range []struct {
		name, ext, code string
		data            []byte
	}{
		{"empty", ".epub", "empty_file", nil},
		{"empty-7z", ".7z", "empty_file", nil},
		{"zeros", ".epub", "zero_filled_file", make([]byte, 150000)},
		{"truncated", ".epub", "invalid_book_container", []byte("PK\x03\x04mimetypeapplication/epub+zip")},
		{"wrong-pdf", ".pdf", "invalid_book_container", []byte("not PDF")},
		{"pdf-header", ".pdf", "", []byte("%PDF-1.7\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), tc.name+tc.ext)
			if err := os.WriteFile(p, tc.data, 0600); err != nil {
				t.Fatal(err)
			}
			err := ValidateBookFile(p, tc.ext)
			if tc.code == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var issue *BookFileIssue
			if !errors.As(err, &issue) || issue.Code != tc.code {
				t.Fatalf("got %v want %s", err, tc.code)
			}
			after, _ := os.ReadFile(p)
			if string(after) != string(tc.data) {
				t.Fatal("source changed")
			}
		})
	}
	var issue *BookFileIssue
	if err := ValidateBookFile(filepath.Join(t.TempDir(), "missing.epub"), ".epub"); err == nil || errors.As(err, &issue) {
		t.Fatalf("missing file classified as structural damage: %v", err)
	}
}
func TestBookHealthChecksEPUBStructureButDoesNotDecodeComicImages(t *testing.T) {
	p := filepath.Join(t.TempDir(), "book.zip")
	f, _ := os.Create(p)
	z := zip.NewWriter(f)
	w, _ := z.Create("page.jpg")
	w.Write([]byte("image fixture"))
	z.Close()
	f.Close()
	if err := ValidateBookFile(p, ".cbz"); err != nil {
		t.Fatal(err)
	}
	var issue *BookFileIssue
	if err := ValidateBookFile(p, ".epub"); !errors.As(err, &issue) || issue.Code != "invalid_epub" {
		t.Fatalf("missing EPUB manifest: %v", err)
	}
}
