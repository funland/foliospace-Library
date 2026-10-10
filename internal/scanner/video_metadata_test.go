package scanner

import (
	"foliospace-reader/internal/db"
	"foliospace-reader/internal/store"
	"os"
	"path/filepath"
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
