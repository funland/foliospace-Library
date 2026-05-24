package store

import (
	"database/sql"
	"fmt"
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
	res, err := s.db.Exec(`INSERT INTO libraries(name, root_path) VALUES(?, ?)
		ON CONFLICT(root_path) DO UPDATE SET name = excluded.name, updated_at = CURRENT_TIMESTAMP`, name, rootPath)
	if err != nil {
		return domain.Library{}, err
	}
	id, _ := res.LastInsertId()
	if id == 0 {
		return s.LibraryByRoot(rootPath)
	}
	return s.LibraryByID(id)
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

func (s *Store) UpsertSeries(libraryID int64, title string) (domain.Series, error) {
	res, err := s.db.Exec(`INSERT INTO series(library_id, title) VALUES(?, ?)
		ON CONFLICT(library_id, title) DO UPDATE SET updated_at = CURRENT_TIMESTAMP`, libraryID, title)
	if err != nil {
		return domain.Series{}, err
	}
	id, _ := res.LastInsertId()
	if id == 0 {
		row := s.db.QueryRow(`SELECT id, library_id, title, 0 FROM series WHERE library_id = ? AND title = ?`, libraryID, title)
		return scanSeries(row)
	}
	row := s.db.QueryRow(`SELECT id, library_id, title, 0 FROM series WHERE id = ?`, id)
	return scanSeries(row)
}

func (s *Store) ListSeries() ([]domain.Series, error) {
	rows, err := s.db.Query(`SELECT s.id, s.library_id, s.title, COUNT(b.id)
		FROM series s LEFT JOIN books b ON b.series_id = s.id
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

func (s *Store) UpsertBook(seriesID int64, title string, format string) (domain.Book, error) {
	res, err := s.db.Exec(`INSERT INTO books(series_id, title, format) VALUES(?, ?, ?)
		ON CONFLICT(series_id, title, format) DO UPDATE SET updated_at = CURRENT_TIMESTAMP`, seriesID, title, format)
	if err != nil {
		return domain.Book{}, err
	}
	id, _ := res.LastInsertId()
	if id == 0 {
		return s.BookBySeriesTitle(seriesID, title, format)
	}
	return s.BookByID(id)
}

func (s *Store) BookBySeriesTitle(seriesID int64, title string, format string) (domain.Book, error) {
	row := s.db.QueryRow(`SELECT b.id, b.series_id, b.title, b.format, b.page_count, b.cover_status, b.analyzed, COALESCE(f.abs_path, '')
		FROM books b LEFT JOIN files f ON f.book_id = b.id
		WHERE b.series_id = ? AND b.title = ? AND b.format = ?`, seriesID, title, format)
	return scanBook(row)
}

func (s *Store) BookByID(id int64) (domain.Book, error) {
	row := s.db.QueryRow(`SELECT b.id, b.series_id, b.title, b.format, b.page_count, b.cover_status, b.analyzed, COALESCE(f.abs_path, '')
		FROM books b LEFT JOIN files f ON f.book_id = b.id
		WHERE b.id = ?`, id)
	return scanBook(row)
}

func (s *Store) ListBooks(seriesID int64) ([]domain.Book, error) {
	rows, err := s.db.Query(`SELECT b.id, b.series_id, b.title, b.format, b.page_count, b.cover_status, b.analyzed, COALESCE(f.abs_path, '')
		FROM books b LEFT JOIN files f ON f.book_id = b.id
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
	_, err := s.db.Exec(`UPDATE scan_jobs SET status = ?, discovered_files = ?, indexed_files = ?, skipped_files = ?, error_count = ?, finished_at = ? WHERE id = ?`,
		job.Status, job.DiscoveredFiles, job.IndexedFiles, job.SkippedFiles, job.ErrorCount, formatOptionalTime(job.FinishedAt), job.ID)
	return err
}

func (s *Store) ScanJobByID(id int64) (domain.ScanJob, error) {
	row := s.db.QueryRow(`SELECT id, library_id, status, discovered_files, indexed_files, skipped_files, error_count, started_at, finished_at FROM scan_jobs WHERE id = ?`, id)
	return scanJob(row)
}

func (s *Store) ListScanJobs() ([]domain.ScanJob, error) {
	rows, err := s.db.Query(`SELECT id, library_id, status, discovered_files, indexed_files, skipped_files, error_count, started_at, finished_at FROM scan_jobs ORDER BY id DESC LIMIT 50`)
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
	_, err := s.db.Exec(`INSERT INTO read_progress(book_id, page_index) VALUES(?, ?)
		ON CONFLICT(book_id) DO UPDATE SET page_index = excluded.page_index, updated_at = CURRENT_TIMESTAMP`, bookID, pageIndex)
	return err
}

func (s *Store) Progress(bookID int64) (domain.ReadProgress, error) {
	row := s.db.QueryRow(`SELECT book_id, page_index, updated_at FROM read_progress WHERE book_id = ?`, bookID)
	var progress domain.ReadProgress
	var updated string
	if err := row.Scan(&progress.BookID, &progress.PageIndex, &updated); err != nil {
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
	rows, err := s.db.Query(`SELECT id, library_id, book_id, file_id, job_id, path, code, message, first_seen, last_seen FROM file_errors ORDER BY last_seen DESC, id DESC`)
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
	if err := row.Scan(&series.ID, &series.LibraryID, &series.Title, &series.BookCount); err != nil {
		return series, err
	}
	return series, nil
}

func scanBook(row scanner) (domain.Book, error) {
	var book domain.Book
	var analyzed int
	if err := row.Scan(&book.ID, &book.SeriesID, &book.Title, &book.Format, &book.PageCount, &book.CoverStatus, &analyzed, &book.FilePath); err != nil {
		return book, err
	}
	book.Analyzed = analyzed != 0
	return book, nil
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
	if err := row.Scan(&job.ID, &job.LibraryID, &job.Status, &job.DiscoveredFiles, &job.IndexedFiles, &job.SkippedFiles, &job.ErrorCount, &started, &finished); err != nil {
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
