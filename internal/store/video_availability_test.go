package store

import (
	"foliospace-reader/internal/domain"
	"testing"
	"time"
)

func TestVideoExpectedAvailabilityExpiryAndDisable(t *testing.T) {
	s, c, v := videoFixture(t)
	season, ep := 1, 1
	if e := s.MatchVideo(v.ID, domain.VideoMatchInput{Title: "Series", Kind: "series", Season: &season, Episode: &ep, Provider: "tmdb", ProviderID: "123"}); e != nil {
		t.Fatal(e)
	}
	m, _ := s.VideoMetadata(v.ID)
	data := []domain.VideoExpectedEpisode{{Season: 1, Number: 1, Title: "Owned", AirDate: "2000-01-01"}, {Season: 1, Number: 2, Title: "Aired", AirDate: "2000-01-02"}, {Season: 1, Number: 3, Title: "Future", AirDate: "2099-01-01"}, {Season: 1, Number: 4, Title: "Unknown"}}
	if e := s.SaveVideoExpected(m.WorkID, 1, "123", data); e != nil {
		t.Fatal(e)
	}
	page, e := s.VideoAvailability(m.WorkID, 1, true, 10, 0)
	if e != nil || page.ExpectedCount == nil || *page.ExpectedCount != 4 {
		t.Fatal(page, e)
	}
	for i, want := range []string{"available", "missing", "unreleased", "unknown"} {
		if page.Items[i].Availability != want {
			t.Fatal(page.Items)
		}
	}
	page, e = s.VideoAvailability(m.WorkID, 1, false, 10, 0)
	if e != nil || page.Total != 1 || page.ExpectedCount != nil || page.Source != "local" {
		t.Fatal(page, e)
	}
	c.Exec(`UPDATE video_expected_seasons SET expires_at=?`, time.Now().Unix()-1)
	page, e = s.VideoAvailability(m.WorkID, 1, true, 10, 0)
	if e != nil || page.Total != 1 || page.ExpectedCount != nil {
		t.Fatal("expired", page, e)
	}
	if _, e = s.PurgeVideoProviderCache(true); e != nil {
		t.Fatal(e)
	}
	var n int
	c.QueryRow(`SELECT COUNT(*) FROM video_expected_episodes`).Scan(&n)
	if n != 0 {
		t.Fatal("provider purge retained expected data")
	}
}
