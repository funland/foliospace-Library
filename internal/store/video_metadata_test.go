package store

import (
	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"testing"
	"time"
)

func TestVideoMetadataSurvivesRescanAndMigration(t *testing.T) {
	c, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s := New(c)
	lib, err := s.CreateLibraryWithType("Movies", t.TempDir(), "video")
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.UpsertVideo(domain.VideoAsset{LibraryID: lib.ID, FilePath: lib.RootPath + "/film.mkv", Title: "filename", Format: "mkv", Size: 123, MTime: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveVideoLocalMetadata(v.ID, "rev1", domain.VideoLocalMetadata{Title: "Local title", Year: 2001}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetVideoMetadataOverride(v.ID, "title", `"My title"`); err != nil {
		t.Fatal(err)
	}
	if err := s.SetVideoMetadataOverride(v.ID, "overview", `""`); err != nil {
		t.Fatal(err)
	}
	v.Title = "rescanned filename"
	updated, err := s.UpsertVideo(v)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != v.ID {
		t.Fatal("video ID changed")
	}
	if err := s.SaveVideoLocalMetadata(v.ID, "rev2", domain.VideoLocalMetadata{Title: "Changed NFO", Overview: "NFO plot", Year: 2002}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := db.Migrate(c); err != nil {
			t.Fatal(err)
		}
	}
	m, err := s.VideoMetadata(v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "My title" || m.Overview != "" || m.Year != 2002 || m.Revision != "rev2" {
		t.Fatalf("metadata = %+v", m)
	}
	if err := s.DeleteVideoMetadataOverride(v.ID, "title"); err != nil {
		t.Fatal(err)
	}
	m, _ = s.VideoMetadata(v.ID)
	if m.Title != "Changed NFO" {
		t.Fatalf("reset = %+v", m)
	}
	if err := s.SetVideoMetadataOverride(v.ID, "filePath", `"/secret"`); err == nil {
		t.Fatal("unapproved field accepted")
	}
	if err := s.SetVideoMetadataOverride(v.ID, "year", `"bad"`); err == nil {
		t.Fatal("invalid field type accepted")
	}
}
