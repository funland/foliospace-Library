package service

import (
	"os"
	"path/filepath"
	"regexp"
	"time"
)

var artworkCacheName = regexp.MustCompile(`^[a-f0-9]{64}$`)

// Sweep only the managed content-addressed cache, never media sidecars.
func (s *Service) PurgeVideoProviderCache(all bool) error {
	s.videoCacheMu.Lock()
	defer s.videoCacheMu.Unlock()
	if !all && time.Since(s.videoCachePurgedAt) < time.Hour {
		return nil
	}
	paths, err := s.store.PurgeVideoProviderCache(all)
	if err != nil {
		return err
	}
	if s.configDir != "" {
		dir := filepath.Join(s.configDir, "cache", "video-artwork")
		entries, e := os.ReadDir(dir)
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		for _, entry := range entries {
			if !artworkCacheName.MatchString(entry.Name()) || !entry.Type().IsRegular() {
				continue
			}
			p := filepath.Join(dir, entry.Name())
			selected := all
			for _, expired := range paths {
				if p == expired {
					selected = true
				}
			}
			if info, e := entry.Info(); e == nil && time.Since(info.ModTime()) > 24*time.Hour {
				selected = true
			}
			if !selected {
				continue
			}
			used, e := s.store.VideoArtworkReferenced(p)
			if e != nil {
				return e
			}
			if !used {
				if e = os.Remove(p); e != nil && !os.IsNotExist(e) {
					return e
				}
			}
		}
	}
	s.videoCachePurgedAt = time.Now()
	return nil
}
