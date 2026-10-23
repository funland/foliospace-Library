package store

import (
	"fmt"
	"reflect"
	"testing"

	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
)

func TestCollectionPageMatchesFullAggregate(t *testing.T) {
	c, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s := New(c)
	for i, kind := range []string{"mixed", "book", "comic", "video"} {
		lib, err := s.CreateLibraryWithType(kind, "/qa/"+kind, kind)
		if err != nil {
			t.Fatal(err)
		}
		for j, title := range []string{"Same title", "100%_literal", "Empty"} {
			series, err := s.UpsertSeries(lib.ID, title, "")
			if err != nil {
				t.Fatal(err)
			}
			if j == 2 {
				continue
			}
			for k := 0; k <= i; k++ {
				format := "cbz"
				if k%2 == 0 {
					format = "epub"
				}
				book, err := s.UpsertBook(series.ID, fmt.Sprintf("Book %d", k), format)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = c.Exec(`INSERT INTO files(book_id,library_id,abs_path,rel_path,size,mtime,ext) VALUES(?,?,?,? ,1,'2026-01-01','.epub')`, book.ID, lib.ID, fmt.Sprintf("/qa/%d", book.ID), "Folder/book.epub"); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	for _, sort := range []string{"", "title", "recently_added", "count", "type", "unknown"} {
		for _, direction := range []string{"asc", "desc"} {
			for _, kind := range []string{"", "all", "book", "comic"} {
				for _, q := range []string{"", "%_", "Same", "absent"} {
					for _, offset := range []int{0, 2, 99} {
						options := domain.CollectionListOptions{Sort: sort, Direction: direction, PrimaryType: kind, Query: q, Offset: offset, Limit: 2}
						got, err := s.ListSeriesPageForProfile(1, options)
						if err != nil {
							t.Fatalf("%+v: %v", options, err)
						}
						where, args := collectionListWhere(options)
						var total int64
						if err = c.QueryRow(`SELECT COUNT(*) FROM (`+collectionListBaseSQL()+`) c`+where, args...).Scan(&total); err != nil {
							t.Fatal(err)
						}
						rows, err := c.Query(`SELECT c.id,c.library_id,c.title,c.directory_path,c.collection_type,c.primary_type,c.book_count,c.cover_book_id,c.created_at FROM (`+collectionListBaseSQL()+`) c`+where+collectionListOrderBy(sort, direction)+` LIMIT ? OFFSET ?`, append(args, 2, offset)...)
						if err != nil {
							t.Fatal(err)
						}
						expected := make([]domain.Series, 0)
						for rows.Next() {
							item, e := scanSeries(rows)
							if e != nil {
								t.Fatal(e)
							}
							expected = append(expected, item)
						}
						if err = rows.Err(); err != nil {
							t.Fatal(err)
						}
						rows.Close()
						if got.Total != total || got.HasMore != (int64(offset+len(expected)) < total) || !reflect.DeepEqual(got.Items, expected) {
							t.Fatalf("%+v: got %+v, expected %+v / total %d", options, got, expected, total)
						}
					}
				}
			}
		}
	}
}

func TestEmptyPrivateStatusStillIncludesMissingRows(t *testing.T) {
	c, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s := New(c)
	l, _ := s.CreateLibrary("Books", "/qa")
	series, _ := s.UpsertSeries(l.ID, "Books", "")
	a, _ := s.UpsertBook(series.ID, "No state", "epub")
	b, _ := s.UpsertBook(series.ID, "Explicit empty", "epub")
	d, _ := s.UpsertBook(series.ID, "Want", "epub")
	_, err = c.Exec(`INSERT INTO profiles(id,name) VALUES(2,'Guest')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Exec(`INSERT INTO book_private_states(profile_id,book_id,private_status) VALUES(1,?,''),(1,?,'want'),(2,?,'want')`, b.ID, d.ID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	books, err := s.ListBooksByPrivateStatusForProfile(1, "", 50)
	if err != nil || len(books) != 2 {
		t.Fatalf("empty: %+v %v", books, err)
	}
	want, err := s.ListBooksByPrivateStatusForProfile(1, " want ", 50)
	if err != nil || len(want) != 1 || want[0].ID != d.ID {
		t.Fatalf("want: %+v %v", want, err)
	}
}
