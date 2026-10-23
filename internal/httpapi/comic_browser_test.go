package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/service"
	"foliospace-reader/internal/store"
)

func TestComicBrowserHTTPContractAuthAndRules(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("Comics", t.TempDir(), "comic")
	if err != nil {
		t.Fatal(err)
	}
	title := "Author/01[Group]Comic-115"
	series, err := st.UpsertSeries(lib.ID, title, title)
	if err != nil {
		t.Fatal(err)
	}
	book, err := st.UpsertBook(series.ID, "Volume 1", "cbz")
	if err != nil {
		t.Fatal(err)
	}
	profile, err := st.CreateProfile("Reader", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateCollectionPrivateStateForProfile(series.ID, profile.ID, domain.CollectionPrivateState{Favorite: true, Liked: true}); err != nil {
		t.Fatal(err)
	}
	routes := NewWithOptions(service.NewWithConfig(st, t.TempDir()), nil, Options{APIToken: "test-secret"}).Routes()
	request := func(method, path, body string, authorized bool) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if authorized {
			r.Header.Set("Authorization", "Bearer test-secret")
		}
		r.Header.Set("X-FolioSpace-Profile-Id", fmt.Sprint(profile.ID))
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, r)
		if strings.Contains(w.Body.String(), lib.RootPath) {
			t.Fatal("absolute root path leaked")
		}
		return w
	}
	browserPath := fmt.Sprintf("/api/client/comic-browser?libraryId=%d", lib.ID)
	rulesPath := fmt.Sprintf("/api/comic-title-rules?libraryId=%d", lib.ID)
	for _, target := range []struct{ method, path, body string }{{"GET", browserPath, ""}, {"GET", rulesPath, ""}, {"PUT", rulesPath, `{"rules":[]}`}, {"POST", rulesPath, `{"reset":true}`}} {
		if w := request(target.method, target.path, target.body, false); w.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated %s: %d", target.path, w.Code)
		}
	}
	w := request("GET", browserPath+"&limit=1", "", true)
	var page store.ComicBrowserPage
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || page.Total != 1 || page.Items[0].Kind != "folder" || page.Items[0].Path != "Author" || page.Items[0].CoverBookID != book.ID {
		t.Fatalf("root: %d %s", w.Code, w.Body)
	}
	if !strings.HasPrefix(page.Items[0].ThumbnailURL, clientThumbnailURL(book.ID, "small")+"&r=") || page.Items[0].ThumbnailStatus != "pending" {
		t.Fatalf("unversioned cover URL: %+v", page.Items[0])
	}
	w = request("GET", browserPath+"&path=Author", "", true)
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || page.Total != 1 || page.Items[0].Kind != "collection" || page.Items[0].ID != series.ID || !page.Items[0].Favorite || !strings.Contains(page.Items[0].ThumbnailURL, "&r=") {
		t.Fatalf("leaf navigation: %d %s", w.Code, w.Body)
	}
	w = request("GET", browserPath+"&q=Comic", "", true)
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || page.Total != 1 || page.Items[0].Kind != "collection" || page.Items[0].ID != series.ID || !page.Items[0].Favorite || !page.Items[0].Liked || page.Items[0].Title != title || page.Items[0].DisplayTitle != "[Group]Comic" || page.Items[0].DirectoryPath != title {
		t.Fatalf("search: %d %s", w.Code, w.Body)
	}
	w = request("GET", rulesPath, "", true)
	var current store.ComicTitleRules
	if err := json.Unmarshal(w.Body.Bytes(), &current); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || len(current.Rules) != 2 || len(current.Samples) != 1 || current.Samples[0].Original != title {
		t.Fatalf("rules: %d %s", w.Code, w.Body)
	}
	w = request("POST", rulesPath, `{"rules":[{"pattern":"Comic","replacement":"Preview"}]}`, true)
	if err := json.Unmarshal(w.Body.Bytes(), &current); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || current.Samples[0].DisplayTitle != "01[Group]Preview-115" {
		t.Fatalf("preview: %d %s", w.Code, w.Body)
	}
	w = request("GET", rulesPath, "", true)
	if err := json.Unmarshal(w.Body.Bytes(), &current); err != nil {
		t.Fatal(err)
	}
	if current.Samples[0].DisplayTitle != "[Group]Comic" {
		t.Fatal("preview saved rules")
	}
	for _, target := range []struct{ body, display string }{{`{"rules":[]}`, "01[Group]Comic-115"}, {`{"reset":true}`, "[Group]Comic"}} {
		w = request("PUT", rulesPath, target.body, true)
		if err := json.Unmarshal(w.Body.Bytes(), &current); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || current.Samples[0].DisplayTitle != target.display {
			t.Fatalf("save/reset: %d %s", w.Code, w.Body)
		}
	}
	for _, target := range []struct {
		method, path, body string
		status             int
	}{
		{"GET", browserPath + "&path=../private", "", 400},
		{"GET", browserPath + "&path=/private", "", 400},
		{"GET", browserPath + "&offset=-1", "", 400},
		{"GET", browserPath + "&limit=bad", "", 400},
		{"GET", "/api/client/comic-browser?libraryId=0&path=Author", "", 400},
		{"GET", "/api/client/comic-browser?libraryId=999999", "", 404},
		{"GET", "/api/comic-title-rules", "", 400},
		{"POST", browserPath, "", 405},
		{"DELETE", rulesPath, "", 405},
		{"PUT", rulesPath, `{"rules":[{"pattern":"(","replacement":""}]}`, 400},
		{"PUT", rulesPath, `{"rules":[],"reset":true}`, 400},
		{"PUT", rulesPath, `{"unknown":true}`, 400},
		{"PUT", rulesPath, `{"rules":null}`, 400},
		{"PUT", rulesPath, `{"rules":[]} {}`, 400},
		{"PUT", rulesPath, `{"rules":[{"pattern":"x","replacement":"` + strings.Repeat("x", 17000) + `"}]}`, 400},
	} {
		w = request(target.method, target.path, target.body, true)
		if w.Code != target.status {
			t.Errorf("%s %s: %d, want %d: %s", target.method, target.path, w.Code, target.status, w.Body)
		}
	}
	unchanged, err := st.SeriesByID(series.ID)
	if err != nil || unchanged.Title != title || unchanged.DirectoryPath != title {
		t.Fatal("HTTP rules mutated raw series")
	}
}
