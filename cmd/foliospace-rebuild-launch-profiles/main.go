package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"

	"foliospace-reader/internal/config"
	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/launchcatalog"
	"foliospace-reader/internal/launchprofile"
	"foliospace-reader/internal/store"
)

const fbNeoCoreSHA256 = "6ebc2675c272c8d654935647ac336d45bbd97452c4d5943290d5ffc75678d9f1"

type rebuildOutput struct {
	Policy          string                                `json:"policy"`
	DATName         string                                `json:"datName"`
	DATVersion      string                                `json:"datVersion"`
	DATSHA256       string                                `json:"datSha256"`
	ProfileRevision int                                   `json:"profileRevision"`
	Candidates      int                                   `json:"candidates"`
	Matched         int                                   `json:"matched"`
	Result          domain.GameLaunchProfileRebuildResult `json:"result"`
	Failures        []string                              `json:"failures,omitempty"`
	DryRun          bool                                  `json:"dryRun"`
}

func main() {
	cfg := config.Load()
	datPath := flag.String("dat", filepath.Join(cfg.ConfigDir, "policies", "fbneo-arcade.dat"), "official FBNeo arcade DAT path")
	policy := flag.String("policy", "fbneo", "audit policy: fbneo or mame")
	mameListXML := flag.String("mame-listxml", filepath.Join(cfg.ConfigDir, "policies", "mame0288lx.zip"), "official MAME 0.288 listxml XML or ZIP path")
	platforms := flag.String("platforms", "model2", "comma-separated platforms to audit with MAME")
	targetsPath := flag.String("targets", "", "JSON file containing exact client runtime targets; defaults to the Windows release target")
	gameID := flag.Int64("game-id", 0, "audit and replace profiles only for this game ID")
	dryRun := flag.Bool("dry-run", false, "audit without writing SQLite")
	failureLimit := flag.Int("failure-limit", 50, "maximum failure details to print")
	flag.Parse()
	if *gameID < 0 {
		log.Fatal("game-id cannot be negative")
	}

	conn, err := db.Open(cfg.ConfigDir)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	appStore := store.New(conn)
	candidates, err := appStore.ListGameLaunchAuditCandidates()
	if err != nil {
		log.Fatal(err)
	}
	var output rebuildOutput
	selectedPolicy := strings.ToLower(strings.TrimSpace(*policy))
	targets, err := loadLaunchProfileTargets(*targetsPath, selectedPolicy)
	if err != nil {
		log.Fatal(err)
	}
	switch selectedPolicy {
	case "fbneo":
		catalog, err := launchprofile.ParseFBNeoDATFile(*datPath)
		if err != nil {
			log.Fatal(err)
		}
		output, err = rebuildFBNeoProfiles(appStore, catalog, candidates, targets, *gameID, *dryRun)
	case "mame":
		output, err = rebuildMAMEProfiles(appStore, *mameListXML, parsePlatformSelection(*platforms), candidates, targets, *gameID, *dryRun)
	default:
		log.Fatalf("unsupported audit policy %q", *policy)
	}
	if err != nil {
		log.Fatal(err)
	}
	if *failureLimit >= 0 && len(output.Failures) > *failureLimit {
		output.Failures = output.Failures[:*failureLimit]
	}
	encoded, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(encoded))
}

func rebuildFBNeoProfiles(appStore *store.Store, catalog launchprofile.FBNeoCatalog, candidates []domain.GameAsset, targets []launchProfileTarget, gameID int64, dryRun bool) (rebuildOutput, error) {
	bySet := make(map[string][]domain.GameAsset)
	for _, candidate := range candidates {
		setName := canonicalSetName(candidate.FilePath)
		if setName != "" {
			bySet[setName] = append(bySet[setName], candidate)
		}
	}
	profiles := make([]domain.GameLaunchProfile, 0, len(candidates))
	updates := make([]domain.GameLaunchCatalogUpdate, 0, len(candidates))
	failures := make([]string, 0)
	matched := 0
	scoped := 0
	for _, candidate := range candidates {
		if gameID > 0 && candidate.ID != gameID {
			continue
		}
		if !eligibleFBNeoCandidate(candidate) || isKnownDependency(candidate) {
			continue
		}
		scoped++
		setName := canonicalSetName(candidate.FilePath)
		datGame, ok := catalog.Games[setName]
		if !ok {
			continue
		}
		matched++
		update := domain.GameLaunchCatalogUpdate{
			GameID: candidate.ID, Platform: datGame.Platform(), ROMSetName: setName,
			EmulatorHint: "fbneo", CatalogRole: launchcatalog.RoleNeedsCuration,
		}
		profile, auditErr := buildFBNeoProfile(catalog, datGame, candidate, bySet, targets[0])
		if auditErr == nil {
			profiles = append(profiles, profile)
			for _, target := range targets[1:] {
				profiles = append(profiles, applyFBNeoTarget(profile, target, catalog, datGame))
			}
			update.CatalogRole = launchcatalog.RoleGame
		} else {
			failures = append(failures, fmt.Sprintf("game=%d set=%s: %v", candidate.ID, setName, auditErr))
		}
		updates = append(updates, update)
	}
	sort.Slice(profiles, func(i, j int) bool {
		if profiles[i].GameID == profiles[j].GameID {
			return profiles[i].ID < profiles[j].ID
		}
		return profiles[i].GameID < profiles[j].GameID
	})
	sort.Slice(updates, func(i, j int) bool { return updates[i].GameID < updates[j].GameID })

	result := domain.GameLaunchProfileRebuildResult{ProfilesWritten: len(profiles)}
	readyGames := make(map[int64]bool)
	for _, profile := range profiles {
		result.FilesWritten += len(profile.Files)
		readyGames[profile.GameID] = true
	}
	result.GamesReady = len(readyGames)
	result.GamesRejected = len(updates) - result.GamesReady
	if gameID > 0 && len(updates) == 0 {
		return rebuildOutput{}, fmt.Errorf("game %d is not matched by the selected FBNeo policy", gameID)
	}
	if !dryRun {
		var written domain.GameLaunchProfileRebuildResult
		var err error
		if gameID > 0 {
			written, err = appStore.ReplaceGameLaunchProfilesForGame(launchprofile.FBNeoPolicy, gameID, profiles, updates)
		} else {
			written, err = appStore.ReplaceGameLaunchProfiles(launchprofile.FBNeoPolicy, profiles, updates)
		}
		if err != nil {
			return rebuildOutput{}, err
		}
		result = written
	}
	return rebuildOutput{
		Policy: launchprofile.FBNeoPolicy, DATName: catalog.Name, DATVersion: catalog.Version,
		DATSHA256: catalog.SHA256, ProfileRevision: catalog.Revision,
		Candidates: scoped, Matched: matched, Result: result, Failures: failures, DryRun: dryRun,
	}, nil
}

func rebuildMAMEProfiles(appStore *store.Store, listXMLPath string, platforms map[string]bool, candidates []domain.GameAsset, targets []launchProfileTarget, gameID int64, dryRun bool) (rebuildOutput, error) {
	if len(platforms) == 0 {
		return rebuildOutput{}, fmt.Errorf("MAME platform selection is empty")
	}
	bySet := make(map[string][]domain.GameAsset)
	requested := make([]string, 0)
	scoped := make([]domain.GameAsset, 0)
	for _, candidate := range candidates {
		setName := canonicalSetName(candidate.FilePath)
		if setName != "" {
			bySet[setName] = append(bySet[setName], candidate)
		}
		if platforms[strings.ToLower(strings.TrimSpace(candidate.Platform))] && !isKnownDependency(candidate) && (gameID == 0 || candidate.ID == gameID) {
			scoped = append(scoped, candidate)
			requested = append(requested, setName)
		}
	}
	catalog, err := launchprofile.ParseMAMEListXMLFile(listXMLPath, requested)
	if err != nil {
		return rebuildOutput{}, err
	}
	version := mameBuildVersion(catalog.Build)
	if version == "" {
		return rebuildOutput{}, fmt.Errorf("MAME listxml build %q has no usable version", catalog.Build)
	}
	policy := launchprofile.MAMEPolicyForVersion(version)

	profiles := make([]domain.GameLaunchProfile, 0, len(scoped))
	updates := make([]domain.GameLaunchCatalogUpdate, 0, len(scoped))
	failures := make([]string, 0)
	matched := 0
	for _, candidate := range scoped {
		setName := canonicalSetName(candidate.FilePath)
		update := domain.GameLaunchCatalogUpdate{
			GameID: candidate.ID, Platform: strings.ToLower(strings.TrimSpace(candidate.Platform)), ROMSetName: setName,
			EmulatorHint: mameEmulatorHint(candidate.Platform), CatalogRole: launchcatalog.RoleNeedsCuration,
		}
		machine, ok := catalog.Machines[setName]
		if !ok {
			failures = append(failures, fmt.Sprintf("game=%d set=%s: set is absent from MAME %s listxml", candidate.ID, setName, version))
			updates = append(updates, update)
			continue
		}
		matched++
		profile, auditErr := buildMAMEProfile(catalog, machine, candidate, bySet, targets[0], policy)
		if auditErr == nil {
			profiles = append(profiles, profile)
			for _, target := range targets[1:] {
				profiles = append(profiles, applyMAMETarget(profile, target, catalog, machine, policy))
			}
			update.CatalogRole = launchcatalog.RoleGame
		} else {
			failures = append(failures, fmt.Sprintf("game=%d set=%s: %v", candidate.ID, setName, auditErr))
		}
		updates = append(updates, update)
	}
	sort.Slice(profiles, func(i, j int) bool {
		if profiles[i].GameID == profiles[j].GameID {
			return profiles[i].ID < profiles[j].ID
		}
		return profiles[i].GameID < profiles[j].GameID
	})
	sort.Slice(updates, func(i, j int) bool { return updates[i].GameID < updates[j].GameID })

	result := domain.GameLaunchProfileRebuildResult{ProfilesWritten: len(profiles)}
	readyGames := make(map[int64]bool)
	for _, profile := range profiles {
		result.FilesWritten += len(profile.Files)
		readyGames[profile.GameID] = true
	}
	result.GamesReady = len(readyGames)
	result.GamesRejected = len(updates) - result.GamesReady
	if gameID > 0 && len(updates) == 0 {
		return rebuildOutput{}, fmt.Errorf("game %d is outside the selected MAME platforms", gameID)
	}
	if !dryRun {
		if gameID > 0 {
			result, err = appStore.ReplaceGameLaunchProfilesForGame(policy, gameID, profiles, updates)
		} else {
			result, err = appStore.ReplaceGameLaunchProfiles(policy, profiles, updates)
		}
		if err != nil {
			return rebuildOutput{}, err
		}
	}
	return rebuildOutput{
		Policy: policy, DATName: "MAME", DATVersion: version,
		DATSHA256: catalog.SHA256, ProfileRevision: catalog.Revision,
		Candidates: len(scoped), Matched: matched, Result: result, Failures: failures, DryRun: dryRun,
	}, nil
}

func buildFBNeoProfile(catalog launchprofile.FBNeoCatalog, datGame launchprofile.FBNeoGame, entry domain.GameAsset, bySet map[string][]domain.GameAsset, target launchProfileTarget) (domain.GameLaunchProfile, error) {
	if !validContainerIdentity(entry) {
		return domain.GameLaunchProfile{}, fmt.Errorf("entry container has no stable SHA-1 identity")
	}
	if err := launchprofile.ValidateFBNeoArchive(entry.FilePath, datGame); err != nil {
		return domain.GameLaunchProfile{}, err
	}
	files := []domain.GameLaunchProfileFile{{
		Position: 0, SourceGameID: entry.ID, SourceSHA1: strings.ToLower(entry.SHA1),
		SourceName: filepath.Base(entry.FilePath), Name: datGame.Name + ".zip", Size: entry.Size, Role: "entry",
	}}
	dependencies, err := catalog.Dependencies(datGame.Name)
	if err != nil {
		return domain.GameLaunchProfile{}, err
	}
	for _, dependency := range dependencies {
		source, err := selectDependencySource(entry, dependency, bySet[dependency.Name])
		if err != nil {
			return domain.GameLaunchProfile{}, err
		}
		files = append(files, domain.GameLaunchProfileFile{
			Position: len(files), SourceGameID: source.ID, SourceSHA1: strings.ToLower(source.SHA1),
			SourceName: filepath.Base(source.FilePath), Name: dependency.Name + ".zip", Size: source.Size, Role: "dependency",
		})
	}
	profile := domain.GameLaunchProfile{
		GameID:   entry.ID,
		Revision: catalog.Revision, Priority: 200, Policy: launchprofile.FBNeoPolicy,
		EntryFile: datGame.Name + ".zip", CanonicalSet: datGame.Name, Status: "ready", Files: files,
	}
	return applyFBNeoTarget(profile, target, catalog, datGame), nil
}

func buildMAMEProfile(catalog launchprofile.MAMECatalog, machine launchprofile.MAMEMachine, entry domain.GameAsset, bySet map[string][]domain.GameAsset, target launchProfileTarget, policy string) (domain.GameLaunchProfile, error) {
	if !validContainerIdentity(entry) {
		return domain.GameLaunchProfile{}, fmt.Errorf("entry container has no stable SHA-1 identity")
	}
	if !machine.Runnable || machine.IsBIOS || machine.IsDevice {
		return domain.GameLaunchProfile{}, fmt.Errorf("MAME machine is not a runnable game")
	}
	if err := launchprofile.ValidateMAMEArchive(entry.FilePath, machine); err != nil {
		return domain.GameLaunchProfile{}, err
	}
	files := []domain.GameLaunchProfileFile{{
		Position: 0, SourceGameID: entry.ID, SourceSHA1: strings.ToLower(entry.SHA1),
		SourceName: filepath.Base(entry.FilePath), Name: machine.Name + ".zip", Size: entry.Size, Role: "entry",
	}}
	dependencies, err := catalog.Dependencies(machine.Name)
	if err != nil {
		return domain.GameLaunchProfile{}, err
	}
	selfContainedClone := machine.CloneOf != "" && machine.ROMOf == machine.CloneOf &&
		launchprofile.ValidateMAMESelfContainedArchive(entry.FilePath, machine) == nil
	for _, dependency := range dependencies {
		if selfContainedClone && dependency.Name == machine.ROMOf {
			continue
		}
		if auditedEmbeddedMAMEDependency(entry, machine, dependency) {
			continue
		}
		source, err := selectMAMEDependencySource(entry, dependency, bySet[dependency.Name])
		if err != nil {
			return domain.GameLaunchProfile{}, err
		}
		files = append(files, domain.GameLaunchProfileFile{
			Position: len(files), SourceGameID: source.ID, SourceSHA1: strings.ToLower(source.SHA1),
			SourceName: filepath.Base(source.FilePath), Name: dependency.Name + ".zip", Size: source.Size, Role: "dependency",
		})
	}
	profile := domain.GameLaunchProfile{
		GameID: entry.ID, Revision: catalog.Revision, Priority: 200, Policy: policy,
		EntryFile: machine.Name + ".zip", CanonicalSet: machine.Name, Status: "ready", Files: files,
	}
	return applyMAMETarget(profile, target, catalog, machine, policy), nil
}

// auditedEmbeddedMAMEDependency records narrowly verified legacy packages that
// MAME can run without a separate device archive because the exact device ROM
// is already embedded in the entry ZIP. Keep this keyed to an immutable source
// fingerprint; do not turn a matching filename alone into a compatibility
// override.
func auditedEmbeddedMAMEDependency(entry domain.GameAsset, machine launchprofile.MAMEMachine, dependency launchprofile.MAMEMachine) bool {
	if machine.Name != "timecris" || dependency.Name != "namcoc71" ||
		entry.Size != 16292369 || !strings.EqualFold(strings.TrimSpace(entry.SHA1), "ee6d57977bd5b10f82292009755cd8d80b9e14f5") {
		return false
	}
	return launchprofile.ValidateMAMEArchive(entry.FilePath, dependency) == nil
}

func applyFBNeoTarget(profile domain.GameLaunchProfile, target launchProfileTarget, catalog launchprofile.FBNeoCatalog, game launchprofile.FBNeoGame) domain.GameLaunchProfile {
	profile.ID = fmt.Sprintf("%s-%s-fbneo-%s-%s", game.Name, target.ID, profileVersion(catalog.Version), catalog.SHA256[:8])
	profile.ClientName = target.ClientName
	profile.MinClientVersion = target.MinClientVersion
	profile.ClientPlatform = target.ClientPlatform
	profile.Architecture = target.Architecture
	profile.Runtime = domain.GameRuntimeDescriptor{ID: "libretro", CoreID: "fbneo", CoreBuildID: target.CoreBuildID, CoreSHA256: target.CoreSHA256}
	return profile
}

func applyMAMETarget(profile domain.GameLaunchProfile, target launchProfileTarget, catalog launchprofile.MAMECatalog, machine launchprofile.MAMEMachine, policy string) domain.GameLaunchProfile {
	version := mameBuildVersion(catalog.Build)
	profile.ID = fmt.Sprintf("%s-%s-mame-%s-%s", machine.Name, target.ID, profileVersion(version), catalog.SHA256[:8])
	profile.Policy = policy
	profile.ClientName = target.ClientName
	profile.MinClientVersion = target.MinClientVersion
	profile.ClientPlatform = target.ClientPlatform
	profile.Architecture = target.Architecture
	profile.Runtime = domain.GameRuntimeDescriptor{ID: "mame", Version: version, ContentSet: "mame-" + version}
	return profile
}

func selectDependencySource(entry domain.GameAsset, dependency launchprofile.FBNeoGame, candidates []domain.GameAsset) (domain.GameAsset, error) {
	sort.SliceStable(candidates, func(i, j int) bool {
		leftSameDir := filepath.Dir(candidates[i].FilePath) == filepath.Dir(entry.FilePath)
		rightSameDir := filepath.Dir(candidates[j].FilePath) == filepath.Dir(entry.FilePath)
		return leftSameDir && !rightSameDir
	})
	for _, candidate := range candidates {
		if !validContainerIdentity(candidate) {
			continue
		}
		if err := launchprofile.ValidateFBNeoArchive(candidate.FilePath, dependency); err == nil {
			return candidate, nil
		}
	}
	return domain.GameAsset{}, fmt.Errorf("verified dependency %s.zip is unavailable", dependency.Name)
}

func selectMAMEDependencySource(entry domain.GameAsset, dependency launchprofile.MAMEMachine, candidates []domain.GameAsset) (domain.GameAsset, error) {
	sort.SliceStable(candidates, func(i, j int) bool {
		leftSameDir := filepath.Dir(candidates[i].FilePath) == filepath.Dir(entry.FilePath)
		rightSameDir := filepath.Dir(candidates[j].FilePath) == filepath.Dir(entry.FilePath)
		return leftSameDir && !rightSameDir
	})
	for _, candidate := range candidates {
		if !validContainerIdentity(candidate) {
			continue
		}
		if err := launchprofile.ValidateMAMEArchive(candidate.FilePath, dependency); err == nil {
			return candidate, nil
		}
	}
	return domain.GameAsset{}, fmt.Errorf("verified MAME dependency %s.zip is unavailable", dependency.Name)
}

func canonicalSetName(path string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))))
}

func eligibleFBNeoCandidate(game domain.GameAsset) bool {
	if launchcatalog.IsStrictArcadePlatform(game.Platform) {
		return true
	}
	path := strings.ToLower(filepath.ToSlash(game.FilePath))
	return strings.Contains(path, "/fbneo/")
}

func isKnownDependency(game domain.GameAsset) bool {
	return strings.EqualFold(strings.TrimSpace(game.CatalogRole), launchcatalog.RoleDependency) ||
		launchcatalog.IsKnownDependencyFile(game.FilePath)
}

func validContainerIdentity(game domain.GameAsset) bool {
	sha1 := strings.ToLower(strings.TrimSpace(game.SHA1))
	if len(sha1) != 40 || game.Size <= 0 {
		return false
	}
	for _, char := range sha1 {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}

func profileVersion(version string) string {
	var builder strings.Builder
	for _, char := range strings.ToLower(strings.TrimSpace(version)) {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
			builder.WriteRune(char)
		}
	}
	if builder.Len() == 0 {
		return "dat"
	}
	return builder.String()
}

func parsePlatformSelection(value string) map[string]bool {
	selected := make(map[string]bool)
	for _, platform := range strings.Split(value, ",") {
		platform = strings.ToLower(strings.TrimSpace(platform))
		if platform != "" {
			selected[platform] = true
		}
	}
	return selected
}

func mameBuildVersion(build string) string {
	fields := strings.Fields(strings.TrimSpace(build))
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(fields[0]), "v")
}

func mameEmulatorHint(platform string) string {
	if strings.EqualFold(strings.TrimSpace(platform), "model2") {
		return "model2"
	}
	return "mame"
}
