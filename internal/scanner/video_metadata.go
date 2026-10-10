package scanner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"foliospace-reader/internal/domain"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const maxVideoNFOBytes = 1 << 20

var videoYearPattern = regexp.MustCompile(`^(.*?)\s*[\(\[]((?:19|20)\d{2})[\)\]]`)
var videoEpisodePattern = regexp.MustCompile(`(?i)^(.*?)\s*[ ._-]*s(\d{1,3})e(\d{1,4})(?:\D|$)`)

func readVideoLocalMetadata(path string) (domain.VideoLocalMetadata, string, error) {
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	m := domain.VideoLocalMetadata{Title: stem, Kind: "unknown", Source: "filename"}
	if p := videoYearPattern.FindStringSubmatch(stem); p != nil {
		m.Kind = "movie"
		m.Title = strings.TrimSpace(p[1])
		m.Year, _ = strconv.Atoi(p[2])
	}
	if p := videoEpisodePattern.FindStringSubmatch(stem); p != nil {
		season, _ := strconv.Atoi(p[2])
		episode, _ := strconv.Atoi(p[3])
		m.Kind = "episode"
		m.ShowTitle = strings.TrimSpace(p[1])
		if m.ShowTitle == "" {
			dir := filepath.Dir(path)
			if regexp.MustCompile(`(?i)^(season[ ._-]*|s)\d+$`).MatchString(filepath.Base(dir)) {
				dir = filepath.Dir(dir)
			}
			m.ShowTitle = filepath.Base(dir)
		}
		m.Season = &season
		m.Episode = &episode
	}
	h := sha256.New()
	io.WriteString(h, "video-local-v2\x00"+stem+"\x00"+m.ShowTitle)
	candidates := []string{strings.TrimSuffix(path, filepath.Ext(path)) + ".nfo"}
	// Folder movie.nfo is ambiguous when a directory contains multiple videos.
	folderNFO := filepath.Join(filepath.Dir(path), "movie.nfo")
	if _, err := os.Lstat(folderNFO); err == nil {
		// Avoid listing large directories for every video when no folder NFO exists.
		entries, err := os.ReadDir(filepath.Dir(path))
		if err != nil {
			return m, "", err
		}
		count := 0
		for _, e := range entries {
			if !e.IsDir() && isVideoExt(strings.ToLower(filepath.Ext(e.Name()))) {
				count++
			}
			if count > 1 {
				break
			}
		}
		if count == 1 {
			candidates = append(candidates, folderNFO)
		}
	}
	for _, candidate := range candidates {
		info, e := os.Lstat(candidate)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return m, "", e
		}
		io.WriteString(h, "\x00"+filepath.Base(candidate))
		if !info.Mode().IsRegular() || info.Size() > maxVideoNFOBytes {
			m.Warning = "nfo-unsafe-or-too-large"
			io.WriteString(h, m.Warning)
			continue
		}
		f, e := os.Open(candidate)
		if e != nil {
			return m, "", e
		}
		opened, e := f.Stat()
		if e != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
			f.Close()
			m.Warning = "nfo-changed-during-read"
			io.WriteString(h, m.Warning)
			continue
		}
		b, e := io.ReadAll(io.LimitReader(f, maxVideoNFOBytes+1))
		f.Close()
		if e != nil {
			return m, "", e
		}
		h.Write(b)
		if len(b) > maxVideoNFOBytes {
			m.Warning = "nfo-too-large"
			continue
		}
		var n struct {
			XMLName   xml.Name
			Title     string `xml:"title"`
			Plot      string `xml:"plot"`
			Year      int    `xml:"year"`
			ShowTitle string `xml:"showtitle"`
			Season    *int   `xml:"season"`
			Episode   *int   `xml:"episode"`
			IDs       []struct {
				Type string `xml:"type,attr"`
				ID   string `xml:",chardata"`
			} `xml:"uniqueid"`
		}
		// Go XML does not resolve external entities. Reject directives explicitly as well.
		upper := strings.ToUpper(string(b))
		if strings.Contains(upper, "<!DOCTYPE") || strings.Contains(upper, "<!ENTITY") || xml.Unmarshal(b, &n) != nil || (n.XMLName.Local != "movie" && n.XMLName.Local != "episodedetails") {
			m.Warning = "nfo-invalid"
			continue
		}
		if n.Year < 0 || n.Year > 9999 || (n.Season != nil && *n.Season < 0) || (n.Episode != nil && *n.Episode < 0) {
			m.Warning = "nfo-invalid-number"
			continue
		}
		m.Kind = "movie"
		if n.XMLName.Local == "episodedetails" {
			m.Kind = "episode"
		}
		if strings.TrimSpace(n.Title) != "" {
			m.Title = strings.TrimSpace(n.Title)
		}
		m.Overview = n.Plot
		if n.Year != 0 {
			m.Year = n.Year
		}
		if strings.TrimSpace(n.ShowTitle) != "" {
			m.ShowTitle = n.ShowTitle
		}
		if n.Season != nil {
			m.Season = n.Season
		}
		if n.Episode != nil {
			m.Episode = n.Episode
		}
		m.Source = "local-nfo"
		m.Warning = ""
		for _, id := range n.IDs {
			if id.Type == "tmdb" {
				v := strings.TrimSpace(id.ID)
				if i, e := strconv.ParseInt(v, 10, 64); e == nil && i > 0 {
					m.Provider = "tmdb"
					m.ProviderID = v
				}
			}
		}
		break
	}
	return m, hex.EncodeToString(h.Sum(nil)), nil
}

func (s *Scanner) refreshVideoLocalMetadata(path string) error {
	v, err := s.store.VideoByPath(path)
	if err != nil {
		return err
	}
	m, revision, err := readVideoLocalMetadata(path)
	if err != nil {
		return err
	}
	return s.store.SaveVideoLocalMetadata(v.ID, revision, m)
}
