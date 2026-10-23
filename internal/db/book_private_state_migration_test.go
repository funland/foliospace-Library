package db_test

import (
	"database/sql"
	"reflect"
	"testing"

	"foliospace-reader/internal/db"
	"foliospace-reader/internal/store"
)

func TestMigrateBookPrivateStatesPreservesSharedTagsAndExistingState(t *testing.T) {
	type state struct {
		status   string
		favorite int
		rating   int
		tags     string
		summary  string
		updated  string
	}
	for _, tc := range []struct {
		name     string
		legacy   state
		existing *state
		wantRow  bool
	}{
		{name: "shared tags only", legacy: state{tags: "Calibre tag", updated: "2026-01-01T00:00:00Z"}},
		{name: "legacy status", legacy: state{status: "reading", tags: "Calibre tag", updated: "2026-01-01T00:00:00Z"}, wantRow: true},
		{name: "legacy favorite", legacy: state{favorite: 1, tags: "Calibre tag", updated: "2026-01-01T00:00:00Z"}, wantRow: true},
		{name: "legacy rating", legacy: state{rating: 4, tags: "Calibre tag", updated: "2026-01-01T00:00:00Z"}, wantRow: true},
		{name: "legacy summary", legacy: state{summary: "Legacy note", tags: "Calibre tag", updated: "2026-01-01T00:00:00Z"}, wantRow: true},
		{
			name:     "existing personal state wins",
			legacy:   state{status: "reading", favorite: 1, rating: 4, tags: "Calibre tag", summary: "Legacy note", updated: "2026-01-01T00:00:00Z"},
			existing: &state{status: "finished", rating: 2, tags: "Personal tag", summary: "User note", updated: "2026-10-23T12:00:00Z"},
			wantRow:  true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, err := db.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			st := store.New(conn)
			lib, err := st.CreateLibrary("Books", t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			series, err := st.UpsertSeries(lib.ID, "Books", ".")
			if err != nil {
				t.Fatal(err)
			}
			book, err := st.UpsertBook(series.ID, tc.name, "epub")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Exec(`UPDATE books SET private_status=?,favorite=?,rating=?,tags=?,summary=?,updated_at=? WHERE id=?`,
				tc.legacy.status, tc.legacy.favorite, tc.legacy.rating, tc.legacy.tags, tc.legacy.summary, tc.legacy.updated, book.ID); err != nil {
				t.Fatal(err)
			}
			if tc.existing != nil {
				x := tc.existing
				if _, err := conn.Exec(`INSERT INTO book_private_states(profile_id,book_id,private_status,favorite,rating,tags,summary,updated_at) VALUES(1,?,?,?,?,?,?,?)`,
					book.ID, x.status, x.favorite, x.rating, x.tags, x.summary, x.updated); err != nil {
					t.Fatal(err)
				}
			}
			for migration := 0; migration < 2; migration++ {
				if err := db.Migrate(conn); err != nil {
					t.Fatal(err)
				}
				var got state
				err := conn.QueryRow(`SELECT private_status,favorite,rating,tags,summary,updated_at FROM book_private_states WHERE profile_id=1 AND book_id=?`, book.ID).
					Scan(&got.status, &got.favorite, &got.rating, &got.tags, &got.summary, &got.updated)
				if !tc.wantRow {
					if err != sql.ErrNoRows {
						t.Fatalf("migration %d created personal state for shared tags: %#v, %v", migration, got, err)
					}
				} else {
					want := tc.legacy
					if tc.existing != nil {
						want = *tc.existing
					}
					if err != nil || got != want {
						t.Fatalf("migration %d personal state = %#v, want %#v, err=%v", migration, got, want, err)
					}
				}
				var shared state
				if err := conn.QueryRow(`SELECT private_status,favorite,rating,tags,summary,updated_at FROM books WHERE id=?`, book.ID).
					Scan(&shared.status, &shared.favorite, &shared.rating, &shared.tags, &shared.summary, &shared.updated); err != nil || shared != tc.legacy {
					t.Fatalf("migration changed legacy book fields: %#v, %v", shared, err)
				}
				projected, err := st.BookByID(book.ID)
				wantTags := []string{"Calibre tag"}
				if tc.existing != nil {
					wantTags = append(wantTags, "Personal tag")
				}
				if err != nil || !reflect.DeepEqual(projected.Tags, wantTags) {
					t.Fatalf("shared metadata tags lost after migration: %v, want %v, err=%v", projected.Tags, wantTags, err)
				}
			}
		})
	}
}

func TestMigrateFirstBookPrivateStateTablePreservesLegacyTags(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibrary("Books", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	series, err := st.UpsertSeries(lib.ID, "Books", ".")
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := st.UpsertBook(series.ID, "Legacy tags", "epub")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`UPDATE books SET tags='Legacy personal tag',updated_at='2026-01-01T00:00:00Z' WHERE id=?`, legacy.ID); err != nil {
		t.Fatal(err)
	}
	// Model a database from before the per-profile private-state table existed.
	if _, err := conn.Exec(`DROP TABLE book_private_states`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(conn); err != nil {
		t.Fatal(err)
	}
	var tags, updated string
	if err := conn.QueryRow(`SELECT tags,updated_at FROM book_private_states WHERE profile_id=1 AND book_id=?`, legacy.ID).Scan(&tags, &updated); err != nil || tags != "Legacy personal tag" || updated != "2026-01-01T00:00:00Z" {
		t.Fatalf("first upgrade lost legacy tags: tags=%q updated=%q err=%v", tags, updated, err)
	}
	if _, err := st.UpdateBookMetadata(legacy.ID, "", "", []string{"Calibre tag"}); err != nil {
		t.Fatal(err)
	}
	imported, err := st.UpsertBook(series.ID, "Later import", "epub")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpdateBookMetadata(imported.ID, "", "", []string{"Metadata only"}); err != nil {
		t.Fatal(err)
	}
	for migration := 0; migration < 2; migration++ {
		if err := db.Migrate(conn); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM book_private_states`).Scan(&count); err != nil || count != 1 {
			t.Fatalf("repeated migration created new private state: count=%d err=%v", count, err)
		}
		if err := conn.QueryRow(`SELECT tags,updated_at FROM book_private_states WHERE profile_id=1 AND book_id=?`, legacy.ID).Scan(&tags, &updated); err != nil || tags != "Legacy personal tag" || updated != "2026-01-01T00:00:00Z" {
			t.Fatalf("repeated migration changed legacy tags: tags=%q updated=%q err=%v", tags, updated, err)
		}
		projected, err := st.BookByID(legacy.ID)
		if err != nil || !reflect.DeepEqual(projected.Tags, []string{"Calibre tag", "Legacy personal tag"}) {
			t.Fatalf("metadata refresh lost preserved personal tags: %v err=%v", projected.Tags, err)
		}
		projected, err = st.BookByID(imported.ID)
		if err != nil || !reflect.DeepEqual(projected.Tags, []string{"Metadata only"}) {
			t.Fatalf("shared metadata projection lost imported tags: %v err=%v", projected.Tags, err)
		}
	}
}
