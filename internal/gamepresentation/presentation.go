// Package gamepresentation derives display hints without changing indexed
// assets, source manifests, launch decisions, or per-game user state.
package gamepresentation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path"
	"regexp"
	"strconv"
	"strings"

	"foliospace-reader/internal/domain"
)

type Info struct {
	GroupKey   string `json:"groupKey"`
	Title      string `json:"title"`
	DiscNumber int    `json:"discNumber"`
}

var (
	bracketedDisc = regexp.MustCompile(`(?i)(?:\(\s*(?:disc|disk|cd)[\s._-]*([0-9]{1,2})(?:\s+of\s+[0-9]{1,2})?\s*\)|\[\s*(?:disc|disk|cd)[\s._-]*([0-9]{1,2})(?:\s+of\s+[0-9]{1,2})?\s*\])`)
	trailingDisc  = regexp.MustCompile(`(?i)[\s._-]+(?:disc|disk|cd)[\s._-]*([0-9]{1,2})(?:\s+of\s+[0-9]{1,2})?$`)
)

// DiscInfo recognizes only an explicit numbered disc marker in a disc-media
// filename. Edition text stays in the identity: region, revision, language,
// pilot and demo labels are never stripped. A hint does not assert that any
// other disc is indexed or that the game can launch.
func DiscInfo(game domain.GameAsset) *Info {
	if strings.EqualFold(strings.TrimSpace(game.CatalogRole), "dependency") || !discPlatform(game.Platform) {
		return nil
	}
	filePath := strings.TrimSpace(game.RelPath)
	if filePath == "" {
		filePath = strings.TrimSpace(game.FilePath)
	}
	if filePath == "" {
		return nil
	}
	filePath = strings.ReplaceAll(filePath, `\`, "/")
	name := path.Base(filePath)
	ext := strings.ToLower(path.Ext(name))
	switch ext {
	case ".chd", ".iso", ".cue", ".gdi", ".ccd", ".mds":
	default:
		// Existing M3U / PC-98 manifests already own their disc structure.
		return nil
	}
	title, number, ok := splitDisc(strings.TrimSuffix(name, path.Ext(name)))
	if !ok {
		return nil
	}
	// Hashing the scope keeps source paths out of client metadata. No file hash
	// participates: identical content is not evidence of a shared edition.
	identity, _ := json.Marshal([]any{game.LibraryID, strings.ToLower(strings.TrimSpace(game.Platform)), path.Dir(filePath), ext, strings.ToLower(strings.TrimSpace(game.Region)), strings.ToLower(title)})
	digest := sha256.Sum256(identity)
	return &Info{GroupKey: "disc-" + hex.EncodeToString(digest[:]), Title: title, DiscNumber: number}
}

func splitDisc(name string) (string, int, bool) {
	matches := bracketedDisc.FindAllStringSubmatchIndex(name, -1)
	pattern := bracketedDisc
	if len(matches) == 0 {
		pattern = trailingDisc
		matches = pattern.FindAllStringSubmatchIndex(name, -1)
	}
	if len(matches) != 1 {
		return "", 0, false
	}
	match := matches[0]
	number := 0
	for index := 2; index+1 < len(match); index += 2 {
		if match[index] >= 0 {
			number, _ = strconv.Atoi(name[match[index]:match[index+1]])
			break
		}
	}
	if number < 1 {
		return "", 0, false
	}
	title := strings.Join(strings.Fields(name[:match[0]]+" "+name[match[1]:]), " ")
	title = strings.Trim(title, " ._-\t")
	if title == "" || bracketedDisc.MatchString(title) || trailingDisc.MatchString(title) {
		return "", 0, false
	}
	return title, number, true
}

func discPlatform(platform string) bool {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "3do", "psx", "ps1", "saturn", "dreamcast", "ps2", "ngc", "segacd", "pcecd":
		return true
	default:
		return false
	}
}
