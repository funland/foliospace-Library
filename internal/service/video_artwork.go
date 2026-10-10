package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"image"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const maxVideoArtworkBytes = 10 << 20

var tmdbImagePath = regexp.MustCompile(`^/[A-Za-z0-9_-]+\.(jpg|png)$`)

func validArtworkRole(role string) bool { return role == "poster" || role == "backdrop" }
func validateVideoArtwork(b []byte) (string, error) {
	if len(b) > maxVideoArtworkBytes {
		return "", fmt.Errorf("artwork-too-large")
	}
	c, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || c.Width < 1 || c.Height < 1 || int64(c.Width)*int64(c.Height) > 20_000_000 {
		return "", fmt.Errorf("artwork-invalid")
	}
	switch format {
	case "jpeg":
		return "image/jpeg", nil
	case "png":
		return "image/png", nil
	case "webp":
		return "image/webp", nil
	}
	return "", fmt.Errorf("artwork-format-unsupported")
}
func (s *Service) saveVideoArtwork(id int64, role string, b []byte, source string, generation int64) error {
	s.videoCacheMu.Lock()
	defer s.videoCacheMu.Unlock()
	if !validArtworkRole(role) {
		return fmt.Errorf("invalid artwork role")
	}
	if _, err := validateVideoArtwork(b); err != nil {
		return err
	}
	if s.configDir == "" {
		return fmt.Errorf("artwork-cache-unconfigured")
	}
	sum := sha256.Sum256(b)
	revision := hex.EncodeToString(sum[:])
	dir := filepath.Join(s.configDir, "cache", "video-artwork")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path := filepath.Join(dir, revision)
	f, err := os.CreateTemp(dir, ".artwork-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	return s.store.SaveVideoArtwork(id, role, path, revision, source, generation)
}
func (s *Service) UploadVideoArtwork(id int64, role string, b []byte, generation int64) error {
	return s.saveVideoArtwork(id, role, b, "manual", generation)
}
func (s *Service) ResetVideoArtwork(id int64, role string) error {
	if !validArtworkRole(role) {
		return fmt.Errorf("invalid role")
	}
	return s.store.DeleteVideoArtwork(id, role)
}
func readVideoSidecar(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxVideoArtworkBytes {
		return nil, fmt.Errorf("unsafe artwork")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("artwork changed")
	}
	b, err := io.ReadAll(io.LimitReader(f, maxVideoArtworkBytes+1))
	if err != nil {
		return nil, err
	}
	_, err = validateVideoArtwork(b)
	return b, err
}
func (s *Service) VideoArtwork(id int64, role string) ([]byte, string, string, error) {
	if err := s.PurgeVideoProviderCache(false); err != nil {
		return nil, "", "", err
	}
	if !validArtworkRole(role) {
		return nil, "", "", fmt.Errorf("invalid role")
	}
	v, err := s.store.VideoByID(id)
	if err != nil {
		return nil, "", "", err
	}
	path, revision, _, err := s.store.VideoArtwork(id, role)
	if err == nil {
		if b, e := readVideoSidecar(path); e == nil {
			mime, _ := validateVideoArtwork(b)
			return b, mime, revision, nil
		}
	} else if err != sql.ErrNoRows {
		return nil, "", "", err
	}
	stem := strings.TrimSuffix(v.FilePath, filepath.Ext(v.FilePath))
	dir := filepath.Dir(v.FilePath)
	names := []string{stem + "-" + role, stem}
	if role == "backdrop" {
		names = []string{stem + "-backdrop", filepath.Join(dir, "fanart")}
	} else {
		names = append(names, filepath.Join(dir, "poster"), filepath.Join(dir, "folder"))
	}
	for _, name := range names {
		for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp"} {
			b, e := readVideoSidecar(name + ext)
			if e != nil {
				continue
			}
			mime, _ := validateVideoArtwork(b)
			sum := sha256.Sum256(b)
			return b, mime, hex.EncodeToString(sum[:]), nil
		}
	}
	// Poster browsing never starts ffmpeg.
	b := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="300" height="450"><rect width="300" height="450" fill="#26334b"/><path d="M125 190v70l65-35z" fill="#91a9d0"/><text x="150" y="315" fill="white" text-anchor="middle" font-family="sans-serif">FolioSpace</text></svg>`)
	return b, "image/svg+xml", "placeholder-v1", nil
}
func (s *Service) downloadVideoArtwork(ctx context.Context, id int64, role, path string, generation int64) error {
	if !tmdbImagePath.MatchString(path) {
		return &videoProviderError{Code: "artwork-path-invalid"}
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://image.tmdb.org/t/p/w780"+path, nil)
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return &videoProviderError{Code: "artwork-unavailable", RetryAfter: 30 * time.Second}
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return &videoProviderError{Code: fmt.Sprintf("artwork-http-%d", resp.StatusCode), RetryAfter: videoRetryAfter(resp)}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxVideoArtworkBytes+1))
	if err != nil {
		return &videoProviderError{Code: "artwork-read-failed"}
	}
	return s.saveVideoArtwork(id, role, b, "tmdb", generation)
}
