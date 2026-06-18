package archive

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
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

func TestListPagesSkipsMacOSResourceForkEntries(t *testing.T) {
	path := makeZip(t, map[string]string{
		"Book/001.png":            "one",
		"Book/002.jpg":            "two",
		"Book/._001.png":          "resource fork",
		"__MACOSX/Book/._001.png": "resource fork",
		"__MACOSX/Book/._002.jpg": "resource fork",
	})

	pages, err := ListPages(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 {
		t.Fatalf("pages len = %d, want 2", len(pages))
	}
	if pages[0].Name != "Book/001.png" || pages[1].Name != "Book/002.jpg" {
		t.Fatalf("pages = %#v, want only real image pages", pages)
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

func TestOpenCoverPrefersPortraitCoverAfterLandscapeCoverZero(t *testing.T) {
	path := makeZipBytes(t, map[string][]byte{
		"0001_cover0.jpg":    makeJPEG(t, 400, 210, color.RGBA{R: 200, G: 40, B: 40, A: 255}),
		"0002_cover1.jpg":    makeJPEG(t, 400, 560, color.RGBA{R: 40, G: 170, B: 80, A: 255}),
		"0003_01_01.jpg":     makeJPEG(t, 400, 4000, color.RGBA{R: 40, G: 60, B: 200, A: 255}),
		"metadata/notes.txt": []byte("skip"),
	})

	pages, err := ListPages(path)
	if err != nil {
		t.Fatal(err)
	}
	if pages[0].Name != "0001_cover0.jpg" {
		t.Fatalf("first reading page = %q, want original archive order preserved", pages[0].Name)
	}

	info, err := CoverInfo(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.FirstName != "0001_cover0.jpg" || info.Name != "0002_cover1.jpg" {
		t.Fatalf("cover info = %#v, want portrait cover1 selected while first page is cover0", info)
	}

	cover, contentType, err := OpenCover(path)
	if err != nil {
		t.Fatal(err)
	}
	defer cover.Close()
	data, err := io.ReadAll(cover)
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "image/jpeg" {
		t.Fatalf("content type = %q, want image/jpeg", contentType)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 400 || img.Bounds().Dy() != 560 {
		t.Fatalf("cover bounds = %v, want selected portrait cover", img.Bounds())
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

func TestReadEPUBManifestUsesEPUB2GuideCover(t *testing.T) {
	path := makeEPUB2GuideCover(t)

	manifest, err := ReadEPUBManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.CoverHref != "OPS/images/legacy-cover.jpg" {
		t.Fatalf("cover href = %q, want EPUB 2 guide cover image", manifest.CoverHref)
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
	if string(data) != "legacy cover" || contentType != "image/jpeg" {
		t.Fatalf("cover = %q contentType=%q, want jpeg guide cover", string(data), contentType)
	}
}

func TestReadEPUBManifestPreservesEPUB3NestedNavigation(t *testing.T) {
	path := makeNestedEPUB3Nav(t)

	manifest, err := ReadEPUBManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.TOCTree) != 1 {
		t.Fatalf("toc tree len = %d, want 1", len(manifest.TOCTree))
	}
	genesis := manifest.TOCTree[0]
	if genesis.Label != "GENESIS" || genesis.Href != "OPS/ch004.xhtml#v01000000" || genesis.Index != 0 {
		t.Fatalf("genesis toc item = %#v", genesis)
	}
	if len(genesis.Children) != 2 {
		t.Fatalf("genesis children len = %d, want 2", len(genesis.Children))
	}
	if genesis.Children[1].Label != "Chapter 2" || genesis.Children[1].Href != "OPS/ch004.xhtml#v01002001" || genesis.Children[1].Index != 0 {
		t.Fatalf("chapter 2 toc item = %#v", genesis.Children[1])
	}
	if len(manifest.TOC) != 3 || manifest.TOC[0].Label != "GENESIS" || manifest.TOC[2].Label != "Chapter 2" {
		t.Fatalf("flat toc = %#v, want preorder-compatible flat toc", manifest.TOC)
	}
}

func TestReadEPUBManifestPreservesEPUB2NestedNCX(t *testing.T) {
	path := makeNestedEPUB2NCX(t)

	manifest, err := ReadEPUBManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.TOCTree) != 1 {
		t.Fatalf("toc tree len = %d, want 1", len(manifest.TOCTree))
	}
	novel := manifest.TOCTree[0]
	if novel.Label != "Collected Novel" || novel.Href != "OPS/book-contents.xhtml" || novel.Index != 0 {
		t.Fatalf("novel toc item = %#v", novel)
	}
	if len(novel.Children) != 2 {
		t.Fatalf("novel children len = %d, want 2", len(novel.Children))
	}
	if novel.Children[0].Label != "Chapter 1. The Riverbank" || novel.Children[0].Href != "OPS/chapter-1.xhtml#chapter-1" || novel.Children[0].Index != 1 {
		t.Fatalf("chapter 1 toc item = %#v", novel.Children[0])
	}
	if len(manifest.TOC) != 3 || manifest.TOC[2].Label != "Chapter 2. The Storm Cellar" {
		t.Fatalf("flat toc = %#v, want parent and child entries", manifest.TOC)
	}
}

func TestReadEPUBManifestExpandsEPUB2ContentsPageChapters(t *testing.T) {
	path := makeEPUB2ContentsPageTOC(t)

	manifest, err := ReadEPUBManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.TOCTree) != 1 {
		t.Fatalf("toc tree len = %d, want 1", len(manifest.TOCTree))
	}
	novel := manifest.TOCTree[0]
	if novel.Label != "Collected Novel" || len(novel.Children) != 2 {
		t.Fatalf("novel toc item = %#v, want generated chapter children", novel)
	}
	if novel.Children[0].Label != "CHAPTER I. The Riverbank at Dawn" || novel.Children[0].Href != "OPS/chapter-1.xhtml#c1" {
		t.Fatalf("generated chapter 1 = %#v", novel.Children[0])
	}
	if novel.Children[1].Label != "CHAPTER II. The Storm Cellar" || novel.Children[1].Href != "OPS/chapter-2.xhtml#c2" {
		t.Fatalf("generated chapter 2 = %#v", novel.Children[1])
	}
	if len(manifest.TOC) != 3 || manifest.TOC[1].Label != "CHAPTER I. The Riverbank at Dawn" {
		t.Fatalf("flat toc = %#v, want generated children included", manifest.TOC)
	}
}

func makeZip(t *testing.T, entries map[string]string) string {
	t.Helper()
	byteEntries := make(map[string][]byte, len(entries))
	for name, body := range entries {
		byteEntries[name] = []byte(body)
	}
	return makeZipBytes(t, byteEntries)
}

func makeZipBytes(t *testing.T, entries map[string][]byte) string {
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
		if _, err := entry.Write(body); err != nil {
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

func makeJPEG(t *testing.T, width int, height int, fill color.RGBA) []byte {
	t.Helper()
	var body bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, fill)
		}
	}
	if err := jpeg.Encode(&body, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	return body.Bytes()
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

func makeEPUB2GuideCover(t *testing.T) string {
	t.Helper()
	return makeZipAt(t, filepath.Join(t.TempDir(), "legacy-cover.epub"), map[string]string{
		"META-INF/container.xml": `<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OPS/package.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`,
		"OPS/package.opf": `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>Legacy Guide Cover EPUB</dc:title>
  </metadata>
  <manifest>
    <item id="cover" href="cover.xhtml" media-type="application/xhtml+xml"/>
    <item id="chapter1" href="text/chapter1.xhtml" media-type="application/xhtml+xml"/>
    <item id="cover-image-file" href="images/legacy-cover.jpg" media-type="image/jpeg"/>
  </manifest>
  <spine>
    <itemref idref="cover"/>
    <itemref idref="chapter1"/>
  </spine>
  <guide>
    <reference type="cover" title="Cover Page" href="images/legacy-cover.jpg"/>
  </guide>
</package>`,
		"OPS/cover.xhtml":             `<html xmlns="http://www.w3.org/1999/xhtml"><body><img src="images/legacy-cover.jpg"/></body></html>`,
		"OPS/text/chapter1.xhtml":     `<html xmlns="http://www.w3.org/1999/xhtml"><body><h1>Chapter</h1></body></html>`,
		"OPS/images/legacy-cover.jpg": "legacy cover",
	})
}

func makeNestedEPUB3Nav(t *testing.T) string {
	t.Helper()
	return makeZipAt(t, filepath.Join(t.TempDir(), "nested-nav.epub"), map[string]string{
		"META-INF/container.xml": `<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OPS/package.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`,
		"OPS/package.opf": `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>Nested EPUB 3</dc:title>
  </metadata>
  <manifest>
    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
    <item id="genesis" href="ch004.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine>
    <itemref idref="genesis"/>
  </spine>
</package>`,
		"OPS/nav.xhtml": `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops">
  <body>
    <nav epub:type="toc">
      <ol>
        <li>
          <a href="ch004.xhtml#v01000000">GENESIS</a>
          <ol>
            <li><a href="ch004.xhtml#v01001001">Chapter 1</a></li>
            <li><a href="ch004.xhtml#v01002001">Chapter 2</a></li>
          </ol>
        </li>
      </ol>
    </nav>
  </body>
</html>`,
		"OPS/ch004.xhtml": `<html xmlns="http://www.w3.org/1999/xhtml"><body><h1 id="v01000000">GENESIS</h1><h2 id="v01001001">Chapter 1</h2><h2 id="v01002001">Chapter 2</h2></body></html>`,
	})
}

func makeNestedEPUB2NCX(t *testing.T) string {
	t.Helper()
	return makeZipAt(t, filepath.Join(t.TempDir(), "nested-ncx.epub"), map[string]string{
		"META-INF/container.xml": `<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OPS/package.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`,
		"OPS/package.opf": `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>Nested EPUB 2</dc:title>
  </metadata>
  <manifest>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
    <item id="contents" href="book-contents.xhtml" media-type="application/xhtml+xml"/>
    <item id="chapter1" href="chapter-1.xhtml" media-type="application/xhtml+xml"/>
    <item id="chapter2" href="chapter-2.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine toc="ncx">
    <itemref idref="contents"/>
    <itemref idref="chapter1"/>
    <itemref idref="chapter2"/>
  </spine>
</package>`,
		"OPS/toc.ncx": `<?xml version="1.0" encoding="UTF-8"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/">
  <navMap>
    <navPoint id="novel">
      <navLabel><text>Collected Novel</text></navLabel>
      <content src="book-contents.xhtml"/>
      <navPoint id="chapter-1">
        <navLabel><text>Chapter 1. The Riverbank</text></navLabel>
        <content src="chapter-1.xhtml#chapter-1"/>
      </navPoint>
      <navPoint id="chapter-2">
        <navLabel><text>Chapter 2. The Storm Cellar</text></navLabel>
        <content src="chapter-2.xhtml#chapter-2"/>
      </navPoint>
    </navPoint>
  </navMap>
</ncx>`,
		"OPS/book-contents.xhtml": `<html xmlns="http://www.w3.org/1999/xhtml"><body><h1>Collected Novel</h1></body></html>`,
		"OPS/chapter-1.xhtml":     `<html xmlns="http://www.w3.org/1999/xhtml"><body><h1 id="chapter-1">Chapter 1</h1></body></html>`,
		"OPS/chapter-2.xhtml":     `<html xmlns="http://www.w3.org/1999/xhtml"><body><h1 id="chapter-2">Chapter 2</h1></body></html>`,
	})
}

func makeEPUB2ContentsPageTOC(t *testing.T) string {
	t.Helper()
	return makeZipAt(t, filepath.Join(t.TempDir(), "contents-page-toc.epub"), map[string]string{
		"META-INF/container.xml": `<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OPS/package.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`,
		"OPS/package.opf": `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>Contents Page EPUB 2</dc:title>
  </metadata>
  <manifest>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
    <item id="contents" href="book-contents.xhtml" media-type="application/xhtml+xml"/>
    <item id="chapter1" href="chapter-1.xhtml" media-type="application/xhtml+xml"/>
    <item id="chapter2" href="chapter-2.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine toc="ncx">
    <itemref idref="contents"/>
    <itemref idref="chapter1"/>
    <itemref idref="chapter2"/>
  </spine>
</package>`,
		"OPS/toc.ncx": `<?xml version="1.0" encoding="UTF-8"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/">
  <navMap>
    <navPoint id="novel">
      <navLabel><text>Collected Novel</text></navLabel>
      <content src="book-contents.xhtml"/>
    </navPoint>
  </navMap>
</ncx>`,
		"OPS/book-contents.xhtml": `<html xmlns="http://www.w3.org/1999/xhtml"><body>
  <p><a href="main-contents.xhtml">back to main contents</a></p>
  <p>
    <a href="chapter-1.xhtml#c1">CHAPTER I.</a>
    <span>The Riverbank at Dawn</span>
    <a href="chapter-2.xhtml#c2">CHAPTER II.</a>
    <span>The Storm Cellar</span>
  </p>
</body></html>`,
		"OPS/chapter-1.xhtml": `<html xmlns="http://www.w3.org/1999/xhtml"><body><h1 id="c1">Chapter 1</h1></body></html>`,
		"OPS/chapter-2.xhtml": `<html xmlns="http://www.w3.org/1999/xhtml"><body><h1 id="c2">Chapter 2</h1></body></html>`,
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
