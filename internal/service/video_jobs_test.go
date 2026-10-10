package service

import (
	"context"
	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/store"
	"path/filepath"
	"testing"
	"time"
)

type fakeVideoProvider struct {
	calls  int
	action func()
}

func (p *fakeVideoProvider) Search(context.Context, string, string) ([]domain.VideoCandidate, error) {
	p.calls++
	if p.action != nil {
		p.action()
	}
	return []domain.VideoCandidate{{ID: "1", Kind: "movie", Title: "Synthetic"}}, nil
}
func TestVideoWorkerNeverAutoMatchesAndRejectsLateResult(t *testing.T) {
	c, e := db.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	st := store.New(c)
	lib, _ := st.CreateLibraryWithType("Test", t.TempDir(), "video")
	v, _ := st.UpsertVideo(domain.VideoAsset{LibraryID: lib.ID, Title: "File", FilePath: filepath.Join(lib.RootPath, "a.mkv"), MTime: time.Now()})
	s := &Service{store: st, configDir: t.TempDir()}
	p := &fakeVideoProvider{}
	w := &videoMetadataWorker{service: s, provider: p}
	j, _ := st.QueueVideoMetadata(v.ID, "Synthetic", "movie")
	w.processOne(context.Background())
	got, _ := st.VideoMetadataJob(j.ID)
	m, _ := st.VideoMetadata(v.ID)
	if got.Status != "ready" || m.WorkID != 0 || len(got.Candidates) != 1 {
		t.Fatalf("automatic match or missing candidates: %+v %+v", got, m)
	}
	if e = s.SelectVideoCandidate(v.ID, j.ID, 0, domain.VideoMatchInput{Generation: m.Generation}); e != nil {
		t.Fatal(e)
	}
	j, _ = st.QueueVideoMetadata(v.ID, "Next", "movie")
	p.action = func() {
		if e := st.CancelVideoMetadataJob(j.ID); e != nil {
			t.Error(e)
		}
	}
	w.processOne(context.Background())
	got, _ = st.VideoMetadataJob(j.ID)
	if got.Status != "cancelled" {
		t.Fatal("late cancellation overwritten", got)
	}
	j, _ = st.QueueVideoMetadata(v.ID, "Next", "movie")
	p.action = func() { st.SetVideoMetadataOverride(v.ID, "title", `"Manual"`) }
	w.processOne(context.Background())
	got, _ = st.VideoMetadataJob(j.ID)
	if got.Status != "stale" {
		t.Fatal("late result overwrote manual input", got)
	}
}

func TestVideoWorkerShutdown(t *testing.T) {
	c, e := db.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	s := &Service{store: store.New(c)}
	s.startVideoMetadataWorker(&fakeVideoProvider{})
	done := make(chan struct{})
	go func() { s.CloseVideoMetadataWorker(); s.CloseVideoMetadataWorker(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
}
