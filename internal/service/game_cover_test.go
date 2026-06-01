package service

import (
	"strings"
	"testing"

	"foliospace-reader/internal/domain"
)

func TestLibretroBoxartCandidatesUsePlatformAndRegion(t *testing.T) {
	urls := libretroBoxartCandidates(domain.GameAsset{
		Title:    "Super Mario World",
		Platform: "snes",
		Region:   "USA",
	})
	if len(urls) != 2 {
		t.Fatalf("urls len = %d, want 2", len(urls))
	}
	if !strings.Contains(urls[0], "Nintendo%20-%20Super%20Nintendo%20Entertainment%20System/Named_Boxarts/Super%20Mario%20World.png") {
		t.Fatalf("first url = %q, want plain title candidate", urls[0])
	}
	if !strings.Contains(urls[1], "Super%20Mario%20World%20%28USA%29.png") {
		t.Fatalf("second url = %q, want region candidate", urls[1])
	}
}

func TestLibretroBoxartCandidatesSkipUnsupportedPlatform(t *testing.T) {
	urls := libretroBoxartCandidates(domain.GameAsset{
		Title:    "mslug",
		Platform: "arcade",
	})
	if len(urls) != 0 {
		t.Fatalf("urls = %#v, want no arcade libretro boxart candidate", urls)
	}
}
