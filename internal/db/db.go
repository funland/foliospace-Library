package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func Open(configDir string) (*sql.DB, error) {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}

	conn, err := sql.Open("sqlite", filepath.Join(configDir, "foliospace-reader.db"))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)
	if err := Migrate(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func Migrate(conn *sql.DB) error {
	stmts := []string{
		`PRAGMA foreign_keys = ON`,
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA journal_mode = WAL`,
		`CREATE TABLE IF NOT EXISTS libraries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			root_path TEXT NOT NULL UNIQUE,
			asset_type TEXT NOT NULL DEFAULT 'mixed',
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS series (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
			title TEXT NOT NULL,
			directory_path TEXT NOT NULL DEFAULT '',
			collection_type TEXT NOT NULL DEFAULT 'directory',
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(library_id, title)
		)`,
		`CREATE TABLE IF NOT EXISTS books (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			series_id INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
			title TEXT NOT NULL,
			creator TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			format TEXT NOT NULL,
			page_count INTEGER NOT NULL DEFAULT 0,
			cover_status TEXT NOT NULL DEFAULT 'pending',
			analyzed INTEGER NOT NULL DEFAULT 0,
			private_status TEXT NOT NULL DEFAULT '',
			favorite INTEGER NOT NULL DEFAULT 0,
			rating INTEGER NOT NULL DEFAULT 0,
			tags TEXT NOT NULL DEFAULT '',
			summary TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(series_id, title, format)
		)`,
		`CREATE TABLE IF NOT EXISTS files (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			book_id INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
			library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
			abs_path TEXT NOT NULL UNIQUE,
			rel_path TEXT NOT NULL,
			size INTEGER NOT NULL,
			mtime TEXT NOT NULL,
			ext TEXT NOT NULL,
			hash TEXT NOT NULL DEFAULT '',
			hash_status TEXT NOT NULL DEFAULT 'pending',
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS games (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
			title TEXT NOT NULL,
			platform TEXT NOT NULL DEFAULT '',
			rom_set_name TEXT NOT NULL DEFAULT '',
			region TEXT NOT NULL DEFAULT '',
			format TEXT NOT NULL,
			file_path TEXT NOT NULL UNIQUE,
			rel_path TEXT NOT NULL,
			size INTEGER NOT NULL,
			mtime TEXT NOT NULL,
			crc32 TEXT NOT NULL DEFAULT '',
			sha1 TEXT NOT NULL DEFAULT '',
			emulator_hint TEXT NOT NULL DEFAULT '',
			compatibility TEXT NOT NULL DEFAULT 'unknown',
			last_played_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS pages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			book_id INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
			page_index INTEGER NOT NULL,
			entry_name TEXT NOT NULL,
			UNIQUE(book_id, page_index)
		)`,
		`CREATE TABLE IF NOT EXISTS scan_jobs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
			status TEXT NOT NULL,
			current_path TEXT NOT NULL DEFAULT '',
			discovered_files INTEGER NOT NULL DEFAULT 0,
			indexed_files INTEGER NOT NULL DEFAULT 0,
			skipped_files INTEGER NOT NULL DEFAULT 0,
			error_count INTEGER NOT NULL DEFAULT 0,
			metadata_updated_files INTEGER NOT NULL DEFAULT 0,
			reclassified_files INTEGER NOT NULL DEFAULT 0,
			started_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			finished_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS job_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			job_id INTEGER NOT NULL REFERENCES scan_jobs(id) ON DELETE CASCADE,
			level TEXT NOT NULL,
			message TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS read_progress (
			book_id INTEGER PRIMARY KEY REFERENCES books(id) ON DELETE CASCADE,
			page_index INTEGER NOT NULL,
			locator TEXT NOT NULL DEFAULT '',
			progress_fraction REAL NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS file_errors (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			library_id INTEGER NOT NULL,
			book_id INTEGER NOT NULL DEFAULT 0,
			file_id INTEGER NOT NULL DEFAULT 0,
			job_id INTEGER NOT NULL DEFAULT 0,
			path TEXT NOT NULL,
			code TEXT NOT NULL,
			message TEXT NOT NULL,
			first_seen TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			last_seen TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(path, code)
		)`,
		`CREATE TABLE IF NOT EXISTS client_preferences (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			locale TEXT NOT NULL DEFAULT 'zh',
			reader_page_mode TEXT NOT NULL DEFAULT 'single',
			epub_page_mode TEXT NOT NULL DEFAULT 'single',
			epub_theme TEXT NOT NULL DEFAULT 'light',
			epub_font_size INTEGER NOT NULL DEFAULT 18,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
	}

	for _, stmt := range stmts {
		if _, err := conn.Exec(stmt); err != nil {
			return fmt.Errorf("migrate sqlite: %w", err)
		}
	}
	if err := addColumnIfMissing(conn, "scan_jobs", "current_path", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(conn, "scan_jobs", "metadata_updated_files", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := addColumnIfMissing(conn, "scan_jobs", "reclassified_files", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := addColumnIfMissing(conn, "series", "directory_path", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(conn, "series", "collection_type", "TEXT NOT NULL DEFAULT 'directory'"); err != nil {
		return err
	}
	if err := addColumnIfMissing(conn, "read_progress", "locator", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(conn, "read_progress", "progress_fraction", "REAL NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := addColumnIfMissing(conn, "books", "private_status", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(conn, "books", "favorite", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := addColumnIfMissing(conn, "books", "rating", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := addColumnIfMissing(conn, "books", "tags", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(conn, "books", "summary", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(conn, "books", "creator", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(conn, "books", "description", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(conn, "libraries", "asset_type", "TEXT NOT NULL DEFAULT 'mixed'"); err != nil {
		return err
	}
	return nil
}

func addColumnIfMissing(conn *sql.DB, table string, column string, definition string) error {
	rows, err := conn.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = conn.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, definition))
	return err
}
