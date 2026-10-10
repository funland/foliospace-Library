package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVideoSeriesSafeInheritanceAndConflicts(t *testing.T) {
	root := t.TempDir()
	show := filepath.Join(root, "Show")
	season := filepath.Join(show, "Season 02")
	os.MkdirAll(season, 0700)
	nfo := filepath.Join(show, "tvshow.nfo")
	os.WriteFile(nfo, []byte(`<tvshow><title>Confirmed local title</title><year>2001</year></tvshow>`), 0600)
	path := filepath.Join(season, "E03.mkv")
	m, first, e := readVideoLocalMetadataWithin(path, root)
	if e != nil || m.Kind != "episode" || m.ShowTitle != "Confirmed local title" || *m.Season != 2 || *m.Episode != 3 {
		t.Fatal(m, e)
	}
	os.WriteFile(nfo, []byte(`<tvshow><title>Changed</title></tvshow>`), 0600)
	m, second, e := readVideoLocalMetadataWithin(path, root)
	if e != nil || first == second || m.ShowTitle != "Changed" {
		t.Fatal("sidecar not observed", m, e)
	}
	m, _, e = readVideoLocalMetadataWithin(filepath.Join(season, "Show S01E03.mkv"), root)
	if e != nil || m.Warning != "season-number-conflict" || m.Kind != "unknown" {
		t.Fatal("conflict guessed", m, e)
	}
	os.WriteFile(filepath.Join(season, "Show S02E03.nfo"), []byte(`<episodedetails><season>2</season><episode>4</episode></episodedetails>`), 0600)
	m, _, _ = readVideoLocalMetadataWithin(filepath.Join(season, "Show S02E03.mkv"), root)
	if m.Kind != "unknown" || m.Warning != "episode-number-conflict" {
		t.Fatal(m)
	}
	m, _, _ = readVideoLocalMetadataWithin(filepath.Join(season, "unnumbered.mkv"), root)
	if m.Kind != "unknown" {
		t.Fatal("invented episode", m)
	}
	m, _, _ = readVideoLocalMetadataWithin(path, season)
	if m.ShowTitle == "Changed" {
		t.Fatal("escaped library root")
	}
	os.Remove(nfo)
	outside := filepath.Join(t.TempDir(), "outside.nfo")
	os.WriteFile(outside, []byte(`<tvshow><title>Secret</title></tvshow>`), 0600)
	os.Symlink(outside, nfo)
	m, _, _ = readVideoLocalMetadataWithin(path, root)
	if m.ShowTitle == "Secret" || m.Warning != "tvshow-nfo-unsafe" {
		t.Fatal("followed symlink", m)
	}
	os.Remove(nfo)
	os.WriteFile(nfo, []byte(`<!DOCTYPE tvshow [<!ENTITY xx SYSTEM "file:///etc/passwd">]><tvshow><title>&xx;</title></tvshow>`), 0600)
	m, _, _ = readVideoLocalMetadataWithin(path, root)
	if m.Kind == "episode" || m.Warning != "tvshow-nfo-invalid" {
		t.Fatal("unsafe XML", m)
	}
}
