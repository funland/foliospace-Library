package archive

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestListPagesSortsImagesAndSkipsNonImages(t *testing.T) {
	path := makeZip(t, map[string]string{
		"002.jpg":   "two",
		"001.png":   "one",
		"notes.txt": "skip",
	})

	pages, err := ListPages(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 {
		t.Fatalf("pages len = %d, want 2", len(pages))
	}
	if pages[0].Name != "001.png" || pages[1].Name != "002.jpg" {
		t.Fatalf("pages = %#v, want sorted image pages", pages)
	}
}

func TestOpenPageStreamsExpectedBytes(t *testing.T) {
	path := makeZip(t, map[string]string{
		"002.jpg": "two",
		"001.png": "one",
	})

	page, contentType, err := OpenPage(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer page.Close()

	data, err := io.ReadAll(page)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "two" {
		t.Fatalf("page bytes = %q, want two", string(data))
	}
	if contentType != "image/jpeg" {
		t.Fatalf("content type = %q, want image/jpeg", contentType)
	}
}

func TestReadEPUBManifestAndResources(t *testing.T) {
	path := makeEPUB(t)

	manifest, err := ReadEPUBManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Title != "Sample EPUB" || manifest.Creator != "FolioSpace" || manifest.Description != "A compact EPUB metadata sample." {
		t.Fatalf("manifest metadata = %#v", manifest)
	}
	if manifest.CoverHref != "OPS/images/cover.jpg" {
		t.Fatalf("cover href = %q, want OPS/images/cover.jpg", manifest.CoverHref)
	}
	if len(manifest.Spine) != 1 || manifest.Spine[0].Href != "OPS/text/chapter1.xhtml" {
		t.Fatalf("spine = %#v, want chapter1", manifest.Spine)
	}

	pages, err := ListEPUBSpine(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 1 || pages[0].Name != "OPS/text/chapter1.xhtml" {
		t.Fatalf("pages = %#v, want epub spine page", pages)
	}

	cover, contentType, err := OpenEPUBCover(path)
	if err != nil {
		t.Fatal(err)
	}
	defer cover.Close()
	data, err := io.ReadAll(cover)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "cover" || contentType != "image/jpeg" {
		t.Fatalf("cover = %q contentType=%q, want jpeg cover", string(data), contentType)
	}
}

func makeZip(t *testing.T, entries map[string]string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "book.cbz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, body := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func makeEPUB(t *testing.T) string {
	t.Helper()
	return makeZipAt(t, filepath.Join(t.TempDir(), "book.epub"), map[string]string{
		"META-INF/container.xml": `<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OPS/package.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`,
		"OPS/package.opf": `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>Sample EPUB</dc:title>
    <dc:creator>FolioSpace</dc:creator>
    <dc:description>A compact EPUB metadata sample.</dc:description>
  </metadata>
  <manifest>
    <item id="chapter1" href="text/chapter1.xhtml" media-type="application/xhtml+xml"/>
    <item id="cover" href="images/cover.jpg" media-type="image/jpeg" properties="cover-image"/>
  </manifest>
  <spine>
    <itemref idref="chapter1"/>
  </spine>
</package>`,
		"OPS/text/chapter1.xhtml": `<html xmlns="http://www.w3.org/1999/xhtml"><body><h1>Chapter</h1></body></html>`,
		"OPS/images/cover.jpg":    "cover",
	})
}

func makeZipAt(t *testing.T, path string, entries map[string]string) string {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, body := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
