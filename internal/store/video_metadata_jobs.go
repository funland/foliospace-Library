package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"foliospace-reader/internal/domain"
	"time"
)

func (s *Store) QueueVideoMetadata(id int64, q, kind string) (domain.VideoMetadataJob, error) {
	if len(q) > 500 || q == "" || (kind != "movie" && kind != "series" && kind != "poster" && kind != "backdrop") {
		return domain.VideoMetadataJob{}, fmt.Errorf("invalid query/kind")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return domain.VideoMetadataJob{}, err
	}
	defer tx.Rollback()
	g, _, err := videoGeneration(tx, id)
	if err != nil {
		return domain.VideoMetadataJob{}, err
	}
	var active int64
	err = tx.QueryRow(`SELECT id FROM video_metadata_jobs WHERE video_id=? AND status IN ('queued','running')`, id).Scan(&active)
	if err != nil && err != sql.ErrNoRows {
		return domain.VideoMetadataJob{}, err
	}
	if active == 0 {
		var pending int
		if err = tx.QueryRow(`SELECT COUNT(*) FROM video_metadata_jobs WHERE status IN ('queued','running')`).Scan(&pending); err != nil {
			return domain.VideoMetadataJob{}, err
		}
		if pending >= 1000 {
			return domain.VideoMetadataJob{}, fmt.Errorf("metadata-queue-full")
		}
		if _, err = tx.Exec(`DELETE FROM video_metadata_jobs WHERE video_id=? AND status NOT IN ('queued','running') AND id NOT IN (SELECT id FROM video_metadata_jobs WHERE video_id=? ORDER BY id DESC LIMIT 10)`, id, id); err != nil {
			return domain.VideoMetadataJob{}, err
		}
		r, e := tx.Exec(`INSERT INTO video_metadata_jobs(video_id,generation,query,kind) VALUES(?,?,?,?)`, id, g, q, kind)
		if e != nil {
			return domain.VideoMetadataJob{}, e
		}
		active, _ = r.LastInsertId()
	}
	if err = tx.Commit(); err != nil {
		return domain.VideoMetadataJob{}, err
	}
	return s.VideoMetadataJob(active)
}

const videoJobSelect = `SELECT id,video_id,generation,query,kind,status,attempts,next_at,error_code,result_json FROM video_metadata_jobs`

func scanVideoJob(row scanner) (domain.VideoMetadataJob, error) {
	var j domain.VideoMetadataJob
	var raw string
	err := row.Scan(&j.ID, &j.VideoID, &j.Generation, &j.Query, &j.Kind, &j.Status, &j.Attempts, &j.NextAt, &j.ErrorCode, &raw)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &j.Candidates)
	}
	return j, err
}
func (s *Store) VideoMetadataJob(id int64) (domain.VideoMetadataJob, error) {
	return scanVideoJob(s.db.QueryRow(videoJobSelect+` WHERE id=?`, id))
}
func (s *Store) LatestVideoMetadataJob(id int64) (domain.VideoMetadataJob, error) {
	return scanVideoJob(s.db.QueryRow(videoJobSelect+` WHERE video_id=? ORDER BY id DESC LIMIT 1`, id))
}
func (s *Store) ClaimVideoMetadataJob() (domain.VideoMetadataJob, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return domain.VideoMetadataJob{}, false, err
	}
	defer tx.Rollback()
	j, err := scanVideoJob(tx.QueryRow(videoJobSelect+` WHERE status='queued' AND next_at<=? ORDER BY id LIMIT 1`, time.Now().Unix()))
	if err == sql.ErrNoRows {
		return j, false, nil
	}
	if err != nil {
		return j, false, err
	}
	if _, err = tx.Exec(`UPDATE video_metadata_jobs SET status='running',attempts=attempts+1 WHERE id=?`, j.ID); err != nil {
		return j, false, err
	}
	j.Status = "running"
	j.Attempts++
	return j, true, tx.Commit()
}
func (s *Store) FinishVideoMetadataJob(j domain.VideoMetadataJob, candidates []domain.VideoCandidate, code string, retryAt int64) error {
	if candidates == nil {
		candidates = []domain.VideoCandidate{}
	}
	b, err := json.Marshal(candidates)
	if err != nil {
		return err
	}
	status := "ready"
	if code != "" {
		status = "failed"
	}
	if retryAt > 0 && j.Attempts < 3 {
		status = "queued"
	}
	_, err = s.db.Exec(`UPDATE video_metadata_jobs SET status=CASE WHEN ?<>(SELECT generation FROM video_match_state WHERE video_id=video_metadata_jobs.video_id) THEN 'stale' ELSE ? END,
 result_json=?,error_code=?,next_at=? WHERE id=? AND status='running'`, j.Generation, status, string(b), code, retryAt, j.ID)
	return err
}
func (s *Store) CancelVideoMetadataJob(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Invalidate an in-flight artwork write as well as its eventual result.
	if _, err = tx.Exec(`UPDATE video_match_state SET generation=generation+1 WHERE video_id IN (SELECT video_id FROM video_metadata_jobs WHERE id=? AND status IN ('queued','running'))`, id); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE video_metadata_jobs SET status='cancelled' WHERE id=? AND status IN ('queued','running')`, id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) RecoverVideoMetadataJobs() error {
	_, err := s.db.Exec(`UPDATE video_metadata_jobs SET status=CASE WHEN attempts>=3 THEN 'failed' ELSE 'queued' END,error_code='worker-interrupted' WHERE status='running'`)
	return err
}
