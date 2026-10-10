package db

import "database/sql"

// Additive and transactional: videos and its IDs/URLs are not rebuilt or backfilled.
func migrateVideoMetadata(c *sql.DB) error {
	tx, err := c.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []string{
		`CREATE TABLE IF NOT EXISTS video_match_state (
 video_id INTEGER PRIMARY KEY REFERENCES videos(id) ON DELETE CASCADE,
 generation INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL DEFAULT 'unmatched')`,
		`CREATE TABLE IF NOT EXISTS video_works (
 id INTEGER PRIMARY KEY, library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
 kind TEXT NOT NULL CHECK(kind IN ('movie','series')), title TEXT NOT NULL,
 year INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE IF NOT EXISTS video_seasons (
 id INTEGER PRIMARY KEY, work_id INTEGER NOT NULL REFERENCES video_works(id) ON DELETE CASCADE,
 number INTEGER NOT NULL CHECK(number>=0), UNIQUE(work_id,number))`,
		`CREATE TABLE IF NOT EXISTS video_episodes (
 id INTEGER PRIMARY KEY, season_id INTEGER NOT NULL REFERENCES video_seasons(id) ON DELETE CASCADE,
 number INTEGER NOT NULL CHECK(number>=0), title TEXT NOT NULL DEFAULT '', UNIQUE(season_id,number))`,
		`CREATE TABLE IF NOT EXISTS video_versions (
 id INTEGER PRIMARY KEY, work_id INTEGER NOT NULL REFERENCES video_works(id) ON DELETE CASCADE,
 label TEXT NOT NULL DEFAULT '', UNIQUE(work_id,label))`,
		`CREATE TABLE IF NOT EXISTS video_file_links (
 video_id INTEGER NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
 version_id INTEGER NOT NULL REFERENCES video_versions(id) ON DELETE CASCADE,
 episode_id INTEGER REFERENCES video_episodes(id) ON DELETE CASCADE,
 position INTEGER NOT NULL DEFAULT 0 CHECK(position>=0),
 PRIMARY KEY(video_id,version_id))`,
		`CREATE INDEX IF NOT EXISTS idx_video_works_library_title ON video_works(library_id,title,id)`,
		`CREATE INDEX IF NOT EXISTS idx_video_links_version ON video_file_links(version_id,position,video_id)`,
		`CREATE TABLE IF NOT EXISTS video_work_details (
 work_id INTEGER PRIMARY KEY REFERENCES video_works(id) ON DELETE CASCADE,
 identity_key TEXT NOT NULL UNIQUE, overview TEXT NOT NULL DEFAULT '',
 provider TEXT NOT NULL DEFAULT '', provider_id TEXT NOT NULL DEFAULT '', raw_json TEXT NOT NULL DEFAULT '{}')`,
		`CREATE TABLE IF NOT EXISTS video_metadata_jobs (
 id INTEGER PRIMARY KEY, video_id INTEGER NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
 generation INTEGER NOT NULL, query TEXT NOT NULL, kind TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'queued', attempts INTEGER NOT NULL DEFAULT 0,
 next_at INTEGER NOT NULL DEFAULT 0, error_code TEXT NOT NULL DEFAULT '',
 result_json TEXT NOT NULL DEFAULT '[]', created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE INDEX IF NOT EXISTS idx_video_metadata_jobs_queue ON video_metadata_jobs(status,next_at,id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_video_metadata_jobs_active ON video_metadata_jobs(video_id)
 WHERE status IN ('queued','running')`,
		`CREATE TABLE IF NOT EXISTS video_artwork (
 video_id INTEGER NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
 role TEXT NOT NULL CHECK(role IN ('poster','backdrop')),
 cache_path TEXT NOT NULL, revision TEXT NOT NULL, source TEXT NOT NULL,
 PRIMARY KEY(video_id,role))`,
		`CREATE TABLE IF NOT EXISTS video_metadata_inputs (
 video_id INTEGER PRIMARY KEY REFERENCES videos(id) ON DELETE CASCADE,
 revision TEXT NOT NULL, local_json TEXT NOT NULL,
 updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE IF NOT EXISTS video_metadata_overrides (
 video_id INTEGER NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
 field TEXT NOT NULL, value_json TEXT NOT NULL,
 updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, PRIMARY KEY(video_id,field))`,
		`CREATE TRIGGER IF NOT EXISTS video_input_insert AFTER INSERT ON video_metadata_inputs BEGIN
 INSERT OR IGNORE INTO video_match_state(video_id) VALUES(NEW.video_id);
 UPDATE video_match_state SET generation=generation+1 WHERE video_id=NEW.video_id; END`,
		`CREATE TRIGGER IF NOT EXISTS video_input_update AFTER UPDATE ON video_metadata_inputs BEGIN
 UPDATE video_match_state SET generation=generation+1 WHERE video_id=NEW.video_id; END`,
		`CREATE TRIGGER IF NOT EXISTS video_override_insert AFTER INSERT ON video_metadata_overrides BEGIN
 INSERT OR IGNORE INTO video_match_state(video_id) VALUES(NEW.video_id);
 UPDATE video_match_state SET generation=generation+1 WHERE video_id=NEW.video_id; END`,
		`CREATE TRIGGER IF NOT EXISTS video_override_update AFTER UPDATE ON video_metadata_overrides BEGIN
 UPDATE video_match_state SET generation=generation+1 WHERE video_id=NEW.video_id; END`,
		`CREATE TRIGGER IF NOT EXISTS video_override_delete AFTER DELETE ON video_metadata_overrides BEGIN
 UPDATE video_match_state SET generation=generation+1 WHERE video_id=OLD.video_id; END`,
	}
	for _, stmt := range statements {
		if _, err = tx.Exec(stmt); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	for _, table := range []string{"video_artwork", "video_work_details"} {
		if err = addColumnIfMissing(c, table, "expires_at", "INTEGER NOT NULL DEFAULT 0"); err != nil {
			return err
		}
	}
	return nil
}
