package store

import "time"

func (s *Store) VideoArtwork(id int64, role string) (path, revision, source string, err error) {
	err = s.db.QueryRow(`SELECT cache_path,revision,source FROM video_artwork WHERE video_id=? AND role=?`, id, role).Scan(&path, &revision, &source)
	return
}
func (s *Store) SaveVideoArtwork(id int64, role, path, revision, source string, generation int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	g, _, err := videoGeneration(tx, id)
	if err != nil {
		return err
	}
	if g != generation {
		return ErrVideoRevision
	}
	expires := int64(0)
	if source == "tmdb" {
		expires = time.Now().Add(150 * 24 * time.Hour).Unix()
	}
	if _, err = tx.Exec(`INSERT INTO video_artwork(video_id,role,cache_path,revision,source,expires_at) VALUES(?,?,?,?,?,?) ON CONFLICT(video_id,role) DO UPDATE SET cache_path=excluded.cache_path,revision=excluded.revision,source=excluded.source,expires_at=excluded.expires_at`, id, role, path, revision, source, expires); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE video_match_state SET generation=generation+1 WHERE video_id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) DeleteVideoArtwork(id int64, role string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, _, err = videoGeneration(tx, id); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM video_artwork WHERE video_id=? AND role=?`, id, role); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE video_match_state SET generation=generation+1 WHERE video_id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}
