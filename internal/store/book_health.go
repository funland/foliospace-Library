package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

func bookHealthKey(id int64) string { return fmt.Sprintf("book-file-health:%d", id) }

// Only diagnostic settings change. Identity, pages, progress and manual metadata
// survive both a failed validation and recovery. Historical errors are retained.
func (s *Store) SetBookFileHealth(bookID int64, code, message string) error {
	if code == "" {
		if _, err := s.Setting(bookHealthKey(bookID)); errors.Is(err, sql.ErrNoRows) {
			var count int
			if err := s.db.QueryRow(`SELECT COUNT(*) FROM file_errors e WHERE (e.book_id=? OR e.path IN (SELECT abs_path FROM files WHERE book_id=?)) AND e.code IN ('empty_file','zero_filled_file','invalid_book_container','invalid_epub','archive_open_failed','archive_empty')`, bookID, bookID).Scan(&count); err != nil {
				return err
			}
			if count == 0 {
				return nil
			}
		} else if err != nil {
			return err
		}
	}
	raw, err := json.Marshal(struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{code, message})
	if err != nil {
		return err
	}
	if previous, e := s.Setting(bookHealthKey(bookID)); e == nil && previous == string(raw) {
		return nil
	}
	return s.UpsertSetting(bookHealthKey(bookID), string(raw))
}
