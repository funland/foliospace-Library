package store

import (
	"database/sql"
	"errors"
	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"path/filepath"
	"testing"
	"time"
)

func videoFixture(t *testing.T) (*Store, *sql.DB, domain.VideoAsset) {
	t.Helper()
	c, e := db.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { c.Close() })
	s := New(c)
	l, e := s.CreateLibraryWithType("Videos", t.TempDir(), "video")
	if e != nil {
		t.Fatal(e)
	}
	v, e := s.UpsertVideo(domain.VideoAsset{LibraryID: l.ID, FilePath: filepath.Join(l.RootPath, "file.mkv"), Title: "File", Format: "mkv", MTime: time.Now()})
	if e != nil {
		t.Fatal(e)
	}
	return s, c, v
}
func TestVideoWorkProtectionGroupingAndPaging(t *testing.T) {
	s, _, v := videoFixture(t)
	season, episode := 1, 2
	local := domain.VideoLocalMetadata{Title: "Episode", ShowTitle: "Series", Kind: "episode", Season: &season, Episode: &episode}
	if err := s.SaveVideoLocalMetadata(v.ID, "one", local); err != nil {
		t.Fatal(err)
	}
	m, _ := s.VideoMetadata(v.ID)
	if m.WorkID == 0 || m.Status != "local" {
		t.Fatalf("local %+v", m)
	}
	p, e := s.VideoWorks("series", "series", 1, 0)
	if e != nil || p.Total != 1 || len(p.Items) != 1 {
		t.Fatalf("page %+v %v", p, e)
	}
	files, e := s.VideoWorkFiles(m.WorkID, &season, 1, 0)
	if e != nil || len(files.Items) != 1 || *files.Items[0].Episode != 2 {
		t.Fatalf("files %+v %v", files, e)
	}
	local.Overview = "Updated local description"
	if e = s.SaveVideoLocalMetadata(v.ID, "one-updated", local); e != nil {
		t.Fatal(e)
	}
	w, e := s.VideoWork(m.WorkID)
	if e != nil || w.Overview != local.Overview {
		t.Fatal("local work description not refreshed", w, e)
	}
	m, _ = s.VideoMetadata(v.ID)
	in := domain.VideoMatchInput{Title: "Chosen movie", Kind: "movie", Year: 2000, Generation: m.Generation, Version: "Director cut"}
	if e = s.MatchVideo(v.ID, in); e != nil {
		t.Fatal(e)
	}
	confirmed, _ := s.VideoMetadata(v.ID)
	local.ShowTitle = "Wrong series"
	if e = s.SaveVideoLocalMetadata(v.ID, "two", local); e != nil {
		t.Fatal(e)
	}
	after, _ := s.VideoMetadata(v.ID)
	if after.WorkID != confirmed.WorkID || after.Status != "confirmed" {
		t.Fatal("rescan overwrote confirmed association")
	}
	if e = s.MatchVideo(v.ID, in); !errors.Is(e, ErrVideoRevision) {
		t.Fatalf("stale write accepted: %v", e)
	}
	if e = s.UnlinkVideo(v.ID, after.Generation); e != nil {
		t.Fatal(e)
	}
	if e = s.SaveVideoLocalMetadata(v.ID, "three", local); e != nil {
		t.Fatal(e)
	}
	after, _ = s.VideoMetadata(v.ID)
	if after.WorkID != 0 || after.Status != "unlinked" {
		t.Fatal("unlink automatically rematched")
	}
	p, e = s.VideoWorks("", "", 10, 0)
	if e != nil || p.Total != 0 {
		t.Fatal("orphan work visible", p, e)
	}
}
func TestVideoJobRecoveryStaleAndCancel(t *testing.T) {
	s, _, v := videoFixture(t)
	j, e := s.QueueVideoMetadata(v.ID, "Movie", "movie")
	if e != nil {
		t.Fatal(e)
	}
	duplicate, e := s.QueueVideoMetadata(v.ID, "Movie", "movie")
	if e != nil || duplicate.ID != j.ID {
		t.Fatal("not deduplicated")
	}
	claimed, ok, e := s.ClaimVideoMetadataJob()
	if e != nil || !ok {
		t.Fatal(e)
	}
	if e = s.RecoverVideoMetadataJobs(); e != nil {
		t.Fatal(e)
	}
	claimed, ok, e = s.ClaimVideoMetadataJob()
	if e != nil || !ok || claimed.Attempts != 2 {
		t.Fatal("not recovered")
	}
	if e = s.SetVideoMetadataOverride(v.ID, "title", `"New"`); e != nil {
		t.Fatal(e)
	}
	if e = s.FinishVideoMetadataJob(claimed, nil, "", 0); e != nil {
		t.Fatal(e)
	}
	stale, _ := s.VideoMetadataJob(j.ID)
	if stale.Status != "stale" {
		t.Fatal("obsolete result accepted")
	}
	j, _ = s.QueueVideoMetadata(v.ID, "Next", "movie")
	claimed, _, _ = s.ClaimVideoMetadataJob()
	if e = s.CancelVideoMetadataJob(j.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.FinishVideoMetadataJob(claimed, nil, "", 0); e != nil {
		t.Fatal(e)
	}
	cancelled, _ := s.VideoMetadataJob(j.ID)
	if cancelled.Status != "cancelled" {
		t.Fatal("cancel overwritten")
	}
}
func TestVideoMigrationBackupRestore(t *testing.T) {
	s, c, v := videoFixture(t)
	if e := s.SetVideoMetadataOverride(v.ID, "title", `"Preserved"`); e != nil {
		t.Fatal(e)
	}
	destination := t.TempDir()
	backup := filepath.Join(destination, "foliospace-reader.db")
	// SQLite creates a consistent backup even in WAL mode, unlike copying the main file.
	if _, e := c.Exec(`VACUUM INTO ?`, backup); e != nil {
		t.Fatal(e)
	}
	restored, e := db.Open(destination)
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	if e = db.Migrate(restored); e != nil {
		t.Fatal(e)
	}
	m, e := New(restored).VideoMetadata(v.ID)
	if e != nil || m.Title != "Preserved" {
		t.Fatalf("restore %+v %v", m, e)
	}
	a, e := New(restored).VideoByID(v.ID)
	if e != nil || a.FilePath != v.FilePath || a.ID != v.ID {
		t.Fatal("legacy identity lost")
	}
}

func TestVideoLegacyDatabaseUpgrade(t *testing.T) {
	s, c, v := videoFixture(t)
	// Recreate the pre-phase database boundary: original catalog remains, no new tables.
	for _, table := range []string{"video_artwork", "video_metadata_jobs", "video_file_links", "video_versions", "video_episodes", "video_seasons", "video_work_details", "video_works", "video_metadata_inputs", "video_metadata_overrides", "video_match_state"} {
		if _, e := c.Exec(`DROP TABLE ` + table); e != nil {
			t.Fatal(table, e)
		}
	}
	if e := db.Migrate(c); e != nil {
		t.Fatal(e)
	}
	if e := db.Migrate(c); e != nil {
		t.Fatal(e)
	}
	a, e := s.VideoByID(v.ID)
	if e != nil || a.FilePath != v.FilePath {
		t.Fatal("legacy file identity changed")
	}
	if e = s.SaveVideoLocalMetadata(v.ID, "new", domain.VideoLocalMetadata{Kind: "movie", Title: "Upgraded"}); e != nil {
		t.Fatal(e)
	}
	m, e := s.VideoMetadata(v.ID)
	if e != nil || m.WorkID == 0 {
		t.Fatal(m, e)
	}
}

func TestVideoJobRetryBudgetAndRestartExhaustion(t *testing.T) {
	s, c, v := videoFixture(t)
	j, e := s.QueueVideoMetadata(v.ID, "Retry", "movie")
	if e != nil {
		t.Fatal(e)
	}
	for attempt := 1; attempt <= 3; attempt++ {
		claimed, ok, e := s.ClaimVideoMetadataJob()
		if e != nil || !ok || claimed.Attempts != attempt {
			t.Fatal(claimed, ok, e)
		}
		if e = s.FinishVideoMetadataJob(claimed, nil, "provider-http-429", time.Now().Add(time.Hour).Unix()); e != nil {
			t.Fatal(e)
		}
		got, _ := s.VideoMetadataJob(j.ID)
		if attempt < 3 {
			if got.Status != "queued" {
				t.Fatal(got)
			}
			if _, ok, e = s.ClaimVideoMetadataJob(); e != nil || ok {
				t.Fatal("backoff ignored")
			}
			c.Exec(`UPDATE video_metadata_jobs SET next_at=0 WHERE id=?`, j.ID)
		} else if got.Status != "failed" {
			t.Fatal("retry budget exceeded", got)
		}
	}
	c.Exec(`UPDATE video_metadata_jobs SET status='running' WHERE id=?`, j.ID)
	if e = s.RecoverVideoMetadataJobs(); e != nil {
		t.Fatal(e)
	}
	got, _ := s.VideoMetadataJob(j.ID)
	if got.Status != "failed" {
		t.Fatal("exhausted job restarted")
	}
}
