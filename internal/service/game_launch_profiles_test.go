package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/store"
)

func TestAuditedGameLaunchProfilesAreValid(t *testing.T) {
	if err := validateAuditedGameLaunchProfiles(); err != nil {
		t.Fatal(err)
	}
}

func TestKonamiPython1RuntimeRequiresReliquaryContentSetOnDesktop(t *testing.T) {
	game := domain.GameAsset{Platform: "konami-python1", CatalogRole: "game"}
	base := domain.GameLaunchResolveRequest{
		Client:   domain.GameLaunchClient{Name: "SpatialEMU.Windows", Version: "1.306", Platform: "windows-x64", Architecture: "x64"},
		Runtimes: []domain.GameRuntimeDescriptor{{ID: "pcsx2-reliquary", Version: "1.5.1.0", ContentSet: "konami-python1"}},
	}
	if runtime, ok := matchingPragmaticRuntime(game, base); !ok || runtime.ID != "pcsx2-reliquary" {
		t.Fatalf("runtime = %#v ok=%v, want Reliquary", runtime, ok)
	}
	wrongRuntime := base
	wrongRuntime.Runtimes = []domain.GameRuntimeDescriptor{{ID: "pcsx2", Version: "2.6.3"}}
	if _, ok := matchingPragmaticRuntime(game, wrongRuntime); ok {
		t.Fatal("ordinary PCSX2 was accepted for Konami Python 1")
	}
	wrongContent := base
	wrongContent.Runtimes = []domain.GameRuntimeDescriptor{{ID: "pcsx2-reliquary", Version: "1.5.1.0", ContentSet: "ps2"}}
	if _, ok := matchingPragmaticRuntime(game, wrongContent); ok {
		t.Fatal("wrong Reliquary content set was accepted")
	}
	mobile := base
	mobile.Client = domain.GameLaunchClient{Name: "SpatialEMU.iPadOS", Version: "1.306", Platform: "ipados-arm64", Architecture: "arm64"}
	if _, ok := matchingPragmaticRuntime(game, mobile); ok {
		t.Fatal("mobile client was accepted for desktop-only Reliquary runtime")
	}
}

func TestLogicalLaunchNamesRejectUnsafeAndCollidingPaths(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../rom.zip", `folder\\rom.zip`, `/rom.zip`, `C:\\rom.zip`, "bad\x00name.zip"} {
		if validLogicalLaunchName(name) {
			t.Fatalf("validLogicalLaunchName(%q) = true, want false", name)
		}
	}
	if !validLogicalLaunchName("tektagtc1a.zip") {
		t.Fatal("expected audited ROM alias to be valid")
	}
}

func TestValidatePragmaticManifestRequiresNaomi2SplitParentClosure(t *testing.T) {
	game := domain.GameAsset{
		Platform: "naomi2", ROMSetName: "clubkrto", Format: "zip", Size: 200,
	}
	entry := domain.GameFile{
		Name: "clubkrto.zip", FilePath: "/games/clubkrto.zip", Size: 100,
		Role: "entry", Position: 0,
	}
	parent := domain.GameFile{
		Name: "clubkrt.zip", FilePath: "/games/clubkrt.zip", Size: 100,
		Role: "dependency", Position: 1,
	}

	if _, err := validatePragmaticManifest(game, []domain.GameFile{entry}); err == nil || !strings.Contains(err.Error(), "clubkrt.zip") {
		t.Fatalf("missing parent error = %v, want exact parent name", err)
	}
	wrongParent := parent
	wrongParent.Name = "vstrik3c.zip"
	if _, err := validatePragmaticManifest(game, []domain.GameFile{entry, wrongParent}); err == nil || !strings.Contains(err.Error(), "unexpected launch file") {
		t.Fatalf("wrong parent error = %v, want rejected closure", err)
	}
	if entryFile, err := validatePragmaticManifest(game, []domain.GameFile{entry, parent}); err != nil || entryFile != "clubkrto.zip" {
		t.Fatalf("complete closure entry=%q err=%v", entryFile, err)
	}
}

func TestValidateGameLaunchResolveRequestRejectsInvalidCoreHash(t *testing.T) {
	req := domain.GameLaunchResolveRequest{
		Client:   domain.GameLaunchClient{Name: "SpatialEMU.Windows", Version: "1.302", Platform: "windows-x64", Architecture: "x64"},
		Runtimes: []domain.GameRuntimeDescriptor{{ID: "libretro", CoreID: "fbneo", CoreSHA256: "ABC"}},
	}
	if err := ValidateGameLaunchResolveRequest(req); err == nil {
		t.Fatal("expected invalid core hash to be rejected")
	}
	req.Runtimes[0].CoreSHA256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	req.Runtimes[0].CoreBuildID = "fbneo:4f7c3a1:patch-v2:arm64:release"
	if err := ValidateGameLaunchResolveRequest(req); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	req.Runtimes[0].CoreBuildID = strings.Repeat("x", 257)
	if err := ValidateGameLaunchResolveRequest(req); err == nil {
		t.Fatal("expected an oversized core build id to be rejected")
	}
}

func TestRuntimeIdentityPrefersStableBuildIDWithLegacyHashFallback(t *testing.T) {
	legacyHash := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	otherHash := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	approved := domain.GameRuntimeDescriptor{CoreBuildID: "fbneo:4f7c3a1:arm64:release", CoreSHA256: legacyHash}

	if !runtimeIdentityMatches(domain.GameRuntimeDescriptor{CoreBuildID: approved.CoreBuildID, CoreSHA256: otherHash}, approved) {
		t.Fatal("matching stable build ids should take precedence over legacy hashes")
	}
	if runtimeIdentityMatches(domain.GameRuntimeDescriptor{CoreBuildID: "fbneo:other:arm64:release", CoreSHA256: legacyHash}, approved) {
		t.Fatal("different stable build ids must not match even when legacy hashes match")
	}
	if !runtimeIdentityMatches(domain.GameRuntimeDescriptor{CoreSHA256: legacyHash}, domain.GameRuntimeDescriptor{CoreSHA256: legacyHash}) {
		t.Fatal("legacy clients and profiles should continue matching by core hash")
	}
	if runtimeIdentityMatches(domain.GameRuntimeDescriptor{}, domain.GameRuntimeDescriptor{CoreBuildID: approved.CoreBuildID}) {
		t.Fatal("a build-id-only policy must not accept an omitted runtime identity through an empty hash fallback")
	}
}

func TestAppleMobileOrdinaryLibretroDoesNotRequireCoreFingerprint(t *testing.T) {
	client := domain.GameLaunchClient{Name: "SpatialEMU.iPadOS", Version: "1.40", Platform: "ipados-arm64", Architecture: "arm64"}
	if !pragmaticRuntimeAllowedForClient(domain.GameRuntimeDescriptor{ID: "libretro", CoreID: "nestopia"}, "nes", client) {
		t.Fatal("ordinary console cores should not require an application-build fingerprint")
	}
	if pragmaticRuntimeAllowedForClient(domain.GameRuntimeDescriptor{ID: "libretro", CoreID: "fbneo"}, "cps1", client) {
		t.Fatal("FBNeo must retain strict runtime identity checks")
	}
	if !pragmaticRuntimeAllowedForClient(domain.GameRuntimeDescriptor{ID: "libretro", CoreID: "fbneo", CoreBuildID: "fbneo:4f7c3a1:arm64:release"}, "cps1", client) {
		t.Fatal("FBNeo should accept a stable core build id")
	}
}

func TestLaunchProfileClientVersionFloor(t *testing.T) {
	if versionAtLeast("1.301", "1.302") {
		t.Fatal("1.301 must not match a 1.302 profile")
	}
	if !versionAtLeast("1.302", "1.302") || !versionAtLeast("1.303", "1.302") {
		t.Fatal("1.302 and later should match the profile floor")
	}
}

func TestSFCSupportsWindowsBSNES(t *testing.T) {
	runtime := domain.GameRuntimeDescriptor{ID: "libretro", CoreID: "bsnes"}
	if !pragmaticRuntimeSupportsPlatform(runtime, "snes") {
		t.Fatal("expected SFC/SNES to accept the Windows bsnes core")
	}
}

func Test3DOAndNDSRuntimeSelectionIsExact(t *testing.T) {
	threeDO := domain.GameAsset{Platform: "3do", CatalogRole: "game"}
	windows := domain.GameLaunchResolveRequest{
		Client:   domain.GameLaunchClient{Name: "SpatialEMU.Windows", Version: "1.302", Platform: "windows-x64", Architecture: "x64"},
		Runtimes: []domain.GameRuntimeDescriptor{{ID: "libretro", CoreID: "opera"}},
	}
	if runtime, ok := matchingPragmaticRuntime(threeDO, windows); !ok || normalizeLaunchCoreID(runtime.CoreID) != "opera" {
		t.Fatalf("3DO runtime = %#v ok=%v, want Opera", runtime, ok)
	}
	windows.Runtimes[0].CoreID = "generic-disc"
	if _, ok := matchingPragmaticRuntime(threeDO, windows); ok {
		t.Fatal("3DO accepted a non-Opera core")
	}

	nds := domain.GameAsset{Platform: "nds", CatalogRole: "game"}
	ipad := domain.GameLaunchResolveRequest{
		Client:   domain.GameLaunchClient{Name: "SpatialEMU.iPadOS", Version: "1.40", Platform: "ipados-arm64", Architecture: "arm64"},
		Runtimes: []domain.GameRuntimeDescriptor{{ID: "libretro", CoreID: "melonds-ds"}},
	}
	if runtime, ok := matchingPragmaticRuntime(nds, ipad); !ok || normalizeLaunchCoreID(runtime.CoreID) != "melondsds" {
		t.Fatalf("NDS runtime = %#v ok=%v, want melonDS DS", runtime, ok)
	}
	ipad.Runtimes[0].CoreID = "melonds"
	if _, ok := matchingPragmaticRuntime(nds, ipad); ok {
		t.Fatal("NDS accepted desktop melonDS instead of melonds-ds")
	}
	windows.Runtimes[0].CoreID = "melonds-ds"
	if _, ok := matchingPragmaticRuntime(nds, windows); ok {
		t.Fatal("NDS mobile profile was exposed to Windows")
	}
	tv := ipad
	tv.Client = domain.GameLaunchClient{Name: "SpatialEMU.tvOS", Version: "1.40", Platform: "tvos-arm64", Architecture: "arm64"}
	if _, ok := matchingPragmaticRuntime(nds, tv); ok {
		t.Fatal("NDS mobile profile was exposed to tvOS")
	}
}

func TestSupportedPragmaticClientPlatforms(t *testing.T) {
	tests := []struct {
		name      string
		client    domain.GameLaunchClient
		supported bool
	}{
		{name: "windows", client: domain.GameLaunchClient{Name: "SpatialEMU.Windows", Version: "1.302", Platform: "windows-x64", Architecture: "x64"}, supported: true},
		{name: "macOS arm64", client: domain.GameLaunchClient{Name: "SpatialEMU.macOS", Version: "1.40", Platform: "macos-arm64", Architecture: "arm64"}, supported: true},
		{name: "iPhone", client: domain.GameLaunchClient{Name: "SpatialEMU.iOS", Version: "1.40", Platform: "ios-arm64", Architecture: "arm64"}, supported: true},
		{name: "iPad", client: domain.GameLaunchClient{Name: "SpatialEMU.iPadOS", Version: "1.40", Platform: "ipados-arm64", Architecture: "arm64"}, supported: true},
		{name: "Vision Pro", client: domain.GameLaunchClient{Name: "SpatialEMU.visionOS", Version: "1.40", Platform: "visionos-arm64", Architecture: "arm64"}, supported: true},
		{name: "Apple TV", client: domain.GameLaunchClient{Name: "SpatialEMU.tvOS", Version: "1.40", Platform: "tvos-arm64", Architecture: "arm64"}, supported: true},
		{name: "Android arm64", client: domain.GameLaunchClient{Name: "GameEMU.Android", Version: "0.1.0-dev", Platform: "android-arm64", Architecture: "arm64"}, supported: true},
		{name: "placeholder Apple identity", client: domain.GameLaunchClient{Name: "SpatialEMU.Apple", Version: "1.40", Platform: "apple", Architecture: "unknown"}},
		{name: "iPad name with iPhone platform", client: domain.GameLaunchClient{Name: "SpatialEMU.iPadOS", Version: "1.40", Platform: "ios-arm64", Architecture: "arm64"}},
		{name: "iOS simulator", client: domain.GameLaunchClient{Name: "SpatialEMU.iOS", Version: "1.40", Platform: "ios-simulator-arm64", Architecture: "arm64"}},
		{name: "mobile x64", client: domain.GameLaunchClient{Name: "SpatialEMU.visionOS", Version: "1.40", Platform: "visionos-arm64", Architecture: "x64"}},
		{name: "Android x64", client: domain.GameLaunchClient{Name: "GameEMU.Android", Version: "0.1.0-dev", Platform: "android-arm64", Architecture: "x64"}},
		{name: "Android generic platform", client: domain.GameLaunchClient{Name: "GameEMU.Android", Version: "0.1.0-dev", Platform: "android", Architecture: "arm64"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if actual := supportedPragmaticClient(test.client); actual != test.supported {
				t.Fatalf("supportedPragmaticClient(%+v)=%t, want %t", test.client, actual, test.supported)
			}
		})
	}
}

func TestPersistedLaunchProfileResolvesFromSQLite(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	root := t.TempDir()
	library, err := st.CreateLibrary("Games", root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "clone.zip")
	if err := os.WriteFile(path, []byte("verified-container"), 0o644); err != nil {
		t.Fatal(err)
	}
	const sourceSHA1 = "0123456789abcdef0123456789abcdef01234567"
	game, err := st.UpsertGame(domain.GameAsset{
		LibraryID: library.ID, Title: "Clone", Platform: "cps1", ROMSetName: "clone", Format: "zip",
		FilePath: path, RelPath: "clone.zip", Size: int64(len("verified-container")), MTime: time.Unix(1, 0),
		SHA1: sourceSHA1, EmulatorHint: "fbneo", CatalogRole: "needs-curation",
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := domain.GameRuntimeDescriptor{
		ID: "libretro", CoreID: "fbneo", CoreBuildID: "fbneo:4f7c3a1:windows-x64:release",
		CoreSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
	profile := domain.GameLaunchProfile{
		GameID: game.ID, ID: "clone-windows-fbneo-test", Revision: 42, Priority: 200, Policy: "test",
		ClientName: "SpatialEMU.Windows", MinClientVersion: "1.302", ClientPlatform: "windows-x64", Architecture: "x64",
		Runtime: runtime, EntryFile: "clone.zip", CanonicalSet: "clone", Status: "ready",
		Files: []domain.GameLaunchProfileFile{{Position: 0, SourceGameID: game.ID, SourceSHA1: sourceSHA1, SourceName: "clone.zip", Name: "clone.zip", Size: game.Size, Role: "entry"}},
	}
	if _, err := st.ReplaceGameLaunchProfiles("test", []domain.GameLaunchProfile{profile}, []domain.GameLaunchCatalogUpdate{{
		GameID: game.ID, Platform: "cps1", ROMSetName: "clone", EmulatorHint: "fbneo", CatalogRole: "game",
	}}); err != nil {
		t.Fatal(err)
	}
	requestedRuntime := runtime
	requestedRuntime.CoreSHA256 = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	request := domain.GameLaunchResolveRequest{
		Client:   domain.GameLaunchClient{Name: "SpatialEMU.Windows", Version: "1.302", Platform: "windows-x64", Architecture: "x64"},
		Runtimes: []domain.GameRuntimeDescriptor{requestedRuntime},
	}
	resolved, err := New(st).ResolveGameLaunchProfile(game.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.LaunchProfileID != profile.ID || resolved.ProfileRevision != 42 || len(resolved.Files) != 1 {
		t.Fatalf("resolution=%+v", resolved)
	}
	if resolved.Runtime.CoreBuildID != requestedRuntime.CoreBuildID || resolved.Runtime.CoreSHA256 != requestedRuntime.CoreSHA256 {
		t.Fatalf("resolved runtime=%+v, want exact request tuple %+v", resolved.Runtime, requestedRuntime)
	}
}

func TestAuditedLaunchCandidatePriorityDoesNotDependOnRuntimeOrder(t *testing.T) {
	const sha1 = "0123456789abcdef0123456789abcdef01234567"
	client := domain.GameLaunchClient{Name: "SpatialEMU.Windows", Version: "1.302", Platform: "windows-x64", Architecture: "x64"}
	mame := domain.GameRuntimeDescriptor{ID: "mame", Version: "0.288", ContentSet: "mame-0.288"}
	fbneo := domain.GameRuntimeDescriptor{ID: "libretro", CoreID: "fbneo", CoreSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
	profile := func(id string, priority int, runtime domain.GameRuntimeDescriptor) auditedGameLaunchProfile {
		return auditedGameLaunchProfile{
			ID: id, Revision: 1, Priority: priority,
			ClientName: client.Name, MinClientVersion: "1.302", ClientPlatform: client.Platform, Architecture: client.Architecture,
			Runtime: runtime, EntrySHA1: sha1, EntrySourceName: "shared.zip",
			Files: []auditedGameLaunchFile{{SourceSHA1: sha1, SourceName: "shared.zip", Name: "shared.zip", Size: 1024, Role: "entry"}},
		}
	}
	profiles := []auditedGameLaunchProfile{
		profile("shared-windows-mame-v1", 100, mame),
		profile("shared-windows-fbneo-v1", 200, fbneo),
	}
	game := domain.GameAsset{FilePath: "/games/shared.zip", Size: 1024, SHA1: sha1}

	for _, runtimes := range [][]domain.GameRuntimeDescriptor{{mame, fbneo}, {fbneo, mame}} {
		candidates := matchingAuditedLaunchCandidates(profiles, game, domain.GameLaunchResolveRequest{Client: client, Runtimes: runtimes})
		if len(candidates) != 2 || candidates[0].Profile.ID != "shared-windows-fbneo-v1" || candidates[0].Runtime.CoreID != "fbneo" {
			t.Fatalf("candidates=%+v, want higher-priority FBNeo profile first", candidates)
		}
	}
}

func TestAuditedLaunchSelectionFallsBackWhenPreferredDependenciesAreMissing(t *testing.T) {
	originalProfiles := auditedGameLaunchProfiles
	t.Cleanup(func() { auditedGameLaunchProfiles = originalProfiles })

	preferred := originalProfiles[0]
	preferred.Priority = 200
	fallback := preferred
	fallback.ID = "vstriker-windows-fbneo-fallback-test-v1"
	fallback.Priority = 100
	fallback.Runtime = domain.GameRuntimeDescriptor{
		ID: "libretro", CoreID: "fbneo", CoreSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
	fallback.Files = append([]auditedGameLaunchFile(nil), preferred.Files[0])
	auditedGameLaunchProfiles = []auditedGameLaunchProfile{preferred, fallback}

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	root := t.TempDir()
	library, err := st.CreateLibrary("Games", root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, preferred.EntrySourceName)
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(preferred.Files[0].Size); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	game, err := st.UpsertGame(domain.GameAsset{
		LibraryID: library.ID, Title: "Virtua Striker", Platform: "model2", ROMSetName: "vstriker", Format: "zip",
		FilePath: path, RelPath: preferred.EntrySourceName, Size: preferred.Files[0].Size, MTime: time.Unix(1, 0),
		SHA1: preferred.EntrySHA1, EmulatorHint: "model2", Compatibility: "unknown", CatalogRole: "game",
	})
	if err != nil {
		t.Fatal(err)
	}

	request := domain.GameLaunchResolveRequest{
		Client:   domain.GameLaunchClient{Name: preferred.ClientName, Version: preferred.MinClientVersion, Platform: preferred.ClientPlatform, Architecture: preferred.Architecture},
		Runtimes: []domain.GameRuntimeDescriptor{preferred.Runtime, fallback.Runtime},
	}
	resolved, err := New(st).ResolveGameLaunchProfile(game.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.LaunchProfileID != fallback.ID || resolved.Runtime.CoreID != "fbneo" || len(resolved.Files) != 1 {
		t.Fatalf("resolution=%+v, want dependency-free FBNeo fallback", resolved)
	}
}

func TestCanonicalPragmaticVersionNormalizesWindowsVersions(t *testing.T) {
	tests := map[string]string{
		"  v0.82.2.0  ": "0.82.2",
		"0, 82, 2, 0":   "0.82.2",
		"2.6.3.0":       "2.6.3",
		"1.20.4":        "1.20.4",
		"2.6":           "2.6",
		"unknown":       "",
	}
	for input, expected := range tests {
		if actual := canonicalPragmaticVersion(input); actual != expected {
			t.Fatalf("canonicalPragmaticVersion(%q)=%q, want %q", input, actual, expected)
		}
	}
}

func TestValidatePragmaticDOSLaunchRejectsUnsafePaths(t *testing.T) {
	valid := domain.DOSLaunch{
		EntrySource: "curated", EntryFile: "GAME/START.BAT", WorkingDirectory: "GAME",
		Candidates: []domain.DOSLaunchCandidate{{Path: "GAME/START.BAT", Kind: "bat"}},
	}
	if err := validatePragmaticDOSLaunch(valid); err != nil {
		t.Fatalf("valid DOS launch rejected: %v", err)
	}
	valid.Candidates = []domain.DOSLaunchCandidate{
		{Path: "C&C107.EXE", Kind: "exe"},
		{Path: "(O)_(-).EXE", Kind: "exe"},
	}
	if err := validatePragmaticDOSLaunch(valid); err != nil {
		t.Fatalf("valid DOS candidate filenames rejected: %v", err)
	}

	unsafe := valid
	unsafe.EntryFile = "GAME/START.BAT|FORMAT"
	if err := validatePragmaticDOSLaunch(unsafe); err == nil {
		t.Fatal("expected shell metacharacter to be rejected")
	}
	unsafe = valid
	unsafe.Candidates = []domain.DOSLaunchCandidate{{Path: "../START.BAT", Kind: "bat"}}
	if err := validatePragmaticDOSLaunch(unsafe); err == nil {
		t.Fatal("expected unsafe candidate path to be rejected")
	}
}
