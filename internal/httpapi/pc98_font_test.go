package httpapi

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"foliospace-reader/internal/db"
	"foliospace-reader/internal/scanner"
	"foliospace-reader/internal/service"
	"foliospace-reader/internal/store"
)

func TestPC98PerVersionFontRefreshManifestAndDownload(t *testing.T) {
	root := filepath.Join(t.TempDir(), "PC98")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	write := func(name string, b []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	bitmap := func(fill byte) []byte {
		b := bytes.Repeat([]byte{fill}, 524350)
		copy(b, make([]byte, 62))
		copy(b, "BM")
		binary.LittleEndian.PutUint32(b[10:14], 62)
		binary.LittleEndian.PutUint32(b[14:18], 40)
		binary.LittleEndian.PutUint32(b[18:22], 2048)
		binary.LittleEndian.PutUint32(b[22:26], 2048)
		binary.LittleEndian.PutUint16(b[26:28], 1)
		binary.LittleEndian.PutUint16(b[28:30], 1)
		return b
	}
	write("GameCN.hdi", syntheticPC98AnexImageForAPI(512, 4, 2, 10))
	jp := syntheticPC98AnexImageForAPI(512, 4, 2, 10)
	jp[len(jp)-1] = 1
	write("GameJP.hdi", jp)
	write("font.bmp", bitmap(0))
	dedicated := bitmap(255)
	write("Font_CN_Specific.BMP", dedicated)
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("PC98", root, "game")
	if err != nil {
		t.Fatal(err)
	}
	scan := scanner.New(st)
	if job, err := scan.ScanLibrary(lib); err != nil || job.ErrorCount != 0 {
		t.Fatalf("initial scan: %#v %v", job, err)
	}
	cn, err := st.GameByPath(filepath.Join(root, "GameCN.hdi"))
	if err != nil {
		t.Fatal(err)
	}
	japan, err := st.GameByPath(filepath.Join(root, "GameJP.hdi"))
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(NewWithOptions(service.New(st), nil, Options{APIToken: "secret"}).Routes())
	defer ts.Close()
	manifestPath := fmt.Sprintf("/api/client/games/%d/manifest", cn.ID)
	before := authGet(t, ts.URL+manifestPath, "secret")
	jpBefore := authGet(t, fmt.Sprintf("%s/api/client/games/%d/manifest", ts.URL, japan.ID), "secret")
	write("gamecn.HDI.font.JSON", []byte(`{"file":"font_cn_specific.bmp"}`))
	if job, err := scan.ScanLibraryPath(lib, filepath.Join(root, "GameCN.hdi")); err != nil || job.ErrorCount != 0 || job.IndexedFiles != 1 {
		t.Fatalf("targeted refresh: %#v %v", job, err)
	}
	after := authGet(t, ts.URL+manifestPath, "secret")
	if after == before {
		t.Fatal("existing manifest did not change")
	}
	if got := authGet(t, fmt.Sprintf("%s/api/client/games/%d/manifest", ts.URL, japan.ID), "secret"); got != jpBefore {
		t.Fatal("JP manifest changed")
	}
	refreshed, err := st.GameByPath(cn.FilePath)
	if err != nil || refreshed.ID != cn.ID || refreshed.SHA1 != cn.SHA1 {
		t.Fatalf("ROM identity changed: %#v %v", refreshed, err)
	}
	var manifest clientGameManifestResponse
	if err := json.Unmarshal([]byte(after), &manifest); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, f := range manifest.Files {
		if f.Role != "font" {
			continue
		}
		count++
		want := fmt.Sprintf("sha1:%x", sha1.Sum(dedicated))
		if f.Name != "FONT.bmp" || f.Checksum != want || f.Size != int64(len(dedicated)) {
			t.Fatalf("font manifest: %#v", f)
		}
		for _, ranged := range []bool{false, true} {
			req, _ := http.NewRequest(http.MethodGet, ts.URL+f.URL, nil)
			req.Header.Set("Authorization", "Bearer secret")
			wantBody := dedicated
			status := 200
			if ranged {
				req.Header.Set("Range", "bytes=0-15")
				wantBody = dedicated[:16]
				status = 206
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			b, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || resp.StatusCode != status || !bytes.Equal(b, wantBody) {
				t.Fatalf("font download status=%d err=%v", resp.StatusCode, err)
			}
		}
	}
	if count != 1 {
		t.Fatalf("font count=%d", count)
	}
}
