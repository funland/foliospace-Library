package service

import (
	"database/sql"
	"encoding/json"
	"errors"
	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/store"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func serviceMetadataWorkflowFixture(t *testing.T, kind string) (*Service, *sql.DB, domain.VideoMetadataPreviewInput) {
	t.Helper()
	c, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	st := store.New(c)
	lib, err := st.CreateLibraryWithType("Synthetic", t.TempDir(), "video")
	if err != nil {
		t.Fatal(err)
	}
	v, err := st.UpsertVideo(domain.VideoAsset{LibraryID: lib.ID, Title: "Filename", FilePath: filepath.Join(lib.RootPath, "synthetic.mkv"), Format: "mkv", MTime: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	j, err := st.QueueVideoMetadata(v.ID, "Synthetic", kind)
	if err != nil {
		t.Fatal(err)
	}
	j, ok, err := st.ClaimVideoMetadataJob()
	if err != nil || !ok {
		t.Fatalf("claim %+v %v %v", j, ok, err)
	}
	if err = st.FinishVideoMetadataJob(j, []domain.VideoCandidate{{ID: "42", Title: "Provider title", Kind: kind, Year: 2026, Overview: "Provider overview"}}, "", 0); err != nil {
		t.Fatal(err)
	}
	in := domain.VideoMetadataPreviewInput{VideoID: v.ID, JobID: j.ID}
	if kind == "series" {
		season, episode := 0, 1
		in.Season, in.Episode = &season, &episode
	}
	return &Service{store: st, configDir: t.TempDir()}, c, in
}

func TestMetadataWorkflowReceiptConfirmationAndScope(t *testing.T) {
	s, _, in := serviceMetadataWorkflowFixture(t, "movie")
	p2, err := s.store.CreateProfile("Second", "", "")
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.PreviewVideoMetadataWorkflow(1, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Receipt) != 64 || preview.Scope != "shared-library" || preview.ExpiresAt.Before(time.Now()) || !preview.PlaybackIdentityMayChange {
		t.Fatalf("preview: %+v", preview)
	}
	apply := domain.VideoMetadataApplyInput{VideoID: in.VideoID, Receipt: preview.Receipt}
	if _, err = s.ApplyVideoMetadataWorkflow(1, apply); err == nil || err.Error() != "metadata-confirm-required" {
		t.Fatalf("unconfirmed apply: %v", err)
	}
	apply.Confirm = true
	if _, err = s.ApplyVideoMetadataWorkflow(p2.ID, apply); err == nil || err.Error() != "metadata-receipt-invalid" {
		t.Fatalf("cross-profile apply: %v", err)
	}
	wrongFile := apply
	wrongFile.VideoID++
	if _, err = s.ApplyVideoMetadataWorkflow(1, wrongFile); err == nil || err.Error() != "metadata-receipt-invalid" {
		t.Fatalf("cross-file apply: %v", err)
	}
	if _, err = s.ApplyVideoMetadataWorkflow(999, apply); !errors.Is(err, store.ErrMetadataProfile) {
		t.Fatalf("profile fallback: %v", err)
	}
	result, err := s.ApplyVideoMetadataWorkflow(1, apply)
	if err != nil || result.WorkID == 0 {
		t.Fatalf("apply %+v %v", result, err)
	}
	if _, err = s.ApplyVideoMetadataWorkflow(1, apply); err == nil || err.Error() != "metadata-receipt-invalid" {
		t.Fatalf("replay: %v", err)
	}
}

func TestMetadataWorkflowReceiptExpiryRestartAndCapacity(t *testing.T) {
	s, _, in := serviceMetadataWorkflowFixture(t, "movie")
	p, err := s.PreviewVideoMetadataWorkflow(1, in)
	if err != nil {
		t.Fatal(err)
	}
	apply := domain.VideoMetadataApplyInput{VideoID: in.VideoID, Receipt: p.Receipt, Confirm: true}
	restarted := &Service{store: s.store}
	if _, err = restarted.ApplyVideoMetadataWorkflow(1, apply); err == nil || err.Error() != "metadata-receipt-invalid" {
		t.Fatalf("restart retained receipt: %v", err)
	}
	s.metadataWorkflowMu.Lock()
	receipt := s.metadataWorkflowReceipts[p.Receipt]
	receipt.ExpiresAt = time.Now().Add(-time.Second)
	s.metadataWorkflowReceipts[p.Receipt] = receipt
	s.metadataWorkflowMu.Unlock()
	if _, err = s.ApplyVideoMetadataWorkflow(1, apply); err == nil || err.Error() != "metadata-receipt-expired" {
		t.Fatalf("expired apply: %v", err)
	}
	for i := 0; i < metadataWorkflowCapacity; i++ {
		s.metadataWorkflowReceipts[string(rune(i))] = metadataWorkflowReceipt{ExpiresAt: time.Now().Add(time.Hour)}
	}
	if _, err = s.PreviewVideoMetadataWorkflow(1, in); err == nil || err.Error() != "metadata-receipt-capacity" {
		t.Fatalf("unbounded receipts: %v", err)
	}
	s.metadataWorkflowReceipts = map[string]metadataWorkflowReceipt{"old": {ExpiresAt: time.Now().Add(-time.Hour)}}
	if _, err = s.PreviewVideoMetadataWorkflow(1, in); err != nil {
		t.Fatal(err)
	}
	if len(s.metadataWorkflowReceipts) != 1 {
		t.Fatal("expired receipt not reclaimed")
	}
}

func TestMetadataWorkflowCandidateAndCurrentChangesInvalidateReceipt(t *testing.T) {
	for _, mutation := range []string{"candidate", "override"} {
		t.Run(mutation, func(t *testing.T) {
			s, c, in := serviceMetadataWorkflowFixture(t, "movie")
			p, err := s.PreviewVideoMetadataWorkflow(1, in)
			if err != nil {
				t.Fatal(err)
			}
			if mutation == "candidate" {
				_, err = c.Exec(`UPDATE video_metadata_jobs SET result_json='[{"id":"42","title":"Changed","kind":"movie","year":2026}]' WHERE id=?`, in.JobID)
			} else {
				err = s.store.SetVideoMetadataOverride(in.VideoID, "overview", `""`)
			}
			if err != nil {
				t.Fatal(err)
			}
			apply := domain.VideoMetadataApplyInput{VideoID: in.VideoID, Receipt: p.Receipt, Confirm: true}
			if _, err = s.ApplyVideoMetadataWorkflow(1, apply); err == nil {
				t.Fatal("stale receipt applied")
			}
			if _, err = s.ApplyVideoMetadataWorkflow(1, apply); err == nil || err.Error() != "metadata-receipt-invalid" {
				t.Fatalf("failed receipt reusable: %v", err)
			}
		})
	}
}

func TestMetadataWorkflowReadPathsNeverPurgeAndRedactDiagnostics(t *testing.T) {
	s, c, in := serviceMetadataWorkflowFixture(t, "movie")
	if _, err := c.Exec(`UPDATE video_metadata_jobs SET error_code='/private/path bearer-secret' WHERE id=?`, in.JobID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Exec(`PRAGMA query_only=ON`); err != nil {
		t.Fatal(err)
	}
	job, err := s.VideoMetadataWorkflowJob(1, in.VideoID, in.JobID)
	if err != nil {
		t.Fatal(err)
	}
	if job.ErrorCode != "provider-failed" {
		t.Fatalf("diagnostic leak: %+v", job)
	}
	p, err := s.PreviewVideoMetadataWorkflow(1, in)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "/private/") || strings.Contains(string(raw), "bearer-secret") {
		t.Fatalf("private payload in preview %s", raw)
	}
	if _, err = c.Exec(`PRAGMA query_only=OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Exec(`UPDATE video_metadata_jobs SET created_at=datetime('now','-151 days') WHERE id=?`, in.JobID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.VideoMetadataWorkflowJob(1, in.VideoID, in.JobID); !errors.Is(err, store.ErrMetadataJobStale) {
		t.Fatalf("expired cache served: %v", err)
	}
	var count int
	if err = c.QueryRow(`SELECT COUNT(*) FROM video_metadata_jobs WHERE id=?`, in.JobID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("read purged row %d %v", count, err)
	}
}

func TestMetadataWorkflowConcurrentReceiptAndFrozenEpisodeSelection(t *testing.T) {
	s, _, in := serviceMetadataWorkflowFixture(t, "series")
	p, err := s.PreviewVideoMetadataWorkflow(1, in)
	if err != nil {
		t.Fatal(err)
	}
	*in.Season = 8
	*in.Episode = 9
	*p.Proposed.Season = 7
	apply := domain.VideoMetadataApplyInput{VideoID: in.VideoID, Receipt: p.Receipt, Confirm: true}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.ApplyVideoMetadataWorkflow(1, apply); results <- err }()
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
		t.Fatalf("success count %d", success)
	}
	m, err := s.store.VideoMetadata(in.VideoID)
	if err != nil {
		t.Fatal(err)
	}
	files, err := s.store.VideoWorkFiles(m.WorkID, nil, 20, 0)
	if err != nil || len(files.Items) != 1 || *files.Items[0].Season != 0 || *files.Items[0].Episode != 1 {
		t.Fatalf("receipt input mutated: %+v %v", files, err)
	}
}

func TestMetadataWorkflowSearchValidatesScopeAndNeverAutoApplies(t *testing.T) {
	s, _, in := serviceMetadataWorkflowFixture(t, "movie")
	s.videoWorker = &videoMetadataWorker{service: s, provider: &fakeVideoProvider{}}
	for _, profile := range []int64{0, -1, 999} {
		if _, err := s.SearchVideoMetadataWorkflow(profile, in.VideoID, "Synthetic", "movie"); err == nil {
			t.Fatalf("invalid profile %d accepted", profile)
		}
	}
	job, err := s.SearchVideoMetadataWorkflow(1, in.VideoID, "Synthetic next", "movie")
	if err != nil || job.Status != "queued" {
		t.Fatalf("search %+v %v", job, err)
	}
	if _, err = s.SearchVideoMetadataWorkflow(1, in.VideoID, "Different query", "movie"); err == nil || err.Error() != "metadata-job-conflict" {
		t.Fatalf("conflicting search: %v", err)
	}
	m, err := s.store.VideoMetadata(in.VideoID)
	if err != nil || m.WorkID != 0 {
		t.Fatalf("search auto-applied %+v %v", m, err)
	}
	for _, kind := range []string{"poster", "backdrop", "invalid"} {
		if _, err = s.SearchVideoMetadataWorkflow(1, in.VideoID, "Synthetic", kind); !errors.Is(err, store.ErrMetadataArguments) {
			t.Fatalf("unexpected search kind %q: %v", kind, err)
		}
	}
}

func TestMetadataWorkflowProfileLookupPreservesDatabaseFailure(t *testing.T) {
	s, c, in := serviceMetadataWorkflowFixture(t, "movie")
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := s.SearchVideoMetadataWorkflow(1, in.VideoID, "Synthetic", "movie")
	if err == nil || errors.Is(err, store.ErrMetadataProfile) {
		t.Fatalf("database fault masked as missing profile: %v", err)
	}
}
