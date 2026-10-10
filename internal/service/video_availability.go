package service

import (
	"context"
	"encoding/json"
	"fmt"
	"foliospace-reader/internal/domain"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type videoSeasonProvider interface {
	Season(context.Context, string, int) ([]domain.VideoExpectedEpisode, error)
}

// Reference: https://developer.themoviedb.org/reference/tv-season-details
func (p *tmdbProvider) Season(ctx context.Context, id string, season int) ([]domain.VideoExpectedEpisode, error) {
	n, err := strconv.ParseInt(id, 10, 32)
	if err != nil || n <= 0 || season < 0 || season > 999 {
		return nil, fmt.Errorf("invalid series/season")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/tv/%d/season/%d?language=%s", p.base, n, season, url.QueryEscape(p.language)), nil)
	if err != nil {
		return nil, fmt.Errorf("provider-request-invalid")
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("provider-unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, &videoProviderError{Code: fmt.Sprintf("provider-http-%d", resp.StatusCode), RetryAfter: videoRetryAfter(resp)}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(b) > 2<<20 {
		return nil, fmt.Errorf("provider-response-too-large")
	}
	var data struct {
		Season   int `json:"season_number"`
		Episodes []struct {
			Number  int    `json:"episode_number"`
			Season  int    `json:"season_number"`
			Title   string `json:"name"`
			AirDate string `json:"air_date"`
		} `json:"episodes"`
	}
	if json.Unmarshal(b, &data) != nil || data.Season != season || data.Episodes == nil || len(data.Episodes) > 2000 {
		return nil, fmt.Errorf("provider-response-invalid")
	}
	out := []domain.VideoExpectedEpisode{}
	for _, e := range data.Episodes {
		if e.Season != season {
			return nil, fmt.Errorf("provider-response-invalid")
		}
		out = append(out, domain.VideoExpectedEpisode{Season: season, Number: e.Number, Title: e.Title, AirDate: e.AirDate})
	}
	return out, nil
}
func (s *Service) VideoAvailability(work int64, season, limit, offset int) (domain.VideoAvailability, error) {
	if err := s.PurgeVideoProviderCache(false); err != nil {
		return domain.VideoAvailability{}, err
	}
	return s.store.VideoAvailability(work, season, s.videoWorker != nil, limit, offset)
}
func (s *Service) RefreshVideoAvailability(ctx context.Context, work int64, season int) error {
	if s.videoWorker == nil {
		return fmt.Errorf("provider-disabled")
	}
	provider, ok := s.videoWorker.provider.(videoSeasonProvider)
	if !ok {
		return fmt.Errorf("provider-disabled")
	}
	if !s.videoAvailabilityMu.TryLock() {
		return fmt.Errorf("metadata-queue-full")
	}
	defer s.videoAvailabilityMu.Unlock()
	w, err := s.VideoWork(work)
	if err != nil {
		return err
	}
	if w.Kind != "series" || w.Provider != "tmdb" || w.ProviderID == "" {
		return fmt.Errorf("confirmed TMDB series required")
	}
	fresh, err := s.store.VideoExpectedFresh(work, season)
	if err != nil {
		return err
	}
	if fresh {
		return nil
	}
	if time.Now().Before(s.videoAvailabilityNext) {
		return fmt.Errorf("metadata-queue-full")
	}
	s.videoAvailabilityNext = time.Now().Add(15 * time.Second)
	items, err := provider.Season(ctx, w.ProviderID, season)
	if err != nil {
		if e, ok := err.(*videoProviderError); ok && e.RetryAfter > 15*time.Second {
			s.videoAvailabilityNext = time.Now().Add(e.RetryAfter)
		}
		return err
	}
	return s.store.SaveVideoExpected(work, season, w.ProviderID, items)
}
