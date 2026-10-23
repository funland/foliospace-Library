package scanner

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/store"
)

func TestScanReconciledDirectoryKeepsSeriesIdentityAndProfileState(t *testing.T) {
	for _, workers := range []int{1, 3} {
		for _, curatedTitle := range []bool{false, true} {
			for _, mode := range []string{"full", "targeted", "recent"} {
				t.Run(fmt.Sprintf("workers=%d/curated=%t/%s", workers, curatedTitle, mode), func(t *testing.T) {
					root := t.TempDir()
					childRoot := filepath.Join(root, "Legacy", "Collection")
					paths := []string{filepath.Join(childRoot, "first.cbz"), filepath.Join(childRoot, "second.zip"), filepath.Join(childRoot, "third.epub")}
					for _, path := range paths[:2] {
						makeZip(t, path, map[string]string{"001.jpg": "image"})
					}
					makeZip(t, paths[2], sampleEPUBEntriesWithMetadata("Third", "Writer", "Description"))
					conn, err := db.Open(t.TempDir())
					if err != nil {
						t.Fatal(err)
					}
					defer conn.Close()
					st := store.New(conn)
					parent, err := st.CreateLibraryWithType("Parent", root, "comic")
					if err != nil {
						t.Fatal(err)
					}
					scanner := NewWithWorkerCount(st, func() int { return workers })
					if job, err := scanner.ScanLibrary(parent); err != nil || job.ErrorCount != 0 {
						t.Fatalf("initial scan=%+v err=%v", job, err)
					}
					profile, err := st.CreateProfile("Other reader", "reader", "blue")
					if err != nil {
						t.Fatal(err)
					}
					manual, err := st.CreateManualCollection(domain.ManualCollection{Name: "Keep these"})
					if err != nil {
						t.Fatal(err)
					}
					before := map[string]store.FileIndex{}
					sharedBefore := map[string]string{}
					var seriesID int64
					for _, path := range paths {
						index, err := st.FileIndexByPath(path)
						if err != nil {
							t.Fatal(err)
						}
						before[path] = index
						if _, err := st.UpdateBookIdentity(index.Book.ID, index.Book.SeriesID, "My "+filepath.Base(path), index.Book.Format); err != nil {
							t.Fatal(err)
						}
						if _, err := st.UpdateBookMetadata(index.Book.ID, "My writer", "My description", []string{"my shared tag"}); err != nil {
							t.Fatal(err)
						}
						shared, err := st.BookSharedMetadata(index.Book.ID)
						if err != nil {
							t.Fatal(err)
						}
						encoded, err := json.Marshal(shared)
						if err != nil {
							t.Fatal(err)
						}
						sharedBefore[path] = string(encoded)
						if seriesID != 0 && seriesID != index.Book.SeriesID {
							t.Fatal("fixture formats did not share one directory series")
						}
						seriesID = index.Book.SeriesID
						for _, profileID := range []int64{1, profile.ID} {
							if err := st.SaveProgressDetailForProfile(index.Book.ID, profileID, 0, "keep-locator", 0.6); err != nil {
								t.Fatal(err)
							}
							if err := st.UpdateBookPrivateStateForProfile(index.Book.ID, profileID, domain.BookPrivateState{Favorite: true, Rating: 4, Tags: []string{"personal"}, Summary: "private note"}); err != nil {
								t.Fatal(err)
							}
							if _, err := conn.Exec(`INSERT INTO profile_read_positions(profile_id,book_id,reader_mode,schema,payload_json) VALUES(?,?,'webtoon','test','{"anchor":"keep"}')`, profileID, index.Book.ID); err != nil {
								t.Fatal(err)
							}
						}
						if err := st.AddManualCollectionItem(manual.ID, domain.ManualCollectionItem{AssetType: "book", AssetID: index.Book.ID}); err != nil {
							t.Fatal(err)
						}
					}
					if err := st.UpdateCollectionPrivateStateForProfile(seriesID, 1, domain.CollectionPrivateState{Favorite: true}); err != nil {
						t.Fatal(err)
					}
					if err := st.UpdateCollectionPrivateStateForProfile(seriesID, profile.ID, domain.CollectionPrivateState{Liked: true}); err != nil {
						t.Fatal(err)
					}
					label := "Legacy/Collection"
					if curatedTitle {
						label = "My curated collection"
					}
					child, err := st.CreateLibraryWithType("Child", childRoot, "comic")
					if err != nil {
						t.Fatal(err)
					}
					// Model the reviewed whole-series migration without changing its ID,
					// label, book identities, or any per-reader rows.
					if _, err := conn.Exec(`UPDATE series SET library_id=?,directory_path='.',title=? WHERE id=?`, child.ID, label, seriesID); err != nil {
						t.Fatal(err)
					}
					for _, path := range paths {
						if _, err := conn.Exec(`UPDATE files SET library_id=?,rel_path=? WHERE abs_path=?`, child.ID, filepath.Base(path), path); err != nil {
							t.Fatal(err)
						}
						info, err := os.Stat(path)
						if err != nil {
							t.Fatal(err)
						}
						// Exercise metadata reindexing for every format, including EPUB.
						changed := info.ModTime().Add(time.Second)
						if err := os.Chtimes(path, changed, changed); err != nil {
							t.Fatal(err)
						}
					}
					stateBefore := reconciliationStateSnapshot(t, conn)
					var job domain.ScanJob
					switch mode {
					case "full":
						job, err = scanner.ScanLibrary(child)
					case "targeted":
						job, err = scanner.ScanLibraryPath(child, childRoot)
					case "recent":
						job, err = scanner.RunRecentScanJobPath(child, mustStartScanJob(t, st, child.ID), childRoot, 20)
					}
					if err != nil || job.ErrorCount != 0 || job.Status != "completed" {
						t.Fatalf("reconciled scan=%+v err=%v", job, err)
					}
					for _, path := range paths {
						after, err := st.FileIndexByPath(path)
						if err != nil || after.File.ID != before[path].File.ID || after.Book.ID != before[path].Book.ID || after.Book.SeriesID != seriesID || after.Book.CollectionTitle != label || after.File.LibraryID != child.ID {
							t.Fatalf("identity changed for %s: %+v err=%v", path, after, err)
						}
						shared, err := st.BookSharedMetadata(after.Book.ID)
						if err != nil {
							t.Fatal(err)
						}
						encoded, err := json.Marshal(shared)
						if err != nil || string(encoded) != sharedBefore[path] {
							t.Fatalf("manual shared metadata changed for %s: %+v err=%v", path, shared, err)
						}
					}
					if after := reconciliationStateSnapshot(t, conn); after != stateBefore {
						t.Fatalf("reader state changed after rescan:\nbefore=%s\nafter=%s", stateBefore, after)
					}
					var count int
					if err := conn.QueryRow(`SELECT COUNT(*) FROM series`).Scan(&count); err != nil || count != 1 {
						t.Fatalf("rescan created a replacement series: count=%d err=%v", count, err)
					}
				})
			}
		}
	}
}

func reconciliationStateSnapshot(t *testing.T, conn *sql.DB) string {
	t.Helper()
	snapshot := map[string][]any{}
	for _, table := range []string{"read_progress", "profile_read_progress", "profile_read_positions", "book_private_states", "collection_private_states", "manual_collection_items"} {
		rows, err := conn.Query(`SELECT * FROM ` + table + ` ORDER BY rowid`)
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				t.Fatal(err)
			}
			snapshot[table] = append(snapshot[table], values)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
