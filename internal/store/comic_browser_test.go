package store

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
)

func comicTestStore(t *testing.T) *Store {
	t.Helper()
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return New(conn)
}

func comicTestSeries(t *testing.T, st *Store, libraryID int64, title, directory, format string, count int) domain.Series {
	t.Helper()
	series, err := st.UpsertSeries(libraryID, title, directory)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < count; i++ {
		if _, err := st.UpsertBook(series.ID, fmt.Sprintf("Volume %02d", i+1), format); err != nil {
			t.Fatal(err)
		}
	}
	return series
}

func TestComicBrowserHierarchyIdentityAndPaging(t *testing.T) {
	st := comicTestStore(t)
	lib, err := st.CreateLibraryWithType("Comics", t.TempDir(), "comic")
	if err != nil {
		t.Fatal(err)
	}
	first := comicTestSeries(t, st, lib.ID, "Alpha/01[作者]同名-115", "Alpha/01[作者]同名-115", "cbz", 2)
	second := comicTestSeries(t, st, lib.ID, "Alpha/02[作者]同名-115", "Alpha/02[作者]同名-115", "cbz", 3)
	comicTestSeries(t, st, lib.ID, "Alpha/Child/Deep", "Alpha/Child/Deep", "cbz", 4)
	comicTestSeries(t, st, lib.ID, "Alphabet/Sibling", "Alphabet/Sibling", "cbz", 5)
	comicTestSeries(t, st, lib.ID, "Root", ".", "cbz", 1)
	comicTestSeries(t, st, lib.ID, "Empty", "Empty", "cbz", 0)
	page, err := st.ComicBrowser(0, ComicBrowserOptions{LibraryID: lib.ID, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || len(page.Items) != 1 || page.Items[0].Path != "Alpha" || page.Items[0].CollectionCount != 3 || page.Items[0].BookCount != 9 || !page.HasMore {
		t.Fatalf("root page = %+v", page)
	}
	page, err = st.ComicBrowser(0, ComicBrowserOptions{LibraryID: lib.ID, Limit: 1, Offset: 1})
	if err != nil || page.Items[0].Path != "Alphabet" || page.Items[0].BookCount != 5 {
		t.Fatalf("page 2 = %+v %v", page, err)
	}
	page, err = st.ComicBrowser(0, ComicBrowserOptions{LibraryID: lib.ID, Path: "Alpha"})
	if err != nil || page.Total != 3 {
		t.Fatalf("Alpha children = %+v %v", page, err)
	}
	var same []ComicBrowserNode
	for _, item := range page.Items {
		if item.DisplayTitle == "[作者]同名" {
			same = append(same, item)
		}
	}
	if len(same) != 2 || same[0].Kind != "collection" || same[1].Kind != "collection" || same[0].ID != first.ID || same[1].ID != second.ID || same[0].Path == same[1].Path || same[0].BookCount+same[1].BookCount != 5 {
		t.Fatalf("cleaned labels merged = %+v", same)
	}
	page, err = st.ComicBrowser(0, ComicBrowserOptions{LibraryID: lib.ID, Path: first.DirectoryPath})
	if err != nil || page.Total != 1 || page.Items[0].ID != first.ID || page.Items[0].Title != first.Title || page.Items[0].DisplayTitle != "[作者]同名" {
		t.Fatalf("leaf = %+v %v", page, err)
	}
	page, err = st.ComicBrowser(0, ComicBrowserOptions{LibraryID: lib.ID, Path: "Alpha", Query: "同名", Limit: 1, Offset: 1})
	if err != nil || page.Total != 2 || len(page.Items) != 1 || page.Items[0].ID != second.ID || page.HasMore {
		t.Fatalf("search page = %+v %v", page, err)
	}
	page, err = st.ComicBrowser(0, ComicBrowserOptions{LibraryID: lib.ID, Path: "Alpha", Query: "Sibling"})
	if err != nil || page.Total != 0 {
		t.Fatalf("prefix escaped scope = %+v %v", page, err)
	}
	unchanged, err := st.SeriesByID(first.ID)
	if err != nil || unchanged.Title != first.Title || unchanged.DirectoryPath != first.DirectoryPath {
		t.Fatalf("identity changed = %+v %v", unchanged, err)
	}
}

func TestComicBrowserLeafCollectionsSkipRedundantDirectory(t *testing.T) {
	st := comicTestStore(t)
	lib, _ := st.CreateLibraryWithType("Comics", t.TempDir(), "comic")
	own := comicTestSeries(t, st, lib.ID, "Group", "Group", "cbz", 1)
	leaf := comicTestSeries(t, st, lib.ID, "Group/Leaf", "Group/Leaf", "cbz", 2)
	alternate := comicTestSeries(t, st, lib.ID, "Curated edition", "Group/Leaf", "cbz", 1)
	deep := comicTestSeries(t, st, lib.ID, "Group/Deep/Child", "Group/Deep/Child", "cbz", 4)
	other, _ := st.CreateLibraryWithType("Other", t.TempDir(), "comic")
	otherLeaf := comicTestSeries(t, st, other.ID, "Group", "Group", "cbz", 1)
	root, err := st.ComicBrowser(0, ComicBrowserOptions{})
	if err != nil || root.Total != 2 || root.Items[0].Kind != "folder" || root.Items[0].LibraryID != lib.ID || root.Items[0].CollectionCount != 4 || root.Items[0].BookCount != 8 || root.Items[1].Kind != "collection" || root.Items[1].ID != otherLeaf.ID {
		t.Fatalf("per-library descendant classification: %+v %v", root, err)
	}
	page, err := st.ComicBrowser(0, ComicBrowserOptions{LibraryID: lib.ID, Path: "Group"})
	if err != nil || page.Total != 4 || page.Items[0].Kind != "folder" || page.Items[0].Path != "Group/Deep" || page.Items[0].BookCount != 4 {
		t.Fatalf("group contents: %+v %v", page, err)
	}
	ids := map[int64]bool{}
	for _, node := range page.Items {
		if node.Kind == "collection" {
			ids[node.ID] = true
		}
	}
	if !ids[own.ID] || !ids[leaf.ID] || !ids[alternate.ID] || len(ids) != 3 {
		t.Fatalf("direct collections lost/merged: %+v", page)
	}
	page, err = st.ComicBrowser(0, ComicBrowserOptions{LibraryID: lib.ID, Path: "Group/Deep"})
	if err != nil || page.Total != 1 || page.Items[0].Kind != "collection" || page.Items[0].ID != deep.ID || page.Items[0].Path != deep.DirectoryPath {
		t.Fatalf("leaf still requires a directory hop: %+v %v", page, err)
	}
}

func TestComicBrowserLibraryTypesPrivateStateAndNoDoubleCounts(t *testing.T) {
	st := comicTestStore(t)
	mixed, _ := st.CreateLibraryWithType("Mixed", t.TempDir(), "mixed")
	comic := comicTestSeries(t, st, mixed.ID, "Shared", "Shared", "cbz", 1)
	comicTestSeries(t, st, mixed.ID, "Books", "Books", "epub", 2)
	for _, kind := range []string{"video", "game", "book"} {
		lib, _ := st.CreateLibraryWithType(kind, t.TempDir(), kind)
		comicTestSeries(t, st, lib.ID, kind, kind, "cbz", 1)
	}
	other, _ := st.CreateLibraryWithType("Other", t.TempDir(), "comic")
	comicTestSeries(t, st, other.ID, "Shared", "Shared", "cbz", 1)
	profile, err := st.CreateProfile("Reader", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateCollectionPrivateStateForProfile(comic.ID, profile.ID, domain.CollectionPrivateState{Favorite: true, Liked: true}); err != nil {
		t.Fatal(err)
	}
	page, err := st.ComicBrowser(0, ComicBrowserOptions{})
	if err != nil || page.Total != 2 || page.Items[0].Kind != "collection" || page.Items[1].Kind != "collection" || page.Items[0].LibraryID == page.Items[1].LibraryID {
		t.Fatalf("cross-library nodes = %+v %v", page, err)
	}
	page, err = st.ComicBrowser(profile.ID, ComicBrowserOptions{LibraryID: mixed.ID})
	if err != nil || page.Total != 1 || !page.Items[0].Favorite || !page.Items[0].Liked {
		t.Fatalf("profile = %+v %v", page, err)
	}
	if page.Items[0].BookCount != 1 || page.Items[0].AddedAt.IsZero() {
		t.Fatalf("aggregate = %+v", page)
	}
	page, err = st.ComicBrowser(0, ComicBrowserOptions{LibraryID: mixed.ID, Path: "Shared"})
	if err != nil || page.Items[0].Favorite || page.Items[0].Liked {
		t.Fatalf("profile leaked = %+v %v", page, err)
	}
	book, err := st.BookBySeriesTitle(comic.ID, "Volume 01", "cbz")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		_, err := st.db.Exec(`INSERT INTO files(book_id,library_id,abs_path,rel_path,ext,size,mtime) VALUES(?,?,?,?,?,?,CURRENT_TIMESTAMP)`, book.ID, mixed.ID, fmt.Sprintf("/fixture/%d.cbz", i), fmt.Sprintf("Shared/%d.cbz", i), "cbz", 100)
		if err != nil {
			t.Fatal(err)
		}
	}
	page, err = st.ComicBrowser(0, ComicBrowserOptions{LibraryID: mixed.ID})
	if err != nil || page.Items[0].BookCount != 1 {
		t.Fatalf("file variants double counted = %+v %v", page, err)
	}
}

func TestComicTitleRulesConservativePreviewPersistenceAndBounds(t *testing.T) {
	st := comicTestStore(t)
	lib, _ := st.CreateLibraryWithType("Comics", t.TempDir(), "comic")
	series := comicTestSeries(t, st, lib.ID, "01[骑大车]作品[第01卷]-115", "01[骑大车]作品[第01卷]-115", "cbz", 1)
	rules := DefaultComicTitleRules()
	compiled, err := compileComicTitleRules(rules)
	if err != nil {
		t.Fatal(err)
	}
	for original, want := range map[string]string{
		"01[骑大车]作品[第01卷]-115": "[骑大车]作品[第01卷]",
		"123【作者】作品-115":       "【作者】作品",
		"20世纪少年":              "20世纪少年",
		"1984":                "1984",
		"作品115":               "作品115",
		"[作者][汉化组]作品 第01卷":    "[作者][汉化组]作品 第01卷",
		"1[第1卷]":              "1[第1卷]",
	} {
		if got := comicDisplayTitle(original, rules, compiled); got != want {
			t.Errorf("%q -> %q, want %q", original, got, want)
		}
	}
	custom := []ComicTitleRule{{Pattern: "作品", Replacement: "漫画"}}
	preview, err := st.PreviewComicTitleRules(lib.ID, custom)
	if err != nil || preview.Samples[0].DisplayTitle != "01[骑大车]漫画[第01卷]-115" {
		t.Fatalf("preview = %+v %v", preview, err)
	}
	current, err := st.ComicTitleRules(lib.ID)
	if err != nil || current.Samples[0].DisplayTitle != "[骑大车]作品[第01卷]" {
		t.Fatalf("preview persisted = %+v %v", current, err)
	}
	if _, err := st.SaveComicTitleRules(lib.ID, custom); err != nil {
		t.Fatal(err)
	}
	current, err = st.ComicTitleRules(lib.ID)
	if err != nil || len(current.Rules) != 1 || current.Rules[0] != custom[0] {
		t.Fatalf("rules not saved = %+v %v", current, err)
	}
	for _, invalid := range [][]ComicTitleRule{{{Pattern: "("}}, {{Pattern: "(?=unsupported)"}}, {{Pattern: strings.Repeat("x", 1025)}}, make([]ComicTitleRule, 17)} {
		if _, err := st.SaveComicTitleRules(lib.ID, invalid); !errors.Is(err, ErrInvalidComicRequest) {
			t.Errorf("invalid rule accepted: %v", err)
		}
	}
	current, err = st.ComicTitleRules(lib.ID)
	if err != nil || current.Rules[0] != custom[0] {
		t.Fatalf("invalid save replaced rules = %+v %v", current, err)
	}
	if _, err := st.SaveComicTitleRules(lib.ID, []ComicTitleRule{}); err != nil {
		t.Fatal(err)
	}
	current, err = st.ComicTitleRules(lib.ID)
	if err != nil || current.Samples[0].DisplayTitle != series.Title || len(current.Rules) != 0 {
		t.Fatalf("disabled = %+v %v", current, err)
	}
	if _, err := st.SaveComicTitleRules(lib.ID, DefaultComicTitleRules()); err != nil {
		t.Fatal(err)
	}
	unchanged, _ := st.SeriesByID(series.ID)
	if unchanged.Title != series.Title || unchanged.DirectoryPath != series.DirectoryPath {
		t.Fatal("rules mutated source")
	}
	empty := []ComicTitleRule{{Pattern: ".*", Replacement: ""}}
	compiled, _ = compileComicTitleRules(empty)
	if comicDisplayTitle("Keep", empty, compiled) != "Keep" {
		t.Fatal("empty label not preserved")
	}
	expanding := []ComicTitleRule{{Pattern: ".", Replacement: strings.Repeat("x", 512)}}
	compiled, _ = compileComicTitleRules(expanding)
	if got := comicDisplayTitle(strings.Repeat("z", 1024), expanding, compiled); len(got) != 1024 {
		t.Fatal("unbounded replacement")
	}
}

func TestComicBrowserRejectsUnsafePaths(t *testing.T) {
	st := comicTestStore(t)
	lib, _ := st.CreateLibraryWithType("Comics", t.TempDir(), "comic")
	comicTestSeries(t, st, lib.ID, "Private", "/private/root/Secret", "cbz", 1)
	for _, path := range []string{"../secret", "/secret", "a/../b", "a//b", "a\\b", "C:/secret"} {
		if _, err := st.ComicBrowser(0, ComicBrowserOptions{LibraryID: lib.ID, Path: path}); !errors.Is(err, ErrInvalidComicRequest) {
			t.Errorf("unsafe path %q: %v", path, err)
		}
	}
	page, err := st.ComicBrowser(0, ComicBrowserOptions{})
	if err != nil || page.Total != 0 {
		t.Fatalf("absolute path exposed = %+v %v", page, err)
	}
}
