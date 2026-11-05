package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"foliospace-reader/internal/db"
)

const catalogRowsPerType = 247

func catalogTestStore(t *testing.T) *Store {
	t.Helper()
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	st := New(conn)
	tx, err := conn.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, statement := range []string{
		`INSERT INTO libraries(id,name,root_path,asset_type) VALUES(1,'Books','/fixture/books','book'),(2,'Comics','/fixture/comics','comic'),(3,'Games','/fixture/games','game'),(4,'Videos','/fixture/videos','video')`,
		`INSERT INTO series(id,library_id,title,directory_path) VALUES(1,1,'Books','Books'),(2,2,'Comics','Comics')`,
		`INSERT INTO profiles(id,name) VALUES(2,'Other reader')`,
	} {
		if _, err := tx.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	for id := 1; id <= catalogRowsPerType; id++ {
		title := fmt.Sprintf("Shared item %03d", id)
		if id <= 2 {
			title = "A tied title"
		}
		bookFormat, gameFormat, videoFormat := "epub", "nes", "mp4"
		if id > 220 {
			bookFormat, gameFormat, videoFormat = "cbz", "zip", "mkv"
		}
		role := []string{"game", "dependency", "needs-curation"}[id%3]
		for _, row := range []struct {
			query string
			args  []any
		}{
			{`INSERT INTO books(id,series_id,title,format) VALUES(?,?,?,?)`, []any{id, 1 + id%2, title, bookFormat}},
			{`INSERT INTO games(id,library_id,title,format,file_path,rel_path,size,mtime,platform,catalog_role) VALUES(?,3,?,?,?,?,1,CURRENT_TIMESTAMP,'nes',?)`, []any{id, title, gameFormat, fmt.Sprintf("/fixture/games/%d.%s", id, gameFormat), fmt.Sprintf("%d.%s", id, gameFormat), role}},
			{`INSERT INTO videos(id,library_id,title,format,file_path,rel_path,size,mtime) VALUES(?,4,?,?,?,?,1,CURRENT_TIMESTAMP)`, []any{id, title, videoFormat, fmt.Sprintf("/fixture/videos/%d.%s", id, videoFormat), fmt.Sprintf("%d.%s", id, videoFormat)}},
		} {
			if _, err := tx.Exec(row.query, row.args...); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return st
}

func catalogCollect(t *testing.T, st *Store, options CatalogOptions) []CatalogItem {
	t.Helper()
	var items []CatalogItem
	seenCursor := map[string]bool{}
	for pageNo := 0; pageNo < 100; pageNo++ {
		page, err := st.SearchCatalog(context.Background(), options)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) > options.Limit || page.ProfileID == 0 || page.Order == "" || page.Consistency == "" || page.Scope == "" {
			t.Fatalf("invalid page contract: %+v", page)
		}
		items = append(items, page.Items...)
		if !page.HasMore {
			if page.NextCursor != "" {
				t.Fatal("terminal page returned a cursor")
			}
			return items
		}
		if page.NextCursor == "" || seenCursor[page.NextCursor] {
			t.Fatal("nonterminal cursor did not advance")
		}
		seenCursor[page.NextCursor] = true
		options.Cursor = page.NextCursor
	}
	t.Fatal("catalog never terminated")
	return nil
}

func TestCatalogKeysetCoversEveryStreamWithoutDuplicateComicBooks(t *testing.T) {
	st := catalogTestStore(t)
	page, err := st.SearchCatalog(context.Background(), CatalogOptions{Limit: 73})
	if err != nil || page.Total != 3*catalogRowsPerType {
		t.Fatalf("full count: %+v, %v", page, err)
	}
	items := catalogCollect(t, st, CatalogOptions{Limit: 73})
	if len(items) != 3*catalogRowsPerType {
		t.Fatalf("got %d items, want %d", len(items), 3*catalogRowsPerType)
	}
	seen := map[string]bool{}
	counts := map[string]int{}
	roles := map[string]bool{}
	for i, item := range items {
		wantType := []string{"book", "game", "video"}[i/catalogRowsPerType]
		wantID := int64(i%catalogRowsPerType + 1)
		if item.AssetType != wantType || item.ID != wantID {
			t.Fatalf("item %d = %s/%d, want %s/%d", i, item.AssetType, item.ID, wantType, wantID)
		}
		key := fmt.Sprintf("%s/%d", item.AssetType, item.ID)
		if seen[key] {
			t.Fatalf("duplicate row %s", key)
		}
		seen[key] = true
		counts[item.AssetType]++
		if item.AssetType == "game" {
			roles[item.CatalogRole] = true
		}
	}
	for _, kind := range []string{"book", "game", "video"} {
		if counts[kind] != catalogRowsPerType || !seen[kind+"/1"] {
			t.Fatalf("type/id namespace collision or truncation: %v", counts)
		}
	}
	if len(roles) != 3 {
		t.Fatalf("game roles silently filtered: %v", roles)
	}
}

func TestCatalogKeysetExcludesLateInsertsAndSkipsDeletions(t *testing.T) {
	st := catalogTestStore(t)
	first, err := st.SearchCatalog(context.Background(), CatalogOptions{Limit: 73})
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`INSERT INTO books(id,series_id,title,format) VALUES(1000,1,'Late book','epub')`,
		`INSERT INTO games(id,library_id,title,format,file_path,rel_path,size,mtime) VALUES(1000,3,'Late game','nes','/fixture/late.nes','late.nes',1,CURRENT_TIMESTAMP)`,
		`INSERT INTO videos(id,library_id,title,format,file_path,rel_path,size,mtime) VALUES(1000,4,'Late video','mkv','/fixture/late.mkv','late.mkv',1,CURRENT_TIMESTAMP)`,
		`DELETE FROM books WHERE id IN (1,150)`,
		`DELETE FROM games WHERE id=180`,
		`DELETE FROM videos WHERE id=200`,
	} {
		if _, err := st.db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	remaining := catalogCollect(t, st, CatalogOptions{Limit: 61, Cursor: first.NextCursor})
	seen := map[string]bool{}
	for _, item := range append(first.Items, remaining...) {
		key := fmt.Sprintf("%s/%d", item.AssetType, item.ID)
		if seen[key] || item.ID == 1000 {
			t.Fatalf("duplicate or late insert: %s", key)
		}
		seen[key] = true
	}
	if len(seen) != 3*catalogRowsPerType-3 || seen["book/150"] || seen["game/180"] || seen["video/200"] || !seen["book/1"] {
		t.Fatalf("deletion shifted page boundaries: %d rows", len(seen))
	}
	fresh, err := st.SearchCatalog(context.Background(), CatalogOptions{Query: "Late", Limit: 10})
	if err != nil || fresh.Total != 3 || len(fresh.Items) != 3 {
		t.Fatalf("fresh search did not see inserts: %+v %v", fresh, err)
	}
}

func TestCatalogFiltersBeforeCountsAndPagesAndKeepsTitleTiesStable(t *testing.T) {
	st := catalogTestStore(t)
	ctx := context.Background()
	books, err := st.CatalogBooks(ctx, CatalogOptions{Format: ".CBZ", Limit: 9, Offset: 18})
	if err != nil || books.Total != 27 || len(books.Items) != 9 || books.HasMore {
		t.Fatalf("book filter applied after bounded page: %+v %v", books, err)
	}
	for _, item := range books.Items {
		if item.Format != "cbz" || item.ID <= 220 {
			t.Fatalf("wrong filtered book: %+v", item)
		}
	}
	for _, tc := range []struct{ format, kind string }{{"cbz", "book"}, {"zip", "game"}, {"mkv", "video"}} {
		page, err := st.SearchCatalog(ctx, CatalogOptions{Format: tc.format, Limit: 7})
		if err != nil || page.Total != 27 || len(page.Items) != 7 || !page.HasMore {
			t.Fatalf("aggregate %s filter/count: %+v %v", tc.format, page, err)
		}
		items := catalogCollect(t, st, CatalogOptions{Format: tc.format, Limit: 7})
		if len(items) != 27 {
			t.Fatalf("%s missing filtered rows: %d", tc.format, len(items))
		}
		for i, item := range items {
			if item.ID != int64(221+i) || item.AssetType != tc.kind || item.Format != tc.format {
				t.Fatalf("%s item %d = %+v", tc.format, i, item)
			}
		}
	}
	var tieIDs []int64
	for offset := 0; offset < 2; offset++ {
		page, err := st.CatalogBooks(ctx, CatalogOptions{Query: "A tied title", Sort: "title", Limit: 1, Offset: offset})
		if err != nil || page.Total != 2 || len(page.Items) != 1 {
			t.Fatalf("tie page: %+v %v", page, err)
		}
		tieIDs = append(tieIDs, page.Items[0].ID)
	}
	if !reflect.DeepEqual(tieIDs, []int64{1, 2}) {
		t.Fatalf("title ties unstable: %v", tieIDs)
	}
	page, err := st.SearchCatalog(ctx, CatalogOptions{Query: "Shared item 24", Limit: 100})
	if err != nil || page.Total != 24 || len(page.Items) != 24 {
		t.Fatalf("query count across media: %+v %v", page, err)
	}
	empty, err := st.SearchCatalog(ctx, CatalogOptions{Query: "%", Limit: 10})
	if err != nil || empty.Total != 0 || empty.Items == nil || empty.HasMore {
		t.Fatalf("literal LIKE wildcard not escaped: %+v %v", empty, err)
	}
}

func TestCatalogCursorBoundToFiltersAndResolvedProfile(t *testing.T) {
	st := catalogTestStore(t)
	ctx := context.Background()
	first, err := st.SearchCatalog(ctx, CatalogOptions{Query: "Shared", Format: "epub", Limit: 3})
	if err != nil || first.NextCursor == "" {
		t.Fatalf("first page: %+v %v", first, err)
	}
	for _, options := range []CatalogOptions{
		{Query: "item", Format: "epub", Limit: 3},
		{Query: "Shared", Format: "cbz", Limit: 3},
		{Query: "Shared", Format: "epub", ProfileID: 2, Limit: 3},
	} {
		options.Cursor = first.NextCursor
		if _, err := st.SearchCatalog(ctx, options); !errors.Is(err, ErrCatalogArguments) {
			t.Errorf("cursor reused with different filter/profile: %+v: %v", options, err)
		}
	}
	if page, err := st.SearchCatalog(ctx, CatalogOptions{Query: " Shared ", Format: ".EPUB", ProfileID: first.ProfileID, Cursor: first.NextCursor, Limit: 5}); err != nil || len(page.Items) != 5 {
		t.Fatalf("equivalent normalized filters rejected: %+v %v", page, err)
	}
	for _, search := range []func(context.Context, CatalogOptions) (CatalogPage, error){st.CatalogBooks, st.SearchCatalog} {
		if _, err := search(ctx, CatalogOptions{ProfileID: 99999}); !errors.Is(err, ErrCatalogProfile) {
			t.Errorf("missing profile fell back: %v", err)
		}
	}
}

func TestCatalogRejectsMalformedCursorsAndArguments(t *testing.T) {
	st := catalogTestStore(t)
	ctx := context.Background()
	first, err := st.SearchCatalog(ctx, CatalogOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	data, err := base64.RawURLEncoding.DecodeString(first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	var valid map[string]any
	if err := json.Unmarshal(data, &valid); err != nil {
		t.Fatal(err)
	}
	cursors := []string{"!", base64.RawURLEncoding.EncodeToString([]byte("{}")), base64.RawURLEncoding.EncodeToString(append(data, []byte(" {}")...)), strings.Repeat("a", 2049)}
	for key, value := range map[string]any{"v": 2, "type": 3, "id": -1, "max": []int{-1, 10, 10}, "unexpected": true, "filter": "wrong"} {
		changed := map[string]any{}
		for k, v := range valid {
			changed[k] = v
		}
		changed[key] = value
		encoded, err := json.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		cursors = append(cursors, base64.RawURLEncoding.EncodeToString(encoded))
	}
	for _, cursor := range cursors {
		if _, err := st.SearchCatalog(ctx, CatalogOptions{Limit: 1, Cursor: cursor}); !errors.Is(err, ErrCatalogArguments) {
			t.Errorf("malformed cursor accepted: %q: %v", cursor, err)
		}
	}
	for _, options := range []CatalogOptions{{Limit: -1}, {Limit: 101}, {Offset: -1}, {Offset: 10000001}, {ProfileID: -1}, {Query: strings.Repeat("q", 513)}, {Format: strings.Repeat("f", 33)}} {
		for _, search := range []func(context.Context, CatalogOptions) (CatalogPage, error){st.CatalogBooks, st.SearchCatalog} {
			if _, err := search(ctx, options); !errors.Is(err, ErrCatalogArguments) {
				t.Errorf("invalid arguments accepted: %+v: %v", options, err)
			}
		}
	}
}

func TestCatalogClosedDatabaseErrorsPropagate(t *testing.T) {
	st := catalogTestStore(t)
	if err := st.db.Close(); err != nil {
		t.Fatal(err)
	}
	for name, search := range map[string]func(context.Context, CatalogOptions) (CatalogPage, error){"books": st.CatalogBooks, "search": st.SearchCatalog} {
		if page, err := search(context.Background(), CatalogOptions{}); err == nil || errors.Is(err, ErrCatalogArguments) || errors.Is(err, ErrCatalogProfile) {
			t.Errorf("%s masked database error with page %+v: %v", name, page, err)
		}
	}
}

func TestCatalogEmptyStreamsAndTerminalEmptyPage(t *testing.T) {
	for _, remaining := range []string{"none", "game", "video"} {
		t.Run(remaining, func(t *testing.T) {
			st := catalogTestStore(t)
			for _, table := range []string{"books", "games", "videos"} {
				if (remaining == "game" && table == "games") || (remaining == "video" && table == "videos") {
					continue
				}
				if _, err := st.db.Exec("DELETE FROM " + table); err != nil {
					t.Fatal(err)
				}
			}
			items := catalogCollect(t, st, CatalogOptions{Limit: 100})
			want := catalogRowsPerType
			if remaining == "none" {
				want = 0
			}
			if len(items) != want {
				t.Fatalf("%s count=%d", remaining, len(items))
			}
			for _, item := range items {
				if item.AssetType != remaining {
					t.Fatalf("wrong stream %+v", item)
				}
			}
		})
	}
	st := catalogTestStore(t)
	first, err := st.SearchCatalog(context.Background(), CatalogOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"books", "games", "videos"} {
		if _, err = st.db.Exec("DELETE FROM " + table); err != nil {
			t.Fatal(err)
		}
	}
	last, err := st.SearchCatalog(context.Background(), CatalogOptions{Cursor: first.NextCursor, Limit: 100})
	if err != nil || last.Items == nil || len(last.Items) != 0 || last.Total != 0 || last.HasMore || last.NextCursor != "" {
		t.Fatalf("terminal empty page %+v %v", last, err)
	}
}

func TestCatalogCursorIsNotMetadataSnapshotAndRejectsOverflow(t *testing.T) {
	st := catalogTestStore(t)
	ctx := context.Background()
	first, err := st.SearchCatalog(ctx, CatalogOptions{Query: "Shared", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	// ID 1 lies before the cursor; ID 4 lies after it. Edits are observed live,
	// but already traversed IDs are not revisited until a fresh search.
	if _, err = st.db.Exec(`UPDATE books SET title='Shared newly matching' WHERE id IN (1,4)`); err != nil {
		t.Fatal(err)
	}
	remaining := catalogCollect(t, st, CatalogOptions{Query: "Shared", Cursor: first.NextCursor, Limit: 100})
	seenBefore, seenAfter := false, false
	for _, item := range remaining {
		if item.AssetType == "book" && item.ID == 1 {
			seenBefore = true
		}
		if item.AssetType == "book" && item.ID == 4 && item.Title == "Shared newly matching" {
			seenAfter = true
		}
	}
	if seenBefore || !seenAfter {
		t.Fatalf("live keyset semantics before=%v after=%v", seenBefore, seenAfter)
	}
	raw, err := base64.RawURLEncoding.DecodeString(first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	for _, changed := range []string{strings.Replace(string(raw), `"id":3`, `"id":9223372036854775808`, 1), strings.Replace(string(raw), `"id":3`, `"id":1.5`, 1)} {
		if changed == string(raw) {
			t.Fatal("fixture cursor did not contain expected ID")
		}
		if _, err = st.SearchCatalog(ctx, CatalogOptions{Query: "Shared", Cursor: base64.RawURLEncoding.EncodeToString([]byte(changed))}); !errors.Is(err, ErrCatalogArguments) {
			t.Fatalf("overflow/fraction cursor accepted: %v", err)
		}
	}
}
