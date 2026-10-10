package scanner

import (
	"encoding/xml"
	"foliospace-reader/internal/domain"
	"hash"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var videoSeasonFolder = regexp.MustCompile(`(?i)^(?:season[ ._-]*|s)(\d{1,3})$`)
var videoEpisodeOnly = regexp.MustCompile(`(?i)^e(\d{1,4})(?:[ ._-]|$)`)

// At most a season directory and its immediate show parent, never arbitrary
// ancestor traversal. Symlinks and NFO external entities are not followed.
func inheritVideoSeries(path, root string, m *domain.VideoLocalMetadata, h hash.Hash) {
	if m.Kind == "movie" || m.Warning == "episode-number-conflict" {
		return
	}
	dir := filepath.Dir(path)
	showDir := dir
	var folderSeason *int
	if match := videoSeasonFolder.FindStringSubmatch(filepath.Base(dir)); match != nil {
		n, _ := strconv.Atoi(match[1])
		folderSeason = &n
		showDir = filepath.Dir(dir)
	}
	if folderSeason != nil && m.Season != nil && *folderSeason != *m.Season {
		m.Warning = "season-number-conflict"
		m.Kind = "unknown"
		return
	}
	rel, err := filepath.Rel(root, showDir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return
	}
	// Reject symlink directories between the library root and the sidecar.
	for p := showDir; ; p = filepath.Dir(p) {
		info, e := os.Lstat(p)
		if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return
		}
		if p == filepath.Clean(root) {
			break
		}
		if p == filepath.Dir(p) {
			return
		}
	}
	nfo := filepath.Join(showDir, "tvshow.nfo")
	info, err := os.Lstat(nfo)
	if os.IsNotExist(err) {
		return
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxVideoNFOBytes {
		m.Warning = "tvshow-nfo-unsafe"
		return
	}
	f, err := os.Open(nfo)
	if err != nil {
		return
	}
	defer f.Close()
	actual, err := f.Stat()
	if err != nil || !os.SameFile(info, actual) {
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, maxVideoNFOBytes+1))
	if err != nil {
		return
	}
	h.Write([]byte("\x00tvshow\x00"))
	h.Write(data)
	var show struct {
		XMLName xml.Name
		Title   string `xml:"title"`
		Year    int    `xml:"year"`
	}
	upper := strings.ToUpper(string(data))
	if len(data) > maxVideoNFOBytes || strings.Contains(upper, "<!DOCTYPE") || strings.Contains(upper, "<!ENTITY") || xml.Unmarshal(data, &show) != nil || show.XMLName.Local != "tvshow" || strings.TrimSpace(show.Title) == "" {
		m.Warning = "tvshow-nfo-invalid"
		return
	}
	if folderSeason != nil && m.Season != nil && *folderSeason != *m.Season {
		m.Warning = "season-number-conflict"
		m.Kind = "unknown"
		return
	}
	if m.Season == nil {
		m.Season = folderSeason
	}
	if m.Episode == nil {
		stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if match := videoEpisodeOnly.FindStringSubmatch(stem); match != nil {
			n, _ := strconv.Atoi(match[1])
			m.Episode = &n
		}
	}
	if m.Season == nil || m.Episode == nil {
		return
	} // Never infer an episode from an unnumbered file.
	m.Kind = "episode"
	m.ShowTitle = strings.TrimSpace(show.Title)
	m.Source = "local-nfo"
	if show.Year >= 0 && show.Year <= 9999 {
		m.Year = show.Year
	}
}
