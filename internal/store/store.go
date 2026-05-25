package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"foliospace-reader/internal/domain"
)

type Store struct {
	db *sql.DB
}

func New(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) CreateLibrary(name string, rootPath string) (domain.Library, error) {
	_, err := s.db.Exec(`INSERT INTO libraries(name, root_path) VALUES(?, ?)
		ON CONFLICT(root_path) DO UPDATE SET name = excluded.name, updated_at = CURRENT_TIMESTAMP`, name, rootPath)
	if err != nil {
		return domain.Library{}, err
	}
	return s.LibraryByRoot(rootPath)
}

func (s *Store) LibraryByID(id int64) (domain.Library, error) {
	row := s.db.QueryRow(`SELECT id, name, root_path, created_at, updated_at FROM libraries WHERE id = ?`, id)
	return scanLibrary(row)
}

func (s *Store) LibraryByRoot(rootPath string) (domain.Library, error) {
	row := s.db.QueryRow(`SELECT id, name, root_path, created_at, updated_at FROM libraries WHERE root_path = ?`, rootPath)
	return scanLibrary(row)
}

func (s *Store) ListLibraries() ([]domain.Library, error) {
	rows, err := s.db.Query(`SELECT id, name, root_path, created_at, updated_at FROM libraries ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Library
	for rows.Next() {
		lib, err := scanLibrary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, lib)
	}
	return out, rows.Err()
}

func (s *Store) DeleteLibrary(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`SELECT b.id FROM books b JOIN series s ON s.id = b.series_id WHERE s.library_id = ?`, id)
	if err != nil {
		return err
	}
	var bookIDs []int64
	for rows.Next() {
		var bookID int64
		if err := rows.Scan(&bookID); err != nil {
			_ = rows.Close()
			return err
		}
		bookIDs = append(bookIDs, bookID)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, bookID := range bookIDs {
		if _, err := tx.Exec(`DELETE FROM read_progress WHERE book_id = ?`, bookID); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM pages WHERE book_id = ?`, bookID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM file_errors WHERE library_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM job_events WHERE job_id IN (SELECT id FROM scan_jobs WHERE library_id = ?)`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM scan_jobs WHERE library_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM files WHERE library_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM books WHERE series_id IN (SELECT id FROM series WHERE library_id = ?)`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM series WHERE library_id = ?`, id); err != nil {
		return err
	}
	res, err := tx.Exec(`DELETE FROM libraries WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

func (s *Store) UpsertSeries(libraryID int64, title string, directoryPath string) (domain.Series, error) {
	_, err := s.db.Exec(`INSERT INTO series(library_id, title, directory_path, collection_type) VALUES(?, ?, ?, 'directory')
		ON CONFLICT(library_id, title) DO UPDATE SET directory_path = excluded.directory_path, collection_type = 'directory', updated_at = CURRENT_TIMESTAMP`,
		libraryID, title, directoryPath)
	if err != nil {
		return domain.Series{}, err
	}
	row := s.db.QueryRow(`SELECT id, library_id, title, directory_path, collection_type, 0 FROM series WHERE library_id = ? AND title = ?`, libraryID, title)
	return scanSeries(row)
}

func (s *Store) ListSeries() ([]domain.Series, error) {
	rows, err := s.db.Query(`SELECT s.id, s.library_id, s.title,
			COALESCE(NULLIF(s.directory_path, ''), MIN(CASE
				WHEN f.rel_path IS NULL THEN ''
				WHEN INSTR(f.rel_path, '/') = 0 THEN '.'
				ELSE SUBSTR(f.rel_path, 1, INSTR(f.rel_path, '/') - 1)
			END), ''),
			s.collection_type,
			COUNT(DISTINCT b.id)
		FROM series s
		LEFT JOIN books b ON b.series_id = s.id
		LEFT JOIN files f ON f.book_id = b.id
		GROUP BY s.id, s.library_id, s.title
		ORDER BY s.title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Series
	for rows.Next() {
		series, err := scanSeries(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, series)
	}
	return out, rows.Err()
}

func (s *Store) DeleteEmptySeries(libraryID int64) error {
	_, err := s.db.Exec(`DELETE FROM series
		WHERE library_id = ?
		AND id NOT IN (SELECT DISTINCT series_id FROM books)`, libraryID)
	return err
}

func (s *Store) UpsertBook(seriesID int64, title string, format string) (domain.Book, error) {
	_, err := s.db.Exec(`INSERT INTO books(series_id, title, format) VALUES(?, ?, ?)
		ON CONFLICT(series_id, title, format) DO UPDATE SET updated_at = CURRENT_TIMESTAMP`, seriesID, title, format)
	if err != nil {
		return domain.Book{}, err
	}
	return s.BookBySeriesTitle(seriesID, title, format)
}

func (s *Store) BookBySeriesTitle(seriesID int64, title string, format string) (domain.Book, error) {
	row := s.db.QueryRow(bookSelectSQL()+`
		WHERE b.series_id = ? AND b.title = ? AND b.format = ?`, seriesID, title, format)
	return scanBook(row)
}

func (s *Store) BookByID(id int64) (domain.Book, error) {
	row := s.db.QueryRow(bookSelectSQL()+` WHERE b.id = ?`, id)
	return scanBook(row)
}

func (s *Store) UpdateBookIdentity(bookID int64, seriesID int64, title string, format string) (domain.Book, error) {
	_, err := s.db.Exec(`UPDATE books
		SET series_id = ?, title = ?, format = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`, seriesID, title, format, bookID)
	if err != nil {
		return domain.Book{}, err
	}
	return s.BookByID(bookID)
}

func (s *Store) UpdateBookPrivateState(bookID int64, state domain.BookPrivateState) error {
	status := strings.TrimSpace(state.Status)
	summary := strings.TrimSpace(state.Summary)
	rating := state.Rating
	if rating < 0 {
		rating = 0
	}
	if rating > 5 {
		rating = 5
	}
	favorite := 0
	if state.Favorite {
		favorite = 1
	}
	_, err := s.db.Exec(`UPDATE books
		SET private_status = ?, favorite = ?, rating = ?, tags = ?, summary = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`, status, favorite, rating, encodeTags(state.Tags), summary, bookID)
	return err
}

func (s *Store) ListBooks(seriesID int64) ([]domain.Book, error) {
	rows, err := s.db.Query(bookSelectSQL()+`
		WHERE b.series_id = ?
		ORDER BY b.title`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Book
	for rows.Next() {
		book, err := scanBook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, book)
	}
	return out, rows.Err()
}

func (s *Store) SearchBooks(query string, limit int) ([]domain.Book, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []domain.Book{}, nil
	}
	limit = normalizeSearchLimit(limit)
	pattern := "%" + escapeLike(query) + "%"
	rows, err := s.db.Query(bookSelectSQL()+`
		WHERE LOWER(b.title) LIKE LOWER(?) ESCAPE '\'
			OR LOWER(s.title) LIKE LOWER(?) ESCAPE '\'
			OR LOWER(b.format) LIKE LOWER(?) ESCAPE '\'
			OR LOWER(b.tags) LIKE LOWER(?) ESCAPE '\'
			OR LOWER(b.summary) LIKE LOWER(?) ESCAPE '\'
		ORDER BY b.favorite DESC, rp.updated_at IS NULL, rp.updated_at DESC, b.updated_at DESC, b.title
		LIMIT ?`, pattern, pattern, pattern, pattern, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Book, 0)
	for rows.Next() {
		book, err := scanBook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, book)
	}
	return out, rows.Err()
}

func normalizeSearchLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func (s *Store) ListBooksPage(options domain.BookListOptions) (domain.BookListPage, error) {
	options.Limit = normalizeBookListLimit(options.Limit)
	if options.Offset < 0 {
		options.Offset = 0
	}

	where, args := bookListWhere(options)
	countArgs := append([]any(nil), args...)
	var total int64
	if err := s.db.QueryRow(`SELECT COUNT(*)
		FROM books b
		JOIN series s ON s.id = b.series_id
		LEFT JOIN read_progress rp ON rp.book_id = b.id`+where, countArgs...).Scan(&total); err != nil {
		return domain.BookListPage{}, err
	}

	queryArgs := append([]any(nil), args...)
	queryArgs = append(queryArgs, options.Limit, options.Offset)
	rows, err := s.db.Query(bookSelectSQL()+where+bookListOrderBy(options.Sort)+`
		LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return domain.BookListPage{}, err
	}
	defer rows.Close()

	items := make([]domain.Book, 0)
	for rows.Next() {
		book, err := scanBook(rows)
		if err != nil {
			return domain.BookListPage{}, err
		}
		items = append(items, book)
	}
	if err := rows.Err(); err != nil {
		return domain.BookListPage{}, err
	}
	return domain.BookListPage{
		Items:   items,
		Total:   total,
		Limit:   options.Limit,
		Offset:  options.Offset,
		HasMore: int64(options.Offset+len(items)) < total,
	}, nil
}

func normalizeBookListLimit(limit int) int {
	if limit <= 0 {
		return 60
	}
	if limit > 200 {
		return 200
	}
	return limit
}

func bookListWhere(options domain.BookListOptions) (string, []any) {
	where := " WHERE b.series_id = ?"
	args := []any{options.SeriesID}
	query := strings.TrimSpace(options.Query)
	if query != "" {
		where += ` AND LOWER(b.title) LIKE LOWER(?) ESCAPE '\'`
		args = append(args, "%"+escapeLike(query)+"%")
	}
	return where, args
}

func bookListOrderBy(sort string) string {
	switch sort {
	case "recently_added":
		return " ORDER BY b.created_at DESC, b.id DESC"
	case "last_read":
		return " ORDER BY rp.updated_at IS NULL, rp.updated_at DESC, b.title"
	case "progress":
		return " ORDER BY rp.progress_fraction DESC, rp.updated_at DESC, b.title"
	case "unread":
		return " ORDER BY COALESCE(rp.progress_fraction, 0) ASC, b.title"
	default:
		return " ORDER BY b.title"
	}
}

func escapeLike(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}

func (s *Store) ListContinueReading(limit int) ([]domain.Book, error) {
	if limit <= 0 {
		limit = 12
	}
	rows, err := s.db.Query(bookSelectSQL()+`
		WHERE rp.book_id IS NOT NULL
		ORDER BY rp.updated_at DESC, b.updated_at DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Book
	for rows.Next() {
		book, err := scanBook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, book)
	}
	return out, rows.Err()
}

func (s *Store) ListRecentBooks(limit int) ([]domain.Book, error) {
	if limit <= 0 {
		limit = 12
	}
	rows, err := s.db.Query(bookSelectSQL()+`
		ORDER BY b.created_at DESC, b.id DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Book
	for rows.Next() {
		book, err := scanBook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, book)
	}
	return out, rows.Err()
}

func (s *Store) ListFavoriteBooks(limit int) ([]domain.Book, error) {
	limit = normalizeShelfLimit(limit)
	rows, err := s.db.Query(bookSelectSQL()+`
		WHERE b.favorite = 1
		ORDER BY b.updated_at DESC, b.title
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBooks(rows)
}

func (s *Store) ListBooksByPrivateStatus(status string, limit int) ([]domain.Book, error) {
	limit = normalizeShelfLimit(limit)
	rows, err := s.db.Query(bookSelectSQL()+`
		WHERE b.private_status = ?
		ORDER BY b.updated_at DESC, b.title
		LIMIT ?`, strings.TrimSpace(status), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBooks(rows)
}

func normalizeShelfLimit(limit int) int {
	if limit <= 0 {
		return 12
	}
	if limit > 50 {
		return 50
	}
	return limit
}

func scanBooks(rows *sql.Rows) ([]domain.Book, error) {
	out := make([]domain.Book, 0)
	for rows.Next() {
		book, err := scanBook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, book)
	}
	return out, rows.Err()
}

func (s *Store) UpsertFile(bookID int64, libraryID int64, absPath string, relPath string, size int64, mtime time.Time, ext string) (domain.File, error) {
	_, err := s.db.Exec(`INSERT INTO files(book_id, library_id, abs_path, rel_path, size, mtime, ext) VALUES(?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(abs_path) DO UPDATE SET book_id = excluded.book_id, library_id = excluded.library_id, rel_path = excluded.rel_path, size = excluded.size, mtime = excluded.mtime, ext = excluded.ext, updated_at = CURRENT_TIMESTAMP`,
		bookID, libraryID, absPath, relPath, size, mtime.Format(time.RFC3339Nano), ext)
	if err != nil {
		return domain.File{}, err
	}
	row := s.db.QueryRow(`SELECT id, book_id, library_id, abs_path, rel_path, size, mtime, ext FROM files WHERE abs_path = ?`, absPath)
	return scanFile(row)
}

type FileIndex struct {
	File      domain.File
	Analyzed  bool
	PageCount int
}

func (s *Store) FileIndexByPath(absPath string) (FileIndex, error) {
	row := s.db.QueryRow(`SELECT f.id, f.book_id, f.library_id, f.abs_path, f.rel_path, f.size, f.mtime, f.ext, b.analyzed, b.page_count
		FROM files f JOIN books b ON b.id = f.book_id
		WHERE f.abs_path = ?`, absPath)
	var item FileIndex
	var mtime string
	var analyzed int
	if err := row.Scan(&item.File.ID, &item.File.BookID, &item.File.LibraryID, &item.File.AbsPath, &item.File.RelPath, &item.File.Size, &mtime, &item.File.Ext, &analyzed, &item.PageCount); err != nil {
		return item, err
	}
	item.File.MTime = parseTime(mtime)
	item.Analyzed = analyzed != 0
	return item, nil
}

func (s *Store) ReplacePages(bookID int64, pages []domain.Page) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM pages WHERE book_id = ?`, bookID); err != nil {
		_ = tx.Rollback()
		return err
	}
	for _, page := range pages {
		if _, err := tx.Exec(`INSERT INTO pages(book_id, page_index, entry_name) VALUES(?, ?, ?)`, bookID, page.Index, page.Name); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	_, err = tx.Exec(`UPDATE books SET page_count = ?, analyzed = 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, len(pages), bookID)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) ListPages(bookID int64) ([]domain.Page, error) {
	rows, err := s.db.Query(`SELECT page_index, entry_name FROM pages WHERE book_id = ? ORDER BY page_index`, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Page
	for rows.Next() {
		var page domain.Page
		if err := rows.Scan(&page.Index, &page.Name); err != nil {
			return nil, err
		}
		out = append(out, page)
	}
	return out, rows.Err()
}

func (s *Store) StartScanJob(libraryID int64) (domain.ScanJob, error) {
	res, err := s.db.Exec(`INSERT INTO scan_jobs(library_id, status) VALUES(?, 'running')`, libraryID)
	if err != nil {
		return domain.ScanJob{}, err
	}
	id, _ := res.LastInsertId()
	return s.ScanJobByID(id)
}

func (s *Store) UpdateScanJob(job domain.ScanJob) error {
	_, err := s.db.Exec(`UPDATE scan_jobs SET status = ?, current_path = ?, discovered_files = ?, indexed_files = ?, skipped_files = ?, error_count = ?, finished_at = ? WHERE id = ?`,
		job.Status, job.CurrentPath, job.DiscoveredFiles, job.IndexedFiles, job.SkippedFiles, job.ErrorCount, formatOptionalTime(job.FinishedAt), job.ID)
	return err
}

func (s *Store) ScanJobByID(id int64) (domain.ScanJob, error) {
	row := s.db.QueryRow(`SELECT id, library_id, status, current_path, discovered_files, indexed_files, skipped_files, error_count, started_at, finished_at FROM scan_jobs WHERE id = ?`, id)
	return scanJob(row)
}

func (s *Store) ListScanJobs() ([]domain.ScanJob, error) {
	rows, err := s.db.Query(`SELECT id, library_id, status, current_path, discovered_files, indexed_files, skipped_files, error_count, started_at, finished_at FROM scan_jobs ORDER BY id DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.ScanJob
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func (s *Store) AddJobEvent(jobID int64, level string, message string) error {
	_, err := s.db.Exec(`INSERT INTO job_events(job_id, level, message) VALUES(?, ?, ?)`, jobID, level, message)
	return err
}

func (s *Store) ListJobEvents(jobID int64) ([]domain.JobEvent, error) {
	rows, err := s.db.Query(`SELECT id, job_id, level, message, created_at FROM job_events WHERE job_id = ? ORDER BY id`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.JobEvent
	for rows.Next() {
		var event domain.JobEvent
		var created string
		if err := rows.Scan(&event.ID, &event.JobID, &event.Level, &event.Message, &created); err != nil {
			return nil, err
		}
		event.CreatedAt = parseTime(created)
		out = append(out, event)
	}
	return out, rows.Err()
}

func (s *Store) SaveProgress(bookID int64, pageIndex int) error {
	return s.SaveProgressDetail(bookID, pageIndex, "", 0)
}

func (s *Store) SaveProgressDetail(bookID int64, pageIndex int, locator string, progressFraction float64) error {
	_, err := s.db.Exec(`INSERT INTO read_progress(book_id, page_index, locator, progress_fraction) VALUES(?, ?, ?, ?)
		ON CONFLICT(book_id) DO UPDATE SET page_index = excluded.page_index, locator = excluded.locator, progress_fraction = excluded.progress_fraction, updated_at = CURRENT_TIMESTAMP`,
		bookID, pageIndex, locator, progressFraction)
	return err
}

func (s *Store) Progress(bookID int64) (domain.ReadProgress, error) {
	row := s.db.QueryRow(`SELECT book_id, page_index, locator, progress_fraction, updated_at FROM read_progress WHERE book_id = ?`, bookID)
	var progress domain.ReadProgress
	var updated string
	if err := row.Scan(&progress.BookID, &progress.PageIndex, &progress.Locator, &progress.ProgressFraction, &updated); err != nil {
		return progress, err
	}
	progress.UpdatedAt = parseTime(updated)
	return progress, nil
}

func (s *Store) RecordFileError(input domain.FileErrorInput) error {
	_, err := s.db.Exec(`INSERT INTO file_errors(library_id, book_id, file_id, job_id, path, code, message) VALUES(?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(path, code) DO UPDATE SET message = excluded.message, job_id = excluded.job_id, last_seen = CURRENT_TIMESTAMP`,
		input.LibraryID, input.BookID, input.FileID, input.JobID, input.Path, string(input.Code), input.Message)
	return err
}

func (s *Store) ListFileErrors() ([]domain.FileError, error) {
	return s.ListFileErrorsByJob(0)
}

func (s *Store) ListFileErrorsByJob(jobID int64) ([]domain.FileError, error) {
	query := `SELECT id, library_id, book_id, file_id, job_id, path, code, message, first_seen, last_seen FROM file_errors`
	args := []any{}
	if jobID > 0 {
		query += ` WHERE job_id = ?`
		args = append(args, jobID)
	}
	query += ` ORDER BY last_seen DESC, id DESC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.FileError
	for rows.Next() {
		var item domain.FileError
		var code string
		var firstSeen string
		var lastSeen string
		if err := rows.Scan(&item.ID, &item.LibraryID, &item.BookID, &item.FileID, &item.JobID, &item.Path, &code, &item.Message, &firstSeen, &lastSeen); err != nil {
			return nil, err
		}
		item.Code = domain.ErrorCode(code)
		item.FirstSeen = parseTime(firstSeen)
		item.LastSeen = parseTime(lastSeen)
		out = append(out, item)
	}
	return out, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanLibrary(row scanner) (domain.Library, error) {
	var lib domain.Library
	var created string
	var updated string
	if err := row.Scan(&lib.ID, &lib.Name, &lib.RootPath, &created, &updated); err != nil {
		return lib, err
	}
	lib.CreatedAt = parseTime(created)
	lib.UpdatedAt = parseTime(updated)
	return lib, nil
}

func scanSeries(row scanner) (domain.Series, error) {
	var series domain.Series
	if err := row.Scan(
		&series.ID,
		&series.LibraryID,
		&series.Title,
		&series.DirectoryPath,
		&series.CollectionType,
		&series.BookCount,
	); err != nil {
		return series, err
	}
	return series, nil
}

func scanBook(row scanner) (domain.Book, error) {
	var book domain.Book
	var analyzed int
	var favorite int
	var addedAt string
	var updatedAt string
	var lastReadAt string
	var tags string
	if err := row.Scan(
		&book.ID,
		&book.SeriesID,
		&book.CollectionTitle,
		&book.Title,
		&book.Format,
		&book.PageCount,
		&book.CoverStatus,
		&analyzed,
		&book.FilePath,
		&addedAt,
		&updatedAt,
		&book.CurrentPage,
		&book.ProgressFraction,
		&lastReadAt,
		&book.PrivateStatus,
		&favorite,
		&book.Rating,
		&tags,
		&book.Summary,
	); err != nil {
		return book, err
	}
	book.BookType = "single_volume"
	book.Analyzed = analyzed != 0
	book.Favorite = favorite != 0
	book.Tags = decodeTags(tags)
	book.AddedAt = parseTime(addedAt)
	book.UpdatedAt = parseTime(updatedAt)
	book.LastReadAt = parseTime(lastReadAt)
	return book, nil
}

func encodeTags(tags []string) string {
	clean := make([]string, 0, len(tags))
	seen := map[string]bool{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		clean = append(clean, tag)
	}
	return strings.Join(clean, ",")
}

func decodeTags(value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{}
	}
	parts := strings.Split(value, ",")
	tags := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			tags = append(tags, part)
		}
	}
	return tags
}

func bookSelectSQL() string {
	return `SELECT b.id, b.series_id, s.title, b.title, b.format, b.page_count, b.cover_status, b.analyzed,
			COALESCE(f.abs_path, ''), b.created_at, b.updated_at,
			COALESCE(rp.page_index, 0), COALESCE(rp.progress_fraction, 0), COALESCE(rp.updated_at, ''),
			b.private_status, b.favorite, b.rating, b.tags, b.summary
		FROM books b
		JOIN series s ON s.id = b.series_id
		LEFT JOIN files f ON f.book_id = b.id
		LEFT JOIN read_progress rp ON rp.book_id = b.id`
}

func scanFile(row scanner) (domain.File, error) {
	var file domain.File
	var mtime string
	if err := row.Scan(&file.ID, &file.BookID, &file.LibraryID, &file.AbsPath, &file.RelPath, &file.Size, &mtime, &file.Ext); err != nil {
		return file, err
	}
	file.MTime = parseTime(mtime)
	return file, nil
}

func scanJob(row scanner) (domain.ScanJob, error) {
	var job domain.ScanJob
	var started string
	var finished string
	if err := row.Scan(&job.ID, &job.LibraryID, &job.Status, &job.CurrentPath, &job.DiscoveredFiles, &job.IndexedFiles, &job.SkippedFiles, &job.ErrorCount, &started, &finished); err != nil {
		return job, err
	}
	job.StartedAt = parseTime(started)
	if finished != "" {
		job.FinishedAt = parseTime(finished)
	}
	return job, nil
}

func parseTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339Nano)
}

func NotFound(err error) bool {
	return err == sql.ErrNoRows
}

func WrapNotFound(name string, err error) error {
	if err == sql.ErrNoRows {
		return fmt.Errorf("%s not found", name)
	}
	return err
}
