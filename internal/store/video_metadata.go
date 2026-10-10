package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"foliospace-reader/internal/domain"
	"strings"
)

func (s *Store) SaveVideoLocalMetadata(id int64, revision string, m domain.VideoLocalMetadata) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO video_metadata_inputs(video_id,revision,local_json) VALUES(?,?,?)
 ON CONFLICT(video_id) DO UPDATE SET revision=excluded.revision,local_json=excluded.local_json,updated_at=CURRENT_TIMESTAMP
 WHERE video_metadata_inputs.revision<>excluded.revision`, id, revision, string(b))
	if err != nil {
		return err
	}
	return s.SyncLocalVideoWork(id, m)
}

// Empty strings are intentional overrides, not requests to unlock a field.
func (s *Store) SetVideoMetadataOverride(id int64, field, value string) error {
	if len(value) > 65536 {
		return fmt.Errorf("metadata value too large")
	}
	var probe domain.VideoLocalMetadata
	if err := applyVideoOverride(&probe, field, value); err != nil {
		return err
	}
	_, err := s.db.Exec(`INSERT INTO video_metadata_overrides(video_id,field,value_json) VALUES(?,?,?)
 ON CONFLICT(video_id,field) DO UPDATE SET value_json=excluded.value_json,updated_at=CURRENT_TIMESTAMP`, id, field, value)
	return err
}

func (s *Store) DeleteVideoMetadataOverride(id int64, field string) error {
	_, err := s.db.Exec(`DELETE FROM video_metadata_overrides WHERE video_id=? AND field=?`, id, field)
	return err
}

func (s *Store) VideoMetadata(id int64) (domain.VideoMetadata, error) {
	m := domain.VideoMetadata{LockedFields: []string{}}
	// Keep this query closed before the next one: SQLite uses one connection.
	v, err := s.VideoByID(id)
	if err != nil {
		return m, err
	}
	m.Title = v.Title
	m.Kind = "unknown"
	m.Source = "filename"
	var raw string
	err = s.db.QueryRow(`SELECT revision,local_json FROM video_metadata_inputs WHERE video_id=?`, id).Scan(&m.Revision, &raw)
	if err != nil && err != sql.ErrNoRows {
		return m, err
	}
	if err == nil {
		if err = json.Unmarshal([]byte(raw), &m.VideoLocalMetadata); err != nil {
			return m, err
		}
	}
	err = s.db.QueryRow(`SELECT generation,status FROM video_match_state WHERE video_id=?`, id).Scan(&m.Generation, &m.Status)
	if err != nil && err != sql.ErrNoRows {
		return m, err
	}
	if m.Status == "" {
		m.Status = "unmatched"
	}
	err = s.db.QueryRow(`SELECT v.work_id FROM video_file_links f JOIN video_versions v ON v.id=f.version_id WHERE f.video_id=? LIMIT 1`, id).Scan(&m.WorkID)
	if err != nil && err != sql.ErrNoRows {
		return m, err
	}
	rows, err := s.db.Query(`SELECT field,value_json FROM video_metadata_overrides WHERE video_id=? ORDER BY field`, id)
	if err != nil {
		return m, err
	}
	defer rows.Close()
	for rows.Next() {
		var field, value string
		if err = rows.Scan(&field, &value); err != nil {
			return m, err
		}
		if err = applyVideoOverride(&m.VideoLocalMetadata, field, value); err != nil {
			return m, err
		}
		m.LockedFields = append(m.LockedFields, field)
	}
	return m, rows.Err()
}

func applyVideoOverride(m *domain.VideoLocalMetadata, field, value string) error {
	if strings.TrimSpace(value) == "null" {
		return fmt.Errorf("use explicit reset to remove override")
	}
	switch field {
	case "title":
		return json.Unmarshal([]byte(value), &m.Title)
	case "overview":
		return json.Unmarshal([]byte(value), &m.Overview)
	case "year":
		var year int
		if err := json.Unmarshal([]byte(value), &year); err != nil {
			return err
		}
		if year < 0 || year > 9999 {
			return fmt.Errorf("invalid year")
		}
		m.Year = year
		return nil
	default:
		return fmt.Errorf("unsupported metadata field %q", field)
	}
}
