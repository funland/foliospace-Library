package db

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
)

// OpenExisting is for maintenance tools. Startup migration belongs to the server,
// never to an audit or dry run against an existing live catalog.
func OpenExisting(configDir string, readOnly bool) (*sql.DB, error) {
	path, err := filepath.Abs(filepath.Join(configDir, "foliospace-reader.db"))
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("an initialized database is required in %s", configDir)
	}
	mode := "rw"
	if readOnly {
		mode = "ro"
	}
	uri := url.URL{Scheme: "file", Path: path, RawQuery: "mode=" + mode}
	conn, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)
	for _, query := range []string{
		"PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000",
		"SELECT id,platform,catalog_role FROM games LIMIT 0",
		"SELECT core_build_id FROM game_launch_profiles LIMIT 0",
		"SELECT source_game_id FROM game_launch_profile_files LIMIT 0",
	} {
		if _, err = conn.Exec(query); err != nil {
			conn.Close()
			return nil, fmt.Errorf("database schema is not ready; upgrade the server first: %w", err)
		}
	}
	if readOnly {
		if _, err = conn.Exec("PRAGMA query_only=ON"); err != nil {
			conn.Close()
			return nil, err
		}
	}
	return conn, nil
}
