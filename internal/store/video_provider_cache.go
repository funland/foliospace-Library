package store

import "time"

// Only provider-derived data expires; original files, NFO and manual fields survive.
func (s *Store) PurgeVideoProviderCache(all bool) ([]string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	rows, err := tx.Query(`SELECT cache_path FROM video_artwork WHERE source='tmdb' AND (? OR expires_at<=?)`, all, now)
	if err != nil {
		return nil, err
	}
	paths := []string{}
	for rows.Next() {
		var p string
		if err = rows.Scan(&p); err != nil {
			rows.Close()
			return nil, err
		}
		paths = append(paths, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if all {
		if _, err = tx.Exec(`UPDATE video_match_state SET generation=generation+1 WHERE video_id IN (SELECT video_id FROM video_metadata_jobs WHERE status IN ('queued','running'))`); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(`UPDATE video_metadata_jobs SET status='cancelled',result_json='[]',query='',error_code=''`); err != nil {
			return nil, err
		}
	} else {
		if _, err = tx.Exec(`UPDATE video_metadata_jobs SET status='expired',result_json='[]',query='',error_code='' WHERE created_at < datetime('now','-150 days')`); err != nil {
			return nil, err
		}
	}
	if _, err = tx.Exec(`DELETE FROM video_artwork WHERE source='tmdb' AND (? OR expires_at<=?)`, all, now); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(`UPDATE video_works SET title=COALESCE((SELECT v.title FROM video_versions vv JOIN video_file_links l ON l.version_id=vv.id JOIN videos v ON v.id=l.video_id WHERE vv.work_id=video_works.id ORDER BY v.id LIMIT 1),'Unmatched'), year=0 WHERE id IN (SELECT work_id FROM video_work_details WHERE provider='tmdb' AND (? OR expires_at<=?))`, all, now); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(`UPDATE video_work_details SET identity_key='expired:'||work_id,overview='',provider='',provider_id='',raw_json='{}',expires_at=0 WHERE provider='tmdb' AND (? OR expires_at<=?)`, all, now); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return paths, nil
}

func (s *Store) VideoArtworkReferenced(path string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM video_artwork WHERE cache_path=?`, path).Scan(&n)
	return n > 0, err
}
