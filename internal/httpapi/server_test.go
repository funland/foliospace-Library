package httpapi

import (
	"archive/zip"
	"bytes"
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
	if !strings.Contains(infoBody, `"apiVersion":"v1"`) ||
		!strings.Contains(infoBody, `"epub"`) ||
		!strings.Contains(infoBody, `"pdf"`) ||
		!strings.Contains(infoBody, `"mp4"`) ||
		!strings.Contains(infoBody, `"videoCatalog":true`) ||
		!strings.Contains(infoBody, `"pdfPageLayout":true`) ||
		!strings.Contains(infoBody, `"pdfWebtoonLayout":true`) ||
		!strings.Contains(infoBody, `"comicWebtoonLayout":true`) ||
		!strings.Contains(infoBody, `"webtoonPositionSync":true`) ||
		!strings.Contains(infoBody, `"pageImageDownsample":true`) ||
		!strings.Contains(infoBody, `"compactReader":true`) ||
		!strings.Contains(infoBody, `"scanSettings":true`) ||
		!strings.Contains(infoBody, `"gameSaveSync":true`) ||
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
	if !strings.Contains(homeBody, `"gameShelf"`) || !strings.Contains(homeBody, `"Super Mario World"`) || strings.Contains(homeBody, "Super Mario World (USA).sfc") {
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
		{LibraryID: lib.ID, Title: "SNES B", Platform: "snes", ROMSetName: "SNES", Format: "sfc", FilePath: "/library/snes/b.sfc", RelPath: "snes/b.sfc", Size: 1, MTime: time.Unix(31, 0), Compatibility: "unknown"},
		{LibraryID: lib.ID, Title: "Arcade C", Platform: "arcade", ROMSetName: "MAME", Format: "zip", FilePath: "/library/arcade/c.zip", RelPath: "arcade/c.zip", Size: 1, MTime: time.Unix(32, 0), Compatibility: "unknown"},
	} {
		if _, err := st.UpsertGame(game); err != nil {
			t.Fatal(err)
		}
	}
	ts := httptest.NewServer(NewWithOptions(service.New(st), nil, Options{APIToken: "secret"}).Routes())
	defer ts.Close()

	firstPage := authGet(t, ts.URL+"/api/client/games?limit=1&sort=title", "secret")
	if !strings.Contains(firstPage, `"total":3`) || strings.Count(firstPage, `"assetType":"game"`) != 1 {
		t.Fatalf("first page = %q, want one item from a three-game catalog", firstPage)
	}

	facets := authGet(t, ts.URL+"/api/client/games/facets", "secret")
	for _, want := range []string{
		`"total":3`,
		`"platform":"arcade"`,
		`"count":1`,
		`"platform":"snes"`,
		`"count":2`,
	} {
		if !strings.Contains(facets, want) {
			t.Fatalf("facets = %q, missing %s", facets, want)
		}
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
