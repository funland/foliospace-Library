package store

import (
	"database/sql"
	"fmt"
	"sort"
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

func (s *Store) Setting(key string) (string, error) {
	row := s.db.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, strings.TrimSpace(key))
	var value string
	if err := row.Scan(&value); err != nil {
		return "", err
	}
	return value, nil
}

func (s *Store) UpsertSetting(key string, value string) error {
	_, err := s.db.Exec(`INSERT INTO app_settings(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP`,
		strings.TrimSpace(key), strings.TrimSpace(value))
	return err
}

func (s *Store) CreateLibrary(name string, rootPath string) (domain.Library, error) {
	return s.CreateLibraryWithType(name, rootPath, "mixed")
}

func (s *Store) CreateLibraryWithType(name string, rootPath string, assetType string) (domain.Library, error) {
	assetType = normalizeLibraryAssetType(assetType)
	_, err := s.db.Exec(`INSERT INTO libraries(name, root_path, asset_type) VALUES(?, ?, ?)
		ON CONFLICT(root_path) DO UPDATE SET name = excluded.name, asset_type = excluded.asset_type, updated_at = CURRENT_TIMESTAMP`, name, rootPath, assetType)
	if err != nil {
		return domain.Library{}, err
	}
	return s.LibraryByRoot(rootPath)
}

func (s *Store) LibraryByID(id int64) (domain.Library, error) {
	row := s.db.QueryRow(`SELECT id, name, root_path, asset_type, created_at, updated_at FROM libraries WHERE id = ?`, id)
	return scanLibrary(row)
}

func (s *Store) LibraryByRoot(rootPath string) (domain.Library, error) {
	row := s.db.QueryRow(`SELECT id, name, root_path, asset_type, created_at, updated_at FROM libraries WHERE root_path = ?`, rootPath)
	return scanLibrary(row)
}

func (s *Store) ListLibraries() ([]domain.Library, error) {
	rows, err := s.db.Query(`SELECT id, name, root_path, asset_type, created_at, updated_at FROM libraries ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Library, 0)
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
	if _, err := tx.Exec(`DELETE FROM games WHERE library_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM videos WHERE library_id = ?`, id); err != nil {
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
	row := s.db.QueryRow(`SELECT s.id, s.library_id, s.title, s.directory_path, s.collection_type,
			CASE WHEN l.asset_type IN ('book', 'comic', 'game', 'video') THEN l.asset_type ELSE 'comic' END,
			0
		FROM series s
		JOIN libraries l ON l.id = s.library_id
		WHERE s.library_id = ? AND s.title = ?`, libraryID, title)
	return scanSeries(row)
}

func (s *Store) SeriesByID(id int64) (domain.Series, error) {
	row := s.db.QueryRow(`SELECT s.id, s.library_id, s.title,
			COALESCE(NULLIF(s.directory_path, ''), ''),
			s.collection_type,
			CASE
				WHEN l.asset_type IN ('book', 'comic', 'game', 'video') THEN l.asset_type
				WHEN SUM(CASE WHEN b.format IN ('epub', 'pdf') THEN 1 ELSE 0 END) > SUM(CASE WHEN b.format IN ('cbz', 'zip', 'cbr', 'rar', '7z') THEN 1 ELSE 0 END) THEN 'book'
				ELSE 'comic'
			END,
			COUNT(DISTINCT b.id)
		FROM series s
		JOIN libraries l ON l.id = s.library_id
		LEFT JOIN books b ON b.series_id = s.id
		WHERE s.id = ?
		GROUP BY s.id, s.library_id, s.title, l.asset_type`, id)
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
			CASE
				WHEN l.asset_type IN ('book', 'comic', 'game', 'video') THEN l.asset_type
				WHEN SUM(CASE WHEN b.format IN ('epub', 'pdf') THEN 1 ELSE 0 END) > SUM(CASE WHEN b.format IN ('cbz', 'zip', 'cbr', 'rar', '7z') THEN 1 ELSE 0 END) THEN 'book'
				ELSE 'comic'
			END,
			COUNT(DISTINCT b.id)
		FROM series s
		JOIN libraries l ON l.id = s.library_id
		LEFT JOIN books b ON b.series_id = s.id
		LEFT JOIN files f ON f.book_id = b.id
		GROUP BY s.id, s.library_id, s.title, l.asset_type
		ORDER BY s.title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Series, 0)
	for rows.Next() {
		series, err := scanSeries(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, series)
	}
	return out, rows.Err()
}

func (s *Store) ListGamePlatformCollections() ([]domain.Series, error) {
	rows, err := s.db.Query(`SELECT platform, COUNT(*) FROM games GROUP BY platform`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Series, 0)
	for rows.Next() {
		var platform string
		var count int64
		if err := rows.Scan(&platform, &count); err != nil {
			return nil, err
		}
		platform = strings.TrimSpace(platform)
		if platform == "" {
			platform = "unknown"
		}
		out = append(out, domain.Series{
			ID:             GamePlatformCollectionID(platform),
			Title:          "Games / " + GamePlatformLabel(platform),
			DirectoryPath:  "Games",
			CollectionType: "game_platform",
			PrimaryType:    "game",
			BookCount:      count,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i int, j int) bool {
		left := GamePlatformSortRank(platformFromGameCollectionTitle(out[i].Title))
		right := GamePlatformSortRank(platformFromGameCollectionTitle(out[j].Title))
		if left != right {
			return left < right
		}
		return out[i].Title < out[j].Title
	})
	return out, nil
}

func (s *Store) DeleteEmptySeries(libraryID int64) error {
	_, err := s.db.Exec(`DELETE FROM series
		WHERE library_id = ?
		AND id NOT IN (SELECT DISTINCT series_id FROM books)`, libraryID)
	return err
}

func (s *Store) DeleteSkippedDirectoryEntries(libraryID int64, names []string) (int64, error) {
	if len(names) == 0 {
		return 0, nil
	}
	conditions, args := skippedDirectoryConditions("rel_path", names)
	args = append([]any{libraryID}, args...)

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`SELECT DISTINCT book_id FROM files WHERE library_id = ? AND (`+conditions+`)`, args...)
	if err != nil {
		return 0, err
	}
	var bookIDs []int64
	for rows.Next() {
		var bookID int64
		if err := rows.Scan(&bookID); err != nil {
			_ = rows.Close()
			return 0, err
		}
		bookIDs = append(bookIDs, bookID)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	fileRes, err := tx.Exec(`DELETE FROM files WHERE library_id = ? AND (`+conditions+`)`, args...)
	if err != nil {
		return 0, err
	}
	deletedFiles, err := fileRes.RowsAffected()
	if err != nil {
		return 0, err
	}

	gameConditions, gameArgs := skippedDirectoryConditions("rel_path", names)
	gameArgs = append([]any{libraryID}, gameArgs...)
	gameRes, err := tx.Exec(`DELETE FROM games WHERE library_id = ? AND (`+gameConditions+`)`, gameArgs...)
	if err != nil {
		return 0, err
	}
	deletedGames, err := gameRes.RowsAffected()
	if err != nil {
		return 0, err
	}

	videoConditions, videoArgs := skippedDirectoryConditions("rel_path", names)
	videoArgs = append([]any{libraryID}, videoArgs...)
	videoRes, err := tx.Exec(`DELETE FROM videos WHERE library_id = ? AND (`+videoConditions+`)`, videoArgs...)
	if err != nil {
		return 0, err
	}
	deletedVideos, err := videoRes.RowsAffected()
	if err != nil {
		return 0, err
	}

	errorConditions, errorArgs := skippedDirectoryConditions("path", names)
	errorArgs = append([]any{libraryID}, errorArgs...)
	if _, err := tx.Exec(`DELETE FROM file_errors WHERE library_id = ? AND (`+errorConditions+`)`, errorArgs...); err != nil {
		return 0, err
	}

	orphanBookIDs, err := orphanedBookIDs(tx, bookIDs)
	if err != nil {
		return 0, err
	}
	if len(orphanBookIDs) > 0 {
		placeholders, deleteArgs := int64Placeholders(orphanBookIDs)
		if _, err := tx.Exec(`DELETE FROM read_progress WHERE book_id IN (`+placeholders+`)`, deleteArgs...); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(`DELETE FROM pages WHERE book_id IN (`+placeholders+`)`, deleteArgs...); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(`DELETE FROM books WHERE id IN (`+placeholders+`)`, deleteArgs...); err != nil {
			return 0, err
		}
	}
	if _, err := tx.Exec(`DELETE FROM series
		WHERE library_id = ?
		AND id NOT IN (SELECT DISTINCT series_id FROM books)`, libraryID); err != nil {
		return 0, err
	}
	return deletedFiles + deletedGames + deletedVideos, tx.Commit()
}

func skippedDirectoryConditions(column string, names []string) (string, []any) {
	conditions := make([]string, 0, len(names)*2)
	args := make([]any, 0, len(names)*3)
	for _, name := range names {
		name = strings.Trim(strings.TrimSpace(name), `/\`)
		if name == "" {
			continue
		}
		conditions = append(conditions, column+" = ?", column+" LIKE ?", column+" LIKE ?")
		args = append(args, name, name+"/%", "%/"+name+"/%")
	}
	if len(conditions) == 0 {
		return "1 = 0", nil
	}
	return strings.Join(conditions, " OR "), args
}

func orphanedBookIDs(tx *sql.Tx, ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders, args := int64Placeholders(ids)
	rows, err := tx.Query(`SELECT b.id FROM books b
		WHERE b.id IN (`+placeholders+`)
		AND NOT EXISTS (SELECT 1 FROM files f WHERE f.book_id = b.id)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]int64, 0, len(ids))
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func int64Placeholders(ids []int64) (string, []any) {
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	return strings.Join(placeholders, ","), args
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

func (s *Store) UpdateBookMetadata(bookID int64, creator string, description string) (domain.Book, error) {
	_, err := s.db.Exec(`UPDATE books
		SET creator = ?, description = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`, strings.TrimSpace(creator), strings.TrimSpace(description), bookID)
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

func (s *Store) ClientPreferences() (domain.ClientPreferences, error) {
	row := s.db.QueryRow(`SELECT locale, reader_page_mode, epub_page_mode, epub_theme, epub_font_size FROM client_preferences WHERE id = 1`)
	var prefs domain.ClientPreferences
	err := row.Scan(&prefs.Locale, &prefs.ReaderPageMode, &prefs.EPUBPageMode, &prefs.EPUBTheme, &prefs.EPUBFontSize)
	if err == sql.ErrNoRows {
		return DefaultClientPreferences(), nil
	}
	if err != nil {
		return domain.ClientPreferences{}, err
	}
	return prefs, nil
}

func (s *Store) SaveClientPreferences(prefs domain.ClientPreferences) error {
	_, err := s.db.Exec(`INSERT INTO client_preferences(id, locale, reader_page_mode, epub_page_mode, epub_theme, epub_font_size)
		VALUES(1, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET locale = excluded.locale,
			reader_page_mode = excluded.reader_page_mode,
			epub_page_mode = excluded.epub_page_mode,
			epub_theme = excluded.epub_theme,
			epub_font_size = excluded.epub_font_size,
			updated_at = CURRENT_TIMESTAMP`,
		prefs.Locale, prefs.ReaderPageMode, prefs.EPUBPageMode, prefs.EPUBTheme, prefs.EPUBFontSize)
	return err
}

func DefaultClientPreferences() domain.ClientPreferences {
	return domain.ClientPreferences{
		Locale:         "zh",
		ReaderPageMode: "single",
		EPUBPageMode:   "single",
		EPUBTheme:      "light",
		EPUBFontSize:   18,
	}
}

func (s *Store) ListBooks(seriesID int64) ([]domain.Book, error) {
	rows, err := s.db.Query(bookSelectSQL()+`
		WHERE b.series_id = ?
		ORDER BY b.title`, seriesID)
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

func (s *Store) UpsertGame(game domain.GameAsset) (domain.GameAsset, error) {
	game.Platform = strings.TrimSpace(game.Platform)
	game.ROMSetName = strings.TrimSpace(game.ROMSetName)
	game.Region = strings.TrimSpace(game.Region)
	game.Format = strings.TrimSpace(game.Format)
	game.EmulatorHint = strings.TrimSpace(game.EmulatorHint)
	if strings.TrimSpace(game.Compatibility) == "" {
		game.Compatibility = "unknown"
	}
	_, err := s.db.Exec(`INSERT INTO games(library_id, title, platform, rom_set_name, region, format, file_path, rel_path, size, mtime, crc32, sha1, emulator_hint, compatibility)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(file_path) DO UPDATE SET library_id = excluded.library_id,
			title = excluded.title,
			platform = excluded.platform,
			rom_set_name = excluded.rom_set_name,
			region = excluded.region,
			format = excluded.format,
			rel_path = excluded.rel_path,
			size = excluded.size,
			mtime = excluded.mtime,
			crc32 = excluded.crc32,
			sha1 = excluded.sha1,
			emulator_hint = excluded.emulator_hint,
			compatibility = excluded.compatibility,
			updated_at = CURRENT_TIMESTAMP`,
		game.LibraryID, game.Title, game.Platform, game.ROMSetName, game.Region, game.Format, game.FilePath, game.RelPath, game.Size, game.MTime.Format(time.RFC3339Nano), game.CRC32, game.SHA1, game.EmulatorHint, game.Compatibility)
	if err != nil {
		return domain.GameAsset{}, err
	}
	return s.GameByPath(game.FilePath)
}

func (s *Store) GameByID(id int64) (domain.GameAsset, error) {
	row := s.db.QueryRow(gameSelectSQL()+` WHERE id = ?`, id)
	return scanGame(row)
}

func (s *Store) GameByPath(filePath string) (domain.GameAsset, error) {
	row := s.db.QueryRow(gameSelectSQL()+` WHERE file_path = ?`, filePath)
	return scanGame(row)
}

func (s *Store) DeleteGameByPath(filePath string) error {
	_, err := s.db.Exec(`DELETE FROM games WHERE file_path = ?`, filePath)
	return err
}

func (s *Store) ListRecentGames(limit int) ([]domain.GameAsset, error) {
	limit = normalizeShelfLimit(limit)
	rows, err := s.db.Query(gameSelectSQL()+` ORDER BY updated_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanGames(rows)
}

func (s *Store) ListGamesPage(options domain.GameListOptions) (domain.GameListPage, error) {
	limit := options.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := options.Offset
	if offset < 0 {
		offset = 0
	}

	where, args := gameListWhere(options)
	var total int64
	countQuery := `SELECT COUNT(*) FROM games` + where
	if err := s.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return domain.GameListPage{}, err
	}

	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, limit, offset)
	rows, err := s.db.Query(gameSelectSQL()+where+gameListOrderBy(options.Sort)+` LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return domain.GameListPage{}, err
	}
	defer rows.Close()
	items, err := scanGames(rows)
	if err != nil {
		return domain.GameListPage{}, err
	}
	return domain.GameListPage{
		Items:   items,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
		HasMore: int64(offset+len(items)) < total,
	}, nil
}

func (s *Store) ListGamesByROMSet(romSetName string) ([]domain.GameAsset, error) {
	rows, err := s.db.Query(gameSelectSQL()+` WHERE rom_set_name = ? ORDER BY platform, title`, strings.TrimSpace(romSetName))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanGames(rows)
}

func (s *Store) ListGamesByPlatform(platform string) ([]domain.GameAsset, error) {
	rows, err := s.db.Query(gameSelectSQL()+` WHERE platform = ? ORDER BY title`, strings.TrimSpace(platform))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanGames(rows)
}

func (s *Store) UpsertVideo(video domain.VideoAsset) (domain.VideoAsset, error) {
	video.Format = strings.TrimSpace(video.Format)
	video.VideoCodec = strings.TrimSpace(video.VideoCodec)
	video.AudioCodec = strings.TrimSpace(video.AudioCodec)
	if strings.TrimSpace(video.ThumbnailStatus) == "" {
		video.ThumbnailStatus = "placeholder"
	}
	_, err := s.db.Exec(`INSERT INTO videos(library_id, title, format, file_path, rel_path, size, mtime, duration_seconds, width, height, video_codec, audio_codec, thumbnail_status)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(file_path) DO UPDATE SET library_id = excluded.library_id,
			title = excluded.title,
			format = excluded.format,
			rel_path = excluded.rel_path,
			size = excluded.size,
			mtime = excluded.mtime,
			duration_seconds = excluded.duration_seconds,
			width = excluded.width,
			height = excluded.height,
			video_codec = excluded.video_codec,
			audio_codec = excluded.audio_codec,
			thumbnail_status = excluded.thumbnail_status,
			updated_at = CURRENT_TIMESTAMP`,
		video.LibraryID, video.Title, video.Format, video.FilePath, video.RelPath, video.Size, video.MTime.Format(time.RFC3339Nano), video.DurationSeconds, video.Width, video.Height, video.VideoCodec, video.AudioCodec, video.ThumbnailStatus)
	if err != nil {
		return domain.VideoAsset{}, err
	}
	return s.VideoByPath(video.FilePath)
}

func (s *Store) VideoByID(id int64) (domain.VideoAsset, error) {
	row := s.db.QueryRow(videoSelectSQL()+` WHERE id = ?`, id)
	return scanVideo(row)
}

func (s *Store) VideoByPath(filePath string) (domain.VideoAsset, error) {
	row := s.db.QueryRow(videoSelectSQL()+` WHERE file_path = ?`, filePath)
	return scanVideo(row)
}

func (s *Store) DeleteVideoByPath(filePath string) error {
	_, err := s.db.Exec(`DELETE FROM videos WHERE file_path = ?`, filePath)
	return err
}

func (s *Store) CanSkipVideo(path string, size int64, mtime time.Time) bool {
	video, err := s.VideoByPath(path)
	if err != nil {
		return false
	}
	return video.Size == size && video.MTime.Equal(mtime)
}

func (s *Store) ListRecentVideos(limit int) ([]domain.VideoAsset, error) {
	limit = normalizeShelfLimit(limit)
	rows, err := s.db.Query(videoSelectSQL()+` ORDER BY updated_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanVideos(rows)
}

func (s *Store) ListVideosPage(options domain.VideoListOptions) (domain.VideoListPage, error) {
	limit := options.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := options.Offset
	if offset < 0 {
		offset = 0
	}

	where, args := videoListWhere(options)
	var total int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM videos`+where, args...).Scan(&total); err != nil {
		return domain.VideoListPage{}, err
	}
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, limit, offset)
	rows, err := s.db.Query(videoSelectSQL()+where+videoListOrderBy(options.Sort)+` LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return domain.VideoListPage{}, err
	}
	defer rows.Close()
	items, err := scanVideos(rows)
	if err != nil {
		return domain.VideoListPage{}, err
	}
	return domain.VideoListPage{
		Items:   items,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
		HasMore: int64(offset+len(items)) < total,
	}, nil
}

func gameListWhere(options domain.GameListOptions) (string, []any) {
	clauses := make([]string, 0, 3)
	args := make([]any, 0, 8)
	if query := strings.TrimSpace(options.Query); query != "" {
		like := "%" + strings.ToLower(query) + "%"
		clauses = append(clauses, `(LOWER(title) LIKE ? OR LOWER(rom_set_name) LIKE ? OR LOWER(region) LIKE ? OR LOWER(platform) LIKE ? OR LOWER(format) LIKE ?)`)
		args = append(args, like, like, like, like, like)
	}
	if platform := strings.TrimSpace(options.Platform); platform != "" {
		clauses = append(clauses, `LOWER(platform) = LOWER(?)`)
		args = append(args, platform)
	}
	if format := strings.TrimSpace(options.Format); format != "" {
		clauses = append(clauses, `LOWER(format) = LOWER(?)`)
		args = append(args, format)
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func videoListWhere(options domain.VideoListOptions) (string, []any) {
	clauses := make([]string, 0, 2)
	args := make([]any, 0, 5)
	if query := strings.TrimSpace(options.Query); query != "" {
		like := "%" + strings.ToLower(query) + "%"
		clauses = append(clauses, `(LOWER(title) LIKE ? OR LOWER(rel_path) LIKE ? OR LOWER(format) LIKE ?)`)
		args = append(args, like, like, like)
	}
	if format := strings.TrimSpace(options.Format); format != "" {
		clauses = append(clauses, `LOWER(format) = LOWER(?)`)
		args = append(args, format)
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func videoListOrderBy(sort string) string {
	switch strings.ToLower(strings.TrimSpace(sort)) {
	case "title":
		return ` ORDER BY LOWER(title), id`
	default:
		return ` ORDER BY updated_at DESC, id DESC`
	}
}

func gameListOrderBy(sort string) string {
	switch strings.ToLower(strings.TrimSpace(sort)) {
	case "title":
		return ` ORDER BY LOWER(title), platform, id`
	case "platform":
		return ` ORDER BY LOWER(platform), LOWER(title), id`
	default:
		return ` ORDER BY updated_at DESC, id DESC`
	}
}

func GamePlatformCollectionID(platform string) int64 {
	return -1000 - int64(GamePlatformSortRank(platform))
}

func PlatformFromGamePlatformCollectionID(id int64) string {
	switch id {
	case GamePlatformCollectionID("nes"):
		return "nes"
	case GamePlatformCollectionID("snes"):
		return "snes"
	case GamePlatformCollectionID("gb"):
		return "gb"
	case GamePlatformCollectionID("gbc"):
		return "gbc"
	case GamePlatformCollectionID("gba"):
		return "gba"
	case GamePlatformCollectionID("md"):
		return "md"
	case GamePlatformCollectionID("neogeo"):
		return "neogeo"
	case GamePlatformCollectionID("32x"):
		return "32x"
	case GamePlatformCollectionID("model3"):
		return "model3"
	case GamePlatformCollectionID("naomi"):
		return "naomi"
	case GamePlatformCollectionID("saturn"):
		return "saturn"
	case GamePlatformCollectionID("arcade"):
		return "arcade"
	default:
		return ""
	}
}

func GamePlatformSortRank(platform string) int {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "nes":
		return 10
	case "snes":
		return 20
	case "gb":
		return 30
	case "gbc":
		return 40
	case "gba":
		return 50
	case "md", "genesis", "mega-drive", "megadrive":
		return 60
	case "32x":
		return 65
	case "saturn":
		return 70
	case "neogeo":
		return 80
	case "model3":
		return 85
	case "naomi":
		return 86
	case "arcade":
		return 90
	default:
		return 999
	}
}

func GamePlatformLabel(platform string) string {
	value := strings.ToLower(strings.TrimSpace(platform))
	switch value {
	case "nes", "snes", "gb", "gbc", "gba":
		return strings.ToUpper(value)
	case "md":
		return "Mega Drive"
	case "genesis", "mega-drive", "megadrive":
		return "Mega Drive"
	case "32x":
		return "32X"
	case "neogeo":
		return "Neo Geo"
	case "model3":
		return "Model 3"
	case "naomi":
		return "NAOMI"
	case "saturn":
		return "Saturn"
	case "arcade":
		return "Arcade"
	default:
		if value == "" {
			return "Unknown"
		}
		return strings.ToUpper(value[:1]) + value[1:]
	}
}

func platformFromGameCollectionTitle(title string) string {
	return strings.ToLower(strings.TrimPrefix(title, "Games / "))
}

func (s *Store) CanSkipGame(path string, size int64, mtime time.Time, platform string) bool {
	game, err := s.GameByPath(path)
	if err != nil {
		return false
	}
	return game.Size == size &&
		game.MTime.Equal(mtime) &&
		game.Platform == platform &&
		game.EmulatorHint == platform &&
		game.CRC32 != "" &&
		game.SHA1 != ""
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
	Book      domain.Book
	Analyzed  bool
	PageCount int
}

func (s *Store) FileIndexByPath(absPath string) (FileIndex, error) {
	row := s.db.QueryRow(`SELECT f.id, f.book_id, f.library_id, f.abs_path, f.rel_path, f.size, f.mtime, f.ext,
			b.id, b.series_id, s.title, b.title, b.creator, b.description, b.format, b.analyzed, b.page_count
		FROM files f JOIN books b ON b.id = f.book_id
		JOIN series s ON s.id = b.series_id
		WHERE f.abs_path = ?`, absPath)
	var item FileIndex
	var mtime string
	var analyzed int
	if err := row.Scan(
		&item.File.ID,
		&item.File.BookID,
		&item.File.LibraryID,
		&item.File.AbsPath,
		&item.File.RelPath,
		&item.File.Size,
		&mtime,
		&item.File.Ext,
		&item.Book.ID,
		&item.Book.SeriesID,
		&item.Book.CollectionTitle,
		&item.Book.Title,
		&item.Book.Creator,
		&item.Book.Description,
		&item.Book.Format,
		&analyzed,
		&item.PageCount,
	); err != nil {
		return item, err
	}
	item.File.MTime = parseTime(mtime)
	item.Analyzed = analyzed != 0
	item.Book.Analyzed = item.Analyzed
	item.Book.PageCount = item.PageCount
	item.Book.FilePath = item.File.AbsPath
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

	out := make([]domain.Page, 0)
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
	_, err := s.db.Exec(`UPDATE scan_jobs SET status = ?, current_path = ?, discovered_files = ?, indexed_files = ?, skipped_files = ?, error_count = ?, metadata_updated_files = ?, reclassified_files = ?, finished_at = ? WHERE id = ?`,
		job.Status, job.CurrentPath, job.DiscoveredFiles, job.IndexedFiles, job.SkippedFiles, job.ErrorCount, job.MetadataUpdatedFiles, job.ReclassifiedFiles, formatOptionalTime(job.FinishedAt), job.ID)
	return err
}

func (s *Store) RequestScanJobPause(id int64) (domain.ScanJob, error) {
	_, err := s.db.Exec(`UPDATE scan_jobs SET status = 'pause_requested' WHERE id = ? AND status = 'running'`, id)
	if err != nil {
		return domain.ScanJob{}, err
	}
	return s.ScanJobByID(id)
}

func (s *Store) RequestScanJobCancel(id int64) (domain.ScanJob, error) {
	_, err := s.db.Exec(`UPDATE scan_jobs SET status = 'cancel_requested' WHERE id = ? AND status IN ('running', 'pause_requested', 'paused')`, id)
	if err != nil {
		return domain.ScanJob{}, err
	}
	return s.ScanJobByID(id)
}

func (s *Store) CancelInterruptedScanJobs() (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`SELECT id FROM scan_jobs WHERE status IN ('running', 'pause_requested', 'cancel_requested')`)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, tx.Commit()
	}

	finishedAt := time.Now().UTC().Format(time.RFC3339Nano)
	for _, id := range ids {
		if _, err := tx.Exec(`UPDATE scan_jobs SET status = 'cancelled', finished_at = ? WHERE id = ?`, finishedAt, id); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(`INSERT INTO job_events(job_id, level, message) VALUES(?, 'warn', 'marked cancelled after service restart')`, id); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int64(len(ids)), nil
}

func (s *Store) ScanJobByID(id int64) (domain.ScanJob, error) {
	row := s.db.QueryRow(`SELECT id, library_id, status, current_path, discovered_files, indexed_files, skipped_files, error_count, metadata_updated_files, reclassified_files, started_at, finished_at FROM scan_jobs WHERE id = ?`, id)
	return scanJob(row)
}

func (s *Store) ListScanJobs() ([]domain.ScanJob, error) {
	rows, err := s.db.Query(`SELECT id, library_id, status, current_path, discovered_files, indexed_files, skipped_files, error_count, metadata_updated_files, reclassified_files, started_at, finished_at FROM scan_jobs ORDER BY id DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.ScanJob, 0)
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

	out := make([]domain.JobEvent, 0)
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

	out := make([]domain.FileError, 0)
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
	if err := row.Scan(&lib.ID, &lib.Name, &lib.RootPath, &lib.AssetType, &created, &updated); err != nil {
		return lib, err
	}
	lib.AssetType = normalizeLibraryAssetType(lib.AssetType)
	lib.CreatedAt = parseTime(created)
	lib.UpdatedAt = parseTime(updated)
	return lib, nil
}

func normalizeLibraryAssetType(value string) string {
	switch strings.TrimSpace(value) {
	case "book", "comic", "game", "video":
		return strings.TrimSpace(value)
	default:
		return "mixed"
	}
}

func scanSeries(row scanner) (domain.Series, error) {
	var series domain.Series
	if err := row.Scan(
		&series.ID,
		&series.LibraryID,
		&series.Title,
		&series.DirectoryPath,
		&series.CollectionType,
		&series.PrimaryType,
		&series.BookCount,
	); err != nil {
		return series, err
	}
	series.PrimaryType = normalizeCollectionPrimaryType(series.PrimaryType)
	return series, nil
}

func normalizeCollectionPrimaryType(value string) string {
	switch strings.TrimSpace(value) {
	case "book", "comic", "game", "video":
		return strings.TrimSpace(value)
	default:
		return "comic"
	}
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
		&book.Creator,
		&book.Description,
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
	return `SELECT b.id, b.series_id, s.title, b.title, b.creator, b.description, b.format, b.page_count, b.cover_status, b.analyzed,
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

func gameSelectSQL() string {
	return `SELECT id, library_id, title, platform, rom_set_name, region, format, file_path, rel_path, size, mtime, crc32, sha1, emulator_hint, compatibility, last_played_at, created_at, updated_at FROM games`
}

func scanGames(rows *sql.Rows) ([]domain.GameAsset, error) {
	out := make([]domain.GameAsset, 0)
	for rows.Next() {
		game, err := scanGame(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, game)
	}
	return out, rows.Err()
}

func scanGame(row scanner) (domain.GameAsset, error) {
	var game domain.GameAsset
	var mtime string
	var lastPlayedAt string
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&game.ID,
		&game.LibraryID,
		&game.Title,
		&game.Platform,
		&game.ROMSetName,
		&game.Region,
		&game.Format,
		&game.FilePath,
		&game.RelPath,
		&game.Size,
		&mtime,
		&game.CRC32,
		&game.SHA1,
		&game.EmulatorHint,
		&game.Compatibility,
		&lastPlayedAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return game, err
	}
	game.MTime = parseTime(mtime)
	game.LastPlayedAt = parseTime(lastPlayedAt)
	game.CreatedAt = parseTime(createdAt)
	game.UpdatedAt = parseTime(updatedAt)
	return game, nil
}

func videoSelectSQL() string {
	return `SELECT id, library_id, title, format, file_path, rel_path, size, mtime, duration_seconds, width, height, video_codec, audio_codec, thumbnail_status, last_played_at, created_at, updated_at FROM videos`
}

func scanVideos(rows *sql.Rows) ([]domain.VideoAsset, error) {
	out := make([]domain.VideoAsset, 0)
	for rows.Next() {
		video, err := scanVideo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, video)
	}
	return out, rows.Err()
}

func scanVideo(row scanner) (domain.VideoAsset, error) {
	var video domain.VideoAsset
	var mtime string
	var lastPlayedAt string
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&video.ID,
		&video.LibraryID,
		&video.Title,
		&video.Format,
		&video.FilePath,
		&video.RelPath,
		&video.Size,
		&mtime,
		&video.DurationSeconds,
		&video.Width,
		&video.Height,
		&video.VideoCodec,
		&video.AudioCodec,
		&video.ThumbnailStatus,
		&lastPlayedAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return video, err
	}
	video.MTime = parseTime(mtime)
	video.LastPlayedAt = parseTime(lastPlayedAt)
	video.CreatedAt = parseTime(createdAt)
	video.UpdatedAt = parseTime(updatedAt)
	video.DirectPlayable, video.PlaybackMode, video.PlaybackReason = videoPlaybackCompatibility(video)
	return video, nil
}

func videoPlaybackCompatibility(video domain.VideoAsset) (bool, string, string) {
	format := strings.ToLower(strings.TrimSpace(video.Format))
	videoCodec := strings.ToLower(strings.TrimSpace(video.VideoCodec))
	audioCodec := strings.ToLower(strings.TrimSpace(video.AudioCodec))
	switch format {
	case "mp4", "m4v":
		if videoCodec == "" && looksLikeHEVCVideo(video) {
			return false, "hls", "filename indicates HEVC/H.265 video"
		}
		if videoCodec == "" {
			return true, "direct", ""
		}
		if isH264Codec(videoCodec) && (audioCodec == "" || audioCodec == "aac" || audioCodec == "mp3") {
			return true, "direct", ""
		}
		return false, "hls", "mp4 codecs may need browser transcode"
	case "webm":
		if videoCodec == "" {
			return true, "direct", ""
		}
		if (videoCodec == "vp8" || videoCodec == "vp9" || videoCodec == "av1") && (audioCodec == "" || audioCodec == "opus" || audioCodec == "vorbis") {
			return true, "direct", ""
		}
		return false, "hls", "webm codecs may need browser transcode"
	default:
		return false, "hls", "container or codecs need browser transcode"
	}
}

func isH264Codec(codec string) bool {
	return codec == "h264" || codec == "avc1" || codec == "avc"
}

func looksLikeHEVCVideo(video domain.VideoAsset) bool {
	haystack := strings.ToLower(strings.Join([]string{video.Title, video.RelPath, video.FilePath}, " "))
	hevcMarkers := []string{"h265", "h.265", "hevc", "x265", "10bit", "10-bit", "hdr", "dolby vision", "dv"}
	for _, marker := range hevcMarkers {
		if strings.Contains(haystack, marker) {
			return true
		}
	}
	return false
}

func scanJob(row scanner) (domain.ScanJob, error) {
	var job domain.ScanJob
	var started string
	var finished string
	if err := row.Scan(&job.ID, &job.LibraryID, &job.Status, &job.CurrentPath, &job.DiscoveredFiles, &job.IndexedFiles, &job.SkippedFiles, &job.ErrorCount, &job.MetadataUpdatedFiles, &job.ReclassifiedFiles, &started, &finished); err != nil {
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
