package store

import "foliospace-reader/internal/domain"

// BookSharedMetadata excludes profile tags and notes when comparing scan input.
func (s *Store) BookSharedMetadata(bookID int64) (domain.Book, error) {
	book := domain.Book{ID: bookID}
	var tags string
	err := s.db.QueryRow(`SELECT series_id, title, creator, description, tags, format FROM books WHERE id = ?`, bookID).Scan(&book.SeriesID, &book.Title, &book.Creator, &book.Description, &tags, &book.Format)
	book.Tags = decodeTags(tags)
	return book, err
}
