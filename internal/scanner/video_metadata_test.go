package scanner

import (
	"crypto/sha256"
	"encoding/hex"
	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/store"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVideoLocalMetadataRefreshWithoutROMStyleHashing(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Example (2001).mp4")
	if err := os.WriteFile(path, []byte("unknown but browsable video fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	st := store.New(c)
	lib, err := st.CreateLibraryWithType("Movies", root, "video")
	if err != nil {
		t.Fatal(err)
	}
	sc := New(st)
	for _, workers := range []int{1, 2} {
		sc = NewWithWorkerCount(st, func() int { return workers })
		if j, e := sc.ScanLibrary(lib); e != nil || j.ErrorCount != 0 {
			t.Fatalf("scan: %+v %v", j, e)
		}
		v, e := st.VideoByPath(path)
		if e != nil {
			t.Fatal(e)
		}
		if err := os.WriteFile(filepath.Join(root, "Example (2001).nfo"), []byte(`<movie><title>From NFO</title><year>2003</year><plot>Local plot</plot><uniqueid type="tmdb">123</uniqueid></movie>`), 0600); err != nil {
			t.Fatal(err)
		}
		if j, e := sc.ScanLibrary(lib); e != nil || j.ErrorCount != 0 {
			t.Fatalf("rescan: %+v %v", j, e)
		}
		after, e := st.VideoByPath(path)
		if e != nil || after.ID != v.ID {
			t.Fatal("identity changed", e)
		}
		m, e := st.VideoMetadata(v.ID)
		if e != nil || m.Title != "From NFO" || m.Year != 2003 || m.ProviderID != "123" {
			t.Fatalf("metadata: %+v %v", m, e)
		}
		if err := os.Remove(filepath.Join(root, "Example (2001).nfo")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestVideoNFOBoundaries(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Film (2001).mkv")
	nfo := filepath.Join(root, "Film (2001).nfo")
	for _, content := range []string{`<!DOCTYPE movie [<!ENTITY x SYSTEM "file:///etc/passwd">]><movie><title>&x;</title></movie>`, `<bad/>`, `<movie><title>broken`} {
		if err := os.WriteFile(nfo, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		m, _, err := readVideoLocalMetadata(path)
		if err != nil {
			t.Fatal(err)
		}
		if m.Title != "Film" || m.Warning == "" {
			t.Fatalf("bad NFO should fall back: %+v", m)
		}
	}
	os.Remove(nfo)
	outside := filepath.Join(t.TempDir(), "outside.nfo")
	os.WriteFile(outside, []byte(`<movie><title>Outside</title></movie>`), 0600)
	if err := os.Symlink(outside, nfo); err != nil {
		t.Fatal(err)
	}
	m, _, err := readVideoLocalMetadata(path)
	if err != nil || m.Title == "Outside" {
		t.Fatalf("symlink accepted: %+v %v", m, err)
	}
}

func TestReleaseFilenameYearFallback(t *testing.T) {
	for _, tc := range []struct {
		name, title string
		year        int
	}{
		{"倩女幽魂3.1991.DoVi.HDR.x265.mkv", "倩女幽魂3", 1991},
		{"Film (1991).mkv", "Film", 1991},
		{"Film [1991].mkv", "Film", 1991},
		{"1917.mkv", "1917", 0},
		{"Blade.Runner.2049.2017.2160p.mkv", "Blade Runner 2049", 2017},
		{"Wonder.Woman.1984.2020.mkv", "Wonder Woman 1984", 2020},
		{"2001 A Space Odyssey.mkv", "2001 A Space Odyssey", 0},
		{"2001.A.Space.Odyssey.mkv", "2001 A Space Odyssey", 0},
		{"sample-national.theatre.live.othello.2023.1080p.web.h264-bawd.mkv", "national theatre live othello", 2023},
		{"SAMPLE_Ordinary.Movie.2023.mkv", "Ordinary Movie", 2023},
		{"sample.Ordinary.Movie.mkv", "Ordinary Movie", 0},
		{"Ordinary.Movie.mkv", "Ordinary Movie", 0},
		{"Pi.3.14.2023.mkv", "Pi 3.14", 2023},
		{"3.14.mkv", "3.14", 0},
		{"Sample.mkv", "Sample", 0},
		{"Sample.2023.mkv", "Sample", 2023},
		{"Sample (2023).mkv", "Sample", 2023},
		{"Sample Movie.mkv", "Sample Movie", 0},
		{"Sampler.Movie.mkv", "Sampler Movie", 0},
		{"sample-.mkv", "sample-", 0},
		{"sample_.mkv", "sample_", 0},
	} {
		m, _, err := readVideoLocalMetadata(filepath.Join(t.TempDir(), tc.name))
		if err != nil || m.Title != tc.title || m.Year != tc.year {
			t.Fatalf("%s: %+v %v", tc.name, m, err)
		}
	}
}

func TestYearInEpisodeNameRetainsEpisodeIdentity(t *testing.T) {
	for _, name := range []string{"Show.2024.S01E01", "Show.2024.S01E02"} {
		m, _, err := readVideoLocalMetadata(filepath.Join(t.TempDir(), name+".mkv"))
		if err != nil || m.Kind != "episode" || m.Title != strings.ReplaceAll(name, ".", " ") || m.ShowTitle != "Show 2024" || m.Season == nil || *m.Season != 1 || m.Episode == nil || *m.Episode != int(name[len(name)-1]-'0') {
			t.Fatalf("episode: %+v %v", m, err)
		}
	}
}

func TestVideoFilenameCleanupPreservesEpisodeNumbersAndFolderTitle(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		path, title, show string
	}{
		{"sample-3.14.Show.S02E003.mkv", "3.14 Show S02E003", "3.14 Show"},
		{filepath.Join("Sample.Show", "S02E003.mkv"), "S02E003", "Sample.Show"},
	} {
		m, _, err := readVideoLocalMetadataWithin(filepath.Join(root, tc.path), root)
		if err != nil || m.Title != tc.title || m.ShowTitle != tc.show || m.Season == nil || *m.Season != 2 || m.Episode == nil || *m.Episode != 3 {
			t.Fatalf("%s: %+v %v", tc.path, m, err)
		}
	}
}

func TestVideoFilenameCleanupPreservesNFOTitle(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sample-Ordinary.Movie.2023.mkv")
	nfo := strings.TrimSuffix(path, filepath.Ext(path)) + ".nfo"
	if err := os.WriteFile(nfo, []byte(`<movie><title>Sample.Authoritative.3.14</title></movie>`), 0600); err != nil {
		t.Fatal(err)
	}
	m, _, err := readVideoLocalMetadata(path)
	if err != nil || m.Title != "Sample.Authoritative.3.14" || m.Source != "local-nfo" {
		t.Fatalf("NFO title: %+v %v", m, err)
	}
}

func TestVideoFilenameCleanupRefreshPreservesOverridesAndMatch(t *testing.T) {
	root := t.TempDir()
	name := "sample-national.theatre.live.othello.2023.1080p.web.h264-bawd.mkv"
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte("unknown but browsable video fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	st := store.New(c)
	lib, err := st.CreateLibraryWithType("Movies", root, "video")
	if err != nil {
		t.Fatal(err)
	}
	sc := New(st)
	if j, err := sc.ScanLibrary(lib); err != nil || j.ErrorCount != 0 {
		t.Fatalf("scan: %+v %v", j, err)
	}
	v, err := st.VideoByPath(path)
	if err != nil {
		t.Fatal(err)
	}
	// Seed the actual previous fingerprint so a rescan of an unchanged video
	// must invalidate v3 and save the newly cleaned automatic title.
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	oldHash := sha256.Sum256([]byte("video-local-v3\x00" + stem + "\x00"))
	oldRevision := hex.EncodeToString(oldHash[:])
	old := domain.VideoLocalMetadata{Title: "sample-national.theatre.live.othello", Year: 2023, Kind: "movie", Source: "filename"}
	if err := st.SaveVideoLocalMetadata(v.ID, oldRevision, old); err != nil {
		t.Fatal(err)
	}
	if j, err := sc.ScanLibrary(lib); err != nil || j.ErrorCount != 0 {
		t.Fatalf("rescan: %+v %v", j, err)
	}
	m, err := st.VideoMetadata(v.ID)
	if err != nil || m.Revision == oldRevision || m.Title != "national theatre live othello" || m.Year != 2023 {
		t.Fatalf("refreshed metadata: %+v %v", m, err)
	}
	if err := st.MatchVideo(v.ID, domain.VideoMatchInput{Title: "Sample.TMDB.Title", Kind: "movie", Year: 2023, Provider: "tmdb", ProviderID: "123", Generation: m.Generation}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetVideoMetadataOverride(v.ID, "title", `"Sample.Manual.Title"`); err != nil {
		t.Fatal(err)
	}
	confirmed, err := st.VideoMetadata(v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveVideoLocalMetadata(v.ID, oldRevision, old); err != nil {
		t.Fatal(err)
	}
	if j, err := sc.ScanLibrary(lib); err != nil || j.ErrorCount != 0 {
		t.Fatalf("protected rescan: %+v %v", j, err)
	}
	after, err := st.VideoMetadata(v.ID)
	if err != nil || after.Title != "Sample.Manual.Title" || after.WorkID != confirmed.WorkID || after.Status != "confirmed" {
		t.Fatalf("protected metadata: %+v %v", after, err)
	}
	// Read the stored provider title directly: the presentation layer may use
	// the manual title, but rescanning must not mutate the confirmed TMDB work.
	var providerTitle string
	if err := c.QueryRow(`SELECT title FROM video_works WHERE id=?`, after.WorkID).Scan(&providerTitle); err != nil || providerTitle != "Sample.TMDB.Title" {
		t.Fatalf("provider title = %q: %v", providerTitle, err)
	}
	if err := st.DeleteVideoMetadataOverride(v.ID, "title"); err != nil {
		t.Fatal(err)
	}
	unlocked, err := st.VideoMetadata(v.ID)
	if err != nil || unlocked.Title != "national theatre live othello" {
		t.Fatalf("refreshed automatic value: %+v %v", unlocked, err)
	}
}
