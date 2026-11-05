package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/httpapi"
	"foliospace-reader/internal/service"
	"foliospace-reader/internal/store"
)

// Exercise the actual stdio -> MCP -> authenticated HTTP -> service -> SQLite
// path, complementing the per-layer fault-injection tests. All media are
// synthetic records in temporary directories; no provider or media I/O occurs.
func TestWave1RealAPIThroughBothStdioTransports(t *testing.T) {
	for _, framed := range []bool{false, true} {
		t.Run(fmt.Sprintf("framed=%v", framed), func(t *testing.T) {
			t.Setenv("FOLIOSPACE_VIDEO_METADATA_PROVIDER", "")
			t.Setenv("FOLIOSPACE_TMDB_TOKEN", "")
			conn, err := db.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			st := store.New(conn)
			lib, err := st.CreateLibraryWithType("Synthetic", t.TempDir(), "mixed")
			if err != nil {
				t.Fatal(err)
			}
			series, err := st.UpsertSeries(lib.ID, "Synthetic collection", "safe-folder")
			if err != nil {
				t.Fatal(err)
			}
			book, err := st.UpsertBook(series.ID, "Synthetic book", "epub")
			if err != nil {
				t.Fatal(err)
			}
			game, err := st.UpsertGame(domain.GameAsset{LibraryID: lib.ID, Title: "Synthetic game", Platform: "nes", Format: "nes", FilePath: filepath.Join(lib.RootPath, "not-opened.nes"), MTime: time.Now(), CatalogRole: "game"})
			if err != nil {
				t.Fatal(err)
			}
			video, err := st.UpsertVideo(domain.VideoAsset{LibraryID: lib.ID, Title: "Synthetic video", Format: "mkv", FilePath: filepath.Join(lib.RootPath, "not-opened.mkv"), MTime: time.Now()})
			if err != nil {
				t.Fatal(err)
			}
			svc := service.New(st)
			defer svc.CloseVideoMetadataWorker()
			api := httptest.NewServer(httpapi.NewWithOptions(svc, nil, httpapi.Options{APIToken: "synthetic-token"}).Routes())
			defer api.Close()
			m := New(api.URL, "synthetic-token")
			invoke := func(server *Server, name string, args map[string]any) (map[string]any, bool) {
				t.Helper()
				raw, e := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
				if e != nil {
					t.Fatal(e)
				}
				var input, output bytes.Buffer
				if framed {
					fmt.Fprintf(&input, "Content-Length: %d\r\n\r\n", len(raw))
					input.Write(raw)
				} else {
					input.Write(raw)
					input.WriteByte('\n')
				}
				if e = server.Serve(context.Background(), &input, &output); e != nil {
					t.Fatal(e)
				}
				body, _, e := readMessage(bufio.NewReader(&output))
				if e != nil {
					t.Fatal(e)
				}
				var response struct {
					Error  *ResponseError `json:"error"`
					Result struct {
						IsError bool `json:"isError"`
						Content []struct {
							Text string `json:"text"`
						} `json:"content"`
					} `json:"result"`
				}
				if e = json.Unmarshal(body, &response); e != nil {
					t.Fatal(e)
				}
				if response.Error != nil || len(response.Result.Content) != 1 {
					t.Fatalf("response %s", body)
				}
				var payload map[string]any
				if e = json.Unmarshal([]byte(response.Result.Content[0].Text), &payload); e != nil {
					t.Fatal(e)
				}
				return payload, response.Result.IsError
			}
			if _, failed := invoke(New(api.URL, "incorrect"), "foliospace.list_books", map[string]any{}); !failed {
				t.Fatal("invalid bearer accepted")
			}
			if _, failed := invoke(m, "foliospace.list_books", map[string]any{"profileId": 99999}); !failed {
				t.Fatal("unknown profile fell back")
			}
			page, failed := invoke(m, "foliospace.list_books", map[string]any{"format": ".EPUB", "profileId": 1})
			if failed || page["total"] != float64(1) {
				t.Fatalf("book page %v", page)
			}
			seen := map[string]bool{}
			cursor := ""
			for {
				args := map[string]any{"q": "Synthetic", "limit": 1, "profileId": 1}
				if cursor != "" {
					args["cursor"] = cursor
				}
				page, failed = invoke(m, "foliospace.search_library", args)
				if failed {
					t.Fatalf("search %v", page)
				}
				for _, value := range page["items"].([]any) {
					item := value.(map[string]any)
					key := fmt.Sprint(item["assetType"], item["id"])
					if seen[key] {
						t.Fatal("duplicate", key)
					}
					seen[key] = true
				}
				if page["hasMore"] != true {
					break
				}
				cursor = page["nextCursor"].(string)
			}
			if len(seen) != 3 {
				t.Fatalf("cross-library coverage %v", seen)
			}
			for name, args := range map[string]map[string]any{"foliospace.diagnose_book": {"bookId": book.ID}, "foliospace.diagnose_game": {"gameId": game.ID}} {
				data, failed := invoke(m, name, args)
				if failed || data["source"] != "stored" || data["filesystemVerified"] != false {
					t.Fatalf("diagnostic %v", data)
				}
			}
			job, err := st.QueueVideoMetadata(video.ID, "Synthetic movie", "movie")
			if err != nil {
				t.Fatal(err)
			}
			claimed, found, err := st.ClaimVideoMetadataJob()
			if err != nil || !found {
				t.Fatalf("claim %v %v", found, err)
			}
			if err = st.FinishVideoMetadataJob(claimed, []domain.VideoCandidate{{ID: "42", Title: "Synthetic movie", Kind: "movie", Year: 2025}}, "", 0); err != nil {
				t.Fatal(err)
			}
			preview, failed := invoke(m, "foliospace.preview_video_metadata", map[string]any{"profileId": 1, "videoId": video.ID, "jobId": job.ID, "candidateIndex": 0})
			if failed {
				t.Fatalf("preview %v", preview)
			}
			before, err := st.VideoMetadata(video.ID)
			if err != nil || before.WorkID != 0 {
				t.Fatal("preview wrote match", before, err)
			}
			apply := map[string]any{"profileId": 1, "videoId": video.ID, "receipt": preview["receipt"]}
			if _, failed = invoke(m, "foliospace.apply_video_metadata", apply); !failed {
				t.Fatal("confirmation not required")
			}
			apply["confirm"] = true
			result, failed := invoke(m, "foliospace.apply_video_metadata", apply)
			if failed || result["scope"] != "shared-library" {
				t.Fatalf("apply %v", result)
			}
			if _, failed = invoke(m, "foliospace.apply_video_metadata", apply); !failed {
				t.Fatal("receipt replay accepted")
			}
		})
	}
}
