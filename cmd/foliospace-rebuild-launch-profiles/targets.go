package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

type launchProfileTarget struct {
	ID               string `json:"id"`
	ClientName       string `json:"clientName"`
	MinClientVersion string `json:"minClientVersion"`
	ClientPlatform   string `json:"clientPlatform"`
	Architecture     string `json:"architecture"`
	CoreBuildID      string `json:"coreBuildId,omitempty"`
	CoreSHA256       string `json:"coreSha256,omitempty"`
}

type launchProfileTargetDocument struct {
	Targets []launchProfileTarget `json:"targets"`
}

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var coreBuildIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,255}$`)

func loadLaunchProfileTargets(path, policy string) ([]launchProfileTarget, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return defaultLaunchProfileTargets(policy), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read launch profile targets: %w", err)
	}
	var document launchProfileTargetDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("parse launch profile targets: %w", err)
	}
	return validateLaunchProfileTargets(document.Targets, policy)
}

func defaultLaunchProfileTargets(policy string) []launchProfileTarget {
	target := launchProfileTarget{
		ID: "windows", ClientName: "SpatialEMU.Windows", MinClientVersion: "1.302",
		ClientPlatform: "windows-x64", Architecture: "x64",
	}
	if strings.EqualFold(strings.TrimSpace(policy), "fbneo") {
		target.CoreSHA256 = fbNeoCoreSHA256
	}
	return []launchProfileTarget{target}
}

func validateLaunchProfileTargets(targets []launchProfileTarget, policy string) ([]launchProfileTarget, error) {
	if len(targets) == 0 {
		return nil, fmt.Errorf("launch profile targets are empty")
	}
	policy = strings.ToLower(strings.TrimSpace(policy))
	seen := make(map[string]bool, len(targets))
	normalized := make([]launchProfileTarget, 0, len(targets))
	for _, target := range targets {
		target.ID = profileVersion(target.ID)
		target.ClientName = strings.TrimSpace(target.ClientName)
		target.MinClientVersion = strings.TrimSpace(target.MinClientVersion)
		target.ClientPlatform = strings.ToLower(strings.TrimSpace(target.ClientPlatform))
		target.Architecture = strings.ToLower(strings.TrimSpace(target.Architecture))
		target.CoreBuildID = strings.TrimSpace(target.CoreBuildID)
		target.CoreSHA256 = strings.ToLower(strings.TrimSpace(target.CoreSHA256))
		if target.ID == "" || target.ID == "dat" || seen[target.ID] {
			return nil, fmt.Errorf("launch profile target id %q is empty, invalid, or duplicated", target.ID)
		}
		if err := validateCanonicalLaunchTarget(target); err != nil {
			return nil, err
		}
		switch policy {
		case "fbneo":
			if target.CoreBuildID == "" && target.CoreSHA256 == "" {
				return nil, fmt.Errorf("launch profile target %q requires an FBNeo coreBuildId or 64-character core SHA-256", target.ID)
			}
			if target.CoreBuildID != "" && !coreBuildIDPattern.MatchString(target.CoreBuildID) {
				return nil, fmt.Errorf("launch profile target %q has an invalid FBNeo coreBuildId", target.ID)
			}
			if target.CoreSHA256 != "" && !sha256Pattern.MatchString(target.CoreSHA256) {
				return nil, fmt.Errorf("launch profile target %q has an invalid FBNeo core SHA-256", target.ID)
			}
		case "mame":
			if target.CoreBuildID != "" || target.CoreSHA256 != "" {
				return nil, fmt.Errorf("launch profile target %q must not set an FBNeo core identity for MAME", target.ID)
			}
		default:
			return nil, fmt.Errorf("unsupported audit policy %q", policy)
		}
		seen[target.ID] = true
		normalized = append(normalized, target)
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i].ID < normalized[j].ID })
	return normalized, nil
}

func validateCanonicalLaunchTarget(target launchProfileTarget) error {
	type identity struct {
		platform     string
		architecture string
	}
	identities := map[string]identity{
		"SpatialEMU.Windows":  {platform: "windows-x64", architecture: "x64"},
		"SpatialEMU.macOS":    {platform: "macos-arm64", architecture: "arm64"},
		"SpatialEMU.iOS":      {platform: "ios-arm64", architecture: "arm64"},
		"SpatialEMU.iPadOS":   {platform: "ipados-arm64", architecture: "arm64"},
		"SpatialEMU.visionOS": {platform: "visionos-arm64", architecture: "arm64"},
		"SpatialEMU.tvOS":     {platform: "tvos-arm64", architecture: "arm64"},
	}
	want, ok := identities[target.ClientName]
	if !ok {
		return fmt.Errorf("launch profile target %q has unsupported client name %q", target.ID, target.ClientName)
	}
	if target.ClientPlatform != want.platform || target.Architecture != want.architecture {
		return fmt.Errorf("launch profile target %q identity mismatch: %s requires %s/%s", target.ID, target.ClientName, want.platform, want.architecture)
	}
	if target.MinClientVersion == "" {
		return fmt.Errorf("launch profile target %q requires minClientVersion", target.ID)
	}
	return nil
}
