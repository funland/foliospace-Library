package launchcatalog

import (
	"path/filepath"
	"strings"

	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/naomi2catalog"
)

const (
	RoleGame          = "game"
	RoleDependency    = "dependency"
	RoleNeedsCuration = "needs-curation"
)

type auditedEntry struct {
	name         string
	size         int64
	sha1         string
	platform     string
	romSetName   string
	emulatorHint string
}

// These entry fingerprints are the assets backed by an exact runtime profile.
// Keep this list aligned with service.auditedGameLaunchProfiles.
var auditedEntries = []auditedEntry{
	{name: "vstriker.zip", size: 10313686, sha1: "8e3518318eeb157ab299b2f284faef176d3f49dd", platform: "model2", romSetName: "vstriker", emulatorHint: "model2"},
	{name: "tektagtac1.zip", size: 120980600, sha1: "d6615a3a70ea9941b61ccd608054a0044d3d6ab3", platform: "arcade", romSetName: "tektagtc1a", emulatorHint: "mame"},
	{name: "sf2.zip", size: 3551819, sha1: "bd59872a57f14dc492e2fb387727a9402f3d4f97", platform: "cps1", romSetName: "sf2", emulatorHint: "fbneo"},
	{name: "sfa.zip", size: 7365582, sha1: "61dece364b8d2f2ff15391505168be334ebb371a", platform: "cps2", romSetName: "sfa", emulatorHint: "fbneo"},
	{name: "sfiii.zip", size: 38868517, sha1: "7aae0cfc4ef8911f19d2e986cee63807deebf1b6", platform: "cps3", romSetName: "sfiii", emulatorHint: "fbneo"},
	{name: "ptblank.zip", size: 5033400, sha1: "15f9dd6ccf009bffcb156b234bdeadbe26344314", platform: "arcade", romSetName: "ptblank", emulatorHint: "fbneo"},
	{name: "ptblanka.zip", size: 131248, sha1: "ee3e54a9f49bfc7c27f3e0c6ad580bf78d04d1e2", platform: "arcade", romSetName: "ptblanka", emulatorHint: "fbneo"},
	{name: "hypreact.zip", size: 8052342, sha1: "e0940f848884c9d53bbc41bb947d584e06cc1845", platform: "mame", romSetName: "hypreact", emulatorHint: "mame"},
	{name: "hypreac2.zip", size: 18291541, sha1: "7fe73cc7ee40a49225a4616106e538c084ef4364", platform: "mame", romSetName: "hypreac2", emulatorHint: "mame"},
	{name: "srmp4.zip", size: 7697767, sha1: "cfcf2cdf61ebca862a84473a8bf75fbe8d76cb7b", platform: "mame", romSetName: "srmp4", emulatorHint: "mame"},
	{name: "fromancr.zip", size: 14121810, sha1: "137e4949d7e204ff10e33372528cc1e9481b962c", platform: "mame", romSetName: "fromancr", emulatorHint: "mame"},
	{name: "fromanc4.zip", size: 21443327, sha1: "ff478f3350d9703e8647f659ce169ee234082249", platform: "mame", romSetName: "fromanc4", emulatorHint: "mame"},
	{name: "mcnpshnt.zip", size: 1205007, sha1: "24a714371a867db1709798a95a171778e0940021", platform: "mame", romSetName: "mcnpshnt", emulatorHint: "mame"},
}

func IsStrictArcadePlatform(platform string) bool {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "arcade", "mame", "model2", "cps", "cps1", "cps2", "cps3", "neogeo":
		return true
	default:
		return false
	}
}

func IsAuditedEntry(game domain.GameAsset) bool {
	_, ok := auditedEntryFor(game)
	return ok
}

func CanonicalizeAuditedGame(game domain.GameAsset) domain.GameAsset {
	entry, ok := auditedEntryFor(game)
	if !ok {
		return game
	}
	game.Platform = entry.platform
	game.ROMSetName = entry.romSetName
	game.EmulatorHint = entry.emulatorHint
	return game
}

func auditedEntryFor(game domain.GameAsset) (auditedEntry, bool) {
	name := strings.ToLower(filepath.Base(strings.TrimSpace(game.FilePath)))
	sha1 := strings.ToLower(strings.TrimSpace(game.SHA1))
	for _, entry := range auditedEntries {
		if name == entry.name && game.Size == entry.size && sha1 == entry.sha1 {
			return entry, true
		}
	}
	return auditedEntry{}, false
}

func IsAuditedEntryIdentity(name string, size int64, sha1 string) bool {
	return IsAuditedEntry(domain.GameAsset{FilePath: name, Size: size, SHA1: sha1})
}

func IsDOSReady(launch domain.DOSLaunch) bool {
	source := strings.ToLower(strings.TrimSpace(launch.EntrySource))
	if source != "curated" && source != "dosboxconfig" {
		return false
	}
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(launch.EntryFile))) {
	case ".bat", ".com", ".exe":
		return true
	default:
		return false
	}
}

func CatalogRole(game domain.GameAsset, dosLaunch *domain.DOSLaunch) string {
	role := strings.ToLower(strings.TrimSpace(game.CatalogRole))
	if role == RoleDependency || IsKnownDependencyFile(game.FilePath) {
		return RoleDependency
	}
	platform := strings.ToLower(strings.TrimSpace(game.Platform))
	if platform == "dos" {
		if dosLaunch != nil && IsDOSReady(*dosLaunch) {
			return RoleGame
		}
		return RoleNeedsCuration
	}
	if IsStrictArcadePlatform(platform) {
		if IsAuditedEntry(game) {
			return RoleGame
		}
		return RoleNeedsCuration
	}
	if role == RoleNeedsCuration {
		return RoleNeedsCuration
	}
	return RoleGame
}

func IsKnownDependencyFile(path string) bool {
	switch strings.ToLower(filepath.Base(strings.TrimSpace(path))) {
	case "neogeo.zip", "awbios.zip", "segabill.zip", "namcoc75.zip", "ym2413_instruments.zip",
		"qsound.zip", "qsound_hle.zip", "dl-1425.bin",
		"coh1000c.zip", "coh3002c.zip":
		return true
	default:
		return false
	}
}

var atomiswaveROMSetNames = map[string]struct{}{
	"anmlbskt": {}, "anmlbskta": {}, "basschal": {}, "basschalo": {},
	"blokpong": {}, "claychal": {}, "demofist": {}, "dirtypig": {},
	"dolphin": {}, "fotns": {}, "ftspeed": {}, "ggisuka": {}, "ggx15": {},
	"kofnw": {}, "kofnwj": {}, "kofxi": {}, "kov7sprt": {}, "maxspeed": {},
	"mslug6": {}, "ngbc": {}, "ngbcj": {}, "rangrmsn": {}, "rumblef": {},
	"rumblef2": {}, "rumblefp": {}, "rumblf2p": {}, "salmankt": {},
	"samsptk": {}, "sprtshot": {}, "sushibar": {}, "vfurlong": {},
	"waidrive": {}, "xtrmhnt2": {}, "xtrmhunt": {},
}

// RequiresAtomiswaveBIOS identifies the Atomiswave sets that currently share
// the NAOMI catalog and therefore require awbios.zip at launch time.
func RequiresAtomiswaveBIOS(game domain.GameAsset) bool {
	if !strings.EqualFold(strings.TrimSpace(game.Platform), "naomi") {
		return false
	}
	name := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(filepath.Base(game.FilePath), filepath.Ext(game.FilePath))))
	_, ok := atomiswaveROMSetNames[name]
	return ok
}

// ParentROMSetName exposes parent/clone relationships that are part of the
// runtime ROM-set identity but do not need a separate database column.
func ParentROMSetName(game domain.GameAsset) string {
	platform := strings.ToLower(strings.TrimSpace(game.Platform))
	romSetName := strings.ToLower(strings.TrimSpace(game.ROMSetName))
	if platform == "naomi" && romSetName == "pjustica" {
		return "pjustic"
	}
	if platform == "naomi2" {
		return naomi2catalog.Parent(romSetName)
	}
	return ""
}
