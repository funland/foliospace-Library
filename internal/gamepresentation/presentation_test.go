package gamepresentation

import (
	"reflect"
	"testing"

	"foliospace-reader/internal/domain"
)

func TestDiscInfoGroupsOnlyMatchingEdition(t *testing.T) {
	for _, title := range []string{"BrainDead 13", "Creature Shock", "D no Shokutaku"} {
		a := domain.GameAsset{ID: 10, LibraryID: 1, Platform: "3do", RelPath: title + " (Japan) (Disc 1).chd", Favorite: true}
		b := a
		b.ID, b.RelPath, b.Favorite = 11, title+" (Japan) (Disc 2).chd", false
		before := a
		first, second := DiscInfo(a), DiscInfo(b)
		if first == nil || second == nil || first.GroupKey != second.GroupKey || first.DiscNumber != 1 || second.DiscNumber != 2 || first.Title != title+" (Japan)" {
			t.Fatalf("invalid disc hints: %+v %+v", first, second)
		}
		if !reflect.DeepEqual(a, before) {
			t.Fatal("presentation changed source asset")
		}
		for _, variant := range []string{" (Japan) (Pilot)", " (Japan) (Demo)", " (USA)", " (Japan) (Rev 1)", " (Japan) (En,Ja)"} {
			b.RelPath = title + variant + " (Disc 2).chd"
			if hint := DiscInfo(b); hint == nil || hint.GroupKey == first.GroupKey {
				t.Fatalf("edition merged: %s %+v", b.RelPath, hint)
			}
		}
	}
}

func TestDiscInfoPreservesSourceBoundariesAndIgnoresChecksums(t *testing.T) {
	base := domain.GameAsset{LibraryID: 1, Platform: "3do", RelPath: "games/D (Japan) (Disc 1).chd", SHA1: "same"}
	want := DiscInfo(base)
	for _, changed := range []domain.GameAsset{
		{LibraryID: 2, Platform: "3do", RelPath: base.RelPath},
		{LibraryID: 1, Platform: "saturn", RelPath: base.RelPath},
		{LibraryID: 1, Platform: "3do", RelPath: "copies/D (Japan) (Disc 1).chd"},
		{LibraryID: 1, Platform: "3do", RelPath: "games/D (Japan) (Disc 2).iso"},
		{LibraryID: 1, Platform: "3do", RelPath: base.RelPath, Region: "Europe"},
	} {
		changed.SHA1 = "same"
		if hint := DiscInfo(changed); hint == nil || hint.GroupKey == want.GroupKey {
			t.Fatalf("source boundary merged: %+v", changed)
		}
	}
	base.SHA1 = "different"
	if DiscInfo(base).GroupKey != want.GroupKey {
		t.Fatal("checksum must not define presentation grouping")
	}
}

func TestDiscInfoRequiresExplicitUnambiguousDiscMedia(t *testing.T) {
	for _, filename := range []string{"D 1.chd", "D CD.chd", "D (Disc 0).chd", "D (Disc 1) (Disk 2).chd", "D (Disc 1).m3u", "D (Disc 1).zip", "D (Disc 1).bin"} {
		if info := DiscInfo(domain.GameAsset{Platform: "3do", RelPath: filename}); info != nil {
			t.Fatalf("unexpected group for %s: %+v", filename, info)
		}
	}
	for _, filename := range []string{"D (Disc 1).chd", "D [Disk 01].chd", "D - CD 1.iso", "D_disc1.cue", "D (Disc 1 of 2).chd"} {
		if info := DiscInfo(domain.GameAsset{Platform: "3do", RelPath: filename}); info == nil || info.Title != "D" || info.DiscNumber != 1 {
			t.Fatalf("missing group for %s: %+v", filename, info)
		}
	}
	for _, game := range []domain.GameAsset{{Platform: "pc98", RelPath: "D (Disc 1).iso"}, {Platform: "pcfx", RelPath: "D (Disc 1).chd"}, {Platform: "3do", CatalogRole: "dependency", RelPath: "D (Disc 1).chd"}, {Platform: "3do", Title: "D (Disc 1)"}} {
		if DiscInfo(game) != nil {
			t.Fatalf("existing aggregation or unproven filename: %+v", game)
		}
	}
}
