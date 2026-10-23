package service

import (
	"bytes"
	"foliospace-reader/internal/db"
	"foliospace-reader/internal/store"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCalibreCoverOverridesEmbeddedAndInvalidatesThumbnail(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "book.epub")
	if err := makeEPUBWithGuideCover(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "metadata.opf"), []byte(`<package xmlns="http://www.idpf.org/2007/opf"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>中文书名</dc:title></metadata></package>`), 0600); err != nil {
		t.Fatal(err)
	}
	coverPath := filepath.Join(root, "cover.jpg")
	writeCover := func(c color.RGBA) []byte {
		t.Helper()
		img := image.NewRGBA(image.Rect(0, 0, 20, 30))
		for y := 0; y < 30; y++ {
			for x := 0; x < 20; x++ {
				img.Set(x, y, c)
			}
		}
		var data bytes.Buffer
		if err := jpeg.Encode(&data, img, nil); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(coverPath, data.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		return data.Bytes()
	}
	original := writeCover(color.RGBA{R: 255, A: 255})
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	library, _ := st.CreateLibrary("Books", root)
	series, _ := st.UpsertSeries(library.ID, "Author", ".")
	book, _ := st.UpsertBook(series.ID, "Title", "epub")
	info, _ := os.Stat(path)
	if _, err = st.UpsertFile(book.ID, library.ID, path, "book.epub", info.Size(), info.ModTime(), ".epub"); err != nil {
		t.Fatal(err)
	}
	book, err = st.BookByID(book.ID)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	opened, err := svc.OpenCover(book.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(opened.Body)
	opened.Body.Close()
	if !bytes.Equal(got, original) {
		t.Fatal("Calibre cover was not preferred")
	}
	thumb, err := svc.openBookThumbnailSource(book)
	if err != nil {
		t.Fatal(err)
	}
	got, _ = io.ReadAll(thumb.Body)
	thumb.Body.Close()
	if !bytes.Equal(got, original) {
		t.Fatal("thumbnail did not share cover source")
	}
	key1, err := svc.bookThumbnailCacheKey(book, "small")
	if err != nil {
		t.Fatal(err)
	}
	writeCover(color.RGBA{B: 255, A: 255})
	future := time.Now().Add(time.Second)
	if err = os.Chtimes(coverPath, future, future); err != nil {
		t.Fatal(err)
	}
	key2, err := svc.bookThumbnailCacheKey(book, "small")
	if err != nil || key1 == key2 {
		t.Fatalf("cover cache not invalidated: %q %q %v", key1, key2, err)
	}
}
