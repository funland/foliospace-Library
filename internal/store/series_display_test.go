package store

import (
	"testing"

	"foliospace-reader/internal/domain"
)

func TestSeriesPresentationMatchesComicBrowserAndPreservesFavorites(t *testing.T) {
	st := comicTestStore(t)
	first, _ := st.CreateLibraryWithType("One", t.TempDir(), "comic")
	second, _ := st.CreateLibraryWithType("Two", t.TempDir(), "comic")
	raw := "Group/01[Author]Title-115"
	a := comicTestSeries(t, st, first.ID, raw, raw, "cbz", 1)
	b := comicTestSeries(t, st, second.ID, raw, raw, "cbz", 1)
	if _, err := st.SaveComicTitleRules(second.ID, []ComicTitleRule{{Pattern: "Title", Replacement: "Custom"}}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateCollectionPrivateStateForProfile(a.ID, 0, domain.CollectionPrivateState{Favorite: true, Liked: true}); err != nil {
		t.Fatal(err)
	}
	for _, list := range []func() ([]domain.Series, error){st.ListSeries, func() ([]domain.Series, error) {
		p, err := st.ListSeriesPageForProfile(0, domain.CollectionListOptions{Limit: 20})
		return p.Items, err
	}} {
		items, err := list()
		if err != nil || len(items) != 2 {
			t.Fatalf("list=%+v %v", items, err)
		}
		for _, item := range items {
			want := "[Author]Title"
			if item.ID == b.ID {
				want = "01[Author]Custom-115"
			}
			if item.Title != raw || item.DirectoryPath != raw || item.DisplayTitle != want {
				t.Fatalf("presentation changed identity: %+v", item)
			}
			if item.ID == a.ID && (!item.Favorite || !item.Liked) {
				t.Fatalf("favorite lost: %+v", item)
			}
			page, err := st.ComicBrowser(0, ComicBrowserOptions{LibraryID: item.LibraryID, Query: "Title"})
			if err != nil || len(page.Items) != 1 || page.Items[0].DisplayTitle != item.DisplayTitle {
				t.Fatalf("browser label disagrees: %+v %+v %v", item, page, err)
			}
		}
	}
	item, err := st.SeriesByID(a.ID)
	if err != nil || item.DisplayTitle != "[Author]Title" || item.Title != raw || !item.Favorite {
		t.Fatalf("detail=%+v %v", item, err)
	}
	for _, series := range []domain.Series{a, b} {
		books, err := st.ListBooks(series.ID)
		if err != nil {
			t.Fatal(err)
		}
		books, err = st.ApplyBookCollectionPresentation(books)
		want := "[Author]Title"
		if series.ID == b.ID {
			want = "01[Author]Custom-115"
		}
		if err != nil || len(books) != 1 || books[0].CollectionTitle != raw || books[0].CollectionDisplayTitle != want {
			t.Fatalf("book collection display=%+v %v", books, err)
		}
	}
}

func TestCreatorPresentationDoesNotRewriteStoredOrCuratedMetadata(t *testing.T) {
	st := comicTestStore(t)
	lib, _ := st.CreateLibraryWithType("Books", t.TempDir(), "book")
	raw := "By Ada; Bob,"
	series := comicTestSeries(t, st, lib.ID, raw, ".", "epub", 1)
	book, _ := st.BookBySeriesTitle(series.ID, "Volume 01", "epub")
	if _, err := st.UpdateBookMetadata(book.ID, raw, "Manual description", nil); err != nil {
		t.Fatal(err)
	}
	book, err := st.BookByID(book.ID)
	if err != nil || book.Creator != raw || book.DisplayCreator != "Ada · Bob" {
		t.Fatalf("book=%+v %v", book, err)
	}
	shared, _ := st.BookSharedMetadata(book.ID)
	if shared.Creator != raw {
		t.Fatalf("raw author overwritten: %+v", shared)
	}
	group, err := st.SeriesByID(series.ID)
	if err != nil || group.Title != raw || group.DisplayTitle != "Ada · Bob" {
		t.Fatalf("author group=%+v %v", group, err)
	}
	curated := comicTestSeries(t, st, lib.ID, "By the Sea", "Curated", "epub", 1)
	group, err = st.SeriesByID(curated.ID)
	if err != nil || group.DisplayTitle != "By the Sea" {
		t.Fatalf("curated title changed=%+v %v", group, err)
	}
}
