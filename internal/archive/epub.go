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
	itemsByHref := map[string]epubManifestItem{}
	for _, item := range pkg.Manifest.Items {
		item.Href = resolveEPUBHref(opfDir, item.Href)
		itemsByID[item.ID] = item
		itemsByHref[item.Href] = item
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
	if coverHref == "" {
		coverHref = epubGuideCoverHref(files, opfDir, pkg, itemsByHref)
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

	tocTree := epubTOCTree(files, opfDir, pkg, spine, itemsByID)

	return domain.EPUBManifest{
		Title:       strings.TrimSpace(pkg.Metadata.Title),
		Creator:     strings.TrimSpace(pkg.Metadata.Creator),
		Description: strings.TrimSpace(pkg.Metadata.Description),
		CoverHref:   coverHref,
		Spine:       spine,
		TOC:         flattenEPUBTOC(tocTree),
		TOCTree:     tocTree,
	}, nil
}

func epubGuideCoverHref(files []*zip.File, opfDir string, pkg epubPackage, itemsByHref map[string]epubManifestItem) string {
	for _, ref := range pkg.Guide.References {
		if !strings.EqualFold(strings.TrimSpace(ref.Type), "cover") || strings.TrimSpace(ref.Href) == "" {
			continue
		}
		href := resolveEPUBHref(opfDir, ref.Href)
		if epubResourceIsImage(files, href, itemsByHref) {
			return href
		}
	}
	return ""
}

func epubResourceIsImage(files []*zip.File, href string, itemsByHref map[string]epubManifestItem) bool {
	cleanHref := cleanEPUBPath(href)
	if cleanHref == "" || !epubZipEntryExists(files, cleanHref) {
		return false
	}
	if item, ok := itemsByHref[cleanHref]; ok && strings.TrimSpace(item.MediaType) != "" {
		return strings.HasPrefix(strings.ToLower(item.MediaType), "image/")
	}
	return strings.HasPrefix(strings.ToLower(epubContentType(cleanHref)), "image/")
}

func epubZipEntryExists(files []*zip.File, name string) bool {
	cleanName := cleanEPUBPath(name)
	for _, file := range files {
		if cleanEPUBPath(file.Name) == cleanName && !file.FileInfo().IsDir() {
			return true
		}
	}
	return false
}

func epubTOCTree(files []*zip.File, opfDir string, pkg epubPackage, spine []domain.EPUBSpineItem, itemsByID map[string]epubManifestItem) []domain.EPUBTOCItem {
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
			return expandEPUBTOCFromContentsPages(files, parseEPUBNavTOCTree(body, path.Dir(navPath), hrefToIndex), hrefToIndex)
		}
	}

	if pkg.Spine.Toc != "" {
		if item, ok := itemsByID[pkg.Spine.Toc]; ok {
			body, err := readZipText(files, item.Href)
			if err == nil {
				return expandEPUBTOCFromContentsPages(files, parseEPUBNCXTOCTree(body, path.Dir(item.Href), hrefToIndex), hrefToIndex)
			}
		}
	}
	return nil
}

func parseEPUBNavTOCTree(body string, baseDir string, hrefToIndex map[string]int) []domain.EPUBTOCItem {
	root, err := parseEPUBXMLTree(body)
	if err != nil {
		return nil
	}
	nav := findEPUBTOCNav(root)
	if nav == nil {
		return nil
	}
	list := firstEPUBChild(nav, "ol")
	if list == nil {
		return nil
	}
	return parseEPUBNavList(list, baseDir, hrefToIndex)
}

func parseEPUBNavList(list *epubXMLNode, baseDir string, hrefToIndex map[string]int) []domain.EPUBTOCItem {
	var out []domain.EPUBTOCItem
	for _, child := range directEPUBChildren(list, "li") {
		if item, ok := parseEPUBNavListItem(child, baseDir, hrefToIndex); ok {
			out = append(out, item)
		}
	}
	return out
}

func parseEPUBNavListItem(item *epubXMLNode, baseDir string, hrefToIndex map[string]int) (domain.EPUBTOCItem, bool) {
	link := firstEPUBChild(item, "a")
	labelNode := link
	href := ""
	if link != nil {
		href = resolveEPUBHref(baseDir, epubXMLAttr(link, "href"))
	} else if span := firstEPUBChild(item, "span"); span != nil {
		labelNode = span
	}

	var children []domain.EPUBTOCItem
	for _, list := range directEPUBChildren(item, "ol") {
		children = append(children, parseEPUBNavList(list, baseDir, hrefToIndex)...)
	}

	label := strings.TrimSpace(epubXMLText(labelNode))
	if label == "" {
		label = href
	}
	index := epubTOCIndex(href, hrefToIndex, children)
	if label == "" && href == "" && len(children) == 0 {
		return domain.EPUBTOCItem{}, false
	}
	if index < 0 && len(children) == 0 {
		return domain.EPUBTOCItem{}, false
	}
	return domain.EPUBTOCItem{Label: label, Href: href, Index: index, Children: children}, true
}

func parseEPUBNCXTOCTree(body string, baseDir string, hrefToIndex map[string]int) []domain.EPUBTOCItem {
	root, err := parseEPUBXMLTree(body)
	if err != nil {
		return nil
	}
	navMap := findEPUBDescendant(root, "navMap")
	if navMap == nil {
		return nil
	}
	var out []domain.EPUBTOCItem
	for _, point := range directEPUBChildren(navMap, "navPoint") {
		if item, ok := parseEPUBNCXNavPoint(point, baseDir, hrefToIndex); ok {
			out = append(out, item)
		}
	}
	return out
}

func parseEPUBNCXNavPoint(point *epubXMLNode, baseDir string, hrefToIndex map[string]int) (domain.EPUBTOCItem, bool) {
	href := ""
	if content := firstEPUBChild(point, "content"); content != nil {
		href = resolveEPUBHref(baseDir, epubXMLAttr(content, "src"))
	}
	label := ""
	if navLabel := firstEPUBChild(point, "navLabel"); navLabel != nil {
		label = strings.TrimSpace(epubXMLText(firstEPUBChild(navLabel, "text")))
	}
	if label == "" {
		label = href
	}

	var children []domain.EPUBTOCItem
	for _, child := range directEPUBChildren(point, "navPoint") {
		if item, ok := parseEPUBNCXNavPoint(child, baseDir, hrefToIndex); ok {
			children = append(children, item)
		}
	}

	index := epubTOCIndex(href, hrefToIndex, children)
	if label == "" && href == "" && len(children) == 0 {
		return domain.EPUBTOCItem{}, false
	}
	if index < 0 && len(children) == 0 {
		return domain.EPUBTOCItem{}, false
	}
	return domain.EPUBTOCItem{Label: label, Href: href, Index: index, Children: children}, true
}

func expandEPUBTOCFromContentsPages(files []*zip.File, items []domain.EPUBTOCItem, hrefToIndex map[string]int) []domain.EPUBTOCItem {
	if len(items) == 0 {
		return items
	}
	return expandEPUBTOCItemsFromContentsPages(files, items, items, hrefToIndex, map[string]bool{})
}

func expandEPUBTOCItemsFromContentsPages(files []*zip.File, items []domain.EPUBTOCItem, rootItems []domain.EPUBTOCItem, hrefToIndex map[string]int, ancestorHrefs map[string]bool) []domain.EPUBTOCItem {
	out := make([]domain.EPUBTOCItem, 0, len(items))
	for _, item := range items {
		spineHref := stripEPUBFragment(item.Href)
		nextAncestors := copyEPUBStringBoolMap(ancestorHrefs)
		if spineHref != "" {
			nextAncestors[spineHref] = true
		}
		if len(item.Children) > 0 {
			item.Children = expandEPUBTOCItemsFromContentsPages(files, item.Children, rootItems, hrefToIndex, nextAncestors)
			out = append(out, item)
			continue
		}
		if spineHref == "" || ancestorHrefs[spineHref] {
			out = append(out, item)
			continue
		}
		body, err := readZipText(files, spineHref)
		if err != nil {
			out = append(out, item)
			continue
		}
		generated := extractEPUBGeneratedTOCChildren(body, spineHref, item.Href, hrefToIndex)
		if len(generated) < 2 || epubGeneratedTOCChildrenRedundant(rootItems, generated) {
			out = append(out, item)
			continue
		}
		item.Children = generated
		out = append(out, item)
	}
	return out
}

func extractEPUBGeneratedTOCChildren(body string, baseHref string, parentTarget string, hrefToIndex map[string]int) []domain.EPUBTOCItem {
	root, err := parseEPUBXMLTree(body)
	if err != nil {
		return nil
	}
	baseDir := path.Dir(stripEPUBFragment(baseHref))
	if baseDir == "." {
		baseDir = ""
	}
	seen := map[string]bool{}
	var out []domain.EPUBTOCItem
	var visit func(*epubXMLNode)
	visit = func(node *epubXMLNode) {
		if node == nil {
			return
		}
		if isEPUBGeneratedTOCContainer(node) {
			for index, child := range node.Children {
				if child.Name != "a" {
					continue
				}
				href := resolveEPUBHref(baseDir, epubXMLAttr(child, "href"))
				spineIndex, ok := hrefToIndex[stripEPUBFragment(href)]
				if !ok {
					continue
				}
				label := normalizeEPUBGeneratedTOCText(strings.TrimSpace(epubXMLText(child)) + " " + epubFollowingTextUntilLink(node.Children[index+1:]))
				if shouldIgnoreEPUBGeneratedTOCEntry(label, href, parentTarget) || seen[href] {
					continue
				}
				seen[href] = true
				out = append(out, domain.EPUBTOCItem{Label: label, Href: href, Index: spineIndex})
			}
		}
		for _, child := range node.Children {
			visit(child)
		}
	}
	visit(root)
	return out
}

func flattenEPUBTOC(items []domain.EPUBTOCItem) []domain.EPUBTOCItem {
	var out []domain.EPUBTOCItem
	var visit func([]domain.EPUBTOCItem)
	visit = func(entries []domain.EPUBTOCItem) {
		for _, item := range entries {
			if item.Href != "" && item.Index >= 0 {
				flat := item
				flat.Children = nil
				out = append(out, flat)
			}
			if len(item.Children) > 0 {
				visit(item.Children)
			}
		}
	}
	visit(items)
	return out
}

func epubTOCIndex(href string, hrefToIndex map[string]int, children []domain.EPUBTOCItem) int {
	if href != "" {
		if index, ok := hrefToIndex[stripEPUBFragment(href)]; ok {
			return index
		}
	}
	for _, child := range children {
		if child.Index >= 0 {
			return child.Index
		}
	}
	return -1
}

func epubGeneratedTOCChildrenRedundant(items []domain.EPUBTOCItem, generated []domain.EPUBTOCItem) bool {
	if len(generated) == 0 {
		return false
	}
	targets := map[string]bool{}
	var visit func([]domain.EPUBTOCItem)
	visit = func(entries []domain.EPUBTOCItem) {
		for _, item := range entries {
			if item.Href != "" {
				targets[item.Href] = true
			}
			if len(item.Children) > 0 {
				visit(item.Children)
			}
		}
	}
	visit(items)
	for _, child := range generated {
		if child.Href == "" || !targets[child.Href] {
			return false
		}
	}
	return true
}

func normalizeEPUBGeneratedTOCText(text string) string {
	parts := strings.Fields(text)
	return strings.TrimSpace(strings.Join(parts, " "))
}

func shouldIgnoreEPUBGeneratedTOCEntry(label string, target string, parentTarget string) bool {
	if label == "" || target == "" {
		return true
	}
	normalizedLabel := strings.ToLower(label)
	normalizedParentTarget := stripEPUBFragment(parentTarget)
	normalizedTarget := stripEPUBFragment(target)
	if target == parentTarget || (normalizedTarget == normalizedParentTarget && !strings.Contains(target, "#")) {
		return true
	}
	return normalizedLabel == "contents" ||
		normalizedLabel == "table of contents" ||
		strings.Contains(normalizedLabel, "back to main contents") ||
		strings.Contains(normalizedLabel, "back to contents") ||
		strings.Contains(normalizedLabel, "main contents")
}

func isEPUBGeneratedTOCContainer(node *epubXMLNode) bool {
	switch node.Name {
	case "body", "div", "li", "p":
		return true
	default:
		return false
	}
}

func epubFollowingTextUntilLink(nodes []*epubXMLNode) string {
	var builder strings.Builder
	for _, node := range nodes {
		if epubNodeContainsLink(node) {
			break
		}
		text := strings.TrimSpace(epubXMLText(node))
		if text != "" {
			if builder.Len() > 0 {
				builder.WriteByte(' ')
			}
			builder.WriteString(text)
		}
	}
	return builder.String()
}

func epubNodeContainsLink(node *epubXMLNode) bool {
	if node == nil {
		return false
	}
	if node.Name == "a" {
		return true
	}
	for _, child := range node.Children {
		if epubNodeContainsLink(child) {
			return true
		}
	}
	return false
}

func copyEPUBStringBoolMap(value map[string]bool) map[string]bool {
	out := make(map[string]bool, len(value))
	for key, item := range value {
		out[key] = item
	}
	return out
}

type epubXMLNode struct {
	Name     string
	Attrs    map[string]string
	Text     string
	Children []*epubXMLNode
}

func parseEPUBXMLTree(body string) (*epubXMLNode, error) {
	decoder := xml.NewDecoder(strings.NewReader(body))
	root := &epubXMLNode{Name: "#document", Attrs: map[string]string{}}
	stack := []*epubXMLNode{root}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch value := token.(type) {
		case xml.StartElement:
			node := &epubXMLNode{Name: value.Name.Local, Attrs: map[string]string{}}
			for _, attr := range value.Attr {
				node.Attrs[strings.ToLower(attr.Name.Local)] = attr.Value
			}
			parent := stack[len(stack)-1]
			parent.Children = append(parent.Children, node)
			stack = append(stack, node)
		case xml.CharData:
			stack[len(stack)-1].Text += string(value)
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	return root, nil
}

func findEPUBTOCNav(root *epubXMLNode) *epubXMLNode {
	var firstNav *epubXMLNode
	var visit func(*epubXMLNode) *epubXMLNode
	visit = func(node *epubXMLNode) *epubXMLNode {
		if node == nil {
			return nil
		}
		if node.Name == "nav" {
			if firstNav == nil {
				firstNav = node
			}
			if strings.Contains(strings.ToLower(epubXMLAttr(node, "type")), "toc") {
				return node
			}
		}
		for _, child := range node.Children {
			if match := visit(child); match != nil {
				return match
			}
		}
		return nil
	}
	if match := visit(root); match != nil {
		return match
	}
	return firstNav
}

func findEPUBDescendant(node *epubXMLNode, name string) *epubXMLNode {
	if node == nil {
		return nil
	}
	if node.Name == name {
		return node
	}
	for _, child := range node.Children {
		if match := findEPUBDescendant(child, name); match != nil {
			return match
		}
	}
	return nil
}

func firstEPUBChild(node *epubXMLNode, name string) *epubXMLNode {
	for _, child := range node.Children {
		if child.Name == name {
			return child
		}
	}
	return nil
}

func directEPUBChildren(node *epubXMLNode, name string) []*epubXMLNode {
	var out []*epubXMLNode
	if node == nil {
		return out
	}
	for _, child := range node.Children {
		if child.Name == name {
			out = append(out, child)
		}
	}
	return out
}

func epubXMLAttr(node *epubXMLNode, name string) string {
	if node == nil || node.Attrs == nil {
		return ""
	}
	return strings.TrimSpace(node.Attrs[strings.ToLower(name)])
}

func epubXMLText(node *epubXMLNode) string {
	if node == nil {
		return ""
	}
	var builder strings.Builder
	builder.WriteString(node.Text)
	for _, child := range node.Children {
		builder.WriteByte(' ')
		builder.WriteString(epubXMLText(child))
	}
	return strings.Join(strings.Fields(builder.String()), " ")
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
	Guide    epubGuide    `xml:"guide"`
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

type epubGuide struct {
	References []epubGuideReference `xml:"reference"`
}

type epubGuideReference struct {
	Type string `xml:"type,attr"`
	Href string `xml:"href,attr"`
}

type epubSpine struct {
	Itemrefs []epubItemref `xml:"itemref"`
	Toc      string        `xml:"toc,attr"`
}

type epubItemref struct {
	IDRef string `xml:"idref,attr"`
}
