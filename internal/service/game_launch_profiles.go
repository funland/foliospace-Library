package service

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/launchcatalog"
	"foliospace-reader/internal/naomi2catalog"
)

type RuntimeProfileNotAvailableError struct {
	GameID         int64
	RuntimeID      string
	RuntimeVersion string
}

type GameLaunchResolveError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func (e *GameLaunchResolveError) Error() string {
	return e.Message
}

func launchResolveError(code, message string, details map[string]any) *GameLaunchResolveError {
	return &GameLaunchResolveError{Code: code, Message: message, Details: details}
}

func (e *RuntimeProfileNotAvailableError) Error() string {
	runtimeName := strings.ToUpper(strings.TrimSpace(e.RuntimeID))
	if runtimeName == "" {
		runtimeName = "compatible runtime"
	}
	if strings.TrimSpace(e.RuntimeVersion) != "" {
		runtimeName += " " + strings.TrimSpace(e.RuntimeVersion)
	}
	return fmt.Sprintf("No %s profile is available for game %d.", runtimeName, e.GameID)
}

type auditedGameLaunchProfile struct {
	ID               string
	Revision         int
	Priority         int
	ClientName       string
	MinClientVersion string
	ClientPlatform   string
	Architecture     string
	Runtime          domain.GameRuntimeDescriptor
	EntrySHA1        string
	EntrySourceName  string
	Title            string
	ROMSetName       string
	Files            []auditedGameLaunchFile
}

type auditedGameLaunchFile struct {
	SourceSHA1 string
	SourceName string
	Name       string
	Size       int64
	Role       string
}

type auditedGameLaunchCandidate struct {
	Profile auditedGameLaunchProfile
	Runtime domain.GameRuntimeDescriptor
}

var sha1Pattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var coreBuildIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,255}$`)
var errAuditedLaunchSourceUnavailable = errors.New("audited launch source unavailable")

const (
	gameEMUAndroidFlycastCoreBuildID = "flycast-392a429-android-v4-arm64-gles3-hle-vmu-arcade-save-bundle"
	pointBlankAppleIOSCoreBuildID    = "fbneo:archive-f1d54ccd94b661434a38930591e3697b89165a5946c45eff98f60d3981fd7b6c:ios-arm64:full-v1"
	pointBlankAppleXROSCoreBuildID   = "fbneo:archive-a161e273b161dc77fad5acc449798987f89741f0f75da1f05bec4ff7b6b75181:xros-arm64:full-localized-v1"
)

var atomiswaveBIOSLaunchFile = auditedGameLaunchFile{
	SourceSHA1: "cdf247154e28c4b352b962a4a523587f2fde9305",
	SourceName: "awbios.zip",
	Name:       "awbios.zip",
	Size:       34620,
	Role:       "dependency",
}

var auditedGameLaunchProfiles = []auditedGameLaunchProfile{
	{
		ID:               "vstriker-windows-mame0288-v1",
		Revision:         1,
		Priority:         100,
		ClientName:       "SpatialEMU.Windows",
		MinClientVersion: "1.302",
		ClientPlatform:   "windows-x64",
		Architecture:     "x64",
		Runtime:          domain.GameRuntimeDescriptor{ID: "mame", Version: "0.288", ContentSet: "mame-0.288"},
		EntrySHA1:        "8e3518318eeb157ab299b2f284faef176d3f49dd",
		EntrySourceName:  "vstriker.zip",
		Title:            "Virtua Striker",
		ROMSetName:       "vstriker",
		Files: []auditedGameLaunchFile{
			{SourceSHA1: "8e3518318eeb157ab299b2f284faef176d3f49dd", SourceName: "vstriker.zip", Name: "vstriker.zip", Size: 10313686, Role: "entry"},
			{SourceSHA1: "4631db7f7f5160a3a6591d3102722be869710f66", SourceName: "segabill.zip", Name: "segabill.zip", Size: 3117, Role: "dependency"},
		},
	},
	{
		ID:               "tektagtc1a-windows-mame0288-v1",
		Revision:         1,
		Priority:         100,
		ClientName:       "SpatialEMU.Windows",
		MinClientVersion: "1.302",
		ClientPlatform:   "windows-x64",
		Architecture:     "x64",
		Runtime:          domain.GameRuntimeDescriptor{ID: "mame", Version: "0.288", ContentSet: "mame-0.288"},
		EntrySHA1:        "d6615a3a70ea9941b61ccd608054a0044d3d6ab3",
		EntrySourceName:  "tektagtac1.zip",
		Title:            "Tekken Tag Tournament (World, TEG2/VER.C1, set 2)",
		ROMSetName:       "tektagtc1a",
		Files: []auditedGameLaunchFile{
			{SourceSHA1: "d6615a3a70ea9941b61ccd608054a0044d3d6ab3", SourceName: "tektagtac1.zip", Name: "tektagtc1a.zip", Size: 120980600, Role: "entry"},
		},
	},
	{
		ID:               "sf2-windows-fbneo-v1",
		Revision:         1,
		Priority:         100,
		ClientName:       "SpatialEMU.Windows",
		MinClientVersion: "1.302",
		ClientPlatform:   "windows-x64",
		Architecture:     "x64",
		Runtime:          domain.GameRuntimeDescriptor{ID: "libretro", CoreID: "fbneo", CoreSHA256: "6ebc2675c272c8d654935647ac336d45bbd97452c4d5943290d5ffc75678d9f1"},
		EntrySHA1:        "bd59872a57f14dc492e2fb387727a9402f3d4f97",
		EntrySourceName:  "sf2.zip",
		ROMSetName:       "sf2",
		Files: []auditedGameLaunchFile{
			{SourceSHA1: "bd59872a57f14dc492e2fb387727a9402f3d4f97", SourceName: "sf2.zip", Name: "sf2.zip", Size: 3551819, Role: "entry"},
		},
	},
	{
		ID:               "sfa-windows-fbneo-v1",
		Revision:         1,
		Priority:         100,
		ClientName:       "SpatialEMU.Windows",
		MinClientVersion: "1.302",
		ClientPlatform:   "windows-x64",
		Architecture:     "x64",
		Runtime:          domain.GameRuntimeDescriptor{ID: "libretro", CoreID: "fbneo", CoreSHA256: "6ebc2675c272c8d654935647ac336d45bbd97452c4d5943290d5ffc75678d9f1"},
		EntrySHA1:        "61dece364b8d2f2ff15391505168be334ebb371a",
		EntrySourceName:  "sfa.zip",
		ROMSetName:       "sfa",
		Files: []auditedGameLaunchFile{
			{SourceSHA1: "61dece364b8d2f2ff15391505168be334ebb371a", SourceName: "sfa.zip", Name: "sfa.zip", Size: 7365582, Role: "entry"},
		},
	},
	{
		ID:               "sfiii-windows-fbneo-v1",
		Revision:         1,
		Priority:         100,
		ClientName:       "SpatialEMU.Windows",
		MinClientVersion: "1.302",
		ClientPlatform:   "windows-x64",
		Architecture:     "x64",
		Runtime:          domain.GameRuntimeDescriptor{ID: "libretro", CoreID: "fbneo", CoreSHA256: "6ebc2675c272c8d654935647ac336d45bbd97452c4d5943290d5ffc75678d9f1"},
		EntrySHA1:        "7aae0cfc4ef8911f19d2e986cee63807deebf1b6",
		EntrySourceName:  "sfiii.zip",
		ROMSetName:       "sfiii",
		Files: []auditedGameLaunchFile{
			{SourceSHA1: "7aae0cfc4ef8911f19d2e986cee63807deebf1b6", SourceName: "sfiii.zip", Name: "sfiii.zip", Size: 38868517, Role: "entry"},
		},
	},
	{
		ID:               "hypreact-windows-mame0288-v1",
		Revision:         1,
		Priority:         100,
		ClientName:       "SpatialEMU.Windows",
		MinClientVersion: "1.302",
		ClientPlatform:   "windows-x64",
		Architecture:     "x64",
		Runtime:          domain.GameRuntimeDescriptor{ID: "mame", Version: "0.288", ContentSet: "mame-0.288"},
		EntrySHA1:        "e0940f848884c9d53bbc41bb947d584e06cc1845",
		EntrySourceName:  "hypreact.zip",
		ROMSetName:       "hypreact",
		Files: []auditedGameLaunchFile{
			{SourceSHA1: "e0940f848884c9d53bbc41bb947d584e06cc1845", SourceName: "hypreact.zip", Name: "hypreact.zip", Size: 8052342, Role: "entry"},
		},
	},
	{
		ID:               "hypreac2-windows-mame0288-v1",
		Revision:         1,
		Priority:         100,
		ClientName:       "SpatialEMU.Windows",
		MinClientVersion: "1.302",
		ClientPlatform:   "windows-x64",
		Architecture:     "x64",
		Runtime:          domain.GameRuntimeDescriptor{ID: "mame", Version: "0.288", ContentSet: "mame-0.288"},
		EntrySHA1:        "7fe73cc7ee40a49225a4616106e538c084ef4364",
		EntrySourceName:  "hypreac2.zip",
		ROMSetName:       "hypreac2",
		Files: []auditedGameLaunchFile{
			{SourceSHA1: "7fe73cc7ee40a49225a4616106e538c084ef4364", SourceName: "hypreac2.zip", Name: "hypreac2.zip", Size: 18291541, Role: "entry"},
		},
	},
	{
		ID:               "srmp4-windows-mame0288-v1",
		Revision:         1,
		Priority:         100,
		ClientName:       "SpatialEMU.Windows",
		MinClientVersion: "1.302",
		ClientPlatform:   "windows-x64",
		Architecture:     "x64",
		Runtime:          domain.GameRuntimeDescriptor{ID: "mame", Version: "0.288", ContentSet: "mame-0.288"},
		EntrySHA1:        "cfcf2cdf61ebca862a84473a8bf75fbe8d76cb7b",
		EntrySourceName:  "srmp4.zip",
		ROMSetName:       "srmp4",
		Files: []auditedGameLaunchFile{
			{SourceSHA1: "cfcf2cdf61ebca862a84473a8bf75fbe8d76cb7b", SourceName: "srmp4.zip", Name: "srmp4.zip", Size: 7697767, Role: "entry"},
		},
	},
	{
		ID:               "fromancr-windows-mame0288-v1",
		Revision:         1,
		Priority:         100,
		ClientName:       "SpatialEMU.Windows",
		MinClientVersion: "1.302",
		ClientPlatform:   "windows-x64",
		Architecture:     "x64",
		Runtime:          domain.GameRuntimeDescriptor{ID: "mame", Version: "0.288", ContentSet: "mame-0.288"},
		EntrySHA1:        "137e4949d7e204ff10e33372528cc1e9481b962c",
		EntrySourceName:  "fromancr.zip",
		ROMSetName:       "fromancr",
		Files: []auditedGameLaunchFile{
			{SourceSHA1: "137e4949d7e204ff10e33372528cc1e9481b962c", SourceName: "fromancr.zip", Name: "fromancr.zip", Size: 14121810, Role: "entry"},
		},
	},
	{
		ID:               "fromanc4-windows-mame0288-v1",
		Revision:         1,
		Priority:         100,
		ClientName:       "SpatialEMU.Windows",
		MinClientVersion: "1.302",
		ClientPlatform:   "windows-x64",
		Architecture:     "x64",
		Runtime:          domain.GameRuntimeDescriptor{ID: "mame", Version: "0.288", ContentSet: "mame-0.288"},
		EntrySHA1:        "ff478f3350d9703e8647f659ce169ee234082249",
		EntrySourceName:  "fromanc4.zip",
		ROMSetName:       "fromanc4",
		Files: []auditedGameLaunchFile{
			{SourceSHA1: "ff478f3350d9703e8647f659ce169ee234082249", SourceName: "fromanc4.zip", Name: "fromanc4.zip", Size: 21443327, Role: "entry"},
		},
	},
	{
		ID:               "mcnpshnt-windows-mame0288-v1",
		Revision:         1,
		Priority:         100,
		ClientName:       "SpatialEMU.Windows",
		MinClientVersion: "1.302",
		ClientPlatform:   "windows-x64",
		Architecture:     "x64",
		Runtime:          domain.GameRuntimeDescriptor{ID: "mame", Version: "0.288", ContentSet: "mame-0.288"},
		EntrySHA1:        "24a714371a867db1709798a95a171778e0940021",
		EntrySourceName:  "mcnpshnt.zip",
		ROMSetName:       "mcnpshnt",
		Files: []auditedGameLaunchFile{
			{SourceSHA1: "24a714371a867db1709798a95a171778e0940021", SourceName: "mcnpshnt.zip", Name: "mcnpshnt.zip", Size: 1205007, Role: "entry"},
			{SourceSHA1: "cbcd6e0698026452bb2bb6a6e6f7f5a3667a675c", SourceName: "ym2413_instruments.zip", Name: "ym2413.zip", Size: 322, Role: "dependency"},
		},
	},
}

func init() {
	auditedGameLaunchProfiles = append(auditedGameLaunchProfiles, pointBlankAppleLaunchProfiles()...)
}

func pointBlankAppleLaunchProfiles() []auditedGameLaunchProfile {
	targets := []struct {
		id         string
		clientName string
		platform   string
		buildID    string
	}{
		{id: "ios", clientName: "SpatialEMU.iOS", platform: "ios-arm64", buildID: pointBlankAppleIOSCoreBuildID},
		{id: "ipados", clientName: "SpatialEMU.iPadOS", platform: "ipados-arm64", buildID: pointBlankAppleIOSCoreBuildID},
		{id: "visionos", clientName: "SpatialEMU.visionOS", platform: "visionos-arm64", buildID: pointBlankAppleXROSCoreBuildID},
	}
	games := []struct {
		id        string
		title     string
		entrySHA1 string
		files     []auditedGameLaunchFile
	}{
		{
			id: "ptblank", title: "Point Blank", entrySHA1: "15f9dd6ccf009bffcb156b234bdeadbe26344314",
			files: []auditedGameLaunchFile{
				{SourceSHA1: "15f9dd6ccf009bffcb156b234bdeadbe26344314", SourceName: "ptblank.zip", Name: "ptblank.zip", Size: 5033400, Role: "entry"},
				{SourceSHA1: "0649e27b7d605add7fc4215ee628b71e3c835328", SourceName: "namcoc75.zip", Name: "namcoc75.zip", Size: 8709, Role: "dependency"},
			},
		},
		{
			id: "ptblanka", title: "Point Blank (Japan)", entrySHA1: "ee3e54a9f49bfc7c27f3e0c6ad580bf78d04d1e2",
			files: []auditedGameLaunchFile{
				{SourceSHA1: "ee3e54a9f49bfc7c27f3e0c6ad580bf78d04d1e2", SourceName: "ptblanka.zip", Name: "ptblanka.zip", Size: 131248, Role: "entry"},
				{SourceSHA1: "15f9dd6ccf009bffcb156b234bdeadbe26344314", SourceName: "ptblank.zip", Name: "ptblank.zip", Size: 5033400, Role: "dependency"},
				{SourceSHA1: "0649e27b7d605add7fc4215ee628b71e3c835328", SourceName: "namcoc75.zip", Name: "namcoc75.zip", Size: 8709, Role: "dependency"},
			},
		},
	}
	profiles := make([]auditedGameLaunchProfile, 0, len(targets)*len(games))
	for _, game := range games {
		for _, target := range targets {
			profiles = append(profiles, auditedGameLaunchProfile{
				ID: game.id + "-" + target.id + "-fbneo-lightgun2p-v1", Revision: 1, Priority: 250,
				ClientName: target.clientName, MinClientVersion: "1.300", ClientPlatform: target.platform, Architecture: "arm64",
				Runtime:   domain.GameRuntimeDescriptor{ID: "libretro", CoreID: "fbneo", CoreBuildID: target.buildID},
				EntrySHA1: game.entrySHA1, EntrySourceName: game.id + ".zip", Title: game.title, ROMSetName: game.id, Files: game.files,
			})
		}
	}
	return profiles
}

func ValidateGameLaunchResolveRequest(req domain.GameLaunchResolveRequest) error {
	for name, value := range map[string]string{
		"client.name": req.Client.Name, "client.version": req.Client.Version,
		"client.platform": req.Client.Platform, "client.architecture": req.Client.Architecture,
	} {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 128 {
			return fmt.Errorf("%s must contain 1 to 128 characters", name)
		}
	}
	if len(req.Runtimes) == 0 || len(req.Runtimes) > 16 {
		return errors.New("runtimes must contain 1 to 16 descriptors")
	}
	for index, runtime := range req.Runtimes {
		if strings.TrimSpace(runtime.ID) == "" || len(strings.TrimSpace(runtime.ID)) > 128 {
			return fmt.Errorf("runtimes[%d].id must contain 1 to 128 characters", index)
		}
		for name, value := range map[string]string{
			"version": runtime.Version, "contentSet": runtime.ContentSet,
			"coreId": runtime.CoreID, "coreSha256": runtime.CoreSHA256,
		} {
			if len(strings.TrimSpace(value)) > 128 {
				return fmt.Errorf("runtimes[%d].%s must not exceed 128 characters", index, name)
			}
		}
		if runtime.CoreSHA256 != "" && !sha256Pattern.MatchString(runtime.CoreSHA256) {
			return fmt.Errorf("runtimes[%d].coreSha256 must be 64 lowercase hexadecimal characters", index)
		}
		if len(strings.TrimSpace(runtime.CoreBuildID)) > 256 {
			return fmt.Errorf("runtimes[%d].coreBuildId must not exceed 256 characters", index)
		}
	}
	return nil
}

func (s *Service) ResolveGameLaunchProfile(gameID int64, req domain.GameLaunchResolveRequest) (domain.GameLaunchResolution, error) {
	if err := ValidateGameLaunchResolveRequest(req); err != nil {
		return domain.GameLaunchResolution{}, err
	}
	game, err := s.store.GameByID(gameID)
	if err != nil {
		return domain.GameLaunchResolution{}, err
	}
	if err := validateAuditedGameLaunchProfiles(); err != nil {
		return domain.GameLaunchResolution{}, err
	}

	var requestedRuntime domain.GameRuntimeDescriptor
	if len(req.Runtimes) > 0 {
		requestedRuntime = req.Runtimes[0]
	}
	persistedProfiles, err := s.store.GameLaunchProfiles(game.ID)
	if err != nil {
		return domain.GameLaunchResolution{}, err
	}
	missingDependency := ""
	for _, profile := range persistedProfiles {
		runtime, ok := matchingPersistedRuntime(profile, req)
		if !ok || len(profile.Files) == 0 {
			continue
		}
		resolvedFiles := make([]domain.GameLaunchResolvedFile, 0, len(profile.Files))
		var totalSize int64
		available := true
		for _, file := range profile.Files {
			source, sourceErr := s.store.GameByID(file.SourceGameID)
			if sourceErr != nil || source.Size != file.Size ||
				!strings.EqualFold(strings.TrimSpace(source.SHA1), file.SourceSHA1) ||
				!strings.EqualFold(filepath.Base(source.FilePath), file.SourceName) {
				available = false
				break
			}
			if info, statErr := os.Stat(source.FilePath); statErr != nil || !info.Mode().IsRegular() || info.Size() != source.Size {
				missingDependency = file.Name
				available = false
				break
			}
			resolvedFiles = append(resolvedFiles, domain.GameLaunchResolvedFile{
				SourceGameID: source.ID, Name: file.Name, Size: file.Size, Role: file.Role, SHA1: file.SourceSHA1,
			})
			totalSize += file.Size
		}
		if !available {
			continue
		}
		resolvedFiles, err = s.appendAutomaticGameDependencies(game, resolvedFiles, req.Client)
		if err != nil {
			missingDependency = atomiswaveBIOSLaunchFile.Name
			continue
		}
		totalSize = resolvedLaunchFileTotalSize(resolvedFiles)
		resolvedGame := resolvedGameForClient(game, req.Client)
		resolvedGame.ROMSetName = profile.CanonicalSet
		resolvedGame.Size = totalSize
		return domain.GameLaunchResolution{
			LaunchProfileID: profile.ID, ProfileRevision: profile.Revision, Runtime: runtime,
			Game: resolvedGame, EntryFile: profile.EntryFile, Files: resolvedFiles,
		}, nil
	}
	for _, candidate := range matchingAuditedLaunchCandidates(auditedGameLaunchProfiles, game, req) {
		profile := candidate.Profile
		resolvedFiles := make([]domain.GameLaunchResolvedFile, 0, len(profile.Files))
		var totalSize int64
		candidateAvailable := true
		for _, file := range profile.Files {
			source, sourceErr := s.resolveAuditedLaunchSource(file)
			if sourceErr != nil {
				if errors.Is(sourceErr, errAuditedLaunchSourceUnavailable) {
					missingDependency = file.Name
					candidateAvailable = false
					break
				}
				return domain.GameLaunchResolution{}, sourceErr
			}
			if info, statErr := os.Stat(source.FilePath); statErr != nil || !info.Mode().IsRegular() || info.Size() != source.Size {
				missingDependency = file.Name
				candidateAvailable = false
				break
			}
			resolvedFiles = append(resolvedFiles, domain.GameLaunchResolvedFile{
				SourceGameID: source.ID, Name: file.Name, Size: file.Size, Role: file.Role, SHA1: file.SourceSHA1,
			})
			totalSize += file.Size
		}
		if !candidateAvailable {
			continue
		}
		resolvedFiles, err = s.appendAutomaticGameDependencies(game, resolvedFiles, req.Client)
		if err != nil {
			missingDependency = atomiswaveBIOSLaunchFile.Name
			continue
		}
		totalSize = resolvedLaunchFileTotalSize(resolvedFiles)
		resolvedGame := resolvedGameForClient(game, req.Client)
		if strings.TrimSpace(profile.Title) != "" {
			resolvedGame.Title = profile.Title
		}
		resolvedGame.ROMSetName = profile.ROMSetName
		resolvedGame.Size = totalSize
		return domain.GameLaunchResolution{
			LaunchProfileID: profile.ID, ProfileRevision: profile.Revision, Runtime: candidate.Runtime,
			Game: resolvedGame, EntryFile: profile.Files[0].Name, Files: resolvedFiles,
		}, nil
	}
	if missingDependency != "" {
		return domain.GameLaunchResolution{}, launchResolveError(
			"dependency-missing", "A required audited launch file is unavailable.",
			map[string]any{"gameId": game.ID, "file": missingDependency},
		)
	}
	if runtime, ok := matchingPragmaticRuntime(game, req); ok {
		return s.resolvePragmaticGameLaunch(game, runtime, req.Client)
	}
	for _, runtime := range req.Runtimes {
		if strings.EqualFold(runtime.ID, "mame") {
			requestedRuntime = runtime
			break
		}
	}
	return domain.GameLaunchResolution{}, classifyLaunchResolveFailure(game, req, persistedProfiles, requestedRuntime)
}

// LegacyGameLaunchDependencies returns audited dependency archives for clients
// that still consume the manifest endpoint instead of the launch resolver.
func (s *Service) LegacyGameLaunchDependencies(gameID int64) ([]domain.GameLaunchResolvedFile, error) {
	game, err := s.store.GameByID(gameID)
	if err != nil {
		return nil, err
	}
	profiles, err := s.store.GameLaunchProfiles(gameID)
	if err != nil {
		return nil, err
	}
	dependencies := make([]domain.GameLaunchResolvedFile, 0, 2)
	for _, profile := range legacyLaunchProfilesByPreference(profiles, game.EmulatorHint) {
		files, available := s.availablePersistedLaunchProfileFiles(profile)
		if !available {
			continue
		}
		for _, file := range files {
			if strings.EqualFold(strings.TrimSpace(file.Role), "dependency") {
				dependencies = append(dependencies, file)
			}
		}
		if len(dependencies) > 0 {
			break
		}
	}
	return s.appendAutomaticGameDependencies(game, dependencies, domain.GameLaunchClient{})
}

func legacyLaunchProfilesByPreference(profiles []domain.GameLaunchProfile, emulatorHint string) []domain.GameLaunchProfile {
	preferred := make([]domain.GameLaunchProfile, 0, len(profiles))
	fallback := make([]domain.GameLaunchProfile, 0, len(profiles))
	hint := strings.ToLower(strings.TrimSpace(emulatorHint))
	for _, profile := range profiles {
		runtimeID := strings.ToLower(strings.TrimSpace(profile.Runtime.ID))
		coreID := strings.ToLower(strings.TrimSpace(profile.Runtime.CoreID))
		if hint != "" && (runtimeID == hint || coreID == hint) {
			preferred = append(preferred, profile)
		} else {
			fallback = append(fallback, profile)
		}
	}
	return append(preferred, fallback...)
}

func (s *Service) availablePersistedLaunchProfileFiles(profile domain.GameLaunchProfile) ([]domain.GameLaunchResolvedFile, bool) {
	if len(profile.Files) == 0 {
		return nil, false
	}
	resolved := make([]domain.GameLaunchResolvedFile, 0, len(profile.Files))
	for _, file := range profile.Files {
		source, err := s.store.GameByID(file.SourceGameID)
		if err != nil || source.Size != file.Size ||
			!strings.EqualFold(strings.TrimSpace(source.SHA1), strings.TrimSpace(file.SourceSHA1)) ||
			!strings.EqualFold(filepath.Base(source.FilePath), file.SourceName) {
			return nil, false
		}
		info, err := os.Stat(source.FilePath)
		if err != nil || !info.Mode().IsRegular() || info.Size() != source.Size {
			return nil, false
		}
		resolved = append(resolved, domain.GameLaunchResolvedFile{
			SourceGameID: source.ID,
			Name:         file.Name,
			Size:         file.Size,
			Role:         file.Role,
			SHA1:         file.SourceSHA1,
		})
	}
	return resolved, true
}

func matchingPersistedRuntime(profile domain.GameLaunchProfile, req domain.GameLaunchResolveRequest) (domain.GameRuntimeDescriptor, bool) {
	if !strings.EqualFold(strings.TrimSpace(req.Client.Name), profile.ClientName) ||
		!strings.EqualFold(strings.TrimSpace(req.Client.Platform), profile.ClientPlatform) ||
		!strings.EqualFold(strings.TrimSpace(req.Client.Architecture), profile.Architecture) ||
		!versionAtLeast(req.Client.Version, profile.MinClientVersion) {
		return domain.GameRuntimeDescriptor{}, false
	}
	for _, runtime := range req.Runtimes {
		if runtimeDescriptorMatches(runtime, profile.Runtime) {
			return runtime, true
		}
	}
	return domain.GameRuntimeDescriptor{}, false
}

func matchingAuditedLaunchCandidates(profiles []auditedGameLaunchProfile, game domain.GameAsset, req domain.GameLaunchResolveRequest) []auditedGameLaunchCandidate {
	candidates := make([]auditedGameLaunchCandidate, 0, 2)
	for _, profile := range profiles {
		runtime, ok := matchingRuntime(profile, req)
		if !ok || !strings.EqualFold(strings.TrimSpace(game.SHA1), profile.EntrySHA1) || game.Size != profile.Files[0].Size || !strings.EqualFold(filepath.Base(game.FilePath), profile.EntrySourceName) {
			continue
		}
		candidates = append(candidates, auditedGameLaunchCandidate{Profile: profile, Runtime: runtime})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Profile.Priority != candidates[j].Profile.Priority {
			return candidates[i].Profile.Priority > candidates[j].Profile.Priority
		}
		return candidates[i].Profile.ID < candidates[j].Profile.ID
	})
	return candidates
}

func (s *Service) resolvePragmaticGameLaunch(game domain.GameAsset, runtime domain.GameRuntimeDescriptor, client domain.GameLaunchClient) (domain.GameLaunchResolution, error) {
	files, err := s.GameFiles(game.ID)
	if err != nil {
		return domain.GameLaunchResolution{}, err
	}
	entryFile, err := validatePragmaticManifest(game, files)
	if err != nil {
		return domain.GameLaunchResolution{}, launchResolveError("dependency-missing", err.Error(), map[string]any{"gameId": game.ID})
	}
	for _, file := range files {
		info, statErr := os.Stat(file.FilePath)
		if statErr != nil || !info.Mode().IsRegular() {
			return domain.GameLaunchResolution{}, launchResolveError(
				"dependency-missing", "A required launch file is missing.",
				map[string]any{"gameId": game.ID, "file": file.Name},
			)
		}
		if gameFileSourceChanged(game, file, info) {
			return domain.GameLaunchResolution{}, launchResolveError(
				"manifest-checksum-unavailable", "A required launch file changed after its checksum was recorded.",
				map[string]any{"gameId": game.ID, "file": file.Name},
			)
		}
		checksum := strings.ToLower(strings.TrimSpace(file.SHA1))
		if !sha1Pattern.MatchString(checksum) {
			return domain.GameLaunchResolution{}, launchResolveError(
				"manifest-checksum-unavailable", "A required launch file has not been checksummed.",
				map[string]any{"gameId": game.ID, "file": file.Name},
			)
		}
	}

	var dosLaunch *domain.DOSLaunch
	if strings.EqualFold(game.Platform, "dos") {
		launch, launchErr := s.store.DOSLaunch(game.ID)
		if launchErr != nil || validatePragmaticDOSLaunch(launch) != nil {
			return domain.GameLaunchResolution{}, launchResolveError(
				"launch-profile-missing", "The DOS launch entry is not curated.", map[string]any{"gameId": game.ID},
			)
		}
		entryFile = launch.EntryFile
		dosLaunch = &launch
	}

	resolvedFiles := make([]domain.GameLaunchResolvedFile, 0, len(files))
	for _, file := range files {
		position := file.Position
		resolvedFiles = append(resolvedFiles, domain.GameLaunchResolvedFile{
			SourceGameID: game.ID, Position: &position, Name: file.Name,
			Size: file.Size, Role: file.Role, SHA1: strings.ToLower(strings.TrimSpace(file.SHA1)),
		})
	}
	resolvedFiles, err = s.appendAutomaticGameDependencies(game, resolvedFiles, client)
	if err != nil {
		return domain.GameLaunchResolution{}, launchResolveError(
			"dependency-missing", "A required launch file is missing.",
			map[string]any{"gameId": game.ID, "file": atomiswaveBIOSLaunchFile.Name},
		)
	}
	resolvedGame := resolvedGameForClient(game, client)
	resolvedGame.Size = resolvedLaunchFileTotalSize(resolvedFiles)

	return domain.GameLaunchResolution{
		LaunchProfileID: pragmaticLaunchProfileID(game, runtime, client),
		ProfileRevision: pragmaticProfileRevision(game, files, client),
		Runtime:         runtime, Game: resolvedGame, EntryFile: entryFile, Files: resolvedFiles, DOSLaunch: dosLaunch,
	}, nil
}

func (s *Service) appendAutomaticGameDependencies(game domain.GameAsset, files []domain.GameLaunchResolvedFile, client domain.GameLaunchClient) ([]domain.GameLaunchResolvedFile, error) {
	if !launchcatalog.RequiresAtomiswaveBIOS(game) || isGameEMUAndroidClient(client) {
		return files, nil
	}
	for _, file := range files {
		if strings.EqualFold(strings.TrimSpace(file.Name), atomiswaveBIOSLaunchFile.Name) {
			return files, nil
		}
	}
	source, err := s.resolveAuditedLaunchSource(atomiswaveBIOSLaunchFile)
	if err != nil {
		return nil, err
	}
	return append(files, domain.GameLaunchResolvedFile{
		SourceGameID: source.ID,
		Name:         atomiswaveBIOSLaunchFile.Name,
		Size:         atomiswaveBIOSLaunchFile.Size,
		Role:         atomiswaveBIOSLaunchFile.Role,
		SHA1:         atomiswaveBIOSLaunchFile.SourceSHA1,
	}), nil
}

func resolvedGameForClient(game domain.GameAsset, client domain.GameLaunchClient) domain.GameAsset {
	if !isGameEMUAndroidClient(client) || !launchcatalog.RequiresAtomiswaveBIOS(game) {
		return game
	}
	game.Platform = "atomiswave"
	game.EmulatorHint = "flycast"
	if strings.TrimSpace(game.ROMSetName) == "" {
		game.ROMSetName = strings.TrimSuffix(strings.ToLower(filepath.Base(game.FilePath)), filepath.Ext(game.FilePath))
	}
	return game
}

func isGameEMUAndroidClient(client domain.GameLaunchClient) bool {
	return strings.EqualFold(strings.TrimSpace(client.Name), "GameEMU.Android") &&
		strings.EqualFold(strings.TrimSpace(client.Platform), "android-arm64") &&
		strings.EqualFold(strings.TrimSpace(client.Architecture), "arm64")
}

func resolvedLaunchFileTotalSize(files []domain.GameLaunchResolvedFile) int64 {
	var total int64
	for _, file := range files {
		total += file.Size
	}
	return total
}

func matchingPragmaticRuntime(game domain.GameAsset, req domain.GameLaunchResolveRequest) (domain.GameRuntimeDescriptor, bool) {
	if !supportedPragmaticClient(req.Client) || isStrictArcadePlatform(game.Platform) || !isLaunchableCatalogGame(game) {
		return domain.GameRuntimeDescriptor{}, false
	}
	platform := normalizeLaunchPlatform(game.Platform)
	for _, runtime := range req.Runtimes {
		if pragmaticRuntimeSupportsPlatform(runtime, platform) && pragmaticRuntimeAllowedForClient(runtime, platform, req.Client) {
			// Windows 1.302 compares every field with the selected request tuple.
			return runtime, true
		}
	}
	return domain.GameRuntimeDescriptor{}, false
}

type pragmaticRuntimeRule struct {
	Platform   string
	RuntimeID  string
	CoreID     string
	MinVersion string
	ContentSet string
}

var pragmaticRuntimeRules = []pragmaticRuntimeRule{
	{Platform: "dreamcast", RuntimeID: "flycast", MinVersion: "2.6"},
	{Platform: "naomi", RuntimeID: "flycast", MinVersion: "2.6"},
	{Platform: "naomi2", RuntimeID: "flycast", MinVersion: "2.6"},
	{Platform: "model3", RuntimeID: "supermodel"},
	{Platform: "ngc", RuntimeID: "dolphin"},
	{Platform: "ps2", RuntimeID: "pcsx2", MinVersion: "2.6.3"},
	{Platform: "konami-python1", RuntimeID: "pcsx2-reliquary", MinVersion: "1.5.1.0", ContentSet: "konami-python1"},
	{Platform: "psp", RuntimeID: "ppsspp", MinVersion: "1.20.4"},
	{Platform: "psp", RuntimeID: "libretro", CoreID: "ppsspp"},
	{Platform: "dos", RuntimeID: "dosbox-staging", MinVersion: "0.82.2"},
	{Platform: "dos", RuntimeID: "libretro", CoreID: "dosboxpure"},
}

func supportedPragmaticClient(client domain.GameLaunchClient) bool {
	name := strings.ToLower(strings.TrimSpace(client.Name))
	platform := strings.ToLower(strings.TrimSpace(client.Platform))
	architecture := strings.ToLower(strings.TrimSpace(client.Architecture))
	switch name {
	case "spatialemu.windows":
		return platform == "windows-x64" && architecture == "x64" && versionAtLeast(client.Version, "1.302")
	case "spatialemu.macos":
		return (platform == "macos-arm64" && architecture == "arm64") ||
			(platform == "macos-x64" && architecture == "x64")
	case "spatialemu.ios":
		return platform == "ios-arm64" && architecture == "arm64"
	case "spatialemu.ipados":
		return platform == "ipados-arm64" && architecture == "arm64"
	case "spatialemu.visionos":
		return platform == "visionos-arm64" && architecture == "arm64"
	case "spatialemu.tvos":
		return platform == "tvos-arm64" && architecture == "arm64"
	case "gameemu.android":
		return platform == "android-arm64" && architecture == "arm64"
	default:
		return false
	}
}

func pragmaticRuntimeSupportsPlatform(runtime domain.GameRuntimeDescriptor, platform string) bool {
	runtimeID := strings.ToLower(strings.TrimSpace(runtime.ID))
	version := canonicalPragmaticVersion(runtime.Version)
	coreID := normalizeLaunchCoreID(runtime.CoreID)
	for _, rule := range pragmaticRuntimeRules {
		if rule.Platform != platform || rule.RuntimeID != runtimeID || (rule.CoreID != "" && rule.CoreID != coreID) ||
			(rule.ContentSet != "" && !strings.EqualFold(strings.TrimSpace(runtime.ContentSet), rule.ContentSet)) {
			continue
		}
		if rule.MinVersion != "" {
			return versionAtLeast(version, rule.MinVersion)
		}
		return optionalNumericVersion(version)
	}
	return runtimeID == "libretro" && ordinaryLibretroCoreSupportsPlatform(runtime.CoreID, platform)
}

func pragmaticRuntimeAllowedForClient(runtime domain.GameRuntimeDescriptor, platform string, client domain.GameLaunchClient) bool {
	runtimeID := strings.ToLower(strings.TrimSpace(runtime.ID))
	if isGameEMUAndroidClient(client) && runtimeID == "flycast" &&
		(platform == "dreamcast" || platform == "naomi" || platform == "naomi2") {
		return runtime.CoreBuildID == gameEMUAndroidFlycastCoreBuildID
	}
	if platform == "nds" {
		if runtimeID != "libretro" || normalizeLaunchCoreID(runtime.CoreID) != "melondsds" {
			return false
		}
		switch strings.ToLower(strings.TrimSpace(client.Name)) {
		case "spatialemu.ios", "spatialemu.ipados", "spatialemu.visionos":
			return true
		default:
			return false
		}
	}
	if platform == "konami-python1" {
		name := strings.ToLower(strings.TrimSpace(client.Name))
		return runtimeID == "pcsx2-reliquary" && (name == "spatialemu.windows" || name == "spatialemu.macos")
	}
	if isAppleMobileClient(client) {
		if (platform == "psp" || platform == "dos") && runtimeID != "libretro" {
			return false
		}
		if runtimeID == "libretro" && normalizeLaunchCoreID(runtime.CoreID) == "fbneo" &&
			!runtimeHasStableIdentity(runtime) {
			return false
		}
	}
	return true
}

func isAppleMobileClient(client domain.GameLaunchClient) bool {
	switch strings.ToLower(strings.TrimSpace(client.Name)) {
	case "spatialemu.ios", "spatialemu.ipados", "spatialemu.visionos", "spatialemu.tvos":
		return true
	default:
		return false
	}
}

func ordinaryLibretroCoreSupportsPlatform(coreID string, platform string) bool {
	core := normalizeLaunchCoreID(coreID)
	if core == "" || core == "fbneo" {
		return false
	}
	allowed := map[string]map[string]bool{
		"nes": {
			"nestopia": true, "nestopiaue": true, "mesen": true, "fceumm": true,
		},
		"snes": {
			"snes9x": true, "snes9xcurrent": true, "mesens": true, "bsnes": true, "bsnesmercury": true,
		},
		"md": {
			"genesisplusgx": true, "picodrive": true,
		},
		"32x": {
			"picodrive": true,
		},
		"gb": {
			"gambatte": true, "sameboy": true, "mesens": true,
		},
		"gbc": {
			"gambatte": true, "sameboy": true, "mesens": true,
		},
		"gba": {
			"mgba": true, "vbanext": true,
		},
		"nds": {
			"melondsds": true,
		},
		"3do": {
			"opera": true,
		},
		"ps1": {
			"swanstation": true, "beetlepsx": true, "beetlepsxhw": true, "pcsxrearmed": true,
		},
		"n64": {
			"mupen64plusnext": true, "mupen64plus": true, "paralleln64": true,
		},
		"saturn": {
			"beetlesaturn": true, "mednafensaturn": true, "yabasanshiro": true, "yabasanshiro2": true,
		},
		"pc-fx": {
			"beetlepcfx": true, "mednafenpcfx": true,
		},
		"pc98": {
			"np2kai": true, "nekoprojectiikai": true,
		},
	}
	return allowed[platform][core]
}

func normalizeLaunchCoreID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimSuffix(value, "_libretro")
	value = strings.TrimSuffix(value, "-libretro")
	var builder strings.Builder
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
			builder.WriteRune(char)
		}
	}
	return builder.String()
}

func normalizeLaunchPlatform(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "sfc":
		return "snes"
	case "megadrive", "mega-drive", "genesis":
		return "md"
	case "psx":
		return "ps1"
	case "gamecube", "game-cube":
		return "ngc"
	case "dc":
		return "dreamcast"
	case "pcfx":
		return "pc-fx"
	case "pc-98":
		return "pc98"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func isStrictArcadePlatform(value string) bool {
	switch normalizeLaunchPlatform(value) {
	case "mame", "arcade", "model2", "cps", "cps1", "cps2", "cps3", "neogeo":
		return true
	default:
		return false
	}
}

func isLaunchableCatalogGame(game domain.GameAsset) bool {
	role := strings.ToLower(strings.TrimSpace(game.CatalogRole))
	return role == "" || role == "game"
}

func optionalNumericVersion(value string) bool {
	if value == "" {
		return true
	}
	_, ok := numericVersion(value)
	return ok
}

func canonicalPragmaticVersion(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "v") || strings.HasPrefix(value, "V") {
		value = value[1:]
	}
	value = strings.ReplaceAll(value, ",", ".")
	value = strings.ReplaceAll(value, " ", "")
	parts, ok := numericVersion(value)
	if !ok {
		return ""
	}
	for len(parts) > 3 && parts[len(parts)-1] == 0 {
		parts = parts[:len(parts)-1]
	}
	text := make([]string, 0, len(parts))
	for _, part := range parts {
		text = append(text, strconv.Itoa(part))
	}
	return strings.Join(text, ".")
}

func validatePragmaticManifest(game domain.GameAsset, files []domain.GameFile) (string, error) {
	if len(files) == 0 {
		return "", errors.New("canonical manifest contains no files")
	}
	entryFile := ""
	entries := 0
	names := make(map[string]struct{}, len(files))
	positions := make(map[int]struct{}, len(files))
	for _, file := range files {
		if !validLaunchRelativePath(file.Name) || file.Size <= 0 || strings.TrimSpace(file.FilePath) == "" || file.Position < 0 {
			return "", fmt.Errorf("canonical manifest contains invalid file %q", file.Name)
		}
		nameKey := strings.ToLower(file.Name)
		if _, exists := names[nameKey]; exists {
			return "", fmt.Errorf("canonical manifest contains duplicate logical name %q", file.Name)
		}
		names[nameKey] = struct{}{}
		if _, exists := positions[file.Position]; exists {
			return "", fmt.Errorf("canonical manifest contains duplicate position %d", file.Position)
		}
		positions[file.Position] = struct{}{}
		switch strings.ToLower(strings.TrimSpace(file.Role)) {
		case "entry":
			entries++
			entryFile = file.Name
		case "dependency", "disk", "font":
		default:
			return "", fmt.Errorf("canonical manifest contains unsupported role %q", file.Role)
		}
	}
	if entries != 1 {
		return "", fmt.Errorf("canonical manifest requires exactly one entry, got %d", entries)
	}
	for position := 0; position < len(files); position++ {
		if _, exists := positions[position]; !exists {
			return "", fmt.Errorf("canonical manifest is missing position %d", position)
		}
	}
	if game.Size <= 0 {
		return "", errors.New("canonical manifest has invalid aggregate size")
	}
	if err := validateCatalogDependencyClosure(game, files); err != nil {
		return "", err
	}
	return entryFile, nil
}

func validateCatalogDependencyClosure(game domain.GameAsset, files []domain.GameFile) error {
	if normalizeLaunchPlatform(game.Platform) != "naomi2" {
		return nil
	}
	parent := naomi2catalog.Parent(game.ROMSetName)
	if parent == "" {
		return nil
	}

	wantEntry := strings.ToLower(strings.TrimSpace(game.ROMSetName)) + ".zip"
	wantParent := parent + ".zip"
	if len(files) != 2 {
		return fmt.Errorf("NAOMI 2 split set %s requires exactly entry %s and parent %s", game.ROMSetName, wantEntry, wantParent)
	}
	foundEntry := false
	foundParent := false
	for _, file := range files {
		name := strings.ToLower(strings.TrimSpace(file.Name))
		role := strings.ToLower(strings.TrimSpace(file.Role))
		switch {
		case name == wantEntry && role == "entry":
			foundEntry = true
		case name == wantParent && role == "dependency":
			foundParent = true
		default:
			return fmt.Errorf("NAOMI 2 split set %s contains unexpected launch file %s", game.ROMSetName, file.Name)
		}
	}
	if !foundEntry {
		return fmt.Errorf("NAOMI 2 split set %s is missing entry %s", game.ROMSetName, wantEntry)
	}
	if !foundParent {
		return fmt.Errorf("NAOMI 2 split set %s is missing parent ROM set %s", game.ROMSetName, wantParent)
	}
	return nil
}

func gameFileSourceChanged(game domain.GameAsset, file domain.GameFile, info os.FileInfo) bool {
	if !info.ModTime().Equal(file.MTime) {
		return true
	}
	if isVirtualGameFile(game, file) {
		return false
	}
	return info.Size() != file.Size
}

func isVirtualGameFile(game domain.GameAsset, file domain.GameFile) bool {
	format := strings.ToLower(strings.TrimSpace(game.Format))
	role := strings.ToLower(strings.TrimSpace(file.Role))
	if role == "entry" && (format == "cue" || format == "m3u") {
		return true
	}
	// ZIP-backed console entries describe the uncompressed ROM. The source path
	// points at the container, so its physical size cannot be compared with the
	// logical entry size recorded in the manifest.
	if role == "entry" && isZippedConsoleROM(game) {
		return true
	}
	if role == "entry" && isZippedThreeDSImage(game.Platform, game.FilePath, game.Format) {
		return true
	}
	if !strings.EqualFold(filepath.Ext(file.FilePath), ".zip") {
		return false
	}
	platform := normalizeLaunchPlatform(game.Platform)
	return platform == "n64" || platform == "pc98"
}

func validatePragmaticDOSLaunch(launch domain.DOSLaunch) error {
	source := strings.ToLower(strings.TrimSpace(launch.EntrySource))
	if source != "curated" && source != "dosboxconfig" {
		return fmt.Errorf("DOS launch entry source %q is not deterministic", launch.EntrySource)
	}
	if !validDOSLaunchPath(launch.EntryFile) || !isDOSExecutablePath(launch.EntryFile) {
		return errors.New("DOS launch entry is invalid")
	}
	for _, path := range []string{launch.InstallDirectory, launch.WorkingDirectory, launch.DOSBoxConfig} {
		if strings.TrimSpace(path) != "" && !validDOSLaunchPath(path) {
			return fmt.Errorf("DOS launch path %q is invalid", path)
		}
	}
	for _, candidate := range launch.Candidates {
		kind := strings.ToLower(strings.TrimSpace(candidate.Kind))
		// Candidates are metadata, not command lines. DOS archives legitimately
		// contain names such as C&C.EXE and (O)_(-).EXE; keep traversal checks,
		// while reserving shell metacharacter restrictions for executable fields.
		if !validLaunchRelativePath(candidate.Path) || (kind != "bat" && kind != "com" && kind != "exe") {
			return fmt.Errorf("DOS launch candidate %q is invalid", candidate.Path)
		}
	}
	for _, argument := range launch.Arguments {
		if strings.ContainsAny(argument, "\x00\r\n") || len(argument) > 512 {
			return errors.New("DOS launch argument is invalid")
		}
	}
	return nil
}

func validDOSLaunchPath(path string) bool {
	return validLaunchRelativePath(path) && !strings.ContainsAny(path, `|&;<>()`)
}

func isDOSExecutablePath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".bat", ".com", ".exe":
		return true
	default:
		return false
	}
}

func validLaunchRelativePath(name string) bool {
	if strings.Contains(name, `\`) {
		return false
	}
	name = strings.TrimSpace(name)
	if name == "" || strings.HasPrefix(name, "/") || filepath.IsAbs(name) || filepath.VolumeName(name) != "" || strings.ContainsRune(name, '\x00') {
		return false
	}
	if len(name) >= 2 && name[1] == ':' {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func pragmaticLaunchProfileID(game domain.GameAsset, runtime domain.GameRuntimeDescriptor, client domain.GameLaunchClient) string {
	key := fmt.Sprintf("%d\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s", game.ID,
		strings.ToLower(strings.TrimSpace(runtime.ID)), strings.TrimSpace(runtime.Version), strings.TrimSpace(runtime.ContentSet),
		strings.ToLower(strings.TrimSpace(runtime.CoreID)), strings.TrimSpace(runtime.CoreBuildID), strings.ToLower(strings.TrimSpace(runtime.CoreSHA256)),
		normalizeLaunchPlatform(game.Platform), strings.ToLower(strings.TrimSpace(client.Name)),
		strings.ToLower(strings.TrimSpace(client.Platform)), strings.ToLower(strings.TrimSpace(client.Architecture)))
	digest := sha256.Sum256([]byte(key))
	return fmt.Sprintf("auto-%x", digest[:10])
}

func pragmaticProfileRevision(game domain.GameAsset, files []domain.GameFile, client domain.GameLaunchClient) int {
	var key strings.Builder
	fmt.Fprintf(&key, "%d\x00%s\x00%d\x00", game.ID, game.UpdatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), game.Size)
	for _, file := range files {
		fmt.Fprintf(&key, "%d\x00%s\x00%d\x00%s\x00%s\x00", file.Position, file.Name, file.Size,
			strings.ToLower(strings.TrimSpace(file.Role)), strings.ToLower(strings.TrimSpace(file.SHA1)))
	}
	if launchcatalog.RequiresAtomiswaveBIOS(game) && !isGameEMUAndroidClient(client) {
		fmt.Fprintf(&key, "automatic\x00%s\x00%d\x00%s\x00", atomiswaveBIOSLaunchFile.Name,
			atomiswaveBIOSLaunchFile.Size, atomiswaveBIOSLaunchFile.SourceSHA1)
	}
	digest := sha256.Sum256([]byte(key.String()))
	revision := int(uint32(digest[0])<<24|uint32(digest[1])<<16|uint32(digest[2])<<8|uint32(digest[3])) & 0x7fffffff
	if revision == 0 {
		return 1
	}
	return revision
}

func classifyLaunchResolveFailure(game domain.GameAsset, req domain.GameLaunchResolveRequest, profiles []domain.GameLaunchProfile, requested domain.GameRuntimeDescriptor) error {
	details := map[string]any{"gameId": game.ID}
	if !supportedPragmaticClient(req.Client) {
		return launchResolveError("runtime-unsupported", "The client identity or architecture is not supported.", details)
	}
	for _, runtime := range req.Runtimes {
		if !strings.EqualFold(strings.TrimSpace(runtime.ID), "libretro") {
			continue
		}
		core := normalizeLaunchCoreID(runtime.CoreID)
		platform := normalizeLaunchPlatform(game.Platform)
		coreMatchesPlatform := ordinaryLibretroCoreSupportsPlatform(core, platform) ||
			(platform == "psp" && core == "ppsspp") || (platform == "dos" && core == "dosboxpure") || core == "fbneo"
		if core == "fbneo" && coreMatchesPlatform && !runtimeHasStableIdentity(runtime) && isAppleMobileClient(req.Client) {
			details["coreId"] = runtime.CoreID
			return launchResolveError("core-fingerprint-unknown", "The client did not provide a recognized core fingerprint.", details)
		}
		for _, profile := range profiles {
			if persistedProfileClientMatches(profile, req.Client) && strings.EqualFold(profile.Runtime.ID, runtime.ID) && strings.EqualFold(profile.Runtime.CoreID, runtime.CoreID) && !runtimeIdentityMatches(runtime, profile.Runtime) {
				details["coreId"] = runtime.CoreID
				return launchResolveError("core-fingerprint-unknown", "The supplied core fingerprint is not present in the launch policy.", details)
			}
		}
		for _, profile := range auditedGameLaunchProfiles {
			if auditedProfileSourceMatchesGame(profile, game) && auditedProfileClientMatches(profile, req.Client) &&
				strings.EqualFold(profile.Runtime.ID, runtime.ID) && strings.EqualFold(profile.Runtime.CoreID, runtime.CoreID) &&
				!runtimeIdentityMatches(runtime, profile.Runtime) {
				details["coreId"] = runtime.CoreID
				return launchResolveError("core-fingerprint-unknown", "The supplied core fingerprint is not present in the launch policy.", details)
			}
		}
	}
	for _, runtime := range req.Runtimes {
		if !strings.EqualFold(strings.TrimSpace(runtime.ID), "mame") {
			continue
		}
		for _, profile := range profiles {
			if persistedProfileClientMatches(profile, req.Client) && strings.EqualFold(profile.Runtime.ID, runtime.ID) && profile.Runtime.ContentSet != runtime.ContentSet {
				details["contentSet"] = runtime.ContentSet
				return launchResolveError("content-set-mismatch", "The requested content set does not match an installed launch profile.", details)
			}
		}
		for _, profile := range auditedGameLaunchProfiles {
			if auditedProfileSourceMatchesGame(profile, game) && auditedProfileClientMatches(profile, req.Client) &&
				strings.EqualFold(profile.Runtime.ID, runtime.ID) && profile.Runtime.ContentSet != runtime.ContentSet {
				details["contentSet"] = runtime.ContentSet
				return launchResolveError("content-set-mismatch", "The requested content set does not match an installed launch profile.", details)
			}
		}
	}
	if !isLaunchableCatalogGame(game) || isStrictArcadePlatform(game.Platform) || len(profiles) > 0 {
		return launchResolveError("launch-profile-missing", "No compatible audited launch profile is available for this game and client.", details)
	}
	if strings.TrimSpace(requested.ID) != "" {
		details["runtimeId"] = requested.ID
	}
	return launchResolveError("runtime-unsupported", "None of the reported runtimes can launch this platform.", details)
}

func persistedProfileClientMatches(profile domain.GameLaunchProfile, client domain.GameLaunchClient) bool {
	return strings.EqualFold(strings.TrimSpace(client.Name), profile.ClientName) &&
		strings.EqualFold(strings.TrimSpace(client.Platform), profile.ClientPlatform) &&
		strings.EqualFold(strings.TrimSpace(client.Architecture), profile.Architecture) &&
		versionAtLeast(client.Version, profile.MinClientVersion)
}

func auditedProfileClientMatches(profile auditedGameLaunchProfile, client domain.GameLaunchClient) bool {
	return strings.EqualFold(strings.TrimSpace(client.Name), profile.ClientName) &&
		strings.EqualFold(strings.TrimSpace(client.Platform), profile.ClientPlatform) &&
		strings.EqualFold(strings.TrimSpace(client.Architecture), profile.Architecture) &&
		versionAtLeast(client.Version, profile.MinClientVersion)
}

func auditedProfileSourceMatchesGame(profile auditedGameLaunchProfile, game domain.GameAsset) bool {
	return len(profile.Files) > 0 && strings.EqualFold(strings.TrimSpace(game.SHA1), profile.EntrySHA1) &&
		game.Size == profile.Files[0].Size && strings.EqualFold(filepath.Base(game.FilePath), profile.EntrySourceName)
}

func (s *Service) resolveAuditedLaunchSource(file auditedGameLaunchFile) (domain.GameAsset, error) {
	candidates, err := s.store.GamesBySHA1(file.SourceSHA1)
	if err != nil {
		return domain.GameAsset{}, err
	}
	for _, candidate := range candidates {
		if candidate.Size == file.Size && strings.EqualFold(filepath.Base(candidate.FilePath), file.SourceName) {
			return candidate, nil
		}
	}
	return domain.GameAsset{}, fmt.Errorf("%w: %q", errAuditedLaunchSourceUnavailable, file.Name)
}

func matchingRuntime(profile auditedGameLaunchProfile, req domain.GameLaunchResolveRequest) (domain.GameRuntimeDescriptor, bool) {
	if !strings.EqualFold(strings.TrimSpace(req.Client.Name), profile.ClientName) ||
		!strings.EqualFold(strings.TrimSpace(req.Client.Platform), profile.ClientPlatform) ||
		!strings.EqualFold(strings.TrimSpace(req.Client.Architecture), profile.Architecture) ||
		!versionAtLeast(req.Client.Version, profile.MinClientVersion) {
		return domain.GameRuntimeDescriptor{}, false
	}
	for _, runtime := range req.Runtimes {
		if runtimeDescriptorMatches(runtime, profile.Runtime) {
			// Windows 1.302 verifies the response tuple against the selected
			// request tuple field by field, so preserve the request verbatim.
			return runtime, true
		}
	}
	return domain.GameRuntimeDescriptor{}, false
}

func runtimeDescriptorMatches(requested domain.GameRuntimeDescriptor, approved domain.GameRuntimeDescriptor) bool {
	return strings.EqualFold(strings.TrimSpace(requested.ID), strings.TrimSpace(approved.ID)) &&
		strings.TrimSpace(requested.Version) == strings.TrimSpace(approved.Version) &&
		strings.TrimSpace(requested.ContentSet) == strings.TrimSpace(approved.ContentSet) &&
		strings.EqualFold(strings.TrimSpace(requested.CoreID), strings.TrimSpace(approved.CoreID)) &&
		runtimeIdentityMatches(requested, approved)
}

func runtimeIdentityMatches(requested domain.GameRuntimeDescriptor, approved domain.GameRuntimeDescriptor) bool {
	requestedBuildID := strings.TrimSpace(requested.CoreBuildID)
	approvedBuildID := strings.TrimSpace(approved.CoreBuildID)
	if requestedBuildID != "" && approvedBuildID != "" {
		return requestedBuildID == approvedBuildID
	}
	requestedSHA256 := strings.ToLower(strings.TrimSpace(requested.CoreSHA256))
	approvedSHA256 := strings.ToLower(strings.TrimSpace(approved.CoreSHA256))
	if sha256Pattern.MatchString(requestedSHA256) && sha256Pattern.MatchString(approvedSHA256) {
		return requestedSHA256 == approvedSHA256
	}
	if approvedBuildID != "" || approvedSHA256 != "" {
		return false
	}
	return requestedBuildID == "" && requestedSHA256 == ""
}

func runtimeHasStableIdentity(runtime domain.GameRuntimeDescriptor) bool {
	return strings.TrimSpace(runtime.CoreBuildID) != "" ||
		sha256Pattern.MatchString(strings.ToLower(strings.TrimSpace(runtime.CoreSHA256)))
}

func versionAtLeast(actual string, minimum string) bool {
	actualParts, ok := numericVersion(actual)
	if !ok {
		return false
	}
	minimumParts, ok := numericVersion(minimum)
	if !ok {
		return false
	}
	count := len(actualParts)
	if len(minimumParts) > count {
		count = len(minimumParts)
	}
	for index := 0; index < count; index++ {
		var actualPart, minimumPart int
		if index < len(actualParts) {
			actualPart = actualParts[index]
		}
		if index < len(minimumParts) {
			minimumPart = minimumParts[index]
		}
		if actualPart != minimumPart {
			return actualPart > minimumPart
		}
	}
	return true
}

func numericVersion(value string) ([]int, bool) {
	parts := strings.Split(strings.TrimSpace(value), ".")
	if len(parts) == 0 {
		return nil, false
	}
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			return nil, false
		}
		parsed, err := strconv.Atoi(part)
		if err != nil || parsed < 0 {
			return nil, false
		}
		out = append(out, parsed)
	}
	return out, true
}

func validateAuditedGameLaunchProfiles() error {
	profileIDs := map[string]struct{}{}
	for _, profile := range auditedGameLaunchProfiles {
		if strings.TrimSpace(profile.ID) == "" || profile.Revision <= 0 || profile.Priority <= 0 {
			return errors.New("audited launch profile requires an id, positive revision, and positive priority")
		}
		if _, exists := profileIDs[profile.ID]; exists {
			return fmt.Errorf("duplicate audited launch profile id %q", profile.ID)
		}
		profileIDs[profile.ID] = struct{}{}
		if !sha1Pattern.MatchString(profile.EntrySHA1) || len(profile.Files) == 0 {
			return fmt.Errorf("audited launch profile %q has an invalid entry", profile.ID)
		}
		if profile.Files[0].SourceSHA1 != profile.EntrySHA1 || !strings.EqualFold(profile.Files[0].SourceName, profile.EntrySourceName) {
			return fmt.Errorf("audited launch profile %q entry identity does not match its first file", profile.ID)
		}
		if !launchcatalog.IsAuditedEntryIdentity(profile.EntrySourceName, profile.Files[0].Size, profile.EntrySHA1) {
			return fmt.Errorf("audited launch profile %q is missing from the shared launch catalog", profile.ID)
		}
		entryCount := 0
		names := map[string]struct{}{}
		for _, file := range profile.Files {
			if !validLogicalLaunchName(file.Name) || !sha1Pattern.MatchString(file.SourceSHA1) || file.Size <= 0 {
				return fmt.Errorf("audited launch profile %q has an invalid file", profile.ID)
			}
			nameKey := strings.ToLower(file.Name)
			if _, exists := names[nameKey]; exists {
				return fmt.Errorf("audited launch profile %q has a case-insensitive logical name collision", profile.ID)
			}
			names[nameKey] = struct{}{}
			if file.Role == "entry" {
				entryCount++
			} else if file.Role != "dependency" {
				return fmt.Errorf("audited launch profile %q has unsupported role %q", profile.ID, file.Role)
			}
		}
		if entryCount != 1 || profile.Files[0].Role != "entry" {
			return fmt.Errorf("audited launch profile %q must have exactly one leading entry", profile.ID)
		}
	}
	return nil
}

func validLogicalLaunchName(name string) bool {
	trimmed := strings.TrimSpace(name)
	if name != trimmed || name == "" || name == "." || name == ".." || filepath.IsAbs(name) || filepath.VolumeName(name) != "" || strings.ContainsAny(name, `/\\`) {
		return false
	}
	if len(name) >= 2 && ((name[0] >= 'A' && name[0] <= 'Z') || (name[0] >= 'a' && name[0] <= 'z')) && name[1] == ':' {
		return false
	}
	return !strings.ContainsRune(name, '\x00')
}
