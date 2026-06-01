package httpapi

import (
	"archive/zip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/service"
	"foliospace-reader/internal/store"
)

func TestAPIIndexesAndStreamsCBZPages(t *testing.T) {
	root := t.TempDir()
	makeZip(t, filepath.Join(root, "Series A", "book1.cbz"), map[string]string{"001.jpg": "image"})
	makeZip(t, filepath.Join(root, "Books", "sample.epub"), map[string]string{
		"META-INF/container.xml": `<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OPS/package.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`,
		"OPS/package.opf": `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>Sample EPUB</dc:title>
  </metadata>
  <manifest>
    <item id="chapter1" href="text/chapter1.xhtml" media-type="application/xhtml+xml"/>
    <item id="cover" href="images/cover.jpg" media-type="image/jpeg" properties="cover-image"/>
  </manifest>
  <spine>
    <itemref idref="chapter1"/>
  </spine>
</package>`,
		"OPS/text/chapter1.xhtml": `<html xmlns="http://www.w3.org/1999/xhtml"><body><h1>Chapter</h1></body></html>`,
		"OPS/images/cover.jpg":    "cover",
	})
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibrary("Test", root)
	if err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(New(service.New(st), nil).Routes())
	defer ts.Close()

	post(t, ts.URL+"/api/libraries/"+itoa(lib.ID)+"/scan", "")
	waitFor(t, func() bool {
		jobs, err := st.ListScanJobs()
		return err == nil && len(jobs) > 0 && jobs[0].Status == "completed"
	})
	body := get(t, ts.URL+"/api/series")
	if !strings.Contains(body, "Series A") {
		t.Fatalf("series response %q does not include Series A", body)
	}
	collectionsBody := get(t, ts.URL+"/api/collections")
	if !strings.Contains(collectionsBody, `"collectionType":"directory"`) || !strings.Contains(collectionsBody, `"directoryPath":"Series A"`) {
		t.Fatalf("collections response %q does not include directory collection fields", collectionsBody)
	}

	series, err := st.ListSeries()
	if err != nil {
		t.Fatal(err)
	}
	var cbzBookID int64
	var cbzSeriesID int64
	for _, seriesItem := range series {
		if seriesItem.Title != "Series A" {
			continue
		}
		cbzSeriesID = seriesItem.ID
		books, err := st.ListBooks(seriesItem.ID)
		if err != nil {
			t.Fatal(err)
		}
		cbzBookID = books[0].ID
	}
	if cbzBookID == 0 {
		t.Fatal("cbz book was not indexed")
	}
	volumesBody := get(t, ts.URL+"/api/collections/"+itoa(cbzSeriesID)+"/volumes")
	if !strings.Contains(volumesBody, `"bookType":"single_volume"`) {
		t.Fatalf("volumes response %q does not include single-volume book type", volumesBody)
	}
	pagedVolumesBody := get(t, ts.URL+"/api/collections/"+itoa(cbzSeriesID)+"/volumes?limit=1&offset=0&sort=title&q=book")
	if !strings.Contains(pagedVolumesBody, `"items"`) || !strings.Contains(pagedVolumesBody, `"total":1`) || !strings.Contains(pagedVolumesBody, `"hasMore":false`) {
		t.Fatalf("paged volumes response %q does not include paging metadata", pagedVolumesBody)
	}

	pages := get(t, ts.URL+"/api/books/"+itoa(cbzBookID)+"/pages")
	if !strings.Contains(pages, "001.jpg") {
		t.Fatalf("pages response %q does not include 001.jpg", pages)
	}
	putJSON(t, ts.URL+"/api/books/"+itoa(cbzBookID)+"/progress", `{"pageIndex":1,"progressFraction":0.5}`)
	continueBody := get(t, ts.URL+"/api/books/continue-reading")
	if !strings.Contains(continueBody, `"currentPage":1`) || !strings.Contains(continueBody, `"progressFraction":0.5`) {
		t.Fatalf("continue-reading response %q does not include saved progress", continueBody)
	}
	recentBody := get(t, ts.URL+"/api/books/recent")
	if !strings.Contains(recentBody, `"collectionTitle":"Series A"`) || !strings.Contains(recentBody, `"addedAt"`) {
		t.Fatalf("recent response %q does not include recent book metadata", recentBody)
	}

	resp, err := http.Get(ts.URL + "/api/books/" + itoa(cbzBookID) + "/pages/0")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "image" {
		t.Fatalf("page body = %q, want image", string(data))
	}

	var epubBookID int64
	for _, seriesItem := range series {
		if seriesItem.Title != "Books" {
			continue
		}
		epubBooks, err := st.ListBooks(seriesItem.ID)
		if err != nil {
			t.Fatal(err)
		}
		epubBookID = epubBooks[0].ID
	}
	if epubBookID == 0 {
		t.Fatal("epub book was not indexed")
	}
	manifest := get(t, ts.URL+"/api/books/"+itoa(epubBookID)+"/epub/manifest")
	if !strings.Contains(manifest, "OPS/text/chapter1.xhtml") {
		t.Fatalf("manifest response %q does not include epub chapter", manifest)
	}
	chapter := get(t, ts.URL+"/api/books/"+itoa(epubBookID)+"/epub/resources/OPS/text/chapter1.xhtml")
	if !strings.Contains(chapter, "Chapter") {
		t.Fatalf("chapter response %q does not include Chapter", chapter)
	}
}

func TestClientAPIHomeAndManifestsHideFilePaths(t *testing.T) {
	root := t.TempDir()
	makeZip(t, filepath.Join(root, "Series A", "book1.cbz"), map[string]string{"001.jpg": "image"})
	makeZip(t, filepath.Join(root, "Books", "sample.epub"), map[string]string{
		"META-INF/container.xml": `<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OPS/package.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`,
		"OPS/package.opf": `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>Sample EPUB</dc:title>
  </metadata>
  <manifest>
    <item id="chapter1" href="text/chapter1.xhtml" media-type="application/xhtml+xml"/>
    <item id="cover" href="images/cover.jpg" media-type="image/jpeg" properties="cover-image"/>
  </manifest>
  <spine>
    <itemref idref="chapter1"/>
  </spine>
</package>`,
		"OPS/text/chapter1.xhtml": `<html xmlns="http://www.w3.org/1999/xhtml"><body><h1>Chapter</h1></body></html>`,
		"OPS/images/cover.jpg":    "cover",
	})
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibrary("Test", root)
	if err != nil {
		t.Fatal(err)
	}
	romPath := filepath.Join(root, "SNES", "Super Mario World (USA).sfc")
	if err := os.MkdirAll(filepath.Dir(romPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(romPath, []byte("rom-body"), 0o644); err != nil {
		t.Fatal(err)
	}
	videoPath := filepath.Join(root, "Movies", "Demo Movie.mp4")
	if err := os.MkdirAll(filepath.Dir(videoPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(videoPath, []byte("video-body"), 0o644); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(New(service.New(st), nil).Routes())
	defer ts.Close()

	post(t, ts.URL+"/api/libraries/"+itoa(lib.ID)+"/scan", "")
	waitFor(t, func() bool {
		jobs, err := st.ListScanJobs()
		return err == nil && len(jobs) > 0 && jobs[0].Status == "completed"
	})

	var cbzBookID, epubBookID int64
	series, err := st.ListSeries()
	if err != nil {
		t.Fatal(err)
	}
	for _, seriesItem := range series {
		books, err := st.ListBooks(seriesItem.ID)
		if err != nil {
			t.Fatal(err)
		}
		switch seriesItem.Title {
		case "Series A":
			cbzBookID = books[0].ID
		case "Books":
			epubBookID = books[0].ID
		}
	}
	if cbzBookID == 0 || epubBookID == 0 {
		t.Fatalf("indexed book ids cbz=%d epub=%d", cbzBookID, epubBookID)
	}
	putJSON(t, ts.URL+"/api/books/"+itoa(cbzBookID)+"/progress", `{"pageIndex":1,"progressFraction":0.5}`)

	infoBody := get(t, ts.URL+"/api/client/info")
	if !strings.Contains(infoBody, `"apiVersion":"v1"`) ||
		!strings.Contains(infoBody, `"epub"`) ||
		!strings.Contains(infoBody, `"pdf"`) ||
		!strings.Contains(infoBody, `"mp4"`) ||
		!strings.Contains(infoBody, `"videoCatalog":true`) ||
		!strings.Contains(infoBody, `"pdfPageLayout":true`) ||
		!strings.Contains(infoBody, `"scanSettings":true`) {
		t.Fatalf("client info response %q does not include v1 capabilities", infoBody)
	}

	homeBody := get(t, ts.URL+"/api/client/home")
	if strings.Contains(homeBody, root) || strings.Contains(homeBody, "filePath") {
		t.Fatalf("client home leaked file path: %q", homeBody)
	}
	if !strings.Contains(homeBody, `"continueReading"`) || !strings.Contains(homeBody, `"recentBooks"`) || !strings.Contains(homeBody, `"collections"`) {
		t.Fatalf("client home response %q is missing expected sections", homeBody)
	}
	if !strings.Contains(homeBody, `"gameShelf"`) || !strings.Contains(homeBody, `"Super Mario World"`) || strings.Contains(homeBody, "Super Mario World (USA).sfc") {
		t.Fatalf("client home response %q is missing safe game shelf", homeBody)
	}
	if !strings.Contains(homeBody, `"videoShelf"`) || !strings.Contains(homeBody, `"Demo Movie"`) || strings.Contains(homeBody, "Movies/Demo Movie.mp4") {
		t.Fatalf("client home response %q is missing safe video shelf", homeBody)
	}
	if !strings.Contains(homeBody, `"/api/books/`+itoa(cbzBookID)+`/cover"`) {
		t.Fatalf("client home response %q does not include cover URL", homeBody)
	}

	cbzManifestBody := get(t, ts.URL+"/api/client/books/"+itoa(cbzBookID)+"/manifest")
	if strings.Contains(cbzManifestBody, root) || strings.Contains(cbzManifestBody, "filePath") {
		t.Fatalf("cbz client manifest leaked file path: %q", cbzManifestBody)
	}
	if !strings.Contains(cbzManifestBody, `"format":"cbz"`) || !strings.Contains(cbzManifestBody, `"/api/books/`+itoa(cbzBookID)+`/pages/0"`) {
		t.Fatalf("cbz client manifest response %q is missing page URLs", cbzManifestBody)
	}

	epubManifestBody := get(t, ts.URL+"/api/client/books/"+itoa(epubBookID)+"/manifest")
	if strings.Contains(epubManifestBody, root) || strings.Contains(epubManifestBody, "filePath") {
		t.Fatalf("epub client manifest leaked file path: %q", epubManifestBody)
	}
	if !strings.Contains(epubManifestBody, `"format":"epub"`) || !strings.Contains(epubManifestBody, `"resourceBaseUrl":"/api/books/`+itoa(epubBookID)+`/epub/resources/"`) {
		t.Fatalf("epub client manifest response %q is missing epub open data", epubManifestBody)
	}

	games, err := st.ListRecentGames(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 {
		t.Fatalf("games = %#v, want one indexed game", games)
	}
	gameManifestBody := get(t, ts.URL+"/api/client/games/"+itoa(games[0].ID)+"/manifest")
	if strings.Contains(gameManifestBody, root) || strings.Contains(gameManifestBody, "filePath") {
		t.Fatalf("game client manifest leaked file path: %q", gameManifestBody)
	}
	if !strings.Contains(gameManifestBody, `"assetType":"game"`) || !strings.Contains(gameManifestBody, `"platform":"snes"`) || !strings.Contains(gameManifestBody, `"/api/client/games/`+itoa(games[0].ID)+`/file"`) {
		t.Fatalf("game client manifest response %q is missing launch metadata", gameManifestBody)
	}

	videos, err := st.ListRecentVideos(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(videos) != 1 {
		t.Fatalf("videos = %#v, want one indexed video", videos)
	}
	videoManifestBody := get(t, ts.URL+"/api/client/videos/"+itoa(videos[0].ID)+"/manifest")
	if strings.Contains(videoManifestBody, root) || strings.Contains(videoManifestBody, "filePath") {
		t.Fatalf("video client manifest leaked file path: %q", videoManifestBody)
	}
	if !strings.Contains(videoManifestBody, `"assetType":"video"`) || !strings.Contains(videoManifestBody, `"format":"mp4"`) || !strings.Contains(videoManifestBody, `"/api/client/videos/`+itoa(videos[0].ID)+`/file"`) {
		t.Fatalf("video client manifest response %q is missing stream metadata", videoManifestBody)
	}

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/client/videos/"+itoa(videos[0].ID)+"/file", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=0-4")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || string(data) != "video" {
		t.Fatalf("video range status=%d body=%q, want 206 video", resp.StatusCode, data)
	}
}

func TestAPIControlsScanJobs(t *testing.T) {
	root := t.TempDir()
	makeZip(t, filepath.Join(root, "Series A", "book1.cbz"), map[string]string{"001.jpg": "image"})

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibrary("Test", root)
	if err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(New(service.New(st), nil).Routes())
	defer ts.Close()

	job, err := st.StartScanJob(lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	pauseBody := postJSONBody(t, ts.URL+"/api/jobs/"+itoa(job.ID)+"/pause", "")
	if !strings.Contains(pauseBody, `"status":"pause_requested"`) {
		t.Fatalf("pause response %q, want pause_requested", pauseBody)
	}
	cancelBody := postJSONBody(t, ts.URL+"/api/jobs/"+itoa(job.ID)+"/cancel", "")
	if !strings.Contains(cancelBody, `"status":"cancel_requested"`) {
		t.Fatalf("cancel response %q, want cancel_requested", cancelBody)
	}

	pausedJob, err := st.StartScanJob(lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	pausedJob.Status = "paused"
	pausedJob.FinishedAt = time.Now()
	if err := st.UpdateScanJob(pausedJob); err != nil {
		t.Fatal(err)
	}
	resumeBody := postJSONBody(t, ts.URL+"/api/jobs/"+itoa(pausedJob.ID)+"/resume", "")
	if !strings.Contains(resumeBody, `"libraryId":`+itoa(lib.ID)) || !strings.Contains(resumeBody, `"status":"running"`) {
		t.Fatalf("resume response %q, want new running job", resumeBody)
	}
}

func TestAPIClientGamesPage(t *testing.T) {
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
	for _, game := range []domain.GameAsset{
		{LibraryID: lib.ID, Title: "Super Contra", Platform: "nes", ROMSetName: "NES", Region: "Japan", Format: "nes", FilePath: "/library/nes/Super Contra.nes", RelPath: "nes/Super Contra.nes", Size: 262160, MTime: time.Unix(30, 0), CRC32: "9bb6059e", SHA1: "5de393e3ad83e6e185e6d338684d7a4475b7d2ce", EmulatorHint: "nes", Compatibility: "unknown"},
		{LibraryID: lib.ID, Title: "Advance Wars", Platform: "gba", ROMSetName: "GBA", Region: "USA", Format: "gba", FilePath: "/library/gba/Advance Wars.gba", RelPath: "gba/Advance Wars.gba", Size: 1024, MTime: time.Unix(31, 0), CRC32: "11111111", SHA1: "1111111111111111111111111111111111111111", EmulatorHint: "gba", Compatibility: "unknown"},
		{LibraryID: lib.ID, Title: "Metal Slug", Platform: "arcade", ROMSetName: "MAME", Region: "World", Format: "zip", FilePath: "/library/arcade/mslug.zip", RelPath: "arcade/mslug.zip", Size: 2048, MTime: time.Unix(32, 0), CRC32: "22222222", SHA1: "2222222222222222222222222222222222222222", EmulatorHint: "arcade", Compatibility: "unknown"},
	} {
		if _, err := st.UpsertGame(game); err != nil {
			t.Fatal(err)
		}
	}
	ts := httptest.NewServer(NewWithOptions(service.New(st), nil, Options{APIToken: "secret"}).Routes())
	defer ts.Close()

	unauthorized, err := http.Get(ts.URL + "/api/client/games?limit=1")
	if err != nil {
		t.Fatal(err)
	}
	_ = unauthorized.Body.Close()
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want 401", unauthorized.StatusCode)
	}

	body := authGet(t, ts.URL+"/api/client/games?limit=2&offset=0&sort=title", "secret")
	if strings.Contains(body, "/library") || strings.Contains(body, "filePath") || strings.Contains(body, "relPath") {
		t.Fatalf("client games leaked internal path: %q", body)
	}
	if !strings.Contains(body, `"total":3`) || !strings.Contains(body, `"limit":2`) || !strings.Contains(body, `"hasMore":true`) || !strings.Contains(body, `"title":"Advance Wars"`) {
		t.Fatalf("client games page %q missing pagination metadata or title sort", body)
	}
	if !strings.Contains(body, `"/api/client/games/`) || !strings.Contains(body, `/manifest"`) {
		t.Fatalf("client games page %q missing manifestUrl", body)
	}

	filtered := authGet(t, ts.URL+"/api/client/games?limit=500&q=japan&platform=nes&format=nes", "secret")
	if !strings.Contains(filtered, `"title":"Super Contra"`) || !strings.Contains(filtered, `"total":1`) || !strings.Contains(filtered, `"limit":200`) || !strings.Contains(filtered, `"hasMore":false`) {
		t.Fatalf("filtered client games page = %q, want clamped one-item response", filtered)
	}

	empty := authGet(t, ts.URL+"/api/client/games?q=missing", "secret")
	if !strings.Contains(empty, `"items":[]`) || !strings.Contains(empty, `"total":0`) {
		t.Fatalf("empty client games page = %q, want empty list response", empty)
	}
}

func TestAPIClientVideosPage(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("Videos", "/library", "video")
	if err != nil {
		t.Fatal(err)
	}
	for _, video := range []domain.VideoAsset{
		{LibraryID: lib.ID, Title: "Alpha Movie", Format: "mp4", FilePath: "/library/Alpha Movie.mp4", RelPath: "Alpha Movie.mp4", Size: 1024, MTime: time.Unix(31, 0), VideoCodec: "h264", AudioCodec: "aac", ThumbnailStatus: "placeholder"},
		{LibraryID: lib.ID, Title: "Beta Clip", Format: "mkv", FilePath: "/library/Beta Clip.mkv", RelPath: "Beta Clip.mkv", Size: 2048, MTime: time.Unix(32, 0), VideoCodec: "hevc", AudioCodec: "dts", ThumbnailStatus: "placeholder"},
	} {
		if _, err := st.UpsertVideo(video); err != nil {
			t.Fatal(err)
		}
	}
	ts := httptest.NewServer(NewWithOptions(service.New(st), nil, Options{APIToken: "secret"}).Routes())
	defer ts.Close()

	unauthorized, err := http.Get(ts.URL + "/api/client/videos?limit=1")
	if err != nil {
		t.Fatal(err)
	}
	_ = unauthorized.Body.Close()
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want 401", unauthorized.StatusCode)
	}

	body := authGet(t, ts.URL+"/api/client/videos?limit=1&offset=0&sort=title", "secret")
	if strings.Contains(body, "/library") || strings.Contains(body, "filePath") || strings.Contains(body, "relPath") {
		t.Fatalf("client videos leaked internal path: %q", body)
	}
	if !strings.Contains(body, `"total":2`) || !strings.Contains(body, `"limit":1`) || !strings.Contains(body, `"hasMore":true`) || !strings.Contains(body, `"title":"Alpha Movie"`) {
		t.Fatalf("client videos page %q missing pagination metadata or title sort", body)
	}
	if !strings.Contains(body, `"/api/client/videos/`) || !strings.Contains(body, `/manifest"`) || !strings.Contains(body, `/transcode/status"`) || !strings.Contains(body, `"/api/videos/`) {
		t.Fatalf("client videos page %q missing manifestUrl, transcodeStatusUrl, or thumbnailUrl", body)
	}

	filtered := authGet(t, ts.URL+"/api/client/videos?q=beta&format=mkv", "secret")
	if !strings.Contains(filtered, `"title":"Beta Clip"`) || !strings.Contains(filtered, `"total":1`) || !strings.Contains(filtered, `"hasMore":false`) {
		t.Fatalf("filtered client videos page = %q, want one-item response", filtered)
	}
	if !strings.Contains(filtered, `"directPlayable":false`) || !strings.Contains(filtered, `"playbackMode":"hls"`) {
		t.Fatalf("filtered client videos page = %q, want hls playback hint for mkv", filtered)
	}

	videos, err := st.ListVideosPage(domain.VideoListOptions{Limit: 10, Sort: "title"})
	if err != nil {
		t.Fatal(err)
	}
	alphaManifest := authGet(t, ts.URL+"/api/client/videos/"+itoa(videos.Items[0].ID)+"/manifest", "secret")
	if !strings.Contains(alphaManifest, `"directPlayable":true`) || !strings.Contains(alphaManifest, `"playbackMode":"direct"`) || !strings.Contains(alphaManifest, `"fileUrl":"/api/client/videos/`) {
		t.Fatalf("alpha video manifest = %q, want direct playback metadata", alphaManifest)
	}
	betaManifest := authGet(t, ts.URL+"/api/client/videos/"+itoa(videos.Items[1].ID)+"/manifest", "secret")
	if !strings.Contains(betaManifest, `"directPlayable":false`) || !strings.Contains(betaManifest, `"playbackMode":"hls"`) || !strings.Contains(betaManifest, `"hlsUrl":"/api/client/videos/`) || !strings.Contains(betaManifest, `"transcodeStatusUrl":"/api/client/videos/`) {
		t.Fatalf("beta video manifest = %q, want hls playback metadata", betaManifest)
	}
	betaStatus := authGet(t, ts.URL+"/api/client/videos/"+itoa(videos.Items[1].ID)+"/transcode/status", "secret")
	if !strings.Contains(betaStatus, `"status":"idle"`) || !strings.Contains(betaStatus, `"segmentCount":0`) {
		t.Fatalf("beta video transcode status = %q, want idle status", betaStatus)
	}
	queueStatus := authGet(t, ts.URL+"/api/client/videos/transcode/status", "secret")
	if !strings.Contains(queueStatus, `"status":"idle"`) || !strings.Contains(queueStatus, `"segmentCount":0`) {
		t.Fatalf("video transcode queue status = %q, want idle status", queueStatus)
	}
}

func TestAPISearchAndPrivateState(t *testing.T) {
	root := t.TempDir()
	makeZip(t, filepath.Join(root, "Series A", "neon.cbz"), map[string]string{"001.jpg": "image"})
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibrary("Test", root)
	if err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(New(service.New(st), nil).Routes())
	defer ts.Close()

	post(t, ts.URL+"/api/libraries/"+itoa(lib.ID)+"/scan", "")
	waitFor(t, func() bool {
		jobs, err := st.ListScanJobs()
		return err == nil && len(jobs) > 0 && jobs[0].Status == "completed"
	})

	series, err := st.ListSeries()
	if err != nil {
		t.Fatal(err)
	}
	books, err := st.ListBooks(series[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	bookID := books[0].ID

	putJSON(t, ts.URL+"/api/books/"+itoa(bookID)+"/private-state", `{"status":"reading","favorite":true,"rating":5,"tags":["vision","noir"],"summary":"Private note"}`)

	bookBody := get(t, ts.URL+"/api/books/"+itoa(bookID))
	if !strings.Contains(bookBody, `"privateStatus":"reading"`) || !strings.Contains(bookBody, `"favorite":true`) || !strings.Contains(bookBody, `"rating":5`) || !strings.Contains(bookBody, `"vision"`) {
		t.Fatalf("book response %q does not include private state", bookBody)
	}

	searchBody := get(t, ts.URL+"/api/search?q=vision&limit=5")
	if !strings.Contains(searchBody, `"books"`) || !strings.Contains(searchBody, `"neon"`) || !strings.Contains(searchBody, `"privateStatus":"reading"`) {
		t.Fatalf("search response %q does not include private-state match", searchBody)
	}

	collectionSearchBody := get(t, ts.URL+"/api/search?q=Series%20A&limit=5")
	if !strings.Contains(collectionSearchBody, `"neon"`) {
		t.Fatalf("collection search response %q does not include collection match", collectionSearchBody)
	}
}

func TestClientAPIPrivateStateUsesSafeDTOs(t *testing.T) {
	root := t.TempDir()
	makeZip(t, filepath.Join(root, "Series A", "neon.cbz"), map[string]string{"001.jpg": "image"})
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibrary("Test", root)
	if err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(New(service.New(st), nil).Routes())
	defer ts.Close()

	post(t, ts.URL+"/api/libraries/"+itoa(lib.ID)+"/scan", "")
	waitFor(t, func() bool {
		jobs, err := st.ListScanJobs()
		return err == nil && len(jobs) > 0 && jobs[0].Status == "completed"
	})

	series, err := st.ListSeries()
	if err != nil {
		t.Fatal(err)
	}
	books, err := st.ListBooks(series[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	bookID := books[0].ID

	stateBody := putJSONBody(t, ts.URL+"/api/client/books/"+itoa(bookID)+"/private-state", `{"status":"want","favorite":true,"rating":4,"tags":["vision","spatial"],"summary":"Vision Pro candidate"}`)
	if strings.Contains(stateBody, root) || strings.Contains(stateBody, "filePath") {
		t.Fatalf("client private-state response leaked file path: %q", stateBody)
	}
	if !strings.Contains(stateBody, `"summary":"Vision Pro candidate"`) || !strings.Contains(stateBody, `"privateStatus":"want"`) {
		t.Fatalf("client private-state response %q does not include saved state", stateBody)
	}

	getStateBody := get(t, ts.URL+"/api/client/books/"+itoa(bookID)+"/private-state")
	if !strings.Contains(getStateBody, `"favorite":true`) || !strings.Contains(getStateBody, `"rating":4`) || !strings.Contains(getStateBody, `"vision"`) {
		t.Fatalf("client private-state get response %q does not include saved state", getStateBody)
	}

	searchBody := get(t, ts.URL+"/api/client/search?q=spatial&limit=5")
	if strings.Contains(searchBody, root) || strings.Contains(searchBody, "filePath") {
		t.Fatalf("client search response leaked file path: %q", searchBody)
	}
	if !strings.Contains(searchBody, `"books"`) || !strings.Contains(searchBody, `"summary":"Vision Pro candidate"`) {
		t.Fatalf("client search response %q does not include private-state match", searchBody)
	}

	favoritesBody := get(t, ts.URL+"/api/client/books/favorites?limit=5")
	if !strings.Contains(favoritesBody, `"favorite":true`) || strings.Contains(favoritesBody, "filePath") {
		t.Fatalf("client favorites response %q is not a safe private-state shelf", favoritesBody)
	}

	wantBody := get(t, ts.URL+"/api/client/books/private-status/want?limit=5")
	if !strings.Contains(wantBody, `"privateStatus":"want"`) || strings.Contains(wantBody, "filePath") {
		t.Fatalf("client private-status response %q is not a safe private-state shelf", wantBody)
	}
}

func TestClientAPIPreferences(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	ts := httptest.NewServer(New(service.New(st), nil).Routes())
	defer ts.Close()

	defaultBody := get(t, ts.URL+"/api/client/preferences")
	if !strings.Contains(defaultBody, `"locale":"zh"`) || !strings.Contains(defaultBody, `"epubFontSize":18`) {
		t.Fatalf("default preferences response %q does not include defaults", defaultBody)
	}

	updatedBody := putJSONBody(t, ts.URL+"/api/client/preferences", `{"locale":"zht","readerPageMode":"webtoon","epubPageMode":"double","epubTheme":"dark","epubFontSize":40}`)
	if !strings.Contains(updatedBody, `"locale":"zht"`) || !strings.Contains(updatedBody, `"readerPageMode":"webtoon"`) || !strings.Contains(updatedBody, `"epubTheme":"dark"`) || !strings.Contains(updatedBody, `"epubFontSize":26`) {
		t.Fatalf("updated preferences response %q does not include normalized preferences", updatedBody)
	}

	savedBody := get(t, ts.URL+"/api/client/preferences")
	if savedBody != updatedBody {
		t.Fatalf("saved preferences = %q, want %q", savedBody, updatedBody)
	}
}

func TestScanSettingsAPI(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	ts := httptest.NewServer(New(service.New(st), nil).Routes())
	defer ts.Close()

	defaultBody := get(t, ts.URL+"/api/settings/scan")
	if !strings.Contains(defaultBody, `"scanWorkers":1`) {
		t.Fatalf("default scan settings = %q, want one worker", defaultBody)
	}

	updatedBody := putJSONBody(t, ts.URL+"/api/settings/scan", `{"scanWorkers":99}`)
	if !strings.Contains(updatedBody, `"scanWorkers":8`) {
		t.Fatalf("updated scan settings = %q, want clamped workers", updatedBody)
	}

	savedBody := get(t, ts.URL+"/api/settings/scan")
	if savedBody != updatedBody {
		t.Fatalf("saved settings = %q, want %q", savedBody, updatedBody)
	}
}

func TestAPICreatesGameTypedLibraryForZipROMSets(t *testing.T) {
	root := t.TempDir()
	makeZip(t, filepath.Join(root, "Arcade", "mslug.zip"), map[string]string{"mslug.rom": "rom"})
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	ts := httptest.NewServer(New(service.New(st), nil).Routes())
	defer ts.Close()

	body := postJSONBody(t, ts.URL+"/api/libraries", `{"name":"Arcade","rootPath":"`+root+`","assetType":"game"}`)
	if !strings.Contains(body, `"assetType":"game"`) {
		t.Fatalf("library response %q does not include game asset type", body)
	}
	libs, err := st.ListLibraries()
	if err != nil {
		t.Fatal(err)
	}
	if len(libs) != 1 || libs[0].AssetType != "game" {
		t.Fatalf("libraries = %#v, want game typed library", libs)
	}

	post(t, ts.URL+"/api/libraries/"+itoa(libs[0].ID)+"/scan", "")
	waitFor(t, func() bool {
		jobs, err := st.ListScanJobs()
		return err == nil && len(jobs) > 0 && jobs[0].Status == "completed"
	})
	gamesBody := get(t, ts.URL+"/api/games/recent")
	if !strings.Contains(gamesBody, `"title":"mslug"`) || !strings.Contains(gamesBody, `"format":"zip"`) || strings.Contains(gamesBody, root) {
		t.Fatalf("games response %q is missing safe zip ROM set", gamesBody)
	}
}

func TestAPICreatesVideoTypedLibrary(t *testing.T) {
	root := t.TempDir()
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	ts := httptest.NewServer(New(service.New(st), nil).Routes())
	defer ts.Close()

	body := postJSONBody(t, ts.URL+"/api/libraries", `{"name":"Videos","rootPath":"`+root+`","assetType":"video"}`)
	if !strings.Contains(body, `"assetType":"video"`) {
		t.Fatalf("library response %q does not include video asset type", body)
	}
}

func TestSetupStatusAndInitializeStoresTokenAndLibrary(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FOLIOSPACE_DIRECTORY_ROOTS", root)
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	ts := httptest.NewServer(New(service.New(st), nil).Routes())
	defer ts.Close()

	statusBody := get(t, ts.URL+"/api/setup/status")
	if !strings.Contains(statusBody, `"initialized":false`) ||
		!strings.Contains(statusBody, `"authEnabled":false`) ||
		!strings.Contains(statusBody, root) {
		t.Fatalf("setup status = %q, want uninitialized status with directory roots", statusBody)
	}

	initResp, err := http.Post(ts.URL+"/api/setup/initialize", "application/json", strings.NewReader(`{"token":"secret-token","name":"Books","rootPath":"`+root+`","assetType":"book"}`))
	if err != nil {
		t.Fatal(err)
	}
	initData, err := io.ReadAll(initResp.Body)
	if err != nil {
		t.Fatal(err)
	}
	_ = initResp.Body.Close()
	if initResp.StatusCode >= 400 {
		t.Fatalf("POST setup initialize status %d: %s", initResp.StatusCode, initData)
	}
	initBody := string(initData)
	if !strings.Contains(initBody, `"name":"Books"`) || !strings.Contains(initBody, `"assetType":"book"`) {
		t.Fatalf("initialize response = %q, want created book library", initBody)
	}
	if len(initResp.Cookies()) == 0 || initResp.Cookies()[0].Name != authCookieName {
		t.Fatalf("initialize cookies = %+v, want auth cookie", initResp.Cookies())
	}

	authBody := get(t, ts.URL+"/api/auth/status")
	if !strings.Contains(authBody, `"enabled":true`) {
		t.Fatalf("auth status = %q, want DB token enabled", authBody)
	}
	resp, err := http.Get(ts.URL + "/api/collections")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated collections status = %d, want 401", resp.StatusCode)
	}
	collectionsBody := authGet(t, ts.URL+"/api/collections", "secret-token")
	if strings.Contains(collectionsBody, "Unauthorized") {
		t.Fatalf("authorized collections response = %q", collectionsBody)
	}
	cookieReq, err := http.NewRequest(http.MethodGet, ts.URL+"/api/collections", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range initResp.Cookies() {
		cookieReq.AddCookie(cookie)
	}
	cookieResp, err := http.DefaultClient.Do(cookieReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = cookieResp.Body.Close()
	if cookieResp.StatusCode != http.StatusOK {
		t.Fatalf("cookie-authenticated collections status = %d, want 200", cookieResp.StatusCode)
	}
}

func TestSetupInitializeRequiresEnvTokenWhenConfigured(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FOLIOSPACE_DIRECTORY_ROOTS", root)
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	ts := httptest.NewServer(NewWithOptions(service.New(st), nil, Options{APIToken: "env-secret"}).Routes())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/setup/initialize", "application/json", strings.NewReader(`{"name":"Books","rootPath":"`+root+`","assetType":"book"}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated initialize status = %d, want 401", resp.StatusCode)
	}

	body := postJSONBodyWithToken(t, ts.URL+"/api/setup/initialize", `{"name":"Books","rootPath":"`+root+`","assetType":"book"}`, "env-secret")
	if !strings.Contains(body, `"name":"Books"`) {
		t.Fatalf("authenticated initialize response = %q, want created library", body)
	}
}

func TestSetupInitializeCanSecureExistingLibrary(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FOLIOSPACE_DIRECTORY_ROOTS", root)
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	svc := service.New(st)
	ts := httptest.NewServer(New(svc, nil).Routes())
	defer ts.Close()
	existing, err := svc.CreateLibraryWithType("Existing", root, "book")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}

	statusBody := get(t, ts.URL+"/api/setup/status")
	if !strings.Contains(statusBody, `"initialized":false`) ||
		!strings.Contains(statusBody, `"hasLibraries":true`) ||
		!strings.Contains(statusBody, `"tokenConfigured":false`) {
		t.Fatalf("unexpected setup status: %s", statusBody)
	}

	body := postJSONBody(t, ts.URL+"/api/setup/initialize", `{"token":"secret-token"}`)
	if !strings.Contains(body, `"id":`+itoa(existing.ID)) || !strings.Contains(body, `"name":"Existing"`) {
		t.Fatalf("initialize existing response = %q, want existing library", body)
	}

	authBody := get(t, ts.URL+"/api/auth/status")
	if !strings.Contains(authBody, `"enabled":true`) {
		t.Fatalf("expected auth enabled after securing existing library, got %s", authBody)
	}
}

func TestConfigDirectoryRootsListsContainerRoots(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FOLIOSPACE_DIRECTORY_ROOTS", root)
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	ts := httptest.NewServer(New(service.New(st), nil).Routes())
	defer ts.Close()

	body := get(t, ts.URL+"/api/config/directory-roots")
	if !strings.Contains(body, `"roots"`) || !strings.Contains(body, root) {
		t.Fatalf("directory roots response = %q, want configured root", body)
	}
}

func TestAPIRequiresBearerTokenWhenConfigured(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	ts := httptest.NewServer(NewWithOptions(service.New(st), nil, Options{APIToken: "secret"}).Routes())
	defer ts.Close()

	statusBody := get(t, ts.URL+"/api/auth/status")
	if !strings.Contains(statusBody, `"enabled":true`) {
		t.Fatalf("auth status = %q, want enabled", statusBody)
	}
	authResp, err := http.Post(ts.URL+"/api/auth/check", "application/json", strings.NewReader(`{"token":"secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	cookies := authResp.Cookies()
	_ = authResp.Body.Close()
	if len(cookies) == 0 {
		t.Fatal("auth check did not set an auth cookie")
	}

	resp, err := http.Get(ts.URL + "/api/collections")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	cookieReq, err := http.NewRequest(http.MethodGet, ts.URL+"/api/collections", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range cookies {
		cookieReq.AddCookie(cookie)
	}
	resp, err = http.DefaultClient.Do(cookieReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cookie authenticated status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/collections", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("authenticated status = %d, want %d: %s", resp.StatusCode, http.StatusOK, body)
	}
}

func get(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func authGet(t *testing.T, url string, token string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode >= 400 {
		t.Fatalf("GET %s status %d: %s", url, resp.StatusCode, data)
	}
	return string(data)
}

func post(t *testing.T, url string, body string) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST %s status %d: %s", url, resp.StatusCode, data)
	}
}

func postJSONBody(t *testing.T, url string, body string) string {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode >= 400 {
		t.Fatalf("POST %s status %d: %s", url, resp.StatusCode, data)
	}
	return string(data)
}

func postJSONBodyWithToken(t *testing.T, url string, body string, token string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode >= 400 {
		t.Fatalf("POST %s status %d: %s", url, resp.StatusCode, data)
	}
	return string(data)
}

func putJSON(t *testing.T, url string, body string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("PUT %s status %d: %s", url, resp.StatusCode, data)
	}
}

func putJSONBody(t *testing.T, url string, body string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode >= 400 {
		t.Fatalf("PUT %s status %d: %s", url, resp.StatusCode, data)
	}
	return string(data)
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	for range 50 {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition was not met")
}

func itoa(value int64) string {
	return strconv.FormatInt(value, 10)
}

func makeZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, body := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
