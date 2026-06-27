package service

import (
	"archive/zip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/store"
)

func TestLibretroBoxartCandidatesUsePlatformAndRegion(t *testing.T) {
	withLibretroListingFetcher(t, func(_ string, _ string) ([]string, error) {
		return nil, errors.New("offline")
	})
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
	withLibretroListingFetcher(t, func(_ string, _ string) ([]string, error) {
		return nil, errors.New("offline")
	})
	urls := libretroBoxartCandidates(domain.GameAsset{
		Title:    "mslug",
		Platform: "arcade",
	})
	if len(urls) != 0 {
		t.Fatalf("urls = %#v, want no arcade libretro boxart candidate", urls)
	}
}

func TestLibretroBoxartCandidatesUseFBNeoArcadePlaylist(t *testing.T) {
	withLibretroListingFetcher(t, func(_ string, _ string) ([]string, error) {
		return nil, errors.New("offline")
	})
	urls := libretroBoxartCandidates(domain.GameAsset{
		Title:      "Blandia",
		Platform:   "arcade",
		ROMSetName: "FBNeo",
	})
	if len(urls) != 4 {
		t.Fatalf("urls len = %d, want 4", len(urls))
	}
	if !strings.Contains(urls[0], "FBNeo%20-%20Arcade%20Games/Named_Boxarts/Blandia.png") {
		t.Fatalf("first url = %q, want FBNeo arcade playlist candidate", urls[0])
	}

	urls = libretroBoxartCandidates(domain.GameAsset{
		Title:      "Metal Slug",
		Platform:   "neogeo",
		ROMSetName: "FBNeo",
		RelPath:    "FBNeo/arcade/mslug.zip",
	})
	if len(urls) != 4 {
		t.Fatalf("neogeo urls len = %d, want 4", len(urls))
	}
	if !strings.Contains(urls[0], "FBNeo%20-%20Arcade%20Games/Named_Boxarts/Metal%20Slug.png") {
		t.Fatalf("neogeo first url = %q, want FBNeo arcade playlist candidate", urls[0])
	}
}

func TestLibretroArtworkCandidatesPreferListingExactMatch(t *testing.T) {
	urls := libretroBoxartCandidatesFromListing(domain.GameAsset{
		Title:    "Super Mario World",
		Platform: "snes",
		Region:   "USA",
	}, []string{
		"Super Mario Kart.png",
		"Super Mario World (USA).png",
		"Super Mario World.png",
	})
	if len(urls) != 1 {
		t.Fatalf("urls len = %d, want 1", len(urls))
	}
	if !strings.Contains(urls[0], "Named_Boxarts/Super%20Mario%20World%20%28USA%29.png") {
		t.Fatalf("url = %q, want exact region listing match", urls[0])
	}
}

func TestLibretroArtworkCandidatesUseFuzzyTagStrippedMatch(t *testing.T) {
	urls := libretroBoxartCandidatesFromListing(domain.GameAsset{
		Title:    "Legend of Zelda The Minish Cap",
		Platform: "gba",
	}, []string{
		"Legend of Zelda, The - The Minish Cap (USA).png",
		"Zelda II - The Adventure of Link (USA).png",
	})
	if len(urls) != 1 {
		t.Fatalf("urls len = %d, want 1", len(urls))
	}
	if !strings.Contains(urls[0], "Legend%20of%20Zelda%2C%20The%20-%20The%20Minish%20Cap%20%28USA%29.png") {
		t.Fatalf("url = %q, want fuzzy tag-stripped listing match", urls[0])
	}
}

func TestLibretroArtworkCandidatesUseInnerZipROMName(t *testing.T) {
	root := t.TempDir()
	romPath := filepath.Join(root, "2020超级棒球 19930312.zip")
	makeGameZip(t, romPath, "2020 Super Baseball (J)(1993)(KAC).sfc")

	urls := libretroBoxartCandidatesFromListing(domain.GameAsset{
		Title:    "2020超级棒球 19930312",
		Platform: "snes",
		Format:   "zip",
		FilePath: romPath,
	}, []string{
		"2020 Super Baseball (Japan).png",
		"Super Baseball Simulator 1.000 (USA).png",
	})
	if len(urls) != 1 {
		t.Fatalf("urls len = %d, want 1", len(urls))
	}
	if !strings.Contains(urls[0], "2020%20Super%20Baseball%20%28Japan%29.png") {
		t.Fatalf("url = %q, want inner ROM name matched to Japan boxart", urls[0])
	}
}

func TestParseLibretroListingFilenames(t *testing.T) {
	html := `<html><body>
<a href="../">../</a>
<a href="Super%20Mario%20World%20%28USA%29.png">Super Mario World (USA).png</a>
<a href="Legend%20of%20Zelda%2C%20The%20-%20The%20Minish%20Cap%20%28USA%29.png">Legend</a>
<a href="Named_Snaps/">Named_Snaps/</a>
</body></html>`
	got := parseLibretroListingFilenames(html)
	want := []string{
		"Super Mario World (USA).png",
		"Legend of Zelda, The - The Minish Cap (USA).png",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filenames = %#v, want %#v", got, want)
	}
}

func makeGameZip(t *testing.T, path string, entryName string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	entry, err := writer.Create(entryName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("rom")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLibretroArtworkCandidatesCanUseSnapAndTitleFolders(t *testing.T) {
	urls := libretroArtworkCandidatesFromListing(domain.GameAsset{
		Title:    "Advance Wars",
		Platform: "gba",
		Region:   "USA",
	}, "Named_Snaps", []string{
		"Advance Wars (USA).png",
	})
	if len(urls) != 1 || !strings.Contains(urls[0], "Named_Snaps/Advance%20Wars%20%28USA%29.png") {
		t.Fatalf("snap urls = %#v, want Named_Snaps match", urls)
	}

	urls = libretroArtworkCandidatesFromListing(domain.GameAsset{
		Title:    "Advance Wars",
		Platform: "gba",
		Region:   "USA",
	}, "Named_Titles", []string{
		"Advance Wars (USA).png",
	})
	if len(urls) != 1 || !strings.Contains(urls[0], "Named_Titles/Advance%20Wars%20%28USA%29.png") {
		t.Fatalf("title urls = %#v, want Named_Titles match", urls)
	}
}

func TestOpenGameCoverPersistsLibretroArtworkSource(t *testing.T) {
	withLibretroListingFetcher(t, func(_ string, _ string) ([]string, error) {
		return []string{"Super Mario World (USA).png"}, nil
	})
	previousDownloader := gameCoverDownloader
	gameCoverDownloader = func(sourceURL string, cachePath string) error {
		if !strings.Contains(sourceURL, "Super%20Mario%20World%20%28USA%29.png") {
			t.Fatalf("sourceURL = %q, want listing-matched region cover", sourceURL)
		}
		if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(cachePath, []byte("cover"), 0o644)
	}
	t.Cleanup(func() {
		gameCoverDownloader = previousDownloader
	})

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibrary("Games", "/library")
	if err != nil {
		t.Fatal(err)
	}
	game, err := st.UpsertGame(domain.GameAsset{
		LibraryID:     lib.ID,
		Title:         "Super Mario World",
		Platform:      "snes",
		Region:        "USA",
		Format:        "sfc",
		FilePath:      "/library/SNES/Super Mario World.sfc",
		RelPath:       "SNES/Super Mario World.sfc",
		Size:          1024,
		MTime:         time.Unix(20, 0),
		CRC32:         "b19ed489",
		SHA1:          "0123456789abcdef0123456789abcdef01234567",
		EmulatorHint:  "snes",
		Compatibility: "unknown",
	})
	if err != nil {
		t.Fatal(err)
	}

	stream, err := NewWithConfig(st, t.TempDir()).OpenGameCover(game.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	data, err := io.ReadAll(stream.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "cover" {
		t.Fatalf("cover data = %q, want downloaded cover", string(data))
	}

	details, err := st.GameDetails(game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(details.Artwork) != 1 || details.Artwork[0].Source != "libretro" || details.Artwork[0].Kind != "cover" || !details.Artwork[0].Selected {
		t.Fatalf("artwork = %#v, want selected libretro cover source", details.Artwork)
	}
	if !strings.Contains(details.Artwork[0].URL, "Super%20Mario%20World%20%28USA%29.png") || details.Artwork[0].CachePath == "" {
		t.Fatalf("artwork = %#v, want source URL and cache path", details.Artwork[0])
	}
}

func TestOpenGameCoverPrefersSelectedArtworkCachePath(t *testing.T) {
	withLibretroListingFetcher(t, func(_ string, _ string) ([]string, error) {
		t.Fatal("selected artwork should be used before libretro lookup")
		return nil, nil
	})
	root := t.TempDir()
	coverPath := filepath.Join(root, "covers", "Advance Wars.png")
	if err := os.MkdirAll(filepath.Dir(coverPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(coverPath, tinyPNG(), 0o644); err != nil {
		t.Fatal(err)
	}

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibrary("Games", root)
	if err != nil {
		t.Fatal(err)
	}
	game, err := st.UpsertGame(domain.GameAsset{
		LibraryID:     lib.ID,
		Title:         "Advance Wars",
		Platform:      "gba",
		Region:        "USA",
		Format:        "gba",
		FilePath:      filepath.Join(root, "gba", "Advance Wars.gba"),
		RelPath:       "gba/Advance Wars.gba",
		Size:          1024,
		MTime:         time.Unix(21, 0),
		CRC32:         "aabbccdd",
		SHA1:          "abcdefabcdefabcdefabcdefabcdefabcdefabcd",
		EmulatorHint:  "gba",
		Compatibility: "unknown",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertGameArtwork(domain.GameArtwork{
		GameID:     game.ID,
		Source:     "gamelist",
		Kind:       "cover",
		CachePath:  coverPath,
		Selected:   true,
		Confidence: 1,
	}); err != nil {
		t.Fatal(err)
	}

	stream, err := NewWithConfig(st, t.TempDir()).OpenGameCover(game.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	if stream.ContentType != "image/png" {
		t.Fatalf("content type = %q, want image/png", stream.ContentType)
	}
	data, err := io.ReadAll(stream.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(tinyPNG()) {
		t.Fatalf("cover bytes len = %d, want selected cache path bytes", len(data))
	}
}

func tinyPNG() []byte {
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
		0xde, 0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41,
		0x54, 0x08, 0xd7, 0x63, 0xf8, 0xff, 0xff, 0x3f,
		0x00, 0x05, 0xfe, 0x02, 0xfe, 0xdc, 0xcc, 0x59,
		0xe7, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e,
		0x44, 0xae, 0x42, 0x60, 0x82,
	}
}

func withLibretroListingFetcher(t *testing.T, fetcher func(string, string) ([]string, error)) {
	t.Helper()
	previous := libretroListingFetcher
	libretroListingFetcher = fetcher
	libretroListingCache.Lock()
	libretroListingCache.items = map[string]libretroListingCacheEntry{}
	libretroListingCache.Unlock()
	t.Cleanup(func() {
		libretroListingFetcher = previous
		libretroListingCache.Lock()
		libretroListingCache.items = map[string]libretroListingCacheEntry{}
		libretroListingCache.Unlock()
	})
}
