package httpapi

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/service"
	"foliospace-reader/internal/store"
)

func TestAPIResolvesAuditedMAMEProfilesAndEnrichesLegacyManifestDependencies(t *testing.T) {
	ts, vstriker, segabill, tekken := launchProfileTestServer(t)
	defer ts.Close()
	info := authGet(t, ts.URL+"/api/client/info", "secret")
	if !strings.Contains(info, `"gameLaunchResolver":true`) || !strings.Contains(info, `"stableRuntimeIdentityV1":true`) {
		t.Fatalf("client info missing resolver capability: %s", info)
	}

	request := auditedMAMERequest("1.302")
	response := postLaunchResolve(t, ts.URL, vstriker.ID, "secret", request, map[string]string{
		"X-FolioSpace-Client": "untrusted-header", "X-FolioSpace-Runtime": "mame-9.999",
	})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("vstriker resolve status=%d body=%s", response.StatusCode, response.Body)
	}
	var resolved clientGameLaunchResolutionResponse
	if err := json.Unmarshal(response.Body, &resolved); err != nil {
		t.Fatal(err)
	}
	if resolved.LaunchProfileID != "vstriker-windows-mame0288-v1" || resolved.ProfileRevision != 1 {
		t.Fatalf("profile=%q revision=%d", resolved.LaunchProfileID, resolved.ProfileRevision)
	}
	if resolved.Runtime.ID != "mame" || resolved.Runtime.Version != "0.288" || resolved.Runtime.ContentSet != "mame-0.288" {
		t.Fatalf("runtime=%+v", resolved.Runtime)
	}
	if resolved.Manifest.Game.ROMSetName != "vstriker" || resolved.Manifest.Game.FileName != "vstriker.zip" || resolved.Manifest.Game.Size != 10316803 {
		t.Fatalf("resolved game=%+v", resolved.Manifest.Game)
	}
	if resolved.Manifest.EntryFile == nil || *resolved.Manifest.EntryFile != "vstriker.zip" || len(resolved.Manifest.Files) != 2 {
		t.Fatalf("resolved manifest=%+v", resolved.Manifest)
	}
	entry, dependency := resolved.Manifest.Files[0], resolved.Manifest.Files[1]
	if entry.Role != "entry" || entry.URL != "/api/client/games/"+itoa(vstriker.ID)+"/file" || entry.Checksum != "sha1:8e3518318eeb157ab299b2f284faef176d3f49dd" {
		t.Fatalf("entry=%+v", entry)
	}
	if dependency.Name != "segabill.zip" || dependency.Role != "dependency" || dependency.URL != "/api/client/games/"+itoa(segabill.ID)+"/file" || dependency.Checksum != "sha1:4631db7f7f5160a3a6591d3102722be869710f66" {
		t.Fatalf("dependency=%+v", dependency)
	}

	legacy := authGet(t, ts.URL+"/api/client/games/"+itoa(vstriker.ID)+"/manifest", "secret")
	if !strings.Contains(legacy, `"romSetName":"vstriker"`) || strings.Contains(legacy, "launchProfileId") {
		t.Fatalf("legacy manifest contract changed: %s", legacy)
	}
	var legacyManifest clientGameManifestResponse
	if err := json.Unmarshal([]byte(legacy), &legacyManifest); err != nil {
		t.Fatal(err)
	}
	if len(legacyManifest.Files) != 2 || legacyManifest.Files[0].Name != "vstriker.zip" {
		t.Fatalf("legacy manifest files=%+v", legacyManifest.Files)
	}
	legacyDependency := legacyManifest.Files[1]
	if legacyDependency.Name != "segabill.zip" || legacyDependency.Role != "dependency" ||
		legacyDependency.URL != "/api/client/games/"+itoa(segabill.ID)+"/file" ||
		legacyDependency.Checksum != "sha1:4631db7f7f5160a3a6591d3102722be869710f66" {
		t.Fatalf("legacy dependency=%+v", legacyDependency)
	}

	tekkenResponse := postLaunchResolve(t, ts.URL, tekken.ID, "secret", request, nil)
	if tekkenResponse.StatusCode != http.StatusOK {
		t.Fatalf("tekken resolve status=%d body=%s", tekkenResponse.StatusCode, tekkenResponse.Body)
	}
	var tekkenResolved clientGameLaunchResolutionResponse
	if err := json.Unmarshal(tekkenResponse.Body, &tekkenResolved); err != nil {
		t.Fatal(err)
	}
	if tekkenResolved.Manifest.Game.Title != "Tekken Tag Tournament (World, TEG2/VER.C1, set 2)" || tekkenResolved.Manifest.Game.ROMSetName != "tektagtc1a" || tekkenResolved.Manifest.Game.FileName != "tektagtc1a.zip" {
		t.Fatalf("tekken game=%+v", tekkenResolved.Manifest.Game)
	}
	if len(tekkenResolved.Manifest.Files) != 1 || tekkenResolved.Manifest.Files[0].Name != "tektagtc1a.zip" || tekkenResolved.Manifest.Files[0].URL != "/api/client/games/"+itoa(tekken.ID)+"/file" {
		t.Fatalf("tekken files=%+v", tekkenResolved.Manifest.Files)
	}
	legacyTekken := authGet(t, ts.URL+"/api/client/games/"+itoa(tekken.ID)+"/manifest", "secret")
	if !strings.Contains(legacyTekken, `"fileName":"tektagtac1.zip"`) || strings.Contains(legacyTekken, "tektagtc1a.zip") {
		t.Fatalf("legacy Tekken manifest exposed alias: %s", legacyTekken)
	}
}

func TestAPICanDisableLaunchResolverWithoutChangingLegacyManifest(t *testing.T) {
	ts, vstriker, _, _ := launchProfileTestServerWithOptions(t, Options{
		APIToken:                  "secret",
		DisableGameLaunchResolver: true,
	})
	defer ts.Close()

	info := authGet(t, ts.URL+"/api/client/info", "secret")
	if !strings.Contains(info, `"gameLaunchResolver":false`) || !strings.Contains(info, `"stableRuntimeIdentityV1":false`) {
		t.Fatalf("client info still advertises resolver capability: %s", info)
	}

	response := postLaunchResolve(t, ts.URL, vstriker.ID, "secret", auditedMAMERequest("1.302"), nil)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("disabled resolver status=%d body=%s", response.StatusCode, response.Body)
	}

	legacy := authGet(t, ts.URL+"/api/client/games/"+itoa(vstriker.ID)+"/manifest", "secret")
	if !strings.Contains(legacy, `"romSetName":"vstriker"`) || strings.Contains(legacy, "launchProfileId") || !strings.Contains(legacy, "segabill.zip") {
		t.Fatalf("legacy manifest changed while resolver disabled: %s", legacy)
	}
}

func TestAPIResolverRejectsUnauthorizedInvalidAndUnmatchedRequests(t *testing.T) {
	ts, vstriker, _, _ := launchProfileTestServer(t)
	defer ts.Close()

	unauthorized := postLaunchResolve(t, ts.URL, vstriker.ID, "", auditedMAMERequest("1.302"), nil)
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d body=%s", unauthorized.StatusCode, unauthorized.Body)
	}

	unmatched := auditedMAMERequest("1.302")
	unmatched.Runtimes[0].Version = "0.289"
	unmatched.Runtimes[0].ContentSet = "mame-0.289"
	conflict := postLaunchResolve(t, ts.URL, vstriker.ID, "secret", unmatched, nil)
	if conflict.StatusCode != http.StatusConflict || !strings.Contains(string(conflict.Body), `"code":"content-set-mismatch"`) {
		t.Fatalf("unmatched status=%d body=%s", conflict.StatusCode, conflict.Body)
	}

	oldClient := postLaunchResolve(t, ts.URL, vstriker.ID, "secret", auditedMAMERequest("1.301"), nil)
	if oldClient.StatusCode != http.StatusConflict {
		t.Fatalf("old client status=%d body=%s", oldClient.StatusCode, oldClient.Body)
	}

	invalid := auditedMAMERequest("1.302")
	invalid.Runtimes[0].CoreSHA256 = "ABC"
	badRequest := postLaunchResolve(t, ts.URL, vstriker.ID, "secret", invalid, nil)
	if badRequest.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid request status=%d body=%s", badRequest.StatusCode, badRequest.Body)
	}

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/client/games/"+itoa(vstriker.ID)+"/resolve", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET resolver status=%d", resp.StatusCode)
	}
}

func TestAPIResolvesAuditedCPSAndMAMECatalogProfiles(t *testing.T) {
	ts, games := catalogLaunchProfileTestServer(t)
	defer ts.Close()

	cpsRequest := domain.GameLaunchResolveRequest{
		Client: domain.GameLaunchClient{Name: "SpatialEMU.Windows", Version: "1.302", Platform: "windows-x64", Architecture: "x64"},
		Runtimes: []domain.GameRuntimeDescriptor{{
			ID: "libretro", CoreID: "fbneo", CoreSHA256: "6ebc2675c272c8d654935647ac336d45bbd97452c4d5943290d5ffc75678d9f1",
		}},
	}
	for _, test := range []struct {
		name      string
		platform  string
		profileID string
	}{
		{name: "sf2", platform: "cps1", profileID: "sf2-windows-fbneo-v1"},
		{name: "sfa", platform: "cps2", profileID: "sfa-windows-fbneo-v1"},
		{name: "sfiii", platform: "cps3", profileID: "sfiii-windows-fbneo-v1"},
	} {
		response := postLaunchResolve(t, ts.URL, games[test.name].ID, "secret", cpsRequest, nil)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s resolve status=%d body=%s", test.name, response.StatusCode, response.Body)
		}
		var resolved clientGameLaunchResolutionResponse
		if err := json.Unmarshal(response.Body, &resolved); err != nil {
			t.Fatal(err)
		}
		if resolved.LaunchProfileID != test.profileID || resolved.Runtime.ID != "libretro" || resolved.Runtime.CoreID != "fbneo" || resolved.Manifest.Game.Platform != test.platform || resolved.Manifest.Game.ROMSetName != test.name || resolved.Manifest.Game.FileName != test.name+".zip" {
			t.Fatalf("%s resolution=%+v", test.name, resolved)
		}
	}

	wrongCore := cpsRequest
	wrongCore.Runtimes = append([]domain.GameRuntimeDescriptor{}, cpsRequest.Runtimes...)
	wrongCore.Runtimes[0].CoreSHA256 = strings.Repeat("0", 64)
	conflict := postLaunchResolve(t, ts.URL, games["sf2"].ID, "secret", wrongCore, nil)
	if conflict.StatusCode != http.StatusConflict || !strings.Contains(string(conflict.Body), `"code":"core-fingerprint-unknown"`) {
		t.Fatalf("wrong CPS core status=%d body=%s", conflict.StatusCode, conflict.Body)
	}

	mameRequest := auditedMAMERequest("1.302")
	for _, test := range []struct {
		name      string
		profileID string
	}{
		{name: "hypreact", profileID: "hypreact-windows-mame0288-v1"},
		{name: "hypreac2", profileID: "hypreac2-windows-mame0288-v1"},
		{name: "srmp4", profileID: "srmp4-windows-mame0288-v1"},
		{name: "fromancr", profileID: "fromancr-windows-mame0288-v1"},
		{name: "fromanc4", profileID: "fromanc4-windows-mame0288-v1"},
		{name: "mcnpshnt", profileID: "mcnpshnt-windows-mame0288-v1"},
	} {
		response := postLaunchResolve(t, ts.URL, games[test.name].ID, "secret", mameRequest, nil)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s resolve status=%d body=%s", test.name, response.StatusCode, response.Body)
		}
		var resolved clientGameLaunchResolutionResponse
		if err := json.Unmarshal(response.Body, &resolved); err != nil {
			t.Fatal(err)
		}
		if resolved.LaunchProfileID != test.profileID || resolved.Manifest.Game.ROMSetName != test.name || resolved.Manifest.Game.FileName != test.name+".zip" {
			t.Fatalf("%s resolution=%+v", test.name, resolved)
		}
		if test.name == "mcnpshnt" {
			if len(resolved.Manifest.Files) != 2 || resolved.Manifest.Files[1].Name != "ym2413.zip" || resolved.Manifest.Files[1].Role != "dependency" || resolved.Manifest.Files[1].URL != "/api/client/games/"+itoa(games["ym2413_instruments"].ID)+"/file" {
				t.Fatalf("mcnpshnt files=%+v", resolved.Manifest.Files)
			}
		}
	}
}

func TestAPIResolvesPointBlankAppleFBNeoProfilesAndHidesDeviceArchive(t *testing.T) {
	ts, games := catalogLaunchProfileTestServer(t)
	defer ts.Close()

	iosBuildID := "fbneo:archive-f1d54ccd94b661434a38930591e3697b89165a5946c45eff98f60d3981fd7b6c:ios-arm64:full-v1"
	xrosBuildID := "fbneo:archive-a161e273b161dc77fad5acc449798987f89741f0f75da1f05bec4ff7b6b75181:xros-arm64:full-localized-v1"
	clients := []struct {
		name       string
		platform   string
		profileTag string
		buildID    string
	}{
		{name: "SpatialEMU.iOS", platform: "ios-arm64", profileTag: "ios", buildID: iosBuildID},
		{name: "SpatialEMU.iPadOS", platform: "ipados-arm64", profileTag: "ipados", buildID: iosBuildID},
		{name: "SpatialEMU.visionOS", platform: "visionos-arm64", profileTag: "visionos", buildID: xrosBuildID},
	}
	for _, client := range clients {
		request := domain.GameLaunchResolveRequest{
			Client:   domain.GameLaunchClient{Name: client.name, Version: "1.300", Platform: client.platform, Architecture: "arm64"},
			Runtimes: []domain.GameRuntimeDescriptor{{ID: "libretro", CoreID: "fbneo", CoreBuildID: client.buildID}},
		}
		for _, game := range []struct {
			name      string
			fileNames []string
		}{
			{name: "ptblank", fileNames: []string{"ptblank.zip", "namcoc75.zip"}},
			{name: "ptblanka", fileNames: []string{"ptblanka.zip", "ptblank.zip", "namcoc75.zip"}},
		} {
			response := postLaunchResolve(t, ts.URL, games[game.name].ID, "secret", request, nil)
			if response.StatusCode != http.StatusOK {
				t.Fatalf("%s/%s resolve status=%d body=%s", client.name, game.name, response.StatusCode, response.Body)
			}
			var resolved clientGameLaunchResolutionResponse
			if err := json.Unmarshal(response.Body, &resolved); err != nil {
				t.Fatal(err)
			}
			if resolved.LaunchProfileID != game.name+"-"+client.profileTag+"-fbneo-lightgun2p-v1" || resolved.Runtime.CoreBuildID != client.buildID || len(resolved.Manifest.Files) != len(game.fileNames) {
				t.Fatalf("%s/%s resolution=%+v", client.name, game.name, resolved)
			}
			for index, name := range game.fileNames {
				if resolved.Manifest.Files[index].Name != name {
					t.Fatalf("%s/%s file[%d]=%q, want %q", client.name, game.name, index, resolved.Manifest.Files[index].Name, name)
				}
			}
		}
	}

	unknown := domain.GameLaunchResolveRequest{
		Client:   domain.GameLaunchClient{Name: "SpatialEMU.iPadOS", Version: "1.300", Platform: "ipados-arm64", Architecture: "arm64"},
		Runtimes: []domain.GameRuntimeDescriptor{{ID: "libretro", CoreID: "fbneo", CoreBuildID: "fbneo-unknown-libretro-ios-arm64-lightgun2p-v1"}},
	}
	response := postLaunchResolve(t, ts.URL, games["ptblank"].ID, "secret", unknown, nil)
	if response.StatusCode != http.StatusConflict || !strings.Contains(string(response.Body), `"code":"core-fingerprint-unknown"`) {
		t.Fatalf("unknown build status=%d body=%s", response.StatusCode, response.Body)
	}

	if err := os.Rename(games["namcoc75"].FilePath, games["namcoc75"].FilePath+".missing"); err != nil {
		t.Fatal(err)
	}
	unknown.Runtimes[0].CoreBuildID = iosBuildID
	response = postLaunchResolve(t, ts.URL, games["ptblank"].ID, "secret", unknown, nil)
	if response.StatusCode != http.StatusConflict || !strings.Contains(string(response.Body), `"code":"dependency-missing"`) || !strings.Contains(string(response.Body), `"file":"namcoc75.zip"`) {
		t.Fatalf("missing dependency status=%d body=%s", response.StatusCode, response.Body)
	}

	if body := authGet(t, ts.URL+"/api/client/games?q=namcoc75", "secret"); strings.Contains(body, `"romSetName":"namcoc75"`) || !strings.Contains(body, `"total":0`) {
		t.Fatalf("client catalog exposed namcoc75: %s", body)
	}
}

func TestAPISelectsMAMEOrFBNeoFromDualArcadeCapabilities(t *testing.T) {
	ts, games := catalogLaunchProfileTestServer(t)
	defer ts.Close()

	mame := domain.GameRuntimeDescriptor{ID: "mame", Version: "0.288", ContentSet: "mame-0.288"}
	fbneo := domain.GameRuntimeDescriptor{ID: "libretro", CoreID: "fbneo", CoreSHA256: "6ebc2675c272c8d654935647ac336d45bbd97452c4d5943290d5ffc75678d9f1"}
	client := domain.GameLaunchClient{Name: "SpatialEMU.Windows", Version: "1.302", Platform: "windows-x64", Architecture: "x64"}

	for _, runtimes := range [][]domain.GameRuntimeDescriptor{{mame, fbneo}, {fbneo, mame}} {
		request := domain.GameLaunchResolveRequest{Client: client, Runtimes: runtimes}
		for _, test := range []struct {
			gameID      int64
			wantRuntime string
			wantCore    string
		}{
			{gameID: games["sf2"].ID, wantRuntime: "libretro", wantCore: "fbneo"},
			{gameID: games["hypreact"].ID, wantRuntime: "mame"},
		} {
			response := postLaunchResolve(t, ts.URL, test.gameID, "secret", request, nil)
			if response.StatusCode != http.StatusOK {
				t.Fatalf("resolve game=%d status=%d body=%s", test.gameID, response.StatusCode, response.Body)
			}
			var resolved clientGameLaunchResolutionResponse
			if err := json.Unmarshal(response.Body, &resolved); err != nil {
				t.Fatal(err)
			}
			if resolved.Runtime.ID != test.wantRuntime || resolved.Runtime.CoreID != test.wantCore {
				t.Fatalf("resolve game=%d runtime=%+v, want id=%q core=%q", test.gameID, resolved.Runtime, test.wantRuntime, test.wantCore)
			}
		}
	}
}

func TestAPIResolvesStrictMobileArcadeProfilesWithExactRuntimeIdentity(t *testing.T) {
	ts, games := catalogLaunchProfileTestServer(t)
	defer ts.Close()

	requests := []struct {
		name    string
		gameID  int64
		request domain.GameLaunchResolveRequest
	}{
		{
			name: "iPadOS FBNeo", gameID: games["sf2"].ID,
			request: domain.GameLaunchResolveRequest{
				Client:   domain.GameLaunchClient{Name: "SpatialEMU.iPadOS", Version: "1.300", Platform: "ipados-arm64", Architecture: "arm64"},
				Runtimes: []domain.GameRuntimeDescriptor{{ID: "libretro", CoreID: "fbneo", CoreSHA256: strings.Repeat("1", 64)}},
			},
		},
		{
			name: "visionOS MAME", gameID: games["hypreact"].ID,
			request: domain.GameLaunchResolveRequest{
				Client:   domain.GameLaunchClient{Name: "SpatialEMU.visionOS", Version: "1.300", Platform: "visionos-arm64", Architecture: "arm64"},
				Runtimes: []domain.GameRuntimeDescriptor{{ID: "mame", Version: "0.287", ContentSet: "mame-0.287"}},
			},
		},
	}
	for _, test := range requests {
		response := postLaunchResolve(t, ts.URL, test.gameID, "secret", test.request, nil)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", test.name, response.StatusCode, response.Body)
		}
	}

	wrongHash := requests[0].request
	wrongHash.Runtimes = append([]domain.GameRuntimeDescriptor{}, wrongHash.Runtimes...)
	wrongHash.Runtimes[0].CoreSHA256 = strings.Repeat("2", 64)
	response := postLaunchResolve(t, ts.URL, games["sf2"].ID, "secret", wrongHash, nil)
	if response.StatusCode != http.StatusConflict || !strings.Contains(string(response.Body), `"code":"core-fingerprint-unknown"`) {
		t.Fatalf("wrong mobile FBNeo hash status=%d body=%s", response.StatusCode, response.Body)
	}
}

func TestAPIResolvesPragmaticConsoleDiscAndDOSProfiles(t *testing.T) {
	ts, games, _ := pragmaticLaunchProfileTestServer(t)
	defer ts.Close()

	tests := []struct {
		name      string
		runtime   domain.GameRuntimeDescriptor
		entryFile string
		fileCount int
	}{
		{name: "nes", runtime: domain.GameRuntimeDescriptor{ID: "libretro", CoreID: "nestopia"}, entryFile: "mario.nes", fileCount: 1},
		{name: "ps1", runtime: domain.GameRuntimeDescriptor{ID: "libretro", CoreID: "swanstation"}, entryFile: "ridge.cue", fileCount: 2},
		{name: "n64", runtime: domain.GameRuntimeDescriptor{ID: "libretro", CoreID: "mupen64plus-next"}, entryFile: "zelda.z64", fileCount: 1},
		{name: "saturn", runtime: domain.GameRuntimeDescriptor{ID: "libretro", CoreID: "beetle-saturn"}, entryFile: "nights.cue", fileCount: 2},
		{name: "pc98", runtime: domain.GameRuntimeDescriptor{ID: "libretro", CoreID: "np2kai"}, entryFile: "game-disk1.fdi", fileCount: 2},
		{name: "ps2", runtime: domain.GameRuntimeDescriptor{ID: "pcsx2", Version: "2.6.3.0"}, entryFile: "mgs2.iso", fileCount: 1},
		{name: "psp", runtime: domain.GameRuntimeDescriptor{ID: "ppsspp", Version: "1.20.4"}, entryFile: "mgs.iso", fileCount: 1},
		{name: "ngc", runtime: domain.GameRuntimeDescriptor{ID: "dolphin"}, entryFile: "twin-snakes.iso", fileCount: 1},
		{name: "dreamcast", runtime: domain.GameRuntimeDescriptor{ID: "flycast", Version: "2.6"}, entryFile: "crazy-taxi.chd", fileCount: 1},
		{name: "dos", runtime: domain.GameRuntimeDescriptor{ID: "dosbox-staging", Version: "0.82.2.0"}, entryFile: "GAME/START.BAT", fileCount: 1},
	}
	client := domain.GameLaunchClient{Name: "SpatialEMU.Windows", Version: "1.302", Platform: "windows-x64", Architecture: "x64"}
	for _, test := range tests {
		request := domain.GameLaunchResolveRequest{Client: client, Runtimes: []domain.GameRuntimeDescriptor{test.runtime}}
		response := postLaunchResolve(t, ts.URL, games[test.name].ID, "secret", request, nil)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s resolve status=%d body=%s", test.name, response.StatusCode, response.Body)
		}
		var resolved clientGameLaunchResolutionResponse
		if err := json.Unmarshal(response.Body, &resolved); err != nil {
			t.Fatal(err)
		}
		if resolved.Runtime != test.runtime {
			t.Fatalf("%s runtime=%+v, want exact request tuple %+v", test.name, resolved.Runtime, test.runtime)
		}
		if !strings.HasPrefix(resolved.LaunchProfileID, "auto-") || resolved.ProfileRevision <= 0 {
			t.Fatalf("%s profile=%q revision=%d", test.name, resolved.LaunchProfileID, resolved.ProfileRevision)
		}
		if resolved.Manifest.EntryFile == nil || *resolved.Manifest.EntryFile != test.entryFile || len(resolved.Manifest.Files) != test.fileCount {
			t.Fatalf("%s manifest=%+v", test.name, resolved.Manifest)
		}
		for position, file := range resolved.Manifest.Files {
			expectedURL := "/api/client/games/" + itoa(games[test.name].ID) + "/files/" + itoa(int64(position))
			if file.URL != expectedURL {
				t.Fatalf("%s file %d URL=%q, want %q", test.name, position, file.URL, expectedURL)
			}
			if !strings.HasPrefix(file.Checksum, "sha1:") || len(file.Checksum) != len("sha1:")+40 {
				t.Fatalf("%s file %d checksum=%q", test.name, position, file.Checksum)
			}
		}

		repeated := postLaunchResolve(t, ts.URL, games[test.name].ID, "secret", request, nil)
		var repeatedResolved clientGameLaunchResolutionResponse
		if repeated.StatusCode != http.StatusOK || json.Unmarshal(repeated.Body, &repeatedResolved) != nil || repeatedResolved.LaunchProfileID != resolved.LaunchProfileID || repeatedResolved.ProfileRevision != resolved.ProfileRevision {
			t.Fatalf("%s repeated resolution is not stable: %s", test.name, repeated.Body)
		}
	}

	dosRequest := domain.GameLaunchResolveRequest{Client: client, Runtimes: []domain.GameRuntimeDescriptor{{ID: "dosbox-staging", Version: "0.82.2.0"}}}
	dosResponse := postLaunchResolve(t, ts.URL, games["dos"].ID, "secret", dosRequest, nil)
	var dosResolved clientGameLaunchResolutionResponse
	if err := json.Unmarshal(dosResponse.Body, &dosResolved); err != nil {
		t.Fatal(err)
	}
	if dosResolved.Manifest.Game.FileName != "dos-game.zip" || dosResolved.Manifest.DOSLaunch == nil || dosResolved.Manifest.DOSLaunch.EntrySource != "curated" || dosResolved.Manifest.DOSLaunch.WorkingDirectory == nil || *dosResolved.Manifest.DOSLaunch.WorkingDirectory != "GAME" {
		t.Fatalf("DOS resolution=%+v", dosResolved)
	}

	pc98Request := domain.GameLaunchResolveRequest{Client: client, Runtimes: []domain.GameRuntimeDescriptor{{ID: "libretro", CoreID: "np2kai"}}}
	pc98Response := postLaunchResolve(t, ts.URL, games["pc98"].ID, "secret", pc98Request, nil)
	var pc98Resolved clientGameLaunchResolutionResponse
	if err := json.Unmarshal(pc98Response.Body, &pc98Resolved); err != nil {
		t.Fatal(err)
	}
	if len(pc98Resolved.Manifest.Files) != 2 || pc98Resolved.Manifest.Files[0].Role != "entry" || pc98Resolved.Manifest.Files[0].DiskIndex == nil || *pc98Resolved.Manifest.Files[0].DiskIndex != 0 || pc98Resolved.Manifest.Files[0].DriveHint != "FDD1" || pc98Resolved.Manifest.Files[1].Role != "disk" || pc98Resolved.Manifest.Files[1].DiskIndex == nil || *pc98Resolved.Manifest.Files[1].DiskIndex != 1 {
		t.Fatalf("PC-98 disk metadata=%+v", pc98Resolved.Manifest.Files)
	}
}

func TestAtomiswaveBIOSIsIncludedInResolverAndLegacyManifest(t *testing.T) {
	ts, games, _ := pragmaticLaunchProfileTestServer(t)
	defer ts.Close()

	request := domain.GameLaunchResolveRequest{
		Client:   domain.GameLaunchClient{Name: "SpatialEMU.Windows", Version: "1.302", Platform: "windows-x64", Architecture: "x64"},
		Runtimes: []domain.GameRuntimeDescriptor{{ID: "flycast", Version: "2.6"}},
	}
	response := postLaunchResolve(t, ts.URL, games["atomiswave"].ID, "secret", request, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("resolve status=%d body=%s", response.StatusCode, response.Body)
	}
	var resolved clientGameLaunchResolutionResponse
	if err := json.Unmarshal(response.Body, &resolved); err != nil {
		t.Fatal(err)
	}
	if len(resolved.Manifest.Files) != 2 || resolved.Manifest.Files[1].Name != "awbios.zip" || resolved.Manifest.Files[1].Role != "dependency" ||
		resolved.Manifest.Files[1].URL != "/api/client/games/"+itoa(games["awbios"].ID)+"/file" {
		t.Fatalf("resolved manifest=%+v", resolved.Manifest)
	}

	androidRequest := domain.GameLaunchResolveRequest{
		Client: domain.GameLaunchClient{
			Name: "GameEMU.Android", Version: "0.1.0-dev", Platform: "android-arm64", Architecture: "arm64",
		},
		Runtimes: []domain.GameRuntimeDescriptor{{
			ID: "flycast", Version: "2.6", CoreBuildID: "flycast-392a429-android-v4-arm64-gles3-hle-vmu-arcade-save-bundle",
		}},
	}
	androidResponse := postLaunchResolve(t, ts.URL, games["atomiswave"].ID, "secret", androidRequest, nil)
	if androidResponse.StatusCode != http.StatusOK {
		t.Fatalf("Android Atomiswave resolve status=%d body=%s", androidResponse.StatusCode, androidResponse.Body)
	}
	var androidResolved clientGameLaunchResolutionResponse
	if err := json.Unmarshal(androidResponse.Body, &androidResolved); err != nil {
		t.Fatal(err)
	}
	if androidResolved.Manifest.Game.Platform != "atomiswave" || androidResolved.Manifest.Game.EmulatorHint != "flycast" ||
		len(androidResolved.Manifest.Files) != 1 || androidResolved.Manifest.Files[0].Name != "kofxi.zip" {
		t.Fatalf("Android manifest=%+v", androidResolved.Manifest)
	}

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/client/games/"+itoa(games["atomiswave"].ID)+"/manifest", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	legacyResponse, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer legacyResponse.Body.Close()
	legacyBody, err := io.ReadAll(legacyResponse.Body)
	if err != nil {
		t.Fatal(err)
	}
	if legacyResponse.StatusCode != http.StatusOK || !strings.Contains(string(legacyBody), `"name":"awbios.zip"`) ||
		!strings.Contains(string(legacyBody), `"role":"dependency"`) {
		t.Fatalf("legacy status=%d body=%s", legacyResponse.StatusCode, legacyBody)
	}
}

func TestAPIResolvesPragmaticProfilesForAppleMobileClients(t *testing.T) {
	ts, games, _ := pragmaticLaunchProfileTestServer(t)
	defer ts.Close()

	clients := []domain.GameLaunchClient{
		{Name: "SpatialEMU.iOS", Version: "1.40", Platform: "ios-arm64", Architecture: "arm64"},
		{Name: "SpatialEMU.iPadOS", Version: "1.40", Platform: "ipados-arm64", Architecture: "arm64"},
		{Name: "SpatialEMU.visionOS", Version: "1.40", Platform: "visionos-arm64", Architecture: "arm64"},
		{Name: "SpatialEMU.tvOS", Version: "1.40", Platform: "tvos-arm64", Architecture: "arm64"},
	}
	runtime := domain.GameRuntimeDescriptor{ID: "libretro", CoreID: "nestopia", CoreSHA256: strings.Repeat("3", 64)}
	profileIDs := make(map[string]struct{}, len(clients))
	for _, client := range clients {
		t.Run(client.Platform, func(t *testing.T) {
			request := domain.GameLaunchResolveRequest{Client: client, Runtimes: []domain.GameRuntimeDescriptor{runtime}}
			response := postLaunchResolve(t, ts.URL, games["nes"].ID, "secret", request, nil)
			if response.StatusCode != http.StatusOK {
				t.Fatalf("resolve status=%d body=%s", response.StatusCode, response.Body)
			}
			var resolved clientGameLaunchResolutionResponse
			if err := json.Unmarshal(response.Body, &resolved); err != nil {
				t.Fatal(err)
			}
			if resolved.Runtime != runtime || !strings.HasPrefix(resolved.LaunchProfileID, "auto-") || resolved.ProfileRevision <= 0 {
				t.Fatalf("resolution=%+v", resolved)
			}
			if resolved.Manifest.EntryFile == nil || *resolved.Manifest.EntryFile != "mario.nes" || len(resolved.Manifest.Files) != 1 {
				t.Fatalf("manifest=%+v", resolved.Manifest)
			}
			if _, exists := profileIDs[resolved.LaunchProfileID]; exists {
				t.Fatalf("profile id %q was reused across client platforms", resolved.LaunchProfileID)
			}
			profileIDs[resolved.LaunchProfileID] = struct{}{}
		})
	}
}

func TestAPIResolvesZippedNDSVirtualEntryForVisionOS(t *testing.T) {
	ts, games, _ := pragmaticLaunchProfileTestServer(t)
	defer ts.Close()

	request := domain.GameLaunchResolveRequest{
		Client: domain.GameLaunchClient{
			Name: "SpatialEMU.visionOS", Version: "1.40", Platform: "visionos-arm64", Architecture: "arm64",
		},
		Runtimes: []domain.GameRuntimeDescriptor{{ID: "libretro", CoreID: "melonds-ds"}},
	}
	response := postLaunchResolve(t, ts.URL, games["nds-zip"].ID, "secret", request, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("zipped NDS resolve status=%d body=%s", response.StatusCode, response.Body)
	}
	var resolved clientGameLaunchResolutionResponse
	if err := json.Unmarshal(response.Body, &resolved); err != nil {
		t.Fatal(err)
	}
	if resolved.Manifest.EntryFile == nil || *resolved.Manifest.EntryFile != "Ouendan.nds" || len(resolved.Manifest.Files) != 1 {
		t.Fatalf("manifest=%+v", resolved.Manifest)
	}
	file := resolved.Manifest.Files[0]
	if file.Name != "Ouendan.nds" || file.Size != int64(len("nds-rom-body")) || !strings.HasPrefix(file.Checksum, "sha1:") {
		t.Fatalf("manifest file=%+v", file)
	}
}

func TestAPIResolvesAndroidDreamcastWithStableFlycastIdentity(t *testing.T) {
	ts, games, _ := pragmaticLaunchProfileTestServer(t)
	defer ts.Close()

	runtime := domain.GameRuntimeDescriptor{
		ID:          "flycast",
		Version:     "2.6",
		CoreBuildID: "flycast-392a429-android-v4-arm64-gles3-hle-vmu-arcade-save-bundle",
	}
	request := domain.GameLaunchResolveRequest{
		Client: domain.GameLaunchClient{
			Name: "GameEMU.Android", Version: "0.1.0-dev", Platform: "android-arm64", Architecture: "arm64",
		},
		Runtimes: []domain.GameRuntimeDescriptor{runtime},
	}
	response := postLaunchResolve(t, ts.URL, games["dreamcast"].ID, "secret", request, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("Android Dreamcast resolve status=%d body=%s", response.StatusCode, response.Body)
	}
	var resolved clientGameLaunchResolutionResponse
	if err := json.Unmarshal(response.Body, &resolved); err != nil {
		t.Fatal(err)
	}
	if resolved.Runtime != runtime {
		t.Fatalf("runtime=%+v, want exact request tuple %+v", resolved.Runtime, runtime)
	}
	if !strings.HasPrefix(resolved.LaunchProfileID, "auto-") || resolved.ProfileRevision <= 0 {
		t.Fatalf("profile=%q revision=%d", resolved.LaunchProfileID, resolved.ProfileRevision)
	}
	if resolved.Manifest.EntryFile == nil || *resolved.Manifest.EntryFile != "crazy-taxi.chd" || len(resolved.Manifest.Files) != 1 {
		t.Fatalf("manifest=%+v", resolved.Manifest)
	}
	file := resolved.Manifest.Files[0]
	if file.Role != "entry" || file.URL != "/api/client/games/"+itoa(games["dreamcast"].ID)+"/files/0" || !strings.HasPrefix(file.Checksum, "sha1:") {
		t.Fatalf("manifest file=%+v", file)
	}
}

func TestAPIResolvesAndroidNaomi2CanonicalManifestWithExactFlycastBuild(t *testing.T) {
	ts, games, _ := pragmaticLaunchProfileTestServer(t)
	defer ts.Close()

	runtime := domain.GameRuntimeDescriptor{
		ID:          "flycast",
		Version:     "2.6",
		CoreBuildID: "flycast-392a429-android-v4-arm64-gles3-hle-vmu-arcade-save-bundle",
	}
	request := domain.GameLaunchResolveRequest{
		Client: domain.GameLaunchClient{
			Name: "GameEMU.Android", Version: "0.1.0-dev", Platform: "android-arm64", Architecture: "arm64",
		},
		Runtimes: []domain.GameRuntimeDescriptor{runtime},
	}
	response := postLaunchResolve(t, ts.URL, games["naomi2"].ID, "secret", request, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("Android NAOMI 2 resolve status=%d body=%s", response.StatusCode, response.Body)
	}
	var resolved clientGameLaunchResolutionResponse
	if err := json.Unmarshal(response.Body, &resolved); err != nil {
		t.Fatal(err)
	}
	if resolved.Runtime != runtime || resolved.Manifest.Game.Platform != "naomi2" ||
		resolved.Manifest.EntryFile == nil || *resolved.Manifest.EntryFile != "vf4.zip" || len(resolved.Manifest.Files) != 2 {
		t.Fatalf("resolution=%+v", resolved)
	}
	entry, chd := resolved.Manifest.Files[0], resolved.Manifest.Files[1]
	if entry.Name != "vf4.zip" || entry.Role != "entry" ||
		chd.Name != "vf4/gds-0012c.chd" || chd.Role != "dependency" ||
		strings.Contains(string(response.Body), "naomi2.zip") {
		t.Fatalf("NAOMI 2 manifest=%+v", resolved.Manifest)
	}

	splitResponse := postLaunchResolve(t, ts.URL, games["naomi2-split"].ID, "secret", request, nil)
	if splitResponse.StatusCode != http.StatusOK {
		t.Fatalf("Android NAOMI 2 split resolve status=%d body=%s", splitResponse.StatusCode, splitResponse.Body)
	}
	var splitResolved clientGameLaunchResolutionResponse
	if err := json.Unmarshal(splitResponse.Body, &splitResolved); err != nil {
		t.Fatal(err)
	}
	if splitResolved.Manifest.Game.ROMSetName != "clubkrto" || splitResolved.Manifest.Game.ParentROMSetName != "clubkrt" ||
		splitResolved.Manifest.EntryFile == nil || *splitResolved.Manifest.EntryFile != "clubkrto.zip" || len(splitResolved.Manifest.Files) != 2 {
		t.Fatalf("split resolution=%+v", splitResolved)
	}
	splitEntry, splitParent := splitResolved.Manifest.Files[0], splitResolved.Manifest.Files[1]
	if splitEntry.Name != "clubkrto.zip" || splitEntry.Role != "entry" ||
		splitParent.Name != "clubkrt.zip" || splitParent.Role != "dependency" ||
		strings.Contains(string(splitResponse.Body), "naomi2.zip") {
		t.Fatalf("NAOMI 2 split manifest=%+v", splitResolved.Manifest)
	}

	request.Runtimes[0].CoreBuildID = "flycast-392a429-android-v3-arm64-gles3-hle-vmu"
	rejected := postLaunchResolve(t, ts.URL, games["naomi2"].ID, "secret", request, nil)
	if rejected.StatusCode != http.StatusConflict || !strings.Contains(string(rejected.Body), `"code":"runtime-unsupported"`) {
		t.Fatalf("stale Android Flycast build status=%d body=%s", rejected.StatusCode, rejected.Body)
	}
}

func TestAPIResolvesMobilePSPAndDOSProfilesWithExactCoreFingerprint(t *testing.T) {
	ts, games, _ := pragmaticLaunchProfileTestServer(t)
	defer ts.Close()

	for _, test := range []struct {
		name    string
		gameID  int64
		client  domain.GameLaunchClient
		runtime domain.GameRuntimeDescriptor
		entry   string
	}{
		{
			name: "iPad PSP", gameID: games["psp"].ID,
			client:  domain.GameLaunchClient{Name: "SpatialEMU.iPadOS", Version: "1.40", Platform: "ipados-arm64", Architecture: "arm64"},
			runtime: domain.GameRuntimeDescriptor{ID: "libretro", CoreID: "ppsspp", CoreSHA256: strings.Repeat("4", 64)},
			entry:   "mgs.iso",
		},
		{
			name: "Vision Pro DOS", gameID: games["dos"].ID,
			client:  domain.GameLaunchClient{Name: "SpatialEMU.visionOS", Version: "1.40", Platform: "visionos-arm64", Architecture: "arm64"},
			runtime: domain.GameRuntimeDescriptor{ID: "libretro", CoreID: "dosbox-pure", CoreSHA256: strings.Repeat("5", 64)},
			entry:   "GAME/START.BAT",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := domain.GameLaunchResolveRequest{Client: test.client, Runtimes: []domain.GameRuntimeDescriptor{test.runtime}}
			response := postLaunchResolve(t, ts.URL, test.gameID, "secret", request, nil)
			if response.StatusCode != http.StatusOK {
				t.Fatalf("resolve status=%d body=%s", response.StatusCode, response.Body)
			}
			var resolved clientGameLaunchResolutionResponse
			if err := json.Unmarshal(response.Body, &resolved); err != nil {
				t.Fatal(err)
			}
			if resolved.Runtime != test.runtime || resolved.Manifest.EntryFile == nil || *resolved.Manifest.EntryFile != test.entry {
				t.Fatalf("resolution=%+v", resolved)
			}
			if len(resolved.Manifest.Files) == 0 || !strings.HasPrefix(resolved.Manifest.Files[0].Checksum, "sha1:") {
				t.Fatalf("manifest files=%+v", resolved.Manifest.Files)
			}
		})
	}
}

func TestAPIPragmaticResolverRejectsUnknownCoreAndUncuratedDOS(t *testing.T) {
	ts, games, _ := pragmaticLaunchProfileTestServer(t)
	defer ts.Close()
	client := domain.GameLaunchClient{Name: "SpatialEMU.Windows", Version: "1.302", Platform: "windows-x64", Architecture: "x64"}

	unknownCore := domain.GameLaunchResolveRequest{Client: client, Runtimes: []domain.GameRuntimeDescriptor{{ID: "libretro", CoreID: "unknown-core"}}}
	response := postLaunchResolve(t, ts.URL, games["nes"].ID, "secret", unknownCore, nil)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("unknown core status=%d body=%s", response.StatusCode, response.Body)
	}

	uncurated := domain.GameLaunchResolveRequest{Client: client, Runtimes: []domain.GameRuntimeDescriptor{{ID: "dosbox-staging", Version: "0.82.2.0"}}}
	response = postLaunchResolve(t, ts.URL, games["dos-unknown"].ID, "secret", uncurated, nil)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("uncurated DOS status=%d body=%s", response.StatusCode, response.Body)
	}
}

func TestAPIPragmaticResolverRejectsChangedAndMissingManifestFiles(t *testing.T) {
	t.Run("changed source invalidates checksum", func(t *testing.T) {
		ts, games, _ := pragmaticLaunchProfileTestServer(t)
		defer ts.Close()
		if err := os.WriteFile(games["nes"].FilePath, []byte("evil"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(games["nes"].FilePath, time.Now(), time.Now()); err != nil {
			t.Fatal(err)
		}
		request := domain.GameLaunchResolveRequest{
			Client:   domain.GameLaunchClient{Name: "SpatialEMU.Windows", Version: "1.302", Platform: "windows-x64", Architecture: "x64"},
			Runtimes: []domain.GameRuntimeDescriptor{{ID: "libretro", CoreID: "nestopia"}},
		}
		response := postLaunchResolve(t, ts.URL, games["nes"].ID, "secret", request, nil)
		if response.StatusCode != http.StatusConflict || !strings.Contains(string(response.Body), `"code":"manifest-checksum-unavailable"`) {
			t.Fatalf("changed source status=%d body=%s", response.StatusCode, response.Body)
		}
	})

	t.Run("missing dependency is explicit", func(t *testing.T) {
		ts, games, _ := pragmaticLaunchProfileTestServer(t)
		defer ts.Close()
		if err := os.Remove(filepath.Join(filepath.Dir(games["ps1"].FilePath), "ridge.bin")); err != nil {
			t.Fatal(err)
		}
		request := domain.GameLaunchResolveRequest{
			Client:   domain.GameLaunchClient{Name: "SpatialEMU.Windows", Version: "1.302", Platform: "windows-x64", Architecture: "x64"},
			Runtimes: []domain.GameRuntimeDescriptor{{ID: "libretro", CoreID: "swanstation"}},
		}
		response := postLaunchResolve(t, ts.URL, games["ps1"].ID, "secret", request, nil)
		if response.StatusCode != http.StatusConflict || !strings.Contains(string(response.Body), `"code":"dependency-missing"`) {
			t.Fatalf("missing dependency status=%d body=%s", response.StatusCode, response.Body)
		}
	})

	t.Run("missing checksum is explicit", func(t *testing.T) {
		ts, games, st := pragmaticLaunchProfileTestServer(t)
		defer ts.Close()
		files, err := st.GameFiles(games["psp"].ID)
		if err != nil {
			t.Fatal(err)
		}
		changedAt := time.Unix(2, 0)
		if err := os.Chtimes(files[0].FilePath, changedAt, changedAt); err != nil {
			t.Fatal(err)
		}
		files[0].MTime = changedAt
		files[0].SHA1 = ""
		if err := st.ReplaceGameFiles(games["psp"].ID, files); err != nil {
			t.Fatal(err)
		}
		request := domain.GameLaunchResolveRequest{
			Client:   domain.GameLaunchClient{Name: "SpatialEMU.iPadOS", Version: "1.40", Platform: "ipados-arm64", Architecture: "arm64"},
			Runtimes: []domain.GameRuntimeDescriptor{{ID: "libretro", CoreID: "ppsspp", CoreSHA256: strings.Repeat("6", 64)}},
		}
		response := postLaunchResolve(t, ts.URL, games["psp"].ID, "secret", request, nil)
		if response.StatusCode != http.StatusConflict || !strings.Contains(string(response.Body), `"code":"manifest-checksum-unavailable"`) {
			t.Fatalf("missing checksum status=%d body=%s", response.StatusCode, response.Body)
		}
	})
}

func pragmaticLaunchProfileTestServer(t *testing.T) (*httptest.Server, map[string]domain.GameAsset, *store.Store) {
	t.Helper()
	root := t.TempDir()
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	st := store.New(conn)
	lib, err := st.CreateLibrary("Games", root)
	if err != nil {
		t.Fatal(err)
	}
	games := map[string]domain.GameAsset{}
	createFile := func(name, contents string) string {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, time.Unix(1, 0), time.Unix(1, 0)); err != nil {
			t.Fatal(err)
		}
		return path
	}
	addGame := func(key, platform, format, entryName string, dependencies ...string) domain.GameAsset {
		entryContents := "game"
		if format == "cue" && len(dependencies) > 0 {
			entryContents = "FILE \"" + dependencies[0] + "\" BINARY\n  TRACK 01 MODE1/2352\n    INDEX 01 00:00:00\n"
		}
		entryPath := createFile(entryName, entryContents)
		files := []domain.GameFile{{Name: entryName, FilePath: entryPath, Size: int64(len(entryContents)), MTime: time.Unix(1, 0), SHA1: testSHA1(entryContents), Role: "entry", Position: 0}}
		totalSize := int64(len(entryContents))
		for index, dependency := range dependencies {
			contents := "track-data-" + dependency
			path := createFile(dependency, contents)
			files = append(files, domain.GameFile{Name: dependency, FilePath: path, Size: int64(len(contents)), MTime: time.Unix(1, 0), SHA1: testSHA1(contents), Role: "dependency", Position: index + 1})
			totalSize += int64(len(contents))
		}
		game, err := st.UpsertGame(domain.GameAsset{
			LibraryID: lib.ID, Title: key, Platform: platform, ROMSetName: strings.ToUpper(platform), Format: format,
			FilePath: entryPath, RelPath: entryName, Size: totalSize, MTime: time.Unix(1, 0),
			SHA1: strings.Repeat("a", 40), EmulatorHint: platform, Compatibility: "unknown", CatalogRole: "game",
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.ReplaceGameFiles(game.ID, files); err != nil {
			t.Fatal(err)
		}
		games[key] = game
		return game
	}

	addGame("nes", "nes", "nes", "mario.nes")
	addGame("ps1", "ps1", "cue", "ridge.cue", "ridge.bin")
	addGame("n64", "n64", "z64", "zelda.z64")
	addGame("saturn", "saturn", "cue", "nights.cue", "nights.bin")
	pc98 := addGame("pc98", "pc98", "fdi", "game-disk1.fdi", "game-disk2.fdi")
	pc98Files, err := st.GameFiles(pc98.ID)
	if err != nil {
		t.Fatal(err)
	}
	pc98Files[1].Role = "disk"
	if err := st.ReplaceGameFiles(pc98.ID, pc98Files); err != nil {
		t.Fatal(err)
	}
	addGame("ps2", "ps2", "iso", "mgs2.iso")
	addGame("psp", "psp", "iso", "mgs.iso")
	addGame("ngc", "ngc", "iso", "twin-snakes.iso")
	addGame("dreamcast", "dreamcast", "chd", "crazy-taxi.chd")
	ndsROM := "nds-rom-body"
	ndsZipPath := filepath.Join(root, "Ouendan.zip")
	makeZip(t, ndsZipPath, map[string]string{"Ouendan.nds": ndsROM})
	if err := os.Chtimes(ndsZipPath, time.Unix(1, 0), time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	nds, err := st.UpsertGame(domain.GameAsset{
		LibraryID: lib.ID, Title: "Osu! Tatakae! Ouendan", Platform: "nds", ROMSetName: "Nintendo DS", Format: "nds",
		FilePath: ndsZipPath, RelPath: "Ouendan.nds", Size: int64(len(ndsROM)), MTime: time.Unix(1, 0),
		SHA1: testSHA1(ndsROM), EmulatorHint: "melonds-ds", Compatibility: "untested", CatalogRole: "game",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceGameFiles(nds.ID, []domain.GameFile{{
		Name: "Ouendan.nds", FilePath: ndsZipPath, Size: int64(len(ndsROM)), MTime: time.Unix(1, 0),
		SHA1: testSHA1(ndsROM), Role: "entry", Position: 0,
	}}); err != nil {
		t.Fatal(err)
	}
	games["nds-zip"] = nds
	addGame("naomi2", "naomi2", "zip", "vf4.zip", "vf4/gds-0012c.chd")
	splitNaomi2 := addGame("naomi2-split", "naomi2", "zip", "clubkrto.zip", "clubkrt.zip")
	splitNaomi2.Title = "Club Kart: European Session"
	splitNaomi2.ROMSetName = "clubkrto"
	splitNaomi2, err = st.UpsertGame(splitNaomi2)
	if err != nil {
		t.Fatal(err)
	}
	games["naomi2-split"] = splitNaomi2
	addGame("atomiswave", "naomi", "zip", "kofxi.zip")
	biosContents := strings.Repeat("b", 34620)
	biosPath := createFile("awbios.zip", biosContents)
	bios, err := st.UpsertGame(domain.GameAsset{
		LibraryID: lib.ID, Title: "awbios", Platform: "naomi", ROMSetName: "awbios", Format: "zip",
		FilePath: biosPath, RelPath: "awbios.zip", Size: int64(len(biosContents)), MTime: time.Unix(1, 0),
		SHA1: "cdf247154e28c4b352b962a4a523587f2fde9305", EmulatorHint: "flycast", Compatibility: "unknown", CatalogRole: "dependency",
	})
	if err != nil {
		t.Fatal(err)
	}
	games["awbios"] = bios
	dos := addGame("dos", "dos", "zip", "dos-game.zip")
	if err := st.UpsertDOSLaunch(domain.DOSLaunch{
		GameID: dos.ID, EntryFile: "GAME/START.BAT", EntrySource: "curated", WorkingDirectory: "GAME",
		Arguments: []string{"-fast"}, Candidates: []domain.DOSLaunchCandidate{{Path: "GAME/START.BAT", Kind: "bat"}},
	}); err != nil {
		t.Fatal(err)
	}
	unknown := addGame("dos-unknown", "dos", "zip", "unknown-dos.zip")
	if err := st.UpsertDOSLaunch(domain.DOSLaunch{
		GameID: unknown.ID, EntrySource: "unknown", Candidates: []domain.DOSLaunchCandidate{{Path: "GAME.EXE", Kind: "exe"}},
	}); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(NewWithOptions(service.New(st), nil, Options{APIToken: "secret"}).Routes())
	return ts, games, st
}

func testSHA1(contents string) string {
	digest := sha1.Sum([]byte(contents))
	return hex.EncodeToString(digest[:])
}

func launchProfileTestServer(t *testing.T) (*httptest.Server, domain.GameAsset, domain.GameAsset, domain.GameAsset) {
	return launchProfileTestServerWithOptions(t, Options{APIToken: "secret"})
}

func launchProfileTestServerWithOptions(t *testing.T, options Options) (*httptest.Server, domain.GameAsset, domain.GameAsset, domain.GameAsset) {
	t.Helper()
	root := t.TempDir()
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	st := store.New(conn)
	lib, err := st.CreateLibrary("Games", root)
	if err != nil {
		t.Fatal(err)
	}
	createSparse := func(name string, size int64) string {
		path := filepath.Join(root, name)
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Truncate(size); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		return path
	}
	upsert := func(title, platform, romSet, name string, size int64, sha1, role string) domain.GameAsset {
		game, err := st.UpsertGame(domain.GameAsset{
			LibraryID: lib.ID, Title: title, Platform: platform, ROMSetName: romSet, Format: "zip",
			FilePath: createSparse(name, size), RelPath: name, Size: size, MTime: time.Unix(1, 0),
			SHA1: sha1, EmulatorHint: platform, Compatibility: "unknown", CatalogRole: role,
		})
		if err != nil {
			t.Fatal(err)
		}
		return game
	}
	vstriker := upsert("vstriker", "model2", "Model2ROMs", "vstriker.zip", 10313686, "8e3518318eeb157ab299b2f284faef176d3f49dd", "game")
	segabill := upsert("segabill", "model2", "Model2ROMs", "segabill.zip", 3117, "4631db7f7f5160a3a6591d3102722be869710f66", "dependency")
	tekken := upsert("tektagtac1", "arcade", "Namco System 12", "tektagtac1.zip", 120980600, "d6615a3a70ea9941b61ccd608054a0044d3d6ab3", "game")
	if _, err := st.ReplaceGameLaunchProfiles("test-mame", []domain.GameLaunchProfile{{
		GameID: vstriker.ID, ID: "vstriker-windows-mame0288-v1", Revision: 1, Priority: 200,
		Policy: "test-mame", ClientName: "SpatialEMU.Windows", MinClientVersion: "1.302",
		ClientPlatform: "windows-x64", Architecture: "x64",
		Runtime:   domain.GameRuntimeDescriptor{ID: "mame", Version: "0.288", ContentSet: "mame-0.288"},
		EntryFile: "vstriker.zip", CanonicalSet: "vstriker", Status: "ready",
		Files: []domain.GameLaunchProfileFile{
			{Position: 0, SourceGameID: vstriker.ID, SourceSHA1: vstriker.SHA1, SourceName: "vstriker.zip", Name: "vstriker.zip", Size: vstriker.Size, Role: "entry"},
			{Position: 1, SourceGameID: segabill.ID, SourceSHA1: segabill.SHA1, SourceName: "segabill.zip", Name: "segabill.zip", Size: segabill.Size, Role: "dependency"},
		},
	}}, []domain.GameLaunchCatalogUpdate{{
		GameID: vstriker.ID, Platform: "model2", ROMSetName: "vstriker", EmulatorHint: "mame", CatalogRole: "game",
	}}); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(NewWithOptions(service.New(st), nil, options).Routes())
	return ts, vstriker, segabill, tekken
}

func catalogLaunchProfileTestServer(t *testing.T) (*httptest.Server, map[string]domain.GameAsset) {
	t.Helper()
	root := t.TempDir()
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	st := store.New(conn)
	lib, err := st.CreateLibrary("Games", root)
	if err != nil {
		t.Fatal(err)
	}
	type asset struct {
		name     string
		platform string
		emulator string
		size     int64
		sha1     string
		role     string
	}
	assets := []asset{
		{name: "sf2", platform: "cps1", emulator: "fbneo", size: 3551819, sha1: "bd59872a57f14dc492e2fb387727a9402f3d4f97", role: "game"},
		{name: "sfa", platform: "cps2", emulator: "fbneo", size: 7365582, sha1: "61dece364b8d2f2ff15391505168be334ebb371a", role: "game"},
		{name: "sfiii", platform: "cps3", emulator: "fbneo", size: 38868517, sha1: "7aae0cfc4ef8911f19d2e986cee63807deebf1b6", role: "game"},
		{name: "hypreact", platform: "mame", emulator: "mame", size: 8052342, sha1: "e0940f848884c9d53bbc41bb947d584e06cc1845", role: "game"},
		{name: "hypreac2", platform: "mame", emulator: "mame", size: 18291541, sha1: "7fe73cc7ee40a49225a4616106e538c084ef4364", role: "game"},
		{name: "srmp4", platform: "mame", emulator: "mame", size: 7697767, sha1: "cfcf2cdf61ebca862a84473a8bf75fbe8d76cb7b", role: "game"},
		{name: "fromancr", platform: "mame", emulator: "mame", size: 14121810, sha1: "137e4949d7e204ff10e33372528cc1e9481b962c", role: "game"},
		{name: "fromanc4", platform: "mame", emulator: "mame", size: 21443327, sha1: "ff478f3350d9703e8647f659ce169ee234082249", role: "game"},
		{name: "mcnpshnt", platform: "mame", emulator: "mame", size: 1205007, sha1: "24a714371a867db1709798a95a171778e0940021", role: "game"},
		{name: "ym2413_instruments", platform: "mame", emulator: "mame", size: 322, sha1: "cbcd6e0698026452bb2bb6a6e6f7f5a3667a675c", role: "dependency"},
		{name: "ptblank", platform: "arcade", emulator: "fbneo", size: 5033400, sha1: "15f9dd6ccf009bffcb156b234bdeadbe26344314", role: "game"},
		{name: "ptblanka", platform: "arcade", emulator: "fbneo", size: 131248, sha1: "ee3e54a9f49bfc7c27f3e0c6ad580bf78d04d1e2", role: "game"},
		{name: "namcoc75", platform: "arcade", emulator: "fbneo", size: 8709, sha1: "0649e27b7d605add7fc4215ee628b71e3c835328", role: "dependency"},
	}
	games := make(map[string]domain.GameAsset, len(assets))
	for _, item := range assets {
		path := filepath.Join(root, item.name+".zip")
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Truncate(item.size); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		game, err := st.UpsertGame(domain.GameAsset{
			LibraryID: lib.ID, Title: "Catalog " + item.name, Platform: item.platform, ROMSetName: item.name,
			Format: "zip", FilePath: path, RelPath: item.name + ".zip", Size: item.size, MTime: time.Unix(1, 0),
			SHA1: item.sha1, EmulatorHint: item.emulator, Compatibility: "unknown", CatalogRole: item.role,
		})
		if err != nil {
			t.Fatal(err)
		}
		games[item.name] = game
	}
	mobileProfiles := []domain.GameLaunchProfile{
		{
			GameID: games["sf2"].ID, ID: "sf2-ipados-fbneo-test", Revision: 1, Priority: 200,
			ClientName: "SpatialEMU.iPadOS", MinClientVersion: "1.300", ClientPlatform: "ipados-arm64", Architecture: "arm64",
			Runtime:   domain.GameRuntimeDescriptor{ID: "libretro", CoreID: "fbneo", CoreSHA256: strings.Repeat("1", 64)},
			EntryFile: "sf2.zip", CanonicalSet: "sf2", Status: "ready",
			Files: []domain.GameLaunchProfileFile{{
				Position: 0, SourceGameID: games["sf2"].ID, SourceSHA1: games["sf2"].SHA1,
				SourceName: "sf2.zip", Name: "sf2.zip", Size: games["sf2"].Size, Role: "entry",
			}},
		},
		{
			GameID: games["hypreact"].ID, ID: "hypreact-visionos-mame0287-test", Revision: 1, Priority: 200,
			ClientName: "SpatialEMU.visionOS", MinClientVersion: "1.300", ClientPlatform: "visionos-arm64", Architecture: "arm64",
			Runtime:   domain.GameRuntimeDescriptor{ID: "mame", Version: "0.287", ContentSet: "mame-0.287"},
			EntryFile: "hypreact.zip", CanonicalSet: "hypreact", Status: "ready",
			Files: []domain.GameLaunchProfileFile{{
				Position: 0, SourceGameID: games["hypreact"].ID, SourceSHA1: games["hypreact"].SHA1,
				SourceName: "hypreact.zip", Name: "hypreact.zip", Size: games["hypreact"].Size, Role: "entry",
			}},
		},
	}
	mobileUpdates := []domain.GameLaunchCatalogUpdate{
		{GameID: games["sf2"].ID, Platform: "cps1", ROMSetName: "sf2", EmulatorHint: "fbneo", CatalogRole: "game"},
		{GameID: games["hypreact"].ID, Platform: "mame", ROMSetName: "hypreact", EmulatorHint: "mame", CatalogRole: "game"},
	}
	if _, err := st.ReplaceGameLaunchProfiles("test-mobile-arcade", mobileProfiles, mobileUpdates); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(NewWithOptions(service.New(st), nil, Options{APIToken: "secret"}).Routes())
	return ts, games
}

func auditedMAMERequest(version string) domain.GameLaunchResolveRequest {
	return domain.GameLaunchResolveRequest{
		Client:   domain.GameLaunchClient{Name: "SpatialEMU.Windows", Version: version, Platform: "windows-x64", Architecture: "x64"},
		Runtimes: []domain.GameRuntimeDescriptor{{ID: "mame", Version: "0.288", ContentSet: "mame-0.288"}},
	}
}

type launchResolveHTTPResponse struct {
	StatusCode int
	Body       []byte
}

func postLaunchResolve(t *testing.T, baseURL string, gameID int64, token string, body domain.GameLaunchResolveRequest, headers map[string]string) launchResolveHTTPResponse {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/client/games/"+itoa(gameID)+"/resolve", bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return launchResolveHTTPResponse{StatusCode: resp.StatusCode, Body: data}
}
