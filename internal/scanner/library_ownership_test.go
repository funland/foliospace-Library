package scanner

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/store"
)

func TestScanNestedLibrariesKeepMostSpecificOwner(t *testing.T) {
	for _, workers := range []int{1, 3} {
		for _, mode := range []string{"full", "targeted", "recent"} {
			t.Run(fmt.Sprintf("workers=%d/%s", workers, mode), func(t *testing.T) {
				root := t.TempDir()
				parentPath := filepath.Join(root, "parent.cbz")
				childPath := filepath.Join(root, "Nested", "child.cbz")
				deepPath := filepath.Join(root, "Nested", "Deep", "deep.cbz")
				siblingPath := filepath.Join(root, "NestedSibling", "sibling.cbz")
				for _, path := range []string{parentPath, childPath, deepPath, siblingPath} {
					makeZip(t, path, map[string]string{"001.jpg": "image"})
				}
				conn, err := db.Open(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				st := store.New(conn)
				parent, _ := st.CreateLibrary("Parent", root)
				child, _ := st.CreateLibrary("Child", filepath.Dir(childPath))
				deep, _ := st.CreateLibrary("Deep", filepath.Dir(deepPath))
				scanner := NewWithWorkerCount(st, func() int { return workers })
				scan := func(lib domain.Library, target string) domain.ScanJob {
					t.Helper()
					var job domain.ScanJob
					var err error
					switch mode {
					case "full":
						job, err = scanner.ScanLibrary(lib)
					case "targeted":
						job, err = scanner.ScanLibraryPath(lib, target)
					case "recent":
						job, err = scanner.RunRecentScanJobPath(lib, mustStartScanJob(t, st, lib.ID), target, 20)
					}
					if err != nil || job.Status != "completed" || job.ErrorCount != 0 {
						t.Fatalf("scan=%+v err=%v", job, err)
					}
					return job
				}
				// Child-first and repeated parent scans must never steal an ID.
				scan(deep, deep.RootPath)
				scan(child, child.RootPath)
				before, err := st.FileIndexByPath(childPath)
				if err != nil {
					t.Fatal(err)
				}
				if err := st.SaveProgressDetail(before.Book.ID, 0, "keep", 0.5); err != nil {
					t.Fatal(err)
				}
				if err := st.UpdateBookPrivateState(before.Book.ID, domain.BookPrivateState{Favorite: true, Summary: "private note"}); err != nil {
					t.Fatal(err)
				}
				scan(parent, parent.RootPath)
				scan(parent, parent.RootPath)
				for path, owner := range map[string]int64{parentPath: parent.ID, childPath: child.ID, deepPath: deep.ID, siblingPath: parent.ID} {
					index, err := st.FileIndexByPath(path)
					if err != nil || index.File.LibraryID != owner {
						t.Fatalf("%s owner=%d want=%d err=%v", path, index.File.LibraryID, owner, err)
					}
				}
				after, _ := st.FileIndexByPath(childPath)
				book, _ := st.BookByID(before.Book.ID)
				progress, _ := st.Progress(before.Book.ID)
				if after.Book.ID != before.Book.ID || !book.Favorite || book.Summary != "private note" || progress.Locator != "keep" {
					t.Fatalf("identity/private state changed: %+v %+v %+v", after, book, progress)
				}
				if mode != "full" {
					job := scan(parent, child.RootPath)
					if job.IndexedFiles != 0 || job.MetadataUpdatedFiles != 0 || job.DiscoveredFiles != 0 {
						t.Fatalf("explicit child target escaped ownership: %+v", job)
					}
				}
				errors, err := st.ListFileErrors()
				if err != nil || len(errors) != 0 {
					t.Fatalf("excluded child generated missing/errors: %+v %v", errors, err)
				}
			})
		}
	}
}

func TestScanNestedLibraryPreservesLegacyOwnershipAndMetadata(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Nested", "book.cbz")
	makeZip(t, path, map[string]string{"001.jpg": "image"})
	writeCalibreOPF(t, path, "Original", "Original author", "original")
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	parent, _ := st.CreateLibrary("Parent", root)
	scanner := NewWithWorkerCount(st, func() int { return 1 })
	if _, err := scanner.ScanLibrary(parent); err != nil {
		t.Fatal(err)
	}
	before, err := st.FileIndexByPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpdateBookMetadata(before.Book.ID, "Manual author;", "Manual description", []string{"manual"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveProgressDetail(before.Book.ID, 0, "legacy", 0.7); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateBookPrivateState(before.Book.ID, domain.BookPrivateState{Favorite: true}); err != nil {
		t.Fatal(err)
	}
	child, _ := st.CreateLibrary("Child", filepath.Dir(path))
	writeCalibreOPF(t, path, "New sidecar", "New author", "new")
	for _, lib := range []domain.Library{parent, child} {
		job, err := scanner.RunRecentScanJobPath(lib, mustStartScanJob(t, st, lib.ID), lib.RootPath, 20)
		if err != nil || job.ErrorCount != 0 || job.MetadataUpdatedFiles != 0 {
			t.Fatalf("recent %+v %v", job, err)
		}
		job, err = scanner.ScanLibrary(lib)
		if err != nil || job.ErrorCount != 0 || job.IndexedFiles != 0 {
			t.Fatalf("full %+v %v", job, err)
		}
	}
	after, err := st.FileIndexByPath(path)
	if err != nil || after.Book.ID != before.Book.ID || after.File.LibraryID != parent.ID || after.Book.Title != "Original" || after.Book.Creator != "Manual author;" {
		t.Fatalf("legacy record changed: %+v %v", after, err)
	}
	book, _ := st.BookByID(before.Book.ID)
	progress, _ := st.Progress(before.Book.ID)
	if !book.Favorite || progress.Locator != "legacy" {
		t.Fatalf("private state changed: %+v %+v", book, progress)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := scanner.ScanLibrary(parent); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FileIndexByPath(path); err != nil {
		t.Fatalf("excluded legacy record deleted: %v", err)
	}
}

func TestScanNestedLibraryDoesNotClaimNaomiCompanion(t *testing.T) {
	root := filepath.Join(t.TempDir(), "NAOMI 2")
	path := filepath.Join(root, "vf4", "vf4.zip")
	makeZip(t, path, map[string]string{"317-0314-com.pic": "vf4-pic"})
	childRoot := filepath.Join(root, "vf4", "vf4")
	if err := os.MkdirAll(childRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	companion := filepath.Join(childRoot, "gds-0012c.chd")
	if err := os.WriteFile(companion, []byte("gd-rom"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	parent, _ := st.CreateLibraryWithType("NAOMI 2", root, "game")
	child, _ := st.CreateLibraryWithType("Child", childRoot, "game")
	game, err := st.UpsertGame(domain.GameAsset{LibraryID: child.ID, Title: "Child choice", FilePath: companion, Format: "chd", Platform: "naomi2", RelPath: "gds-0012c.chd"})
	if err != nil {
		t.Fatal(err)
	}
	job, err := NewWithWorkerCount(st, func() int { return 1 }).ScanLibrary(parent)
	if err != nil || job.ErrorCount != 0 || job.IndexedFiles != 1 {
		t.Fatalf("scan=%+v %v", job, err)
	}
	after, err := st.GameByID(game.ID)
	if err != nil || after.LibraryID != child.ID || after.Title != "Child choice" {
		t.Fatalf("indirect companion changed: %+v %v", after, err)
	}
}

func TestScanNestedLibraryPreservesLegacySkippedDirectoryEntries(t *testing.T) {
	for _, workers := range []int{1, 3} {
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			root := t.TempDir()
			childRoot := filepath.Join(root, "Nested")
			if err := os.MkdirAll(childRoot, 0o755); err != nil {
				t.Fatal(err)
			}
			conn, err := db.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			st := store.New(conn)
			parent, _ := st.CreateLibrary("Parent", root)
			if _, err := st.CreateLibrary("Child", childRoot); err != nil {
				t.Fatal(err)
			}
			series, err := st.UpsertSeries(parent.ID, "Legacy", "Nested")
			if err != nil {
				t.Fatal(err)
			}
			before := map[string]int64{}
			for _, rel := range []string{"Nested/#recycle/old.cbz", "Nested/._old.cbz"} {
				book, err := st.UpsertBook(series.ID, rel, "cbz")
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(root, rel)
				if _, err := st.UpsertFile(book.ID, parent.ID, path, rel, 10, time.Now(), ".cbz"); err != nil {
					t.Fatal(err)
				}
				if err := st.UpdateBookPrivateState(book.ID, domain.BookPrivateState{Favorite: true}); err != nil {
					t.Fatal(err)
				}
				before[path] = book.ID
			}
			job, err := NewWithWorkerCount(st, func() int { return workers }).ScanLibrary(parent)
			if err != nil || job.ErrorCount != 0 {
				t.Fatalf("scan=%+v %v", job, err)
			}
			for path, id := range before {
				index, err := st.FileIndexByPath(path)
				if err != nil || index.File.LibraryID != parent.ID || index.Book.ID != id {
					t.Fatalf("legacy skipped row changed: %+v %v", index, err)
				}
				book, err := st.BookByID(id)
				if err != nil || !book.Favorite {
					t.Fatalf("favorite deleted: %+v %v", book, err)
				}
			}
		})
	}
}
