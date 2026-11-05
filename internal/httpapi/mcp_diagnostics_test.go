package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/service"
	"foliospace-reader/internal/store"
)

func TestMCPDiagnosticsHTTPReadOnlyAuthProfilesAndErrors(t *testing.T) {
	t.Setenv("FOLIOSPACE_VIDEO_METADATA_PROVIDER", "")
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	svc := service.NewWithConfig(st, t.TempDir())
	svc.PauseThumbnailWorker()
	defer svc.CloseVideoMetadataWorker()
	if err := svc.SetAdminToken("diagnostic-admin-token"); err != nil {
		t.Fatal(err)
	}
	library, err := st.CreateLibrary("Synthetic", "/nonexistent-diagnostic-http")
	if err != nil {
		t.Fatal(err)
	}
	series, err := st.UpsertSeries(library.ID, "Synthetic", ".")
	if err != nil {
		t.Fatal(err)
	}
	book, err := st.UpsertBook(series.ID, "Private title", "epub")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertFile(book.ID, library.ID, "/nonexistent-diagnostic-http/private-source.epub", "private-source.epub", 64, time.Unix(1, 0), ".epub"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetBookFileHealth(book.ID, "zero_filled_file", "PRIVATE-HTTP-ERROR /nonexistent-diagnostic-http"); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplacePages(book.ID, []domain.Page{{Index: 0, Name: "PRIVATE-HTTP-PAGE"}}); err != nil {
		t.Fatal(err)
	}
	game, err := st.UpsertGame(domain.GameAsset{LibraryID: library.ID, Title: "PRIVATE-HTTP-TITLE", Platform: "snes", CatalogRole: "game", Format: "zip", FilePath: "/nonexistent-diagnostic-http/private-game.zip", MTime: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := st.CreateProfile("PRIVATE-HTTP-PROFILE", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateBookPrivateStateForProfile(book.ID, profile.ID, domain.BookPrivateState{Summary: "PRIVATE-HTTP-NOTE", Favorite: true}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateGamePrivateStateForProfile(game.ID, profile.ID, domain.GamePrivateState{Favorite: true, Liked: true}); err != nil {
		t.Fatal(err)
	}
	var before int64
	if err := conn.QueryRow(`SELECT total_changes()`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`PRAGMA query_only=ON`); err != nil {
		t.Fatal(err)
	}
	for _, auth := range []struct{ name, apiToken, bearer string }{
		{"api", "diagnostic-api-token", "diagnostic-api-token"},
		{"admin", "", "diagnostic-admin-token"},
	} {
		t.Run(auth.name, func(t *testing.T) {
			handler := NewWithOptions(svc, nil, Options{APIToken: auth.apiToken}).Routes()
			for _, asset := range []struct {
				kind string
				id   int64
			}{{"books", book.ID}, {"games", game.ID}} {
				base := fmt.Sprintf("/api/client/diagnostics/%s/%d", asset.kind, asset.id)
				for _, tc := range []struct {
					name, method, suffix, header, bearer, code string
					status                                     int
					profile                                    int64
				}{
					{name: "default", method: "GET", bearer: auth.bearer, status: 200, profile: 1},
					{name: "explicit-query", method: "GET", suffix: fmt.Sprintf("?profileId=%d", profile.ID), bearer: auth.bearer, status: 200, profile: profile.ID},
					{name: "explicit-header", method: "GET", header: fmt.Sprint(profile.ID), bearer: auth.bearer, status: 200, profile: profile.ID},
					{name: "missing-auth", method: "GET", status: 401},
					{name: "bad-auth", method: "GET", bearer: "wrong-token", status: 401},
					{name: "unknown-profile", method: "GET", suffix: "?profileId=999999", bearer: auth.bearer, status: 404, code: "profile-not-found"},
					{name: "unknown-header-profile", method: "GET", header: "999999", bearer: auth.bearer, status: 404, code: "profile-not-found"},
					{name: "empty-profile", method: "GET", suffix: "?profileId=", bearer: auth.bearer, status: 400, code: "invalid-arguments"},
					{name: "malformed-profile", method: "GET", suffix: "?profileId=%ZZ", bearer: auth.bearer, status: 400, code: "invalid-arguments"},
					{name: "zero-profile", method: "GET", suffix: "?profileId=0", bearer: auth.bearer, status: 400, code: "invalid-arguments"},
					{name: "text-profile", method: "GET", suffix: "?profileId=no", bearer: auth.bearer, status: 400, code: "invalid-arguments"},
					{name: "duplicate-profile", method: "GET", suffix: "?profileId=1&profileId=2", bearer: auth.bearer, status: 400, code: "invalid-arguments"},
					{name: "conflicting-profile", method: "GET", suffix: "?profileId=1", header: fmt.Sprint(profile.ID), bearer: auth.bearer, status: 400, code: "invalid-arguments"},
					{name: "bad-header", method: "GET", header: "-1", bearer: auth.bearer, status: 400, code: "invalid-arguments"},
					{name: "unknown-argument", method: "GET", suffix: "?scan=true", bearer: auth.bearer, status: 400, code: "invalid-arguments"},
					{name: "extra-segment", method: "GET", suffix: "/scan", bearer: auth.bearer, status: 400, code: "invalid-arguments"},
					{name: "post", method: "POST", bearer: auth.bearer, status: 405, code: "method-not-allowed"},
					{name: "delete", method: "DELETE", bearer: auth.bearer, status: 405, code: "method-not-allowed"},
				} {
					t.Run(asset.kind+"/"+tc.name, func(t *testing.T) {
						req := httptest.NewRequest(tc.method, base+tc.suffix, nil)
						if tc.bearer != "" {
							req.Header.Set("Authorization", "Bearer "+tc.bearer)
						}
						if tc.header != "" {
							req.Header.Set("X-FolioSpace-Profile-Id", tc.header)
						}
						rec := httptest.NewRecorder()
						handler.ServeHTTP(rec, req)
						if rec.Code != tc.status {
							t.Fatalf("got %d: %s, want %d", rec.Code, rec.Body.String(), tc.status)
						}
						var result map[string]any
						if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
							t.Fatal(err)
						}
						if tc.code != "" && result["code"] != tc.code {
							t.Fatalf("unexpected error: %v", result)
						}
						if tc.status == 200 && (result["profileId"] != float64(tc.profile) || result["source"] != "stored" || result["filesystemVerified"] != false || rec.Header().Get("Cache-Control") != "private, no-store") {
							t.Fatalf("unexpected diagnostic context: %v", result)
						}
						for _, secret := range []string{"PRIVATE-HTTP", "nonexistent-diagnostic", "favorite", "liked", "privateState", "filePath", "profileName", "diagnostic-api-token", "diagnostic-admin-token"} {
							if strings.Contains(rec.Body.String(), secret) {
								t.Fatalf("leaked %q: %s", secret, rec.Body.String())
							}
						}
					})
				}
				for _, id := range []string{"0", "-1", "invalid", "9007199254740992", "999999"} {
					req := httptest.NewRequest("GET", "/api/client/diagnostics/"+asset.kind+"/"+id, nil)
					req.Header.Set("Authorization", "Bearer "+auth.bearer)
					rec := httptest.NewRecorder()
					handler.ServeHTTP(rec, req)
					want := 400
					if id == "999999" {
						want = 404
					}
					if rec.Code != want {
						t.Fatalf("id %s: %d %s", id, rec.Code, rec.Body.String())
					}
				}
			}
		})
	}
	var after int64
	if err := conn.QueryRow(`SELECT total_changes()`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("diagnostic HTTP requests wrote to the database: %d -> %d", before, after)
	}
}

func TestMCPDiagnosticDatabaseFailureIsRedacted(t *testing.T) {
	t.Setenv("FOLIOSPACE_VIDEO_METADATA_PROVIDER", "")
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc := service.NewWithConfig(store.New(conn), t.TempDir())
	svc.PauseThumbnailWorker()
	svc.CloseVideoMetadataWorker()
	conn.Close()
	handler := NewWithOptions(svc, nil, Options{APIToken: "diagnostic-api-token"}).Routes()
	for _, kind := range []string{"books", "games"} {
		req := httptest.NewRequest("GET", "/api/client/diagnostics/"+kind+"/1", nil)
		req.Header.Set("Authorization", "Bearer diagnostic-api-token")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != 500 || strings.TrimSpace(rec.Body.String()) != `{"code":"diagnostic-unavailable"}` {
			t.Fatalf("raw database failure exposed: %d %s", rec.Code, rec.Body.String())
		}
	}
}
