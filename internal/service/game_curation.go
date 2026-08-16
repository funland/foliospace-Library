package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/launchcatalog"
)

const defaultMAMEPlatforms = "arcade,mame,model2,cps,cps1,cps2,cps3,neogeo"

func (s *Service) defaultGameCatalogSettings() domain.GameCatalogSettings {
	policyDir := filepath.Join(s.configDir, "policies")
	return domain.GameCatalogSettings{
		AutoAnalyzeAfterScan: true,
		EnableLibretroCovers: true,
		FBNeoDATPath:         filepath.Join(policyDir, "fbneo-arcade.dat"),
		MAMEListXMLPath:      filepath.Join(policyDir, "mame0288lx.zip"),
		FBNeoTargetsPath:     filepath.Join(policyDir, "fbneo-mobile-targets.json"),
		MAMETargetsPath:      filepath.Join(policyDir, "mame-mobile-targets.json"),
		LaunchTargetsPath:    filepath.Join(policyDir, "targets.json"),
		MAMEPlatforms:        defaultMAMEPlatforms,
		MetadataProvider:     "local",
	}
}

func (s *Service) GameCatalogSettings() domain.GameCatalogSettings {
	settings := s.defaultGameCatalogSettings()
	raw, err := s.store.Setting(gameCatalogSettingsSetting)
	if err != nil {
		return settings
	}
	var saved domain.GameCatalogSettings
	if json.Unmarshal([]byte(raw), &saved) != nil {
		return settings
	}
	return normalizeGameCatalogSettings(saved, settings)
}

func normalizeGameCatalogSettings(settings, defaults domain.GameCatalogSettings) domain.GameCatalogSettings {
	if strings.TrimSpace(settings.FBNeoDATPath) == "" {
		settings.FBNeoDATPath = defaults.FBNeoDATPath
	}
	if strings.TrimSpace(settings.MAMEListXMLPath) == "" {
		settings.MAMEListXMLPath = defaults.MAMEListXMLPath
	}
	if strings.TrimSpace(settings.FBNeoTargetsPath) == "" {
		settings.FBNeoTargetsPath = defaults.FBNeoTargetsPath
	}
	if strings.TrimSpace(settings.MAMETargetsPath) == "" {
		settings.MAMETargetsPath = defaults.MAMETargetsPath
	}
	if strings.TrimSpace(settings.LaunchTargetsPath) == "" {
		settings.LaunchTargetsPath = defaults.LaunchTargetsPath
	}
	if strings.TrimSpace(settings.MAMEPlatforms) == "" {
		settings.MAMEPlatforms = defaults.MAMEPlatforms
	}
	switch strings.ToLower(strings.TrimSpace(settings.MetadataProvider)) {
	case "local", "hasheous", "disabled":
		settings.MetadataProvider = strings.ToLower(strings.TrimSpace(settings.MetadataProvider))
	default:
		settings.MetadataProvider = defaults.MetadataProvider
	}
	return settings
}

func (s *Service) SaveGameCatalogSettings(settings domain.GameCatalogSettings) error {
	settings = normalizeGameCatalogSettings(settings, s.defaultGameCatalogSettings())
	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	return s.store.UpsertSetting(gameCatalogSettingsSetting, string(data))
}

func (s *Service) GameCurationSummary() (domain.GameCurationSummary, error) {
	var summary domain.GameCurationSummary
	var err error
	summary.Total, summary.Ready, summary.NeedsCuration, summary.Dependencies, err = s.store.GameCatalogRoleCounts()
	if err != nil {
		return summary, err
	}
	summary.MetadataReady, summary.ArtworkReady, err = s.store.GameCatalogEnrichmentCounts()
	if err != nil {
		return summary, err
	}
	summary.FileCount, summary.Checksummed, err = s.store.GameFileChecksumCounts()
	if err != nil {
		return summary, err
	}
	summary.ChecksumPending = summary.FileCount - summary.Checksummed
	summary.Policies = s.gameCatalogPolicyStatuses()
	summary.LastTask = s.GameCatalogTaskStatus()
	return summary, nil
}

func (s *Service) gameCatalogPolicyStatuses() []domain.GameCatalogPolicyStatus {
	settings := s.GameCatalogSettings()
	fbneoTargets := effectiveTargetsPath(settings.FBNeoTargetsPath, settings.LaunchTargetsPath)
	mameTargets := effectiveTargetsPath(settings.MAMETargetsPath, settings.LaunchTargetsPath)
	return []domain.GameCatalogPolicyStatus{
		gameCatalogPolicyStatus("fbneo", settings.FBNeoDATPath),
		gameCatalogPolicyStatus("mame", settings.MAMEListXMLPath),
		gameCatalogTargetsPolicyStatus(fbneoTargets, mameTargets),
	}
}

func effectiveTargetsPath(preferred, legacy string) string {
	preferred = strings.TrimSpace(preferred)
	if info, err := os.Stat(preferred); err == nil && !info.IsDir() {
		return preferred
	}
	legacy = strings.TrimSpace(legacy)
	if info, err := os.Stat(legacy); err == nil && !info.IsDir() {
		return legacy
	}
	return preferred
}

func gameCatalogPolicyStatus(id, path string) domain.GameCatalogPolicyStatus {
	status := domain.GameCatalogPolicyStatus{ID: id, Path: path}
	info, err := os.Stat(path)
	status.Available = err == nil && !info.IsDir()
	if !status.Available {
		status.Message = "File is not installed. Copy a compatible policy file into /config/policies or choose another path."
	}
	return status
}

func gameCatalogTargetsPolicyStatus(fbneoPath, mamePath string) domain.GameCatalogPolicyStatus {
	status := domain.GameCatalogPolicyStatus{ID: "targets", Path: fbneoPath + " | " + mamePath, Available: true}
	if err := validateGameCatalogTargetsFile(fbneoPath, true); err != nil {
		status.Available = false
		status.Message = "FBNeo targets: " + err.Error()
		return status
	}
	if err := validateGameCatalogTargetsFile(mamePath, false); err != nil {
		status.Available = false
		status.Message = "MAME targets: " + err.Error()
	}
	return status
}

func validateGameCatalogTargetsFile(path string, requireCore bool) error {
	data, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return err
	}
	var document struct {
		Targets []struct {
			ID          string `json:"id"`
			CoreBuildID string `json:"coreBuildId"`
			CoreSHA256  string `json:"coreSha256"`
		} `json:"targets"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return err
	}
	if len(document.Targets) == 0 {
		return errors.New("target list is empty")
	}
	for _, target := range document.Targets {
		if strings.TrimSpace(target.ID) == "" {
			return errors.New("target ID is empty")
		}
		buildID := strings.TrimSpace(target.CoreBuildID)
		core := strings.ToLower(strings.TrimSpace(target.CoreSHA256))
		if requireCore && buildID == "" && core == "" {
			return fmt.Errorf("target %q has no FBNeo coreBuildId or complete core SHA-256", target.ID)
		}
		if requireCore && buildID != "" && !coreBuildIDPattern.MatchString(buildID) {
			return fmt.Errorf("target %q has an invalid FBNeo coreBuildId", target.ID)
		}
		if requireCore && core != "" && !sha256Pattern.MatchString(core) {
			return fmt.Errorf("target %q has an invalid core SHA-256", target.ID)
		}
		if !requireCore && (buildID != "" || core != "") {
			return fmt.Errorf("target %q must not set an FBNeo core identity", target.ID)
		}
	}
	return nil
}

func (s *Service) ListGameCurationPage(options domain.GameListOptions) (domain.GameCurationPage, error) {
	options.IncludeDependencies = true
	page, err := s.store.ListGamesPage(options)
	if err != nil {
		return domain.GameCurationPage{}, err
	}
	gameIDs := make([]int64, 0, len(page.Items))
	for _, game := range page.Items {
		gameIDs = append(gameIDs, game.ID)
	}
	stats, err := s.store.GameCurationStats(gameIDs)
	if err != nil {
		return domain.GameCurationPage{}, err
	}
	items := make([]domain.GameCurationItem, 0, len(page.Items))
	policies := s.gameCatalogPolicyStatuses()
	for _, game := range page.Items {
		gameStats := stats[game.ID]
		item := domain.GameCurationItem{
			Game: game, MetadataStatus: gameStats.MetadataStatus, ArtworkStatus: gameStats.ArtworkStatus,
			ReadyProfiles: gameStats.ReadyProfiles, FileCount: gameStats.FileCount, Checksummed: gameStats.Checksummed,
		}
		item.MobileReady = item.FileCount > 0 && item.Checksummed == item.FileCount &&
			(!launchcatalog.IsStrictArcadePlatform(game.Platform) || item.ReadyProfiles > 0) &&
			isLaunchableCatalogGame(game)
		item.IssueCode, item.IssueMessage = gameCurationIssue(game, item.ReadyProfiles, policies, item.FileCount, item.Checksummed)
		items = append(items, item)
	}
	return domain.GameCurationPage{Items: items, Total: page.Total, Limit: page.Limit, Offset: page.Offset, HasMore: page.HasMore}, nil
}

func gameCurationIssue(game domain.GameAsset, readyProfiles int, policies []domain.GameCatalogPolicyStatus, fileCount, checksummed int) (string, string) {
	switch strings.ToLower(strings.TrimSpace(game.CatalogRole)) {
	case "dependency":
		return "dependency", "This file is a BIOS, device, parent, or track dependency and is intentionally hidden from clients."
	case "needs-curation":
		if strings.TrimSpace(game.SHA1) == "" && strings.EqualFold(game.Format, "zip") {
			return "identity-missing", "The archive has no stable SHA-1 identity and cannot be audited. Rescan the file."
		}
		available := false
		for _, policy := range policies {
			if (policy.ID == "fbneo" || policy.ID == "mame") && policy.Available {
				available = true
			}
		}
		if !available {
			return "policy-pack-missing", "No FBNeo or MAME compatibility policy is installed."
		}
		return "launch-profile-missing", "The ROM did not pass an installed compatibility policy, or a required parent/BIOS archive is missing."
	default:
		if readyProfiles == 0 && launchcatalog.IsStrictArcadePlatform(game.Platform) {
			return "launch-profile-missing", "The catalog entry is visible but has no audited runtime profile. Re-run compatibility analysis."
		}
	}
	if fileCount == 0 {
		return "dependency-missing", "The canonical launch manifest has no indexed files. Rescan this game."
	}
	if checksummed < fileCount {
		return "manifest-checksum-unavailable", "One or more launch files need a checksum before mobile clients can use the resolver."
	}
	return "", ""
}

func (s *Service) handleCompletedScan(library domain.Library, job domain.ScanJob) {
	// A completed scan is also a safe retry point for files that failed hashing
	// earlier because a mount or permission was temporarily unavailable.
	_ = s.store.RetryFailedContentHashes()
	if library.AssetType != "game" && library.AssetType != "mixed" {
		return
	}
	if !scanShouldTriggerCatalogAnalysis(library, job) {
		return
	}
	if _, err := s.store.Setting(gameCatalogSettingsSetting); err != nil {
		return
	}
	if s.GameCatalogSettings().AutoAnalyzeAfterScan {
		_, _ = s.StartGameCatalogAnalysis()
	}
}

func scanShouldTriggerCatalogAnalysis(library domain.Library, job domain.ScanJob) bool {
	targetPath := filepath.Clean(strings.TrimSpace(job.TargetPath))
	if targetPath == "." || targetPath == "" {
		return false
	}
	if targetPath == filepath.Clean(library.RootPath) {
		return true
	}
	info, err := os.Stat(targetPath)
	return err == nil && info.IsDir()
}

func (s *Service) GameCatalogTaskStatus() domain.GameCatalogTaskStatus {
	s.gameCatalogMu.Lock()
	defer s.gameCatalogMu.Unlock()
	if s.gameCatalogTask.Status != "" {
		return s.gameCatalogTask
	}
	raw, err := s.store.Setting(gameCatalogTaskSetting)
	if err == nil {
		_ = json.Unmarshal([]byte(raw), &s.gameCatalogTask)
		if s.gameCatalogTask.Status == "running" {
			now := time.Now()
			s.gameCatalogTask.Status = "interrupted"
			s.gameCatalogTask.EndedAt = &now
			s.gameCatalogTask.Message = "The previous background task was interrupted by a service restart."
		}
	}
	return s.gameCatalogTask
}

func (s *Service) beginGameCatalogTask(action string) (domain.GameCatalogTaskStatus, error) {
	s.gameCatalogMu.Lock()
	defer s.gameCatalogMu.Unlock()
	if s.gameCatalogTask.Status == "running" {
		return s.gameCatalogTask, fmt.Errorf("game catalog task %s is already running", s.gameCatalogTask.ID)
	}
	now := time.Now()
	s.gameCatalogTask = domain.GameCatalogTaskStatus{
		ID: fmt.Sprintf("%s-%d", action, now.Unix()), Action: action, Status: "running",
		Message: "Background task started.", StartedAt: &now, Details: map[string]any{},
	}
	s.persistGameCatalogTaskLocked()
	return s.gameCatalogTask, nil
}

func (s *Service) updateGameCatalogTask(update func(*domain.GameCatalogTaskStatus)) {
	s.gameCatalogMu.Lock()
	defer s.gameCatalogMu.Unlock()
	update(&s.gameCatalogTask)
	s.persistGameCatalogTaskLocked()
}

func (s *Service) persistGameCatalogTaskLocked() {
	data, err := json.Marshal(s.gameCatalogTask)
	if err == nil {
		_ = s.store.UpsertSetting(gameCatalogTaskSetting, string(data))
	}
}

func (s *Service) finishGameCatalogTask(err error, message string) {
	s.updateGameCatalogTask(func(task *domain.GameCatalogTaskStatus) {
		now := time.Now()
		task.EndedAt = &now
		task.Message = message
		if task.StartedAt != nil {
			if task.Details == nil {
				task.Details = map[string]any{}
			}
			task.Details["durationSeconds"] = now.Sub(*task.StartedAt).Seconds()
		}
		if err != nil {
			task.Status = "failed"
			task.Failed++
		} else {
			task.Status = "completed"
		}
	})
}

func (s *Service) StartGameCatalogAnalysis() (domain.GameCatalogTaskStatus, error) {
	task, err := s.beginGameCatalogTask("analyze")
	if err != nil {
		return task, err
	}
	go s.runGameCatalogAnalysis()
	return task, nil
}

func (s *Service) StartGameChecksumBackfill(limit int, gameIDs ...int64) (domain.GameCatalogTaskStatus, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	var gameID int64
	if len(gameIDs) > 0 {
		gameID = gameIDs[0]
		if gameID < 0 {
			return domain.GameCatalogTaskStatus{}, errors.New("game ID cannot be negative")
		}
	}
	task, err := s.beginGameCatalogTask("checksums")
	if err != nil {
		return task, err
	}
	go s.runGameChecksumBackfill(limit, gameID)
	return task, nil
}

func (s *Service) StartGameCompatibilityRebuild(req domain.GameCompatibilityRebuildRequest) (domain.GameCatalogTaskStatus, error) {
	req.Scope = strings.ToLower(strings.TrimSpace(req.Scope))
	if req.Scope == "" {
		req.Scope = "all"
	}
	req.Platform = strings.ToLower(strings.TrimSpace(req.Platform))
	switch req.Scope {
	case "game":
		if req.GameID <= 0 {
			return domain.GameCatalogTaskStatus{}, errors.New("a positive gameId is required for game scope")
		}
		if _, err := s.store.GameByID(req.GameID); err != nil {
			return domain.GameCatalogTaskStatus{}, fmt.Errorf("game %d is unavailable: %w", req.GameID, err)
		}
	case "platform":
		if req.Platform == "" {
			return domain.GameCatalogTaskStatus{}, errors.New("platform is required for platform scope")
		}
	case "all":
		req.GameID = 0
		req.Platform = ""
	default:
		return domain.GameCatalogTaskStatus{}, fmt.Errorf("unsupported compatibility rebuild scope %q", req.Scope)
	}
	task, err := s.beginGameCatalogTask("compatibility-rebuild")
	if err != nil {
		return task, err
	}
	s.updateGameCatalogTask(func(status *domain.GameCatalogTaskStatus) {
		status.Scope = req.Scope
		status.GameID = req.GameID
		status.Platform = req.Platform
		status.Force = req.Force
		status.Message = "Preparing compatibility data rebuild."
		status.Details["checksumWorkers"] = 1
		status.Details["checksumBatchSize"] = 32
		status.Details["profileGOMAXPROCS"] = 1
		status.Details["profileGOMEMLIMIT"] = profileRebuildMemoryLimit()
	})
	go s.runGameCompatibilityRebuild(req)
	return s.GameCatalogTaskStatus(), nil
}

func (s *Service) runGameCompatibilityRebuild(req domain.GameCompatibilityRebuildRequest) {
	total, checksummed, err := s.gameChecksumCountsForRebuild(req)
	if err != nil {
		s.finishGameCatalogTask(err, err.Error())
		return
	}
	pending := total - checksummed
	s.updateGameCatalogTask(func(task *domain.GameCatalogTaskStatus) {
		task.Total = pending
		task.Message = fmt.Sprintf("Checksumming %d pending canonical launch file(s) with one worker.", pending)
	})

	var afterID int64
	for {
		files, listErr := s.store.GameFilesMissingSHA1ForScope(req.GameID, req.Platform, afterID, 32)
		if listErr != nil {
			s.finishGameCatalogTask(listErr, listErr.Error())
			return
		}
		if len(files) == 0 {
			break
		}
		for _, file := range files {
			afterID = file.ID
			updated, checksumErr := s.checksumGameFile(file)
			s.updateGameCatalogTask(func(task *domain.GameCatalogTaskStatus) {
				task.Processed++
				if checksumErr != nil {
					task.Failed++
					if len(task.Errors) < 100 {
						task.Errors = append(task.Errors, fmt.Sprintf("game=%d file=%s: %v", file.GameID, file.Name, checksumErr))
					}
				} else if updated {
					task.Matched++
				} else {
					task.Skipped++
				}
				task.Message = fmt.Sprintf("Checksummed %d of %d pending launch file(s).", task.Processed, task.Total)
			})
		}
	}

	remainingTotal, remainingChecksummed, countErr := s.gameChecksumCountsForRebuild(req)
	if countErr != nil {
		s.finishGameCatalogTask(countErr, countErr.Error())
		return
	}
	remaining := remainingTotal - remainingChecksummed
	s.updateGameCatalogTask(func(task *domain.GameCatalogTaskStatus) {
		task.Details["checksumRemaining"] = remaining
		task.Message = "Checksums completed; rebuilding compatibility profiles."
	})

	processed, changed, normalizeErr := s.normalizeGameCatalogRoles()
	if normalizeErr != nil {
		s.finishGameCatalogTask(normalizeErr, normalizeErr.Error())
		return
	}
	s.updateGameCatalogTask(func(task *domain.GameCatalogTaskStatus) {
		task.Details["catalogEntriesReviewed"] = processed
		task.Details["catalogRolesChanged"] = changed
	})

	policyNeeded, policyCheckErr := s.compatibilityPolicyRebuildNeeded(req)
	if policyCheckErr != nil {
		s.finishGameCatalogTask(policyCheckErr, policyCheckErr.Error())
		return
	}
	if policyNeeded {
		policyResults, policyErr := s.runConfiguredGameCompatibilityPolicies(req)
		if policyErr != nil {
			s.finishGameCatalogTask(policyErr, policyErr.Error())
			return
		}
		s.updateGameCatalogTask(func(task *domain.GameCatalogTaskStatus) {
			for id, result := range policyResults {
				task.Details[id] = result
			}
		})
	} else {
		s.updateGameCatalogTask(func(task *domain.GameCatalogTaskStatus) {
			task.Details["profilesSkipped"] = "No missing profiles were found in the selected scope."
		})
	}
	message := fmt.Sprintf("Compatibility data completed: %d succeeded, %d skipped, %d failed; %d checksum(s) remain.",
		s.GameCatalogTaskStatus().Matched, s.GameCatalogTaskStatus().Skipped, s.GameCatalogTaskStatus().Failed, remaining)
	s.finishGameCatalogTask(nil, message)
}

func (s *Service) compatibilityPolicyRebuildNeeded(req domain.GameCompatibilityRebuildRequest) (bool, error) {
	if req.Force {
		return true, nil
	}
	switch req.Scope {
	case "game":
		game, err := s.store.GameByID(req.GameID)
		if err != nil {
			return false, err
		}
		if !launchcatalog.IsStrictArcadePlatform(game.Platform) {
			return false, nil
		}
		profiles, err := s.store.GameLaunchProfiles(req.GameID)
		return len(profiles) == 0, err
	case "platform":
		if !launchcatalog.IsStrictArcadePlatform(req.Platform) {
			return false, nil
		}
		page, err := s.store.ListGamesPage(domain.GameListOptions{Limit: 1, Platform: req.Platform, CatalogRole: launchcatalog.RoleNeedsCuration})
		return page.Total > 0, err
	default:
		_, _, needsCuration, _, err := s.store.GameCatalogRoleCounts()
		return needsCuration > 0, err
	}
}

func (s *Service) gameChecksumCountsForRebuild(req domain.GameCompatibilityRebuildRequest) (int64, int64, error) {
	switch req.Scope {
	case "game":
		total, checksummed, err := s.store.GameFileChecksumCountsForGame(req.GameID)
		return int64(total), int64(checksummed), err
	case "platform":
		return s.store.GameFileChecksumCountsForPlatform(req.Platform)
	default:
		return s.store.GameFileChecksumCounts()
	}
}

func (s *Service) checksumGameFile(file domain.GameFile) (bool, error) {
	game, err := s.store.GameByID(file.GameID)
	if err != nil {
		return false, err
	}
	before, err := os.Stat(file.FilePath)
	if err != nil {
		return false, err
	}
	if !before.Mode().IsRegular() || gameFileSourceChanged(game, file, before) {
		return false, errors.New("source is not a stable regular file")
	}
	stream, _, err := s.OpenGameFilePart(file.GameID, file.Position)
	if err != nil {
		return false, err
	}
	hash := sha1.New()
	_, copyErr := io.Copy(hash, stream.Body)
	closeErr := stream.Body.Close()
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return false, copyErr
	}
	after, err := os.Stat(file.FilePath)
	if err != nil {
		return false, err
	}
	if !after.Mode().IsRegular() || gameFileSourceChanged(game, file, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return false, errors.New("source changed while checksum was being computed")
	}
	return s.store.UpdateGameFileSHA1(file, hex.EncodeToString(hash.Sum(nil)))
}

func (s *Service) runConfiguredGameCompatibilityPolicies(req domain.GameCompatibilityRebuildRequest) (map[string]map[string]any, error) {
	settings := s.GameCatalogSettings()
	platform := req.Platform
	if req.Scope == "game" {
		game, err := s.store.GameByID(req.GameID)
		if err != nil {
			return nil, err
		}
		platform = strings.ToLower(strings.TrimSpace(game.Platform))
	}
	if req.Scope != "all" && !launchcatalog.IsStrictArcadePlatform(platform) {
		return map[string]map[string]any{}, nil
	}
	results := map[string]map[string]any{}
	runs := []struct {
		id          string
		path        string
		targetsPath string
		args        []string
	}{
		{id: "fbneo", path: settings.FBNeoDATPath, targetsPath: effectiveTargetsPath(settings.FBNeoTargetsPath, settings.LaunchTargetsPath), args: []string{"-policy", "fbneo", "-dat", settings.FBNeoDATPath}},
		{id: "mame", path: settings.MAMEListXMLPath, targetsPath: effectiveTargetsPath(settings.MAMETargetsPath, settings.LaunchTargetsPath), args: []string{"-policy", "mame", "-mame-listxml", settings.MAMEListXMLPath, "-platforms", settings.MAMEPlatforms}},
	}
	for _, run := range runs {
		if info, statErr := os.Stat(run.path); statErr != nil || info.IsDir() {
			continue
		}
		args := append([]string{}, run.args...)
		if req.Scope == "game" {
			args = append(args, "-game-id", fmt.Sprintf("%d", req.GameID))
		}
		if targetInfo, targetErr := os.Stat(run.targetsPath); targetErr == nil && !targetInfo.IsDir() {
			args = append(args, "-targets", run.targetsPath)
		}
		output, err := s.runLaunchProfileRebuild(args)
		if err != nil {
			return nil, fmt.Errorf("%s compatibility analysis failed: %w", run.id, err)
		}
		results[run.id] = output
	}
	return results, nil
}

func (s *Service) runGameChecksumBackfill(limit int, gameID int64) {
	total, checksummed, err := s.store.GameFileChecksumCounts()
	if err != nil {
		s.finishGameCatalogTask(err, err.Error())
		return
	}
	pending := total - checksummed
	var files []domain.GameFile
	if gameID > 0 {
		gameTotal, gameChecksummed, countErr := s.store.GameFileChecksumCountsForGame(gameID)
		if countErr != nil {
			s.finishGameCatalogTask(countErr, countErr.Error())
			return
		}
		pending = int64(gameTotal - gameChecksummed)
		files, err = s.store.GameFilesMissingSHA1ForGame(gameID, limit)
	} else {
		files, err = s.store.GameFilesMissingSHA1(limit)
	}
	if err != nil {
		s.finishGameCatalogTask(err, err.Error())
		return
	}
	if len(files) == 0 {
		s.finishGameCatalogTask(nil, "All canonical launch files already have checksums.")
		return
	}
	s.updateGameCatalogTask(func(task *domain.GameCatalogTaskStatus) {
		task.Total = int64(len(files))
		if gameID > 0 {
			task.Message = fmt.Sprintf("Checksumming %d pending launch file(s) for game %d.", len(files), gameID)
		} else {
			task.Message = fmt.Sprintf("Checksumming %d of %d pending canonical launch files.", len(files), pending)
		}
	})

	for _, file := range files {
		game, gameErr := s.store.GameByID(file.GameID)
		before, statErr := os.Stat(file.FilePath)
		if gameErr != nil || statErr != nil || !before.Mode().IsRegular() || gameFileSourceChanged(game, file, before) {
			s.updateGameCatalogTask(func(task *domain.GameCatalogTaskStatus) {
				task.Processed++
				task.Failed++
				task.Message = fmt.Sprintf("Skipped changed or missing source for game %d file %q.", file.GameID, file.Name)
			})
			continue
		}
		stream, _, openErr := s.OpenGameFilePart(file.GameID, file.Position)
		if openErr != nil {
			s.updateGameCatalogTask(func(task *domain.GameCatalogTaskStatus) {
				task.Processed++
				task.Failed++
				task.Message = fmt.Sprintf("Could not checksum game %d file %q.", file.GameID, file.Name)
			})
			continue
		}
		hash := sha1.New()
		_, copyErr := io.Copy(hash, stream.Body)
		closeErr := stream.Body.Close()
		if copyErr == nil {
			copyErr = closeErr
		}
		updated := false
		if copyErr == nil {
			after, afterErr := os.Stat(file.FilePath)
			if afterErr != nil || !after.Mode().IsRegular() || gameFileSourceChanged(game, file, after) ||
				before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
				copyErr = errors.New("source changed while checksum was being computed")
			} else {
				checksum := hex.EncodeToString(hash.Sum(nil))
				updated, copyErr = s.store.UpdateGameFileSHA1(file, checksum)
			}
		}
		s.updateGameCatalogTask(func(task *domain.GameCatalogTaskStatus) {
			task.Processed++
			if copyErr != nil || !updated {
				task.Failed++
			} else {
				task.Matched++
			}
			task.Message = fmt.Sprintf("Checksummed %d of %d pending files in this batch.", task.Processed, len(files))
		})
	}

	remainingTotal, remainingChecksummed, countErr := s.store.GameFileChecksumCounts()
	if gameID > 0 {
		gameTotal, gameChecksummed, gameCountErr := s.store.GameFileChecksumCountsForGame(gameID)
		remainingTotal = int64(gameTotal)
		remainingChecksummed = int64(gameChecksummed)
		countErr = gameCountErr
	}
	if countErr != nil {
		s.finishGameCatalogTask(countErr, countErr.Error())
		return
	}
	remaining := remainingTotal - remainingChecksummed
	message := fmt.Sprintf("Checksum batch completed; %d file(s) remain. Run the task again to continue.", remaining)
	if gameID > 0 {
		message = fmt.Sprintf("Checksum repair completed for game %d; %d file(s) remain.", gameID, remaining)
	}
	if gameID > 0 && remaining == 0 {
		message = fmt.Sprintf("Checksum repair completed for game %d.", gameID)
	} else if remaining == 0 {
		message = "Checksum backfill completed for all canonical launch files."
	}
	s.finishGameCatalogTask(nil, message)
}

func (s *Service) runGameCatalogAnalysis() {
	processed, changed, err := s.normalizeGameCatalogRoles()
	if err != nil {
		s.finishGameCatalogTask(err, err.Error())
		return
	}
	s.updateGameCatalogTask(func(task *domain.GameCatalogTaskStatus) {
		task.Processed = processed
		task.Matched = changed
		task.Message = "Base catalog classification completed; compatibility policies are being evaluated."
	})

	settings := s.GameCatalogSettings()
	policyRuns := 0
	for _, run := range []struct {
		id          string
		path        string
		targetsPath string
		args        []string
	}{
		{id: "fbneo", path: settings.FBNeoDATPath, targetsPath: effectiveTargetsPath(settings.FBNeoTargetsPath, settings.LaunchTargetsPath), args: []string{"-policy", "fbneo", "-dat", settings.FBNeoDATPath}},
		{id: "mame", path: settings.MAMEListXMLPath, targetsPath: effectiveTargetsPath(settings.MAMETargetsPath, settings.LaunchTargetsPath), args: []string{"-policy", "mame", "-mame-listxml", settings.MAMEListXMLPath, "-platforms", settings.MAMEPlatforms}},
	} {
		if info, statErr := os.Stat(run.path); statErr != nil || info.IsDir() {
			continue
		}
		args := append([]string{}, run.args...)
		if targetInfo, targetErr := os.Stat(run.targetsPath); targetErr == nil && !targetInfo.IsDir() {
			args = append(args, "-targets", run.targetsPath)
		}
		output, runErr := s.runLaunchProfileRebuild(args)
		if runErr != nil {
			s.finishGameCatalogTask(runErr, fmt.Sprintf("%s compatibility analysis failed: %v", run.id, runErr))
			return
		}
		policyRuns++
		s.updateGameCatalogTask(func(task *domain.GameCatalogTaskStatus) {
			task.Details[run.id] = output
		})
	}
	message := fmt.Sprintf("Catalog analysis completed; %d compatibility policy run(s) applied.", policyRuns)
	if policyRuns == 0 {
		message = "Base classification completed. Install an FBNeo or MAME policy file to publish audited arcade ROMs."
	}
	s.finishGameCatalogTask(nil, message)
}

func (s *Service) normalizeGameCatalogRoles() (processed, changed int64, err error) {
	for offset := 0; ; offset += 200 {
		page, pageErr := s.store.ListGamesPage(domain.GameListOptions{Limit: 200, Offset: offset, Sort: "title"})
		if pageErr != nil {
			return processed, changed, pageErr
		}
		for _, game := range page.Items {
			processed++
			var dosLaunch *domain.DOSLaunch
			if strings.EqualFold(game.Platform, "dos") {
				if launch, launchErr := s.store.DOSLaunch(game.ID); launchErr == nil {
					dosLaunch = &launch
				}
			}
			expected := launchcatalog.CatalogRole(game, dosLaunch)
			if launchcatalog.IsStrictArcadePlatform(game.Platform) {
				if profiles, profileErr := s.store.GameLaunchProfiles(game.ID); profileErr == nil && len(profiles) > 0 {
					expected = launchcatalog.RoleGame
				}
			}
			if expected != strings.ToLower(strings.TrimSpace(game.CatalogRole)) {
				if updateErr := s.store.UpdateGameCatalogRole(game.ID, expected); updateErr != nil {
					return processed, changed, updateErr
				}
				changed++
			}
		}
		if !page.HasMore {
			break
		}
	}
	return processed, changed, nil
}

func (s *Service) runLaunchProfileRebuild(args []string) (map[string]any, error) {
	binary := strings.TrimSpace(os.Getenv("FOLIOSPACE_PROFILE_REBUILD_BIN"))
	if binary == "" {
		binary = "/app/foliospace-rebuild-launch-profiles"
	}
	if _, err := os.Stat(binary); err != nil {
		if path, lookupErr := exec.LookPath("foliospace-rebuild-launch-profiles"); lookupErr == nil {
			binary = path
		} else {
			return nil, fmt.Errorf("launch profile rebuild tool is not installed")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Hour)
	defer cancel()
	command := exec.CommandContext(ctx, binary, args...)
	command.Env = append(os.Environ(),
		"FOLIOSPACE_CONFIG_DIR="+s.configDir,
		"GOMAXPROCS=1",
		"GOMEMLIMIT="+profileRebuildMemoryLimit(),
	)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("invalid rebuild output: %w", err)
	}
	return result, nil
}

func profileRebuildMemoryLimit() string {
	if value := strings.TrimSpace(os.Getenv("FOLIOSPACE_PROFILE_GOMEMLIMIT")); value != "" {
		return value
	}
	return "768MiB"
}

func (s *Service) StartGameCoverMatch(includeNetwork bool) (domain.GameCatalogTaskStatus, error) {
	if includeNetwork && !s.GameCatalogSettings().EnableLibretroCovers {
		return domain.GameCatalogTaskStatus{}, fmt.Errorf("Libretro cover matching is disabled in game catalog settings")
	}
	action := "covers-local"
	if includeNetwork {
		action = "covers-libretro"
	}
	task, err := s.beginGameCatalogTask(action)
	if err != nil {
		return task, err
	}
	go s.runGameCoverMatch(includeNetwork)
	return task, nil
}

func (s *Service) runGameCoverMatch(includeNetwork bool) {
	var processed, matched, failed int64
	for offset := 0; ; offset += 200 {
		page, err := s.store.ListGamesPage(domain.GameListOptions{Limit: 200, Offset: offset})
		if err != nil {
			s.finishGameCatalogTask(err, err.Error())
			return
		}
		for _, game := range page.Items {
			processed++
			if stream, ok := s.openSelectedGameCover(game.ID); ok {
				_ = stream.Body.Close()
				continue
			}
			localPath := ""
			for _, candidate := range localGameCoverCandidates(game.FilePath) {
				if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
					localPath = candidate
					break
				}
			}
			if localPath != "" {
				_, err = s.store.UpsertGameArtwork(domain.GameArtwork{GameID: game.ID, Source: "local", Kind: "cover", CachePath: localPath, Selected: true, Confidence: 1})
				if err == nil {
					matched++
				} else {
					failed++
				}
			} else if includeNetwork {
				stream, coverErr := s.OpenGameCover(game.ID)
				if coverErr == nil {
					_ = stream.Body.Close()
					matched++
				} else {
					failed++
				}
			}
			if processed%10 == 0 {
				s.updateGameCatalogTask(func(task *domain.GameCatalogTaskStatus) {
					task.Processed, task.Matched, task.Failed = processed, matched, failed
					task.Message = fmt.Sprintf("Checked %d games; matched %d covers.", processed, matched)
				})
			}
		}
		if !page.HasMore {
			break
		}
	}
	s.updateGameCatalogTask(func(task *domain.GameCatalogTaskStatus) {
		task.Processed, task.Matched, task.Failed = processed, matched, failed
	})
	s.finishGameCatalogTask(nil, fmt.Sprintf("Cover matching completed: %d matched, %d unavailable.", matched, failed))
}

func (s *Service) UpdateGameMetadata(id int64, metadata domain.GameMetadata) (domain.GameDetails, error) {
	if _, err := s.store.GameByID(id); err != nil {
		return domain.GameDetails{}, err
	}
	metadata.GameID = id
	if metadata.Rating < 0 || metadata.Rating > 5 {
		return domain.GameDetails{}, fmt.Errorf("rating must be between 0 and 5")
	}
	if err := s.store.UpsertGameMetadata(metadata); err != nil {
		return domain.GameDetails{}, err
	}
	_, err := s.store.UpsertGameMetadataSource(domain.GameMetadataSource{
		GameID: id, Source: "manual", SourceID: fmt.Sprintf("game:%d", id), MatchedBy: "manual", Confidence: 1,
	})
	if err != nil {
		return domain.GameDetails{}, err
	}
	return s.store.GameDetails(id)
}
