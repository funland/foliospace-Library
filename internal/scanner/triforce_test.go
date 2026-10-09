package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/store"
)

func TestTriforceClientCompatibleRouting(t *testing.T) {
	for _, tc := range []struct{ name, root, path, ext, want string }{
		{"Games", "/games", "Triforce/F-Zero AX.iso", ".iso", "ngc"},
		{"Games", "/games", "triforce/Mario Kart GP 2.rvz", ".rvz", "ngc"},
		{"Triforce", "/library", "game.gcm", ".gcm", "ngc"},
		{"Games", "/games/Triforce", "game.iso", ".iso", "ngc"},
		{"Games", "/games", "NGC/game.iso", ".iso", "ngc"},
		{"Games", "/games", "Other/game.iso", ".iso", "disc"},
		{"Games", "/games", "Triforce-ish/game.iso", ".iso", "disc"},
	} {
		got := inferLibraryGamePlatform(domain.Library{Name: tc.name, RootPath: tc.root}, tc.ext, tc.path)
		if got != tc.want {
			t.Fatalf("%+v: got %s", tc, got)
		}
	}
}

func TestTriforceScanPreservesHardwareAndOriginalEntry(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Triforce", "F-Zero AX (Rev E) (Japan).iso")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	body := []byte("scanner routing fixture; not a playable image")
	if err := os.WriteFile(path, body, 0644); err != nil {
		t.Fatal(err)
	}
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("Games", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		job, err := New(st).ScanLibrary(lib)
		if err != nil || job.ErrorCount != 0 {
			t.Fatalf("scan: %+v %v", job, err)
		}
		games, err := st.ListRecentGames(10)
		if err != nil || len(games) != 1 {
			t.Fatalf("games: %+v %v", games, err)
		}
		g := games[0]
		if g.Platform != "ngc" || g.ROMSetName != "Triforce" || g.EmulatorHint != "dolphin" || g.Title != "F-Zero AX" {
			t.Fatalf("game: %+v", g)
		}
		files, err := st.GameFiles(g.ID)
		if err != nil || len(files) != 1 || files[0].Name != filepath.Base(path) || files[0].Role != "entry" {
			t.Fatalf("files: %+v %v", files, err)
		}
	}
}
