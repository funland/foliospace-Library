package scanner

import (
	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/store"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestGameRescanPreservesManualMetadataAndPrivateState(t *testing.T) {
	root := t.TempDir()
	rom := filepath.Join(root, "SNES", "sample.sfc")
	if err := os.MkdirAll(filepath.Dir(rom), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rom, []byte("test cartridge"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(rom), "gamelist.xml"), []byte(`<gameList><game><path>sample.sfc</path><name>Sidecar title</name><desc>Sidecar summary</desc></game></gameList>`), 0644); err != nil {
		t.Fatal(err)
	}
	c, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	st := store.New(c)
	lib, err := st.CreateLibraryWithType("Games", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	scan := New(st)
	if j, e := scan.ScanLibrary(lib); e != nil || j.ErrorCount != 0 {
		t.Fatalf("%+v %v", j, e)
	}
	g, err := st.GameByPath(rom)
	if err != nil {
		t.Fatal(err)
	}
	if err = st.UpsertManualGameMetadata(domain.GameMetadata{GameID: g.ID, DisplayTitle: "My title", Summary: "", Publishers: []string{"My publisher"}}); err != nil {
		t.Fatal(err)
	}
	if err = st.UpdateGamePrivateStateForProfile(g.ID, 1, domain.GamePrivateState{Favorite: true, Liked: true}); err != nil {
		t.Fatal(err)
	}
	before, _ := st.GameDetails(g.ID)
	// A changed source forces reindexing and sidecar import, not the skip path.
	if err = os.WriteFile(rom, []byte("changed test cartridge"), 0644); err != nil {
		t.Fatal(err)
	}
	if j, e := scan.ScanLibrary(lib); e != nil || j.ErrorCount != 0 || j.IndexedFiles != 1 {
		t.Fatalf("%+v %v", j, e)
	}
	after, _ := st.GameDetails(g.ID)
	if !reflect.DeepEqual(before.Metadata, after.Metadata) || !after.Game.Favorite || !after.Game.Liked || after.Game.ID != before.Game.ID {
		t.Fatalf("manual data changed: %+v", after)
	}
	if err = scan.applyPegasusMetadata(g.ID, pegasusGame{Name: "Overwrite", Description: "not manual"}, true); err != nil {
		t.Fatal(err)
	}
	after, _ = st.GameDetails(g.ID)
	if !reflect.DeepEqual(before.Metadata, after.Metadata) {
		t.Fatal("Pegasus overwrote manual metadata")
	}
}
