package scanner

import (
	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/store"
	"os"
	"path/filepath"
	"testing"
)

func TestScanBookDamageAndRecoveryRetainsExistingState(t *testing.T) {
	for _, workers := range []int{1, 4} {
		t.Run(string(rune('0'+workers)), func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "Book", "sample.epub")
			makeZip(t, path, sampleEPUBEntriesWithMetadata("Title", "Author", "Description"))
			c, e := db.Open(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			st := store.New(c)
			lib, _ := st.CreateLibrary("Books", root)
			scan := NewWithWorkerCount(st, func() int { return workers })
			if j, e := scan.ScanLibrary(lib); e != nil || j.ErrorCount != 0 {
				t.Fatalf("initial %+v %v", j, e)
			}
			index, _ := st.FileIndexByPath(path)
			id := index.Book.ID
			st.SaveProgressDetail(id, 3, "keep", .4)
			st.UpdateBookPrivateState(id, domain.BookPrivateState{Favorite: true, Summary: "manual note"})
			os.WriteFile(path, nil, 0600)
			if j, e := scan.ScanLibraryPath(lib, path); e != nil || j.ErrorCount != 1 {
				t.Fatalf("empty %+v %v", j, e)
			}
			b, _ := st.BookByID(id)
			if b.FileErrorCode != "empty_file" || !b.Favorite || b.Summary != "manual note" || b.ProgressLocator != "keep" {
				t.Fatalf("lost state %+v", b)
			}
			recent, _ := st.ListRecentBooks(10)
			if len(recent) != 0 {
				t.Fatal("empty book on homepage")
			}
			makeZip(t, path, sampleEPUBEntriesWithMetadata("Title", "Author", "Description"))
			if j, e := scan.ScanLibraryPath(lib, path); e != nil || j.ErrorCount != 0 {
				t.Fatalf("recovery %+v %v", j, e)
			}
			b, _ = st.BookByID(id)
			if b.FileErrorCode != "" || !b.Favorite || b.Summary != "manual note" || b.ProgressLocator != "keep" {
				t.Fatalf("recovery lost state %+v", b)
			}
			history, e := st.ListFileErrors()
			if e != nil || len(history) != 1 || !history[0].Resolved || history[0].Path != path {
				t.Fatalf("history %+v %v", history, e)
			}
		})
	}
}
