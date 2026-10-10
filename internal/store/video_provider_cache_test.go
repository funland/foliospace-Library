package store

import (
	"fmt"
	"foliospace-reader/internal/domain"
	"testing"
	"time"
)

func TestProviderCacheExpiryAndManualPreservation(t *testing.T) {
	s, c, v := videoFixture(t)
	s.SetVideoMetadataOverride(v.ID, "title", `"My title"`)
	m, _ := s.VideoMetadata(v.ID)
	if e := s.MatchVideo(v.ID, domain.VideoMatchInput{Generation: m.Generation, Title: "Provider title", Kind: "movie", Provider: "tmdb", ProviderID: "1"}); e != nil {
		t.Fatal(e)
	}
	m, _ = s.VideoMetadata(v.ID)
	if e := s.SaveVideoArtwork(v.ID, "poster", "provider-cache", "p", "tmdb", m.Generation); e != nil {
		t.Fatal(e)
	}
	m, _ = s.VideoMetadata(v.ID)
	if e := s.SaveVideoArtwork(v.ID, "backdrop", "manual-cache", "m", "manual", m.Generation); e != nil {
		t.Fatal(e)
	}
	c.Exec(`UPDATE video_artwork SET expires_at=1 WHERE source='tmdb'`)
	c.Exec(`UPDATE video_work_details SET expires_at=1 WHERE provider='tmdb'`)
	paths, e := s.PurgeVideoProviderCache(false)
	if e != nil || len(paths) != 1 {
		t.Fatal(paths, e)
	}
	w, e := s.VideoWork(m.WorkID)
	if e != nil || w.Provider != "" || w.Title == "Provider title" {
		t.Fatal(w, e)
	}
	m, _ = s.VideoMetadata(v.ID)
	if m.Title != "My title" {
		t.Fatal("manual title removed")
	}
	path, _, _, e := s.VideoArtwork(v.ID, "backdrop")
	if e != nil || path != "manual-cache" {
		t.Fatal("manual artwork removed")
	}
	if _, e = s.VideoByID(v.ID); e != nil {
		t.Fatal("original video removed")
	}
}

func TestVideoPaginationAndLibraryIsolation(t *testing.T) {
	s, _, v := videoFixture(t)
	for i := 0; i < 125; i++ {
		a, e := s.UpsertVideo(domain.VideoAsset{LibraryID: v.LibraryID, Title: fmt.Sprintf("Movie %03d", i), FilePath: fmt.Sprintf("/synthetic/%d.mkv", i), MTime: time.Now()})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.SaveVideoLocalMetadata(a.ID, "v1", domain.VideoLocalMetadata{Title: a.Title, Kind: "movie"}); e != nil {
			t.Fatal(e)
		}
	}
	a, e := s.VideoWorks("", "", 500, 0)
	if e != nil || a.Total != 125 || len(a.Items) != 100 || !a.HasMore {
		t.Fatal(a, e)
	}
	b, e := s.VideoWorks("", "", 100, 100)
	if e != nil || len(b.Items) != 25 || b.HasMore || a.Items[99].ID == b.Items[0].ID {
		t.Fatal(b, e)
	}
	l, _ := s.CreateLibraryWithType("Other", t.TempDir(), "video")
	other, _ := s.UpsertVideo(domain.VideoAsset{LibraryID: l.ID, Title: "Other", FilePath: "/synthetic/other.mkv", MTime: time.Now()})
	m, _ := s.VideoMetadata(other.ID)
	if e = s.MatchVideo(other.ID, domain.VideoMatchInput{Generation: m.Generation, WorkID: a.Items[0].ID, Title: "Other", Kind: "movie"}); e == nil {
		t.Fatal("cross-library link accepted")
	}
}
