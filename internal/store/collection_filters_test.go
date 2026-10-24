package store

import (
	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"testing"
)

func TestCollectionFiltersBeforePaginationAndMatchingCovers(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	s := New(conn)
	bookLib, _ := s.CreateLibraryWithType("Books", "/books", "book")
	comicLib, _ := s.CreateLibraryWithType("Comics", "/comics", "comic")
	mixedLib, _ := s.CreateLibraryWithType("Mixed", "/mixed", "mixed")
	a, _ := s.UpsertSeries(bookLib.ID, "Alpha", "Alpha")
	b, _ := s.UpsertSeries(bookLib.ID, "Beta", "Beta")
	c, _ := s.UpsertSeries(comicLib.ID, "PDF Comic", "PDF Comic")
	d, _ := s.UpsertSeries(mixedLib.ID, "Mixed Ebook", "Mixed Ebook")
	s.UpsertBook(a.ID, "A EPUB", "epub")
	pdf, _ := s.UpsertBook(a.ID, "Z PDF", "pdf")
	s.UpsertBook(b.ID, "Only EPUB", "epub")
	s.UpsertBook(c.ID, "Comic PDF", "pdf")
	s.UpsertBook(d.ID, "Mixed PDF", "pdf")
	for _, typ := range []string{"all", "book", "comic"} {
		for _, sort := range []string{"title", "count", "recently_added", "type"} {
			p, err := s.ListSeriesPageForProfile(0, domain.CollectionListOptions{PrimaryType: typ, Format: ".PDF", Limit: 1, Sort: sort})
			if err != nil {
				t.Fatal(err)
			}
			want := int64(3)
			if typ == "book" {
				want = 2
			}
			if typ == "comic" {
				want = 1
			}
			if p.Total != want || len(p.Items) != 1 || p.HasMore != (want > 1) {
				t.Fatalf("%s/%s page=%+v", typ, sort, p)
			}
		}
	}
	p, err := s.ListSeriesPageForProfile(0, domain.CollectionListOptions{LibraryID: bookLib.ID, Format: "pdf", Limit: 1})
	if err != nil || p.Total != 1 || p.Items[0].ID != a.ID || p.Items[0].CoverBookID != pdf.ID || p.Items[0].BookCount != 1 {
		t.Fatalf("filtered=%+v err=%v", p, err)
	}
	p, err = s.ListSeriesPageForProfile(0, domain.CollectionListOptions{PrimaryType: "book", Format: "pdf", Limit: 1, Offset: 1, Sort: "title"})
	if err != nil || p.Total != 2 || p.HasMore || p.Items[0].ID != d.ID {
		t.Fatalf("second page=%+v err=%v", p, err)
	}
	p, err = s.ListSeriesPageForProfile(0, domain.CollectionListOptions{LibraryID: comicLib.ID, PrimaryType: "book", Format: "pdf"})
	if err != nil || p.Total != 0 {
		t.Fatalf("comic PDF misclassified: %+v %v", p, err)
	}
}
