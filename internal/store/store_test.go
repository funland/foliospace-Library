package store

import (
	"testing"
	"time"

	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
)

func TestStorePersistsLibraryBookProgressAndErrors(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	s := New(conn)
	lib, err := s.CreateLibrary("Comics", "/library")
	if err != nil {
		t.Fatal(err)
	}
	series, err := s.UpsertSeries(lib.ID, "Series A", "Series A")
	if err != nil {
		t.Fatal(err)
	}
	book, err := s.UpsertBook(series.ID, "Book 1", "cbz")
	if err != nil {
		t.Fatal(err)
	}
	file, err := s.UpsertFile(book.ID, lib.ID, "/library/Series A/Book 1.cbz", "Series A/Book 1.cbz", 100, time.Unix(10, 0), ".cbz")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.ReplacePages(book.ID, []domain.Page{{Index: 0, Name: "001.jpg"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveProgressDetail(book.ID, 4, "", 0.4); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordFileError(domain.FileErrorInput{
		LibraryID: lib.ID,
		BookID:    book.ID,
		FileID:    file.ID,
		Path:      file.AbsPath,
		Code:      domain.ErrorEmptyFile,
		Message:   "empty file",
	}); err != nil {
		t.Fatal(err)
	}

	libraries, err := s.ListLibraries()
	if err != nil {
		t.Fatal(err)
	}
	if len(libraries) != 1 {
		t.Fatalf("libraries len = %d, want 1", len(libraries))
	}
	seriesList, err := s.ListSeries()
	if err != nil {
		t.Fatal(err)
	}
	if len(seriesList) != 1 || seriesList[0].DirectoryPath != "Series A" || seriesList[0].CollectionType != "directory" {
		t.Fatalf("series list = %#v, want directory collection at Series A", seriesList)
	}

	progress, err := s.Progress(book.ID)
	if err != nil {
		t.Fatal(err)
	}
	if progress.PageIndex != 4 {
		t.Fatalf("progress = %d, want 4", progress.PageIndex)
	}
	continueBooks, err := s.ListContinueReading(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(continueBooks) != 1 || continueBooks[0].CurrentPage != 4 || continueBooks[0].ProgressFraction != 0.4 {
		t.Fatalf("continue books = %#v, want saved progress", continueBooks)
	}
	recentBooks, err := s.ListRecentBooks(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recentBooks) != 1 || recentBooks[0].CollectionTitle != "Series A" || recentBooks[0].AddedAt.IsZero() {
		t.Fatalf("recent books = %#v, want collection title and added time", recentBooks)
	}

	errors, err := s.ListFileErrors()
	if err != nil {
		t.Fatal(err)
	}
	if len(errors) != 1 || errors[0].Code != domain.ErrorEmptyFile {
		t.Fatalf("errors = %#v, want one empty_file", errors)
	}
}

func TestStoreListsBooksPageWithSearchAndSort(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	s := New(conn)
	lib, err := s.CreateLibrary("Comics", "/library")
	if err != nil {
		t.Fatal(err)
	}
	series, err := s.UpsertSeries(lib.ID, "Series A", "Series A")
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"Alpha", "Beta", "Gamma", "Alphabet"} {
		book, err := s.UpsertBook(series.ID, title, "cbz")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.UpsertFile(book.ID, lib.ID, "/library/Series A/"+title+".cbz", "Series A/"+title+".cbz", 100, time.Now(), ".cbz"); err != nil {
			t.Fatal(err)
		}
	}

	page, err := s.ListBooksPage(domain.BookListOptions{
		SeriesID: series.ID,
		Limit:    2,
		Offset:   1,
		Query:    "alpha",
		Sort:     "title",
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || page.Limit != 2 || page.Offset != 1 || page.HasMore {
		t.Fatalf("page metadata = %#v, want total 2 offset 1 limit 2 hasMore false", page)
	}
	if len(page.Items) != 1 || page.Items[0].Title != "Alphabet" {
		t.Fatalf("page items = %#v, want Alphabet as second alpha match", page.Items)
	}

	recent, err := s.ListBooksPage(domain.BookListOptions{
		SeriesID: series.ID,
		Limit:    2,
		Sort:     "recently_added",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(recent.Items) != 2 || recent.Items[0].Title != "Alphabet" || recent.Items[1].Title != "Gamma" {
		t.Fatalf("recent items = %#v, want newest books first", recent.Items)
	}
	if recent.Total != 4 || !recent.HasMore {
		t.Fatalf("recent metadata = %#v, want total 4 and hasMore", recent)
	}

	empty, err := s.ListBooksPage(domain.BookListOptions{
		SeriesID: series.ID,
		Limit:    2,
		Query:    "missing",
	})
	if err != nil {
		t.Fatal(err)
	}
	if empty.Items == nil || len(empty.Items) != 0 || empty.Total != 0 {
		t.Fatalf("empty page = %#v, want empty non-nil items", empty)
	}
}

func TestStoreSearchesBooksAndPersistsPrivateState(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	s := New(conn)
	lib, err := s.CreateLibrary("Comics", "/library")
	if err != nil {
		t.Fatal(err)
	}
	seriesA, err := s.UpsertSeries(lib.ID, "Cyberpunk", "Cyberpunk")
	if err != nil {
		t.Fatal(err)
	}
	seriesB, err := s.UpsertSeries(lib.ID, "Quiet Drama", "Quiet Drama")
	if err != nil {
		t.Fatal(err)
	}
	bookA, err := s.UpsertBook(seriesA.ID, "Neon City", "cbz")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertBook(seriesB.ID, "Winter Notes", "epub"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertFile(bookA.ID, lib.ID, "/library/Cyberpunk/Neon City.cbz", "Cyberpunk/Neon City.cbz", 100, time.Now(), ".cbz"); err != nil {
		t.Fatal(err)
	}

	state := domain.BookPrivateState{
		Status:   "reading",
		Favorite: true,
		Rating:   5,
		Tags:     []string{"noir", "vision"},
		Summary:  "Private note",
	}
	if err := s.UpdateBookPrivateState(bookA.ID, state); err != nil {
		t.Fatal(err)
	}

	book, err := s.BookByID(bookA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if book.PrivateStatus != "reading" || !book.Favorite || book.Rating != 5 || book.Summary != "Private note" {
		t.Fatalf("book private state = %#v, want persisted state", book)
	}
	if len(book.Tags) != 2 || book.Tags[0] != "noir" || book.Tags[1] != "vision" {
		t.Fatalf("book tags = %#v, want stored tags", book.Tags)
	}

	tagResults, err := s.SearchBooks("vision", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(tagResults) != 1 || tagResults[0].ID != bookA.ID {
		t.Fatalf("tag search = %#v, want Neon City", tagResults)
	}

	collectionResults, err := s.SearchBooks("quiet", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(collectionResults) != 1 || collectionResults[0].Title != "Winter Notes" {
		t.Fatalf("collection search = %#v, want Winter Notes", collectionResults)
	}
}

func TestStoreListsPrivateShelves(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	s := New(conn)
	lib, err := s.CreateLibrary("Comics", "/library")
	if err != nil {
		t.Fatal(err)
	}
	series, err := s.UpsertSeries(lib.ID, "Series A", "Series A")
	if err != nil {
		t.Fatal(err)
	}
	wantBook, err := s.UpsertBook(series.ID, "Want Book", "cbz")
	if err != nil {
		t.Fatal(err)
	}
	favoriteBook, err := s.UpsertBook(series.ID, "Favorite Book", "epub")
	if err != nil {
		t.Fatal(err)
	}
	finishedBook, err := s.UpsertBook(series.ID, "Finished Book", "cbz")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateBookPrivateState(wantBook.ID, domain.BookPrivateState{Status: "want"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateBookPrivateState(favoriteBook.ID, domain.BookPrivateState{Status: "reading", Favorite: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateBookPrivateState(finishedBook.ID, domain.BookPrivateState{Status: "finished"}); err != nil {
		t.Fatal(err)
	}

	favorites, err := s.ListFavoriteBooks(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(favorites) != 1 || favorites[0].ID != favoriteBook.ID || !favorites[0].Favorite {
		t.Fatalf("favorites = %#v, want favorite book", favorites)
	}

	wantBooks, err := s.ListBooksByPrivateStatus("want", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(wantBooks) != 1 || wantBooks[0].ID != wantBook.ID || wantBooks[0].PrivateStatus != "want" {
		t.Fatalf("want books = %#v, want wanted book", wantBooks)
	}

	finishedBooks, err := s.ListBooksByPrivateStatus("finished", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(finishedBooks) != 1 || finishedBooks[0].ID != finishedBook.ID {
		t.Fatalf("finished books = %#v, want finished book", finishedBooks)
	}
}
