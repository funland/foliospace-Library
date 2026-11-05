package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/service"
	"foliospace-reader/internal/store"
)

type catalogHTTPFixture struct {
	server    *httptest.Server
	conn      *sql.DB
	profileID int64
	libraryID int64
	alphaID   int64
	betaID    int64
	deepID    int64
	rootPath  string
}

func newCatalogHTTPFixture(t *testing.T) catalogHTTPFixture {
	t.Helper()
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("Comics", t.TempDir(), "comic")
	if err != nil {
		t.Fatal(err)
	}
	profile, err := st.CreateProfile("Private reader", "", "")
	if err != nil {
		t.Fatal(err)
	}
	f := catalogHTTPFixture{conn: conn, profileID: profile.ID, libraryID: lib.ID, rootPath: lib.RootPath}
	for index, path := range []string{"Group/Alpha", "Group/Beta", "Group/Deep/Child", "Other"} {
		collection, err := st.UpsertSeries(lib.ID, path, path)
		if err != nil {
			t.Fatal(err)
		}
		switch index {
		case 0:
			f.alphaID = collection.ID
		case 1:
			f.betaID = collection.ID
		case 2:
			f.deepID = collection.ID
		}
		book, err := st.UpsertBook(collection.ID, "Volume "+path, "cbz")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.UpsertFile(book.ID, lib.ID, lib.RootPath+"/"+path+".cbz", path+".cbz", 10, time.Unix(100, 0), ".cbz"); err != nil {
			t.Fatal(err)
		}
		for _, profileID := range []int64{1, profile.ID} {
			if err := st.UpdateBookPrivateStateForProfile(book.ID, profileID, domain.BookPrivateState{Favorite: true, Rating: 5, Tags: []string{"PRIVATE-TAG-MARKER"}, Summary: "PRIVATE-SUMMARY-MARKER", Status: "reading"}); err != nil {
				t.Fatal(err)
			}
			if err := st.SaveProgressDetailForProfile(book.ID, profileID, 3, "PRIVATE-LOCATOR-MARKER", 0.75); err != nil {
				t.Fatal(err)
			}
			if err := st.UpdateCollectionPrivateStateForProfile(collection.ID, profileID, domain.CollectionPrivateState{Favorite: true, Liked: true}); err != nil {
				t.Fatal(err)
			}
		}
		if index == 0 {
			if _, err := st.UpsertBook(collection.ID, "Noncomic EPUB", "epub"); err != nil {
				t.Fatal(err)
			}
		}
	}
	game, err := st.UpsertGame(domain.GameAsset{LibraryID: lib.ID, Title: "Game", Format: "nes", FilePath: lib.RootPath + "/private-game.nes", RelPath: "private-game.nes", MTime: time.Unix(100, 0), CatalogRole: "dependency"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateGamePrivateStateForProfile(game.ID, profile.ID, domain.GamePrivateState{Favorite: true, Liked: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertVideo(domain.VideoAsset{LibraryID: lib.ID, Title: "Video", Format: "mkv", FilePath: lib.RootPath + "/private-video.mkv", RelPath: "private-video.mkv", MTime: time.Unix(100, 0)}); err != nil {
		t.Fatal(err)
	}
	f.server = httptest.NewServer(NewWithOptions(service.NewWithConfig(st, t.TempDir()), nil, Options{APIToken: "catalog-test-secret"}).Routes())
	t.Cleanup(f.server.Close)
	return f
}

func (f catalogHTTPFixture) request(t *testing.T, method, path, token, profileHeader string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, f.server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if profileHeader != "" {
		req.Header.Set("X-FolioSpace-Profile-Id", profileHeader)
	}
	response, err := f.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, body
}

func catalogHTTPObject(t *testing.T, status int, body []byte) map[string]any {
	t.Helper()
	if status != http.StatusOK {
		t.Fatalf("status %d: %s", status, body)
	}
	var object map[string]any
	if err := json.Unmarshal(body, &object); err != nil {
		t.Fatal(err)
	}
	return object
}

func TestCatalogHTTPAuthenticationStrictProfilesAndValidation(t *testing.T) {
	f := newCatalogHTTPFixture(t)
	for _, path := range []string{"/api/client/catalog/books", "/api/client/catalog/search", "/api/client/catalog/comics"} {
		for _, token := range []string{"", "wrong-token"} {
			if status, body := f.request(t, "GET", path, token, ""); status != http.StatusUnauthorized {
				t.Errorf("%s unauthenticated status %d: %s", path, status, body)
			}
		}
		if status, body := f.request(t, "POST", path, "catalog-test-secret", ""); status != http.StatusMethodNotAllowed {
			t.Errorf("%s method status %d: %s", path, status, body)
		}
		status, body := f.request(t, "GET", path, "catalog-test-secret", "")
		if page := catalogHTTPObject(t, status, body); page["profileId"] != float64(1) {
			t.Errorf("default profile missing: %s", body)
		}
		for _, tc := range []struct {
			query, header string
			status        int
		}{
			{"?profileId=%zz", "", 400},
			{"?profileId=9223372036854775808", "", 400},
			{"?limit=9223372036854775808", "", 400},
			{"?profileId=" + fmt.Sprint(f.profileID), "", 200},
			{"", fmt.Sprint(f.profileID), 200},
			{"?profileId=" + fmt.Sprint(f.profileID), fmt.Sprint(f.profileID), 200},
			{"?profileId=0", "", 400},
			{"?profileId=-1", "", 400},
			{"?profileId=1.5", "", 400},
			{"?profileId=bad", "", 400},
			{"?profileId=", "", 400},
			{"?profileId=9007199254740992", "", 400},
			{"?profileId=99999", "", 404},
			{"", "99999", 404},
			{"", "bad", 400},
			{"", "0", 400},
			{"?profileId=1", fmt.Sprint(f.profileID), 400},
			{"?profileId=1&profileId=1", "", 400},
			{"?limit=101", "", 400},
			{"?limit=0", "", 400},
			{"?limit=1.5", "", 400},
			{"?limit=", "", 400},
			{"?q=a&q=b", "", 400},
			{"?unknown=1", "", 400},
			{"?q=" + strings.Repeat("x", 513), "", 400},
		} {
			status, body := f.request(t, "GET", path+tc.query, "catalog-test-secret", tc.header)
			if status != tc.status {
				t.Errorf("%s%s header %q status %d, want %d: %s", path, tc.query, tc.header, status, tc.status, body)
			}
			if status == 200 {
				if page := catalogHTTPObject(t, status, body); page["profileId"] != float64(f.profileID) {
					t.Errorf("explicit profile changed: %s", body)
				}
			}
		}
	}
	for _, path := range []string{
		"/api/client/catalog/books?cursor=x", "/api/client/catalog/books?sort=unsupported", "/api/client/catalog/books?offset=-1",
		"/api/client/catalog/search?offset=1", "/api/client/catalog/search?cursor=not-a-cursor", "/api/client/catalog/search?format=" + strings.Repeat("x", 33),
		"/api/client/catalog/comics?format=cbz", "/api/client/catalog/comics?libraryId=-1",
	} {
		if status, body := f.request(t, "GET", path, "catalog-test-secret", ""); status != http.StatusBadRequest {
			t.Errorf("invalid route arguments %s status %d: %s", path, status, body)
		}
	}
	status, body := f.request(t, "GET", "/api/client/info", "catalog-test-secret", "")
	info := catalogHTTPObject(t, status, body)
	capabilities, ok := info["capabilities"].(map[string]any)
	if !ok || capabilities["catalogSearchV1"] != true {
		t.Fatalf("catalog capability absent: %s", body)
	}
}

func TestCatalogHTTPSharedMetadataOmitsPrivateStateAndFilesystemPaths(t *testing.T) {
	f := newCatalogHTTPFixture(t)
	for _, profileID := range []int64{1, f.profileID} {
		for _, endpoint := range []string{"books", "search", "comics"} {
			path := fmt.Sprintf("/api/client/catalog/%s?profileId=%d", endpoint, profileID)
			status, body := f.request(t, "GET", path, "catalog-test-secret", "")
			page := catalogHTTPObject(t, status, body)
			for _, marker := range []string{f.rootPath, "PRIVATE-TAG-MARKER", "PRIVATE-SUMMARY-MARKER", "PRIVATE-LOCATOR-MARKER", "private-game.nes", "private-video.mkv"} {
				if strings.Contains(string(body), marker) {
					t.Errorf("%s leaked private value %q: %s", path, marker, body)
				}
			}
			items, ok := page["items"].([]any)
			if !ok || len(items) == 0 {
				t.Fatalf("%s missing items: %s", path, body)
			}
			allowed := "assetType id libraryId title format creator collectionId collectionTitle platform catalogRole"
			if endpoint == "comics" {
				allowed = "id libraryId kind path displayTitle bookCount collectionCount"
			}
			for _, value := range items {
				item := value.(map[string]any)
				for key := range item {
					if !strings.Contains(" "+allowed+" ", " "+key+" ") {
						t.Errorf("%s exposes unexpected field %q: %s", path, key, body)
					}
				}
			}
		}
	}
	status, body := f.request(t, "GET", "/api/client/catalog/books?format=epub&limit=1", "catalog-test-secret", "")
	page := catalogHTTPObject(t, status, body)
	if page["total"] != float64(1) || page["hasMore"] != false || page["items"].([]any)[0].(map[string]any)["format"] != "epub" {
		t.Fatalf("HTTP format was not forwarded: %s", body)
	}
}

func TestCatalogHTTPComicFolderTraversalPaginationAndSearch(t *testing.T) {
	f := newCatalogHTTPFixture(t)
	base := fmt.Sprintf("/api/client/catalog/comics?libraryId=%d", f.libraryID)
	status, body := f.request(t, "GET", base+"&limit=1", "catalog-test-secret", "")
	page := catalogHTTPObject(t, status, body)
	item := page["items"].([]any)[0].(map[string]any)
	if page["total"] != float64(2) || page["hasMore"] != true || item["kind"] != "folder" || item["path"] != "Group" || item["bookCount"] != float64(4) {
		t.Fatalf("root folder page: %s", body)
	}
	var ids []int64
	var paths []string
	for _, offset := range []int{0, 2} {
		status, body := f.request(t, "GET", base+fmt.Sprintf("&path=Group&limit=2&offset=%d", offset), "catalog-test-secret", "")
		page := catalogHTTPObject(t, status, body)
		if page["total"] != float64(3) || page["hasMore"] != (offset == 0) {
			t.Fatalf("child page totals: %s", body)
		}
		for _, value := range page["items"].([]any) {
			item := value.(map[string]any)
			paths = append(paths, item["path"].(string))
			if item["kind"] == "collection" {
				ids = append(ids, int64(item["id"].(float64)))
			}
		}
	}
	if len(paths) != 3 || paths[0] != "Group/Deep" || len(ids) != 2 || ids[0] != f.alphaID || ids[1] != f.betaID {
		t.Fatalf("children missing or repeated across pages: %v, ids %v", paths, ids)
	}
	for _, tc := range []struct {
		query string
		id    int64
		path  string
	}{
		{"&path=Group/Deep", f.deepID, "Group/Deep/Child"},
		{"&path=Group/Alpha", f.alphaID, "Group/Alpha"},
		{"&path=Group&q=Alpha", f.alphaID, "Group/Alpha"},
	} {
		status, body := f.request(t, "GET", base+tc.query, "catalog-test-secret", "")
		page := catalogHTTPObject(t, status, body)
		items := page["items"].([]any)
		if page["total"] != float64(1) || len(items) != 1 || items[0].(map[string]any)["id"] != float64(tc.id) || items[0].(map[string]any)["path"] != tc.path {
			t.Fatalf("folder/leaf navigation %s: %s", tc.query, body)
		}
	}
	for _, path := range []string{"../secret", "/secret", "Group/../Other", "Group//Alpha", `Group\Alpha`, "C:/secret"} {
		if status, body := f.request(t, "GET", base+"&path="+url.QueryEscape(path), "catalog-test-secret", ""); status != 400 {
			t.Errorf("unsafe path %q status %d: %s", path, status, body)
		}
	}
	if status, body := f.request(t, "GET", "/api/client/catalog/comics?libraryId=99999", "catalog-test-secret", ""); status != 404 {
		t.Errorf("missing library status %d, want 404: %s", status, body)
	}
}

func TestCatalogHTTPDatabaseFailuresReturnSanitizedErrors(t *testing.T) {
	f := newCatalogHTTPFixture(t)
	if err := f.conn.Close(); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"books", "search", "comics"} {
		status, body := f.request(t, "GET", "/api/client/catalog/"+endpoint, "catalog-test-secret", "")
		if status != http.StatusInternalServerError || !strings.Contains(string(body), "catalog unavailable") || strings.Contains(string(body), "sql:") || strings.Contains(string(body), f.rootPath) {
			t.Errorf("%s DB error masked or leaked: %d %s", endpoint, status, body)
		}
	}
}
