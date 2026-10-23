package store

import (
	"database/sql"
	"encoding/json"
	"foliospace-reader/internal/domain"
	"time"
)

func (s *Store) VideoWorkDetails(id int64) (domain.VideoWorkDetails, error) {
	var raw string
	var expires int64
	err := s.db.QueryRow(`SELECT raw_json,expires_at FROM video_work_details WHERE work_id=?`, id).Scan(&raw, &expires)
	if err != nil {
		return domain.VideoWorkDetails{}, err
	}
	var envelope struct {
		Details domain.VideoWorkDetails `json:"details"`
	}
	if expires <= time.Now().Unix() {
		return domain.VideoWorkDetails{}, nil
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return domain.VideoWorkDetails{}, nil
	}
	return envelope.Details, nil
}

// Provider replies may arrive after a purge/rematch; compare the captured identity.
// Only the cache member changes, preserving original match input and all user state.
func (s *Store) SaveVideoWorkDetails(work domain.VideoWork, details domain.VideoWorkDetails) error {
	b, err := json.Marshal(details)
	if err != nil {
		return err
	}
	result, err := s.db.Exec(`UPDATE video_work_details SET raw_json=json_set(raw_json,'$.details',json(?)) WHERE work_id=? AND provider='tmdb' AND provider_id=? AND expires_at>? AND EXISTS(SELECT 1 FROM video_works w WHERE w.id=work_id AND w.kind=?)`, string(b), work.ID, work.ProviderID, time.Now().Unix(), work.Kind)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return sql.ErrNoRows
	}
	return err
}
