package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"foliospace-reader/internal/domain"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func readyMetadataWorkflowJob(t *testing.T, s *Store, videoID int64, candidate domain.VideoCandidate) domain.VideoMetadataPreviewInput {
	t.Helper()
	job, err := s.QueueVideoMetadata(videoID, "Synthetic query", candidate.Kind)
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := s.ClaimVideoMetadataJob()
	if err != nil || !ok || claimed.ID != job.ID {
		t.Fatalf("claim: %+v %v %v", claimed, ok, err)
	}
	if err = s.FinishVideoMetadataJob(claimed, []domain.VideoCandidate{candidate}, "", 0); err != nil {
		t.Fatal(err)
	}
	return domain.VideoMetadataPreviewInput{VideoID: videoID, JobID: job.ID}
}

func TestMetadataWorkflowPreviewReadOnlyAndApplyPreservesSharedWork(t *testing.T) {
	s, c, v := videoFixture(t)
	other, err := s.UpsertVideo(domain.VideoAsset{LibraryID: v.LibraryID, FilePath: filepath.Join(filepath.Dir(v.FilePath), "other.mkv"), Title: "Other", Format: "mkv", MTime: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	// Keep the existing sibling as the work card's representative. Lower-ID
	// targets with different overrides are rejected by the separate guard test.
	v, other = other, v
	if err = s.MatchVideo(other.ID, domain.VideoMatchInput{Title: "Existing title", Kind: "movie", Year: 1999, Overview: "Existing shared overview", Provider: "tmdb", ProviderID: "42"}); err != nil {
		t.Fatal(err)
	}
	if err = s.SetVideoMetadataOverride(v.ID, "title", `""`); err != nil {
		t.Fatal(err)
	}
	if err = s.SetVideoMetadataOverride(v.ID, "overview", `""`); err != nil {
		t.Fatal(err)
	}
	if err = s.SetVideoMetadataOverride(v.ID, "year", `0`); err != nil {
		t.Fatal(err)
	}
	p2, err := s.CreateProfile("Second", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range []int64{1, p2.ID} {
		if _, err = c.Exec(`INSERT INTO video_playback_state(profile_id,video_id,media_key,revision,position_seconds,duration_seconds,completed,updated_at) VALUES(?,?,'saved-key',4,21,100,0,123)`, profile, v.ID); err != nil {
			t.Fatal(err)
		}
	}
	in := readyMetadataWorkflowJob(t, s, v.ID, domain.VideoCandidate{ID: "42", Title: "New provider title", Kind: "movie", Year: 2026, Overview: "Replacement overview"})
	before := metadataWorkflowTableRows(t, c, `SELECT w.*,d.* FROM video_works w JOIN video_work_details d ON d.work_id=w.id ORDER BY w.id`)
	otherMetadata, err := s.VideoMetadata(other.ID)
	if err != nil {
		t.Fatal(err)
	}
	displayBefore, err := s.VideoWork(otherMetadata.WorkID)
	if err != nil {
		t.Fatal(err)
	}
	privateBefore := metadataWorkflowTableRows(t, c, `SELECT * FROM video_playback_state ORDER BY profile_id,video_id`)
	if _, err = c.Exec(`PRAGMA query_only=ON`); err != nil {
		t.Fatal(err)
	}
	snap, err := s.PreviewVideoMetadataWorkflow(1, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.VideoMetadataWorkflowJob(1, v.ID, in.JobID); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Exec(`PRAGMA query_only=OFF`); err != nil {
		t.Fatal(err)
	}
	if !snap.Preview.ReusesWork || snap.Preview.Proposed.Title != "" || snap.Preview.Proposed.Overview != "" || snap.Preview.Proposed.Year != 0 || len(snap.Preview.LockedFields) != 3 {
		t.Fatalf("preview: %+v", snap.Preview)
	}
	result, err := s.ApplyVideoMetadataWorkflow(1, in, snap.Fingerprint, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if result.WorkID != snap.Preview.Proposed.WorkID || result.Scope != "shared-library" || result.Generation != snap.Preview.Generation+1 {
		t.Fatalf("result: %+v", result)
	}
	if after := metadataWorkflowTableRows(t, c, `SELECT w.*,d.* FROM video_works w JOIN video_work_details d ON d.work_id=w.id ORDER BY w.id`); !reflect.DeepEqual(before, after) {
		t.Fatalf("shared work changed: %v -> %v", before, after)
	}
	displayAfter, err := s.VideoWork(otherMetadata.WorkID)
	if err != nil {
		t.Fatal(err)
	}
	if displayAfter.Title != displayBefore.Title || displayAfter.Year != displayBefore.Year || displayAfter.Overview != displayBefore.Overview {
		t.Fatalf("shared display changed: %+v -> %+v", displayBefore, displayAfter)
	}
	if after := metadataWorkflowTableRows(t, c, `SELECT * FROM video_playback_state ORDER BY profile_id,video_id`); !reflect.DeepEqual(privateBefore, after) {
		t.Fatalf("private state changed: %v -> %v", privateBefore, after)
	}
	m, err := s.VideoMetadata(v.ID)
	if err != nil || m.Title != "" || m.Overview != "" || m.Year != 0 || len(m.LockedFields) != 3 {
		t.Fatalf("overrides lost: %+v %v", m, err)
	}
	if _, err = s.ApplyVideoMetadataWorkflow(1, in, snap.Fingerprint, time.Now().Add(time.Minute)); !errors.Is(err, ErrMetadataJobStale) {
		t.Fatalf("replay accepted: %v", err)
	}
}

func TestMetadataWorkflowStaleSnapshots(t *testing.T) {
	for _, tc := range []struct {
		name, sql string
		candidate domain.VideoCandidate
	}{
		{"candidate-payload", `UPDATE video_metadata_jobs SET result_json='[{"id":"42","title":"Changed","kind":"movie","year":2000,"overview":"different"}]'`, domain.VideoCandidate{ID: "42", Title: "Selected", Kind: "movie", Year: 2000}},
		{"job-status", `UPDATE video_metadata_jobs SET status='cancelled'`, domain.VideoCandidate{ID: "42", Title: "Selected", Kind: "movie"}},
		{"job-expiry", `UPDATE video_metadata_jobs SET created_at=datetime('now','-151 days')`, domain.VideoCandidate{ID: "42", Title: "Selected", Kind: "movie"}},
		{"file-scope", `UPDATE videos SET size=size+1`, domain.VideoCandidate{ID: "42", Title: "Selected", Kind: "movie"}},
		{"local-input", `INSERT INTO video_metadata_inputs(video_id,revision,local_json) SELECT id,'changed','{}' FROM videos`, domain.VideoCandidate{ID: "42", Title: "Selected", Kind: "movie"}},
		{"manual-override", `INSERT INTO video_metadata_overrides(video_id,field,value_json) SELECT id,'title','""' FROM videos`, domain.VideoCandidate{ID: "42", Title: "Selected", Kind: "movie"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, c, v := videoFixture(t)
			in := readyMetadataWorkflowJob(t, s, v.ID, tc.candidate)
			snap, err := s.PreviewVideoMetadataWorkflow(1, in)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = c.Exec(tc.sql); err != nil {
				t.Fatal(err)
			}
			if _, err = s.ApplyVideoMetadataWorkflow(1, in, snap.Fingerprint, time.Now().Add(time.Minute)); err == nil {
				t.Fatal("stale snapshot applied")
			}
			var links int
			if err = c.QueryRow(`SELECT COUNT(*) FROM video_file_links WHERE video_id=?`, v.ID).Scan(&links); err != nil || links != 0 {
				t.Fatalf("partial apply %d %v", links, err)
			}
		})
	}
}

func TestMetadataWorkflowReusedWorkMutationInvalidatesPreview(t *testing.T) {
	s, c, v := videoFixture(t)
	if err := s.MatchVideo(v.ID, domain.VideoMatchInput{Title: "Original", Kind: "movie", Provider: "tmdb", ProviderID: "42"}); err != nil {
		t.Fatal(err)
	}
	in := readyMetadataWorkflowJob(t, s, v.ID, domain.VideoCandidate{ID: "42", Title: "Provider", Kind: "movie"})
	snap, err := s.PreviewVideoMetadataWorkflow(1, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Exec(`UPDATE video_work_details SET overview='Changed without generation increment'`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ApplyVideoMetadataWorkflow(1, in, snap.Fingerprint, time.Now().Add(time.Minute)); !errors.Is(err, ErrMetadataPreviewStale) {
		t.Fatalf("shared state race accepted: %v", err)
	}
}

func TestMetadataWorkflowEpisodeBoundsAndProfileScope(t *testing.T) {
	s, c, v := videoFixture(t)
	in := readyMetadataWorkflowJob(t, s, v.ID, domain.VideoCandidate{ID: "7", Title: "Synthetic show", Kind: "series"})
	if _, err := s.PreviewVideoMetadataWorkflow(1, in); !errors.Is(err, ErrMetadataArguments) {
		t.Fatalf("missing episode accepted: %v", err)
	}
	season, episode := 0, 1
	in.Season, in.Episode = &season, &episode
	for _, profile := range []int64{0, -1, 999} {
		if _, err := s.PreviewVideoMetadataWorkflow(profile, in); err == nil {
			t.Fatalf("profile %d defaulted", profile)
		}
	}
	other := in
	other.VideoID++
	if _, err := s.PreviewVideoMetadataWorkflow(1, other); err == nil {
		t.Fatal("cross-video job accepted")
	}
	snap, err := s.PreviewVideoMetadataWorkflow(1, in)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.ApplyVideoMetadataWorkflow(1, in, snap.Fingerprint, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	var gotSeason, gotEpisode int
	if err = c.QueryRow(`SELECT s.number,e.number FROM video_file_links f JOIN video_episodes e ON e.id=f.episode_id JOIN video_seasons s ON s.id=e.season_id WHERE f.video_id=?`, v.ID).Scan(&gotSeason, &gotEpisode); err != nil || gotSeason != 0 || gotEpisode != 1 || result.WorkID == 0 {
		t.Fatalf("episode mapping %d/%d %+v %v", gotSeason, gotEpisode, result, err)
	}
}

func TestMetadataWorkflowConcurrentApplyOnlyOneSucceeds(t *testing.T) {
	s, _, v := videoFixture(t)
	in := readyMetadataWorkflowJob(t, s, v.ID, domain.VideoCandidate{ID: "42", Title: "Synthetic", Kind: "movie"})
	snap, err := s.PreviewVideoMetadataWorkflow(1, in)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.ApplyVideoMetadataWorkflow(1, in, snap.Fingerprint, time.Now().Add(time.Minute))
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("successes=%d", success)
	}
}

func TestMetadataWorkflowRejectsSharedDisplayChanges(t *testing.T) {
	for _, origin := range []bool{false, true} {
		t.Run(map[bool]string{false: "destination", true: "origin"}[origin], func(t *testing.T) {
			s, c, target := videoFixture(t)
			sibling, err := s.UpsertVideo(domain.VideoAsset{LibraryID: target.LibraryID, FilePath: target.FilePath + ".sibling", Title: "Sibling", Format: "mkv", MTime: time.Now()})
			if err != nil {
				t.Fatal(err)
			}
			match := domain.VideoMatchInput{Title: "Shared", Kind: "movie", Year: 2000, Overview: "Shared overview", Provider: "tmdb", ProviderID: "42"}
			if err = s.MatchVideo(sibling.ID, match); err != nil {
				t.Fatal(err)
			}
			candidateID := "42"
			if origin {
				if err = s.MatchVideo(target.ID, match); err != nil {
					t.Fatal(err)
				}
				candidateID = "43"
			}
			for _, field := range []string{"title", "overview"} {
				if err = s.SetVideoMetadataOverride(target.ID, field, `""`); err != nil {
					t.Fatal(err)
				}
			}
			if err = s.SetVideoMetadataOverride(target.ID, "year", `0`); err != nil {
				t.Fatal(err)
			}
			in := readyMetadataWorkflowJob(t, s, target.ID, domain.VideoCandidate{ID: candidateID, Title: "Destination", Kind: "movie"})
			before := metadataWorkflowTableRows(t, c, `SELECT * FROM video_file_links ORDER BY video_id,version_id`)
			if _, err = s.PreviewVideoMetadataWorkflow(1, in); !errors.Is(err, ErrMetadataSharedWork) {
				t.Fatalf("shared display change accepted: %v", err)
			}
			if after := metadataWorkflowTableRows(t, c, `SELECT * FROM video_file_links ORDER BY video_id,version_id`); after != before {
				t.Fatal("preview changed links")
			}
		})
	}
}

func TestMetadataWorkflowSiblingEditsInvalidateReceipt(t *testing.T) {
	s, _, sibling := videoFixture(t)
	target, err := s.UpsertVideo(domain.VideoAsset{LibraryID: sibling.LibraryID, FilePath: sibling.FilePath + ".target", Title: "Target", Format: "mkv", MTime: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.MatchVideo(sibling.ID, domain.VideoMatchInput{Title: "Shared", Kind: "movie", Provider: "tmdb", ProviderID: "42"}); err != nil {
		t.Fatal(err)
	}
	in := readyMetadataWorkflowJob(t, s, target.ID, domain.VideoCandidate{ID: "42", Title: "Provider", Kind: "movie"})
	snap, err := s.PreviewVideoMetadataWorkflow(1, in)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetVideoMetadataOverride(sibling.ID, "overview", `"Sibling edit"`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ApplyVideoMetadataWorkflow(1, in, snap.Fingerprint, time.Now().Add(time.Minute)); !errors.Is(err, ErrMetadataPreviewStale) {
		t.Fatalf("sibling change missed: %v", err)
	}
}

func TestMetadataWorkflowRejectsExpiredReusedWork(t *testing.T) {
	s, c, v := videoFixture(t)
	if err := s.MatchVideo(v.ID, domain.VideoMatchInput{Title: "Shared", Kind: "movie", Provider: "tmdb", ProviderID: "42"}); err != nil {
		t.Fatal(err)
	}
	in := readyMetadataWorkflowJob(t, s, v.ID, domain.VideoCandidate{ID: "42", Title: "Provider", Kind: "movie"})
	if _, err := c.Exec(`UPDATE video_work_details SET expires_at=?`, time.Now().Add(-time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PreviewVideoMetadataWorkflow(1, in); !errors.Is(err, ErrMetadataJobStale) {
		t.Fatalf("expired reused work accepted: %v", err)
	}
	if _, err := c.Exec(`UPDATE video_work_details SET expires_at=0`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PreviewVideoMetadataWorkflow(1, in); !errors.Is(err, ErrMetadataJobStale) {
		t.Fatalf("legacy zero-expiry work accepted: %v", err)
	}
}

func TestMetadataWorkflowRejectsSharedEpisodeRepresentativeChanges(t *testing.T) {
	for _, origin := range []bool{false, true} {
		t.Run(map[bool]string{false: "destination", true: "origin"}[origin], func(t *testing.T) {
			s, c, target := videoFixture(t)
			sibling, err := s.UpsertVideo(domain.VideoAsset{LibraryID: target.LibraryID, FilePath: target.FilePath + ".sibling", Title: "Sibling episode title", Format: "mkv", MTime: time.Now()})
			if err != nil {
				t.Fatal(err)
			}
			season, one, two := 1, 1, 2
			match := domain.VideoMatchInput{Title: "Series", Kind: "series", Provider: "tmdb", ProviderID: "42", Season: &season, Episode: &one}
			if err = s.MatchVideo(sibling.ID, match); err != nil {
				t.Fatal(err)
			}
			if !origin {
				match.Episode = &two
			}
			if err = s.MatchVideo(target.ID, match); err != nil {
				t.Fatal(err)
			}
			in := readyMetadataWorkflowJob(t, s, target.ID, domain.VideoCandidate{ID: "42", Title: "Series", Kind: "series"})
			in.Season = &season
			if origin {
				in.Episode = &two
			} else {
				in.Episode = &one
			}
			m, err := s.VideoMetadata(sibling.ID)
			if err != nil {
				t.Fatal(err)
			}
			before, err := s.VideoEpisodes(m.WorkID, nil, 20, 0)
			if err != nil {
				t.Fatal(err)
			}
			links := metadataWorkflowTableRows(t, c, `SELECT * FROM video_file_links ORDER BY video_id,version_id`)
			if _, err = s.PreviewVideoMetadataWorkflow(1, in); !errors.Is(err, ErrMetadataSharedWork) {
				t.Fatalf("episode representative changed: %v", err)
			}
			after, err := s.VideoEpisodes(m.WorkID, nil, 20, 0)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) || links != metadataWorkflowTableRows(t, c, `SELECT * FROM video_file_links ORDER BY video_id,version_id`) {
				t.Fatal("rejected preview changed episode display")
			}
		})
	}
}

func TestMetadataWorkflowEmptyOverridesMatchDisplayedNewWork(t *testing.T) {
	s, _, v := videoFixture(t)
	for _, field := range []string{"title", "overview"} {
		if err := s.SetVideoMetadataOverride(v.ID, field, `""`); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetVideoMetadataOverride(v.ID, "year", `0`); err != nil {
		t.Fatal(err)
	}
	in := readyMetadataWorkflowJob(t, s, v.ID, domain.VideoCandidate{ID: "42", Title: "Provider", Kind: "movie", Year: 2026, Overview: "Provider overview"})
	snap, err := s.PreviewVideoMetadataWorkflow(1, in)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.ApplyVideoMetadataWorkflow(1, in, snap.Fingerprint, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	w, err := s.VideoWork(result.WorkID)
	if err != nil {
		t.Fatal(err)
	}
	if w.Title != snap.Preview.Proposed.Title || w.Year != snap.Preview.Proposed.Year || w.Overview != snap.Preview.Proposed.Overview || w.Title != "" || w.Year != 0 || w.Overview != "" {
		t.Fatalf("display differs from preview %+v %+v", snap.Preview.Proposed, w)
	}
	files, err := s.VideoWorkFiles(result.WorkID, nil, 20, 0)
	if err != nil || len(files.Items) != 1 || files.Items[0].Title != "" {
		t.Fatalf("file override lost %+v %v", files, err)
	}
}

func TestMetadataWorkflowExpiryWhileWaitingForTransaction(t *testing.T) {
	s, c, v := videoFixture(t)
	in := readyMetadataWorkflowJob(t, s, v.ID, domain.VideoCandidate{ID: "42", Title: "Synthetic", Kind: "movie"})
	snap, err := s.PreviewVideoMetadataWorkflow(1, in)
	if err != nil {
		t.Fatal(err)
	}
	blocker, err := c.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	expires := time.Now().Add(40 * time.Millisecond)
	finished := make(chan error, 1)
	go func() { _, err := s.ApplyVideoMetadataWorkflow(1, in, snap.Fingerprint, expires); finished <- err }()
	<-time.After(time.Until(expires) + 20*time.Millisecond)
	if err = blocker.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err = <-finished; !errors.Is(err, ErrMetadataReceiptExpired) {
		t.Fatalf("receipt expired while blocked: %v", err)
	}
	var links int
	if err = c.QueryRow(`SELECT COUNT(*) FROM video_file_links WHERE video_id=?`, v.ID).Scan(&links); err != nil || links != 0 {
		t.Fatalf("expired apply wrote links %d %v", links, err)
	}
}

func metadataWorkflowTableRows(t *testing.T, c *sql.DB, query string) string {
	t.Helper()
	tx, err := c.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	rows, err := metadataWorkflowRows(tx, query)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
