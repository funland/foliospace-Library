package httpapi

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
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

func TestServeGameStreamSupportsRangeRequests(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "game-*.nds")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("0123456789"); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/client/games/1/files/0", nil)
	req.Header.Set("Range", "bytes=2-5")
	recorder := httptest.NewRecorder()
	serveGameStream(recorder, req, service.PageStream{Body: file, ContentType: "application/octet-stream"}, 10, "game.nds")
	resp := recorder.Result()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusPartialContent || string(body) != "2345" || resp.Header.Get("Accept-Ranges") != "bytes" {
		t.Fatalf("range response status=%d body=%q headers=%v", resp.StatusCode, body, resp.Header)
	}
}

func TestServeGameStreamSupportsRangeRequestsForNonSeekableBodies(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/client/games/1/files/0", nil)
	req.Header.Set("Range", "bytes=3-6")
	recorder := httptest.NewRecorder()
	serveGameStream(recorder, req, service.PageStream{
		Body: io.NopCloser(bytes.NewBufferString("0123456789")), ContentType: "application/octet-stream",
	}, 10, "game.3ds")
	resp := recorder.Result()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusPartialContent || string(body) != "3456" || resp.Header.Get("Content-Range") != "bytes 3-6/10" || resp.Header.Get("Accept-Ranges") != "bytes" {
		t.Fatalf("range response status=%d body=%q headers=%v", resp.StatusCode, body, resp.Header)
	}
}

func TestAPIClientNDSZIPDownloadSupportsRange(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "Elite Beat Agents (USA).zip")
	romName := "Elite Beat Agents (USA).nds"
	rom := []byte("nds-rom-body")
	makeZip(t, zipPath, map[string]string{romName: string(rom)})

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("NDS", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	game, err := st.UpsertGame(domain.GameAsset{
		LibraryID: lib.ID, Title: "Elite Beat Agents", Platform: "nds", ROMSetName: "Nintendo DS", Format: "nds",
		FilePath: zipPath, RelPath: romName, Size: int64(len(rom)), MTime: time.Now(),
		EmulatorHint: "melonds-ds", Compatibility: "untested", CatalogRole: "game",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceGameFiles(game.ID, []domain.GameFile{{
		Name: romName, FilePath: zipPath, Size: int64(len(rom)), MTime: time.Now(), Role: "entry", Position: 0,
	}}); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(NewWithOptions(service.New(st), nil, Options{APIToken: "secret"}).Routes())
	defer ts.Close()
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/client/games/"+itoa(game.ID)+"/file", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Range", "bytes=2-5")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusPartialContent || !bytes.Equal(body, rom[2:6]) || resp.Header.Get("Content-Range") != "bytes 2-5/12" {
		t.Fatalf("NDS range status=%d body=%q headers=%v", resp.StatusCode, body, resp.Header)
	}
	if resp.Header.Get("Content-Disposition") != `attachment; filename="Elite Beat Agents (USA).nds"` {
		t.Fatalf("NDS content disposition = %q", resp.Header.Get("Content-Disposition"))
	}
}

func TestClientGameNintendo3DSContentMode(t *testing.T) {
	launch := clientGameItem(domain.GameAsset{ID: 1, Platform: "3ds", Format: "3ds"})
	if launch.ContentMode != "launch" || launch.InputProfile != "standard" {
		t.Fatalf("launch item = %#v", launch)
	}
	install := clientGameItem(domain.GameAsset{ID: 2, Platform: "3ds", Format: "cia"})
	if install.ContentMode != "install" {
		t.Fatalf("install item = %#v", install)
	}
}

func TestAPIClientBookFileDownloadSupportsAuthAndRanges(t *testing.T) {
	root := t.TempDir()
	bookPath := filepath.Join(root, "Series A", "book1.cbz")
	makeJPEGZip(t, bookPath)
	want, err := os.ReadFile(bookPath)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(bookPath)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibrary("Comics", root)
	if err != nil {
		t.Fatal(err)
	}
	series, err := st.UpsertSeries(lib.ID, "Series A", "Series A")
	if err != nil {
		t.Fatal(err)
	}
	book, err := st.UpsertBook(series.ID, "book1", "cbz")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertFile(book.ID, lib.ID, bookPath, "Series A/book1.cbz", info.Size(), info.ModTime(), ".cbz"); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(NewWithOptions(service.New(st), nil, Options{APIToken: "secret"}).Routes())
	defer ts.Close()

	catalog := authGet(t, ts.URL+"/api/client/books?limit=10", "secret")
	downloadURL := "/api/client/books/" + itoa(book.ID) + "/file"
	if !strings.Contains(catalog, `"downloadUrl":"`+downloadURL+`"`) {
		t.Fatalf("catalog = %q, want downloadUrl", catalog)
	}
	manifest := authGet(t, ts.URL+"/api/client/books/"+itoa(book.ID)+"/manifest", "secret")
	if !strings.Contains(manifest, `"fileUrl":"`+downloadURL+`"`) {
		t.Fatalf("manifest = %q, want fileUrl", manifest)
	}

	unauthorized, err := http.Get(ts.URL + downloadURL)
	if err != nil {
		t.Fatal(err)
	}
	unauthorized.Body.Close()
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want 401", unauthorized.StatusCode)
	}

	req, err := http.NewRequest(http.MethodGet, ts.URL+downloadURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Range", "bytes=0-15")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusPartialContent || !bytes.Equal(got, want[:16]) {
		t.Fatalf("range status=%d body=%x, want first 16 source bytes", resp.StatusCode, got)
	}
	if !strings.Contains(resp.Header.Get("Content-Disposition"), "book1.cbz") {
		t.Fatalf("content disposition = %q, want source filename", resp.Header.Get("Content-Disposition"))
	}

	if err := os.Remove(bookPath); err != nil {
		t.Fatal(err)
	}
	missingReq, err := http.NewRequest(http.MethodGet, ts.URL+downloadURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	missingReq.Header.Set("Authorization", "Bearer secret")
	missingResp, err := http.DefaultClient.Do(missingReq)
	if err != nil {
		t.Fatal(err)
	}
	missingResp.Body.Close()
	if missingResp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing source status = %d, want 404", missingResp.StatusCode)
	}
}

func TestProjectJusticeRevAManifestUsesCanonicalCloneName(t *testing.T) {
	game := domain.GameAsset{
		ID:           15961,
		Title:        "Project Justice / Moero! Justice Gakuen (Rev A)",
		Platform:     "naomi",
		ROMSetName:   "pjustica",
		Format:       "zip",
		RelPath:      "NAOMI/pjustica.zip",
		FilePath:     "/games/NAOMI/pjustic.zip",
		EmulatorHint: "flycast",
		CatalogRole:  "game",
	}
	manifest := clientGameManifest(game, []domain.GameFile{{
		Name: "pjustica.zip", FilePath: game.FilePath, Role: "entry", Position: 0,
	}}, nil)
	if manifest.Game.FileName != "pjustica.zip" || manifest.Game.ParentROMSetName != "pjustic" {
		t.Fatalf("manifest game = %#v", manifest.Game)
	}
	if manifest.EntryFile == nil || *manifest.EntryFile != "pjustica.zip" || len(manifest.Files) != 1 || manifest.Files[0].Name != "pjustica.zip" {
		t.Fatalf("manifest = %#v", manifest)
	}
}

func TestKonamiPython1ManifestPreservesRelativeDependenciesAndChecksums(t *testing.T) {
	game := domain.GameAsset{
		ID: 19001, Title: "World Soccer Winning Eleven Arcade Game", Platform: "konami-python1",
		ROMSetName: "KONAMI-PYTHON1", Format: "py1", RelPath: "KONAMI/kpython1/wswe.py1",
		FilePath: "/games/KONAMI/kpython1/wswe.py1", EmulatorHint: "pcsx2-reliquary", CatalogRole: "game",
	}
	files := []domain.GameFile{
		{Name: "wswe.py1", Size: 100, SHA1: strings.Repeat("1", 40), Role: "entry", Position: 0},
		{Name: "wswe/c18jaa03.chd", Size: 200, SHA1: strings.Repeat("2", 40), Role: "dependency", Position: 1},
		{Name: "wswe/m48t58y.u48", Size: 10, SHA1: strings.Repeat("3", 40), Role: "dependency", Position: 2},
		{Name: "wswe/b22a01.u42", Size: 10, SHA1: strings.Repeat("4", 40), Role: "dependency", Position: 3},
		{Name: "wswe/d72872gc.crom", Size: 10, SHA1: strings.Repeat("5", 40), Role: "dependency", Position: 4},
		{Name: "wswe/ds2430.u3", Size: 10, SHA1: strings.Repeat("6", 40), Role: "dependency", Position: 5},
		{Name: "wswe/kn00002.ps2", Size: 10, SHA1: strings.Repeat("7", 40), Role: "dependency", Position: 6},
		{Name: "wswe/kn00002.id", Size: 10, SHA1: strings.Repeat("8", 40), Role: "dependency", Position: 7},
	}
	manifest := clientGameManifest(game, files, nil)
	if manifest.EntryFile == nil || *manifest.EntryFile != "wswe.py1" || manifest.Game.InputProfile != "standard" || len(manifest.Files) != 8 {
		t.Fatalf("manifest = %#v", manifest)
	}
	if manifest.Files[1].Name != "wswe/c18jaa03.chd" || manifest.Files[1].Checksum != "sha1:"+strings.Repeat("2", 40) {
		t.Fatalf("dependency = %#v", manifest.Files[1])
	}
}

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

func TestAPIStreamsDownsampledComicPage(t *testing.T) {
	root := t.TempDir()
	makeImageZip(t, filepath.Join(root, "Tall", "chapter.cbz"), "001.jpg", 800, 2400)
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
	if len(series) != 1 {
		t.Fatalf("series count = %d, want 1", len(series))
	}
	books, err := st.ListBooks(series[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 1 {
		t.Fatalf("book count = %d, want 1", len(books))
	}
	manifestBody := get(t, ts.URL+"/api/client/books/"+itoa(books[0].ID)+"/manifest")
	if !strings.Contains(manifestBody, `"displayUrl":"/api/books/`+itoa(books[0].ID)+`/pages/0?maxWidth=1200"`) {
		t.Fatalf("manifest response %q is missing safe display URL", manifestBody)
	}

	resp, err := http.Get(ts.URL + "/api/books/" + itoa(books[0].ID) + "/pages/0?maxWidth=400")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("content type = %q, want image/jpeg", resp.Header.Get("Content-Type"))
	}
	img, _, err := image.Decode(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got := img.Bounds().Dx(); got != 400 {
		t.Fatalf("downsampled width = %d, want 400", got)
	}
}

func TestAPIReadingPositionWebtoonV1(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	root := t.TempDir()
	lib, err := st.CreateLibrary("Comics", root)
	if err != nil {
		t.Fatal(err)
	}
	series, err := st.UpsertSeries(lib.ID, "Series A", "Series A")
	if err != nil {
		t.Fatal(err)
	}
	book, err := st.UpsertBook(series.ID, "Book 1", "cbz")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ReplacePages(book.ID, []domain.Page{
		{Index: 0, Name: "0000.webp"},
		{Index: 1, Name: "nested/0001.webp"},
	}); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(New(service.New(st), nil).Routes())
	defer ts.Close()

	body := putJSONBody(t, ts.URL+"/api/books/"+itoa(book.ID)+"/reading-position/webtoon", `{
		"schema":"webtoon-position-v1",
		"pageIndex":1,
		"pageKey":"archive:nested/0001.webp",
		"pageYOffsetRatio":1.5,
		"viewportAnchorRatio":0.28,
		"documentProgress":-0.2,
		"pageCount":2,
		"contentSignature":"sig-a"
	}`)
	if !strings.Contains(body, `"schema":"webtoon-position-v1"`) ||
		!strings.Contains(body, `"pageKey":"archive:nested/0001.webp"`) ||
		!strings.Contains(body, `"pageYOffsetRatio":1`) ||
		!strings.Contains(body, `"documentProgress":0`) {
		t.Fatalf("save reading-position body = %q, want normalized webtoon position", body)
	}

	positionsBody := get(t, ts.URL+"/api/books/"+itoa(book.ID)+"/reading-position")
	if !strings.Contains(positionsBody, `"positions"`) ||
		!strings.Contains(positionsBody, `"webtoon"`) ||
		!strings.Contains(positionsBody, `"pageKey":"archive:nested/0001.webp"`) ||
		!strings.Contains(positionsBody, `"viewportAnchorRatio":0.28`) {
		t.Fatalf("reading-position body = %q, want stored webtoon position", positionsBody)
	}

	progressBody := get(t, ts.URL+"/api/books/"+itoa(book.ID)+"/progress")
	if !strings.Contains(progressBody, `"pageIndex":1`) ||
		!strings.Contains(progressBody, `"locator":"webtoon:0"`) ||
		!strings.Contains(progressBody, `"progressFraction":0`) {
		t.Fatalf("legacy progress body = %q, want synced legacy progress with webtoon locator fallback", progressBody)
	}

	manifestBody := get(t, ts.URL+"/api/client/books/"+itoa(book.ID)+"/manifest")
	if !strings.Contains(manifestBody, `"pageKey":"archive:nested/0001.webp"`) {
		t.Fatalf("manifest body = %q, want stable pageKey in page refs", manifestBody)
	}
}

func TestThumbnailAPIAndWorkerControls(t *testing.T) {
	root := t.TempDir()
	bookPath := filepath.Join(root, "Series A", "book1.cbz")
	makeJPEGZip(t, bookPath)
	info, err := os.Stat(bookPath)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibrary("Comics", root)
	if err != nil {
		t.Fatal(err)
	}
	series, err := st.UpsertSeries(lib.ID, "Series A", "Series A")
	if err != nil {
		t.Fatal(err)
	}
	book, err := st.UpsertBook(series.ID, "book1", "cbz")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertFile(book.ID, lib.ID, bookPath, "Series A/book1.cbz", info.Size(), info.ModTime(), ".cbz"); err != nil {
		t.Fatal(err)
	}
	configDir := t.TempDir()
	svc := service.NewWithConfig(st, configDir)
	svc.PauseThumbnailWorker()
	ts := httptest.NewServer(New(svc, nil).Routes())
	defer ts.Close()

	volumesBody := get(t, ts.URL+"/api/collections/"+itoa(series.ID)+"/volumes?limit=1")
	var volumesPage struct {
		Items []domain.Book `json:"items"`
	}
	if err := json.Unmarshal([]byte(volumesBody), &volumesPage); err != nil {
		t.Fatal(err)
	}
	if len(volumesPage.Items) != 1 || volumesPage.Items[0].ThumbnailURL != "/api/books/"+itoa(book.ID)+"/thumbnail?size=small&v=v1-cover-refresh-4" || volumesPage.Items[0].ThumbnailStatus == "" {
		t.Fatalf("volumes page = %#v, want thumbnail URL with upgraded client cache version", volumesPage)
	}
	putJSON(t, ts.URL+"/api/books/"+itoa(book.ID)+"/progress", `{"pageIndex":1,"progressFraction":0.5}`)
	continueBody := get(t, ts.URL+"/api/books/continue-reading?limit=1")
	var continueBooks []domain.Book
	if err := json.Unmarshal([]byte(continueBody), &continueBooks); err != nil {
		t.Fatal(err)
	}
	if len(continueBooks) != 1 || continueBooks[0].ThumbnailURL != "/api/books/"+itoa(book.ID)+"/thumbnail?size=small&v=v1-cover-refresh-4" || continueBooks[0].ThumbnailStatus == "" {
		t.Fatalf("continue reading = %#v, want versioned thumbnail URL", continueBooks)
	}
	searchBody := get(t, ts.URL+"/api/search?q=book&limit=1")
	var searchResult searchResponse
	if err := json.Unmarshal([]byte(searchBody), &searchResult); err != nil {
		t.Fatal(err)
	}
	if len(searchResult.Books) != 1 || searchResult.Books[0].ThumbnailURL != "/api/books/"+itoa(book.ID)+"/thumbnail?size=small&v=v1-cover-refresh-4" || searchResult.Books[0].ThumbnailStatus == "" {
		t.Fatalf("search result = %#v, want versioned thumbnail URL", searchResult)
	}

	resp, err := http.Get(ts.URL + "/api/books/" + itoa(book.ID) + "/thumbnail?size=small")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.Header.Get("Content-Type") != "image/jpeg" || resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("ETag") != "" || resp.Header.Get("X-FolioSpace-Thumbnail-Fallback") != "source" || len(body) == 0 {
		t.Fatalf("source fallback response type=%q cache=%q etag=%q fallback=%q len=%d", resp.Header.Get("Content-Type"), resp.Header.Get("Cache-Control"), resp.Header.Get("ETag"), resp.Header.Get("X-FolioSpace-Thumbnail-Fallback"), len(body))
	}
	headResp, err := http.Head(ts.URL + "/api/books/" + itoa(book.ID) + "/thumbnail?size=small")
	if err != nil {
		t.Fatal(err)
	}
	_ = headResp.Body.Close()
	if headResp.Header.Get("Content-Type") != "image/jpeg" || headResp.Header.Get("Cache-Control") != "no-store" || headResp.Header.Get("ETag") != "" || headResp.Header.Get("X-FolioSpace-Thumbnail-Fallback") != "source" {
		t.Fatalf("source fallback HEAD type=%q cache=%q etag=%q fallback=%q", headResp.Header.Get("Content-Type"), headResp.Header.Get("Cache-Control"), headResp.Header.Get("ETag"), headResp.Header.Get("X-FolioSpace-Thumbnail-Fallback"))
	}
	statusBody := get(t, ts.URL+"/api/thumbnail-worker/status")
	if !strings.Contains(statusBody, `"status":"paused"`) || !strings.Contains(statusBody, `"queued":1`) {
		t.Fatalf("status body %q, want paused queued worker", statusBody)
	}

	resumeBody := postJSONBody(t, ts.URL+"/api/thumbnail-worker/resume", "")
	if !strings.Contains(resumeBody, `"workerEnabled":true`) {
		t.Fatalf("resume body %q, want worker status", resumeBody)
	}
	if err := svc.ProcessNextThumbnailJobForTest(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		status, err := svc.ThumbnailWorkerStatus()
		return err == nil && status.Ready == 1
	})
	resp, err = http.Get(ts.URL + "/api/books/" + itoa(book.ID) + "/thumbnail?size=small")
	if err != nil {
		t.Fatal(err)
	}
	imageBody, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.Header.Get("Content-Type") != "image/jpeg" || !strings.Contains(resp.Header.Get("Cache-Control"), "max-age=2592000") || resp.Header.Get("ETag") == "" || len(imageBody) == 0 {
		t.Fatalf("cached response type=%q cache=%q etag=%q len=%d", resp.Header.Get("Content-Type"), resp.Header.Get("Cache-Control"), resp.Header.Get("ETag"), len(imageBody))
	}
	headResp, err = http.Head(ts.URL + "/api/books/" + itoa(book.ID) + "/thumbnail?size=small")
	if err != nil {
		t.Fatal(err)
	}
	_ = headResp.Body.Close()
	if headResp.Header.Get("Content-Type") != "image/jpeg" || !strings.Contains(headResp.Header.Get("Cache-Control"), "max-age=2592000") || headResp.Header.Get("ETag") == "" {
		t.Fatalf("cached HEAD type=%q cache=%q etag=%q", headResp.Header.Get("Content-Type"), headResp.Header.Get("Cache-Control"), headResp.Header.Get("ETag"))
	}

	svc.PauseThumbnailWorker()
	_, err = http.Get(ts.URL + "/api/books/" + itoa(book.ID) + "/thumbnail?size=medium")
	if err != nil {
		t.Fatal(err)
	}
	cancelBody := postJSONBody(t, ts.URL+"/api/thumbnail-worker/cancel", "")
	if !strings.Contains(cancelBody, `"cancelled":1`) {
		t.Fatalf("cancel body %q, want one cancelled thumbnail job", cancelBody)
	}

	orphanPath := filepath.Join(configDir, "cache", "book-thumbnails", "small", "orphan.jpg")
	if err := os.WriteFile(orphanPath, []byte("orphan-cache"), 0o644); err != nil {
		t.Fatal(err)
	}
	cleanupBody := postJSONBody(t, ts.URL+"/api/thumbnail-worker/cleanup-orphans", "")
	if !strings.Contains(cleanupBody, `"orphanFiles":0`) {
		t.Fatalf("cleanup body %q, want orphan cache files cleaned", cleanupBody)
	}
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Fatalf("orphan cache file still exists or stat failed unexpectedly: %v", err)
	}
}

func TestThumbnailAPIStreamsStaleCacheFallbackWithoutLongCaching(t *testing.T) {
	root := t.TempDir()
	bookPath := filepath.Join(root, "Series A", "book1.cbz")
	makeJPEGZip(t, bookPath)
	info, err := os.Stat(bookPath)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibrary("Comics", root)
	if err != nil {
		t.Fatal(err)
	}
	series, err := st.UpsertSeries(lib.ID, "Series A", "Series A")
	if err != nil {
		t.Fatal(err)
	}
	book, err := st.UpsertBook(series.ID, "book1", "cbz")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertFile(book.ID, lib.ID, bookPath, "Series A/book1.cbz", info.Size(), info.ModTime(), ".cbz"); err != nil {
		t.Fatal(err)
	}
	configDir := t.TempDir()
	svc := service.NewWithConfig(st, configDir)
	svc.PauseThumbnailWorker()
	stalePath := filepath.Join(configDir, "cache", "book-thumbnails", "small", itoa(book.ID)+"-legacy-fallback.jpg")
	if err := os.MkdirAll(filepath.Dir(stalePath), 0o755); err != nil {
		t.Fatal(err)
	}
	staleBytes := makeJPEGBytes(t, 32, 44, color.RGBA{R: 100, G: 80, B: 170, A: 255})
	if err := os.WriteFile(stalePath, staleBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(New(svc, nil).Routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/books/" + itoa(book.ID) + "/thumbnail?size=small")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.Header.Get("Content-Type") != "image/jpeg" || resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("ETag") != "" || resp.Header.Get("X-FolioSpace-Thumbnail-Fallback") != "stale" || !bytes.Equal(body, staleBytes) {
		t.Fatalf("stale response type=%q cache=%q etag=%q fallback=%q len=%d, want no-store stale jpeg", resp.Header.Get("Content-Type"), resp.Header.Get("Cache-Control"), resp.Header.Get("ETag"), resp.Header.Get("X-FolioSpace-Thumbnail-Fallback"), len(body))
	}
	headResp, err := http.Head(ts.URL + "/api/books/" + itoa(book.ID) + "/thumbnail?size=small")
	if err != nil {
		t.Fatal(err)
	}
	_ = headResp.Body.Close()
	if headResp.Header.Get("Content-Type") != "image/jpeg" || headResp.Header.Get("Cache-Control") != "no-store" || headResp.Header.Get("ETag") != "" || headResp.Header.Get("X-FolioSpace-Thumbnail-Fallback") != "stale" {
		t.Fatalf("stale HEAD type=%q cache=%q etag=%q fallback=%q, want no-store stale jpeg headers", headResp.Header.Get("Content-Type"), headResp.Header.Get("Cache-Control"), headResp.Header.Get("ETag"), headResp.Header.Get("X-FolioSpace-Thumbnail-Fallback"))
	}
	status, err := svc.ThumbnailWorkerStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status.Queued != 1 || status.Ready != 0 || !status.Paused {
		t.Fatalf("thumbnail worker status = %#v, want queued regeneration while stale fallback is streamed", status)
	}
}

func TestCollectionVolumesPreservesLegacyBookFieldsWithThumbnails(t *testing.T) {
	root := t.TempDir()
	bookPath := filepath.Join(root, "Series A", "book1.cbz")
	makeJPEGZip(t, bookPath)
	info, err := os.Stat(bookPath)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibrary("Comics", root)
	if err != nil {
		t.Fatal(err)
	}
	series, err := st.UpsertSeries(lib.ID, "Series A", "Series A")
	if err != nil {
		t.Fatal(err)
	}
	book, err := st.UpsertBook(series.ID, "book1", "cbz")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertFile(book.ID, lib.ID, bookPath, "Series A/book1.cbz", info.Size(), info.ModTime(), ".cbz"); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(New(service.NewWithConfig(st, t.TempDir()), nil).Routes())
	defer ts.Close()

	body := get(t, ts.URL+"/api/collections/"+itoa(series.ID)+"/volumes")
	var volumes []map[string]any
	if err := json.Unmarshal([]byte(body), &volumes); err != nil {
		t.Fatal(err)
	}
	if len(volumes) != 1 || volumes[0]["filePath"] != bookPath || volumes[0]["thumbnailUrl"] == "" || volumes[0]["thumbnailStatus"] == "" {
		t.Fatalf("collection volumes = %#v, want legacy filePath plus thumbnail fields", volumes)
	}

	pagedBody := get(t, ts.URL+"/api/collections/"+itoa(series.ID)+"/volumes?limit=1")
	var page struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal([]byte(pagedBody), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0]["filePath"] != bookPath || page.Items[0]["thumbnailUrl"] == "" || page.Items[0]["thumbnailStatus"] == "" {
		t.Fatalf("paged collection volumes = %#v, want legacy filePath plus thumbnail fields", page.Items)
	}
}

func TestCollectionsIncludeRepresentativeThumbnail(t *testing.T) {
	root := t.TempDir()
	bookPath := filepath.Join(root, "Series A", "book1.cbz")
	makeJPEGZip(t, bookPath)
	info, err := os.Stat(bookPath)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibrary("Comics", root)
	if err != nil {
		t.Fatal(err)
	}
	series, err := st.UpsertSeries(lib.ID, "Series A", "Series A")
	if err != nil {
		t.Fatal(err)
	}
	book, err := st.UpsertBook(series.ID, "book1", "cbz")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertFile(book.ID, lib.ID, bookPath, "Series A/book1.cbz", info.Size(), info.ModTime(), ".cbz"); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(New(service.NewWithConfig(st, t.TempDir()), nil).Routes())
	defer ts.Close()

	collectionsBody := get(t, ts.URL+"/api/collections")
	var collections []map[string]any
	if err := json.Unmarshal([]byte(collectionsBody), &collections); err != nil {
		t.Fatal(err)
	}
	if len(collections) != 1 ||
		collections[0]["coverBookId"] != float64(book.ID) ||
		collections[0]["thumbnailUrl"] != "/api/books/"+itoa(book.ID)+"/thumbnail?size=small&v=v1-cover-refresh-4" ||
		collections[0]["thumbnailStatus"] != "pending" {
		t.Fatalf("collections = %#v, want representative thumbnail fields", collections)
	}
	pagedCollectionsBody := get(t, ts.URL+"/api/collections?primaryType=comic&limit=1&offset=0&sort=title&direction=asc")
	var pagedCollections struct {
		Items   []map[string]any `json:"items"`
		Total   int64            `json:"total"`
		Limit   int              `json:"limit"`
		Offset  int              `json:"offset"`
		HasMore bool             `json:"hasMore"`
	}
	if err := json.Unmarshal([]byte(pagedCollectionsBody), &pagedCollections); err != nil {
		t.Fatal(err)
	}
	if pagedCollections.Total != 1 ||
		pagedCollections.Limit != 1 ||
		pagedCollections.Offset != 0 ||
		pagedCollections.HasMore ||
		len(pagedCollections.Items) != 1 ||
		pagedCollections.Items[0]["primaryType"] != "comic" ||
		pagedCollections.Items[0]["thumbnailUrl"] != "/api/books/"+itoa(book.ID)+"/thumbnail?size=small&v=v1-cover-refresh-4" {
		t.Fatalf("paged collections = %#v, want comic page with representative thumbnail", pagedCollections)
	}

	homeBody := get(t, ts.URL+"/api/client/home")
	var home struct {
		Collections []map[string]any `json:"collections"`
	}
	if err := json.Unmarshal([]byte(homeBody), &home); err != nil {
		t.Fatal(err)
	}
	if len(home.Collections) != 1 ||
		home.Collections[0]["coverBookId"] != float64(book.ID) ||
		home.Collections[0]["thumbnailUrl"] != "/api/books/"+itoa(book.ID)+"/thumbnail?size=small&v=v1-cover-refresh-4" ||
		home.Collections[0]["thumbnailStatus"] != "pending" {
		t.Fatalf("client home collections = %#v, want representative thumbnail fields", home.Collections)
	}
}

func TestClientHomeLimitsCollections(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibrary("Comics", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if _, err := st.UpsertSeries(lib.ID, fmt.Sprintf("Series %02d", i), fmt.Sprintf("Series %02d", i)); err != nil {
			t.Fatal(err)
		}
	}

	ts := httptest.NewServer(New(service.NewWithConfig(st, t.TempDir()), nil).Routes())
	defer ts.Close()

	homeBody := get(t, ts.URL+"/api/client/home")
	var home struct {
		Collections []map[string]any `json:"collections"`
	}
	if err := json.Unmarshal([]byte(homeBody), &home); err != nil {
		t.Fatal(err)
	}
	if len(home.Collections) != 12 {
		t.Fatalf("client home collections = %d, want default shelf limit 12", len(home.Collections))
	}
}

func TestClientHomeCanSkipCollections(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibrary("Comics", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertSeries(lib.ID, "Series A", "Series A"); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(New(service.NewWithConfig(st, t.TempDir()), nil).Routes())
	defer ts.Close()

	homeBody := get(t, ts.URL+"/api/client/home?includeCollections=false")
	var home struct {
		Collections []map[string]any `json:"collections"`
	}
	if err := json.Unmarshal([]byte(homeBody), &home); err != nil {
		t.Fatal(err)
	}
	if len(home.Collections) != 0 {
		t.Fatalf("client home collections = %d, want omitted collection shelf", len(home.Collections))
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

	var cbzBookID, epubBookID, seriesAID int64
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
			seriesAID = seriesItem.ID
			cbzBookID = books[0].ID
		case "Books":
			epubBookID = books[0].ID
		}
	}
	if cbzBookID == 0 || epubBookID == 0 {
		t.Fatalf("indexed book ids cbz=%d epub=%d", cbzBookID, epubBookID)
	}
	putJSON(t, ts.URL+"/api/books/"+itoa(cbzBookID)+"/progress", `{"pageIndex":1,"progressFraction":0.5}`)
	collectionStateBody := putJSONBody(t, ts.URL+"/api/collections/"+itoa(seriesAID)+"/private-state", `{"favorite":true,"liked":true}`)
	if !strings.Contains(collectionStateBody, `"favorite":true`) || !strings.Contains(collectionStateBody, `"liked":true`) {
		t.Fatalf("collection private state response %q is missing saved flags", collectionStateBody)
	}

	infoBody := get(t, ts.URL+"/api/client/info")
	if !strings.Contains(infoBody, `"serviceVersion":"0.996"`) ||
		!strings.Contains(infoBody, `"apiVersion":"v1"`) ||
		!strings.Contains(infoBody, `"epub"`) ||
		!strings.Contains(infoBody, `"pdf"`) ||
		!strings.Contains(infoBody, `"mp4"`) ||
		!strings.Contains(infoBody, `"z64"`) ||
		!strings.Contains(infoBody, `"gdi"`) ||
		!strings.Contains(infoBody, `"m3u"`) ||
		!strings.Contains(infoBody, `"videoCatalog":true`) ||
		!strings.Contains(infoBody, `"pdfPageLayout":true`) ||
		!strings.Contains(infoBody, `"pdfWebtoonLayout":true`) ||
		!strings.Contains(infoBody, `"comicWebtoonLayout":true`) ||
		!strings.Contains(infoBody, `"webtoonPositionSync":true`) ||
		!strings.Contains(infoBody, `"pageImageDownsample":true`) ||
		!strings.Contains(infoBody, `"compactReader":true`) ||
		!strings.Contains(infoBody, `"scanSettings":true`) ||
		!strings.Contains(infoBody, `"gameSaveSync":true`) ||
		!strings.Contains(infoBody, `"gamePlatformCatalog":true`) ||
		!strings.Contains(infoBody, `"gameMetadataProviders":true`) {
		t.Fatalf("client info response %q does not include v1 capabilities", infoBody)
	}
	if !strings.Contains(infoBody, `"bookCatalog":true`) {
		t.Fatalf("client info response %q does not advertise book catalog", infoBody)
	}
	if !strings.Contains(infoBody, `"collectionCatalog":true`) {
		t.Fatalf("client info response %q does not advertise collection catalog", infoBody)
	}

	homeBody := get(t, ts.URL+"/api/client/home")
	if strings.Contains(homeBody, root) || strings.Contains(homeBody, "filePath") {
		t.Fatalf("client home leaked file path: %q", homeBody)
	}
	if !strings.Contains(homeBody, `"continueReading"`) || !strings.Contains(homeBody, `"recentBooks"`) || !strings.Contains(homeBody, `"collections"`) {
		t.Fatalf("client home response %q is missing expected sections", homeBody)
	}
	if !strings.Contains(homeBody, `"favorite":true`) || !strings.Contains(homeBody, `"liked":true`) {
		t.Fatalf("client home response %q is missing collection private state", homeBody)
	}
	if !strings.Contains(homeBody, `"gameShelf"`) || !strings.Contains(homeBody, `"Super Mario World"`) || strings.Contains(homeBody, filepath.Join(root, "Games")) {
		t.Fatalf("client home response %q is missing safe game shelf", homeBody)
	}
	if !strings.Contains(homeBody, `"videoShelf"`) || !strings.Contains(homeBody, `"Demo Movie"`) || strings.Contains(homeBody, "Movies/Demo Movie.mp4") {
		t.Fatalf("client home response %q is missing safe video shelf", homeBody)
	}
	if !strings.Contains(homeBody, `"/api/books/`+itoa(cbzBookID)+`/cover?v=v1-cover-refresh-4"`) {
		t.Fatalf("client home response %q does not include cover URL", homeBody)
	}

	catalogBody := get(t, ts.URL+"/api/client/books?limit=1&offset=0&sort=title&direction=desc&format=all")
	if strings.Contains(catalogBody, root) || strings.Contains(catalogBody, "filePath") {
		t.Fatalf("client book catalog leaked file path: %q", catalogBody)
	}
	if !strings.Contains(catalogBody, `"items"`) ||
		!strings.Contains(catalogBody, `"total":2`) ||
		!strings.Contains(catalogBody, `"limit":1`) ||
		!strings.Contains(catalogBody, `"offset":0`) ||
		!strings.Contains(catalogBody, `"hasMore":true`) ||
		!strings.Contains(catalogBody, `"manifestUrl"`) {
		t.Fatalf("client book catalog response %q is missing page metadata or client fields", catalogBody)
	}
	epubCatalogBody := get(t, ts.URL+"/api/client/books?format=epub&limit=10")
	if !strings.Contains(epubCatalogBody, `"total":1`) || !strings.Contains(epubCatalogBody, `"format":"epub"`) || strings.Contains(epubCatalogBody, `"format":"cbz"`) {
		t.Fatalf("client EPUB catalog response %q does not filter by format", epubCatalogBody)
	}

	cbzManifestBody := get(t, ts.URL+"/api/client/books/"+itoa(cbzBookID)+"/manifest")
	if strings.Contains(cbzManifestBody, root) || strings.Contains(cbzManifestBody, "filePath") {
		t.Fatalf("cbz client manifest leaked file path: %q", cbzManifestBody)
	}
	if !strings.Contains(cbzManifestBody, `"format":"cbz"`) ||
		!strings.Contains(cbzManifestBody, `"readerModes":["single","double","webtoon"]`) ||
		!strings.Contains(cbzManifestBody, `"defaultReaderMode":"single"`) ||
		!strings.Contains(cbzManifestBody, `"/api/books/`+itoa(cbzBookID)+`/pages/0"`) {
		t.Fatalf("cbz client manifest response %q is missing reader modes or page URLs", cbzManifestBody)
	}

	epubManifestBody := get(t, ts.URL+"/api/client/books/"+itoa(epubBookID)+"/manifest")
	if strings.Contains(epubManifestBody, root) || strings.Contains(epubManifestBody, "filePath") {
		t.Fatalf("epub client manifest leaked file path: %q", epubManifestBody)
	}
	if !strings.Contains(epubManifestBody, `"format":"epub"`) ||
		!strings.Contains(epubManifestBody, `"readerModes":["single"]`) ||
		!strings.Contains(epubManifestBody, `"defaultReaderMode":"single"`) ||
		!strings.Contains(epubManifestBody, `"resourceBaseUrl":"/api/books/`+itoa(epubBookID)+`/epub/resources/"`) {
		t.Fatalf("epub client manifest response %q is missing reader modes or epub open data", epubManifestBody)
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
		{LibraryID: lib.ID, Title: "srmp7", Platform: "arcade", ROMSetName: "FBNeo", Region: "World", Format: "zip", FilePath: "/library/arcade/srmp7.zip", RelPath: "arcade/srmp7.zip", Size: 4096, MTime: time.Unix(33, 0), CRC32: "33333333", SHA1: "3333333333333333333333333333333333333333", EmulatorHint: "arcade", Compatibility: "unknown"},
		{LibraryID: lib.ID, Title: "Hidden Uncurated", Platform: "arcade", ROMSetName: "unknown", Region: "World", Format: "zip", FilePath: "/library/arcade/unknown.zip", RelPath: "arcade/unknown.zip", Size: 512, MTime: time.Unix(34, 0), CRC32: "44444444", SHA1: "4444444444444444444444444444444444444444", EmulatorHint: "arcade", Compatibility: "unknown", CatalogRole: "needs-curation"},
		{LibraryID: lib.ID, Title: "Neo Geo BIOS", Platform: "neogeo", ROMSetName: "neogeo", Region: "World", Format: "zip", FilePath: "/library/arcade/neogeo.zip", RelPath: "arcade/neogeo.zip", Size: 256, MTime: time.Unix(35, 0), CRC32: "55555555", SHA1: "5555555555555555555555555555555555555555", EmulatorHint: "fbneo", Compatibility: "unknown", CatalogRole: "dependency"},
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
	if !strings.Contains(body, `"total":5`) || !strings.Contains(body, `"limit":2`) || !strings.Contains(body, `"hasMore":true`) || !strings.Contains(body, `"title":"Advance Wars"`) {
		t.Fatalf("client games page %q missing pagination metadata or title sort", body)
	}
	uncuratedBody := authGet(t, ts.URL+"/api/client/games?q=Hidden%20Uncurated", "secret")
	if !strings.Contains(uncuratedBody, `"title":"Hidden Uncurated"`) || !strings.Contains(uncuratedBody, `"catalogRole":"needs-curation"`) {
		t.Fatalf("client games page %q did not preserve discoverable needs-curation entry", uncuratedBody)
	}
	if strings.Contains(authGet(t, ts.URL+"/api/client/games?q=Neo%20Geo%20BIOS", "secret"), "Neo Geo BIOS") {
		t.Fatalf("client games exposed a dependency entry")
	}
	if !strings.Contains(body, `"/api/client/games/`) || !strings.Contains(body, `/manifest"`) {
		t.Fatalf("client games page %q missing manifestUrl", body)
	}

	updatedState := authPut(t, ts.URL+"/api/client/games/2/private-state", "secret", `{"favorite":true,"liked":true}`)
	if !strings.Contains(updatedState, `"favorite":true`) || !strings.Contains(updatedState, `"liked":true`) {
		t.Fatalf("game private-state response %q missing favorite and liked", updatedState)
	}
	platformBody := authGet(t, ts.URL+"/api/client/games?limit=20&sort=platform", "secret")
	if strings.Index(platformBody, `"platform":"arcade"`) > strings.Index(platformBody, `"platform":"gba"`) ||
		strings.Index(platformBody, `"platform":"gba"`) > strings.Index(platformBody, `"platform":"nes"`) {
		t.Fatalf("client games page %q is not platform ordered", platformBody)
	}
	if !strings.Contains(platformBody, `"title":"Advance Wars"`) || !strings.Contains(platformBody, `"favorite":true`) || !strings.Contains(platformBody, `"liked":true`) {
		t.Fatalf("client games page %q missing saved private state", platformBody)
	}
	mahjongBody := authGet(t, ts.URL+"/api/client/games?q=srmp7", "secret")
	if !strings.Contains(mahjongBody, `"title":"srmp7"`) || !strings.Contains(mahjongBody, `"inputProfile":"mahjong"`) {
		t.Fatalf("client games page %q missing mahjong input profile for srmp7", mahjongBody)
	}

	oldestBody := authGet(t, ts.URL+"/api/client/games?limit=20&sort=oldest", "secret")
	if strings.Index(oldestBody, `"title":"Super Contra"`) > strings.Index(oldestBody, `"title":"Advance Wars"`) ||
		strings.Index(oldestBody, `"title":"Advance Wars"`) > strings.Index(oldestBody, `"title":"Metal Slug"`) {
		t.Fatalf("client games page %q is not oldest ordered", oldestBody)
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

func TestAPIClientGameFacetsUseFullCatalog(t *testing.T) {
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
		{LibraryID: lib.ID, Title: "SNES A", Platform: "snes", ROMSetName: "SNES", Format: "sfc", FilePath: "/library/snes/a.sfc", RelPath: "snes/a.sfc", Size: 1, MTime: time.Unix(30, 0), Compatibility: "unknown"},
		{LibraryID: lib.ID, Title: "SNES B", Platform: "snes", ROMSetName: "No-Intro", Format: "smc", FilePath: "/library/snes/b.smc", RelPath: "snes/b.smc", Size: 1, MTime: time.Unix(31, 0), Compatibility: "unknown"},
		{LibraryID: lib.ID, Title: "Arcade C", Platform: "arcade", ROMSetName: "MAME", Format: "zip", FilePath: "/library/arcade/c.zip", RelPath: "arcade/c.zip", Size: 1, MTime: time.Unix(32, 0), Compatibility: "unknown"},
		{LibraryID: lib.ID, Title: "Dreamcast D", Platform: "dreamcast", ROMSetName: "DC", Format: "gdi", FilePath: "/library/dc/d.gdi", RelPath: "dc/d.gdi", Size: 1, MTime: time.Unix(33, 0), EmulatorHint: "dreamcast", Compatibility: "unknown"},
		{LibraryID: lib.ID, Title: "Legacy Disc E", Platform: "disc", ROMSetName: "Legacy", Format: "chd", FilePath: "/library/legacy/e.chd", RelPath: "legacy/e.chd", Size: 1, MTime: time.Unix(34, 0), EmulatorHint: "disc", Compatibility: "unknown"},
	} {
		if _, err := st.UpsertGame(game); err != nil {
			t.Fatal(err)
		}
	}
	ts := httptest.NewServer(NewWithOptions(service.New(st), nil, Options{APIToken: "secret"}).Routes())
	defer ts.Close()

	firstPage := authGet(t, ts.URL+"/api/client/games?limit=1&sort=title", "secret")
	if !strings.Contains(firstPage, `"total":5`) || strings.Count(firstPage, `"assetType":"game"`) != 1 {
		t.Fatalf("first page = %q, want one item from a five-game catalog", firstPage)
	}

	facets := authGet(t, ts.URL+"/api/client/games/facets", "secret")
	for _, want := range []string{
		`"total":5`,
		`"platform":"arcade"`,
		`"count":1`,
		`"platform":"disc"`,
		`"platform":"dreamcast"`,
		`"platform":"snes"`,
		`"count":2`,
	} {
		if !strings.Contains(facets, want) {
			t.Fatalf("facets = %q, missing %s", facets, want)
		}
	}
	if strings.Count(facets, `"platform":"snes"`) != 1 {
		t.Fatalf("facets = %q, want exactly one aggregate per platform", facets)
	}
	dreamcast := authGet(t, ts.URL+"/api/client/games?platform=dreamcast", "secret")
	if !strings.Contains(dreamcast, `"total":1`) || !strings.Contains(dreamcast, `"title":"Dreamcast D"`) || strings.Contains(dreamcast, `"title":"Legacy Disc E"`) {
		t.Fatalf("dreamcast page = %q, want one canonical Dreamcast launch record", dreamcast)
	}
	legacyDisc := authGet(t, ts.URL+"/api/client/games?platform=disc", "secret")
	if !strings.Contains(legacyDisc, `"total":1`) || !strings.Contains(legacyDisc, `"title":"Legacy Disc E"`) {
		t.Fatalf("legacy disc page = %q, want old platform query compatibility", legacyDisc)
	}
}

func TestAPIClientGamePlatformsDeclareSupportedCatalog(t *testing.T) {
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
		{LibraryID: lib.ID, Title: "SNES A", Platform: "snes", Format: "sfc", FilePath: "/library/snes/a.sfc", RelPath: "snes/a.sfc", Size: 1, MTime: time.Unix(40, 0)},
		{LibraryID: lib.ID, Title: "Future A", Platform: "futurebox", Format: "rom", FilePath: "/library/future/a.rom", RelPath: "future/a.rom", Size: 1, MTime: time.Unix(41, 0)},
	} {
		if _, err := st.UpsertGame(game); err != nil {
			t.Fatal(err)
		}
	}
	ts := httptest.NewServer(NewWithOptions(service.New(st), nil, Options{APIToken: "secret"}).Routes())
	defer ts.Close()

	platforms := authGet(t, ts.URL+"/api/client/games/platforms", "secret")
	for _, want := range []string{
		`"platform":"snes","title":"SNES","aliases":["sfc","super-nintendo"],"count":1,"available":true`,
		`"platform":"virtualboy","title":"Virtual Boy"`,
		`"platform":"ps2","title":"PlayStation 2"`,
		`"platform":"futurebox","title":"Futurebox","count":1,"available":true`,
	} {
		if !strings.Contains(platforms, want) {
			t.Fatalf("platform catalog = %q, missing %s", platforms, want)
		}
	}
	if !strings.Contains(platforms, `"platform":"virtualboy","title":"Virtual Boy","aliases":["virtual-boy","virtual boy"],"count":0,"available":false`) {
		t.Fatalf("platform catalog = %q, want unavailable declared Virtual Boy entry", platforms)
	}

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/client/games/platforms", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST game platforms status = %d, want 405", response.StatusCode)
	}
}

func TestClientModel2GameUsesOperatorProfileAndDependencyRole(t *testing.T) {
	game := clientGameItem(domain.GameAsset{
		ID: 42, Title: "Virtua Fighter 2", Platform: "model2", ROMSetName: "Model2ROMs",
		Format: "zip", EmulatorHint: "model2", Compatibility: "untested", CatalogRole: "game",
	})
	if game.InputProfile != "operatorArcade" || game.CatalogRole != "game" {
		t.Fatalf("game = %#v, want Model 2 operator profile", game)
	}
	dependency := clientGameItem(domain.GameAsset{
		ID: 43, Title: "segabill", Platform: "model2", ROMSetName: "Model2ROMs",
		Format: "zip", EmulatorHint: "model2", Compatibility: "unknown", CatalogRole: "dependency",
	})
	if dependency.InputProfile != "" || dependency.CatalogRole != "dependency" {
		t.Fatalf("dependency = %#v, want hidden dependency without launch input profile", dependency)
	}
}

func TestClientNaomi2GameUsesOperatorProfileAndDependencyRole(t *testing.T) {
	game := clientGameItem(domain.GameAsset{
		ID: 44, Title: "Virtua Fighter 4 (Ver. C)", Platform: "naomi2", ROMSetName: "vf4",
		Format: "zip", EmulatorHint: "flycast", Compatibility: "playable", CatalogRole: "game",
	})
	if game.InputProfile != "operatorArcade" || game.CatalogRole != "game" {
		t.Fatalf("game = %#v, want NAOMI 2 operator profile", game)
	}
	dependency := clientGameItem(domain.GameAsset{
		ID: 45, Title: "gds-0012c", Platform: "naomi2", ROMSetName: "gds-0012c",
		Format: "chd", EmulatorHint: "flycast", Compatibility: "unknown", CatalogRole: "dependency",
	})
	if dependency.InputProfile != "" || dependency.CatalogRole != "dependency" {
		t.Fatalf("dependency = %#v, want hidden NAOMI 2 dependency without launch input profile", dependency)
	}
}

func TestClientModernDiscGamesUseStandardInputProfile(t *testing.T) {
	for _, game := range []domain.GameAsset{
		{ID: 46, Title: "PSP Game", Platform: "psp", ROMSetName: "PSP", Format: "cso", EmulatorHint: "ppsspp"},
		{ID: 47, Title: "GameCube Game", Platform: "ngc", ROMSetName: "NGC", Format: "rvz", EmulatorHint: "dolphin"},
		{ID: 48, Title: "PS2 Game", Platform: "ps2", ROMSetName: "PS2", Format: "iso", EmulatorHint: "pcsx2"},
	} {
		if item := clientGameItem(game); item.InputProfile != "standard" {
			t.Fatalf("game = %#v, want standard input profile", item)
		}
	}
}

func TestAPIClientN64ZIPCatalogManifestAndDownload(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "F-Zero X.zip")
	rom := append([]byte{0x40, 0x12, 0x37, 0x80}, []byte("n64-rom-body")...)
	makeZip(t, zipPath, map[string]string{
		"README.txt":       "notes",
		"ROM/F-Zero X.n64": string(rom),
	})

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("N64", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	game, err := st.UpsertGame(domain.GameAsset{
		LibraryID: lib.ID, Title: "F-Zero X", Platform: "n64", ROMSetName: "Nintendo 64", Format: "n64",
		FilePath: zipPath, RelPath: "F-Zero X.n64", Size: int64(len(rom)), MTime: time.Now(),
		CRC32: "12345678", SHA1: "1234567890123456789012345678901234567890", EmulatorHint: "mupen64plus", Compatibility: "untested",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceGameFiles(game.ID, []domain.GameFile{{
		Name: "F-Zero X.n64", FilePath: zipPath, Size: int64(len(rom)), MTime: time.Now(), Role: "entry", Position: 0,
	}}); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(NewWithOptions(service.New(st), nil, Options{APIToken: "secret"}).Routes())
	defer ts.Close()

	catalog := authGet(t, ts.URL+"/api/client/games?platform=n64", "secret")
	for _, want := range []string{
		`"total":1`, `"platform":"n64"`, `"romSetName":"Nintendo 64"`, `"format":"n64"`,
		`"fileName":"F-Zero X.n64"`, `"emulatorHint":"mupen64plus"`, `"inputProfile":"standard"`,
		`"compatibility":"untested"`, `"downloadUrl":"/api/client/games/`,
	} {
		if !strings.Contains(catalog, want) {
			t.Fatalf("N64 catalog = %q, missing %s", catalog, want)
		}
	}
	if strings.Contains(catalog, root) || strings.Contains(catalog, "filePath") || strings.Contains(catalog, "relPath") {
		t.Fatalf("N64 catalog leaked internal path: %q", catalog)
	}

	facets := authGet(t, ts.URL+"/api/client/games/facets?platform=n64", "secret")
	for _, want := range []string{`"total":1`, `"platform":"n64"`, `"title":"Nintendo 64"`, `"count":1`} {
		if !strings.Contains(facets, want) {
			t.Fatalf("N64 facets = %q, missing %s", facets, want)
		}
	}

	manifest := authGet(t, ts.URL+"/api/client/games/"+itoa(game.ID)+"/manifest", "secret")
	for _, want := range []string{`"entryFile":"F-Zero X.n64"`, `"name":"F-Zero X.n64"`, `"role":"entry"`} {
		if !strings.Contains(manifest, want) {
			t.Fatalf("N64 manifest = %q, missing %s", manifest, want)
		}
	}

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/client/games/"+itoa(game.ID)+"/file", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || !bytes.Equal(got, rom) {
		t.Fatalf("N64 download status=%d data=%x, want raw ROM", resp.StatusCode, got)
	}
	if resp.ContentLength != int64(len(rom)) || resp.Header.Get("Content-Disposition") != `attachment; filename="F-Zero X.n64"` {
		t.Fatalf("N64 download headers length=%d disposition=%q", resp.ContentLength, resp.Header.Get("Content-Disposition"))
	}
}

func TestAPIClientPC98ZIPCatalogManifestAndDownload(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "Love Escalator.zip")
	media := syntheticPC98AnexImageForAPI(512, 4, 2, 10)
	makeZip(t, zipPath, map[string]string{
		"README.txt":                   "notes",
		"GAME.PC98/Love Escalator.hdi": string(media),
	})

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("PC-98", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	game, err := st.UpsertGame(domain.GameAsset{
		LibraryID: lib.ID, Title: "Love Escalator", Platform: "pc98", ROMSetName: "PC-98", Format: "hdi",
		FilePath: zipPath, RelPath: "Love Escalator.hdi", Size: int64(len(media)), MTime: time.Now(),
		CRC32: "12345678", SHA1: "1234567890123456789012345678901234567890", EmulatorHint: "np2kai", Compatibility: "untested",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceGameFiles(game.ID, []domain.GameFile{{
		Name: "Love Escalator.hdi", FilePath: zipPath, Size: int64(len(media)), MTime: time.Now(), Role: "entry", Position: 0,
	}}); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(NewWithOptions(service.New(st), nil, Options{APIToken: "secret"}).Routes())
	defer ts.Close()

	catalog := authGet(t, ts.URL+"/api/client/games?platform=pc98", "secret")
	for _, want := range []string{
		`"total":1`, `"platform":"pc98"`, `"romSetName":"PC-98"`, `"format":"hdi"`,
		`"fileName":"Love Escalator.hdi"`, `"emulatorHint":"np2kai"`, `"inputProfile":"standard"`,
		`"compatibility":"untested"`, `"downloadUrl":"/api/client/games/`,
	} {
		if !strings.Contains(catalog, want) {
			t.Fatalf("PC-98 catalog = %q, missing %s", catalog, want)
		}
	}
	if strings.Contains(catalog, root) || strings.Contains(catalog, "filePath") || strings.Contains(catalog, "relPath") {
		t.Fatalf("PC-98 catalog leaked internal path: %q", catalog)
	}

	facets := authGet(t, ts.URL+"/api/client/games/facets?platform=pc98", "secret")
	for _, want := range []string{`"total":1`, `"platform":"pc98"`, `"title":"NEC PC-98"`, `"romSetName":"PC-98"`, `"emulatorHint":"np2kai"`, `"count":1`} {
		if !strings.Contains(facets, want) {
			t.Fatalf("PC-98 facets = %q, missing %s", facets, want)
		}
	}

	manifest := authGet(t, ts.URL+"/api/client/games/"+itoa(game.ID)+"/manifest", "secret")
	for _, want := range []string{`"entryFile":"Love Escalator.hdi"`, `"name":"Love Escalator.hdi"`, `"role":"entry"`} {
		if !strings.Contains(manifest, want) {
			t.Fatalf("PC-98 manifest = %q, missing %s", manifest, want)
		}
	}
	if strings.Contains(manifest, `"diskIndex"`) || strings.Contains(manifest, `"driveHint"`) || strings.Contains(manifest, `"label":"Disk`) {
		t.Fatalf("PC-98 hard-disk manifest must not expose floppy metadata: %q", manifest)
	}

	for _, endpoint := range []string{"/file", "/files/0"} {
		req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/client/games/"+itoa(game.ID)+endpoint, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer secret")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		got, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if resp.StatusCode != http.StatusOK || !bytes.Equal(got, media) {
			t.Fatalf("PC-98 download %s status=%d size=%d, want raw media", endpoint, resp.StatusCode, len(got))
		}
		if resp.ContentLength != int64(len(media)) || resp.Header.Get("Content-Disposition") != `attachment; filename="Love Escalator.hdi"` {
			t.Fatalf("PC-98 download %s headers length=%d disposition=%q", endpoint, resp.ContentLength, resp.Header.Get("Content-Disposition"))
		}
	}
}

func TestAPIClientPC98ManifestPublishesOrderedDisksAndFont(t *testing.T) {
	root := t.TempDir()
	filesOnDisk := []struct {
		name string
		role string
		body []byte
	}{
		{name: "Disk A.fdi", role: "entry", body: []byte("disk-a")},
		{name: "Disk B.fdi", role: "dependency", body: []byte("disk-b")},
		{name: "FONT.bmp", role: "font", body: []byte("font")},
	}
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("PC-98", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	game, err := st.UpsertGame(domain.GameAsset{
		LibraryID: lib.ID, Title: "Example Multi-Disk Game", Platform: "pc98", ROMSetName: "PC-98", Format: "fdi",
		FilePath: filepath.Join(root, filesOnDisk[0].name), RelPath: filesOnDisk[0].name, Size: 18, MTime: time.Now(),
		EmulatorHint: "np2kai", Compatibility: "untested",
	})
	if err != nil {
		t.Fatal(err)
	}
	gameFiles := make([]domain.GameFile, 0, len(filesOnDisk))
	for position, fixture := range filesOnDisk {
		path := filepath.Join(root, fixture.name)
		if err := os.WriteFile(path, fixture.body, 0o644); err != nil {
			t.Fatal(err)
		}
		gameFiles = append(gameFiles, domain.GameFile{
			Name: fixture.name, FilePath: path, Size: int64(len(fixture.body)), MTime: time.Now(), Role: fixture.role, Position: position,
		})
	}
	if err := st.ReplaceGameFiles(game.ID, gameFiles); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(NewWithOptions(service.New(st), nil, Options{APIToken: "secret"}).Routes())
	defer ts.Close()
	manifest := authGet(t, ts.URL+"/api/client/games/"+itoa(game.ID)+"/manifest", "secret")
	for _, want := range []string{
		`"entryFile":"Disk A.fdi"`,
		`"name":"Disk A.fdi","size":6,"role":"entry","label":"Disk 1","diskIndex":0,"driveHint":"FDD1"`,
		`"name":"Disk B.fdi","size":6,"role":"disk","label":"Disk 2","diskIndex":1`,
		`"name":"FONT.bmp","size":4,"role":"font"`,
	} {
		if !strings.Contains(manifest, want) {
			t.Fatalf("manifest = %q, missing %s", manifest, want)
		}
	}
	fontStart := strings.Index(manifest, `"name":"FONT.bmp"`)
	if fontStart < 0 {
		t.Fatal("font missing from manifest")
	}
	fontFragment := manifest[fontStart:]
	if end := strings.Index(fontFragment, "}"); end >= 0 {
		fontFragment = fontFragment[:end]
	}
	if strings.Contains(fontFragment, "diskIndex") || strings.Contains(fontFragment, "driveHint") {
		t.Fatalf("font manifest entry must not expose disk metadata: %q", fontFragment)
	}
}

func syntheticPC98AnexImageForAPI(sectorSize, sectors, surfaces, cylinders uint32) []byte {
	const headerSize = uint32(4096)
	dataSize := sectorSize * sectors * surfaces * cylinders
	image := make([]byte, int(headerSize+dataSize))
	binary.LittleEndian.PutUint32(image[8:12], headerSize)
	binary.LittleEndian.PutUint32(image[12:16], dataSize)
	binary.LittleEndian.PutUint32(image[16:20], sectorSize)
	binary.LittleEndian.PutUint32(image[20:24], sectors)
	binary.LittleEndian.PutUint32(image[24:28], surfaces)
	binary.LittleEndian.PutUint32(image[28:32], cylinders)
	for index := int(headerSize); index < len(image); index++ {
		image[index] = byte(index)
	}
	return image
}

func TestClientGameItemNormalizesN64FileNameFromDetectedFormat(t *testing.T) {
	item := clientGameItem(domain.GameAsset{
		Platform: "n64",
		Format:   "v64",
		RelPath:  "N64/Yoshi Story.n64",
	})
	if item.FileName != "Yoshi Story.v64" {
		t.Fatalf("fileName = %q, want header-detected extension", item.FileName)
	}
}

func TestAPIClientDreamcastManifestListsCompleteGDISet(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"game.gdi":    "2\n1 0 4 2352 track01.bin 0\n2 45000 4 2352 track03.bin 0\n",
		"track01.bin": "one",
		"track03.bin": "three",
		"legacy.bin":  "legacy-single-file",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("DC", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	game, err := st.UpsertGame(domain.GameAsset{
		LibraryID: lib.ID, Title: "Game", Platform: "dreamcast", ROMSetName: "DC", Format: "gdi",
		FilePath: filepath.Join(root, "game.gdi"), RelPath: "game.gdi", Size: 100, MTime: time.Unix(30, 0),
		EmulatorHint: "dreamcast", Compatibility: "unknown",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceGameFiles(game.ID, []domain.GameFile{
		{GameID: game.ID, Name: "game.gdi", FilePath: filepath.Join(root, "game.gdi"), Size: 64, MTime: time.Unix(30, 0), Role: "entry", Position: 0},
		{GameID: game.ID, Name: "track01.bin", FilePath: filepath.Join(root, "track01.bin"), Size: 3, MTime: time.Unix(30, 0), Role: "dependency", Position: 1},
		{GameID: game.ID, Name: "track03.bin", FilePath: filepath.Join(root, "track03.bin"), Size: 5, MTime: time.Unix(30, 0), Role: "dependency", Position: 2},
	}); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(NewWithOptions(service.New(st), nil, Options{APIToken: "secret"}).Routes())
	defer ts.Close()

	manifest := authGet(t, ts.URL+"/api/client/games/"+itoa(game.ID)+"/manifest", "secret")
	for _, want := range []string{`"entryFile":"game.gdi"`, `"name":"track01.bin"`, `"role":"dependency"`, `/files/2`} {
		if !strings.Contains(manifest, want) {
			t.Fatalf("manifest = %q, missing %s", manifest, want)
		}
	}
	track := authGet(t, ts.URL+"/api/client/games/"+itoa(game.ID)+"/files/2", "secret")
	if track != "three" {
		t.Fatalf("track download = %q, want complete dependency bytes", track)
	}

	legacyInfo, err := os.Stat(filepath.Join(root, "legacy.bin"))
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := st.UpsertGame(domain.GameAsset{
		LibraryID: lib.ID, Title: "Legacy", Platform: "disc", Format: "bin",
		FilePath: filepath.Join(root, "legacy.bin"), RelPath: "legacy.bin", Size: legacyInfo.Size(), MTime: legacyInfo.ModTime(),
		EmulatorHint: "disc", Compatibility: "unknown",
	})
	if err != nil {
		t.Fatal(err)
	}
	legacyManifest := authGet(t, ts.URL+"/api/client/games/"+itoa(legacy.ID)+"/manifest", "secret")
	if !strings.Contains(legacyManifest, `"entryFile":"legacy.bin"`) || !strings.Contains(legacyManifest, `/files/0`) {
		t.Fatalf("legacy manifest = %q, want synthesized single-file entry", legacyManifest)
	}
	legacyBody := authGet(t, ts.URL+"/api/client/games/"+itoa(legacy.ID)+"/files/0", "secret")
	if legacyBody != "legacy-single-file" {
		t.Fatalf("legacy file download = %q, want fallback bytes", legacyBody)
	}
}

func TestAPIClientSaturnManifestListsCompleteCUESet(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"guardian.cue": `FILE "C:\SATURN\track01.bin" BINARY
  TRACK 01 MODE1/2352
FILE "track02.wav" WAVE
  TRACK 02 AUDIO
`,
		"track01.bin": "data-track",
		"track02.wav": "audio-track",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("Saturn", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	game, err := st.UpsertGame(domain.GameAsset{
		LibraryID: lib.ID, Title: "Guardian Heroes", Platform: "saturn", ROMSetName: "SS", Format: "cue",
		FilePath: filepath.Join(root, "guardian.cue"), RelPath: "guardian.cue", Size: 100, MTime: time.Unix(30, 0),
		EmulatorHint: "saturn", Compatibility: "unknown",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceGameFiles(game.ID, []domain.GameFile{
		{GameID: game.ID, Name: "guardian.cue", FilePath: filepath.Join(root, "guardian.cue"), Size: 100, MTime: time.Unix(30, 0), Role: "entry", Position: 0},
		{GameID: game.ID, Name: "track01.bin", FilePath: filepath.Join(root, "track01.bin"), Size: 10, MTime: time.Unix(30, 0), Role: "dependency", Position: 1},
		{GameID: game.ID, Name: "track02.wav", FilePath: filepath.Join(root, "track02.wav"), Size: 11, MTime: time.Unix(30, 0), Role: "dependency", Position: 2},
	}); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(NewWithOptions(service.New(st), nil, Options{APIToken: "secret"}).Routes())
	defer ts.Close()

	manifest := authGet(t, ts.URL+"/api/client/games/"+itoa(game.ID)+"/manifest", "secret")
	for _, want := range []string{`"entryFile":"guardian.cue"`, `"name":"track01.bin"`, `"name":"track02.wav"`, `"role":"dependency"`, `/files/2`} {
		if !strings.Contains(manifest, want) {
			t.Fatalf("manifest = %q, missing %s", manifest, want)
		}
	}
	track := authGet(t, ts.URL+"/api/client/games/"+itoa(game.ID)+"/files/2", "secret")
	if track != "audio-track" {
		t.Fatalf("track download = %q, want complete CUE dependency bytes", track)
	}
	descriptor := authGet(t, ts.URL+"/api/client/games/"+itoa(game.ID)+"/files/0", "secret")
	if strings.Contains(descriptor, `C:\SATURN`) || !strings.Contains(descriptor, `FILE "track01.bin" BINARY`) {
		t.Fatalf("descriptor download = %q, want normalized relative CUE references", descriptor)
	}
}

func TestAPIClientPCFXManifestStreamsVirtualM3UAndCasePreservedTracks(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"Last Imperial Prince - Disc A.cue": `FILE "disc a.BIN" BINARY`,
		"Last Imperial Prince - Disc A.bin": "disc-a",
		"Last Imperial Prince - Disc B.cue": `FILE "disc b.BIN" BINARY`,
		"Last Imperial Prince - Disc B.bin": "disc-b",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("PC-FX", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	game, err := st.UpsertGame(domain.GameAsset{LibraryID: lib.ID, Title: "王子最终传承", Platform: "pc-fx", ROMSetName: "PC-FX", Format: "m3u", FilePath: filepath.Join(root, "Last Imperial Prince - Disc A.cue"), RelPath: "Last Imperial Prince - Disc A.cue", Size: 80, MTime: time.Unix(30, 0), CRC32: "crc", SHA1: "sha", EmulatorHint: "pcfx", Compatibility: "unknown"})
	if err != nil {
		t.Fatal(err)
	}
	files := []domain.GameFile{
		{GameID: game.ID, Name: "Last Imperial Prince.m3u", FilePath: game.FilePath, Size: 76, MTime: time.Unix(30, 0), Role: "entry", Position: 0},
		{GameID: game.ID, Name: "Last Imperial Prince - Disc A.cue", FilePath: filepath.Join(root, "Last Imperial Prince - Disc A.cue"), Size: 24, MTime: time.Unix(30, 0), Role: "dependency", Position: 1},
		{GameID: game.ID, Name: "disc a.BIN", FilePath: filepath.Join(root, "Last Imperial Prince - Disc A.bin"), Size: 6, MTime: time.Unix(30, 0), Role: "dependency", Position: 2},
		{GameID: game.ID, Name: "Last Imperial Prince - Disc B.cue", FilePath: filepath.Join(root, "Last Imperial Prince - Disc B.cue"), Size: 24, MTime: time.Unix(30, 0), Role: "dependency", Position: 3},
		{GameID: game.ID, Name: "disc b.BIN", FilePath: filepath.Join(root, "Last Imperial Prince - Disc B.bin"), Size: 6, MTime: time.Unix(30, 0), Role: "dependency", Position: 4},
	}
	if err := st.ReplaceGameFiles(game.ID, files); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(NewWithOptions(service.New(st), nil, Options{APIToken: "secret"}).Routes())
	defer ts.Close()

	manifest := authGet(t, ts.URL+"/api/client/games/"+itoa(game.ID)+"/manifest", "secret")
	for _, want := range []string{`"platform":"pc-fx"`, `"romSetName":"PC-FX"`, `"emulatorHint":"pcfx"`, `"entryFile":"Last Imperial Prince.m3u"`, `"name":"disc a.BIN"`, `"name":"disc b.BIN"`} {
		if !strings.Contains(manifest, want) {
			t.Fatalf("manifest = %q, missing %s", manifest, want)
		}
	}
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/client/games/"+itoa(game.ID)+"/files/0", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	wantM3U := "Last Imperial Prince - Disc A.cue\nLast Imperial Prince - Disc B.cue\n"
	if string(body) != wantM3U || resp.Header.Get("Content-Length") != strconv.Itoa(len(wantM3U)) {
		t.Fatalf("M3U body=%q Content-Length=%q, want %q and %d", body, resp.Header.Get("Content-Length"), wantM3U, len(wantM3U))
	}
}

func TestAPIClientGameSaveSyncArchiveUploadDownload(t *testing.T) {
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
		Title:         "Super Contra",
		Platform:      "nes",
		ROMSetName:    "NES",
		Region:        "Japan",
		Format:        "nes",
		FilePath:      "/library/nes/Super Contra.nes",
		RelPath:       "nes/Super Contra.nes",
		Size:          262160,
		MTime:         time.Unix(30, 0),
		CRC32:         "9bb6059e",
		SHA1:          "5de393e3ad83e6e185e6d338684d7a4475b7d2ce",
		EmulatorHint:  "nes",
		Compatibility: "unknown",
	})
	if err != nil {
		t.Fatal(err)
	}
	configDir := t.TempDir()
	ts := httptest.NewServer(NewWithOptions(service.NewWithConfig(st, configDir), nil, Options{APIToken: "secret"}).Routes())
	defer ts.Close()

	archiveData := []byte(`{"schemaVersion":1,"records":[],"files":[]}`)
	path := ts.URL + "/api/client/games/" + itoa(game.ID) + "/save-sync/archive"

	unauthorizedReq, err := http.NewRequest(http.MethodGet, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	unauthorizedResp, err := http.DefaultClient.Do(unauthorizedReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = unauthorizedResp.Body.Close()
	if unauthorizedResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized save archive status = %d, want 401", unauthorizedResp.StatusCode)
	}

	putReq, err := http.NewRequest(http.MethodPut, path, bytes.NewReader(archiveData))
	if err != nil {
		t.Fatal(err)
	}
	putReq.Header.Set("Authorization", "Bearer secret")
	putReq.Header.Set("Content-Type", "application/vnd.gameemu.save-sync+json")
	putResp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatal(err)
	}
	putBody, err := io.ReadAll(putResp.Body)
	_ = putResp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if putResp.StatusCode != http.StatusOK {
		t.Fatalf("save archive PUT status = %d body=%s, want 200", putResp.StatusCode, putBody)
	}
	if !strings.Contains(string(putBody), `"ok":true`) {
		t.Fatalf("save archive PUT body = %q, want ok", putBody)
	}

	emptyReq, err := http.NewRequest(http.MethodPut, path, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	emptyReq.Header.Set("Authorization", "Bearer secret")
	emptyResp, err := http.DefaultClient.Do(emptyReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = emptyResp.Body.Close()
	if emptyResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty save archive status = %d, want 400", emptyResp.StatusCode)
	}

	getReq, err := http.NewRequest(http.MethodGet, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	getReq.Header.Set("Authorization", "Bearer secret")
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatal(err)
	}
	gotArchive, err := io.ReadAll(getResp.Body)
	_ = getResp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("save archive GET status = %d body=%s, want 200", getResp.StatusCode, gotArchive)
	}
	if string(gotArchive) != string(archiveData) {
		t.Fatalf("save archive GET body = %q, want %q", gotArchive, archiveData)
	}
	if got := getResp.Header.Get("Content-Type"); got != "application/vnd.gameemu.save-sync+json" {
		t.Fatalf("save archive content type = %q, want save archive type", got)
	}

	missingReq, err := http.NewRequest(http.MethodPut, ts.URL+"/api/client/games/9999/save-sync/archive", bytes.NewReader(archiveData))
	if err != nil {
		t.Fatal(err)
	}
	missingReq.Header.Set("Authorization", "Bearer secret")
	missingResp, err := http.DefaultClient.Do(missingReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = missingResp.Body.Close()
	if missingResp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing game save archive status = %d, want 404", missingResp.StatusCode)
	}
}

func TestAPIClientGamePlayStatsAreProfileScopedAndIdempotent(t *testing.T) {
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
		LibraryID: lib.ID, Title: "Metal Slug", Platform: "neogeo", ROMSetName: "FBNeo",
		Format: "zip", FilePath: "/library/mslug.zip", RelPath: "mslug.zip", Size: 1024,
		MTime: time.Unix(30, 0), EmulatorHint: "neogeo", Compatibility: "unknown",
	})
	if err != nil {
		t.Fatal(err)
	}
	guest, err := st.CreateProfile("Guest", "game", "violet")
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(NewWithOptions(service.New(st), nil, Options{APIToken: "secret"}).Routes())
	defer ts.Close()
	path := ts.URL + "/api/client/games/" + itoa(game.ID) + "/play-stats"

	initial := authGet(t, path, "secret")
	if !strings.Contains(initial, `"totalPlaySeconds":0`) || !strings.Contains(initial, `"launchCount":0`) {
		t.Fatalf("initial play stats = %q, want zero values", initial)
	}

	first := authPut(t, path, "secret", `{"sessionId":"launch-1","startedAt":"2026-07-22T10:00:00Z","elapsedSeconds":10}`)
	if !strings.Contains(first, `"totalPlaySeconds":10`) || !strings.Contains(first, `"launchCount":1`) || !strings.Contains(first, `"sessionPlaySeconds":10`) {
		t.Fatalf("first play report = %q, want 10 seconds and one launch", first)
	}
	duplicate := authPut(t, path, "secret", `{"sessionId":"launch-1","elapsedSeconds":10}`)
	if !strings.Contains(duplicate, `"totalPlaySeconds":10`) || !strings.Contains(duplicate, `"launchCount":1`) {
		t.Fatalf("duplicate play report = %q, want unchanged totals", duplicate)
	}
	heartbeat := authPut(t, path, "secret", `{"sessionId":"launch-1","elapsedSeconds":25}`)
	if !strings.Contains(heartbeat, `"totalPlaySeconds":25`) || !strings.Contains(heartbeat, `"launchCount":1`) {
		t.Fatalf("heartbeat play report = %q, want cumulative delta", heartbeat)
	}
	second := authPut(t, path, "secret", `{"sessionId":"launch-2","elapsedSeconds":5,"endedAt":"2026-07-22T11:00:05Z"}`)
	if !strings.Contains(second, `"totalPlaySeconds":30`) || !strings.Contains(second, `"launchCount":2`) || !strings.Contains(second, `"ended":true`) {
		t.Fatalf("second play report = %q, want 30 seconds and two launches", second)
	}

	guestPath := path + "?profileId=" + itoa(guest.ID)
	guestInitial := authGet(t, guestPath, "secret")
	if !strings.Contains(guestInitial, `"totalPlaySeconds":0`) || !strings.Contains(guestInitial, `"launchCount":0`) {
		t.Fatalf("guest initial play stats = %q, want isolated zero values", guestInitial)
	}
	guestReport := authPut(t, guestPath, "secret", `{"sessionId":"guest-1","elapsedSeconds":7}`)
	if !strings.Contains(guestReport, `"totalPlaySeconds":7`) || !strings.Contains(guestReport, `"launchCount":1`) {
		t.Fatalf("guest play report = %q, want isolated values", guestReport)
	}
	defaultStats := authGet(t, path, "secret")
	if !strings.Contains(defaultStats, `"totalPlaySeconds":30`) || !strings.Contains(defaultStats, `"launchCount":2`) {
		t.Fatalf("default stats after guest report = %q, want unchanged values", defaultStats)
	}
	played := authGet(t, ts.URL+"/api/client/games/played?sort=playtime&direction=desc", "secret")
	if !strings.Contains(played, `"gameId":`+itoa(game.ID)) || !strings.Contains(played, `"title":"Metal Slug"`) || !strings.Contains(played, `"totalPlaySeconds":30`) || !strings.Contains(played, `"total":1`) {
		t.Fatalf("played games = %q", played)
	}
	guestPlayed := authGet(t, ts.URL+"/api/client/games/played?profileId="+itoa(guest.ID), "secret")
	if !strings.Contains(guestPlayed, `"totalPlaySeconds":7`) || !strings.Contains(guestPlayed, `"total":1`) {
		t.Fatalf("guest played games = %q", guestPlayed)
	}

	invalidReq, err := http.NewRequest(http.MethodPut, path, strings.NewReader(`{"sessionId":"","elapsedSeconds":1}`))
	if err != nil {
		t.Fatal(err)
	}
	invalidReq.Header.Set("Authorization", "Bearer secret")
	invalidReq.Header.Set("Content-Type", "application/json")
	invalidResp, err := http.DefaultClient.Do(invalidReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = invalidResp.Body.Close()
	if invalidResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid report status = %d, want 400", invalidResp.StatusCode)
	}
}

func TestAPIClientGameDetailsExposeMetadataState(t *testing.T) {
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
		ROMSetName:    "No-Intro",
		Region:        "USA",
		Format:        "sfc",
		FilePath:      "/library/snes/Super Mario World.sfc",
		RelPath:       "snes/Super Mario World.sfc",
		Size:          1024,
		MTime:         time.Unix(30, 0),
		CRC32:         "b19ed489",
		SHA1:          "0123456789abcdef0123456789abcdef01234567",
		EmulatorHint:  "snes",
		Compatibility: "unknown",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertGameMetadata(domain.GameMetadata{
		GameID:       game.ID,
		DisplayTitle: "Super Mario World",
		Summary:      "Dinosaur Land platform adventure.",
		ReleaseDate:  "1990-11-21",
		Genres:       []string{"Platform"},
		Developers:   []string{"Nintendo EAD"},
		Publishers:   []string{"Nintendo"},
		Players:      "1-2",
		Rating:       9.3,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertGameMetadataSource(domain.GameMetadataSource{
		GameID:     game.ID,
		Source:     "gamelist",
		SourceID:   "snes/smw",
		MatchedBy:  "manual",
		Confidence: 1,
		RawJSON:    `{"name":"Super Mario World"}`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertGameArtwork(domain.GameArtwork{
		GameID:     game.ID,
		Source:     "gamelist",
		Kind:       "cover",
		URL:        "/api/games/1/cover",
		Width:      600,
		Height:     800,
		Selected:   true,
		Confidence: 1,
	}); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(NewWithOptions(service.New(st), nil, Options{APIToken: "secret"}).Routes())
	defer ts.Close()

	body := authGet(t, ts.URL+"/api/client/games/"+itoa(game.ID)+"/details", "secret")
	if strings.Contains(body, "/library") || strings.Contains(body, "filePath") || strings.Contains(body, "relPath") {
		t.Fatalf("client game details leaked internal path: %q", body)
	}
	for _, want := range []string{`"metadataStatus":"matched"`, `"displayTitle":"Super Mario World"`, `"source":"gamelist"`, `"kind":"cover"`, `"manifestUrl":"/api/client/games/`} {
		if !strings.Contains(body, want) {
			t.Fatalf("client game details = %q, want %q", body, want)
		}
	}

	metadataBody := authGet(t, ts.URL+"/api/client/games/"+itoa(game.ID)+"/metadata", "secret")
	if !strings.Contains(metadataBody, `"metadataStatus":"matched"`) || !strings.Contains(metadataBody, `"sources"`) || strings.Contains(metadataBody, "/library") {
		t.Fatalf("client game metadata = %q, want safe metadata response", metadataBody)
	}
}

func TestAPIGameGamelistExportReturnsDraftXML(t *testing.T) {
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
		{LibraryID: lib.ID, Title: "Advance Wars", Platform: "gba", ROMSetName: "GBA", Region: "USA", Format: "gba", FilePath: "/library/GBA/Advance Wars.gba", RelPath: "GBA/Advance Wars.gba", Size: 1024, MTime: time.Unix(40, 0), CRC32: "11111111", SHA1: "1111111111111111111111111111111111111111", EmulatorHint: "gba", Compatibility: "unknown"},
		{LibraryID: lib.ID, Title: "Super Mario World", Platform: "snes", ROMSetName: "SNES", Region: "USA", Format: "sfc", FilePath: "/library/SNES/Super Mario World.sfc", RelPath: "SNES/Super Mario World.sfc", Size: 2048, MTime: time.Unix(41, 0), CRC32: "22222222", SHA1: "2222222222222222222222222222222222222222", EmulatorHint: "snes", Compatibility: "unknown"},
	} {
		if _, err := st.UpsertGame(game); err != nil {
			t.Fatal(err)
		}
	}
	ts := httptest.NewServer(NewWithOptions(service.New(st), nil, Options{APIToken: "secret"}).Routes())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/games/gamelist.xml?romSetName=GBA&basePath=GBA", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body = %q, want 200", resp.StatusCode, body)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "application/xml") {
		t.Fatalf("content type = %q, want application/xml", resp.Header.Get("Content-Type"))
	}
	for _, want := range []string{`<?xml version="1.0" encoding="UTF-8"?>`, `<gameList>`, `<path>./Advance Wars.gba</path>`, `<name>Advance Wars</name>`} {
		if !strings.Contains(body, want) {
			t.Fatalf("gamelist export = %q, want %q", body, want)
		}
	}
	if strings.Contains(body, "Super Mario World") || strings.Contains(body, "/library") {
		t.Fatalf("gamelist export = %q, want filtered safe relative paths", body)
	}
}

func TestAPIGameMetadataProvidersReportBuiltInAndCredentialedSources(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	ts := httptest.NewServer(NewWithOptions(service.New(st), nil, Options{APIToken: "secret"}).Routes())
	defer ts.Close()

	body := authGet(t, ts.URL+"/api/games/metadata/providers", "secret")
	for _, want := range []string{
		`"id":"gamelist"`,
		`"enabled":true`,
		`"requiresCredentials":false`,
		`"id":"libretro"`,
		`"id":"igdb"`,
		`"requiresCredentials":true`,
		`"configured":false`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("providers response = %q, want %q", body, want)
		}
	}
	if strings.Contains(body, "secret") {
		t.Fatalf("providers response leaked credential material: %q", body)
	}
}

func TestAPIGameMetadataActionsReturnProviderState(t *testing.T) {
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
		Title:         "Unknown Game",
		Platform:      "gba",
		Format:        "gba",
		FilePath:      "/library/gba/Unknown Game.gba",
		RelPath:       "gba/Unknown Game.gba",
		Size:          1024,
		MTime:         time.Unix(31, 0),
		CRC32:         "11111111",
		SHA1:          "1111111111111111111111111111111111111111",
		EmulatorHint:  "gba",
		Compatibility: "unknown",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertGameMetadataSource(domain.GameMetadataSource{
		GameID:     game.ID,
		Source:     "gamelist",
		SourceID:   "gba/Unknown Game.gba",
		MatchedBy:  "path",
		Confidence: 1,
		RawJSON:    `{"name":"Unknown Game"}`,
	}); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(NewWithOptions(service.New(st), nil, Options{APIToken: "secret"}).Routes())
	defer ts.Close()

	refresh := authPost(t, ts.URL+"/api/games/"+itoa(game.ID)+"/metadata/refresh", "secret", `{}`)
	if !strings.Contains(refresh, `"status":"completed"`) ||
		!strings.Contains(refresh, `"gameId":`+itoa(game.ID)) ||
		!strings.Contains(refresh, `"providers"`) ||
		!strings.Contains(refresh, `"metadataStatus":"matched"`) {
		t.Fatalf("refresh response = %q, want completed provider state", refresh)
	}

	selectMatch := authPost(t, ts.URL+"/api/games/"+itoa(game.ID)+"/metadata/select-match", "secret", `{"source":"gamelist","sourceId":"gba/Unknown Game.gba"}`)
	if !strings.Contains(selectMatch, `"status":"completed"`) ||
		!strings.Contains(selectMatch, `"action":"select-match"`) ||
		!strings.Contains(selectMatch, `"matchedBy":"manual"`) {
		t.Fatalf("select-match response = %q, want completed manual match state", selectMatch)
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

func TestAPIClientManualCollectionsSpanAssetTypes(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	bookLib, err := st.CreateLibraryWithType("Books", "/books", "book")
	if err != nil {
		t.Fatal(err)
	}
	series, err := st.UpsertSeries(bookLib.ID, "Guides", "Guides")
	if err != nil {
		t.Fatal(err)
	}
	book, err := st.UpsertBook(series.ID, "Arcade Guide", "pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertFile(book.ID, bookLib.ID, "/books/Guides/Arcade Guide.pdf", "Guides/Arcade Guide.pdf", 2048, time.Unix(10, 0), ".pdf"); err != nil {
		t.Fatal(err)
	}
	gameLib, err := st.CreateLibraryWithType("Games", "/games", "game")
	if err != nil {
		t.Fatal(err)
	}
	game, err := st.UpsertGame(domain.GameAsset{LibraryID: gameLib.ID, Title: "Metal Slug", Platform: "arcade", ROMSetName: "MAME", Format: "zip", FilePath: "/games/arcade/mslug.zip", RelPath: "arcade/mslug.zip", Size: 1024, MTime: time.Unix(11, 0), CRC32: "22222222", SHA1: "2222222222222222222222222222222222222222", EmulatorHint: "arcade", Compatibility: "unknown"})
	if err != nil {
		t.Fatal(err)
	}
	videoLib, err := st.CreateLibraryWithType("Videos", "/videos", "video")
	if err != nil {
		t.Fatal(err)
	}
	video, err := st.UpsertVideo(domain.VideoAsset{LibraryID: videoLib.ID, Title: "Cabinet Tour", Format: "mp4", FilePath: "/videos/Cabinet Tour.mp4", RelPath: "Cabinet Tour.mp4", Size: 4096, MTime: time.Unix(12, 0), ThumbnailStatus: "placeholder"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(NewWithOptions(service.New(st), nil, Options{APIToken: "secret"}).Routes())
	defer ts.Close()

	created := authPost(t, ts.URL+"/api/client/manual-collections", "secret", `{"name":"Arcade Night","description":"Cross-media picks"}`)
	if !strings.Contains(created, `"name":"Arcade Night"`) || !strings.Contains(created, `"itemCount":0`) {
		t.Fatalf("create manual collection = %q, want empty collection", created)
	}
	for _, body := range []string{
		`{"assetType":"book","assetId":` + itoa(book.ID) + `}`,
		`{"assetType":"game","assetId":` + itoa(game.ID) + `}`,
		`{"assetType":"video","assetId":` + itoa(video.ID) + `}`,
	} {
		added := authPost(t, ts.URL+"/api/client/manual-collections/1/items", "secret", body)
		if !strings.Contains(added, `"itemCount"`) {
			t.Fatalf("add manual collection item = %q, want collection response", added)
		}
	}
	details := authGet(t, ts.URL+"/api/client/manual-collections/1", "secret")
	if strings.Contains(details, "/books/Guides") || strings.Contains(details, "/games/arcade") || strings.Contains(details, "/videos/Cabinet") || strings.Contains(details, "filePath") || strings.Contains(details, "relPath") {
		t.Fatalf("manual collection details leaked internal paths: %q", details)
	}
	if !strings.Contains(details, `"assetType":"book"`) || !strings.Contains(details, `"title":"Arcade Guide"`) ||
		!strings.Contains(details, `"assetType":"game"`) || !strings.Contains(details, `"title":"Metal Slug"`) ||
		!strings.Contains(details, `"assetType":"video"`) || !strings.Contains(details, `"title":"Cabinet Tour"`) {
		t.Fatalf("manual collection details = %q, want resolved book/game/video items", details)
	}
	authDelete(t, ts.URL+"/api/client/manual-collections/1/items/game/"+itoa(game.ID), "secret")
	afterDelete := authGet(t, ts.URL+"/api/client/manual-collections/1", "secret")
	if strings.Contains(afterDelete, `"title":"Metal Slug"`) || !strings.Contains(afterDelete, `"itemCount":2`) {
		t.Fatalf("manual collection after delete = %q, want game removed", afterDelete)
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

func TestAPIProfilesScopeWebReadingStateWithDefaultFallback(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	root := t.TempDir()
	bookPath := filepath.Join(root, "Series A", "Shared Book.cbz")
	makeZip(t, bookPath, map[string]string{"001.jpg": "image"})
	info, err := os.Stat(bookPath)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := st.CreateLibrary("Comics", root)
	if err != nil {
		t.Fatal(err)
	}
	series, err := st.UpsertSeries(lib.ID, "Series A", "Series A")
	if err != nil {
		t.Fatal(err)
	}
	book, err := st.UpsertBook(series.ID, "Shared Book", "cbz")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertFile(book.ID, lib.ID, bookPath, "Series A/Shared Book.cbz", info.Size(), info.ModTime(), ".cbz"); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(New(service.New(st), nil).Routes())
	defer ts.Close()

	profilesBody := get(t, ts.URL+"/api/profiles")
	if !strings.Contains(profilesBody, `"isDefault":true`) || !strings.Contains(profilesBody, `"Default"`) {
		t.Fatalf("profiles body = %q, want default profile", profilesBody)
	}

	var created struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		IsDefault bool   `json:"isDefault"`
	}
	if err := json.Unmarshal([]byte(postJSONBody(t, ts.URL+"/api/profiles", `{"name":"Guest"}`)), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.Name != "Guest" || created.IsDefault {
		t.Fatalf("created profile = %#v, want non-default guest", created)
	}
	profileHeader := itoa(created.ID)

	putJSON(t, ts.URL+"/api/books/"+itoa(book.ID)+"/progress", `{"pageIndex":1,"locator":"default","progressFraction":0.1}`)
	putJSONWithProfile(t, ts.URL+"/api/books/"+itoa(book.ID)+"/progress", `{"pageIndex":7,"locator":"guest","progressFraction":0.7}`, profileHeader)
	putJSON(t, ts.URL+"/api/books/"+itoa(book.ID)+"/private-state", `{"status":"reading","favorite":true,"rating":5,"tags":["default"],"summary":"default note"}`)
	putJSONWithProfile(t, ts.URL+"/api/books/"+itoa(book.ID)+"/private-state", `{"status":"want","favorite":false,"rating":2,"tags":["guest"],"summary":"guest note"}`, profileHeader)

	defaultProgress := get(t, ts.URL+"/api/books/"+itoa(book.ID)+"/progress")
	if !strings.Contains(defaultProgress, `"pageIndex":1`) || !strings.Contains(defaultProgress, `"locator":"default"`) {
		t.Fatalf("default progress = %q, want default profile state", defaultProgress)
	}
	guestProgress := getWithProfile(t, ts.URL+"/api/books/"+itoa(book.ID)+"/progress", profileHeader)
	if !strings.Contains(guestProgress, `"pageIndex":7`) || !strings.Contains(guestProgress, `"locator":"guest"`) {
		t.Fatalf("guest progress = %q, want guest profile state", guestProgress)
	}

	defaultFavorites := get(t, ts.URL+"/api/books/favorites?limit=5")
	if !strings.Contains(defaultFavorites, `"favorite":true`) || !strings.Contains(defaultFavorites, `"default note"`) {
		t.Fatalf("default favorites = %q, want default favorite", defaultFavorites)
	}
	guestFavorites := getWithProfile(t, ts.URL+"/api/books/favorites?limit=5", profileHeader)
	if strings.Contains(guestFavorites, `"favorite":true`) || strings.Contains(guestFavorites, `"default note"`) {
		t.Fatalf("guest favorites = %q, want isolated guest state", guestFavorites)
	}
	guestWant := getWithProfile(t, ts.URL+"/api/books/private-status/want?limit=5", profileHeader)
	if !strings.Contains(guestWant, `"privateStatus":"want"`) || !strings.Contains(guestWant, `"guest note"`) {
		t.Fatalf("guest want shelf = %q, want guest private state", guestWant)
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

func TestLibraryScanAcceptsTargetPath(t *testing.T) {
	root := t.TempDir()
	makeZip(t, filepath.Join(root, "Series A", "target.cbz"), map[string]string{"001.jpg": "image"})
	makeZip(t, filepath.Join(root, "Series B", "other.cbz"), map[string]string{"001.jpg": "image"})

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	ts := httptest.NewServer(New(service.New(st), nil).Routes())
	defer ts.Close()

	body := postJSONBody(t, ts.URL+"/api/libraries", `{"name":"Books","rootPath":"`+root+`"}`)
	if !strings.Contains(body, `"id":`) {
		t.Fatalf("library response = %q", body)
	}
	libs, err := st.ListLibraries()
	if err != nil {
		t.Fatal(err)
	}
	postJSONBody(t, ts.URL+"/api/libraries/"+itoa(libs[0].ID)+"/scan", `{"path":"Series A/target.cbz"}`)
	waitFor(t, func() bool {
		jobs, err := st.ListScanJobs()
		return err == nil && len(jobs) > 0 && jobs[0].Status == "completed"
	})

	series, err := st.ListSeries()
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 || series[0].Title != "Series A" {
		t.Fatalf("series = %#v, want targeted scan to index only Series A", series)
	}
}

func TestLibraryScanAcceptsRecentMode(t *testing.T) {
	root := t.TempDir()
	makeZip(t, filepath.Join(root, "Series A", "old.cbz"), map[string]string{"001.jpg": "image"})
	makeZip(t, filepath.Join(root, "Series B", "new.cbz"), map[string]string{"001.jpg": "image"})
	oldTime := time.Now().Add(-1 * time.Hour)
	newTime := time.Now()
	if err := os.Chtimes(filepath.Join(root, "Series A", "old.cbz"), oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(root, "Series B", "new.cbz"), newTime, newTime); err != nil {
		t.Fatal(err)
	}

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	ts := httptest.NewServer(New(service.New(st), nil).Routes())
	defer ts.Close()

	body := postJSONBody(t, ts.URL+"/api/libraries", `{"name":"Books","rootPath":"`+root+`"}`)
	if !strings.Contains(body, `"id":`) {
		t.Fatalf("library response = %q", body)
	}
	libs, err := st.ListLibraries()
	if err != nil {
		t.Fatal(err)
	}
	postJSONBody(t, ts.URL+"/api/libraries/"+itoa(libs[0].ID)+"/scan", `{"mode":"recent","recentLimit":1}`)
	waitFor(t, func() bool {
		jobs, err := st.ListScanJobs()
		return err == nil && len(jobs) > 0 && jobs[0].Status == "completed"
	})

	if _, err := st.FileIndexByPath(filepath.Join(root, "Series B", "new.cbz")); err != nil {
		t.Fatalf("new file not indexed: %v", err)
	}
	if _, err := st.FileIndexByPath(filepath.Join(root, "Series A", "old.cbz")); err == nil {
		t.Fatalf("old file was indexed, want recent limit to skip it")
	}
}

func TestLibraryScanReturnsExistingRunningTargetJob(t *testing.T) {
	root := t.TempDir()
	targetPath := filepath.Join(root, "Series A")
	makeZip(t, filepath.Join(targetPath, "target.cbz"), map[string]string{"001.jpg": "image"})

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibrary("Books", root)
	if err != nil {
		t.Fatal(err)
	}
	existing, err := st.StartScanJobWithTarget(lib.ID, targetPath)
	if err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(New(service.New(st), nil).Routes())
	defer ts.Close()

	body := postJSONBody(t, ts.URL+"/api/libraries/"+itoa(lib.ID)+"/scan", `{"path":"Series A"}`)
	var got domain.ScanJob
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != existing.ID || got.TargetPath != targetPath {
		t.Fatalf("scan response = %#v, want existing job %#v", got, existing)
	}
	jobs, err := st.ListScanJobs()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("jobs = %#v, want no duplicate job", jobs)
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
	game, err := st.GameByPath(filepath.Join(root, "Arcade", "mslug.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if game.CatalogRole != "needs-curation" {
		t.Fatalf("indexed game role = %q, want needs-curation", game.CatalogRole)
	}
	gamesBody := get(t, ts.URL+"/api/games/recent")
	if !strings.Contains(gamesBody, `"title":"mslug"`) || !strings.Contains(gamesBody, `"catalogRole":"needs-curation"`) {
		t.Fatalf("games response %q did not expose the uncurated game with its audit status", gamesBody)
	}
	if strings.Contains(gamesBody, root) {
		t.Fatalf("games response %q leaked the library root", gamesBody)
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

func getWithProfile(t *testing.T, url string, profileID string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-FolioSpace-Profile-Id", profileID)
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

func authPost(t *testing.T, url string, token string, body string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
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
		t.Fatalf("POST %s status %d: %s", url, resp.StatusCode, data)
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

func authPut(t *testing.T, url string, token string, body string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
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

func authDelete(t *testing.T, url string, token string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, url, nil)
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
		t.Fatalf("DELETE %s status %d: %s", url, resp.StatusCode, data)
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

func putJSONWithProfile(t *testing.T, url string, body string, profileID string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-FolioSpace-Profile-Id", profileID)
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

func TestStaticHTMLDisablesBrowserCache(t *testing.T) {
	static := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<div id=\"root\"></div>"))
	})
	server := New(nil, static)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	server.handleStatic(rr, req)

	if got := rr.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestAccessTokenRequestSetsAuthCookie(t *testing.T) {
	server := NewWithOptions(nil, nil, Options{APIToken: "secret"})
	req := httptest.NewRequest(http.MethodGet, "/api/books/1/epub/resources/chapter.xhtml?access_token=secret", nil)
	rr := httptest.NewRecorder()

	if !server.authorizeAPI(rr, req) {
		t.Fatal("authorizeAPI returned false")
	}

	resp := rr.Result()
	defer resp.Body.Close()
	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}
	if cookies[0].Name != authCookieName || cookies[0].Value != "secret" {
		t.Fatalf("cookie = %s:%s, want %s:secret", cookies[0].Name, cookies[0].Value, authCookieName)
	}
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

func makeJPEGZip(t *testing.T, path string) {
	t.Helper()
	imageBody := bytes.NewBuffer(makeJPEGBytes(t, 16, 24, color.RGBA{R: 40, G: 50, B: 180, A: 255}))
	makeZip(t, path, map[string]string{"001.jpg": imageBody.String()})
}

func makeJPEGBytes(t *testing.T, width int, height int, fill color.RGBA) []byte {
	t.Helper()
	var imageBody bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, fill)
		}
	}
	if err := jpeg.Encode(&imageBody, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	return imageBody.Bytes()
}

func makeImageZip(t *testing.T, path string, entryName string, width int, height int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: 180, A: 255})
		}
	}
	var body bytes.Buffer
	if err := jpeg.Encode(&body, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create(entryName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(body.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
