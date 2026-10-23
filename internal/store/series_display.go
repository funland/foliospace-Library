package store

import (
	"regexp"
	"strings"

	"foliospace-reader/internal/domain"
)

// Apply the same per-library labels to list/detail/favorite projections as the
// comic browser, while keeping title, paths, IDs and private state unchanged.
func (s *Store) applySeriesPresentation(items []domain.Series) ([]domain.Series, error) {
	type ruleSet struct {
		rules    []ComicTitleRule
		compiled []*regexp.Regexp
	}
	rulesByLibrary := map[int64]ruleSet{}
	bookSeries := []any{}
	for i := range items {
		item := &items[i]
		item.DisplayTitle = item.Title
		if item.PrimaryType == "book" {
			bookSeries = append(bookSeries, item.ID)
		}
		if item.PrimaryType != "comic" || item.LibraryID <= 0 {
			continue
		}
		rel, err := comicRelativePath(item.DirectoryPath)
		if err != nil {
			continue
		}
		set, found := rulesByLibrary[item.LibraryID]
		if !found {
			set.rules, err = s.comicTitleRules(item.LibraryID)
			if err != nil {
				return nil, err
			}
			set.compiled, err = compileComicTitleRules(set.rules)
			if err != nil {
				return nil, err
			}
			rulesByLibrary[item.LibraryID] = set
		}
		item.DisplayTitle = comicDisplayTitle(comicLeafTitle(*item, rel), set.rules, set.compiled)
	}
	if len(bookSeries) == 0 {
		return items, nil
	}
	// Normalize an author group only when its stored label exactly matches all
	// member authors. Ordinary curated collection names must stay verbatim.
	placeholders := strings.TrimRight(strings.Repeat("?,", len(bookSeries)), ",")
	rows, err := s.db.Query(`SELECT s.id FROM series s JOIN books b ON b.series_id=s.id
		WHERE s.id IN (`+placeholders+`) GROUP BY s.id
		HAVING MIN(b.creator)=s.title AND MAX(b.creator)=s.title AND s.title<>''`, bookSeries...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	authors := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		authors[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range items {
		if authors[items[i].ID] {
			if label := domain.CreatorDisplayName(items[i].Title); label != "" {
				items[i].DisplayTitle = label
			}
		}
	}
	return items, nil
}
