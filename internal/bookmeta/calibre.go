// Package bookmeta reads local Calibre metadata without changing the source library.
package bookmeta

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"html"
	"image/jpeg"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	maxDatabaseBytes = 256 << 20
	maxOPFBytes      = 2 << 20
	maxCoverBytes    = 32 << 20
	maxRows          = 250000
)

type Metadata struct {
	Title         string
	Creator       string
	Description   string
	Tags          []string
	Source        string
	CoverPath     string
	CoverRevision string
}

type Cover struct {
	Path        string
	ContentType string
	Revision    string
}

var coverCache = struct {
	sync.Mutex
	entries map[string]Cover
}{entries: map[string]Cover{}}

// CoverInfo performs only bounded sidecar reads and file stats, never a database
// query or copy. Its cache expires when the folder, OPF, DB or cover changes.
func CoverInfo(libraryRoot, bookPath string) (Cover, error) {
	if _, err := safeRegular(libraryRoot, bookPath, 0); err != nil {
		return Cover{}, err
	}
	coverPath := filepath.Join(filepath.Dir(bookPath), "cover.jpg")
	cover, err := safeRegular(libraryRoot, coverPath, maxCoverBytes)
	if err != nil {
		return Cover{}, err
	}
	key := libraryRoot + "\x00" + bookPath + "\x00" + statMarker(cover)
	folder, err := os.Stat(filepath.Dir(bookPath))
	if err != nil {
		return Cover{}, err
	}
	key += "\x00" + statMarker(folder)
	if opf, err := safeRegular(libraryRoot, filepath.Join(filepath.Dir(bookPath), "metadata.opf"), maxOPFBytes); err == nil {
		key += "\x00opf:" + statMarker(opf)
	} else if !os.IsNotExist(err) {
		return Cover{}, err
	}
	for dir := filepath.Dir(bookPath); within(libraryRoot, dir); dir = filepath.Dir(dir) {
		if db, err := safeRegular(libraryRoot, filepath.Join(dir, "metadata.db"), maxDatabaseBytes); err == nil {
			key += "\x00db:" + dir + statMarker(db)
			break
		} else if !os.IsNotExist(err) {
			return Cover{}, err
		}
		if dir == filepath.Clean(libraryRoot) {
			break
		}
	}
	coverCache.Lock()
	cached, ok := coverCache.entries[key]
	coverCache.Unlock()
	if ok {
		return cached, nil
	}
	path, revision, err := coverInFolder(libraryRoot, bookPath, false)
	if err != nil {
		return Cover{}, err
	}
	result := Cover{Path: path, Revision: revision, ContentType: "image/jpeg"}
	coverCache.Lock()
	if len(coverCache.entries) >= 256 {
		coverCache.entries = map[string]Cover{}
	}
	coverCache.entries[key] = result
	coverCache.Unlock()
	return result, nil
}

func statMarker(info os.FileInfo) string {
	return fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
}

type lookupResult struct {
	metadata Metadata
	found    bool
	err      error
}
type databaseResult struct {
	books map[string]Metadata
	err   error
}

type sourceLocation struct {
	database string
	hasOPF   bool
	err      error
}

// Resolver is a per-scan snapshot. Recreate it for each scan so sidecar edits are
// visible even when the book itself has not changed. It is safe for scan workers.
type Resolver struct {
	root              string
	mu                sync.Mutex
	lookups           map[string]lookupResult
	databases         map[string]databaseResult
	databaseLocations map[string]sourceLocation
	folderSources     map[string]sourceLocation
}

func NewResolver(libraryRoot string) *Resolver {
	return &Resolver{root: filepath.Clean(libraryRoot), lookups: map[string]lookupResult{}, databases: map[string]databaseResult{}, databaseLocations: map[string]sourceLocation{}, folderSources: map[string]sourceLocation{}}
}

// Lookup uses an exact Calibre format path, then an unambiguous metadata.opf in
// the book's own directory. An error with found=true is a nonfatal DB warning:
// the returned OPF metadata is still usable.
func (r *Resolver) Lookup(bookPath string) (Metadata, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	bookPath = filepath.Clean(bookPath)
	if result, ok := r.lookups[bookPath]; ok {
		return result.metadata, result.found, result.err
	}
	result := r.lookup(bookPath)
	r.lookups[bookPath] = result
	return result.metadata, result.found, result.err
}

func (r *Resolver) lookup(bookPath string) lookupResult {
	if !within(r.root, bookPath) {
		return lookupResult{err: fmt.Errorf("book path outside library")}
	}
	source := r.sourceForFolder(filepath.Dir(bookPath))
	if source.database == "" && !source.hasOPF && source.err == nil {
		return lookupResult{}
	}
	if _, err := safeRegular(r.root, bookPath, 0); err != nil {
		return lookupResult{err: err}
	}
	databaseErr := source.err
	if dbPath := source.database; dbPath != "" {
		cached, ok := r.databases[dbPath]
		if !ok {
			cached.books, cached.err = readDatabase(r.root, dbPath)
			r.databases[dbPath] = cached
		}
		if metadata, ok := cached.books[bookPath]; ok {
			metadata.CoverPath, metadata.CoverRevision, _ = coverInFolder(r.root, bookPath, true)
			return lookupResult{metadata: metadata, found: true}
		}
		databaseErr = cached.err
	}
	metadata, found, err := readOPF(r.root, bookPath)
	if err != nil {
		return lookupResult{err: err}
	}
	if found {
		metadata.CoverPath, metadata.CoverRevision, _ = coverInFolder(r.root, bookPath, true)
		return lookupResult{metadata: metadata, found: true, err: databaseErr}
	}
	return lookupResult{err: databaseErr}
}

// Cache both positive and negative discovery for this scan. Ordinary comic
// folders then pay only two sidecar stats once, not a tree of stats per book.
func (r *Resolver) sourceForFolder(dir string) sourceLocation {
	if source, ok := r.folderSources[dir]; ok {
		return source
	}
	source := r.databaseForFolder(dir)
	if _, err := os.Lstat(filepath.Join(dir, "metadata.opf")); err == nil {
		source.hasOPF = true
	} else if !os.IsNotExist(err) {
		source.err = err
	}
	r.folderSources[dir] = source
	return source
}

func (r *Resolver) databaseForFolder(dir string) sourceLocation {
	if source, ok := r.databaseLocations[dir]; ok {
		return source
	}
	if !within(r.root, dir) {
		return sourceLocation{}
	}
	var source sourceLocation
	path := filepath.Join(dir, "metadata.db")
	if _, err := os.Lstat(path); err == nil {
		source.database = path
	} else if !os.IsNotExist(err) {
		source.err = err
	} else if dir != r.root {
		source = r.databaseForFolder(filepath.Dir(dir))
	}
	r.databaseLocations[dir] = source
	return source
}

func within(root, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// Reject symlinks in every component beneath the selected library root. The
// source is opened separately and checked with SameFile to detect replacements.
func safeRegular(root, path string, limit int64) (os.FileInfo, error) {
	if !within(root, path) {
		return nil, fmt.Errorf("metadata path outside library")
	}
	rel, _ := filepath.Rel(root, path)
	current := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("metadata path contains symlink")
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || (limit > 0 && info.Size() > limit) {
		return nil, fmt.Errorf("metadata source is not a bounded regular file")
	}
	return info, nil
}

func readBounded(root, path string, limit int64) ([]byte, error) {
	info, err := safeRegular(root, path, limit)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("metadata source changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(info, after) || info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) || int64(len(data)) > limit {
		return nil, fmt.Errorf("metadata source changed while reading or exceeded limit")
	}
	return data, nil
}

// Opening SQLite on a writable source can create shared-memory files, even for
// a read-only connection. Copy a quiescent rollback-mode DB first; never open
// the source through SQLite, and never ignore an active WAL or rollback journal.
func readDatabase(root, dbPath string) (map[string]Metadata, error) {
	if err := rejectActiveJournal(root, dbPath); err != nil {
		return nil, err
	}
	data, err := readBounded(root, dbPath, maxDatabaseBytes)
	if err != nil {
		return nil, err
	}
	if err := rejectActiveJournal(root, dbPath); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "foliospace-calibre-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	copyPath := filepath.Join(tmp, "metadata.db")
	if err := os.WriteFile(copyPath, data, 0o600); err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: copyPath}
	q := url.Values{"mode": {"ro"}, "_pragma": {"query_only(1)", "busy_timeout(100)"}}
	u.RawQuery = q.Encode()
	conn, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return queryDatabase(ctx, conn, filepath.Dir(dbPath))
}

func rejectActiveJournal(root, dbPath string) error {
	for _, suffix := range []string{"-wal", "-journal"} {
		info, err := os.Lstat(dbPath + suffix)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() != 0 {
			return fmt.Errorf("Calibre database has an active journal; using OPF fallback")
		}
		if _, err := safeRegular(root, dbPath+suffix, maxDatabaseBytes); err != nil {
			return err
		}
	}
	return nil
}

func queryDatabase(ctx context.Context, conn *sql.DB, root string) (map[string]Metadata, error) {
	type record struct {
		id       int64
		path     string
		metadata Metadata
	}
	rows, err := conn.QueryContext(ctx, `SELECT b.id, b.path, d.name, d.format, b.title, COALESCE(c.text, '')
		FROM books b JOIN data d ON d.book = b.id LEFT JOIN comments c ON c.book = b.id ORDER BY b.id, d.id LIMIT ?`, maxRows+1)
	if err != nil {
		return nil, err
	}
	var records []record
	for rows.Next() {
		var id int64
		var dir, name, format, title, description string
		if err := rows.Scan(&id, &dir, &name, &format, &title, &description); err != nil {
			rows.Close()
			return nil, err
		}
		if len(records) >= maxRows {
			rows.Close()
			return nil, fmt.Errorf("Calibre metadata row limit exceeded")
		}
		if len(title)+len(description)+len(dir)+len(name) > maxOPFBytes {
			rows.Close()
			return nil, fmt.Errorf("Calibre metadata field limit exceeded")
		}
		format = strings.ToLower(strings.TrimSpace(format))
		if !bookExtension("." + format) {
			continue
		}
		if strings.ContainsAny(name, `/\`) || name == "" || filepath.IsAbs(dir) || strings.Contains(dir, `\`) {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(dir), name+"."+format)
		if !within(root, path) {
			continue
		}
		records = append(records, record{id, path, Metadata{Title: strings.TrimSpace(title), Description: plainText(description), Source: "calibre-db"}})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	authors, err := linkedValues(ctx, conn, `SELECT l.book, a.name FROM books_authors_link l JOIN authors a ON a.id=l.author ORDER BY l.book, l.id LIMIT ?`)
	if err != nil {
		return nil, err
	}
	tags, err := linkedValues(ctx, conn, `SELECT l.book, t.name FROM books_tags_link l JOIN tags t ON t.id=l.tag ORDER BY l.book, l.id LIMIT ?`)
	if err != nil {
		return nil, err
	}
	books := make(map[string]Metadata, len(records))
	for _, record := range records {
		record.metadata.Creator = strings.Join(authors[record.id], ", ")
		record.metadata.Tags = tags[record.id]
		books[record.path] = record.metadata
	}
	return books, nil
}

func linkedValues(ctx context.Context, conn *sql.DB, query string) (map[int64][]string, error) {
	rows, err := conn.QueryContext(ctx, query, maxRows+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := map[int64][]string{}
	count := 0
	for rows.Next() {
		count++
		if count > maxRows {
			return nil, fmt.Errorf("Calibre metadata link limit exceeded")
		}
		var id int64
		var value string
		if err := rows.Scan(&id, &value); err != nil {
			return nil, err
		}
		if len(value) > maxOPFBytes {
			return nil, fmt.Errorf("Calibre metadata field limit exceeded")
		}
		values[id] = unique(append(values[id], value))
	}
	return values, rows.Err()
}

type opfPackage struct {
	XMLName  xml.Name
	Metadata struct {
		Title       string   `xml:"http://purl.org/dc/elements/1.1/ title"`
		Creators    []string `xml:"http://purl.org/dc/elements/1.1/ creator"`
		Description string   `xml:"http://purl.org/dc/elements/1.1/ description"`
		Subjects    []string `xml:"http://purl.org/dc/elements/1.1/ subject"`
	} `xml:"metadata"`
}

func readOPF(root, bookPath string) (Metadata, bool, error) {
	path := filepath.Join(filepath.Dir(bookPath), "metadata.opf")
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return Metadata{}, false, nil
	}
	data, err := readBounded(root, path, maxOPFBytes)
	if err != nil {
		return Metadata{}, false, err
	}
	if !unambiguousFolder(bookPath) {
		return Metadata{}, false, fmt.Errorf("metadata.opf is ambiguous for multiple books")
	}
	upper := strings.ToUpper(string(data))
	if strings.Contains(upper, "<!DOCTYPE") || strings.Contains(upper, "<!ENTITY") {
		return Metadata{}, false, fmt.Errorf("OPF directives are unsupported")
	}
	var pkg opfPackage
	if err := xml.Unmarshal(data, &pkg); err != nil {
		return Metadata{}, false, err
	}
	if pkg.XMLName.Local != "package" || (pkg.XMLName.Space != "" && pkg.XMLName.Space != "http://www.idpf.org/2007/opf") {
		return Metadata{}, false, fmt.Errorf("invalid OPF package")
	}
	m := Metadata{Title: strings.TrimSpace(pkg.Metadata.Title), Creator: strings.Join(unique(pkg.Metadata.Creators), ", "), Description: plainText(pkg.Metadata.Description), Tags: unique(pkg.Metadata.Subjects), Source: "calibre-opf"}
	if m.Title == "" && m.Creator == "" && m.Description == "" && len(m.Tags) == 0 {
		return Metadata{}, false, fmt.Errorf("OPF has no book metadata")
	}
	return m, true, nil
}

func bookExtension(ext string) bool {
	switch strings.ToLower(ext) {
	case ".epub", ".pdf", ".cbz", ".zip", ".cbr", ".rar", ".7z", ".cb7", ".mobi", ".azw", ".azw3":
		return true
	}
	return false
}

func unambiguousFolder(bookPath string) bool {
	entries, err := os.ReadDir(filepath.Dir(bookPath))
	if err != nil {
		return false
	}
	stem := strings.TrimSuffix(filepath.Base(bookPath), filepath.Ext(bookPath))
	for _, entry := range entries {
		if entry.IsDir() || !bookExtension(filepath.Ext(entry.Name())) {
			continue
		}
		if strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())) != stem {
			return false
		}
	}
	return true
}

var markupPattern = regexp.MustCompile(`<[^>]*>`)

func plainText(value string) string {
	value = html.UnescapeString(value)
	value = markupPattern.ReplaceAllString(value, " ")
	return strings.Join(strings.Fields(value), " ")
}

func unique(values []string) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

// CoverRevision returns a source revision suitable for thumbnail keys and URLs.
// This hot path never queries SQLite. A folder must contain one book stem and a
// local OPF or lie within a Calibre library before its cover.jpg is accepted.
func CoverRevision(libraryRoot, bookPath string) (string, error) {
	cover, err := CoverInfo(libraryRoot, bookPath)
	return cover.Revision, err
}

func OpenCover(libraryRoot, bookPath string) (io.ReadCloser, string, string, error) {
	cover, err := CoverInfo(libraryRoot, bookPath)
	if err != nil {
		return nil, "", "", err
	}
	data, err := readBounded(libraryRoot, cover.Path, maxCoverBytes)
	if err != nil {
		return nil, "", "", err
	}
	after, err := CoverInfo(libraryRoot, bookPath)
	if err != nil || after.Revision != cover.Revision {
		return nil, "", "", fmt.Errorf("Calibre cover changed while opening")
	}
	return io.NopCloser(bytes.NewReader(data)), cover.ContentType, cover.Revision, nil
}

func coverInFolder(root, bookPath string, matched bool) (string, string, error) {
	if _, err := safeRegular(root, bookPath, 0); err != nil {
		return "", "", err
	}
	if !matched {
		if !unambiguousFolder(bookPath) {
			return "", "", os.ErrNotExist
		}
		_, hasOPF, _ := readOPF(root, bookPath)
		matched = hasOPF
		for dir := filepath.Dir(bookPath); !matched && within(root, dir); dir = filepath.Dir(dir) {
			if _, err := safeRegular(root, filepath.Join(dir, "metadata.db"), maxDatabaseBytes); err == nil {
				matched = true
			}
			if dir == root {
				break
			}
		}
	}
	if !matched {
		return "", "", os.ErrNotExist
	}
	path := filepath.Join(filepath.Dir(bookPath), "cover.jpg")
	info, err := safeRegular(root, path, maxCoverBytes)
	if err != nil {
		return "", "", err
	}
	data, err := readBounded(root, path, maxCoverBytes)
	if err != nil {
		return "", "", err
	}
	config, decodeErr := jpeg.DecodeConfig(bytes.NewReader(data))
	if decodeErr != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 40000000 {
		return "", "", fmt.Errorf("Calibre cover is not a valid bounded JPEG")
	}
	// A valid SOF header does not establish a valid entropy-coded image. Decode
	// the same bounded snapshot fully before allowing it to override a cover.
	if _, err := jpeg.Decode(bytes.NewReader(data)); err != nil {
		return "", "", fmt.Errorf("Calibre cover has invalid JPEG image data: %w", err)
	}
	after, statErr := os.Stat(path)
	if statErr != nil || !os.SameFile(info, after) || info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) {
		return "", "", fmt.Errorf("Calibre cover changed while validating")
	}
	sum := sha256.Sum256([]byte(path + "\x00" + statMarker(info)))
	return path, "calibre-cover-" + hex.EncodeToString(sum[:16]), nil
}
