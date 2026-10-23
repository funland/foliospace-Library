package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"foliospace-reader/internal/db"
	"foliospace-reader/internal/service"
	"foliospace-reader/internal/store"
)

func TestEPUBSpineResourcesPreserveInlineImagesAndFootnoteLinks(t *testing.T) {
	root := t.TempDir()
	bookPath := filepath.Join(root, "inline-images.epub")
	image, err := base64.StdEncoding.DecodeString("R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==")
	if err != nil {
		t.Fatal(err)
	}
	chapter := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><head><title>Inline images</title><link href="../styles/book.css" rel="stylesheet"/></head><body><p>玉<img src="../images/tz001.gif" alt="tz001" class="glyph"/>黧<img src="../images/字 002.gif" alt="tz002" class="glyph"/><a epub:type="noteref" href="notes.html#z1">[1]</a><a href="#local-note">[2]</a></p><p id="local-note">Local note</p></body></html>`
	notes := `<html xmlns="http://www.w3.org/1999/xhtml"><head><title>Notes</title></head><body><p id="z1">Footnote</p></body></html>`
	css := `.glyph { height: 1em; vertical-align: baseline; }`
	makeZip(t, bookPath, map[string]string{
		"META-INF/container.xml": `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="OPS/package.opf"/></rootfiles></container>`,
		"OPS/package.opf":        `<package xmlns="http://www.idpf.org/2007/opf" version="3.0"><metadata><title>Inline images</title></metadata><manifest><item id="chapter" href="text/chapter.xhtml" media-type="application/xhtml+xml"/><item id="notes" href="text/notes.html" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="chapter"/><itemref idref="notes"/></spine></package>`,
		"OPS/text/chapter.xhtml": chapter,
		"OPS/text/notes.html":    notes,
		"OPS/styles/book.css":    css,
		"OPS/images/tz001.gif":   string(image),
		"OPS/images/字 002.gif":   string(image),
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
	info, err := os.Stat(bookPath)
	if err != nil {
		t.Fatal(err)
	}
	book, err := st.UpsertBasicBookFile(lib.ID, "Test", "", "Inline images", "epub", bookPath, filepath.Base(bookPath), info.Size(), info.ModTime(), ".epub")
	if err != nil {
		t.Fatal(err)
	}
	handler := NewWithOptions(service.New(st), nil, Options{APIToken: "test-epub-token"}).Routes()
	spineURL := "/api/books/" + itoa(book.ID) + "/epub/spine/0"
	request := func(method, target string, cookie *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, target, nil)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		return recorder
	}
	if response := request(http.MethodGet, spineURL, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous spine status = %d, want 401", response.Code)
	}
	spine := request(http.MethodGet, spineURL+"?access_token=test-epub-token", nil)
	if spine.Code != http.StatusOK || spine.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("spine status=%d content type=%q", spine.Code, spine.Header().Get("Content-Type"))
	}
	if strings.Contains(spine.Body.String(), "<?xml") {
		t.Fatal("HTML spine still has an XML declaration")
	}
	resourceBase := "/api/books/" + itoa(book.ID) + "/epub/resources/OPS/text/"
	for _, markup := range []string{
		`<base href="` + resourceBase + `">`,
		`src="../images/tz001.gif" alt="tz001"`,
		`src="../images/字 002.gif" alt="tz002"`,
		`href="notes.html#z1"`,
		`href="#local-note"`,
	} {
		if !strings.Contains(spine.Body.String(), markup) {
			t.Errorf("spine did not preserve %q", markup)
		}
	}
	cookies := spine.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != authCookieName || !cookies[0].HttpOnly || cookies[0].Path != "/" {
		t.Fatalf("spine did not set a root-scoped HttpOnly auth cookie: %#v", cookies)
	}
	baseURL, err := url.Parse("http://example.com" + resourceBase)
	if err != nil {
		t.Fatal(err)
	}
	for _, resource := range []struct {
		href        string
		contentType string
		body        []byte
	}{
		{"../images/tz001.gif", "image/gif", image},
		{"../images/字 002.gif", "image/gif", image},
		{"../styles/book.css", "text/css; charset=utf-8", []byte(css)},
		{"notes.html#z1", "application/xhtml+xml; charset=utf-8", []byte(notes)},
	} {
		t.Run(resource.href, func(t *testing.T) {
			reference, err := url.Parse(resource.href)
			if err != nil {
				t.Fatal(err)
			}
			target := baseURL.ResolveReference(reference).RequestURI()
			if response := request(http.MethodGet, target, nil); response.Code != http.StatusUnauthorized {
				t.Fatalf("anonymous resource status=%d, want 401", response.Code)
			}
			response := request(http.MethodGet, target, cookies[0])
			if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), resource.body) || response.Header().Get("Content-Type") != resource.contentType {
				t.Fatalf("resource status=%d content type=%q body length=%d", response.Code, response.Header().Get("Content-Type"), response.Body.Len())
			}
			if response.Header().Get("Content-Security-Policy") != spine.Header().Get("Content-Security-Policy") {
				t.Fatal("resource security policy differs from spine policy")
			}
			head := request(http.MethodHead, target, cookies[0])
			if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Type") != resource.contentType {
				t.Fatalf("HEAD status=%d content type=%q body length=%d", head.Code, head.Header().Get("Content-Type"), head.Body.Len())
			}
		})
	}
	if csp := spine.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") || !strings.Contains(csp, "img-src 'self' data: blob:") || !strings.Contains(csp, "style-src 'self' 'unsafe-inline'") {
		t.Fatalf("spine content security policy = %q", csp)
	}
	if err := st.SaveProgressDetail(book.ID, 0, "3", .5); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"/api/books/recent", "/api/books/continue-reading"} {
		response := request(http.MethodGet, target, cookies[0])
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"progressLocator":"3"`) {
			t.Fatalf("%s does not expose progress locator: status=%d body=%s", target, response.Code, response.Body.String())
		}
	}
	for _, locator := range []string{"1", "text/part0006.html"} {
		t.Run("client home progress "+locator, func(t *testing.T) {
			if err := st.SaveProgressDetail(book.ID, 0, locator, .2); err != nil {
				t.Fatal(err)
			}
			response := request(http.MethodGet, "/api/client/home?includeCollections=false", cookies[0])
			if response.Code != http.StatusOK {
				t.Fatalf("client home status=%d", response.Code)
			}
			var home struct {
				ContinueReading []struct {
					ID               int64   `json:"id"`
					CurrentPage      int     `json:"currentPage"`
					ProgressLocator  *string `json:"progressLocator"`
					ProgressFraction float64 `json:"progressFraction"`
				} `json:"continueReading"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &home); err != nil {
				t.Fatal(err)
			}
			if len(home.ContinueReading) != 1 {
				t.Fatalf("client home continue-reading count=%d, want 1", len(home.ContinueReading))
			}
			got := home.ContinueReading[0]
			if got.ID != book.ID || got.CurrentPage != 0 || got.ProgressLocator == nil || *got.ProgressLocator != locator || got.ProgressFraction != .2 {
				t.Fatalf("client home lost progress semantics: %#v, want locator=%q", got, locator)
			}
		})
	}
}
