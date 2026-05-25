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
	if !strings.Contains(infoBody, `"apiVersion":"v1"`) || !strings.Contains(infoBody, `"epub"`) {
		t.Fatalf("client info response %q does not include v1 capabilities", infoBody)
	}

	homeBody := get(t, ts.URL+"/api/client/home")
	if strings.Contains(homeBody, root) || strings.Contains(homeBody, "filePath") {
		t.Fatalf("client home leaked file path: %q", homeBody)
	}
	if !strings.Contains(homeBody, `"continueReading"`) || !strings.Contains(homeBody, `"recentBooks"`) || !strings.Contains(homeBody, `"collections"`) {
		t.Fatalf("client home response %q is missing expected sections", homeBody)
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
