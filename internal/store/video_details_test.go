package store

import (
	"foliospace-reader/internal/domain"
	"testing"
	"time"
)

func TestVideoDetailsPreserveManualFieldsAndIdentity(t *testing.T) {
	s, c, v := videoFixture(t)
	if err := s.MatchVideo(v.ID, domain.VideoMatchInput{Title: "Provider title", Kind: "movie", Provider: "tmdb", ProviderID: "42", Overview: "Provider overview"}); err != nil {
		t.Fatal(err)
	}
	m, _ := s.VideoMetadata(v.ID)
	if err := s.SetVideoMetadataOverride(v.ID, "overview", `"My own notes"`); err != nil {
		t.Fatal(err)
	}
	w, err := s.VideoWork(m.WorkID)
	if err != nil || w.Overview != "My own notes" {
		t.Fatal("manual overview", w, err)
	}
	before, _ := s.VideoPlaybackState(v.ID, 1)
	d := domain.VideoWorkDetails{OriginalTitle: "Original", RuntimeMinutes: 123, FetchedAt: time.Now().Unix()}
	if err = s.SaveVideoWorkDetails(w, d); err != nil {
		t.Fatal(err)
	}
	got, err := s.VideoWorkDetails(w.ID)
	if err != nil || got.OriginalTitle != "Original" {
		t.Fatal(got, err)
	}
	after, _ := s.VideoPlaybackState(v.ID, 1)
	if before != after {
		t.Fatal("playback identity changed")
	}
	var title string
	c.QueryRow(`SELECT json_extract(raw_json,'$.title') FROM video_work_details WHERE work_id=?`, w.ID).Scan(&title)
	if title != "Provider title" {
		t.Fatal("match input lost", title)
	}
	w.ProviderID = "43"
	if err = s.SaveVideoWorkDetails(w, d); err == nil {
		t.Fatal("stale provider cache accepted")
	}
	c.Exec(`UPDATE video_work_details SET expires_at=1 WHERE work_id=?`, w.ID)
	got, err = s.VideoWorkDetails(w.ID)
	if err != nil || got.FetchedAt != 0 {
		t.Fatal("expired provider content served")
	}
	if err = s.SaveVideoWorkDetails(w, d); err == nil {
		t.Fatal("expired identity accepted")
	}
}
