package store

import (
	"sort"
	"strings"

	"foliospace-reader/internal/domain"
)

// ApplyBookCollectionPresentation decorates a bounded response once per distinct
// series. Keeping it out of scanner metadata reads avoids affecting source
// comparisons and avoids a presentation query for every indexed file.
func (s *Store) ApplyBookCollectionPresentation(books []domain.Book) ([]domain.Book, error) {
	ids := map[int64]bool{}
	for _, book := range books {
		ids[book.SeriesID] = true
	}
	if len(ids) == 0 {
		return books, nil
	}
	ordered := make([]int64, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	titles := map[int64]string{}
	// Older list endpoints may return an entire collection; cap SQL parameters.
	for start := 0; start < len(ordered); start += 200 {
		end := start + 200
		if end > len(ordered) {
			end = len(ordered)
		}
		args := make([]any, 0, end-start)
		for _, id := range ordered[start:end] {
			args = append(args, id)
		}
		placeholders := strings.TrimRight(strings.Repeat("?,", len(args)), ",")
		rows, err := s.db.Query(`SELECT c.id, c.library_id, c.title, c.directory_path, c.collection_type, c.primary_type, c.book_count, c.cover_book_id, c.created_at
			FROM (`+collectionListBaseSQLSelected(` WHERE s.id IN (`+placeholders+`)`)+`) c`, args...)
		if err != nil {
			return nil, err
		}
		series := []domain.Series{}
		for rows.Next() {
			item, err := scanSeries(rows)
			if err != nil {
				rows.Close()
				return nil, err
			}
			series = append(series, item)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		series, err = s.applySeriesPresentation(series)
		if err != nil {
			return nil, err
		}
		for _, item := range series {
			titles[item.ID] = item.DisplayTitle
		}
	}
	for i := range books {
		books[i].CollectionDisplayTitle = titles[books[i].SeriesID]
	}
	return books, nil
}
