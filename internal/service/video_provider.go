package service

import (
	"context"
	"encoding/json"
	"fmt"
	"foliospace-reader/internal/domain"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type videoMetadataProvider interface {
	Search(context.Context, string, string) ([]domain.VideoCandidate, error)
}
type videoProviderError struct {
	Code       string
	RetryAfter time.Duration
}

func (e *videoProviderError) Error() string { return e.Code }

type tmdbProvider struct {
	token, language, base string
	client                *http.Client
}

func newTMDBProvider() videoMetadataProvider {
	if os.Getenv("FOLIOSPACE_VIDEO_METADATA_PROVIDER") != "tmdb" || strings.TrimSpace(os.Getenv("FOLIOSPACE_TMDB_TOKEN")) == "" {
		return nil
	}
	language := os.Getenv("FOLIOSPACE_TMDB_LANGUAGE")
	if language == "" {
		language = "en-US"
	}
	return &tmdbProvider{token: os.Getenv("FOLIOSPACE_TMDB_TOKEN"), language: language, base: "https://api.themoviedb.org/3", client: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (p *tmdbProvider) Search(ctx context.Context, q, kind string) ([]domain.VideoCandidate, error) {
	route := "movie"
	if kind == "series" {
		route = "tv"
	}
	values := url.Values{"query": {q}, "language": {p.language}, "include_adult": {"false"}, "page": {"1"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.base+"/search/"+route+"?"+values.Encode(), nil)
	if err != nil {
		return nil, &videoProviderError{Code: "provider-request-invalid"}
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, &videoProviderError{Code: "provider-unavailable", RetryAfter: 30 * time.Second}
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		code := fmt.Sprintf("provider-http-%d", resp.StatusCode)
		return nil, &videoProviderError{Code: code, RetryAfter: videoRetryAfter(resp)}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(b) > 2<<20 {
		return nil, &videoProviderError{Code: "provider-response-too-large"}
	}
	var result struct {
		Results []struct {
			ID       int64  `json:"id"`
			Title    string `json:"title"`
			Name     string `json:"name"`
			Date     string `json:"release_date"`
			AirDate  string `json:"first_air_date"`
			Overview string `json:"overview"`
			Poster   string `json:"poster_path"`
			Backdrop string `json:"backdrop_path"`
		} `json:"results"`
	}
	if json.Unmarshal(b, &result) != nil {
		return nil, &videoProviderError{Code: "provider-response-invalid"}
	}
	out := []domain.VideoCandidate{}
	for _, v := range result.Results {
		if v.ID <= 0 {
			continue
		}
		title, date := v.Title, v.Date
		if kind == "series" {
			title, date = v.Name, v.AirDate
		}
		year := 0
		if len(date) >= 4 {
			year, _ = strconv.Atoi(date[:4])
		}
		out = append(out, domain.VideoCandidate{ID: strconv.FormatInt(v.ID, 10), Title: title, Kind: kind, Year: year, Overview: v.Overview, PosterPath: v.Poster, BackdropPath: v.Backdrop})
		if len(out) >= 20 {
			break
		}
	}
	return out, nil
}

func videoRetryAfter(resp *http.Response) time.Duration {
	if resp.StatusCode != 429 && resp.StatusCode < 500 {
		return 0
	}
	retry := 30 * time.Second
	if seconds, e := strconv.ParseInt(resp.Header.Get("Retry-After"), 10, 64); e == nil && seconds > 0 {
		if seconds > 86400 {
			seconds = 86400
		}
		retry = time.Duration(seconds) * time.Second
	} else if d, e := http.ParseTime(resp.Header.Get("Retry-After")); e == nil && time.Until(d) > 0 {
		retry = time.Until(d)
	}
	if retry > 24*time.Hour {
		retry = 24 * time.Hour
	}
	return retry
}
