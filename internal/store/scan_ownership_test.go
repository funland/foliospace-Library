package store

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"foliospace-reader/internal/domain"
)

func TestLibraryOwnershipPreviewIsBoundedReadOnlyAndPathScoped(t *testing.T) {
	st := comicTestStore(t)
	root := filepath.Join(t.TempDir(), "Library%_")
	parent, _ := st.CreateLibrary("Parent", root)
	child, _ := st.CreateLibrary("Child", filepath.Join(root, "Child"))
	sibling, _ := st.CreateLibrary("Sibling", root+"Sibling")
	series, _ := st.UpsertSeries(parent.ID, "Legacy", "Child")
	for i := 0; i < 15; i++ {
		book, err := st.UpsertBook(series.ID, fmt.Sprintf("Book %02d", i), "cbz")
		if err != nil {
			t.Fatal(err)
		}
		rel := fmt.Sprintf("Child/book%02d.cbz", i)
		if _, err := st.UpsertFile(book.ID, parent.ID, filepath.Join(root, rel), rel, 10, time.Now(), ".cbz"); err != nil {
			t.Fatal(err)
		}
	}
	game, err := st.UpsertGame(domain.GameAsset{LibraryID: parent.ID, Title: "Game", Platform: "gba", Format: "gba", FilePath: filepath.Join(child.RootPath, "game.gba"), RelPath: "Child/game.gba"})
	if err != nil {
		t.Fatal(err)
	}
	video, err := st.UpsertVideo(domain.VideoAsset{LibraryID: parent.ID, Title: "Video", Format: "mp4", FilePath: filepath.Join(child.RootPath, "video.mp4"), RelPath: "Child/video.mp4"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertGame(domain.GameAsset{LibraryID: sibling.ID, Title: "Sibling", Platform: "gba", Format: "gba", FilePath: filepath.Join(sibling.RootPath, "game.gba"), RelPath: "game.gba"}); err != nil {
		t.Fatal(err)
	}
	for _, library := range []domain.Library{parent, child} {
		preview, err := st.PreviewLibraryOwnership(library.ID)
		if err != nil || preview.ConflictingRecords != 17 || len(preview.Examples) != 12 {
			t.Fatalf("preview=%+v %v", preview, err)
		}
		if library.ID == parent.ID && (len(preview.ExcludedRoots) != 1 || preview.ExcludedRoots[0].LibraryID != child.ID) {
			t.Fatalf("excluded roots=%+v", preview.ExcludedRoots)
		}
		if library.ID == child.ID && len(preview.ExcludedRoots) != 0 {
			t.Fatalf("child roots=%+v", preview.ExcludedRoots)
		}
		for _, item := range preview.Examples {
			if item.CurrentLibraryID != parent.ID || item.ExpectedLibraryID != child.ID {
				t.Fatalf("unexpected conflict=%+v", item)
			}
		}
	}
	paths, err := st.ForeignScanPaths(child.ID, child.RootPath)
	if err != nil || len(paths) != 17 || !paths[game.FilePath] || !paths[video.FilePath] {
		t.Fatalf("foreign paths=%+v %v", paths, err)
	}
	updatedGame, _ := st.GameByID(game.ID)
	updatedVideo, _ := st.VideoByID(video.ID)
	if updatedGame.LibraryID != parent.ID || updatedVideo.LibraryID != parent.ID {
		t.Fatalf("preview reassigned records: %+v %+v", updatedGame, updatedVideo)
	}
	all, err := st.ListFileIndexesByLibrary(parent.ID)
	if err != nil || len(all) != 15 {
		t.Fatalf("preview changed books=%+v %v", all, err)
	}
}
