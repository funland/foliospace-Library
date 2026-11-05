package service

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/store"
)

func diagnosticTestStore(t *testing.T) (*sql.DB, *store.Store, *Service, int64) {
	t.Helper()
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	st := store.New(conn)
	library, err := st.CreateLibrary("Synthetic diagnostics", "/nonexistent-diagnostic-fixtures")
	if err != nil {
		t.Fatal(err)
	}
	return conn, st, &Service{store: st}, library.ID
}

// Include every table, not just the returned asset. total_changes additionally
// catches writes that would otherwise leave the same final values behind.
func diagnosticSnapshot(t *testing.T, conn *sql.DB) string {
	t.Helper()
	rows, err := conn.Query(`SELECT name FROM sqlite_master WHERE type='table' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	state := map[string]any{}
	for _, table := range tables {
		rows, err := conn.Query(`SELECT * FROM "` + strings.ReplaceAll(table, `"`, `""`) + `" ORDER BY rowid`)
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		var contents [][]any
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				t.Fatal(err)
			}
			contents = append(contents, values)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		state[table] = contents
	}
	var changes int64
	if err := conn.QueryRow(`SELECT total_changes()`).Scan(&changes); err != nil {
		t.Fatal(err)
	}
	state["total_changes()"] = changes
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestBookDiagnosticsStoredOnlyAndNoWrites(t *testing.T) {
	conn, st, svc, libraryID := diagnosticTestStore(t)
	series, err := st.UpsertSeries(libraryID, "Synthetic", ".")
	if err != nil {
		t.Fatal(err)
	}
	profile, err := st.CreateProfile("Private diagnostic profile", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, code, health, index string
		analyzed                  bool
		pages                     []domain.Page
	}{
		{name: "unexamined", health: "unknown", index: "unknown"},
		{name: "indexed", health: "unknown", index: "indexed", pages: []domain.Page{{Index: 0, Name: "/private-page-name", PageKey: "private-page-key"}}},
		{name: "damaged", code: "zero_filled_file", health: "damaged", index: "indexed", pages: []domain.Page{{Index: 0, Name: "private-page"}}},
		{name: "unrecognized-issue", code: "private-unknown-issue", health: "unknown", index: "unknown"},
		{name: "empty-index", health: "unknown", index: "incomplete", analyzed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			book, err := st.UpsertBook(series.ID, tc.name, "epub")
			if err != nil {
				t.Fatal(err)
			}
			path := fmt.Sprintf("/nonexistent-diagnostic-fixtures/%d.epub", book.ID)
			file, err := st.UpsertFile(book.ID, libraryID, path, "private-filename.epub", 10, time.Unix(1, 0), ".epub")
			if err != nil {
				t.Fatal(err)
			}
			if tc.pages != nil || tc.analyzed {
				if err := st.ReplacePages(book.ID, tc.pages); err != nil {
					t.Fatal(err)
				}
			}
			if err := st.UpdateBookPrivateStateForProfile(book.ID, profile.ID, domain.BookPrivateState{Favorite: true, Summary: "private-book-note", Tags: []string{"private-book-tag"}}); err != nil {
				t.Fatal(err)
			}
			if err := st.SaveProgressDetailForProfile(book.ID, profile.ID, 7, "private-locator", .7); err != nil {
				t.Fatal(err)
			}
			if tc.code != "" {
				if err := st.SetBookFileHealth(book.ID, tc.code, "private-error-path "+path); err != nil {
					t.Fatal(err)
				}
				if err := st.RecordFileError(domain.FileErrorInput{LibraryID: libraryID, BookID: book.ID, FileID: file.ID, Path: path, Code: domain.ErrorCode(tc.code), Message: "private-error-message"}); err != nil {
					t.Fatal(err)
				}
			}
			before := diagnosticSnapshot(t, conn)
			if _, err := conn.Exec(`PRAGMA query_only=ON`); err != nil {
				t.Fatal(err)
			}
			for _, requested := range []int64{0, profile.ID} {
				result, err := svc.BookDiagnostics(book.ID, requested)
				if err != nil {
					t.Fatal(err)
				}
				wantProfile := requested
				if requested == 0 {
					wantProfile = 1
				}
				wantCode := tc.code
				if tc.name == "unrecognized-issue" {
					wantCode = "stored-file-issue"
				}
				if result.ProfileID != wantProfile || result.Health.Status != tc.health || result.Health.Code != wantCode || result.Index.Status != tc.index || result.Index.IndexedPages != len(tc.pages) || result.Source != "stored" || result.FilesystemVerified {
					t.Fatalf("unexpected diagnostics: %+v", result)
				}
				encoded, _ := json.Marshal(result)
				if strings.Contains(string(encoded), "private-") || strings.Contains(string(encoded), "nonexistent-diagnostic") || strings.Contains(string(encoded), "favorite") || strings.Contains(string(encoded), "locator") {
					t.Fatalf("private data leaked: %s", encoded)
				}
			}
			if after := diagnosticSnapshot(t, conn); before != after {
				t.Fatal("diagnosis mutated stored book, health, history, pages or other state")
			}
			if _, err := conn.Exec(`PRAGMA query_only=OFF`); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGameDiagnosticsStoredOnlyAndNoWrites(t *testing.T) {
	conn, st, svc, libraryID := diagnosticTestStore(t)
	game, err := st.UpsertGame(domain.GameAsset{LibraryID: libraryID, Title: "Synthetic", Platform: "snes", CatalogRole: "game", FilePath: "/nonexistent-diagnostic-fixtures/private-game.zip", Format: "zip", MTime: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	files := []domain.GameFile{
		{Name: "private-entry", FilePath: "/private-source-file", Size: 10, MTime: time.Unix(1, 0), SHA1: strings.Repeat("a", 40), Role: "entry"},
		{Name: "private-dependency", FilePath: "/private-other-file", Size: 20, MTime: time.Unix(1, 0), Role: "dependency"},
	}
	if err := st.ReplaceGameFiles(game.ID, files); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReplaceGameLaunchProfiles("private-policy", []domain.GameLaunchProfile{{GameID: game.ID, ID: "private-profile-name", Status: "ready", EntryFile: "private-entry", Policy: "private-policy", Files: []domain.GameLaunchProfileFile{{Name: "private-entry", SourceName: "private-source", SourceGameID: game.ID}}}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateGamePrivateStateForProfile(game.ID, 1, domain.GamePrivateState{Favorite: true, Liked: true}); err != nil {
		t.Fatal(err)
	}
	before := diagnosticSnapshot(t, conn)
	if _, err := conn.Exec(`PRAGMA query_only=ON`); err != nil {
		t.Fatal(err)
	}
	result, err := svc.GameDiagnostics(game.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.ProfileID != 1 || result.Source != "stored" || result.FilesystemVerified || result.Files.Count != 2 || result.Files.Checksummed != 1 || result.Files.ChecksumPending != 1 || result.Launch.ReadyProfiles != 1 || result.Launch.Status != "ready" {
		t.Fatalf("unexpected diagnostics: %+v", result)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "private-") || strings.Contains(string(encoded), "favorite") || strings.Contains(string(encoded), "liked") || strings.Contains(string(encoded), "sha1") {
		t.Fatalf("private data leaked: %s", encoded)
	}
	if before != diagnosticSnapshot(t, conn) {
		t.Fatal("game diagnosis mutated stored state")
	}
}

func TestDiagnosticsRejectMissingProfileAndAsset(t *testing.T) {
	conn, _, svc, _ := diagnosticTestStore(t)
	before := diagnosticSnapshot(t, conn)
	for _, diagnose := range []func(int64, int64) error{
		func(id, profile int64) error { _, err := svc.BookDiagnostics(id, profile); return err },
		func(id, profile int64) error { _, err := svc.GameDiagnostics(id, profile); return err },
	} {
		for _, tc := range []struct {
			id, profile int64
			want        error
		}{{1, 99999, ErrDiagnosticProfileNotFound}, {1, -1, ErrDiagnosticInvalidArgument}, {0, 0, ErrDiagnosticInvalidArgument}, {99999, 0, sql.ErrNoRows}, {99999, 1, sql.ErrNoRows}} {
			if got := diagnose(tc.id, tc.profile); !errors.Is(got, tc.want) {
				t.Fatalf("id=%d profile=%d: %v, want %v", tc.id, tc.profile, got, tc.want)
			}
		}
	}
	if before != diagnosticSnapshot(t, conn) {
		t.Fatal("invalid diagnosis mutated stored state")
	}
}
