package service

import (
	"fmt"
	"path/filepath"
	"testing"

	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/store"
)

func TestGameRoleNormalizationKeepsStablePagesAndDependencyRoles(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	library, err := st.CreateLibrary("Games", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var last domain.GameAsset
	for index := 0; index < 201; index++ {
		name := fmt.Sprintf("game%03d.zip", index)
		if index == 0 {
			name = "neogeo.zip"
		}
		last, err = st.UpsertGame(domain.GameAsset{LibraryID: library.ID, Title: fmt.Sprintf("Game %03d", index), Platform: "mame", FilePath: filepath.Join(library.RootPath, name), Format: "zip", CatalogRole: "game"})
		if err != nil {
			t.Fatal(err)
		}
	}
	dependency, err := st.UpsertGame(domain.GameAsset{LibraryID: library.ID, Title: "Existing dependency", Platform: "mame", FilePath: filepath.Join(library.RootPath, "device.zip"), CatalogRole: "dependency"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReplaceGameLaunchProfiles("legacy", []domain.GameLaunchProfile{{GameID: dependency.ID, ID: "legacy", Status: "ready"}}, []domain.GameLaunchCatalogUpdate{{GameID: dependency.ID, Platform: "mame", CatalogRole: "dependency"}}); err != nil {
		t.Fatal(err)
	}
	svc := &Service{store: st}
	processed, changed, err := svc.normalizeGameCatalogRoles()
	if err != nil {
		t.Fatal(err)
	}
	if processed != 202 || changed != 201 {
		t.Fatalf("normalization skipped records after hiding a dependency: processed=%d changed=%d", processed, changed)
	}
	updated, err := st.GameByID(last.ID)
	if err != nil || updated.CatalogRole != "needs-curation" {
		t.Fatalf("last page was skipped: %+v %v", updated, err)
	}
	updated, err = st.GameByID(dependency.ID)
	if err != nil || updated.CatalogRole != "dependency" {
		t.Fatalf("legacy profile promoted a dependency: %+v %v", updated, err)
	}
}
