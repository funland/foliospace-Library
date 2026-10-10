package service

import (
	"context"
	"errors"
	"fmt"
	"foliospace-reader/internal/domain"
	"sync"
	"time"
)

type videoMetadataWorker struct {
	service   *Service
	provider  videoMetadataProvider
	cancel    context.CancelFunc
	done      chan struct{}
	once      sync.Once
	notBefore time.Time
}

func (s *Service) startVideoMetadataWorker(provider videoMetadataProvider) {
	if provider == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &videoMetadataWorker{service: s, provider: provider, cancel: cancel, done: make(chan struct{})}
	s.videoWorker = w
	_ = s.store.RecoverVideoMetadataJobs()
	go func() {
		defer close(w.done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			if ctx.Err() != nil {
				return
			}
			if time.Now().Before(w.notBefore) {
				continue
			}
			w.processOne(ctx)
		}
	}()
}
func (s *Service) CloseVideoMetadataWorker() {
	if w := s.videoWorker; w != nil {
		w.once.Do(w.cancel)
		<-w.done
	}
}
func (w *videoMetadataWorker) processOne(ctx context.Context) {
	if w.service.PurgeVideoProviderCache(false) != nil {
		return
	}
	j, ok, err := w.service.store.ClaimVideoMetadataJob()
	if err != nil || !ok {
		return
	}
	m, e := w.service.store.VideoMetadata(j.VideoID)
	if e != nil || m.Generation != j.Generation {
		_ = w.service.store.FinishVideoMetadataJob(j, nil, "input-changed", 0)
		return
	}
	call, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var candidates []domain.VideoCandidate
	if j.Kind == "poster" || j.Kind == "backdrop" {
		err = w.service.downloadVideoArtwork(call, j.VideoID, j.Kind, j.Query, j.Generation)
	} else {
		candidates, err = w.provider.Search(call, j.Query, j.Kind)
	}
	code := ""
	retryAt := int64(0)
	if err != nil {
		code = "provider-failed"
		var p *videoProviderError
		if errors.As(err, &p) {
			code = p.Code
			if p.RetryAfter > 0 {
				retryAt = time.Now().Add(p.RetryAfter * time.Duration(1<<uint(j.Attempts-1))).Unix()
			}
		}
	}
	if retryAt > 0 {
		w.notBefore = time.Unix(retryAt, 0)
	}
	if err == nil && (j.Kind == "poster" || j.Kind == "backdrop") {
		j.Generation++
	}
	_ = w.service.store.FinishVideoMetadataJob(j, candidates, code, retryAt)
}
func (s *Service) VideoProviderStatus() map[string]any {
	return map[string]any{"provider": "tmdb", "enabled": s.videoWorker != nil, "attribution": "This application uses TMDB and the TMDB APIs but is not endorsed, certified, or otherwise approved by TMDB.", "website": "https://www.themoviedb.org"}
}
func (s *Service) QueueVideoMetadata(id int64, q, kind string) (domain.VideoMetadataJob, error) {
	if s.videoWorker == nil {
		return domain.VideoMetadataJob{}, &videoProviderError{Code: "provider-disabled"}
	}
	return s.store.QueueVideoMetadata(id, q, kind)
}
func (s *Service) VideoMetadataJob(id int64) (domain.VideoMetadataJob, error) {
	if err := s.PurgeVideoProviderCache(false); err != nil {
		return domain.VideoMetadataJob{}, err
	}
	return s.store.VideoMetadataJob(id)
}
func (s *Service) LatestVideoMetadataJob(id int64) (domain.VideoMetadataJob, error) {
	if err := s.PurgeVideoProviderCache(false); err != nil {
		return domain.VideoMetadataJob{}, err
	}
	return s.store.LatestVideoMetadataJob(id)
}
func (s *Service) CancelVideoMetadataJob(id int64) error { return s.store.CancelVideoMetadataJob(id) }
func (s *Service) SelectVideoCandidate(id, jobID int64, index int, in domain.VideoMatchInput) error {
	j, err := s.VideoMetadataJob(jobID)
	if err != nil {
		return err
	}
	if j.VideoID != id || j.Status != "ready" || index < 0 || index >= len(j.Candidates) {
		return fmt.Errorf("invalid candidate")
	}
	if in.Generation != j.Generation {
		return fmt.Errorf("video-metadata-revision-conflict")
	}
	c := j.Candidates[index]
	in.WorkID = 0
	in.Title = c.Title
	in.Kind = c.Kind
	in.Year = c.Year
	in.Overview = c.Overview
	in.Provider = "tmdb"
	in.ProviderID = c.ID
	return s.store.MatchVideo(id, in)
}
func (s *Service) QueueCandidateArtwork(id, jobID int64, index int, role string) (domain.VideoMetadataJob, error) {
	j, err := s.VideoMetadataJob(jobID)
	if err != nil {
		return domain.VideoMetadataJob{}, err
	}
	if j.VideoID != id || j.Status != "ready" || index < 0 || index >= len(j.Candidates) || !validArtworkRole(role) {
		return domain.VideoMetadataJob{}, fmt.Errorf("invalid candidate artwork")
	}
	path := j.Candidates[index].PosterPath
	if role == "backdrop" {
		path = j.Candidates[index].BackdropPath
	}
	if !tmdbImagePath.MatchString(path) {
		return domain.VideoMetadataJob{}, fmt.Errorf("artwork unavailable")
	}
	return s.QueueVideoMetadata(id, path, role)
}
