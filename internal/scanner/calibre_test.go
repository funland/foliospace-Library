package scanner

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/store"
)

func TestCalibreUnchangedMetadataDoesNotWriteAndCoverEditDoes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Author", "Book", "book.epub")
	makeZip(t, path, sampleEPUBEntriesWithMetadata("Embedded", "Writer", "Description"))
	writeCalibreOPF(t, path, "Title", "Writer", "source")
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
	scanner := NewWithWorkerCount(st, func() int { return 4 })
	if job, err := scanner.ScanLibrary(lib); err != nil || job.ErrorCount != 0 {
		t.Fatalf("first=%#v err=%v", job, err)
	}
	index, err := st.FileIndexByPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateBookPrivateState(index.Book.ID, domain.BookPrivateState{Tags: []string{"private"}}); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{`CREATE TEMP TABLE metadata_writes(value INTEGER)`, `CREATE TEMP TRIGGER imported_book_update AFTER UPDATE ON books BEGIN INSERT INTO metadata_writes VALUES(1); END`, `CREATE TEMP TRIGGER imported_series_update AFTER UPDATE ON series BEGIN INSERT INTO metadata_writes VALUES(1); END`, `CREATE TEMP TRIGGER imported_file_update AFTER UPDATE ON files BEGIN INSERT INTO metadata_writes VALUES(1); END`, `CREATE TEMP TRIGGER imported_setting_update AFTER UPDATE ON app_settings BEGIN INSERT INTO metadata_writes VALUES(1); END`} {
		if _, err := conn.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if job, err := scanner.ScanLibrary(lib); err != nil || job.ErrorCount != 0 || job.MetadataUpdatedFiles != 0 {
		t.Fatalf("unchanged=%#v err=%v", job, err)
	}
	job, err := st.StartScanJobWithTarget(lib.ID, root)
	if err != nil {
		t.Fatal(err)
	}
	job, err = scanner.RunRecentScanJobPath(lib, job, root, 1)
	if err != nil || job.ErrorCount != 0 || job.MetadataUpdatedFiles != 0 {
		t.Fatalf("recent=%#v err=%v", job, err)
	}
	var writes int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM metadata_writes`).Scan(&writes); err != nil {
		t.Fatal(err)
	}
	if writes != 0 {
		t.Fatalf("unchanged import performed %d writes", writes)
	}
	var cover bytes.Buffer
	if err := jpeg.Encode(&cover, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "cover.jpg"), cover.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if job, err := scanner.ScanLibrary(lib); err != nil || job.ErrorCount != 0 || job.MetadataUpdatedFiles != 1 {
		t.Fatalf("cover-only=%#v err=%v", job, err)
	}
	if err := conn.QueryRow(`SELECT COUNT(*) FROM metadata_writes`).Scan(&writes); err != nil {
		t.Fatal(err)
	}
	if writes == 0 {
		t.Fatal("cover change did not revise metadata")
	}
}

func TestCalibreUnavailableSourceDoesNotDropNewBooks(t *testing.T) {
	for _, quick := range []bool{false, true} {
		t.Run(fmt.Sprint(quick), func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "Folder", "book.cbz")
			makeZip(t, path, map[string]string{"001.jpg": "image"})
			if err := os.WriteFile(filepath.Join(root, "metadata.db"), []byte("invalid database"), 0o644); err != nil {
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
			scanner := NewWithWorkerCount(st, func() int { return 1 })
			var job domain.ScanJob
			if quick {
				job, err = scanner.ScanLibraryPath(lib, path)
			} else {
				job, err = scanner.ScanLibrary(lib)
			}
			if err != nil || job.ErrorCount != 0 || job.IndexedFiles != 1 {
				t.Fatalf("job=%#v err=%v", job, err)
			}
			index, err := st.FileIndexByPath(path)
			if err != nil || index.Book.Title != "book" {
				t.Fatalf("lost new book=%#v err=%v", index, err)
			}
			events, err := st.ListJobEvents(job.ID)
			if err != nil {
				t.Fatal(err)
			}
			warned := false
			for _, event := range events {
				if event.Level == "warning" {
					warned = true
				}
			}
			if !warned {
				t.Fatal("missing fallback warning")
			}
		})
	}
}

func TestCalibreComicLibraryKeepsPDFAndEPUBDirectoryCollections(t *testing.T) {
	root := t.TempDir()
	epub := filepath.Join(root, "First", "book.epub")
	pdf := filepath.Join(root, "Second", "book.pdf")
	makeZip(t, epub, sampleEPUBEntriesWithMetadata("Embedded", "Writer", "Description"))
	if err := os.MkdirAll(filepath.Dir(pdf), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pdf, []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{epub, pdf} {
		writeCalibreOPF(t, path, "Title", "Same writer", "tag")
	}
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("Comics", root, "comic")
	if err != nil {
		t.Fatal(err)
	}
	if job, err := NewWithWorkerCount(st, func() int { return 4 }).ScanLibrary(lib); err != nil || job.ErrorCount != 0 {
		t.Fatalf("job=%#v err=%v", job, err)
	}
	for _, path := range []string{epub, pdf} {
		index, err := st.FileIndexByPath(path)
		if err != nil || index.Book.CollectionTitle != filepath.Base(filepath.Dir(path)) || index.Book.Creator != "Same writer" {
			t.Fatalf("merged comics=%#v err=%v", index, err)
		}
	}
}

func writeCalibreOPF(t *testing.T, bookPath, title, creator, tag string) {
	t.Helper()
	data := fmt.Sprintf(`<package xmlns="http://www.idpf.org/2007/opf"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>%s</dc:title><dc:creator>%s</dc:creator><dc:description>A description.</dc:description><dc:subject>%s</dc:subject></metadata></package>`, title, creator, tag)
	if err := os.WriteFile(filepath.Join(filepath.Dir(bookPath), "metadata.opf"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCalibreOPFRescanPreservesIdentityAndReaderState(t *testing.T) {
	for _, workers := range []int{1, 4} {
		t.Run(fmt.Sprint(workers), func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "Author", "Book (42)", "book.epub")
			makeZip(t, path, sampleEPUBEntriesWithMetadata("Embedded title", "Embedded author", "Embedded description"))
			writeCalibreOPF(t, path, "Calibre title", "Calibre author", "imported")
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
			scanner := NewWithWorkerCount(st, func() int { return workers })
			job, err := scanner.ScanLibrary(lib)
			if err != nil || job.ErrorCount != 0 {
				t.Fatalf("initial job=%#v err=%v", job, err)
			}
			index, err := st.FileIndexByPath(path)
			if err != nil {
				t.Fatal(err)
			}
			originalID := index.Book.ID
			if index.Book.Title != "Calibre title" || index.Book.Creator != "Calibre author" {
				t.Fatalf("metadata=%#v", index.Book)
			}
			if err := st.SaveProgressDetail(originalID, 1, "original-locator", 0.5); err != nil {
				t.Fatal(err)
			}
			if err := st.UpdateBookPrivateState(originalID, domain.BookPrivateState{Favorite: true, Tags: []string{"personal"}, Summary: "private note"}); err != nil {
				t.Fatal(err)
			}
			collection, err := st.CreateManualCollection(domain.ManualCollection{Name: "Keep"})
			if err != nil {
				t.Fatal(err)
			}
			if err := st.AddManualCollectionItem(collection.ID, domain.ManualCollectionItem{AssetType: "book", AssetID: originalID}); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Exec(`UPDATE pages SET entry_name='preserve-existing-page' WHERE book_id=? AND page_index=0`, originalID); err != nil {
				t.Fatal(err)
			}
			// The archive cannot be reopened; only the external sidecar is refreshed.
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, make([]byte, info.Size()), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
				t.Fatal(err)
			}
			writeCalibreOPF(t, path, "Renamed", "New creator", "updated")
			job, err = scanner.ScanLibrary(lib)
			if err != nil || job.ErrorCount != 0 || job.MetadataUpdatedFiles != 1 {
				t.Fatalf("rescan=%#v err=%v", job, err)
			}
			index, err = st.FileIndexByPath(path)
			if err != nil {
				t.Fatal(err)
			}
			book, err := st.BookByID(originalID)
			if err != nil {
				t.Fatal(err)
			}
			if index.Book.ID != originalID || book.Title != "Renamed" || book.Creator != "New creator" || !book.Favorite || book.Summary != "private note" {
				t.Fatalf("state changed=%#v index=%#v", book, index)
			}
			if got := strings.Join(book.Tags, ","); !strings.Contains(got, "updated") || !strings.Contains(got, "personal") {
				t.Fatalf("tags=%v", book.Tags)
			}
			progress, err := st.Progress(originalID)
			if err != nil || progress.PageIndex != 1 || progress.Locator != "original-locator" {
				t.Fatalf("progress=%#v err=%v", progress, err)
			}
			pages, err := st.ListPages(originalID)
			if err != nil || len(pages) == 0 || pages[0].Name != "preserve-existing-page" {
				t.Fatalf("pages rebuilt=%#v err=%v", pages, err)
			}
			items, err := st.ListManualCollectionItems(collection.ID)
			if err != nil || len(items) != 1 || items[0].AssetID != originalID {
				t.Fatalf("manual collection=%#v err=%v", items, err)
			}
		})
	}
}

func TestCalibreTargetedScanImportsWithoutExpandingArchiveAndKeepsID(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Author", "Book", "book.cbz")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("deferred archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeCalibreOPF(t, path, "First", "Writer", "first")
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
	scanner := NewWithWorkerCount(st, func() int { return 4 })
	first, err := scanner.ScanLibraryPath(lib, path)
	if err != nil || first.ErrorCount != 0 {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	index, err := st.FileIndexByPath(path)
	if err != nil {
		t.Fatal(err)
	}
	id := index.Book.ID
	if index.Book.Title != "First" || index.Book.CollectionTitle != "Author/Book" || index.Analyzed || index.PageCount != 0 {
		t.Fatalf("basic index=%#v", index)
	}
	writeCalibreOPF(t, path, "Second", "Another writer", "second")
	if err := os.WriteFile(path, []byte("changed but still deferred archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := scanner.ScanLibraryPath(lib, path)
	if err != nil || second.ErrorCount != 0 {
		t.Fatalf("second=%#v err=%v", second, err)
	}
	index, err = st.FileIndexByPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if index.Book.ID != id || index.Book.Title != "Second" || index.Book.CollectionTitle != "Author/Book" || index.Analyzed || index.PageCount != 0 {
		t.Fatalf("identity/page index changed=%#v", index)
	}
}

func TestCalibreRecentRefreshBypassesDirectoryPruningAndLimit(t *testing.T) {
	root := t.TempDir()
	paths := []string{filepath.Join(root, "Author", "One", "one.cbz"), filepath.Join(root, "Author", "Two", "two.cbz")}
	for i, path := range paths {
		makeZip(t, path, map[string]string{"001.jpg": "image"})
		writeCalibreOPF(t, path, fmt.Sprintf("Before %d", i), "Writer", "tag")
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
	scanner := NewWithWorkerCount(st, func() int { return 1 })
	if job, err := scanner.ScanLibrary(lib); err != nil || job.ErrorCount != 0 {
		t.Fatalf("first=%#v err=%v", job, err)
	}
	ids := map[string]int64{}
	for i, path := range paths {
		index, err := st.FileIndexByPath(path)
		if err != nil {
			t.Fatal(err)
		}
		ids[path] = index.Book.ID
		writeCalibreOPF(t, path, fmt.Sprintf("After %d", i), "Writer", "changed")
	}
	job, err := st.StartScanJobWithTarget(lib.ID, root)
	if err != nil {
		t.Fatal(err)
	}
	job, err = scanner.RunRecentScanJobPath(lib, job, root, 1)
	if err != nil || job.ErrorCount != 0 || job.MetadataUpdatedFiles != 2 {
		t.Fatalf("recent=%#v err=%v", job, err)
	}
	for i, path := range paths {
		book, err := st.BookByID(ids[path])
		if err != nil || book.Title != fmt.Sprintf("After %d", i) {
			t.Fatalf("book=%#v err=%v", book, err)
		}
	}
}

func TestCalibreDatabaseWinsAndRefreshesUnchangedPDF(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Writer", "Book (999)", "exact.pdf")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeCalibreOPF(t, path, "Sidecar title", "Sidecar writer", "sidecar")
	calibre, err := sql.Open("sqlite", filepath.Join(root, "metadata.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE books(id INTEGER PRIMARY KEY,path TEXT,title TEXT)`, `CREATE TABLE data(id INTEGER PRIMARY KEY,book INTEGER,name TEXT,format TEXT)`, `CREATE TABLE comments(book INTEGER,text TEXT)`, `CREATE TABLE authors(id INTEGER PRIMARY KEY,name TEXT)`, `CREATE TABLE books_authors_link(id INTEGER PRIMARY KEY,book INTEGER,author INTEGER)`, `CREATE TABLE tags(id INTEGER PRIMARY KEY,name TEXT)`, `CREATE TABLE books_tags_link(id INTEGER PRIMARY KEY,book INTEGER,tag INTEGER)`,
		`INSERT INTO books VALUES(7,'Writer/Book (999)','Database title')`, `INSERT INTO data VALUES(1,7,'exact','PDF')`, `INSERT INTO comments VALUES(7,'Database description')`, `INSERT INTO authors VALUES(1,'Database writer')`, `INSERT INTO books_authors_link VALUES(1,7,1)`, `INSERT INTO tags VALUES(1,'database')`, `INSERT INTO books_tags_link VALUES(1,7,1)`,
	} {
		if _, err := calibre.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	calibre.Close()
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
	scanner := NewWithWorkerCount(st, func() int { return 1 })
	if job, err := scanner.ScanLibrary(lib); err != nil || job.ErrorCount != 0 {
		t.Fatalf("first=%#v err=%v", job, err)
	}
	index, err := st.FileIndexByPath(path)
	if err != nil {
		t.Fatal(err)
	}
	id := index.Book.ID
	if index.Book.Title != "Database title" || index.Book.Creator != "Database writer" {
		t.Fatalf("metadata=%#v", index)
	}
	calibre, err = sql.Open("sqlite", filepath.Join(root, "metadata.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := calibre.Exec(`UPDATE books SET title='Database changed'`); err != nil {
		t.Fatal(err)
	}
	calibre.Close()
	if job, err := scanner.ScanLibrary(lib); err != nil || job.ErrorCount != 0 || job.MetadataUpdatedFiles != 1 {
		t.Fatalf("rescan=%#v err=%v", job, err)
	}
	index, err = st.FileIndexByPath(path)
	if err != nil || index.Book.ID != id || index.Book.Title != "Database changed" {
		t.Fatalf("index=%#v err=%v", index, err)
	}
	// A live/unavailable Calibre DB without an OPF must not erase the last import.
	if err := os.Remove(filepath.Join(filepath.Dir(path), "metadata.opf")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "metadata.db-wal"), []byte("active WAL"), 0o644); err != nil {
		t.Fatal(err)
	}
	job, err := scanner.ScanLibrary(lib)
	if err != nil || job.ErrorCount != 1 {
		t.Fatalf("unavailable source job=%#v err=%v", job, err)
	}
	index, err = st.FileIndexByPath(path)
	if err != nil || index.Book.ID != id || index.Book.Title != "Database changed" || index.Book.Creator != "Database writer" {
		t.Fatalf("lost prior import=%#v err=%v", index, err)
	}
}

func TestCalibreConcurrentDuplicateTitlesRetainDistinctBookIDs(t *testing.T) {
	root := t.TempDir()
	paths := []string{filepath.Join(root, "Author", "First (10)", "book.epub"), filepath.Join(root, "Author", "Second (20)", "book.epub")}
	for i, path := range paths {
		makeZip(t, path, sampleEPUBEntriesWithMetadata("Embedded", "Embedded", "Description"))
		writeCalibreOPF(t, path, fmt.Sprintf("Unique %d", i), "Author", "tag")
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
	scanner := NewWithWorkerCount(st, func() int { return 4 })
	if job, err := scanner.ScanLibrary(lib); err != nil || job.ErrorCount != 0 {
		t.Fatalf("first=%#v err=%v", job, err)
	}
	ids := map[string]int64{}
	for _, path := range paths {
		index, err := st.FileIndexByPath(path)
		if err != nil {
			t.Fatal(err)
		}
		ids[path] = index.Book.ID
		writeCalibreOPF(t, path, "Shared title", "Author", "tag")
	}
	for scan := 0; scan < 2; scan++ {
		if job, err := scanner.ScanLibrary(lib); err != nil || job.ErrorCount != 0 {
			t.Fatalf("duplicate rescan=%#v err=%v", job, err)
		}
	}
	titles := map[string]bool{}
	for _, path := range paths {
		index, err := st.FileIndexByPath(path)
		if err != nil || index.Book.ID != ids[path] {
			t.Fatalf("lost identity=%#v err=%v", index, err)
		}
		titles[index.Book.Title] = true
	}
	if len(titles) != 2 || !titles["Shared title (10)"] || !titles["Shared title (20)"] {
		t.Fatalf("titles=%v", titles)
	}
}

func TestCalibreRescanPreservesSharedEditsAcrossSourceChanges(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Author", "Book (42)", "book.epub")
	makeZip(t, path, sampleEPUBEntriesWithMetadata("Embedded", "Writer", "Description"))
	writeCalibreOPF(t, path, "Imported", "Writer", "tag")
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
	scanner := NewWithWorkerCount(st, func() int { return 1 })
	if _, err := scanner.ScanLibrary(lib); err != nil {
		t.Fatal(err)
	}
	index, err := st.FileIndexByPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpdateBookIdentity(index.Book.ID, index.Book.SeriesID, "My title", "epub"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpdateBookMetadata(index.Book.ID, "My author", "", []string{}); err != nil {
		t.Fatal(err)
	}
	for _, sourceTitle := range []string{"Changed", "My title", "Changed again"} {
		writeCalibreOPF(t, path, sourceTitle, "New author", "new tag")
		if job, err := scanner.ScanLibrary(lib); err != nil || job.ErrorCount != 0 {
			t.Fatalf("job=%#v err=%v", job, err)
		}
		shared, err := st.BookSharedMetadata(index.Book.ID)
		if err != nil || shared.Title != "My title" || shared.Creator != "My author" || shared.Description != "" || len(shared.Tags) != 0 || shared.SeriesID != index.Book.SeriesID {
			t.Fatalf("shared edit overwritten: %#v err=%v", shared, err)
		}
	}
	raw, err := st.Setting(importedBookStateKey(index.Book.ID))
	var state importedBookState
	if err != nil || json.Unmarshal([]byte(raw), &state) != nil || state.Title != "Changed again" || state.Creator != "New author" || state.Description != "A description." || !state.preserves("title") || !state.preserves("description") {
		t.Fatalf("source baseline contains preserved user values: %s err=%v", raw, err)
	}
	if err := os.Remove(filepath.Join(filepath.Dir(path), "metadata.opf")); err != nil {
		t.Fatal(err)
	}
	if job, err := scanner.ScanLibrary(lib); err != nil || job.ErrorCount != 0 {
		t.Fatalf("removed source=%#v err=%v", job, err)
	}
	shared, err := st.BookSharedMetadata(index.Book.ID)
	if err != nil || shared.Title != "My title" || shared.Creator != "My author" || shared.Description != "" || len(shared.Tags) != 0 || shared.SeriesID != index.Book.SeriesID {
		t.Fatalf("source removal overwrote user values: %#v err=%v", shared, err)
	}
}

func TestCalibreLegacyImportPreservesUnprovenSharedValuesAndReaderState(t *testing.T) {
	for _, mode := range []string{"automatic", "manual", "cleared", "unreadable"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "Folder", "Book (42)", "book.epub")
			makeZip(t, path, sampleEPUBEntriesWithMetadata("Embedded", "Old author", "Embedded description"))
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
			scanner := NewWithWorkerCount(st, func() int { return 1 })
			if job, err := scanner.ScanLibrary(lib); err != nil || job.ErrorCount != 0 {
				t.Fatalf("legacy job=%#v err=%v", job, err)
			}
			index, err := st.FileIndexByPath(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.Setting(importedBookStateKey(index.Book.ID)); err != sql.ErrNoRows {
				t.Fatalf("legacy entry unexpectedly has provenance: %v", err)
			}
			if err := st.SaveProgressDetail(index.Book.ID, 1, "legacy-locator", 0.5); err != nil {
				t.Fatal(err)
			}
			if err := st.UpdateBookPrivateState(index.Book.ID, domain.BookPrivateState{Favorite: true, Summary: "private", Tags: []string{"private-tag"}}); err != nil {
				t.Fatal(err)
			}
			if err := st.UpdateCollectionPrivateStateForProfile(index.Book.SeriesID, 0, domain.CollectionPrivateState{Favorite: true}); err != nil {
				t.Fatal(err)
			}
			if mode == "manual" {
				if _, err := st.UpdateBookIdentity(index.Book.ID, index.Book.SeriesID, "My title", "epub"); err != nil {
					t.Fatal(err)
				}
				if _, err := st.UpdateBookMetadata(index.Book.ID, "My author", "Embedded description", []string{"my-tag"}); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "cleared" {
				if _, err := st.UpdateBookMetadata(index.Book.ID, "", "", nil); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "unreadable" {
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, make([]byte, info.Size()), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
					t.Fatal(err)
				}
			}
			for _, title := range []string{"Calibre title", "Updated Calibre title"} {
				writeCalibreOPF(t, path, title, "New author", "source-tag")
				if job, err := scanner.ScanLibraryPath(lib, path); err != nil || job.ErrorCount != 0 {
					t.Fatalf("refresh=%#v err=%v", job, err)
				}
			}
			book, err := st.BookByID(index.Book.ID)
			if err != nil || !book.Favorite || book.Summary != "private" {
				t.Fatalf("private state changed: %#v err=%v", book, err)
			}
			progress, err := st.Progress(index.Book.ID)
			if err != nil || progress.Locator != "legacy-locator" {
				t.Fatalf("progress changed: %#v err=%v", progress, err)
			}
			shared, err := st.BookSharedMetadata(index.Book.ID)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "automatic":
				if shared.Title != "Updated Calibre title" || shared.Creator != "New author" || shared.SeriesID == index.Book.SeriesID {
					t.Fatalf("automatic fields not refreshed: %#v", shared)
				}
			case "manual":
				if shared.Title != "My title" || shared.Creator != "My author" || shared.Description != "A description." || !sameStringList(shared.Tags, []string{"my-tag"}) || shared.SeriesID != index.Book.SeriesID {
					t.Fatalf("manual fields changed: %#v", shared)
				}
			case "unreadable":
				if shared.Title != "Embedded" || shared.Creator != "Old author" || shared.Description != "Embedded description" || shared.SeriesID != index.Book.SeriesID {
					t.Fatalf("unproven legacy fields changed: %#v", shared)
				}
			case "cleared":
				if shared.Creator != "" || shared.Description != "" || shared.SeriesID != index.Book.SeriesID {
					t.Fatalf("cleared legacy fields refilled: %#v", shared)
				}
			}
			collection, err := st.CollectionPrivateStateForProfile(shared.SeriesID, 0)
			if err != nil || !collection.Favorite {
				t.Fatalf("collection favorite lost: %#v err=%v", collection, err)
			}
		})
	}
}

func TestCalibreTitleCollisionDoesNotRenamePreservedTitle(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "Author", "First (10)", "book.epub")
	second := filepath.Join(root, "Author", "Second (20)", "book.epub")
	for i, path := range []string{first, second} {
		makeZip(t, path, sampleEPUBEntriesWithMetadata("Embedded", "Writer", "Description"))
		writeCalibreOPF(t, path, fmt.Sprintf("Title %d", i), "Writer", "tag")
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
	scanner := NewWithWorkerCount(st, func() int { return 1 })
	if _, err := scanner.ScanLibrary(lib); err != nil {
		t.Fatal(err)
	}
	index, err := st.FileIndexByPath(first)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpdateBookIdentity(index.Book.ID, index.Book.SeriesID, "Keep", "epub"); err != nil {
		t.Fatal(err)
	}
	if _, err := scanner.ScanLibraryPath(lib, first); err != nil {
		t.Fatal(err)
	}
	writeCalibreOPF(t, second, "Keep", "Writer", "tag")
	if job, err := scanner.ScanLibraryPath(lib, second); err != nil || job.ErrorCount != 0 {
		t.Fatalf("collision=%#v err=%v", job, err)
	}
	book, err := st.BookByID(index.Book.ID)
	if err != nil || book.Title != "Keep" {
		t.Fatalf("preserved title renamed: %#v err=%v", book, err)
	}
	other, err := st.FileIndexByPath(second)
	if err != nil || other.Book.Title != "Keep (20)" {
		t.Fatalf("automatic title not disambiguated: %#v err=%v", other, err)
	}
}
