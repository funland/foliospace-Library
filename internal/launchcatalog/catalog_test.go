package launchcatalog

import (
	"testing"

	"foliospace-reader/internal/domain"
)

func TestAuditedSF2OverridesCandidateMetadata(t *testing.T) {
	game := domain.GameAsset{
		FilePath:     "/games/FBNeo/arcade/sf2.zip",
		Size:         3551819,
		SHA1:         "bd59872a57f14dc492e2fb387727a9402f3d4f97",
		Platform:     "arcade",
		ROMSetName:   "sf2",
		EmulatorHint: "mame",
		CatalogRole:  RoleNeedsCuration,
	}
	game = CanonicalizeAuditedGame(game)
	if game.Platform != "cps1" || game.ROMSetName != "sf2" || game.EmulatorHint != "fbneo" || CatalogRole(game, nil) != RoleGame {
		t.Fatalf("game = %#v, want audited CPS1/FBNeo entry", game)
	}
}

func TestAuditedPointBlankRoutesToFBNeoAndHidesNamcoC75(t *testing.T) {
	for _, test := range []struct {
		name string
		size int64
		sha1 string
	}{
		{name: "ptblank", size: 5033400, sha1: "15f9dd6ccf009bffcb156b234bdeadbe26344314"},
		{name: "ptblanka", size: 131248, sha1: "ee3e54a9f49bfc7c27f3e0c6ad580bf78d04d1e2"},
	} {
		game := CanonicalizeAuditedGame(domain.GameAsset{
			FilePath: "/games/MAME/" + test.name + ".zip", Size: test.size, SHA1: test.sha1,
			Platform: "arcade", ROMSetName: test.name, EmulatorHint: "mame", CatalogRole: RoleNeedsCuration,
		})
		if game.Platform != "arcade" || game.ROMSetName != test.name || game.EmulatorHint != "fbneo" || CatalogRole(game, nil) != RoleGame {
			t.Fatalf("%s = %#v, want audited Arcade/FBNeo entry", test.name, game)
		}
	}
	device := domain.GameAsset{FilePath: "/games/MAME/namcoc75.zip", Platform: "arcade", CatalogRole: RoleGame}
	if got := CatalogRole(device, nil); got != RoleDependency {
		t.Fatalf("namcoc75 role = %q, want %q", got, RoleDependency)
	}
}

func TestExplicitNeedsCurationSurvivesForNonArcadeContent(t *testing.T) {
	game := domain.GameAsset{Platform: "unknown", CatalogRole: RoleNeedsCuration}
	if got := CatalogRole(game, nil); got != RoleNeedsCuration {
		t.Fatalf("CatalogRole = %q, want %q", got, RoleNeedsCuration)
	}
}

func TestCapcomAudioAndZNBIOSFilesAreDependencies(t *testing.T) {
	for _, path := range []string{
		"/games/NAOMI/awbios.zip",
		"/games/Capcom BIOS/qsound.zip",
		"/games/Capcom BIOS/qsound_hle.zip",
		"/games/Capcom BIOS/dl-1425.bin",
		"/games/Capcom BIOS/coh1000c.zip",
		"/games/Capcom BIOS/coh3002c.zip",
	} {
		game := domain.GameAsset{FilePath: path, Platform: "mame", CatalogRole: RoleNeedsCuration}
		if got := CatalogRole(game, nil); got != RoleDependency {
			t.Fatalf("CatalogRole(%q) = %q, want %q", path, got, RoleDependency)
		}
	}
}

func TestAtomiswaveSetsRequireSharedBIOS(t *testing.T) {
	for _, path := range []string{
		"/games/NAOMI/kofxi.zip",
		"/games/NAOMI/rumblef2.zip",
		"/games/NAOMI/samsptk.zip",
	} {
		if !RequiresAtomiswaveBIOS(domain.GameAsset{Platform: "naomi", FilePath: path}) {
			t.Fatalf("RequiresAtomiswaveBIOS(%q) = false", path)
		}
	}
	if RequiresAtomiswaveBIOS(domain.GameAsset{Platform: "naomi", FilePath: "/games/NAOMI/ikaruga.zip"}) {
		t.Fatal("ordinary NAOMI game unexpectedly requires awbios.zip")
	}
	if RequiresAtomiswaveBIOS(domain.GameAsset{Platform: "dreamcast", FilePath: "/games/DC/kofxi.zip"}) {
		t.Fatal("non-NAOMI platform unexpectedly requires awbios.zip")
	}
}

func TestProjectJusticeRevAExposesParentROMSet(t *testing.T) {
	game := domain.GameAsset{Platform: "naomi", ROMSetName: "pjustica"}
	if got := ParentROMSetName(game); got != "pjustic" {
		t.Fatalf("ParentROMSetName = %q, want pjustic", got)
	}
	if got := ParentROMSetName(domain.GameAsset{Platform: "naomi", ROMSetName: "pjustic"}); got != "" {
		t.Fatalf("parent set ParentROMSetName = %q, want empty", got)
	}
}

func TestNaomi2SplitCartridgeClonesExposeParentROMSet(t *testing.T) {
	tests := map[string]string{
		"clubkrto":  "clubkrt",
		"clubkrta":  "clubkrt",
		"clubkrtc":  "clubkrt",
		"kingrt66p": "kingrt66",
		"vstrik3co": "vstrik3c",
	}
	for clone, parent := range tests {
		game := domain.GameAsset{Platform: "naomi2", ROMSetName: clone}
		if got := ParentROMSetName(game); got != parent {
			t.Errorf("ParentROMSetName(%q) = %q, want %q", clone, got, parent)
		}
	}
	if got := ParentROMSetName(domain.GameAsset{Platform: "naomi2", ROMSetName: "vf4"}); got != "" {
		t.Fatalf("independent NAOMI 2 set ParentROMSetName = %q, want empty", got)
	}
}
