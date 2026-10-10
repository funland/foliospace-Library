package store

import (
	"errors"
	"fmt"
	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func playbackEvent(t *testing.T, s *Store, id, profile int64, name, kind string, pos float64) domain.VideoPlaybackState {
	t.Helper()
	state, e := s.VideoPlaybackState(id, profile)
	if e != nil {
		t.Fatal(e)
	}
	out, e := s.ApplyVideoPlayback(id, profile, domain.VideoPlaybackEvent{EventID: name, SessionID: name, ClientSequence: 1, BaseRevision: state.Revision, MediaKey: state.MediaKey, EventKind: kind, PositionSeconds: pos, DurationSeconds: 1000})
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func TestVideoPlaybackIdempotencyCASProfilesAndIdentity(t *testing.T) {
	s, c, v := videoFixture(t)
	p, _ := s.CreateProfile("Second", "", "")
	initial, e := s.VideoPlaybackState(v.ID, 0)
	if e != nil {
		t.Fatal(e)
	}
	in := domain.VideoPlaybackEvent{EventID: "one", SessionID: "device-a", ClientSequence: 1, BaseRevision: 0, MediaKey: initial.MediaKey, EventKind: "progress", PositionSeconds: 120, DurationSeconds: 1000}
	first, e := s.ApplyVideoPlayback(v.ID, 0, in)
	if e != nil || first.Revision != 1 {
		t.Fatal(first, e)
	}
	replay, e := s.ApplyVideoPlayback(v.ID, 0, in)
	if e != nil || replay != first {
		t.Fatal("replay", replay, e)
	}
	checkConflict := func(in domain.VideoPlaybackEvent, code string) {
		t.Helper()
		_, e := s.ApplyVideoPlayback(v.ID, 0, in)
		var ce *VideoPlaybackConflict
		if !errors.As(e, &ce) || ce.Code != code {
			t.Fatalf("want %s got %v", code, e)
		}
	}
	changed := in
	changed.PositionSeconds = 130
	checkConflict(changed, "playback-event-conflict")
	changed = in
	changed.EventID = "older-sequence"
	changed.BaseRevision = 1
	checkConflict(changed, "playback-sequence-conflict")
	changed = in
	changed.EventID = "device-b"
	changed.SessionID = "b"
	checkConflict(changed, "playback-revision-conflict")
	second, e := s.VideoPlaybackState(v.ID, p.ID)
	if e != nil || second.Revision != 0 {
		t.Fatal("profile leak")
	}
	if _, e = s.VideoPlaybackState(v.ID, 999999); e == nil {
		t.Fatal("unknown profile silently defaulted")
	}
	playbackEvent(t, s, v.ID, 0, "watched", "completed", 1000)
	changed = in
	changed.EventID = "offline"
	changed.ClientSequence = 2
	checkConflict(changed, "playback-revision-conflict")
	reset := playbackEvent(t, s, v.ID, 0, "unwatched", "unwatched", 0)
	if reset.Completed || reset.PositionSeconds != 0 {
		t.Fatal(reset)
	}
	if _, e = s.PurgeVideoProviderCache(true); e != nil {
		t.Fatal(e)
	}
	after, _ := s.VideoPlaybackState(v.ID, 0)
	if after != reset {
		t.Fatal("provider purge erased progress")
	}
	if e = db.Migrate(c); e != nil {
		t.Fatal(e)
	}
	after, _ = s.VideoPlaybackState(v.ID, 0)
	if after != reset {
		t.Fatal("migration erased progress")
	}
	c.Exec(`UPDATE videos SET size=size+1 WHERE id=?`, v.ID)
	after, e = s.VideoPlaybackState(v.ID, 0)
	if e != nil || !after.IdentityChanged || after.Revision != reset.Revision {
		t.Fatal("identity not invalidated", after, e)
	}
	changed.EventID = "old-content"
	changed.BaseRevision = after.Revision
	checkConflict(changed, "playback-identity-conflict")
	// Old retries retain their original acknowledgement but never restore old state.
	replay, e = s.ApplyVideoPlayback(v.ID, 0, in)
	if e != nil || replay != first {
		t.Fatal("replay after identity change", e)
	}
	after, _ = s.VideoPlaybackState(v.ID, 0)
	if !after.IdentityChanged {
		t.Fatal("replay mutated state")
	}
}

func TestVideoPlaybackConcurrentCASAndFeeds(t *testing.T) {
	s, _, v := videoFixture(t)
	state, _ := s.VideoPlaybackState(v.ID, 0)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, e := s.ApplyVideoPlayback(v.ID, 0, domain.VideoPlaybackEvent{EventID: fmt.Sprint(i), SessionID: fmt.Sprint(i), ClientSequence: 1, MediaKey: state.MediaKey, EventKind: "progress", PositionSeconds: 100})
			results <- e
		}(i)
	}
	wg.Wait()
	close(results)
	ok, conflicts := 0, 0
	for e := range results {
		if e == nil {
			ok++
		} else {
			var ce *VideoPlaybackConflict
			if errors.As(e, &ce) {
				conflicts++
			} else {
				t.Fatal(e)
			}
		}
	}
	if ok != 1 || conflicts != 1 {
		t.Fatal(ok, conflicts)
	}
	feed, e := s.VideoPlaybackFeed(0, 0, "continue", 1, 0)
	if e != nil || feed.Total != 1 || feed.Items[0].VideoID != v.ID {
		t.Fatal(feed, e)
	}
}

func TestVideoSeasonsVersionsNextUpAndRegroup(t *testing.T) {
	s, c, v := videoFixture(t)
	ids := []int64{}
	for i, pair := range [][2]int{{0, 1}, {1, 1}, {1, 2}, {1, 2}, {1, 2}, {2, 1}} {
		a, e := s.UpsertVideo(domain.VideoAsset{LibraryID: v.LibraryID, FilePath: filepath.Join(filepath.Dir(v.FilePath), fmt.Sprintf("%d.mkv", i)), Title: fmt.Sprint(i), Format: "mkv", MTime: time.Now()})
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, a.ID)
		e = s.SaveVideoLocalMetadata(a.ID, fmt.Sprint(i), domain.VideoLocalMetadata{Title: fmt.Sprint(i), Kind: "episode", ShowTitle: "Show", Season: &pair[0], Episode: &pair[1]})
		if e != nil {
			t.Fatal(e)
		}
	}
	m, _ := s.VideoMetadata(ids[1])
	work := m.WorkID
	seasons, e := s.VideoSeasons(work, 1, 0)
	if e != nil || seasons.Total != 3 || !seasons.HasMore || seasons.Items[0].Number != 0 {
		t.Fatal(seasons, e)
	}
	one := 1
	eps, e := s.VideoEpisodes(work, &one, 1, 1)
	if e != nil || eps.Total != 2 || eps.Items[0].FileCount != 3 {
		t.Fatal(eps, e)
	}
	eid := eps.Items[0].ID
	files, e := s.VideoWorkEpisodeFiles(work, &one, eid, 10, 0)
	if e != nil || files.Total != 3 || files.Items[0].EpisodeID == nil {
		t.Fatal(files, e)
	}
	w, e := s.VideoWork(work)
	if e != nil || w.AvailableEpisodeCount != 4 || w.FileCount != 6 {
		t.Fatal(w, e)
	}
	if _, e = c.Exec(`INSERT INTO video_seasons(work_id,number) VALUES(?,3)`, work); e != nil {
		t.Fatal(e)
	}
	seasons, e = s.VideoSeasons(work, 100, 0)
	if e != nil || seasons.Items[3].FileCount != 0 || seasons.Items[3].ExpectedCount != nil {
		t.Fatal("empty season", seasons, e)
	}
	playbackEvent(t, s, ids[1], 0, "complete1", "completed", 1000)
	next, e := s.VideoPlaybackFeed(0, 0, "next-up", 10, 0)
	if e != nil || next.Total != 1 || next.Items[0].VideoID != ids[2] {
		t.Fatal("next", next, e)
	}
	playbackEvent(t, s, ids[3], 0, "complete2", "completed", 1000) // alternate version of E2
	next, e = s.VideoPlaybackFeed(0, 0, "next-up", 10, 0)
	if e != nil || next.Total != 1 || next.Items[0].VideoID != ids[5] {
		t.Fatal("cross-season next", next, e)
	}
	// Title override alone preserves progress; reassociation does not.
	old, _ := s.VideoPlaybackState(ids[3], 0)
	s.SetVideoMetadataOverride(ids[3], "title", `"Corrected"`)
	after, _ := s.VideoPlaybackState(ids[3], 0)
	if old.MediaKey != after.MediaKey {
		t.Fatal("title reset identity")
	}
	md, _ := s.VideoMetadata(ids[3])
	if e = s.MatchVideo(ids[3], domain.VideoMatchInput{Generation: md.Generation, Title: "Other film", Kind: "movie"}); e != nil {
		t.Fatal(e)
	}
	after, _ = s.VideoPlaybackState(ids[3], 0)
	if !after.IdentityChanged || after.Completed {
		t.Fatal("regroup reused watched", after)
	}
}

func TestVideoWorkThousandPagingAndLibraryScope(t *testing.T) {
	s, c, v := videoFixture(t)
	tx, e := c.Begin()
	if e != nil {
		t.Fatal(e)
	}
	for i := 1; i <= 1001; i++ {
		_, e = tx.Exec(`INSERT INTO video_works(id,library_id,kind,title) VALUES(?,?,'movie','Same')`, i, v.LibraryID)
		if e != nil {
			t.Fatal(e)
		}
		tx.Exec(`INSERT INTO video_work_details(work_id,identity_key) VALUES(?,?)`, i, fmt.Sprint(i))
		tx.Exec(`INSERT INTO video_versions(id,work_id) VALUES(?,?)`, i, i)
		tx.Exec(`INSERT INTO video_file_links(video_id,version_id) VALUES(?,?)`, v.ID, i)
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	seen := map[int64]bool{}
	for offset := 0; offset < 1001; offset += 100 {
		p, e := s.VideoWorksFiltered("Same", "movie", v.LibraryID, "recent", 100, offset)
		if e != nil || p.Total != 1001 {
			t.Fatal(p, e)
		}
		for _, w := range p.Items {
			if seen[w.ID] {
				t.Fatal("duplicate page item")
			}
			seen[w.ID] = true
		}
	}
	if len(seen) != 1001 {
		t.Fatal(len(seen))
	}
	p, e := s.VideoWorksFiltered("", "", 99999, "title", 10, 0)
	if e != nil || p.Total != 0 {
		t.Fatal("scope leak", p, e)
	}
}
