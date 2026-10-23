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
	"strings"
	"time"
)

type videoDetailsProvider interface {
	Details(context.Context, string, string) (domain.VideoWorkDetails, error)
}

// https://developer.themoviedb.org/reference/movie-details
// https://developer.themoviedb.org/reference/tv-series-details
func (p *tmdbProvider) Details(ctx context.Context, id, kind string) (domain.VideoWorkDetails, error) {
	out := domain.VideoWorkDetails{}
	n, err := strconv.ParseInt(id, 10, 32)
	if err != nil || n <= 0 || (kind != "movie" && kind != "series") {
		return out, fmt.Errorf("provider-request-invalid")
	}
	route := "movie"
	if kind == "series" {
		route = "tv"
	}
	values := url.Values{"language": {p.language}, "append_to_response": {"credits"}}
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/%s/%d?%s", p.base, route, n, values.Encode()), nil)
	if err != nil {
		return out, fmt.Errorf("provider-request-invalid")
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	resp, err := p.client.Do(req)
	if err != nil {
		return out, fmt.Errorf("provider-unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return out, &videoProviderError{Code: fmt.Sprintf("provider-http-%d", resp.StatusCode), RetryAfter: videoRetryAfter(resp)}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(b) > 2<<20 {
		return out, fmt.Errorf("provider-response-too-large")
	}
	var data struct {
		ID             int64   `json:"id"`
		OriginalTitle  string  `json:"original_title"`
		OriginalName   string  `json:"original_name"`
		Runtime        int     `json:"runtime"`
		EpisodeRuntime []int   `json:"episode_run_time"`
		Rating         float64 `json:"vote_average"`
		Votes          int     `json:"vote_count"`
		Genres         []struct {
			Name string `json:"name"`
		} `json:"genres"`
		Creators []struct {
			Name string `json:"name"`
		} `json:"created_by"`
		Credits struct {
			Cast []struct {
				Name string `json:"name"`
			} `json:"cast"`
			Crew []struct {
				Name string `json:"name"`
				Job  string `json:"job"`
			} `json:"crew"`
		} `json:"credits"`
	}
	if json.Unmarshal(b, &data) != nil || data.ID != n {
		return out, fmt.Errorf("provider-response-invalid")
	}
	out.OriginalTitle = strings.TrimSpace(data.OriginalTitle)
	out.RuntimeMinutes = data.Runtime
	if kind == "series" {
		out.OriginalTitle = strings.TrimSpace(data.OriginalName)
		if len(data.EpisodeRuntime) > 0 {
			out.RuntimeMinutes = data.EpisodeRuntime[0]
		}
	}
	if out.RuntimeMinutes < 0 || out.RuntimeMinutes > 10000 {
		out.RuntimeMinutes = 0
	}
	if data.Votes > 0 && data.Rating >= 0 && data.Rating <= 10 {
		out.Rating = data.Rating
		out.VoteCount = data.Votes
	}
	add := func(items *[]string, name string, limit int) {
		name = strings.TrimSpace(name)
		if name == "" || len(name) > 500 || len(*items) >= limit {
			return
		}
		for _, old := range *items {
			if old == name {
				return
			}
		}
		*items = append(*items, name)
	}
	for _, g := range data.Genres {
		add(&out.Genres, g.Name, 20)
	}
	for _, c := range data.Credits.Crew {
		if c.Job == "Director" {
			add(&out.Directors, c.Name, 12)
		}
	}
	if kind == "series" {
		for _, c := range data.Creators {
			add(&out.Directors, c.Name, 12)
		}
	}
	for _, c := range data.Credits.Cast {
		add(&out.Cast, c.Name, 12)
	}
	out.FetchedAt = time.Now().Unix()
	return out, nil
}

func (s *Service) VideoWorkDetails(id int64) (domain.VideoWorkDetails, error) {
	return s.store.VideoWorkDetails(id)
}
func (s *Service) RefreshVideoWorkDetails(ctx context.Context, id int64) (domain.VideoWorkDetails, error) {
	empty := domain.VideoWorkDetails{}
	if s.videoWorker == nil {
		return empty, fmt.Errorf("provider-disabled")
	}
	provider, ok := s.videoWorker.provider.(videoDetailsProvider)
	if !ok {
		return empty, fmt.Errorf("provider-disabled")
	}
	// Share the bounded provider lane and backoff with availability requests.
	if !s.videoAvailabilityMu.TryLock() {
		return empty, fmt.Errorf("metadata-queue-full")
	}
	defer s.videoAvailabilityMu.Unlock()
	w, err := s.VideoWork(id)
	if err != nil {
		return empty, err
	}
	if w.Provider != "tmdb" || w.ProviderID == "" {
		return empty, fmt.Errorf("confirmed TMDB work required")
	}
	cached, err := s.store.VideoWorkDetails(id)
	if err != nil {
		return empty, err
	}
	if cached.FetchedAt > time.Now().Add(-24*time.Hour).Unix() {
		return cached, nil
	}
	if time.Now().Before(s.videoAvailabilityNext) {
		return empty, fmt.Errorf("metadata-queue-full")
	}
	s.videoAvailabilityNext = time.Now().Add(15 * time.Second)
	detail, err := provider.Details(ctx, w.ProviderID, w.Kind)
	if err != nil {
		if e, ok := err.(*videoProviderError); ok && e.RetryAfter > 15*time.Second {
			s.videoAvailabilityNext = time.Now().Add(e.RetryAfter)
		}
		return empty, err
	}
	if err = s.store.SaveVideoWorkDetails(w, detail); err != nil {
		return empty, err
	}
	return detail, nil
}
