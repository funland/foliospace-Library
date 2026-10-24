package service

import (
	"fmt"
	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/store"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestBookValidationHidesRecentRetainsIdentityPrivateAndErrorHistory(t *testing.T) {
	c, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	st := store.New(c)
	svc := New(st)
	root := t.TempDir()
	lib, _ := st.CreateLibrary("Books", root)
	series, _ := st.UpsertSeries(lib.ID, "Manual collection", ".")
	healthy, _ := st.UpsertBook(series.ID, "Older healthy", "pdf")
	var damaged int64
	for i := 0; i < 3; i++ {
		b, e := st.UpsertBook(series.ID, fmt.Sprint("Manual title ", i), "epub")
		if e != nil {
			t.Fatal(e)
		}
		p := filepath.Join(root, fmt.Sprintf("bad-%d.epub", i))
		os.WriteFile(p, make([]byte, 64), 0600)
		if _, e = st.UpsertFile(b.ID, lib.ID, p, filepath.Base(p), 64, time.Now(), ".epub"); e != nil {
			t.Fatal(e)
		}
		st.UpdateBookPrivateState(b.ID, domain.BookPrivateState{Favorite: true, Tags: []string{"manual"}, Summary: "Keep note"})
		st.SaveProgressDetail(b.ID, 7, "saved.xhtml", .35)
		st.ReplacePages(b.ID, []domain.Page{{Index: 0, Name: "preserve-old-page"}})
		before, _ := st.BookByID(b.ID)
		progress, _ := st.Progress(b.ID)
		pages, _ := st.ListPages(b.ID)
		after, e := svc.ValidateBookFile(b.ID)
		if e != nil || after.FileErrorCode != "zero_filled_file" {
			t.Fatalf("%+v %v", after, e)
		}
		if after.ID != before.ID || after.Title != before.Title || !after.Favorite || after.Summary != before.Summary || after.ProgressFraction != before.ProgressFraction {
			t.Fatal("existing user state changed")
		}
		p2, _ := st.Progress(b.ID)
		pages2, _ := st.ListPages(b.ID)
		if !reflect.DeepEqual(progress, p2) || !reflect.DeepEqual(pages, pages2) {
			t.Fatal("progress/pages changed")
		}
		damaged = b.ID
	}
	recent, e := st.ListRecentBooks(1)
	if e != nil || len(recent) != 1 || recent[0].ID != healthy.ID {
		t.Fatalf("filter must run before limit: %+v %v", recent, e)
	}
	history, e := st.ListFileErrors()
	if e != nil || len(history) != 3 || history[0].Resolved || history[0].BookID == 0 || history[0].Path == "" {
		t.Fatalf("missing linked errors: %+v %v", history, e)
	}
	b, _ := st.BookByID(damaged)
	originalPath := b.FilePath
	// A missing mount/file must not erase the known diagnostic or mutate progress.
	os.Rename(originalPath, originalPath+".save")
	if _, e = svc.ValidateBookFile(damaged); e == nil {
		t.Fatal("missing source accepted")
	}
	current, _ := st.BookByID(damaged)
	if current.FileErrorCode != b.FileErrorCode {
		t.Fatal("operational failure cleared damage")
	}
	os.Rename(originalPath+".save", originalPath)
	entries := map[string][]byte{"META-INF/container.xml": []byte(`<container><rootfiles><rootfile full-path="content.opf"/></rootfiles></container>`), "content.opf": []byte(`<package><metadata/><manifest><item id="c" href="chapter.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="c"/></spine></package>`), "chapter.xhtml": []byte(`<html><body>Restored</body></html>`)}
	if e = makeZipBytesAt(originalPath, entries); e != nil {
		t.Fatal(e)
	}
	recovered, e := svc.ValidateBookFile(damaged)
	if e != nil || recovered.FileErrorCode != "" || recovered.ID != damaged || recovered.Title != b.Title || recovered.Summary != b.Summary || !recovered.Favorite {
		t.Fatalf("recovery changed user fields %+v %v", recovered, e)
	}
	history, e = st.ListFileErrors()
	if e != nil || len(history) != 3 {
		t.Fatal("error history deleted")
	}
	found := false
	for _, h := range history {
		if h.BookID == damaged {
			found = h.Resolved
		}
	}
	if !found {
		t.Fatal("old error not marked resolved")
	}
	recent, e = st.ListRecentBooks(1)
	if e != nil || len(recent) != 1 || recent[0].ID != damaged {
		t.Fatal("repaired book not restored to recent")
	}
}

func TestValidationDoesNotClearUnsupportedFormatError(t *testing.T) {
	c, e := db.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	st := store.New(c)
	root := t.TempDir()
	lib, _ := st.CreateLibrary("Books", root)
	series, _ := st.UpsertSeries(lib.ID, "Books", ".")
	b, _ := st.UpsertBook(series.ID, "Unsupported", "7z")
	p := filepath.Join(root, "book.7z")
	os.WriteFile(p, []byte("7z fixture"), 0600)
	st.UpsertFile(b.ID, lib.ID, p, "book.7z", 10, time.Now(), ".7z")
	st.RecordFileError(domain.FileErrorInput{LibraryID: lib.ID, BookID: b.ID, Path: p, Code: domain.ErrorArchiveOpenFailed, Message: "unsupported archive"})
	_, e = New(st).ValidateBookFile(b.ID)
	if e != nil {
		t.Fatal(e)
	}
	history, e := st.ListFileErrors()
	if e != nil || len(history) != 1 || history[0].Resolved {
		t.Fatalf("unsupported format incorrectly recovered: %+v %v", history, e)
	}
}
