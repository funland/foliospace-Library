package scanner

import (
	"fmt"
	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/store"
	"os"
	"path/filepath"
	"testing"
)

func TestMixedScanCompletionOnlyCountsGamesInScope(t *testing.T) {
	for _, workers := range []int{1, 3} {
		for _, mode := range []string{"full", "targeted", "recent"} {
			t.Run(fmt.Sprintf("%d/%s", workers, mode), func(t *testing.T) {
				root := t.TempDir()
				comics := filepath.Join(root, "Comics")
				games := filepath.Join(root, "SNES")
				if err := os.MkdirAll(games, 0755); err != nil {
					t.Fatal(err)
				}
				makeZip(t, filepath.Join(comics, "Book.cbz"), map[string]string{"001.jpg": "image"})
				if err := os.WriteFile(filepath.Join(games, "Game.sfc"), []byte("game"), 0600); err != nil {
					t.Fatal(err)
				}
				conn, err := db.Open(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				st := store.New(conn)
				lib, err := st.CreateLibraryWithType("Mixed", root, "mixed")
				if err != nil {
					t.Fatal(err)
				}
				scanner := NewWithWorkerCount(st, func() int { return workers })
				var completed []domain.ScanJob
				scanner.SetCompletionHook(func(_ domain.Library, j domain.ScanJob) { completed = append(completed, j) })
				run := func(path string) {
					t.Helper()
					var j domain.ScanJob
					var e error
					if mode == "recent" {
						j, e = st.StartScanJobWithTarget(lib.ID, root)
						if e == nil {
							j, e = scanner.RunRecentScanJobPath(lib, j, root, 100)
						}
					} else if mode == "full" {
						j, e = scanner.ScanLibrary(lib)
					} else {
						j, e = scanner.ScanLibraryPath(lib, path)
					}
					if e != nil || j.ErrorCount != 0 {
						t.Fatalf("scan=%+v %v", j, e)
					}
				}
				run(root)
				if completed[len(completed)-1].GameFiles != 1 {
					t.Fatalf("no game observation: %+v", completed)
				}
				// Even when a game elsewhere is already indexed, a comic directory scan must not trigger analysis.
				if _, err = scanner.ScanLibraryPath(lib, comics); err != nil {
					t.Fatal(err)
				}
				if completed[len(completed)-1].GameFiles != 0 {
					t.Fatal("unrelated game leaked into comic scan")
				}
				// Skipped games still constitute a real game scan.
				run(root)
				want := 1
				if mode == "recent" {
					want = 0
				} // recent scans skip cached games
				if completed[len(completed)-1].GameFiles != want {
					t.Fatal("unchanged game observation lost")
				}
			})
		}
	}
}
