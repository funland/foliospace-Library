package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"foliospace-reader/internal/domain"
)

// Comic titles are presentation data. Rules never rewrite series identities,
// scanner paths, book titles or collection private states.
type ComicTitleRule struct {
	Pattern     string `json:"pattern"`
	Replacement string `json:"replacement"`
}

type ComicTitleSample struct {
	ID           int64  `json:"id"`
	Path         string `json:"path"`
	Original     string `json:"original"`
	DisplayTitle string `json:"displayTitle"`
}

type ComicTitleRules struct {
	LibraryID int64              `json:"libraryId"`
	Rules     []ComicTitleRule   `json:"rules"`
	Defaults  []ComicTitleRule   `json:"defaults"`
	Samples   []ComicTitleSample `json:"samples"`
}

type ComicBrowserOptions struct {
	LibraryID int64
	Path      string
	Query     string
	Limit     int
	Offset    int
}

type ComicBrowserNode struct {
	domain.Series
	Kind            string `json:"kind"`
	Path            string `json:"path"`
	DisplayTitle    string `json:"displayTitle"`
	CollectionCount int64  `json:"collectionCount"`
	ThumbnailURL    string `json:"thumbnailUrl,omitempty"`
	ThumbnailStatus string `json:"thumbnailStatus,omitempty"`
}

type ComicBrowserPage struct {
	Items     []ComicBrowserNode `json:"items"`
	Total     int64              `json:"total"`
	Limit     int                `json:"limit"`
	Offset    int                `json:"offset"`
	HasMore   bool               `json:"hasMore"`
	LibraryID int64              `json:"libraryId"`
	Path      string             `json:"path"`
	Query     string             `json:"query"`
}

var ErrInvalidComicRequest = errors.New("invalid comic request")

func DefaultComicTitleRules() []ComicTitleRule {
	return []ComicTitleRule{
		{Pattern: `\s*-115\s*$`, Replacement: ""},
		{Pattern: `^\s*[0-9]{2,}[\s._-]*(\[|【|［|\(|（)`, Replacement: `${1}`},
	}
}

func compileComicTitleRules(rules []ComicTitleRule) ([]*regexp.Regexp, error) {
	if len(rules) > 16 {
		return nil, fmt.Errorf("%w: at most 16 title rules", ErrInvalidComicRequest)
	}
	compiled := make([]*regexp.Regexp, 0, len(rules))
	total := 0
	for _, rule := range rules {
		total += len(rule.Pattern) + len(rule.Replacement)
		if rule.Pattern == "" || len(rule.Pattern) > 1024 || len(rule.Replacement) > 512 || total > 8192 || !utf8.ValidString(rule.Pattern) || !utf8.ValidString(rule.Replacement) {
			return nil, fmt.Errorf("%w: title rule exceeds size limits", ErrInvalidComicRequest)
		}
		re, err := regexp.Compile(rule.Pattern)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid title rule regular expression", ErrInvalidComicRequest)
		}
		compiled = append(compiled, re)
	}
	return compiled, nil
}

func comicDisplayTitle(title string, rules []ComicTitleRule, compiled []*regexp.Regexp) string {
	original := strings.TrimSpace(title)
	if len(original) > 8192 {
		return original
	}
	out := original
	for i, re := range compiled {
		// Bound each intermediate result; RE2 matching itself is linear time.
		matches := re.FindAllStringSubmatchIndex(out, -1)
		var result strings.Builder
		last := 0
		for _, match := range matches {
			expanded := re.ExpandString(nil, rules[i].Replacement, out, match)
			if result.Len()+match[0]-last+len(expanded) > 16384 {
				return original
			}
			result.WriteString(out[last:match[0]])
			result.Write(expanded)
			last = match[1]
		}
		if result.Len()+len(out)-last > 16384 {
			return original
		}
		result.WriteString(out[last:])
		out = result.String()
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return original
	}
	return out
}

func comicRulesKey(libraryID int64) string {
	return "comic_title_rules:" + strconv.FormatInt(libraryID, 10)
}

func (s *Store) comicLibrary(libraryID int64) error {
	library, err := s.LibraryByID(libraryID)
	if err != nil {
		return err
	}
	if library.AssetType != "comic" && library.AssetType != "mixed" {
		return fmt.Errorf("%w: library is not a comic library", ErrInvalidComicRequest)
	}
	return nil
}

func (s *Store) comicTitleRules(libraryID int64) ([]ComicTitleRule, error) {
	value, err := s.Setting(comicRulesKey(libraryID))
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultComicTitleRules(), nil
	}
	if err != nil {
		return nil, err
	}
	var rules []ComicTitleRule
	if err := json.Unmarshal([]byte(value), &rules); err != nil {
		return nil, err
	}
	if rules == nil {
		rules = []ComicTitleRule{}
	}
	return rules, nil
}

func (s *Store) ComicTitleRules(libraryID int64) (ComicTitleRules, error) {
	if err := s.comicLibrary(libraryID); err != nil {
		return ComicTitleRules{}, err
	}
	rules, err := s.comicTitleRules(libraryID)
	if err != nil {
		return ComicTitleRules{}, err
	}
	return s.PreviewComicTitleRules(libraryID, rules)
}

func (s *Store) PreviewComicTitleRules(libraryID int64, rules []ComicTitleRule) (ComicTitleRules, error) {
	if err := s.comicLibrary(libraryID); err != nil {
		return ComicTitleRules{}, err
	}
	compiled, err := compileComicTitleRules(rules)
	if err != nil {
		return ComicTitleRules{}, err
	}
	series, err := s.comicBrowserSeries(libraryID)
	if err != nil {
		return ComicTitleRules{}, err
	}
	out := ComicTitleRules{LibraryID: libraryID, Rules: append([]ComicTitleRule{}, rules...), Defaults: DefaultComicTitleRules(), Samples: []ComicTitleSample{}}
	for _, item := range series {
		rel, err := comicRelativePath(item.DirectoryPath)
		if err != nil {
			continue
		}
		out.Samples = append(out.Samples, ComicTitleSample{ID: item.ID, Path: rel, Original: item.Title, DisplayTitle: comicDisplayTitle(comicLeafTitle(item, rel), rules, compiled)})
		if len(out.Samples) == 12 {
			break
		}
	}
	return out, nil
}

func (s *Store) SaveComicTitleRules(libraryID int64, rules []ComicTitleRule) (ComicTitleRules, error) {
	out, err := s.PreviewComicTitleRules(libraryID, rules)
	if err != nil {
		return ComicTitleRules{}, err
	}
	encoded, err := json.Marshal(out.Rules)
	if err != nil {
		return ComicTitleRules{}, err
	}
	if err := s.UpsertSetting(comicRulesKey(libraryID), string(encoded)); err != nil {
		return ComicTitleRules{}, err
	}
	return out, nil
}

// Only series metadata is loaded. Book aggregation is once per series, without
// joining file variants, so a book with several files never inflates totals.
func (s *Store) comicBrowserSeries(libraryID int64) ([]domain.Series, error) {
	where := ` WHERE l.asset_type IN ('comic', 'mixed')`
	args := []any{}
	if libraryID > 0 {
		where += ` AND s.library_id = ?`
		args = append(args, libraryID)
	}
	rows, err := s.db.Query(`SELECT s.id, s.library_id, s.title, COALESCE(s.directory_path, ''), s.collection_type, 'comic',
		COUNT(b.id), COALESCE((SELECT b2.id FROM books b2 WHERE b2.series_id=s.id ORDER BY b2.title,b2.id LIMIT 1),0),
		COALESCE(MAX(b.created_at), s.created_at)
		FROM series s JOIN libraries l ON l.id=s.library_id JOIN books b ON b.series_id=s.id`+where+`
		GROUP BY s.id HAVING l.asset_type='comic' OR
		SUM(CASE WHEN b.format IN ('epub','pdf') THEN 1 ELSE 0 END) <= SUM(CASE WHEN b.format IN ('cbz','zip','cbr','rar','7z') THEN 1 ELSE 0 END)
		ORDER BY s.title,s.library_id,s.directory_path,s.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.Series{}
	for rows.Next() {
		item, err := scanSeries(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func comicRelativePath(value string) (string, error) {
	if value == "" || value == "." {
		return "", nil
	}
	if len(value) > 4096 || strings.HasPrefix(value, "/") || strings.ContainsAny(value, "\\\x00") || !utf8.ValidString(value) {
		return "", fmt.Errorf("%w: invalid relative path", ErrInvalidComicRequest)
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." || (len(part) == 2 && part[1] == ':') {
			return "", fmt.Errorf("%w: invalid relative path", ErrInvalidComicRequest)
		}
	}
	return value, nil
}

func comicLeafTitle(item domain.Series, rel string) string {
	// Scanner titles encode their relative directory. Preserve separately curated
	// titles; shorten only an actual path-derived title for the directory card.
	if rel != "" && (item.Title == rel || item.Title == item.DirectoryPath) {
		return path.Base(rel)
	}
	return item.Title
}

func (s *Store) ComicBrowser(profileID int64, options ComicBrowserOptions) (ComicBrowserPage, error) {
	var err error
	options.Path, err = comicRelativePath(options.Path)
	if err != nil {
		return ComicBrowserPage{}, err
	}
	options.Query = strings.TrimSpace(options.Query)
	if options.LibraryID < 0 || options.Offset < 0 || len(options.Query) > 512 || (options.LibraryID == 0 && options.Path != "") {
		return ComicBrowserPage{}, fmt.Errorf("%w: invalid browser options", ErrInvalidComicRequest)
	}
	if options.LibraryID > 0 {
		if err := s.comicLibrary(options.LibraryID); err != nil {
			return ComicBrowserPage{}, err
		}
	}
	options.Limit = normalizeCollectionListLimit(options.Limit)
	series, err := s.comicBrowserSeries(options.LibraryID)
	if err != nil {
		return ComicBrowserPage{}, err
	}
	type ruleSet struct {
		rules    []ComicTitleRule
		compiled []*regexp.Regexp
	}
	rulesByLibrary := map[int64]ruleSet{}
	nodes := []ComicBrowserNode{}
	type folderKey struct {
		libraryID int64
		path      string
	}
	folders := map[folderKey]int{}
	query := strings.ToLower(options.Query)
	// Mark proper ancestor paths once. Leaf directories can open their existing
	// collection directly, while directories with descendants retain navigation.
	hasDescendants := map[folderKey]bool{}
	if query == "" {
		for _, item := range series {
			rel, err := comicRelativePath(item.DirectoryPath)
			if err != nil {
				continue
			}
			for i := range rel {
				if rel[i] == '/' {
					hasDescendants[folderKey{item.LibraryID, rel[:i]}] = true
				}
			}
		}
	}
	for _, item := range series {
		rel, err := comicRelativePath(item.DirectoryPath)
		if err != nil { // Never expose legacy absolute paths as browser nodes.
			continue
		}
		if options.Path != "" && rel != options.Path && !strings.HasPrefix(rel, options.Path+"/") {
			continue
		}
		set, ok := rulesByLibrary[item.LibraryID]
		if !ok {
			set.rules, err = s.comicTitleRules(item.LibraryID)
			if err != nil {
				return ComicBrowserPage{}, err
			}
			set.compiled, err = compileComicTitleRules(set.rules)
			if err != nil {
				return ComicBrowserPage{}, err
			}
			rulesByLibrary[item.LibraryID] = set
		}
		displayTitle := comicDisplayTitle(comicLeafTitle(item, rel), set.rules, set.compiled)
		if query != "" {
			if !strings.Contains(strings.ToLower(item.Title+"\n"+rel+"\n"+displayTitle), query) {
				continue
			}
			nodes = append(nodes, ComicBrowserNode{Series: item, Kind: "collection", Path: rel, DisplayTitle: displayTitle, CollectionCount: 1})
			continue
		}
		if rel == options.Path {
			nodes = append(nodes, ComicBrowserNode{Series: item, Kind: "collection", Path: rel, DisplayTitle: displayTitle, CollectionCount: 1})
			continue
		}
		remainder := rel
		if options.Path != "" {
			remainder = strings.TrimPrefix(rel, options.Path+"/")
		}
		segment := strings.SplitN(remainder, "/", 2)[0]
		childPath := segment
		if options.Path != "" {
			childPath = options.Path + "/" + segment
		}
		key := folderKey{item.LibraryID, childPath}
		if rel == childPath && !hasDescendants[key] {
			nodes = append(nodes, ComicBrowserNode{Series: item, Kind: "collection", Path: rel, DisplayTitle: displayTitle, CollectionCount: 1})
			continue
		}
		if index, exists := folders[key]; exists {
			nodes[index].CollectionCount++
			nodes[index].BookCount += item.BookCount
			if item.AddedAt.After(nodes[index].AddedAt) {
				nodes[index].AddedAt = item.AddedAt
			}
		} else {
			folders[key] = len(nodes)
			nodes = append(nodes, ComicBrowserNode{Series: domain.Series{LibraryID: item.LibraryID, Title: segment, DirectoryPath: childPath, PrimaryType: "comic", BookCount: item.BookCount, CoverBookID: item.CoverBookID, AddedAt: item.AddedAt}, Kind: "folder", Path: childPath, DisplayTitle: comicDisplayTitle(segment, set.rules, set.compiled), CollectionCount: 1})
		}
	}
	sort.Slice(nodes, func(i, j int) bool {
		a, b := nodes[i], nodes[j]
		if a.Kind != b.Kind {
			return a.Kind == "folder"
		}
		at, bt := strings.ToLower(a.DisplayTitle), strings.ToLower(b.DisplayTitle)
		if at != bt {
			return at < bt
		}
		if a.LibraryID != b.LibraryID {
			return a.LibraryID < b.LibraryID
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.ID < b.ID
	})
	out := ComicBrowserPage{Items: []ComicBrowserNode{}, Total: int64(len(nodes)), Limit: options.Limit, Offset: options.Offset, LibraryID: options.LibraryID, Path: options.Path, Query: options.Query}
	if options.Offset < len(nodes) {
		end := options.Offset + options.Limit
		if end > len(nodes) {
			end = len(nodes)
		}
		out.Items = nodes[options.Offset:end]
		out.HasMore = end < len(nodes)
	}
	selected := []domain.Series{}
	for _, node := range out.Items {
		if node.Kind == "collection" {
			selected = append(selected, node.Series)
		}
	}
	selected, err = s.applyCollectionPrivateStates(profileID, selected)
	if err != nil {
		return ComicBrowserPage{}, err
	}
	index := 0
	for i := range out.Items {
		if out.Items[i].Kind == "collection" {
			out.Items[i].Series = selected[index]
			index++
		}
	}
	return out, nil
}
