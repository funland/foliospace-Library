package store

import (
	"fmt"
	"strings"
)

// Resolve a page's cover versions in one bounded query, without touching media.
func (s *Store) BookArtworkRevisions(ids []int64) map[int64]string {
	result := map[int64]string{}
	for start := 0; start < len(ids); start += 200 {
		end := start + 200
		if end > len(ids) {
			end = len(ids)
		}
		args := make([]any, 0, end-start)
		for _, id := range ids[start:end] {
			args = append(args, id)
		}
		rows, err := s.db.Query(`SELECT id,updated_at FROM books WHERE id IN (`+strings.TrimRight(strings.Repeat("?,", len(args)), ",")+`)`, args...)
		if err != nil {
			return result
		}
		for rows.Next() {
			var id int64
			var updated string
			if rows.Scan(&id, &updated) == nil {
				result[id] = fmt.Sprintf("&r=%d", parseTime(updated).UnixNano())
			}
		}
		rows.Close()
	}
	return result
}

func (s *Store) BookLibraryRoot(bookID int64) (string, error) {
	var root string
	err := s.db.QueryRow(`SELECT l.root_path FROM books b JOIN series s ON s.id=b.series_id JOIN libraries l ON l.id=s.library_id WHERE b.id=?`, bookID).Scan(&root)
	return root, err
}
