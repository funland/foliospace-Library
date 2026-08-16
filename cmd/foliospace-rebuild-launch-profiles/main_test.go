package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"

	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/launchprofile"
)

func TestBuildMAMEProfileUsesAuditedModel2Set(t *testing.T) {
	rom := []byte("model2-rom")
	path := filepath.Join(t.TempDir(), "vf2.zip")
	writeMAMETestArchive(t, path, map[string][]byte{"epr-test.ic1": rom})
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	machine := launchprofile.MAMEMachine{
		Name:     "vf2",
		Runnable: true,
		ROMs: []launchprofile.MAMEROM{{
			Name: "epr-test.ic1", Size: int64(len(rom)), CRC: fmt.Sprintf("%08x", crc32.ChecksumIEEE(rom)),
		}},
	}
	catalog := launchprofile.MAMECatalog{
		Build: "0.288 (mame0288)", SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Revision: 288, Machines: map[string]launchprofile.MAMEMachine{"vf2": machine},
	}
	entry := domain.GameAsset{
		ID: 42, Platform: "model2", FilePath: path, Size: info.Size(),
		SHA1: "0123456789abcdef0123456789abcdef01234567",
	}

	target := defaultLaunchProfileTargets("mame")[0]
	profile, err := buildMAMEProfile(catalog, machine, entry, map[string][]domain.GameAsset{"vf2": {entry}}, target, launchprofile.MAMEPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Policy != launchprofile.MAMEPolicy || profile.Runtime.ID != "mame" || profile.Runtime.Version != "0.288" || profile.Runtime.ContentSet != "mame-0.288" {
		t.Fatalf("unexpected runtime profile: %+v", profile)
	}
	if profile.CanonicalSet != "vf2" || profile.EntryFile != "vf2.zip" || len(profile.Files) != 1 || profile.Files[0].Role != "entry" {
		t.Fatalf("unexpected profile files: %+v", profile)
	}
}

func TestBuildMAMEProfileUsesExactMobileRuntime(t *testing.T) {
	rom := []byte("mobile-mame-rom")
	path := filepath.Join(t.TempDir(), "sf2.zip")
	writeMAMETestArchive(t, path, map[string][]byte{"sf2.rom": rom})
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	machine := launchprofile.MAMEMachine{
		Name: "sf2", Runnable: true,
		ROMs: []launchprofile.MAMEROM{{Name: "sf2.rom", Size: int64(len(rom)), CRC: fmt.Sprintf("%08x", crc32.ChecksumIEEE(rom))}},
	}
	catalog := launchprofile.MAMECatalog{
		Build: "0.287 (mame0287)", SHA256: "1123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Revision: 287, Machines: map[string]launchprofile.MAMEMachine{"sf2": machine},
	}
	entry := domain.GameAsset{ID: 43, Platform: "cps1", FilePath: path, Size: info.Size(), SHA1: "1123456789abcdef0123456789abcdef01234567"}
	target := launchProfileTarget{
		ID: "ipados", ClientName: "SpatialEMU.iPadOS", MinClientVersion: "1.300",
		ClientPlatform: "ipados-arm64", Architecture: "arm64",
	}
	policy := launchprofile.MAMEPolicyForVersion("0.287")
	profile, err := buildMAMEProfile(catalog, machine, entry, map[string][]domain.GameAsset{"sf2": {entry}}, target, policy)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Policy != "mame-0.287-listxml" || profile.ClientName != "SpatialEMU.iPadOS" || profile.ClientPlatform != "ipados-arm64" || profile.Architecture != "arm64" {
		t.Fatalf("unexpected mobile target: %+v", profile)
	}
	if profile.Runtime.ID != "mame" || profile.Runtime.Version != "0.287" || profile.Runtime.ContentSet != "mame-0.287" {
		t.Fatalf("unexpected mobile runtime: %+v", profile.Runtime)
	}
}

func TestBuildMAMEProfileAcceptsAuditedSelfContainedClone(t *testing.T) {
	cloneROM := []byte("clone-rom")
	sharedROM := []byte("shared-rom")
	biosROM := []byte("bios-rom")
	dir := t.TempDir()
	clonePath := filepath.Join(dir, "clone.zip")
	writeMAMETestArchive(t, clonePath, map[string][]byte{"clone.bin": cloneROM, "shared.bin": sharedROM})
	biosPath := filepath.Join(dir, "bios.zip")
	writeMAMETestArchive(t, biosPath, map[string][]byte{"bios.bin": biosROM})
	cloneInfo, _ := os.Stat(clonePath)
	biosInfo, _ := os.Stat(biosPath)
	parent := launchprofile.MAMEMachine{
		Name: "parent", Runnable: true, DeviceRefs: []string{"bios"},
		ROMs: []launchprofile.MAMEROM{
			{Name: "parent-region.bin", Size: 6, CRC: "00000001"},
			{Name: "shared.bin", Size: int64(len(sharedROM)), CRC: fmt.Sprintf("%08x", crc32.ChecksumIEEE(sharedROM))},
		},
	}
	clone := launchprofile.MAMEMachine{
		Name: "clone", CloneOf: "parent", ROMOf: "parent", Runnable: true,
		ROMs: []launchprofile.MAMEROM{
			{Name: "clone.bin", Size: int64(len(cloneROM)), CRC: fmt.Sprintf("%08x", crc32.ChecksumIEEE(cloneROM))},
			{Name: "shared.bin", Merge: "shared.bin", Size: int64(len(sharedROM)), CRC: fmt.Sprintf("%08x", crc32.ChecksumIEEE(sharedROM))},
		},
	}
	bios := launchprofile.MAMEMachine{
		Name: "bios", IsDevice: true, Runnable: false,
		ROMs: []launchprofile.MAMEROM{{Name: "bios.bin", Size: int64(len(biosROM)), CRC: fmt.Sprintf("%08x", crc32.ChecksumIEEE(biosROM))}},
	}
	catalog := launchprofile.MAMECatalog{
		Build: "0.288 (mame0288)", SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Revision: 288, Machines: map[string]launchprofile.MAMEMachine{"clone": clone, "parent": parent, "bios": bios},
	}
	entry := domain.GameAsset{ID: 44, Platform: "mame", FilePath: clonePath, Size: cloneInfo.Size(), SHA1: "2123456789abcdef0123456789abcdef01234567"}
	biosAsset := domain.GameAsset{ID: 45, Platform: "mame", FilePath: biosPath, Size: biosInfo.Size(), SHA1: "3123456789abcdef0123456789abcdef01234567"}
	profile, err := buildMAMEProfile(catalog, clone, entry, map[string][]domain.GameAsset{
		"clone": {entry}, "bios": {biosAsset},
	}, defaultLaunchProfileTargets("mame")[0], launchprofile.MAMEPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Files) != 2 || profile.Files[0].Name != "clone.zip" || profile.Files[1].Name != "bios.zip" {
		t.Fatalf("unexpected self-contained clone profile: %+v", profile.Files)
	}
}

func TestBuildMAMEProfileAcceptsAuditedTimeCrisisEmbeddedDeviceROM(t *testing.T) {
	gameROM := []byte("time-crisis-program")
	deviceROM := []byte("namco-c71-device")
	path := filepath.Join(t.TempDir(), "timecris.zip")
	writeMAMETestArchive(t, path, map[string][]byte{
		"ts2ver-b.1": gameROM,
		"c71.bin":    deviceROM,
	})
	machine := launchprofile.MAMEMachine{
		Name: "timecris", Runnable: true, DeviceRefs: []string{"namcoc71"},
		ROMs: []launchprofile.MAMEROM{{
			Name: "ts2verb.1", Size: int64(len(gameROM)), CRC: fmt.Sprintf("%08x", crc32.ChecksumIEEE(gameROM)),
		}},
	}
	device := launchprofile.MAMEMachine{
		Name: "namcoc71", IsDevice: true,
		ROMs: []launchprofile.MAMEROM{{
			Name: "c71.bin", Size: int64(len(deviceROM)), CRC: fmt.Sprintf("%08x", crc32.ChecksumIEEE(deviceROM)),
		}},
	}
	catalog := launchprofile.MAMECatalog{
		Build: "0.287 (mame0287)", SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Revision: 287, Machines: map[string]launchprofile.MAMEMachine{"timecris": machine, "namcoc71": device},
	}
	entry := domain.GameAsset{
		ID: 46, Platform: "mame", FilePath: path, Size: 16292369,
		SHA1: "ee6d57977bd5b10f82292009755cd8d80b9e14f5",
	}
	profile, err := buildMAMEProfile(catalog, machine, entry, map[string][]domain.GameAsset{
		"timecris": {entry},
	}, defaultLaunchProfileTargets("mame")[0], launchprofile.MAMEPolicyForVersion("0.287"))
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Files) != 1 || profile.Files[0].Name != "timecris.zip" || profile.Files[0].Role != "entry" {
		t.Fatalf("unexpected embedded-device profile: %+v", profile.Files)
	}

	entry.SHA1 = "0123456789abcdef0123456789abcdef01234567"
	if _, err := buildMAMEProfile(catalog, machine, entry, map[string][]domain.GameAsset{
		"timecris": {entry},
	}, defaultLaunchProfileTargets("mame")[0], launchprofile.MAMEPolicyForVersion("0.287")); err == nil {
		t.Fatal("expected an unregistered Time Crisis fingerprint to require namcoc71.zip")
	}
}

func TestLaunchProfileTargetsRequireCanonicalIdentityAndStableFBNeoIdentity(t *testing.T) {
	document := launchProfileTargetDocument{Targets: []launchProfileTarget{
		{
			ID: "iPadOS", ClientName: "SpatialEMU.iPadOS", MinClientVersion: "1.300",
			ClientPlatform: "ipados-arm64", Architecture: "arm64",
			CoreBuildID: "fbneo:archive-f1d54ccd94b661434a38930591e3697b89165a5946c45eff98f60d3981fd7b6c:ios-arm64:full-v1",
		},
		{
			ID: "visionOS", ClientName: "SpatialEMU.visionOS", MinClientVersion: "1.300",
			ClientPlatform: "visionos-arm64", Architecture: "arm64", CoreSHA256: "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		},
	}}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "targets.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	targets, err := loadLaunchProfileTargets(path, "fbneo")
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 || targets[0].ID != "ipados" || targets[0].CoreBuildID != document.Targets[0].CoreBuildID {
		t.Fatalf("targets=%+v", targets)
	}

	document.Targets[0].ClientPlatform = "ios-arm64"
	data, _ = json.Marshal(document)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadLaunchProfileTargets(path, "fbneo"); err == nil {
		t.Fatal("expected canonical identity mismatch")
	}
}

func TestApplyFBNeoTargetUsesExactStableCoreIdentity(t *testing.T) {
	catalog := launchprofile.FBNeoCatalog{Version: "v1", SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
	game := launchprofile.FBNeoGame{Name: "sf2"}
	target := launchProfileTarget{
		ID: "visionos", ClientName: "SpatialEMU.visionOS", MinClientVersion: "1.300",
		ClientPlatform: "visionos-arm64", Architecture: "arm64",
		CoreBuildID: "fbneo:archive-a161e273b161dc77fad5acc449798987f89741f0f75da1f05bec4ff7b6b75181:xros-arm64:full-localized-v1",
		CoreSHA256:  "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
	}
	profile := applyFBNeoTarget(domain.GameLaunchProfile{GameID: 1}, target, catalog, game)
	if profile.ClientName != target.ClientName || profile.Runtime.CoreBuildID != target.CoreBuildID || profile.Runtime.CoreSHA256 != target.CoreSHA256 || profile.Runtime.CoreID != "fbneo" {
		t.Fatalf("profile=%+v", profile)
	}
}

func writeMAMETestArchive(t *testing.T, path string, files map[string][]byte) {
	t.Helper()
	archive, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(archive)
	for name, data := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
}
