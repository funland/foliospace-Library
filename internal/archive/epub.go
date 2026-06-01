package archive

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"mime"
	"path"
	"path/filepath"
	"strings"

	"foliospace-reader/internal/domain"
)

func ListEPUBSpine(filePath string) ([]domain.Page, error) {
	manifest, err := ReadEPUBManifest(filePath)
	if err != nil {
		return nil, err
	}
	if len(manifest.Spine) == 0 {
		return nil, fmt.Errorf("epub has no spine items")
	}
	pages := make([]domain.Page, 0, len(manifest.Spine))
	for _, item := range manifest.Spine {
		pages = append(pages, domain.Page{Index: item.Index, Name: item.Href})
	}
	return pages, nil
}

func ReadEPUBManifest(filePath string) (domain.EPUBManifest, error) {
	reader, err := zip.OpenReader(filePath)
	if err != nil {
		return domain.EPUBManifest{}, fmt.Errorf("open epub: %w", err)
	}
	defer reader.Close()
	return readEPUBManifest(reader.File)
}

func OpenEPUBResource(filePath string, resourcePath string) (io.ReadCloser, string, error) {
	reader, err := zip.OpenReader(filePath)
	if err != nil {
		return nil, "", fmt.Errorf("open epub: %w", err)
	}
	cleanPath := cleanEPUBPath(resourcePath)
	if cleanPath == "" {
		_ = reader.Close()
		return nil, "", fmt.Errorf("epub resource path is required")
	}
	for _, file := range reader.File {
		if cleanEPUBPath(file.Name) != cleanPath || file.FileInfo().IsDir() {
			continue
		}
		body, err := file.Open()
		if err != nil {
			_ = reader.Close()
			return nil, "", fmt.Errorf("open epub resource: %w", err)
		}
		return &zipPageReadCloser{ReadCloser: body, closeReader: reader.Close}, epubContentType(file.Name), nil
	}
	_ = reader.Close()
	return nil, "", fmt.Errorf("epub resource %q not found", resourcePath)
}

func OpenEPUBCover(filePath string) (io.ReadCloser, string, error) {
	manifest, err := ReadEPUBManifest(filePath)
	if err != nil {
		return nil, "", err
	}
	if manifest.CoverHref == "" {
		return nil, "", fmt.Errorf("epub cover not found")
	}
	return OpenEPUBResource(filePath, manifest.CoverHref)
}

func readEPUBManifest(files []*zip.File) (domain.EPUBManifest, error) {
	containerBytes, err := readZipText(files, "META-INF/container.xml")
	if err != nil {
		return domain.EPUBManifest{}, err
	}
	var container epubContainer
	if err := xml.Unmarshal([]byte(containerBytes), &container); err != nil {
		return domain.EPUBManifest{}, fmt.Errorf("parse container.xml: %w", err)
	}
	if len(container.Rootfiles) == 0 || container.Rootfiles[0].FullPath == "" {
		return domain.EPUBManifest{}, fmt.Errorf("epub container has no rootfile")
	}

	opfPath := cleanEPUBPath(container.Rootfiles[0].FullPath)
	opfBytes, err := readZipText(files, opfPath)
	if err != nil {
		return domain.EPUBManifest{}, err
	}
	var pkg epubPackage
	if err := xml.Unmarshal([]byte(opfBytes), &pkg); err != nil {
		return domain.EPUBManifest{}, fmt.Errorf("parse opf: %w", err)
	}

	opfDir := path.Dir(opfPath)
	if opfDir == "." {
		opfDir = ""
	}
	itemsByID := map[string]epubManifestItem{}
	for _, item := range pkg.Manifest.Items {
		item.Href = resolveEPUBHref(opfDir, item.Href)
		itemsByID[item.ID] = item
	}

	coverID := ""
	for _, meta := range pkg.Metadata.Meta {
		if strings.EqualFold(meta.Name, "cover") {
			coverID = meta.Content
			break
		}
	}
	coverHref := ""
	for _, item := range itemsByID {
		if coverHref == "" && coverID != "" && item.ID == coverID {
			coverHref = item.Href
		}
		if coverHref == "" && strings.Contains(item.Properties, "cover-image") {
			coverHref = item.Href
		}
	}

	spine := make([]domain.EPUBSpineItem, 0, len(pkg.Spine.Itemrefs))
	for _, itemref := range pkg.Spine.Itemrefs {
		item, ok := itemsByID[itemref.IDRef]
		if !ok {
			continue
		}
		spine = append(spine, domain.EPUBSpineItem{
			Index:     len(spine),
			ID:        item.ID,
			Href:      item.Href,
			MediaType: item.MediaType,
		})
	}

	return domain.EPUBManifest{
		Title:       strings.TrimSpace(pkg.Metadata.Title),
		Creator:     strings.TrimSpace(pkg.Metadata.Creator),
		Description: strings.TrimSpace(pkg.Metadata.Description),
		CoverHref:   coverHref,
		Spine:       spine,
		TOC:         epubTOC(files, opfDir, pkg, spine, itemsByID),
	}, nil
}

func epubTOC(files []*zip.File, opfDir string, pkg epubPackage, spine []domain.EPUBSpineItem, itemsByID map[string]epubManifestItem) []domain.EPUBTOCItem {
	hrefToIndex := map[string]int{}
	for _, item := range spine {
		hrefToIndex[stripEPUBFragment(item.Href)] = item.Index
	}

	for _, item := range pkg.Manifest.Items {
		if !strings.Contains(item.Properties, "nav") {
			continue
		}
		navPath := resolveEPUBHref(opfDir, item.Href)
		body, err := readZipText(files, navPath)
		if err == nil {
			return parseEPUBNavTOC(body, path.Dir(navPath), hrefToIndex)
		}
	}

	if pkg.Spine.Toc != "" {
		if item, ok := itemsByID[pkg.Spine.Toc]; ok {
			body, err := readZipText(files, item.Href)
			if err == nil {
				return parseEPUBNCXTOC(body, path.Dir(item.Href), hrefToIndex)
			}
		}
	}
	return nil
}

func parseEPUBNavTOC(body string, baseDir string, hrefToIndex map[string]int) []domain.EPUBTOCItem {
	decoder := xml.NewDecoder(strings.NewReader(body))
	var out []domain.EPUBTOCItem
	var activeHref string
	var activeText strings.Builder
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch value := token.(type) {
		case xml.StartElement:
			if value.Name.Local != "a" {
				continue
			}
			activeHref = ""
			activeText.Reset()
			for _, attr := range value.Attr {
				if attr.Name.Local == "href" {
					activeHref = resolveEPUBHref(baseDir, attr.Value)
				}
			}
		case xml.CharData:
			if activeHref != "" {
				activeText.Write([]byte(value))
			}
		case xml.EndElement:
			if value.Name.Local != "a" || activeHref == "" {
				continue
			}
			label := strings.TrimSpace(activeText.String())
			if label == "" {
				label = activeHref
			}
			if index, ok := hrefToIndex[stripEPUBFragment(activeHref)]; ok {
				out = append(out, domain.EPUBTOCItem{Label: label, Href: activeHref, Index: index})
			}
			activeHref = ""
			activeText.Reset()
		}
	}
	return out
}

func parseEPUBNCXTOC(body string, baseDir string, hrefToIndex map[string]int) []domain.EPUBTOCItem {
	decoder := xml.NewDecoder(strings.NewReader(body))
	var out []domain.EPUBTOCItem
	var inText bool
	var label strings.Builder
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch value := token.(type) {
		case xml.StartElement:
			if value.Name.Local == "text" {
				inText = true
				label.Reset()
			}
			if value.Name.Local == "content" {
				for _, attr := range value.Attr {
					if attr.Name.Local != "src" {
						continue
					}
					href := resolveEPUBHref(baseDir, attr.Value)
					if index, ok := hrefToIndex[stripEPUBFragment(href)]; ok {
						text := strings.TrimSpace(label.String())
						if text == "" {
							text = href
						}
						out = append(out, domain.EPUBTOCItem{Label: text, Href: href, Index: index})
					}
				}
			}
		case xml.CharData:
			if inText {
				label.Write([]byte(value))
			}
		case xml.EndElement:
			if value.Name.Local == "text" {
				inText = false
			}
		}
	}
	return out
}

func stripEPUBFragment(value string) string {
	if index := strings.Index(value, "#"); index >= 0 {
		return value[:index]
	}
	return value
}

func readZipText(files []*zip.File, name string) (string, error) {
	cleanName := cleanEPUBPath(name)
	for _, file := range files {
		if cleanEPUBPath(file.Name) != cleanName {
			continue
		}
		body, err := file.Open()
		if err != nil {
			return "", fmt.Errorf("open epub entry %q: %w", name, err)
		}
		defer body.Close()
		data, err := io.ReadAll(body)
		if err != nil {
			return "", fmt.Errorf("read epub entry %q: %w", name, err)
		}
		return string(data), nil
	}
	return "", fmt.Errorf("epub entry %q not found", name)
}

func cleanEPUBPath(value string) string {
	value = strings.ReplaceAll(value, "\\", "/")
	value = path.Clean("/" + value)
	value = strings.TrimPrefix(value, "/")
	if value == "." {
		return ""
	}
	return value
}

func resolveEPUBHref(baseDir string, href string) string {
	if baseDir == "" {
		return cleanEPUBPath(href)
	}
	return cleanEPUBPath(path.Join(baseDir, href))
}

func epubContentType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".xhtml", ".html", ".htm":
		return "application/xhtml+xml; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".otf":
		return "font/otf"
	case ".ttf":
		return "font/ttf"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	}
	if value := contentType(name); value != "application/octet-stream" {
		return value
	}
	if value := mime.TypeByExtension(ext); value != "" {
		return value
	}
	return "application/octet-stream"
}

type epubContainer struct {
	Rootfiles []epubRootfile `xml:"rootfiles>rootfile"`
}

type epubRootfile struct {
	FullPath string `xml:"full-path,attr"`
}

type epubPackage struct {
	Metadata epubMetadata `xml:"metadata"`
	Manifest epubManifest `xml:"manifest"`
	Spine    epubSpine    `xml:"spine"`
}

type epubMetadata struct {
	Title       string     `xml:"title"`
	Creator     string     `xml:"creator"`
	Description string     `xml:"description"`
	Meta        []epubMeta `xml:"meta"`
}

type epubMeta struct {
	Name    string `xml:"name,attr"`
	Content string `xml:"content,attr"`
}

type epubManifest struct {
	Items []epubManifestItem `xml:"item"`
}

type epubManifestItem struct {
	ID         string `xml:"id,attr"`
	Href       string `xml:"href,attr"`
	MediaType  string `xml:"media-type,attr"`
	Properties string `xml:"properties,attr"`
}

type epubSpine struct {
	Itemrefs []epubItemref `xml:"itemref"`
	Toc      string        `xml:"toc,attr"`
}

type epubItemref struct {
	IDRef string `xml:"idref,attr"`
}
