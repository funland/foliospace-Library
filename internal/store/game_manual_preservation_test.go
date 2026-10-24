package store

import (
	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"reflect"
	"testing"
)

func TestImportedMetadataCannotOverwriteManualEditsOrDeliberateBlanks(t *testing.T) {
	c, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s := New(c)
	l, _ := s.CreateLibrary("Games", "/games")
	g, err := s.UpsertGame(domain.GameAsset{LibraryID: l.ID, Title: "Game", Platform: "arcade", FilePath: "/games/a.zip", ROMSetName: "My set", EmulatorHint: "My emulator"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.UpsertImportedGameMetadata(domain.GameMetadata{GameID: g.ID, DisplayTitle: "Sidecar"}); err != nil {
		t.Fatal(err)
	}
	original := domain.GameMetadata{GameID: g.ID, DisplayTitle: "Manual title", Summary: "", Rating: 4, Genres: []string{"Manual genre"}, ExternalLinks: []string{"https://example.test/kept"}}
	if err = s.UpsertManualGameMetadata(original); err != nil {
		t.Fatal(err)
	}
	// A scanner reindex must also retain curated catalog identity for manually edited games.
	changed := g
	changed.Title = "Scanner title"
	changed.ROMSetName = "Scanner set"
	changed.EmulatorHint = "Scanner emulator"
	changed.Platform = "cps1"
	if _, err = s.UpsertGame(changed); err != nil {
		t.Fatal(err)
	}
	keptScan, _ := s.GameByID(g.ID)
	if keptScan.Title != g.Title || keptScan.Platform != g.Platform || keptScan.ROMSetName != g.ROMSetName || keptScan.EmulatorHint != g.EmulatorHint {
		t.Fatalf("scanner changed curated identity: %+v", keptScan)
	}
	before, _ := s.GameDetails(g.ID)
	if err = s.UpsertImportedGameMetadata(domain.GameMetadata{GameID: g.ID, DisplayTitle: "Overwrite", Summary: "Fill deliberate blank", Publishers: []string{"Automatic"}}); err != nil {
		t.Fatal(err)
	}
	after, _ := s.GameDetails(g.ID)
	if !reflect.DeepEqual(before.Metadata, after.Metadata) || !reflect.DeepEqual(before.Sources, after.Sources) {
		t.Fatalf("manual data changed: before=%+v after=%+v", before, after)
	}
	if _, err = s.ReplaceAuditedGameLaunchProfiles("test-policy", g.ID, nil, []domain.GameLaunchCatalogUpdate{{GameID: g.ID, Platform: "cps1", ROMSetName: "a", EmulatorHint: "fbneo", CatalogRole: "needs-curation"}}); err != nil {
		t.Fatal(err)
	}
	kept, _ := s.GameByID(g.ID)
	if kept.Platform != g.Platform || kept.ROMSetName != g.ROMSetName || kept.EmulatorHint != g.EmulatorHint || kept.Title != g.Title {
		t.Fatalf("catalog identity rewritten: %+v", kept)
	}
}
