package bookmeta

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func calibreFixture(t *testing.T, root string) {
	t.Helper()
	conn, err := sql.Open("sqlite", filepath.Join(root, "metadata.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for _, statement := range []string{
		`CREATE TABLE books(id INTEGER PRIMARY KEY, path TEXT, title TEXT)`,
		`CREATE TABLE data(id INTEGER PRIMARY KEY, book INTEGER, name TEXT, format TEXT)`,
		`CREATE TABLE comments(book INTEGER, text TEXT)`,
		`CREATE TABLE authors(id INTEGER PRIMARY KEY, name TEXT)`,
		`CREATE TABLE books_authors_link(id INTEGER PRIMARY KEY, book INTEGER, author INTEGER)`,
		`CREATE TABLE tags(id INTEGER PRIMARY KEY, name TEXT)`,
		`CREATE TABLE books_tags_link(id INTEGER PRIMARY KEY, book INTEGER, tag INTEGER)`,
		`INSERT INTO books VALUES(7, 'Writer/Book (999)', 'Database title')`,
		`INSERT INTO data VALUES(1, 7, 'exact filename', 'EPUB')`,
		`INSERT INTO data VALUES(2, 7, 'exact filename', 'PDF')`,
		`INSERT INTO comments VALUES(7, '<p>First &amp; second.</p><p>Next.</p>')`,
		`INSERT INTO authors VALUES(1, 'One'), (2, 'Two')`,
		`INSERT INTO books_authors_link VALUES(1, 7, 2), (2, 7, 1)`,
		`INSERT INTO tags VALUES(1, 'tag one'), (2, 'tag two')`,
		`INSERT INTO books_tags_link VALUES(1, 7, 1), (2, 7, 2)`,
	} {
		if _, err := conn.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
}

func opfFixture(title string) string {
	return `<package xmlns="http://www.idpf.org/2007/opf"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>` + title + `</dc:title><dc:creator>First</dc:creator><dc:creator>Second</dc:creator><dc:description>&lt;p&gt;An &amp;amp; account.&lt;/p&gt;</dc:description><dc:subject>one</dc:subject><dc:subject>two</dc:subject><dc:subject>one</dc:subject></metadata></package>`
}

func sourceSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	snapshot := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		snapshot[path] = fmt.Sprintf("%x|%d|%d|%s", sha256.Sum256(data), info.Size(), info.ModTime().UnixNano(), info.Mode())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestDatabaseUsesExactFormatPathsWithoutChangingSource(t *testing.T) {
	root := t.TempDir()
	calibreFixture(t, root)
	path := filepath.Join(root, "Writer", "Book (999)", "exact filename.epub")
	writeTestFile(t, path, "book")
	writeTestFile(t, strings.TrimSuffix(path, ".epub")+".pdf", "pdf")
	wrong := filepath.Join(filepath.Dir(path), "wrong.epub")
	writeTestFile(t, wrong, "wrong")
	before := sourceSnapshot(t, root)
	resolver := NewResolver(root)
	for _, bookPath := range []string{path, strings.TrimSuffix(path, ".epub") + ".pdf"} {
		got, found, err := resolver.Lookup(bookPath)
		if err != nil || !found {
			t.Fatalf("found=%v err=%v", found, err)
		}
		if got.Title != "Database title" || got.Creator != "Two, One" || got.Description != "First & second. Next." || !reflect.DeepEqual(got.Tags, []string{"tag one", "tag two"}) || got.Source != "calibre-db" {
			t.Fatalf("metadata=%#v", got)
		}
	}
	if got, found, err := resolver.Lookup(wrong); err != nil || found {
		t.Fatalf("unmapped file got=%#v found=%v err=%v", got, found, err)
	}
	if after := sourceSnapshot(t, root); !reflect.DeepEqual(before, after) {
		t.Fatalf("source files changed: before=%v after=%v", before, after)
	}
}

func TestOPFFallbackAndPerScanRefresh(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Book", "book.cbz")
	writeTestFile(t, path, "book")
	writeTestFile(t, filepath.Join(root, "metadata.db"), "invalid database")
	opf := filepath.Join(filepath.Dir(path), "metadata.opf")
	writeTestFile(t, opf, opfFixture("Sidecar title"))
	resolver := NewResolver(root)
	got, found, warning := resolver.Lookup(path)
	if !found || warning == nil || got.Title != "Sidecar title" || got.Creator != "First, Second" || got.Description != "An & account." || !reflect.DeepEqual(got.Tags, []string{"one", "two"}) {
		t.Fatalf("metadata=%#v found=%v warning=%v", got, found, warning)
	}
	writeTestFile(t, opf, opfFixture("Changed sidecar"))
	if got, _, _ := NewResolver(root).Lookup(path); got.Title != "Changed sidecar" {
		t.Fatalf("new scan saw %#v", got)
	}
}

func TestActiveJournalUsesOPFAndLeavesSourceUntouched(t *testing.T) {
	for _, suffix := range []string{"-wal", "-journal"} {
		t.Run(suffix, func(t *testing.T) {
			root := t.TempDir()
			calibreFixture(t, root)
			path := filepath.Join(root, "Writer", "Book (999)", "exact filename.epub")
			writeTestFile(t, path, "book")
			writeTestFile(t, filepath.Join(filepath.Dir(path), "metadata.opf"), opfFixture("OPF title"))
			writeTestFile(t, filepath.Join(root, "metadata.db")+suffix, "active journal")
			before := sourceSnapshot(t, root)
			got, found, warning := NewResolver(root).Lookup(path)
			if !found || warning == nil || got.Title != "OPF title" {
				t.Fatalf("got=%#v found=%v warning=%v", got, found, warning)
			}
			if !reflect.DeepEqual(before, sourceSnapshot(t, root)) {
				t.Fatal("source changed")
			}
		})
	}
}

func TestOPFRejectsAmbiguityDirectivesOversizeAndSymlinks(t *testing.T) {
	for _, variant := range []string{"ambiguous", "directive", "oversize", "symlink", "parent-symlink", "wrong-namespace"} {
		t.Run(variant, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "Book", "book.cbz")
			opf := filepath.Join(filepath.Dir(path), "metadata.opf")
			writeTestFile(t, path, "book")
			writeTestFile(t, opf, opfFixture("Safe"))
			switch variant {
			case "wrong-namespace":
				writeTestFile(t, opf, strings.ReplaceAll(opfFixture("Wrong"), "http://purl.org/dc/elements/1.1/", "urn:wrong"))
			case "ambiguous":
				writeTestFile(t, filepath.Join(filepath.Dir(path), "other.pdf"), "other")
			case "directive":
				writeTestFile(t, opf, `<!DOCTYPE package [<!ENTITY x SYSTEM "file:///etc/passwd">]>`+opfFixture("Unsafe"))
			case "oversize":
				if err := os.Truncate(opf, maxOPFBytes+1); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				outside := filepath.Join(t.TempDir(), "outside.opf")
				writeTestFile(t, outside, opfFixture("Outside"))
				if err := os.Remove(opf); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, opf); err != nil {
					t.Fatal(err)
				}
			case "parent-symlink":
				alias := filepath.Join(root, "Alias")
				if err := os.Symlink(filepath.Dir(path), alias); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(alias, "book.cbz")
			}
			if _, found, err := NewResolver(root).Lookup(path); found || err == nil {
				t.Fatalf("unsafe source found=%v err=%v", found, err)
			}
		})
	}
}

func TestDatabaseTraversalDoesNotMatchAndOPFSupportsMultipleFormats(t *testing.T) {
	root := t.TempDir()
	calibreFixture(t, root)
	conn, err := sql.Open("sqlite", filepath.Join(root, "metadata.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`UPDATE books SET path = '../outside'`); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	path := filepath.Join(root, "Book", "book.epub")
	writeTestFile(t, path, "book")
	writeTestFile(t, filepath.Join(filepath.Dir(path), "book.pdf"), "pdf")
	writeTestFile(t, filepath.Join(filepath.Dir(path), "metadata.opf"), opfFixture("Formats"))
	if got, found, err := NewResolver(root).Lookup(path); err != nil || !found || got.Title != "Formats" {
		t.Fatalf("got=%#v found=%v err=%v", got, found, err)
	}
}

func TestCoverInfoRefreshesWithoutOpeningDatabaseAndRejectsEscape(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Book", "book.epub")
	writeTestFile(t, path, "book")
	writeTestFile(t, filepath.Join(filepath.Dir(path), "metadata.opf"), opfFixture("Cover"))
	coverPath := filepath.Join(filepath.Dir(path), "cover.jpg")
	firstJPEG := testJPEG(t, color.RGBA{R: 255, A: 255})
	secondJPEG := testJPEG(t, color.RGBA{B: 255, A: 255})
	writeTestFile(t, coverPath, firstJPEG)
	first, err := CoverInfo(root, path)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, coverPath, secondJPEG)
	mtime := time.Now().Add(time.Second)
	if err := os.Chtimes(coverPath, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	second, err := CoverInfo(root, path)
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision == second.Revision {
		t.Fatal("cover revision was stale")
	}
	body, mime, revision, err := OpenCover(root, path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(body)
	body.Close()
	if err != nil || mime != "image/jpeg" || revision != second.Revision || string(data) != secondJPEG {
		t.Fatalf("mime=%q revision=%q body=%q err=%v", mime, revision, data, err)
	}
	outside := filepath.Join(t.TempDir(), "outside.jpg")
	writeTestFile(t, outside, "\xff\xd8\xffoutside")
	if err := os.Remove(coverPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, coverPath); err != nil {
		t.Fatal(err)
	}
	if _, err := CoverInfo(root, path); err == nil {
		t.Fatal("cached cover bypassed symlink validation")
	}
}

func testJPEG(t *testing.T, c color.RGBA) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, c)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestCoverInfoRejectsTruncatedJPEG(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Book", "book.epub")
	writeTestFile(t, path, "book")
	writeTestFile(t, filepath.Join(filepath.Dir(path), "metadata.opf"), opfFixture("Book"))
	for _, data := range []string{"\xff\xd8\xff", "\xff\xd8\xffinvalid", testJPEG(t, color.RGBA{A: 255})[:30]} {
		writeTestFile(t, filepath.Join(filepath.Dir(path), "cover.jpg"), data)
		if _, err := CoverInfo(root, path); err == nil {
			t.Fatal("invalid JPEG accepted")
		}
		if body, _, _, err := OpenCover(root, path); err == nil {
			body.Close()
			t.Fatal("invalid JPEG streamed")
		}
	}
}

func TestCoverInfoRejectsTruncatedImageWithValidJPEGHeader(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x*31 + y*17), G: uint8(x*11 + y*29), B: uint8(x*7 + y*43), A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	truncated := encoded.Bytes()[:encoded.Len()/2]
	if _, err := jpeg.DecodeConfig(bytes.NewReader(truncated)); err != nil {
		t.Fatalf("fixture must have a valid JPEG header: %v", err)
	}
	if _, err := jpeg.Decode(bytes.NewReader(truncated)); err == nil {
		t.Fatal("fixture must have truncated image data")
	}
	root := t.TempDir()
	path := filepath.Join(root, "Book", "book.epub")
	writeTestFile(t, path, "book")
	writeTestFile(t, filepath.Join(filepath.Dir(path), "metadata.opf"), opfFixture("Book"))
	writeTestFile(t, filepath.Join(filepath.Dir(path), "cover.jpg"), string(truncated))
	if _, err := CoverInfo(root, path); err == nil {
		t.Fatal("valid header concealed truncated JPEG image data")
	}
	if body, _, _, err := OpenCover(root, path); err == nil {
		body.Close()
		t.Fatal("truncated JPEG image data streamed")
	}
}
