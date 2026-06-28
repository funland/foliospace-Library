package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"foliospace-reader/internal/domain"
)

type Store struct {
	db *sql.DB
}

const defaultProfileID int64 = 1

func New(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) DefaultProfile() (domain.Profile, error) {
	row := s.db.QueryRow(`SELECT id, name, avatar, color, is_default, created_at, updated_at FROM profiles WHERE is_default = 1 ORDER BY id LIMIT 1`)
	return scanProfile(row)
}

func (s *Store) ProfileByID(profileID int64) (domain.Profile, error) {
	row := s.db.QueryRow(`SELECT id, name, avatar, color, is_default, created_at, updated_at FROM profiles WHERE id = ?`, profileID)
	return scanProfile(row)
}

func (s *Store) ResolveProfileID(profileID int64) (int64, error) {
	if profileID > 0 {
		if _, err := s.ProfileByID(profileID); err == nil {
			return profileID, nil
		} else if err != sql.ErrNoRows {
			return 0, err
		}
	}
	profile, err := s.DefaultProfile()
	if err != nil {
		return 0, err
	}
	return profile.ID, nil
}

func (s *Store) ListProfiles() ([]domain.Profile, error) {
	rows, err := s.db.Query(`SELECT id, name, avatar, color, is_default, created_at, updated_at FROM profiles ORDER BY is_default DESC, name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Profile, 0)
	for rows.Next() {
		profile, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, profile)
	}
	return out, rows.Err()
}

func (s *Store) CreateProfile(name string, avatar string, color string) (domain.Profile, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Profile"
	}
	avatar = normalizeProfileAvatar(avatar)
	color = normalizeProfileColor(color)
	result, err := s.db.Exec(`INSERT INTO profiles(name, avatar, color, is_default) VALUES(?, ?, ?, 0)`, name, avatar, color)
	if err != nil {
		return domain.Profile{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return domain.Profile{}, err
	}
	return s.ProfileByID(id)
}

func (s *Store) UpdateProfile(profileID int64, name string, avatar string, color string) (domain.Profile, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Profile"
	}
	avatar = normalizeProfileAvatar(avatar)
	color = normalizeProfileColor(color)
	if _, err := s.db.Exec(`UPDATE profiles SET name = ?, avatar = ?, color = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, name, avatar, color, profileID); err != nil {
		return domain.Profile{}, err
	}
	return s.ProfileByID(profileID)
}

func (s *Store) RenameProfile(profileID int64, name string) (domain.Profile, error) {
	profile, err := s.ProfileByID(profileID)
	if err != nil {
		return domain.Profile{}, err
	}
	return s.UpdateProfile(profileID, name, profile.Avatar, profile.Color)
}

func (s *Store) DeleteProfile(profileID int64) error {
	if profileID == defaultProfileID {
		return fmt.Errorf("cannot delete default profile")
	}
	_, err := s.db.Exec(`DELETE FROM profiles WHERE id = ? AND is_default = 0`, profileID)
	return err
}

func (s *Store) Setting(key string) (string, error) {
	row := s.db.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, strings.TrimSpace(key))
	var value string
	if err := row.Scan(&value); err != nil {
		return "", err
	}
	return value, nil
}

func (s *Store) UpsertSetting(key string, value string) error {
	_, err := s.db.Exec(`INSERT INTO app_settings(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP`,
		strings.TrimSpace(key), strings.TrimSpace(value))
	return err
}

func (s *Store) CreateLibrary(name string, rootPath string) (domain.Library, error) {
	return s.CreateLibraryWithType(name, rootPath, "mixed")
}

func (s *Store) CreateLibraryWithType(name string, rootPath string, assetType string) (domain.Library, error) {
	return s.CreateLibraryWithOptions(name, rootPath, assetType, nil)
}

func (s *Store) CreateLibraryWithOptions(name string, rootPath string, assetType string, excludePatterns []string) (domain.Library, error) {
	assetType = normalizeLibraryAssetType(assetType)
	excludeJSON := encodeStringList(normalizeLibraryExcludePatterns(excludePatterns))
	_, err := s.db.Exec(`INSERT INTO libraries(name, root_path, asset_type, exclude_patterns) VALUES(?, ?, ?, ?)
		ON CONFLICT(root_path) DO UPDATE SET name = excluded.name, asset_type = excluded.asset_type, exclude_patterns = excluded.exclude_patterns, updated_at = CURRENT_TIMESTAMP`,
		name, rootPath, assetType, excludeJSON)
	if err != nil {
		return domain.Library{}, err
	}
	return s.LibraryByRoot(rootPath)
}

func (s *Store) LibraryByID(id int64) (domain.Library, error) {
	row := s.db.QueryRow(`SELECT id, name, root_path, asset_type, exclude_patterns, created_at, updated_at FROM libraries WHERE id = ?`, id)
	return scanLibrary(row)
}

func (s *Store) LibraryByRoot(rootPath string) (domain.Library, error) {
	row := s.db.QueryRow(`SELECT id, name, root_path, asset_type, exclude_patterns, created_at, updated_at FROM libraries WHERE root_path = ?`, rootPath)
	return scanLibrary(row)
}

func (s *Store) ListLibraries() ([]domain.Library, error) {
	rows, err := s.db.Query(`SELECT id, name, root_path, asset_type, exclude_patterns, created_at, updated_at FROM libraries ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Library, 0)
	for rows.Next() {
		lib, err := scanLibrary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, lib)
	}
	return out, rows.Err()
}

func (s *Store) UpdateLibraryExcludePatterns(id int64, excludePatterns []string) (domain.Library, error) {
	result, err := s.db.Exec(`UPDATE libraries SET exclude_patterns = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		encodeStringList(normalizeLibraryExcludePatterns(excludePatterns)), id)
	if err != nil {
		return domain.Library{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return domain.Library{}, err
	}
	if affected == 0 {
		return domain.Library{}, sql.ErrNoRows
	}
	return s.LibraryByID(id)
}

func (s *Store) DeleteLibrary(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`SELECT b.id FROM books b JOIN series s ON s.id = b.series_id WHERE s.library_id = ?`, id)
	if err != nil {
		return err
	}
	var bookIDs []int64
	for rows.Next() {
		var bookID int64
		if err := rows.Scan(&bookID); err != nil {
			_ = rows.Close()
			return err
		}
		bookIDs = append(bookIDs, bookID)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, bookID := range bookIDs {
		if _, err := tx.Exec(`DELETE FROM read_progress WHERE book_id = ?`, bookID); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM pages WHERE book_id = ?`, bookID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM file_errors WHERE library_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM job_events WHERE job_id IN (SELECT id FROM scan_jobs WHERE library_id = ?)`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM scan_jobs WHERE library_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM games WHERE library_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM videos WHERE library_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM files WHERE library_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM books WHERE series_id IN (SELECT id FROM series WHERE library_id = ?)`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM series WHERE library_id = ?`, id); err != nil {
		return err
	}
	res, err := tx.Exec(`DELETE FROM libraries WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

func (s *Store) UpsertSeries(libraryID int64, title string, directoryPath string) (domain.Series, error) {
	_, err := s.db.Exec(`INSERT INTO series(library_id, title, directory_path, collection_type) VALUES(?, ?, ?, 'directory')
		ON CONFLICT(library_id, title) DO UPDATE SET directory_path = excluded.directory_path, collection_type = 'directory', updated_at = CURRENT_TIMESTAMP`,
		libraryID, title, directoryPath)
	if err != nil {
		return domain.Series{}, err
	}
	row := s.db.QueryRow(`SELECT s.id, s.library_id, s.title, s.directory_path, s.collection_type,
			CASE WHEN l.asset_type IN ('book', 'comic', 'game', 'video') THEN l.asset_type ELSE 'comic' END,
			0,
			0,
			s.created_at
		FROM series s
		JOIN libraries l ON l.id = s.library_id
		WHERE s.library_id = ? AND s.title = ?`, libraryID, title)
	return scanSeries(row)
}

func (s *Store) SeriesByID(id int64) (domain.Series, error) {
	return s.SeriesByIDForProfile(id, defaultProfileID)
}

func (s *Store) SeriesByIDForProfile(id int64, profileID int64) (domain.Series, error) {
	row := s.db.QueryRow(`SELECT s.id, s.library_id, s.title,
			COALESCE(NULLIF(s.directory_path, ''), ''),
			s.collection_type,
			CASE
				WHEN l.asset_type IN ('book', 'comic', 'game', 'video') THEN l.asset_type
				WHEN SUM(CASE WHEN b.format IN ('epub', 'pdf') THEN 1 ELSE 0 END) > SUM(CASE WHEN b.format IN ('cbz', 'zip', 'cbr', 'rar', '7z') THEN 1 ELSE 0 END) THEN 'book'
				ELSE 'comic'
			END,
			COUNT(DISTINCT b.id),
			COALESCE((
				SELECT b2.id
				FROM books b2
				WHERE b2.series_id = s.id
				ORDER BY b2.title, b2.id
				LIMIT 1
			), 0),
			COALESCE(MAX(b.created_at), s.created_at)
		FROM series s
		JOIN libraries l ON l.id = s.library_id
		LEFT JOIN books b ON b.series_id = s.id
		WHERE s.id = ?
		GROUP BY s.id, s.library_id, s.title, s.created_at, l.asset_type`, id)
	series, err := scanSeries(row)
	if err != nil {
		return domain.Series{}, err
	}
	items, err := s.applyCollectionPrivateStates(profileID, []domain.Series{series})
	if err != nil {
		return domain.Series{}, err
	}
	return items[0], nil
}

func (s *Store) ListSeries() ([]domain.Series, error) {
	return s.ListSeriesForProfile(defaultProfileID)
}

func (s *Store) ListSeriesForProfile(profileID int64) ([]domain.Series, error) {
	return s.listSeriesForProfile(profileID, 0)
}

func (s *Store) ListSeriesForProfileLimit(profileID int64, limit int) ([]domain.Series, error) {
	return s.listSeriesForProfile(profileID, limit)
}

func (s *Store) ListSeriesPageForProfile(profileID int64, options domain.CollectionListOptions) (domain.CollectionListPage, error) {
	profileID, err := s.ResolveProfileID(profileID)
	if err != nil {
		return domain.CollectionListPage{}, err
	}
	options.Limit = normalizeCollectionListLimit(options.Limit)
	if options.Offset < 0 {
		options.Offset = 0
	}
	where, args := collectionListWhere(options)
	countArgs := append([]any(nil), args...)
	var total int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM (`+collectionListBaseSQL()+`) c`+where, countArgs...).Scan(&total); err != nil {
		return domain.CollectionListPage{}, err
	}
	queryArgs := append([]any(nil), args...)
	queryArgs = append(queryArgs, options.Limit, options.Offset)
	rows, err := s.db.Query(`SELECT c.id, c.library_id, c.title, c.directory_path, c.collection_type, c.primary_type, c.book_count, c.cover_book_id, c.created_at
		FROM (`+collectionListBaseSQL()+`) c`+where+collectionListOrderBy(options.Sort, options.Direction)+`
		LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return domain.CollectionListPage{}, err
	}
	defer rows.Close()
	items := make([]domain.Series, 0)
	for rows.Next() {
		series, err := scanSeries(rows)
		if err != nil {
			return domain.CollectionListPage{}, err
		}
		items = append(items, series)
	}
	if err := rows.Err(); err != nil {
		return domain.CollectionListPage{}, err
	}
	items, err = s.applyCollectionPrivateStates(profileID, items)
	if err != nil {
		return domain.CollectionListPage{}, err
	}
	return domain.CollectionListPage{
		Items:   items,
		Total:   total,
		Limit:   options.Limit,
		Offset:  options.Offset,
		HasMore: int64(options.Offset+len(items)) < total,
	}, nil
}

func (s *Store) listSeriesForProfile(profileID int64, limit int) ([]domain.Series, error) {
	query := `SELECT c.id, c.library_id, c.title, c.directory_path, c.collection_type, c.primary_type, c.book_count, c.cover_book_id, c.created_at
		FROM (` + collectionListBaseSQL() + `) c
		ORDER BY c.title`
	args := []any{}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Series, 0)
	for rows.Next() {
		series, err := scanSeries(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, series)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return s.applyCollectionPrivateStates(profileID, out)
}

func collectionListBaseSQL() string {
	return `SELECT s.id AS id, s.library_id AS library_id, s.title AS title,
			COALESCE(NULLIF(s.directory_path, ''), MIN(CASE
				WHEN f.rel_path IS NULL THEN ''
				WHEN INSTR(f.rel_path, '/') = 0 THEN '.'
				ELSE SUBSTR(f.rel_path, 1, INSTR(f.rel_path, '/') - 1)
			END), '') AS directory_path,
			s.collection_type AS collection_type,
			CASE
				WHEN l.asset_type IN ('book', 'comic', 'game', 'video') THEN l.asset_type
				WHEN SUM(CASE WHEN b.format IN ('epub', 'pdf') THEN 1 ELSE 0 END) > SUM(CASE WHEN b.format IN ('cbz', 'zip', 'cbr', 'rar', '7z') THEN 1 ELSE 0 END) THEN 'book'
				ELSE 'comic'
			END AS primary_type,
			COUNT(DISTINCT b.id) AS book_count,
			COALESCE((
				SELECT b2.id
				FROM books b2
				WHERE b2.series_id = s.id
				ORDER BY b2.title, b2.id
				LIMIT 1
			), 0) AS cover_book_id,
			COALESCE(MAX(b.created_at), s.created_at) AS created_at
		FROM series s
		JOIN libraries l ON l.id = s.library_id
		LEFT JOIN books b ON b.series_id = s.id
		LEFT JOIN files f ON f.book_id = b.id
		GROUP BY s.id, s.library_id, s.title, s.created_at, l.asset_type`
}

func normalizeCollectionListLimit(limit int) int {
	if limit <= 0 {
		return 60
	}
	if limit > 200 {
		return 200
	}
	return limit
}

func collectionListWhere(options domain.CollectionListOptions) (string, []any) {
	whereParts := make([]string, 0, 2)
	args := make([]any, 0, 2)
	primaryType := strings.ToLower(strings.TrimSpace(options.PrimaryType))
	if primaryType != "" && primaryType != "all" {
		whereParts = append(whereParts, "LOWER(c.primary_type) = ?")
		args = append(args, primaryType)
	}
	query := strings.TrimSpace(options.Query)
	if query != "" {
		whereParts = append(whereParts, `LOWER(c.title) LIKE LOWER(?) ESCAPE '\'`)
		args = append(args, "%"+escapeLike(query)+"%")
	}
	if len(whereParts) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(whereParts, " AND "), args
}

func collectionListOrderBy(sort string, direction string) string {
	titleDir := normalizedSortDirection(direction, "ASC")
	recentDir := normalizedSortDirection(direction, "DESC")
	switch strings.ToLower(strings.TrimSpace(sort)) {
	case "recent", "recently_added":
		return " ORDER BY c.created_at " + recentDir + ", c.title ASC, c.id ASC"
	case "book_count", "count":
		return " ORDER BY c.book_count " + normalizedSortDirection(direction, "DESC") + ", c.title ASC, c.id ASC"
	case "type", "primary_type":
		return " ORDER BY c.primary_type " + titleDir + ", c.title ASC, c.id ASC"
	default:
		return " ORDER BY c.title " + titleDir + ", c.id " + titleDir
	}
}

func (s *Store) ListGamePlatformCollections() ([]domain.Series, error) {
	rows, err := s.db.Query(`SELECT platform, COUNT(*) FROM games GROUP BY platform`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Series, 0)
	for rows.Next() {
		var platform string
		var count int64
		if err := rows.Scan(&platform, &count); err != nil {
			return nil, err
		}
		platform = strings.TrimSpace(platform)
		if platform == "" {
			platform = "unknown"
		}
		out = append(out, domain.Series{
			ID:             GamePlatformCollectionID(platform),
			Title:          "Games / " + GamePlatformLabel(platform),
			DirectoryPath:  "Games",
			CollectionType: "game_platform",
			PrimaryType:    "game",
			BookCount:      count,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i int, j int) bool {
		left := GamePlatformSortRank(platformFromGameCollectionTitle(out[i].Title))
		right := GamePlatformSortRank(platformFromGameCollectionTitle(out[j].Title))
		if left != right {
			return left < right
		}
		return out[i].Title < out[j].Title
	})
	return out, nil
}

func (s *Store) DeleteEmptySeries(libraryID int64) error {
	_, err := s.db.Exec(`DELETE FROM series
		WHERE library_id = ?
		AND id NOT IN (SELECT DISTINCT series_id FROM books)`, libraryID)
	return err
}

func (s *Store) CollectionPrivateStateForProfile(seriesID int64, profileID int64) (domain.CollectionPrivateState, error) {
	profileID, err := s.ResolveProfileID(profileID)
	if err != nil {
		return domain.CollectionPrivateState{}, err
	}
	var favorite int
	var liked int
	err = s.db.QueryRow(`SELECT favorite, liked FROM collection_private_states WHERE profile_id = ? AND series_id = ?`, profileID, seriesID).Scan(&favorite, &liked)
	if err == sql.ErrNoRows {
		return domain.CollectionPrivateState{}, nil
	}
	if err != nil {
		return domain.CollectionPrivateState{}, err
	}
	return domain.CollectionPrivateState{Favorite: favorite != 0, Liked: liked != 0}, nil
}

func (s *Store) UpdateCollectionPrivateStateForProfile(seriesID int64, profileID int64, state domain.CollectionPrivateState) error {
	profileID, err := s.ResolveProfileID(profileID)
	if err != nil {
		return err
	}
	if _, err := s.SeriesByID(seriesID); err != nil {
		return err
	}
	favorite := 0
	if state.Favorite {
		favorite = 1
	}
	liked := 0
	if state.Liked {
		liked = 1
	}
	_, err = s.db.Exec(`INSERT INTO collection_private_states(profile_id, series_id, favorite, liked)
		VALUES(?, ?, ?, ?)
		ON CONFLICT(profile_id, series_id) DO UPDATE SET favorite = excluded.favorite,
			liked = excluded.liked,
			updated_at = CURRENT_TIMESTAMP`, profileID, seriesID, favorite, liked)
	return err
}

func (s *Store) applyCollectionPrivateStates(profileID int64, items []domain.Series) ([]domain.Series, error) {
	if len(items) == 0 {
		return items, nil
	}
	profileID, err := s.ResolveProfileID(profileID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT series_id, favorite, liked FROM collection_private_states WHERE profile_id = ?`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type state struct {
		favorite bool
		liked    bool
	}
	states := make(map[int64]state)
	for rows.Next() {
		var seriesID int64
		var favorite int
		var liked int
		if err := rows.Scan(&seriesID, &favorite, &liked); err != nil {
			return nil, err
		}
		states[seriesID] = state{favorite: favorite != 0, liked: liked != 0}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range items {
		if itemState, ok := states[items[i].ID]; ok {
			items[i].Favorite = itemState.favorite
			items[i].Liked = itemState.liked
		}
	}
	return items, nil
}

func (s *Store) CreateManualCollection(collection domain.ManualCollection) (domain.ManualCollection, error) {
	name := strings.TrimSpace(collection.Name)
	if name == "" {
		return domain.ManualCollection{}, fmt.Errorf("manual collection name is required")
	}
	description := strings.TrimSpace(collection.Description)
	res, err := s.db.Exec(`INSERT INTO manual_collections(name, description) VALUES(?, ?)`, name, description)
	if err != nil {
		return domain.ManualCollection{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.ManualCollection{}, err
	}
	return s.ManualCollectionByID(id)
}

func (s *Store) UpdateManualCollection(collectionID int64, collection domain.ManualCollection) (domain.ManualCollection, error) {
	name := strings.TrimSpace(collection.Name)
	if name == "" {
		return domain.ManualCollection{}, fmt.Errorf("manual collection name is required")
	}
	description := strings.TrimSpace(collection.Description)
	res, err := s.db.Exec(`UPDATE manual_collections SET name = ?, description = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, name, description, collectionID)
	if err != nil {
		return domain.ManualCollection{}, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return domain.ManualCollection{}, err
	}
	if affected == 0 {
		return domain.ManualCollection{}, sql.ErrNoRows
	}
	return s.ManualCollectionByID(collectionID)
}

func (s *Store) DeleteManualCollection(collectionID int64) error {
	res, err := s.db.Exec(`DELETE FROM manual_collections WHERE id = ?`, collectionID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) ManualCollectionByID(collectionID int64) (domain.ManualCollection, error) {
	row := s.db.QueryRow(`SELECT c.id, c.name, c.description, COUNT(i.asset_id), c.created_at, c.updated_at
		FROM manual_collections c
		LEFT JOIN manual_collection_items i ON i.collection_id = c.id
		WHERE c.id = ?
		GROUP BY c.id`, collectionID)
	return scanManualCollection(row)
}

func (s *Store) ListManualCollections() ([]domain.ManualCollection, error) {
	rows, err := s.db.Query(`SELECT c.id, c.name, c.description, COUNT(i.asset_id), c.created_at, c.updated_at
		FROM manual_collections c
		LEFT JOIN manual_collection_items i ON i.collection_id = c.id
		GROUP BY c.id
		ORDER BY LOWER(c.name), c.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.ManualCollection, 0)
	for rows.Next() {
		collection, err := scanManualCollection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, collection)
	}
	return out, rows.Err()
}

func (s *Store) AddManualCollectionItem(collectionID int64, item domain.ManualCollectionItem) error {
	if _, err := s.ManualCollectionByID(collectionID); err != nil {
		return err
	}
	assetType := normalizeManualCollectionAssetType(item.AssetType)
	if assetType == "" {
		return fmt.Errorf("unsupported manual collection asset type: %s", item.AssetType)
	}
	if item.AssetID <= 0 {
		return fmt.Errorf("manual collection asset id is required")
	}
	_, err := s.db.Exec(`INSERT OR IGNORE INTO manual_collection_items(collection_id, asset_type, asset_id) VALUES(?, ?, ?)`, collectionID, assetType, item.AssetID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE manual_collections SET updated_at = CURRENT_TIMESTAMP WHERE id = ?`, collectionID)
	return err
}

func (s *Store) RemoveManualCollectionItem(collectionID int64, assetType string, assetID int64) error {
	assetType = normalizeManualCollectionAssetType(assetType)
	if assetType == "" {
		return fmt.Errorf("unsupported manual collection asset type")
	}
	res, err := s.db.Exec(`DELETE FROM manual_collection_items WHERE collection_id = ? AND asset_type = ? AND asset_id = ?`, collectionID, assetType, assetID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	_, err = s.db.Exec(`UPDATE manual_collections SET updated_at = CURRENT_TIMESTAMP WHERE id = ?`, collectionID)
	return err
}

func (s *Store) ListManualCollectionItems(collectionID int64) ([]domain.ManualCollectionItem, error) {
	if _, err := s.ManualCollectionByID(collectionID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT collection_id, asset_type, asset_id, created_at
		FROM manual_collection_items
		WHERE collection_id = ?
		ORDER BY created_at, rowid`, collectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.ManualCollectionItem, 0)
	for rows.Next() {
		var item domain.ManualCollectionItem
		var createdAt string
		if err := rows.Scan(&item.CollectionID, &item.AssetType, &item.AssetID, &createdAt); err != nil {
			return nil, err
		}
		item.CreatedAt = parseTime(createdAt)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) DeleteSkippedDirectoryEntries(libraryID int64, names []string) (int64, error) {
	if len(names) == 0 {
		return 0, nil
	}
	conditions, args := skippedDirectoryConditions("rel_path", names)
	args = append([]any{libraryID}, args...)

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`SELECT DISTINCT book_id FROM files WHERE library_id = ? AND (`+conditions+`)`, args...)
	if err != nil {
		return 0, err
	}
	var bookIDs []int64
	for rows.Next() {
		var bookID int64
		if err := rows.Scan(&bookID); err != nil {
			_ = rows.Close()
			return 0, err
		}
		bookIDs = append(bookIDs, bookID)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	fileRes, err := tx.Exec(`DELETE FROM files WHERE library_id = ? AND (`+conditions+`)`, args...)
	if err != nil {
		return 0, err
	}
	deletedFiles, err := fileRes.RowsAffected()
	if err != nil {
		return 0, err
	}

	gameConditions, gameArgs := skippedDirectoryConditions("rel_path", names)
	gameArgs = append([]any{libraryID}, gameArgs...)
	gameRes, err := tx.Exec(`DELETE FROM games WHERE library_id = ? AND (`+gameConditions+`)`, gameArgs...)
	if err != nil {
		return 0, err
	}
	deletedGames, err := gameRes.RowsAffected()
	if err != nil {
		return 0, err
	}

	videoConditions, videoArgs := skippedDirectoryConditions("rel_path", names)
	videoArgs = append([]any{libraryID}, videoArgs...)
	videoRes, err := tx.Exec(`DELETE FROM videos WHERE library_id = ? AND (`+videoConditions+`)`, videoArgs...)
	if err != nil {
		return 0, err
	}
	deletedVideos, err := videoRes.RowsAffected()
	if err != nil {
		return 0, err
	}

	errorConditions, errorArgs := skippedDirectoryConditions("path", names)
	errorArgs = append([]any{libraryID}, errorArgs...)
	if _, err := tx.Exec(`DELETE FROM file_errors WHERE library_id = ? AND (`+errorConditions+`)`, errorArgs...); err != nil {
		return 0, err
	}

	orphanBookIDs, err := orphanedBookIDs(tx, bookIDs)
	if err != nil {
		return 0, err
	}
	if len(orphanBookIDs) > 0 {
		placeholders, deleteArgs := int64Placeholders(orphanBookIDs)
		if _, err := tx.Exec(`DELETE FROM read_progress WHERE book_id IN (`+placeholders+`)`, deleteArgs...); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(`DELETE FROM pages WHERE book_id IN (`+placeholders+`)`, deleteArgs...); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(`DELETE FROM books WHERE id IN (`+placeholders+`)`, deleteArgs...); err != nil {
			return 0, err
		}
	}
	if _, err := tx.Exec(`DELETE FROM series
		WHERE library_id = ?
		AND id NOT IN (SELECT DISTINCT series_id FROM books)`, libraryID); err != nil {
		return 0, err
	}
	return deletedFiles + deletedGames + deletedVideos, tx.Commit()
}

func skippedDirectoryConditions(column string, names []string) (string, []any) {
	conditions := make([]string, 0, len(names)*2)
	args := make([]any, 0, len(names)*3)
	for _, name := range names {
		name = strings.Trim(strings.TrimSpace(name), `/\`)
		if name == "" {
			continue
		}
		conditions = append(conditions, column+" = ?", column+" LIKE ?", column+" LIKE ?")
		args = append(args, name, name+"/%", "%/"+name+"/%")
	}
	if len(conditions) == 0 {
		return "1 = 0", nil
	}
	return strings.Join(conditions, " OR "), args
}

func orphanedBookIDs(tx *sql.Tx, ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders, args := int64Placeholders(ids)
	rows, err := tx.Query(`SELECT b.id FROM books b
		WHERE b.id IN (`+placeholders+`)
		AND NOT EXISTS (SELECT 1 FROM files f WHERE f.book_id = b.id)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]int64, 0, len(ids))
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func int64Placeholders(ids []int64) (string, []any) {
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	return strings.Join(placeholders, ","), args
}

func (s *Store) UpsertBook(seriesID int64, title string, format string) (domain.Book, error) {
	_, err := s.db.Exec(`INSERT INTO books(series_id, title, format) VALUES(?, ?, ?)
		ON CONFLICT(series_id, title, format) DO UPDATE SET updated_at = CURRENT_TIMESTAMP`, seriesID, title, format)
	if err != nil {
		return domain.Book{}, err
	}
	return s.BookBySeriesTitle(seriesID, title, format)
}

func (s *Store) BookBySeriesTitle(seriesID int64, title string, format string) (domain.Book, error) {
	row := s.db.QueryRow(bookSelectSQL(defaultProfileID)+`
		WHERE b.series_id = ? AND b.title = ? AND b.format = ?`, seriesID, title, format)
	return scanBook(row)
}

func (s *Store) BookByID(id int64) (domain.Book, error) {
	return s.BookByIDForProfile(id, defaultProfileID)
}

func (s *Store) BookByIDForProfile(id int64, profileID int64) (domain.Book, error) {
	profileID, err := s.ResolveProfileID(profileID)
	if err != nil {
		return domain.Book{}, err
	}
	row := s.db.QueryRow(bookSelectSQL(profileID)+` WHERE b.id = ?`, id)
	return scanBook(row)
}

func (s *Store) UpdateBookIdentity(bookID int64, seriesID int64, title string, format string) (domain.Book, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return domain.Book{}, err
	}
	defer tx.Rollback()

	var previousSeriesID int64
	if err := tx.QueryRow(`SELECT series_id FROM books WHERE id = ?`, bookID).Scan(&previousSeriesID); err != nil {
		return domain.Book{}, err
	}

	_, err = tx.Exec(`UPDATE books
		SET series_id = ?, title = ?, format = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`, seriesID, title, format, bookID)
	if err != nil {
		return domain.Book{}, err
	}
	if previousSeriesID != seriesID {
		if err := mergeCollectionPrivateStatesTx(tx, previousSeriesID, seriesID); err != nil {
			return domain.Book{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return domain.Book{}, err
	}
	return s.BookByID(bookID)
}

func mergeCollectionPrivateStatesTx(tx *sql.Tx, fromSeriesID int64, toSeriesID int64) error {
	_, err := tx.Exec(`INSERT INTO collection_private_states(profile_id, series_id, favorite, liked, updated_at)
		SELECT profile_id, ?, favorite, liked, CURRENT_TIMESTAMP
		FROM collection_private_states
		WHERE series_id = ? AND (favorite <> 0 OR liked <> 0)
		ON CONFLICT(profile_id, series_id) DO UPDATE SET
			favorite = CASE
				WHEN collection_private_states.favorite <> 0 OR excluded.favorite <> 0 THEN 1
				ELSE 0
			END,
			liked = CASE
				WHEN collection_private_states.liked <> 0 OR excluded.liked <> 0 THEN 1
				ELSE 0
			END,
			updated_at = CURRENT_TIMESTAMP`, toSeriesID, fromSeriesID)
	return err
}

func (s *Store) UpdateBookMetadata(bookID int64, creator string, description string, tags []string) (domain.Book, error) {
	_, err := s.db.Exec(`UPDATE books
		SET creator = ?, description = ?, tags = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`, strings.TrimSpace(creator), strings.TrimSpace(description), encodeTags(tags), bookID)
	if err != nil {
		return domain.Book{}, err
	}
	return s.BookByID(bookID)
}

func (s *Store) UpdateBookPrivateState(bookID int64, state domain.BookPrivateState) error {
	return s.UpdateBookPrivateStateForProfile(bookID, defaultProfileID, state)
}

func (s *Store) UpdateBookPrivateStateForProfile(bookID int64, profileID int64, state domain.BookPrivateState) error {
	profileID, err := s.ResolveProfileID(profileID)
	if err != nil {
		return err
	}
	status := strings.TrimSpace(state.Status)
	summary := strings.TrimSpace(state.Summary)
	rating := state.Rating
	if rating < 0 {
		rating = 0
	}
	if rating > 5 {
		rating = 5
	}
	favorite := 0
	if state.Favorite {
		favorite = 1
	}
	_, err = s.db.Exec(`INSERT INTO book_private_states(profile_id, book_id, private_status, favorite, rating, tags, summary)
		VALUES(?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(profile_id, book_id) DO UPDATE SET private_status = excluded.private_status,
			favorite = excluded.favorite,
			rating = excluded.rating,
			tags = excluded.tags,
			summary = excluded.summary,
			updated_at = CURRENT_TIMESTAMP`, profileID, bookID, status, favorite, rating, encodeTags(state.Tags), summary)
	if err != nil {
		return err
	}
	if profileID == defaultProfileID {
		_, err = s.db.Exec(`UPDATE books
			SET private_status = ?, favorite = ?, rating = ?, tags = ?, summary = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?`, status, favorite, rating, encodeTags(state.Tags), summary, bookID)
	}
	return err
}

func (s *Store) ClientPreferences() (domain.ClientPreferences, error) {
	return s.ClientPreferencesForProfile(defaultProfileID)
}

func (s *Store) ClientPreferencesForProfile(profileID int64) (domain.ClientPreferences, error) {
	profileID, err := s.ResolveProfileID(profileID)
	if err != nil {
		return domain.ClientPreferences{}, err
	}
	row := s.db.QueryRow(`SELECT locale, reader_page_mode, epub_page_mode, epub_theme, epub_font_size FROM profile_client_preferences WHERE profile_id = ?`, profileID)
	var prefs domain.ClientPreferences
	err = row.Scan(&prefs.Locale, &prefs.ReaderPageMode, &prefs.EPUBPageMode, &prefs.EPUBTheme, &prefs.EPUBFontSize)
	if err == nil {
		return prefs, nil
	}
	if err != sql.ErrNoRows {
		return domain.ClientPreferences{}, err
	}
	if profileID != defaultProfileID {
		return DefaultClientPreferences(), nil
	}
	row = s.db.QueryRow(`SELECT locale, reader_page_mode, epub_page_mode, epub_theme, epub_font_size FROM client_preferences WHERE id = 1`)
	err = row.Scan(&prefs.Locale, &prefs.ReaderPageMode, &prefs.EPUBPageMode, &prefs.EPUBTheme, &prefs.EPUBFontSize)
	if err == sql.ErrNoRows {
		return DefaultClientPreferences(), nil
	}
	if err != nil {
		return domain.ClientPreferences{}, err
	}
	return prefs, nil
}

func (s *Store) SaveClientPreferences(prefs domain.ClientPreferences) error {
	return s.SaveClientPreferencesForProfile(defaultProfileID, prefs)
}

func (s *Store) SaveClientPreferencesForProfile(profileID int64, prefs domain.ClientPreferences) error {
	profileID, err := s.ResolveProfileID(profileID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO profile_client_preferences(profile_id, locale, reader_page_mode, epub_page_mode, epub_theme, epub_font_size)
		VALUES(?, ?, ?, ?, ?, ?)
		ON CONFLICT(profile_id) DO UPDATE SET locale = excluded.locale,
			reader_page_mode = excluded.reader_page_mode,
			epub_page_mode = excluded.epub_page_mode,
			epub_theme = excluded.epub_theme,
			epub_font_size = excluded.epub_font_size,
			updated_at = CURRENT_TIMESTAMP`,
		profileID, prefs.Locale, prefs.ReaderPageMode, prefs.EPUBPageMode, prefs.EPUBTheme, prefs.EPUBFontSize)
	if err != nil {
		return err
	}
	if profileID == defaultProfileID {
		_, err = s.db.Exec(`INSERT INTO client_preferences(id, locale, reader_page_mode, epub_page_mode, epub_theme, epub_font_size)
		VALUES(1, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET locale = excluded.locale,
			reader_page_mode = excluded.reader_page_mode,
			epub_page_mode = excluded.epub_page_mode,
			epub_theme = excluded.epub_theme,
			epub_font_size = excluded.epub_font_size,
			updated_at = CURRENT_TIMESTAMP`,
			prefs.Locale, prefs.ReaderPageMode, prefs.EPUBPageMode, prefs.EPUBTheme, prefs.EPUBFontSize)
	}
	return err
}

func DefaultClientPreferences() domain.ClientPreferences {
	return domain.ClientPreferences{
		Locale:         "zh",
		ReaderPageMode: "single",
		EPUBPageMode:   "single",
		EPUBTheme:      "light",
		EPUBFontSize:   18,
	}
}

func (s *Store) ListBooks(seriesID int64) ([]domain.Book, error) {
	return s.ListBooksForProfile(seriesID, defaultProfileID)
}

func (s *Store) ListBooksForProfile(seriesID int64, profileID int64) ([]domain.Book, error) {
	profileID, err := s.ResolveProfileID(profileID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(bookSelectSQL(profileID)+`
		WHERE b.series_id = ?
		ORDER BY b.title`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Book, 0)
	for rows.Next() {
		book, err := scanBook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, book)
	}
	return out, rows.Err()
}

func (s *Store) SearchBooks(query string, limit int) ([]domain.Book, error) {
	return s.SearchBooksForProfile(query, defaultProfileID, limit)
}

func (s *Store) SearchBooksForProfile(query string, profileID int64, limit int) ([]domain.Book, error) {
	profileID, err := s.ResolveProfileID(profileID)
	if err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return []domain.Book{}, nil
	}
	limit = normalizeSearchLimit(limit)
	pattern := "%" + escapeLike(query) + "%"
	rows, err := s.db.Query(bookSelectSQL(profileID)+`
		WHERE LOWER(b.title) LIKE LOWER(?) ESCAPE '\'
			OR LOWER(s.title) LIKE LOWER(?) ESCAPE '\'
			OR LOWER(COALESCE(b.creator, '')) LIKE LOWER(?) ESCAPE '\'
			OR LOWER(b.format) LIKE LOWER(?) ESCAPE '\'
			OR LOWER(COALESCE(b.tags, '')) LIKE LOWER(?) ESCAPE '\'
			OR LOWER(COALESCE(ps.tags, '')) LIKE LOWER(?) ESCAPE '\'
			OR LOWER(COALESCE(ps.summary, '')) LIKE LOWER(?) ESCAPE '\'
		ORDER BY COALESCE(ps.favorite, 0) DESC, rp.updated_at IS NULL, rp.updated_at DESC, b.updated_at DESC, b.title
		LIMIT ?`, pattern, pattern, pattern, pattern, pattern, pattern, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Book, 0)
	for rows.Next() {
		book, err := scanBook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, book)
	}
	return out, rows.Err()
}

func normalizeSearchLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func (s *Store) ListBooksPage(options domain.BookListOptions) (domain.BookListPage, error) {
	return s.ListBooksPageForProfile(options, defaultProfileID)
}

func (s *Store) ListBooksPageForProfile(options domain.BookListOptions, profileID int64) (domain.BookListPage, error) {
	profileID, err := s.ResolveProfileID(profileID)
	if err != nil {
		return domain.BookListPage{}, err
	}
	options.Limit = normalizeBookListLimit(options.Limit)
	if options.Offset < 0 {
		options.Offset = 0
	}

	where, args := bookListWhere(options)
	countArgs := append([]any(nil), args...)
	var total int64
	if err := s.db.QueryRow(`SELECT COUNT(*)
		FROM books b
		JOIN series s ON s.id = b.series_id
		LEFT JOIN profile_read_progress rp ON rp.book_id = b.id AND rp.profile_id = `+profileIDSQL(profileID)+where, countArgs...).Scan(&total); err != nil {
		return domain.BookListPage{}, err
	}

	queryArgs := append([]any(nil), args...)
	queryArgs = append(queryArgs, options.Limit, options.Offset)
	rows, err := s.db.Query(bookSelectSQL(profileID)+where+bookListOrderByDirection(options.Sort, options.Direction)+`
		LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return domain.BookListPage{}, err
	}
	defer rows.Close()

	items := make([]domain.Book, 0)
	for rows.Next() {
		book, err := scanBook(rows)
		if err != nil {
			return domain.BookListPage{}, err
		}
		items = append(items, book)
	}
	if err := rows.Err(); err != nil {
		return domain.BookListPage{}, err
	}
	return domain.BookListPage{
		Items:   items,
		Total:   total,
		Limit:   options.Limit,
		Offset:  options.Offset,
		HasMore: int64(options.Offset+len(items)) < total,
	}, nil
}

func normalizeBookListLimit(limit int) int {
	if limit <= 0 {
		return 60
	}
	if limit > 200 {
		return 200
	}
	return limit
}

func bookListWhere(options domain.BookListOptions) (string, []any) {
	whereParts := make([]string, 0, 3)
	args := make([]any, 0, 3)
	if options.SeriesID > 0 {
		whereParts = append(whereParts, "b.series_id = ?")
		args = append(args, options.SeriesID)
	}
	query := strings.TrimSpace(options.Query)
	if query != "" {
		whereParts = append(whereParts, `(LOWER(b.title) LIKE LOWER(?) ESCAPE '\'
			OR LOWER(s.title) LIKE LOWER(?) ESCAPE '\'
			OR LOWER(COALESCE(b.creator, '')) LIKE LOWER(?) ESCAPE '\'
			OR LOWER(COALESCE(b.tags, '')) LIKE LOWER(?) ESCAPE '\')`)
		pattern := "%" + escapeLike(query) + "%"
		args = append(args, pattern, pattern, pattern, pattern)
	}
	format := strings.ToLower(strings.TrimSpace(options.Format))
	if format != "" && format != "all" {
		format = strings.TrimPrefix(format, ".")
		whereParts = append(whereParts, "LOWER(b.format) = ?")
		args = append(args, format)
	}
	if len(whereParts) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(whereParts, " AND "), args
}

func bookListOrderBy(sort string) string {
	return bookListOrderByDirection(sort, "")
}

func bookListOrderByDirection(sort string, direction string) string {
	dir := normalizedSortDirection(direction, "ASC")
	recencyDir := normalizedSortDirection(direction, "DESC")
	switch sort {
	case "recently_added":
		if recencyDir == "ASC" {
			return " ORDER BY b.created_at ASC, b.id ASC"
		}
		return " ORDER BY b.created_at DESC, b.id DESC"
	case "recent":
		if recencyDir == "ASC" {
			return " ORDER BY b.created_at ASC, b.id ASC"
		}
		return " ORDER BY b.created_at DESC, b.id DESC"
	case "last_read":
		if recencyDir == "ASC" {
			return " ORDER BY rp.updated_at IS NULL, rp.updated_at ASC, b.title"
		}
		return " ORDER BY rp.updated_at IS NULL, rp.updated_at DESC, b.title"
	case "progress":
		if recencyDir == "ASC" {
			return " ORDER BY rp.progress_fraction ASC, rp.updated_at ASC, b.title"
		}
		return " ORDER BY rp.progress_fraction DESC, rp.updated_at DESC, b.title"
	case "unread":
		return " ORDER BY COALESCE(rp.progress_fraction, 0) ASC, b.title"
	default:
		return " ORDER BY b.title " + dir + ", b.id " + dir
	}
}

func normalizedSortDirection(direction string, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(direction)) {
	case "asc":
		return "ASC"
	case "desc":
		return "DESC"
	default:
		return fallback
	}
}

func escapeLike(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}

func (s *Store) ListContinueReading(limit int) ([]domain.Book, error) {
	return s.ListContinueReadingForProfile(defaultProfileID, limit)
}

func (s *Store) ListContinueReadingForProfile(profileID int64, limit int) ([]domain.Book, error) {
	profileID, err := s.ResolveProfileID(profileID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 12
	}
	rows, err := s.db.Query(bookSelectSQL(profileID)+`
		WHERE rp.book_id IS NOT NULL
		ORDER BY rp.updated_at DESC, b.updated_at DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Book, 0)
	for rows.Next() {
		book, err := scanBook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, book)
	}
	return out, rows.Err()
}

func (s *Store) ListRecentBooks(limit int) ([]domain.Book, error) {
	return s.ListRecentBooksForProfile(defaultProfileID, limit)
}

func (s *Store) ListRecentBooksForProfile(profileID int64, limit int) ([]domain.Book, error) {
	profileID, err := s.ResolveProfileID(profileID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 12
	}
	rows, err := s.db.Query(bookSelectSQL(profileID)+`
		ORDER BY b.created_at DESC, b.id DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Book, 0)
	for rows.Next() {
		book, err := scanBook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, book)
	}
	return out, rows.Err()
}

func (s *Store) ListFavoriteBooks(limit int) ([]domain.Book, error) {
	return s.ListFavoriteBooksForProfile(defaultProfileID, limit)
}

func (s *Store) ListFavoriteBooksForProfile(profileID int64, limit int) ([]domain.Book, error) {
	profileID, err := s.ResolveProfileID(profileID)
	if err != nil {
		return nil, err
	}
	limit = normalizeShelfLimit(limit)
	rows, err := s.db.Query(bookSelectSQL(profileID)+`
		WHERE COALESCE(ps.favorite, 0) = 1
		ORDER BY ps.updated_at DESC, b.title
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBooks(rows)
}

func (s *Store) ListBooksByPrivateStatus(status string, limit int) ([]domain.Book, error) {
	return s.ListBooksByPrivateStatusForProfile(defaultProfileID, status, limit)
}

func (s *Store) ListBooksByPrivateStatusForProfile(profileID int64, status string, limit int) ([]domain.Book, error) {
	profileID, err := s.ResolveProfileID(profileID)
	if err != nil {
		return nil, err
	}
	limit = normalizeShelfLimit(limit)
	rows, err := s.db.Query(bookSelectSQL(profileID)+`
		WHERE COALESCE(ps.private_status, '') = ?
		ORDER BY ps.updated_at DESC, b.title
		LIMIT ?`, strings.TrimSpace(status), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBooks(rows)
}

func normalizeShelfLimit(limit int) int {
	if limit <= 0 {
		return 12
	}
	if limit > 50 {
		return 50
	}
	return limit
}

func (s *Store) UpsertGame(game domain.GameAsset) (domain.GameAsset, error) {
	game.Platform = strings.TrimSpace(game.Platform)
	game.ROMSetName = strings.TrimSpace(game.ROMSetName)
	game.Region = strings.TrimSpace(game.Region)
	game.Format = strings.TrimSpace(game.Format)
	game.EmulatorHint = strings.TrimSpace(game.EmulatorHint)
	if strings.TrimSpace(game.Compatibility) == "" {
		game.Compatibility = "unknown"
	}
	_, err := s.db.Exec(`INSERT INTO games(library_id, title, platform, rom_set_name, region, format, file_path, rel_path, size, mtime, crc32, sha1, emulator_hint, compatibility)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(file_path) DO UPDATE SET library_id = excluded.library_id,
			title = excluded.title,
			platform = excluded.platform,
			rom_set_name = excluded.rom_set_name,
			region = excluded.region,
			format = excluded.format,
			rel_path = excluded.rel_path,
			size = excluded.size,
			mtime = excluded.mtime,
			crc32 = excluded.crc32,
			sha1 = excluded.sha1,
			emulator_hint = excluded.emulator_hint,
			compatibility = excluded.compatibility,
			updated_at = CURRENT_TIMESTAMP`,
		game.LibraryID, game.Title, game.Platform, game.ROMSetName, game.Region, game.Format, game.FilePath, game.RelPath, game.Size, game.MTime.Format(time.RFC3339Nano), game.CRC32, game.SHA1, game.EmulatorHint, game.Compatibility)
	if err != nil {
		return domain.GameAsset{}, err
	}
	return s.GameByPath(game.FilePath)
}

func (s *Store) GameByID(id int64) (domain.GameAsset, error) {
	return s.GameByIDForProfile(id, defaultProfileID)
}

func (s *Store) GameByIDForProfile(id int64, profileID int64) (domain.GameAsset, error) {
	row := s.db.QueryRow(gameSelectSQL()+` WHERE id = ?`, id)
	game, err := scanGame(row)
	if err != nil {
		return domain.GameAsset{}, err
	}
	items, err := s.applyGamePrivateStates(profileID, []domain.GameAsset{game})
	if err != nil {
		return domain.GameAsset{}, err
	}
	return items[0], nil
}

func (s *Store) GameByPath(filePath string) (domain.GameAsset, error) {
	row := s.db.QueryRow(gameSelectSQL()+` WHERE file_path = ?`, filePath)
	return scanGame(row)
}

func (s *Store) DeleteGameByPath(filePath string) error {
	_, err := s.db.Exec(`DELETE FROM games WHERE file_path = ?`, filePath)
	return err
}

func (s *Store) ListRecentGames(limit int) ([]domain.GameAsset, error) {
	limit = normalizeShelfLimit(limit)
	rows, err := s.db.Query(gameSelectSQL()+` ORDER BY updated_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanGames(rows)
}

func (s *Store) ListGamesPage(options domain.GameListOptions) (domain.GameListPage, error) {
	return s.ListGamesPageForProfile(options, defaultProfileID)
}

func (s *Store) ListGamesPageForProfile(options domain.GameListOptions, profileID int64) (domain.GameListPage, error) {
	limit := options.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := options.Offset
	if offset < 0 {
		offset = 0
	}

	where, args := gameListWhere(options)
	var total int64
	countQuery := `SELECT COUNT(*) FROM games` + where
	if err := s.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return domain.GameListPage{}, err
	}

	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, limit, offset)
	rows, err := s.db.Query(gameSelectSQL()+where+gameListOrderBy(options.Sort)+` LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return domain.GameListPage{}, err
	}
	defer rows.Close()
	items, err := scanGames(rows)
	if err != nil {
		return domain.GameListPage{}, err
	}
	items, err = s.applyGamePrivateStates(profileID, items)
	if err != nil {
		return domain.GameListPage{}, err
	}
	return domain.GameListPage{
		Items:   items,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
		HasMore: int64(offset+len(items)) < total,
	}, nil
}

func (s *Store) ListGameFacets(options domain.GameListOptions) (domain.GameListFacets, error) {
	where, args := gameListWhere(options)
	var total int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM games`+where, args...).Scan(&total); err != nil {
		return domain.GameListFacets{}, err
	}

	rows, err := s.db.Query(`
		SELECT platform, rom_set_name, format, emulator_hint, COUNT(*)
		FROM games`+where+`
		GROUP BY LOWER(platform), LOWER(rom_set_name), LOWER(format), LOWER(emulator_hint)
		ORDER BY LOWER(platform), LOWER(rom_set_name), LOWER(format), LOWER(emulator_hint)`, args...)
	if err != nil {
		return domain.GameListFacets{}, err
	}
	defer rows.Close()

	facets := make([]domain.GamePlatformFacet, 0)
	for rows.Next() {
		var facet domain.GamePlatformFacet
		if err := rows.Scan(&facet.Platform, &facet.ROMSetName, &facet.Format, &facet.EmulatorHint, &facet.Count); err != nil {
			return domain.GameListFacets{}, err
		}
		facets = append(facets, facet)
	}
	if err := rows.Err(); err != nil {
		return domain.GameListFacets{}, err
	}
	return domain.GameListFacets{Total: total, Platforms: facets}, nil
}

func (s *Store) GamePrivateStateForProfile(gameID int64, profileID int64) (domain.GamePrivateState, error) {
	profileID, err := s.ResolveProfileID(profileID)
	if err != nil {
		return domain.GamePrivateState{}, err
	}
	var favorite int
	var liked int
	err = s.db.QueryRow(`SELECT favorite, liked FROM game_private_states WHERE profile_id = ? AND game_id = ?`, profileID, gameID).Scan(&favorite, &liked)
	if err == sql.ErrNoRows {
		return domain.GamePrivateState{}, nil
	}
	if err != nil {
		return domain.GamePrivateState{}, err
	}
	return domain.GamePrivateState{Favorite: favorite != 0, Liked: liked != 0}, nil
}

func (s *Store) UpdateGamePrivateStateForProfile(gameID int64, profileID int64, state domain.GamePrivateState) error {
	profileID, err := s.ResolveProfileID(profileID)
	if err != nil {
		return err
	}
	if _, err := scanGame(s.db.QueryRow(gameSelectSQL()+` WHERE id = ?`, gameID)); err != nil {
		return err
	}
	favorite := 0
	if state.Favorite {
		favorite = 1
	}
	liked := 0
	if state.Liked {
		liked = 1
	}
	_, err = s.db.Exec(`INSERT INTO game_private_states(profile_id, game_id, favorite, liked)
		VALUES(?, ?, ?, ?)
		ON CONFLICT(profile_id, game_id) DO UPDATE SET favorite = excluded.favorite,
			liked = excluded.liked,
			updated_at = CURRENT_TIMESTAMP`, profileID, gameID, favorite, liked)
	return err
}

func (s *Store) applyGamePrivateStates(profileID int64, items []domain.GameAsset) ([]domain.GameAsset, error) {
	if len(items) == 0 {
		return items, nil
	}
	profileID, err := s.ResolveProfileID(profileID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT game_id, favorite, liked FROM game_private_states WHERE profile_id = ?`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type state struct {
		favorite bool
		liked    bool
	}
	states := make(map[int64]state)
	for rows.Next() {
		var gameID int64
		var favorite int
		var liked int
		if err := rows.Scan(&gameID, &favorite, &liked); err != nil {
			return nil, err
		}
		states[gameID] = state{favorite: favorite != 0, liked: liked != 0}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range items {
		if itemState, ok := states[items[i].ID]; ok {
			items[i].Favorite = itemState.favorite
			items[i].Liked = itemState.liked
		}
	}
	return items, nil
}

func (s *Store) ListGamesByROMSet(romSetName string) ([]domain.GameAsset, error) {
	rows, err := s.db.Query(gameSelectSQL()+` WHERE rom_set_name = ? ORDER BY platform, title`, strings.TrimSpace(romSetName))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanGames(rows)
}

func (s *Store) ListGamesByPlatform(platform string) ([]domain.GameAsset, error) {
	rows, err := s.db.Query(gameSelectSQL()+` WHERE platform = ? ORDER BY title`, strings.TrimSpace(platform))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanGames(rows)
}

func (s *Store) UpsertGameMetadata(metadata domain.GameMetadata) error {
	_, err := s.db.Exec(`INSERT INTO game_metadata(game_id, display_title, summary, release_date, genres, developers, publishers, players, rating, external_links)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(game_id) DO UPDATE SET
			display_title = excluded.display_title,
			summary = excluded.summary,
			release_date = excluded.release_date,
			genres = excluded.genres,
			developers = excluded.developers,
			publishers = excluded.publishers,
			players = excluded.players,
			rating = excluded.rating,
			external_links = excluded.external_links,
			updated_at = CURRENT_TIMESTAMP`,
		metadata.GameID,
		strings.TrimSpace(metadata.DisplayTitle),
		strings.TrimSpace(metadata.Summary),
		strings.TrimSpace(metadata.ReleaseDate),
		encodeStringList(metadata.Genres),
		encodeStringList(metadata.Developers),
		encodeStringList(metadata.Publishers),
		strings.TrimSpace(metadata.Players),
		metadata.Rating,
		encodeStringList(metadata.ExternalLinks))
	return err
}

func (s *Store) UpsertGameMetadataSource(source domain.GameMetadataSource) (domain.GameMetadataSource, error) {
	source.Source = strings.TrimSpace(source.Source)
	source.SourceID = strings.TrimSpace(source.SourceID)
	source.MatchedBy = strings.TrimSpace(source.MatchedBy)
	_, err := s.db.Exec(`INSERT INTO game_metadata_sources(game_id, source, source_id, matched_by, confidence, raw_json)
		VALUES(?, ?, ?, ?, ?, ?)
		ON CONFLICT(game_id, source, source_id) DO UPDATE SET
			matched_by = excluded.matched_by,
			confidence = excluded.confidence,
			raw_json = excluded.raw_json,
			updated_at = CURRENT_TIMESTAMP`,
		source.GameID, source.Source, source.SourceID, source.MatchedBy, source.Confidence, strings.TrimSpace(source.RawJSON))
	if err != nil {
		return domain.GameMetadataSource{}, err
	}
	row := s.db.QueryRow(`SELECT id, game_id, source, source_id, matched_by, confidence, raw_json, created_at, updated_at
		FROM game_metadata_sources WHERE game_id = ? AND source = ? AND source_id = ?`, source.GameID, source.Source, source.SourceID)
	return scanGameMetadataSource(row)
}

func (s *Store) UpsertGameArtwork(artwork domain.GameArtwork) (domain.GameArtwork, error) {
	artwork.Source = strings.TrimSpace(artwork.Source)
	artwork.Kind = strings.TrimSpace(artwork.Kind)
	artwork.URL = strings.TrimSpace(artwork.URL)
	artwork.CachePath = strings.TrimSpace(artwork.CachePath)
	_, err := s.db.Exec(`INSERT INTO game_artwork(game_id, source, kind, url, cache_path, width, height, selected, confidence)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(game_id, source, kind, url, cache_path) DO UPDATE SET
			width = excluded.width,
			height = excluded.height,
			selected = excluded.selected,
			confidence = excluded.confidence,
			updated_at = CURRENT_TIMESTAMP`,
		artwork.GameID, artwork.Source, artwork.Kind, artwork.URL, artwork.CachePath, artwork.Width, artwork.Height, boolInt(artwork.Selected), artwork.Confidence)
	if err != nil {
		return domain.GameArtwork{}, err
	}
	row := s.db.QueryRow(`SELECT id, game_id, source, kind, url, cache_path, width, height, selected, confidence, created_at, updated_at
		FROM game_artwork WHERE game_id = ? AND source = ? AND kind = ? AND url = ? AND cache_path = ?`,
		artwork.GameID, artwork.Source, artwork.Kind, artwork.URL, artwork.CachePath)
	return scanGameArtwork(row)
}

func (s *Store) GameDetails(id int64) (domain.GameDetails, error) {
	game, err := s.GameByID(id)
	if err != nil {
		return domain.GameDetails{}, err
	}
	metadata, hasMetadata, err := s.gameMetadata(id)
	if err != nil {
		return domain.GameDetails{}, err
	}
	sources, err := s.gameMetadataSources(id)
	if err != nil {
		return domain.GameDetails{}, err
	}
	artwork, err := s.gameArtwork(id)
	if err != nil {
		return domain.GameDetails{}, err
	}
	status := "unmatched"
	if hasMetadata || len(sources) > 0 {
		status = "matched"
	}
	if metadata.GameID == 0 {
		metadata.GameID = id
	}
	return domain.GameDetails{
		Game:           game,
		MetadataStatus: status,
		Metadata:       metadata,
		Sources:        sources,
		Artwork:        artwork,
	}, nil
}

func (s *Store) gameMetadata(gameID int64) (domain.GameMetadata, bool, error) {
	row := s.db.QueryRow(`SELECT game_id, display_title, summary, release_date, genres, developers, publishers, players, rating, external_links, updated_at
		FROM game_metadata WHERE game_id = ?`, gameID)
	metadata, err := scanGameMetadata(row)
	if err == sql.ErrNoRows {
		return emptyGameMetadata(gameID), false, nil
	}
	return metadata, err == nil, err
}

func (s *Store) gameMetadataSources(gameID int64) ([]domain.GameMetadataSource, error) {
	rows, err := s.db.Query(`SELECT id, game_id, source, source_id, matched_by, confidence, raw_json, created_at, updated_at
		FROM game_metadata_sources WHERE game_id = ? ORDER BY source, id`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.GameMetadataSource{}
	for rows.Next() {
		source, err := scanGameMetadataSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, source)
	}
	return out, rows.Err()
}

func (s *Store) gameArtwork(gameID int64) ([]domain.GameArtwork, error) {
	rows, err := s.db.Query(`SELECT id, game_id, source, kind, url, cache_path, width, height, selected, confidence, created_at, updated_at
		FROM game_artwork WHERE game_id = ? ORDER BY selected DESC, kind, source, id`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.GameArtwork{}
	for rows.Next() {
		artwork, err := scanGameArtwork(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, artwork)
	}
	return out, rows.Err()
}

func (s *Store) UpsertVideo(video domain.VideoAsset) (domain.VideoAsset, error) {
	video.Format = strings.TrimSpace(video.Format)
	video.VideoCodec = strings.TrimSpace(video.VideoCodec)
	video.AudioCodec = strings.TrimSpace(video.AudioCodec)
	if strings.TrimSpace(video.ThumbnailStatus) == "" {
		video.ThumbnailStatus = "placeholder"
	}
	_, err := s.db.Exec(`INSERT INTO videos(library_id, title, format, file_path, rel_path, size, mtime, duration_seconds, width, height, video_codec, audio_codec, thumbnail_status)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(file_path) DO UPDATE SET library_id = excluded.library_id,
			title = excluded.title,
			format = excluded.format,
			rel_path = excluded.rel_path,
			size = excluded.size,
			mtime = excluded.mtime,
			duration_seconds = excluded.duration_seconds,
			width = excluded.width,
			height = excluded.height,
			video_codec = excluded.video_codec,
			audio_codec = excluded.audio_codec,
			thumbnail_status = excluded.thumbnail_status,
			updated_at = CURRENT_TIMESTAMP`,
		video.LibraryID, video.Title, video.Format, video.FilePath, video.RelPath, video.Size, video.MTime.Format(time.RFC3339Nano), video.DurationSeconds, video.Width, video.Height, video.VideoCodec, video.AudioCodec, video.ThumbnailStatus)
	if err != nil {
		return domain.VideoAsset{}, err
	}
	return s.VideoByPath(video.FilePath)
}

func (s *Store) VideoByID(id int64) (domain.VideoAsset, error) {
	row := s.db.QueryRow(videoSelectSQL()+` WHERE id = ?`, id)
	return scanVideo(row)
}

func (s *Store) VideoByPath(filePath string) (domain.VideoAsset, error) {
	row := s.db.QueryRow(videoSelectSQL()+` WHERE file_path = ?`, filePath)
	return scanVideo(row)
}

func (s *Store) DeleteVideoByPath(filePath string) error {
	_, err := s.db.Exec(`DELETE FROM videos WHERE file_path = ?`, filePath)
	return err
}

func (s *Store) CanSkipVideo(path string, size int64, mtime time.Time) bool {
	video, err := s.VideoByPath(path)
	if err != nil {
		return false
	}
	return video.Size == size && video.MTime.Equal(mtime)
}

func (s *Store) ListRecentVideos(limit int) ([]domain.VideoAsset, error) {
	limit = normalizeShelfLimit(limit)
	rows, err := s.db.Query(videoSelectSQL()+` ORDER BY updated_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanVideos(rows)
}

func (s *Store) ListVideosPage(options domain.VideoListOptions) (domain.VideoListPage, error) {
	limit := options.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := options.Offset
	if offset < 0 {
		offset = 0
	}

	where, args := videoListWhere(options)
	var total int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM videos`+where, args...).Scan(&total); err != nil {
		return domain.VideoListPage{}, err
	}
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, limit, offset)
	rows, err := s.db.Query(videoSelectSQL()+where+videoListOrderBy(options.Sort)+` LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return domain.VideoListPage{}, err
	}
	defer rows.Close()
	items, err := scanVideos(rows)
	if err != nil {
		return domain.VideoListPage{}, err
	}
	return domain.VideoListPage{
		Items:   items,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
		HasMore: int64(offset+len(items)) < total,
	}, nil
}

func gameListWhere(options domain.GameListOptions) (string, []any) {
	clauses := make([]string, 0, 3)
	args := make([]any, 0, 8)
	if query := strings.TrimSpace(options.Query); query != "" {
		like := "%" + strings.ToLower(query) + "%"
		clauses = append(clauses, `(LOWER(title) LIKE ? OR LOWER(rom_set_name) LIKE ? OR LOWER(region) LIKE ? OR LOWER(platform) LIKE ? OR LOWER(format) LIKE ?)`)
		args = append(args, like, like, like, like, like)
	}
	if platform := strings.TrimSpace(options.Platform); platform != "" {
		platforms := splitFilterValues(platform)
		if len(platforms) == 1 {
			clauses = append(clauses, `LOWER(platform) = LOWER(?)`)
			args = append(args, platforms[0])
		} else if len(platforms) > 1 {
			placeholders := strings.TrimRight(strings.Repeat("LOWER(?),", len(platforms)), ",")
			clauses = append(clauses, `LOWER(platform) IN (`+placeholders+`)`)
			for _, value := range platforms {
				args = append(args, value)
			}
		}
	}
	if romSetName := strings.TrimSpace(options.ROMSetName); romSetName != "" {
		clauses = append(clauses, `LOWER(rom_set_name) = LOWER(?)`)
		args = append(args, romSetName)
	}
	if format := strings.TrimSpace(options.Format); format != "" {
		clauses = append(clauses, `LOWER(format) = LOWER(?)`)
		args = append(args, format)
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func splitFilterValues(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func videoListWhere(options domain.VideoListOptions) (string, []any) {
	clauses := make([]string, 0, 2)
	args := make([]any, 0, 5)
	if query := strings.TrimSpace(options.Query); query != "" {
		like := "%" + strings.ToLower(query) + "%"
		clauses = append(clauses, `(LOWER(title) LIKE ? OR LOWER(rel_path) LIKE ? OR LOWER(format) LIKE ?)`)
		args = append(args, like, like, like)
	}
	if format := strings.TrimSpace(options.Format); format != "" {
		clauses = append(clauses, `LOWER(format) = LOWER(?)`)
		args = append(args, format)
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func videoListOrderBy(sort string) string {
	switch strings.ToLower(strings.TrimSpace(sort)) {
	case "title":
		return ` ORDER BY LOWER(title), id`
	default:
		return ` ORDER BY updated_at DESC, id DESC`
	}
}

func gameListOrderBy(sort string) string {
	switch strings.ToLower(strings.TrimSpace(sort)) {
	case "title":
		return ` ORDER BY LOWER(title), platform, id`
	case "platform":
		return ` ORDER BY LOWER(platform), LOWER(title), id`
	case "oldest":
		return ` ORDER BY updated_at ASC, id ASC`
	default:
		return ` ORDER BY updated_at DESC, id DESC`
	}
}

func GamePlatformCollectionID(platform string) int64 {
	return -1000 - int64(GamePlatformSortRank(platform))
}

func PlatformFromGamePlatformCollectionID(id int64) string {
	switch id {
	case GamePlatformCollectionID("nes"):
		return "nes"
	case GamePlatformCollectionID("snes"):
		return "snes"
	case GamePlatformCollectionID("gb"):
		return "gb"
	case GamePlatformCollectionID("gbc"):
		return "gbc"
	case GamePlatformCollectionID("gba"):
		return "gba"
	case GamePlatformCollectionID("md"):
		return "md"
	case GamePlatformCollectionID("neogeo"):
		return "neogeo"
	case GamePlatformCollectionID("32x"):
		return "32x"
	case GamePlatformCollectionID("model3"):
		return "model3"
	case GamePlatformCollectionID("naomi"):
		return "naomi"
	case GamePlatformCollectionID("saturn"):
		return "saturn"
	case GamePlatformCollectionID("arcade"):
		return "arcade"
	default:
		return ""
	}
}

func GamePlatformSortRank(platform string) int {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "nes":
		return 10
	case "snes":
		return 20
	case "gb":
		return 30
	case "gbc":
		return 40
	case "gba":
		return 50
	case "md", "genesis", "mega-drive", "megadrive":
		return 60
	case "32x":
		return 65
	case "saturn":
		return 70
	case "neogeo":
		return 80
	case "model3":
		return 85
	case "naomi":
		return 86
	case "arcade":
		return 90
	default:
		return 999
	}
}

func GamePlatformLabel(platform string) string {
	value := strings.ToLower(strings.TrimSpace(platform))
	switch value {
	case "nes", "snes", "gb", "gbc", "gba":
		return strings.ToUpper(value)
	case "md":
		return "Mega Drive"
	case "genesis", "mega-drive", "megadrive":
		return "Mega Drive"
	case "32x":
		return "32X"
	case "neogeo":
		return "Neo Geo"
	case "model3":
		return "Model 3"
	case "naomi":
		return "NAOMI"
	case "saturn":
		return "Saturn"
	case "arcade":
		return "Arcade"
	default:
		if value == "" {
			return "Unknown"
		}
		return strings.ToUpper(value[:1]) + value[1:]
	}
}

func platformFromGameCollectionTitle(title string) string {
	return strings.ToLower(strings.TrimPrefix(title, "Games / "))
}

func (s *Store) CanSkipGame(path string, size int64, mtime time.Time, platform string) bool {
	game, err := s.GameByPath(path)
	if err != nil {
		return false
	}
	return game.Size == size &&
		game.MTime.Equal(mtime) &&
		game.Platform == platform &&
		game.EmulatorHint == platform &&
		game.CRC32 != "" &&
		game.SHA1 != ""
}

func scanBooks(rows *sql.Rows) ([]domain.Book, error) {
	out := make([]domain.Book, 0)
	for rows.Next() {
		book, err := scanBook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, book)
	}
	return out, rows.Err()
}

func (s *Store) UpsertFile(bookID int64, libraryID int64, absPath string, relPath string, size int64, mtime time.Time, ext string) (domain.File, error) {
	_, err := s.db.Exec(`INSERT INTO files(book_id, library_id, abs_path, rel_path, size, mtime, ext) VALUES(?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(abs_path) DO UPDATE SET book_id = excluded.book_id, library_id = excluded.library_id, rel_path = excluded.rel_path, size = excluded.size, mtime = excluded.mtime, ext = excluded.ext, updated_at = CURRENT_TIMESTAMP`,
		bookID, libraryID, absPath, relPath, size, mtime.Format(time.RFC3339Nano), ext)
	if err != nil {
		return domain.File{}, err
	}
	row := s.db.QueryRow(`SELECT id, book_id, library_id, abs_path, rel_path, size, mtime, ext FROM files WHERE abs_path = ?`, absPath)
	return scanFile(row)
}

func (s *Store) UpsertBasicBookFile(libraryID int64, seriesTitle string, directoryPath string, title string, format string, absPath string, relPath string, size int64, mtime time.Time, ext string) (domain.Book, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return domain.Book{}, err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`INSERT INTO series(library_id, title, directory_path, collection_type) VALUES(?, ?, ?, 'directory')
		ON CONFLICT(library_id, title) DO UPDATE SET directory_path = excluded.directory_path, collection_type = 'directory', updated_at = CURRENT_TIMESTAMP`,
		libraryID, seriesTitle, directoryPath); err != nil {
		return domain.Book{}, err
	}

	var seriesID int64
	if err := tx.QueryRow(`SELECT id FROM series WHERE library_id = ? AND title = ?`, libraryID, seriesTitle).Scan(&seriesID); err != nil {
		return domain.Book{}, err
	}

	if _, err := tx.Exec(`INSERT INTO books(series_id, title, format) VALUES(?, ?, ?)
		ON CONFLICT(series_id, title, format) DO UPDATE SET updated_at = CURRENT_TIMESTAMP`, seriesID, title, format); err != nil {
		return domain.Book{}, err
	}

	var bookID int64
	if err := tx.QueryRow(`SELECT id FROM books WHERE series_id = ? AND title = ? AND format = ?`, seriesID, title, format).Scan(&bookID); err != nil {
		return domain.Book{}, err
	}

	if _, err := tx.Exec(`INSERT INTO files(book_id, library_id, abs_path, rel_path, size, mtime, ext) VALUES(?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(abs_path) DO UPDATE SET book_id = excluded.book_id, library_id = excluded.library_id, rel_path = excluded.rel_path, size = excluded.size, mtime = excluded.mtime, ext = excluded.ext, updated_at = CURRENT_TIMESTAMP`,
		bookID, libraryID, absPath, relPath, size, mtime.Format(time.RFC3339Nano), ext); err != nil {
		return domain.Book{}, err
	}

	if err := tx.Commit(); err != nil {
		return domain.Book{}, err
	}
	return s.BookByID(bookID)
}

type FileIndex struct {
	File      domain.File
	Book      domain.Book
	Analyzed  bool
	PageCount int
}

type ScanDirectoryIndex struct {
	AbsPath    string
	MTime      time.Time
	HasSubdirs bool
}

func (s *Store) FileIndexByPath(absPath string) (FileIndex, error) {
	row := s.db.QueryRow(`SELECT f.id, f.book_id, f.library_id, f.abs_path, f.rel_path, f.size, f.mtime, f.ext,
				b.id, b.series_id, s.title, b.title, b.creator, b.description, b.format, b.analyzed, b.page_count
			FROM files f JOIN books b ON b.id = f.book_id
			JOIN series s ON s.id = b.series_id
			WHERE f.abs_path = ?`, absPath)
	return scanFileIndex(row)
}

func (s *Store) ListFileIndexesByLibrary(libraryID int64) (map[string]FileIndex, error) {
	rows, err := s.db.Query(`SELECT f.id, f.book_id, f.library_id, f.abs_path, f.rel_path, f.size, f.mtime, f.ext,
				b.id, b.series_id, s.title, b.title, b.creator, b.description, b.format, b.analyzed, b.page_count
			FROM files f JOIN books b ON b.id = f.book_id
			JOIN series s ON s.id = b.series_id
			WHERE f.library_id = ?`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	indexes := map[string]FileIndex{}
	for rows.Next() {
		item, err := scanFileIndex(rows)
		if err != nil {
			return nil, err
		}
		indexes[item.File.AbsPath] = item
	}
	return indexes, rows.Err()
}

func scanFileIndex(row scanner) (FileIndex, error) {
	var item FileIndex
	var mtime string
	var analyzed int
	if err := row.Scan(
		&item.File.ID,
		&item.File.BookID,
		&item.File.LibraryID,
		&item.File.AbsPath,
		&item.File.RelPath,
		&item.File.Size,
		&mtime,
		&item.File.Ext,
		&item.Book.ID,
		&item.Book.SeriesID,
		&item.Book.CollectionTitle,
		&item.Book.Title,
		&item.Book.Creator,
		&item.Book.Description,
		&item.Book.Format,
		&analyzed,
		&item.PageCount,
	); err != nil {
		return item, err
	}
	item.File.MTime = parseTime(mtime)
	item.Analyzed = analyzed != 0
	item.Book.Analyzed = item.Analyzed
	item.Book.PageCount = item.PageCount
	item.Book.FilePath = item.File.AbsPath
	return item, nil
}

func (s *Store) ReplacePages(bookID int64, pages []domain.Page) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM pages WHERE book_id = ?`, bookID); err != nil {
		_ = tx.Rollback()
		return err
	}
	for _, page := range pages {
		if _, err := tx.Exec(`INSERT INTO pages(book_id, page_index, entry_name) VALUES(?, ?, ?)`, bookID, page.Index, page.Name); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	_, err = tx.Exec(`UPDATE books SET page_count = ?, analyzed = 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, len(pages), bookID)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) ListPages(bookID int64) ([]domain.Page, error) {
	rows, err := s.db.Query(`SELECT page_index, entry_name FROM pages WHERE book_id = ? ORDER BY page_index`, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Page, 0)
	for rows.Next() {
		var page domain.Page
		if err := rows.Scan(&page.Index, &page.Name); err != nil {
			return nil, err
		}
		page.PageKey = stablePageKey(page.Index, page.Name)
		out = append(out, page)
	}
	return out, rows.Err()
}

func stablePageKey(index int, name string) string {
	name = strings.TrimSpace(name)
	if name != "" {
		return "archive:" + name
	}
	return fmt.Sprintf("index:%d", index)
}

func (s *Store) StartScanJob(libraryID int64) (domain.ScanJob, error) {
	return s.StartScanJobWithTarget(libraryID, "")
}

func (s *Store) StartScanJobWithTarget(libraryID int64, targetPath string) (domain.ScanJob, error) {
	res, err := s.db.Exec(`INSERT INTO scan_jobs(library_id, status, target_path) VALUES(?, 'running', ?)`, libraryID, targetPath)
	if err != nil {
		return domain.ScanJob{}, err
	}
	id, _ := res.LastInsertId()
	return s.ScanJobByID(id)
}

func (s *Store) UpdateScanJob(job domain.ScanJob) error {
	_, err := s.db.Exec(`UPDATE scan_jobs SET status = ?, current_path = ?, discovered_files = ?, indexed_files = ?, skipped_files = ?, error_count = ?, metadata_updated_files = ?, reclassified_files = ?, finished_at = ? WHERE id = ?`,
		job.Status, job.CurrentPath, job.DiscoveredFiles, job.IndexedFiles, job.SkippedFiles, job.ErrorCount, job.MetadataUpdatedFiles, job.ReclassifiedFiles, formatOptionalTime(job.FinishedAt), job.ID)
	return err
}

func (s *Store) RequestScanJobPause(id int64) (domain.ScanJob, error) {
	_, err := s.db.Exec(`UPDATE scan_jobs SET status = 'pause_requested' WHERE id = ? AND status = 'running'`, id)
	if err != nil {
		return domain.ScanJob{}, err
	}
	return s.ScanJobByID(id)
}

func (s *Store) RequestScanJobCancel(id int64) (domain.ScanJob, error) {
	_, err := s.db.Exec(`UPDATE scan_jobs SET status = 'cancel_requested' WHERE id = ? AND status IN ('running', 'pause_requested', 'paused')`, id)
	if err != nil {
		return domain.ScanJob{}, err
	}
	return s.ScanJobByID(id)
}

func (s *Store) CancelInterruptedScanJobs() (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`SELECT id FROM scan_jobs WHERE status IN ('running', 'pause_requested', 'cancel_requested')`)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, tx.Commit()
	}

	finishedAt := time.Now().UTC().Format(time.RFC3339Nano)
	for _, id := range ids {
		if _, err := tx.Exec(`UPDATE scan_jobs SET status = 'cancelled', finished_at = ? WHERE id = ?`, finishedAt, id); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(`INSERT INTO job_events(job_id, level, message) VALUES(?, 'warn', 'marked cancelled after service restart')`, id); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int64(len(ids)), nil
}

func (s *Store) ScanJobByID(id int64) (domain.ScanJob, error) {
	row := s.db.QueryRow(`SELECT id, library_id, status, target_path, current_path, discovered_files, indexed_files, skipped_files, error_count, metadata_updated_files, reclassified_files, started_at, finished_at FROM scan_jobs WHERE id = ?`, id)
	return scanJob(row)
}

func (s *Store) ListScanJobs() ([]domain.ScanJob, error) {
	rows, err := s.db.Query(`SELECT id, library_id, status, target_path, current_path, discovered_files, indexed_files, skipped_files, error_count, metadata_updated_files, reclassified_files, started_at, finished_at FROM scan_jobs ORDER BY id DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.ScanJob, 0)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func (s *Store) RunningScanJobByLibraryTarget(libraryID int64, targetPath string) (domain.ScanJob, error) {
	row := s.db.QueryRow(`SELECT id, library_id, status, target_path, current_path, discovered_files, indexed_files, skipped_files, error_count, metadata_updated_files, reclassified_files, started_at, finished_at
		FROM scan_jobs
		WHERE library_id = ? AND target_path = ? AND status IN ('running', 'pause_requested')
		ORDER BY id DESC LIMIT 1`, libraryID, targetPath)
	return scanJob(row)
}

func (s *Store) UpsertScanDirectory(libraryID int64, absPath string, mtime time.Time, hasSubdirs bool) error {
	hasSubdirValue := 0
	if hasSubdirs {
		hasSubdirValue = 1
	}
	_, err := s.db.Exec(`INSERT INTO scan_directories(library_id, abs_path, mtime, has_subdirs) VALUES(?, ?, ?, ?)
		ON CONFLICT(library_id, abs_path) DO UPDATE SET mtime = excluded.mtime, has_subdirs = excluded.has_subdirs, updated_at = CURRENT_TIMESTAMP`,
		libraryID, absPath, mtime.Format(time.RFC3339Nano), hasSubdirValue)
	return err
}

func (s *Store) ListScanDirectoriesByLibrary(libraryID int64) (map[string]ScanDirectoryIndex, error) {
	rows, err := s.db.Query(`SELECT abs_path, mtime, has_subdirs FROM scan_directories WHERE library_id = ?`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]ScanDirectoryIndex{}
	for rows.Next() {
		var item ScanDirectoryIndex
		var mtime string
		var hasSubdirs int
		if err := rows.Scan(&item.AbsPath, &mtime, &hasSubdirs); err != nil {
			return nil, err
		}
		item.MTime = parseTime(mtime)
		item.HasSubdirs = hasSubdirs != 0
		out[item.AbsPath] = item
	}
	return out, rows.Err()
}

func (s *Store) EnqueueThumbnailJob(input domain.ThumbnailJobInput) (domain.ThumbnailJob, error) {
	size := normalizeThumbnailSize(input.Size)
	cacheKey := strings.TrimSpace(input.CacheKey)
	if cacheKey == "" {
		return domain.ThumbnailJob{}, fmt.Errorf("thumbnail cache key is required")
	}
	priority := input.Priority
	_, err := s.db.Exec(`INSERT INTO thumbnail_jobs(book_id, size, status, priority, cache_key)
		VALUES(?, ?, 'queued', ?, ?)
		ON CONFLICT(book_id, size, cache_key) DO UPDATE SET
			priority = CASE WHEN excluded.priority > thumbnail_jobs.priority THEN excluded.priority ELSE thumbnail_jobs.priority END,
			status = CASE WHEN thumbnail_jobs.status = 'running' THEN thumbnail_jobs.status ELSE 'queued' END,
			cache_path = CASE WHEN thumbnail_jobs.status = 'running' THEN thumbnail_jobs.cache_path ELSE '' END,
			content_type = CASE WHEN thumbnail_jobs.status = 'running' THEN thumbnail_jobs.content_type ELSE '' END,
			width = CASE WHEN thumbnail_jobs.status = 'running' THEN thumbnail_jobs.width ELSE 0 END,
			height = CASE WHEN thumbnail_jobs.status = 'running' THEN thumbnail_jobs.height ELSE 0 END,
			byte_size = CASE WHEN thumbnail_jobs.status = 'running' THEN thumbnail_jobs.byte_size ELSE 0 END,
			error_message = CASE WHEN thumbnail_jobs.status = 'running' THEN thumbnail_jobs.error_message ELSE '' END,
			started_at = CASE WHEN thumbnail_jobs.status = 'running' THEN thumbnail_jobs.started_at ELSE '' END,
			finished_at = CASE WHEN thumbnail_jobs.status = 'running' THEN thumbnail_jobs.finished_at ELSE '' END,
			updated_at = CURRENT_TIMESTAMP`,
		input.BookID, size, priority, cacheKey)
	if err != nil {
		return domain.ThumbnailJob{}, err
	}
	return s.ThumbnailJobByKey(input.BookID, size, cacheKey)
}

func (s *Store) ThumbnailJobByKey(bookID int64, size string, cacheKey string) (domain.ThumbnailJob, error) {
	row := s.db.QueryRow(thumbnailJobSelectSQL()+` WHERE tj.book_id = ? AND tj.size = ? AND tj.cache_key = ?`, bookID, normalizeThumbnailSize(size), strings.TrimSpace(cacheKey))
	return scanThumbnailJob(row)
}

func (s *Store) ThumbnailJobByID(id int64) (domain.ThumbnailJob, error) {
	row := s.db.QueryRow(thumbnailJobSelectSQL()+` WHERE tj.id = ?`, id)
	return scanThumbnailJob(row)
}

func (s *Store) ClaimNextThumbnailJob() (domain.ThumbnailJob, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return domain.ThumbnailJob{}, false, err
	}
	defer tx.Rollback()

	var id int64
	err = tx.QueryRow(`SELECT id FROM thumbnail_jobs WHERE status = 'queued' ORDER BY priority DESC, id LIMIT 1`).Scan(&id)
	if err == sql.ErrNoRows {
		return domain.ThumbnailJob{}, false, tx.Commit()
	}
	if err != nil {
		return domain.ThumbnailJob{}, false, err
	}
	if _, err := tx.Exec(`UPDATE thumbnail_jobs SET status = 'running', started_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, id); err != nil {
		return domain.ThumbnailJob{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return domain.ThumbnailJob{}, false, err
	}
	job, err := s.ThumbnailJobByID(id)
	return job, true, err
}

func (s *Store) CompleteThumbnailJob(id int64, cachePath string, contentType string, width int, height int, byteSize int64) error {
	_, err := s.db.Exec(`UPDATE thumbnail_jobs
		SET status = 'ready', cache_path = ?, content_type = ?, width = ?, height = ?, byte_size = ?, error_message = '', finished_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		cachePath, contentType, width, height, byteSize, id)
	return err
}

func (s *Store) FailThumbnailJob(id int64, message string) error {
	_, err := s.db.Exec(`UPDATE thumbnail_jobs
		SET status = 'failed', error_message = ?, finished_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		strings.TrimSpace(message), id)
	return err
}

func (s *Store) CancelQueuedThumbnailJobs() (int64, error) {
	result, err := s.db.Exec(`UPDATE thumbnail_jobs
		SET status = 'cancelled', finished_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE status = 'queued'`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) ResetRunningThumbnailJobs() (int64, error) {
	result, err := s.db.Exec(`UPDATE thumbnail_jobs
		SET status = 'queued', started_at = '', updated_at = CURRENT_TIMESTAMP
		WHERE status = 'running'`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) ListReadyThumbnailCacheEntries() ([]domain.ThumbnailCacheEntry, error) {
	rows, err := s.db.Query(`SELECT book_id, size, cache_key, cache_path, byte_size FROM thumbnail_jobs WHERE status = 'ready' AND cache_path <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.ThumbnailCacheEntry, 0)
	for rows.Next() {
		var entry domain.ThumbnailCacheEntry
		if err := rows.Scan(&entry.BookID, &entry.Size, &entry.CacheKey, &entry.CachePath, &entry.ByteSize); err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

func (s *Store) ThumbnailQueueStatus() (domain.ThumbnailQueueStatus, error) {
	status := domain.ThumbnailQueueStatus{Status: "idle"}
	rows, err := s.db.Query(`SELECT status, COUNT(*) FROM thumbnail_jobs GROUP BY status`)
	if err != nil {
		return status, err
	}
	for rows.Next() {
		var state string
		var count int
		if err := rows.Scan(&state, &count); err != nil {
			_ = rows.Close()
			return status, err
		}
		switch state {
		case "queued":
			status.Queued = count
		case "running":
			status.Running = count
		case "ready":
			status.Ready = count
		case "failed":
			status.Failed = count
		case "cancelled":
			status.Cancelled = count
		}
	}
	if err := rows.Close(); err != nil {
		return status, err
	}
	if err := rows.Err(); err != nil {
		return status, err
	}
	status.Processed = status.Ready + status.Failed + status.Cancelled
	if status.Running > 0 {
		status.Status = "running"
		active, err := s.activeThumbnailJob()
		if err != nil && err != sql.ErrNoRows {
			return status, err
		}
		status.ActiveJob = &active
	} else if status.Queued > 0 {
		status.Status = "queued"
	}
	lastError, err := s.lastThumbnailError()
	if err != nil && err != sql.ErrNoRows {
		return status, err
	}
	status.LastError = lastError
	return status, nil
}

func (s *Store) activeThumbnailJob() (domain.ThumbnailJob, error) {
	row := s.db.QueryRow(thumbnailJobSelectSQL() + ` WHERE tj.status = 'running' ORDER BY tj.started_at DESC, tj.id DESC LIMIT 1`)
	return scanThumbnailJob(row)
}

func (s *Store) lastThumbnailError() (string, error) {
	row := s.db.QueryRow(`SELECT error_message FROM thumbnail_jobs WHERE status = 'failed' AND error_message <> '' ORDER BY finished_at DESC, id DESC LIMIT 1`)
	var message string
	if err := row.Scan(&message); err != nil {
		return "", err
	}
	return message, nil
}

func normalizeThumbnailSize(size string) string {
	switch strings.ToLower(strings.TrimSpace(size)) {
	case "medium":
		return "medium"
	default:
		return "small"
	}
}

func (s *Store) AddJobEvent(jobID int64, level string, message string) error {
	_, err := s.db.Exec(`INSERT INTO job_events(job_id, level, message) VALUES(?, ?, ?)`, jobID, level, message)
	return err
}

func (s *Store) ListJobEvents(jobID int64) ([]domain.JobEvent, error) {
	rows, err := s.db.Query(`SELECT id, job_id, level, message, created_at FROM job_events WHERE job_id = ? ORDER BY id`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.JobEvent, 0)
	for rows.Next() {
		var event domain.JobEvent
		var created string
		if err := rows.Scan(&event.ID, &event.JobID, &event.Level, &event.Message, &created); err != nil {
			return nil, err
		}
		event.CreatedAt = parseTime(created)
		out = append(out, event)
	}
	return out, rows.Err()
}

func (s *Store) SaveProgress(bookID int64, pageIndex int) error {
	return s.SaveProgressDetail(bookID, pageIndex, "", 0)
}

func (s *Store) SaveProgressDetail(bookID int64, pageIndex int, locator string, progressFraction float64) error {
	return s.SaveProgressDetailForProfile(bookID, defaultProfileID, pageIndex, locator, progressFraction)
}

func (s *Store) SaveProgressDetailForProfile(bookID int64, profileID int64, pageIndex int, locator string, progressFraction float64) error {
	profileID, err := s.ResolveProfileID(profileID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO profile_read_progress(profile_id, book_id, page_index, locator, progress_fraction) VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(profile_id, book_id) DO UPDATE SET page_index = excluded.page_index, locator = excluded.locator, progress_fraction = excluded.progress_fraction, updated_at = CURRENT_TIMESTAMP`,
		profileID, bookID, pageIndex, locator, progressFraction)
	if err != nil {
		return err
	}
	if profileID == defaultProfileID {
		_, err = s.db.Exec(`INSERT INTO read_progress(book_id, page_index, locator, progress_fraction) VALUES(?, ?, ?, ?)
			ON CONFLICT(book_id) DO UPDATE SET page_index = excluded.page_index, locator = excluded.locator, progress_fraction = excluded.progress_fraction, updated_at = CURRENT_TIMESTAMP`,
			bookID, pageIndex, locator, progressFraction)
	}
	return err
}

func (s *Store) SaveReadingPositionForProfile(bookID int64, profileID int64, readerMode string, position domain.ReadingPosition) (domain.ReadingPosition, error) {
	profileID, err := s.ResolveProfileID(profileID)
	if err != nil {
		return domain.ReadingPosition{}, err
	}
	readerMode = strings.TrimSpace(readerMode)
	position.BookID = bookID
	position.ReaderMode = readerMode
	position.Schema = strings.TrimSpace(position.Schema)
	position.PageKey = strings.TrimSpace(position.PageKey)
	position.ContentSignature = strings.TrimSpace(position.ContentSignature)
	position.PayloadJSON = strings.TrimSpace(position.PayloadJSON)
	if position.ViewportAnchorRatio == 0 {
		position.ViewportAnchorRatio = 0.28
	}
	_, err = s.db.Exec(`INSERT INTO profile_read_positions(
			profile_id, book_id, reader_mode, schema, page_index, page_key, page_y_offset_ratio,
			viewport_anchor_ratio, document_progress, page_count, content_signature, payload_json
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(profile_id, book_id, reader_mode) DO UPDATE SET
			schema = excluded.schema,
			page_index = excluded.page_index,
			page_key = excluded.page_key,
			page_y_offset_ratio = excluded.page_y_offset_ratio,
			viewport_anchor_ratio = excluded.viewport_anchor_ratio,
			document_progress = excluded.document_progress,
			page_count = excluded.page_count,
			content_signature = excluded.content_signature,
			payload_json = excluded.payload_json,
			updated_at = CURRENT_TIMESTAMP`,
		profileID, bookID, readerMode, position.Schema, position.PageIndex, position.PageKey, position.PageYOffsetRatio,
		position.ViewportAnchorRatio, position.DocumentProgress, position.PageCount, position.ContentSignature, position.PayloadJSON)
	if err != nil {
		return domain.ReadingPosition{}, err
	}
	if readerMode == "webtoon" {
		if err := s.SaveProgressDetailForProfile(bookID, profileID, position.PageIndex, webtoonLegacyLocator(position.DocumentProgress), position.DocumentProgress); err != nil {
			return domain.ReadingPosition{}, err
		}
	}
	return s.ReadingPositionForProfile(bookID, profileID, readerMode)
}

func webtoonLegacyLocator(documentProgress float64) string {
	return "webtoon:" + strconv.FormatFloat(documentProgress, 'f', -1, 64)
}

func (s *Store) ReadingPositionsForProfile(bookID int64, profileID int64) (map[string]domain.ReadingPosition, error) {
	profileID, err := s.ResolveProfileID(profileID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT book_id, reader_mode, schema, page_index, page_key, page_y_offset_ratio,
			viewport_anchor_ratio, document_progress, page_count, content_signature, payload_json, updated_at
		FROM profile_read_positions WHERE book_id = ? AND profile_id = ?`, bookID, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]domain.ReadingPosition)
	for rows.Next() {
		var position domain.ReadingPosition
		var updated string
		if err := rows.Scan(&position.BookID, &position.ReaderMode, &position.Schema, &position.PageIndex, &position.PageKey,
			&position.PageYOffsetRatio, &position.ViewportAnchorRatio, &position.DocumentProgress, &position.PageCount,
			&position.ContentSignature, &position.PayloadJSON, &updated); err != nil {
			return nil, err
		}
		position.UpdatedAt = parseTime(updated)
		out[position.ReaderMode] = position
	}
	return out, rows.Err()
}

func (s *Store) ReadingPositionForProfile(bookID int64, profileID int64, readerMode string) (domain.ReadingPosition, error) {
	positions, err := s.ReadingPositionsForProfile(bookID, profileID)
	if err != nil {
		return domain.ReadingPosition{}, err
	}
	position, ok := positions[strings.TrimSpace(readerMode)]
	if !ok {
		return domain.ReadingPosition{}, sql.ErrNoRows
	}
	return position, nil
}

func (s *Store) Progress(bookID int64) (domain.ReadProgress, error) {
	return s.ProgressForProfile(bookID, defaultProfileID)
}

func (s *Store) ProgressForProfile(bookID int64, profileID int64) (domain.ReadProgress, error) {
	profileID, err := s.ResolveProfileID(profileID)
	if err != nil {
		return domain.ReadProgress{}, err
	}
	row := s.db.QueryRow(`SELECT book_id, page_index, locator, progress_fraction, updated_at FROM profile_read_progress WHERE book_id = ? AND profile_id = ?`, bookID, profileID)
	var progress domain.ReadProgress
	var updated string
	if err := row.Scan(&progress.BookID, &progress.PageIndex, &progress.Locator, &progress.ProgressFraction, &updated); err != nil {
		return progress, err
	}
	progress.UpdatedAt = parseTime(updated)
	return progress, nil
}

func (s *Store) RecordFileError(input domain.FileErrorInput) error {
	_, err := s.db.Exec(`INSERT INTO file_errors(library_id, book_id, file_id, job_id, path, code, message) VALUES(?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(path, code) DO UPDATE SET message = excluded.message, job_id = excluded.job_id, last_seen = CURRENT_TIMESTAMP`,
		input.LibraryID, input.BookID, input.FileID, input.JobID, input.Path, string(input.Code), input.Message)
	return err
}

func (s *Store) ListFileErrors() ([]domain.FileError, error) {
	return s.ListFileErrorsByJob(0)
}

func (s *Store) ListFileErrorsByJob(jobID int64) ([]domain.FileError, error) {
	query := `SELECT id, library_id, book_id, file_id, job_id, path, code, message, first_seen, last_seen FROM file_errors`
	args := []any{}
	if jobID > 0 {
		query += ` WHERE job_id = ?`
		args = append(args, jobID)
	}
	query += ` ORDER BY last_seen DESC, id DESC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.FileError, 0)
	for rows.Next() {
		var item domain.FileError
		var code string
		var firstSeen string
		var lastSeen string
		if err := rows.Scan(&item.ID, &item.LibraryID, &item.BookID, &item.FileID, &item.JobID, &item.Path, &code, &item.Message, &firstSeen, &lastSeen); err != nil {
			return nil, err
		}
		item.Code = domain.ErrorCode(code)
		item.FirstSeen = parseTime(firstSeen)
		item.LastSeen = parseTime(lastSeen)
		out = append(out, item)
	}
	return out, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanLibrary(row scanner) (domain.Library, error) {
	var lib domain.Library
	var excludePatterns string
	var created string
	var updated string
	if err := row.Scan(&lib.ID, &lib.Name, &lib.RootPath, &lib.AssetType, &excludePatterns, &created, &updated); err != nil {
		return lib, err
	}
	lib.AssetType = normalizeLibraryAssetType(lib.AssetType)
	lib.ExcludePatterns = normalizeLibraryExcludePatterns(decodeStringList(excludePatterns))
	lib.CreatedAt = parseTime(created)
	lib.UpdatedAt = parseTime(updated)
	return lib, nil
}

func scanProfile(row scanner) (domain.Profile, error) {
	var profile domain.Profile
	var isDefault int
	var created string
	var updated string
	if err := row.Scan(&profile.ID, &profile.Name, &profile.Avatar, &profile.Color, &isDefault, &created, &updated); err != nil {
		return profile, err
	}
	profile.Avatar = normalizeProfileAvatar(profile.Avatar)
	profile.Color = normalizeProfileColor(profile.Color)
	profile.IsDefault = isDefault != 0
	profile.CreatedAt = parseTime(created)
	profile.UpdatedAt = parseTime(updated)
	return profile, nil
}

func normalizeProfileAvatar(value string) string {
	switch strings.TrimSpace(value) {
	case "reader", "comic", "game", "movie", "star", "archive", "coffee", "rocket":
		return strings.TrimSpace(value)
	default:
		return "reader"
	}
}

func normalizeProfileColor(value string) string {
	switch strings.TrimSpace(value) {
	case "teal", "amber", "violet", "rose", "blue", "green", "slate", "copper":
		return strings.TrimSpace(value)
	default:
		return "teal"
	}
}

func normalizeLibraryAssetType(value string) string {
	switch strings.TrimSpace(value) {
	case "book", "comic", "game", "video":
		return strings.TrimSpace(value)
	default:
		return "mixed"
	}
}

func normalizeLibraryExcludePatterns(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(filepathSlash(value))
		if value == "" || value == "." || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func filepathSlash(value string) string {
	return strings.Trim(strings.ReplaceAll(value, "\\", "/"), "/")
}

func encodeStringList(values []string) string {
	if len(values) == 0 {
		return ""
	}
	data, err := json.Marshal(values)
	if err != nil {
		return ""
	}
	return string(data)
}

func decodeStringList(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(value), &out); err == nil {
		return out
	}
	parts := strings.Split(value, ",")
	out = make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func nonNilStringList(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func scanSeries(row scanner) (domain.Series, error) {
	var series domain.Series
	var addedAt string
	if err := row.Scan(
		&series.ID,
		&series.LibraryID,
		&series.Title,
		&series.DirectoryPath,
		&series.CollectionType,
		&series.PrimaryType,
		&series.BookCount,
		&series.CoverBookID,
		&addedAt,
	); err != nil {
		return series, err
	}
	series.PrimaryType = normalizeCollectionPrimaryType(series.PrimaryType)
	series.AddedAt = parseTime(addedAt)
	return series, nil
}

func normalizeCollectionPrimaryType(value string) string {
	switch strings.TrimSpace(value) {
	case "book", "comic", "game", "video":
		return strings.TrimSpace(value)
	default:
		return "comic"
	}
}

func thumbnailJobSelectSQL() string {
	return `SELECT tj.id, tj.book_id, COALESCE(b.title, ''), tj.size, tj.status, tj.priority, tj.cache_key, tj.cache_path, tj.content_type,
			tj.width, tj.height, tj.byte_size, tj.error_message, tj.created_at, tj.updated_at, tj.started_at, tj.finished_at
		FROM thumbnail_jobs tj
		LEFT JOIN books b ON b.id = tj.book_id`
}

func scanThumbnailJob(row scanner) (domain.ThumbnailJob, error) {
	var job domain.ThumbnailJob
	var created string
	var updated string
	var started string
	var finished string
	if err := row.Scan(
		&job.ID,
		&job.BookID,
		&job.BookTitle,
		&job.Size,
		&job.Status,
		&job.Priority,
		&job.CacheKey,
		&job.CachePath,
		&job.ContentType,
		&job.Width,
		&job.Height,
		&job.ByteSize,
		&job.ErrorMessage,
		&created,
		&updated,
		&started,
		&finished,
	); err != nil {
		return job, err
	}
	job.CreatedAt = parseTime(created)
	job.UpdatedAt = parseTime(updated)
	job.StartedAt = parseTime(started)
	job.FinishedAt = parseTime(finished)
	return job, nil
}

func scanBook(row scanner) (domain.Book, error) {
	var book domain.Book
	var analyzed int
	var favorite int
	var addedAt string
	var updatedAt string
	var lastReadAt string
	var fileMTime string
	var tags string
	if err := row.Scan(
		&book.ID,
		&book.SeriesID,
		&book.CollectionTitle,
		&book.Title,
		&book.Creator,
		&book.Description,
		&book.Format,
		&book.PageCount,
		&book.CoverStatus,
		&analyzed,
		&book.FilePath,
		&book.FileSize,
		&fileMTime,
		&addedAt,
		&updatedAt,
		&book.CurrentPage,
		&book.ProgressFraction,
		&lastReadAt,
		&book.PrivateStatus,
		&favorite,
		&book.Rating,
		&tags,
		&book.Summary,
	); err != nil {
		return book, err
	}
	book.BookType = "single_volume"
	if book.ThumbnailStatus == "" {
		book.ThumbnailStatus = "pending"
	}
	book.Analyzed = analyzed != 0
	book.Favorite = favorite != 0
	book.Tags = decodeTags(tags)
	book.FileMTime = parseTime(fileMTime)
	book.AddedAt = parseTime(addedAt)
	book.UpdatedAt = parseTime(updatedAt)
	book.LastReadAt = parseTime(lastReadAt)
	return book, nil
}

func encodeTags(tags []string) string {
	clean := make([]string, 0, len(tags))
	seen := map[string]bool{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		clean = append(clean, tag)
	}
	return strings.Join(clean, ",")
}

func decodeTags(value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{}
	}
	parts := strings.Split(value, ",")
	tags := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" && !seen[part] {
			seen[part] = true
			tags = append(tags, part)
		}
	}
	return tags
}

func profileIDSQL(profileID int64) string {
	if profileID <= 0 {
		profileID = defaultProfileID
	}
	return fmt.Sprintf("%d", profileID)
}

func bookSelectSQL(profileID int64) string {
	profileIDValue := profileIDSQL(profileID)
	return `SELECT b.id, b.series_id, s.title, b.title, b.creator, b.description, b.format, b.page_count, b.cover_status, b.analyzed,
			COALESCE(f.abs_path, ''), COALESCE(f.size, 0), COALESCE(f.mtime, ''), b.created_at, b.updated_at,
			COALESCE(rp.page_index, 0), COALESCE(rp.progress_fraction, 0), COALESCE(rp.updated_at, ''),
			COALESCE(ps.private_status, ''), COALESCE(ps.favorite, 0), COALESCE(ps.rating, 0),
			TRIM(COALESCE(b.tags, '') ||
				CASE
					WHEN COALESCE(b.tags, '') <> '' AND COALESCE(ps.tags, '') <> '' THEN ',' || COALESCE(ps.tags, '')
					ELSE COALESCE(ps.tags, '')
				END, ','),
			COALESCE(ps.summary, '')
		FROM books b
		JOIN series s ON s.id = b.series_id
		LEFT JOIN files f ON f.book_id = b.id
		LEFT JOIN profile_read_progress rp ON rp.book_id = b.id AND rp.profile_id = ` + profileIDValue + `
		LEFT JOIN book_private_states ps ON ps.book_id = b.id AND ps.profile_id = ` + profileIDValue
}

func scanManualCollection(row scanner) (domain.ManualCollection, error) {
	var collection domain.ManualCollection
	var createdAt string
	var updatedAt string
	if err := row.Scan(&collection.ID, &collection.Name, &collection.Description, &collection.ItemCount, &createdAt, &updatedAt); err != nil {
		return collection, err
	}
	collection.CreatedAt = parseTime(createdAt)
	collection.UpdatedAt = parseTime(updatedAt)
	return collection, nil
}

func normalizeManualCollectionAssetType(assetType string) string {
	switch strings.ToLower(strings.TrimSpace(assetType)) {
	case "book", "comic":
		return "book"
	case "game":
		return "game"
	case "video":
		return "video"
	default:
		return ""
	}
}

func scanFile(row scanner) (domain.File, error) {
	var file domain.File
	var mtime string
	if err := row.Scan(&file.ID, &file.BookID, &file.LibraryID, &file.AbsPath, &file.RelPath, &file.Size, &mtime, &file.Ext); err != nil {
		return file, err
	}
	file.MTime = parseTime(mtime)
	return file, nil
}

func gameSelectSQL() string {
	return `SELECT id, library_id, title, platform, rom_set_name, region, format, file_path, rel_path, size, mtime, crc32, sha1, emulator_hint, compatibility, last_played_at, created_at, updated_at FROM games`
}

func scanGames(rows *sql.Rows) ([]domain.GameAsset, error) {
	out := make([]domain.GameAsset, 0)
	for rows.Next() {
		game, err := scanGame(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, game)
	}
	return out, rows.Err()
}

func scanGame(row scanner) (domain.GameAsset, error) {
	var game domain.GameAsset
	var mtime string
	var lastPlayedAt string
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&game.ID,
		&game.LibraryID,
		&game.Title,
		&game.Platform,
		&game.ROMSetName,
		&game.Region,
		&game.Format,
		&game.FilePath,
		&game.RelPath,
		&game.Size,
		&mtime,
		&game.CRC32,
		&game.SHA1,
		&game.EmulatorHint,
		&game.Compatibility,
		&lastPlayedAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return game, err
	}
	game.MTime = parseTime(mtime)
	game.LastPlayedAt = parseTime(lastPlayedAt)
	game.CreatedAt = parseTime(createdAt)
	game.UpdatedAt = parseTime(updatedAt)
	return game, nil
}

func scanGameMetadata(row scanner) (domain.GameMetadata, error) {
	var metadata domain.GameMetadata
	var genres string
	var developers string
	var publishers string
	var externalLinks string
	var updatedAt string
	if err := row.Scan(
		&metadata.GameID,
		&metadata.DisplayTitle,
		&metadata.Summary,
		&metadata.ReleaseDate,
		&genres,
		&developers,
		&publishers,
		&metadata.Players,
		&metadata.Rating,
		&externalLinks,
		&updatedAt,
	); err != nil {
		return metadata, err
	}
	metadata.Genres = nonNilStringList(decodeStringList(genres))
	metadata.Developers = nonNilStringList(decodeStringList(developers))
	metadata.Publishers = nonNilStringList(decodeStringList(publishers))
	metadata.ExternalLinks = nonNilStringList(decodeStringList(externalLinks))
	metadata.UpdatedAt = parseTime(updatedAt)
	return metadata, nil
}

func emptyGameMetadata(gameID int64) domain.GameMetadata {
	return domain.GameMetadata{
		GameID:        gameID,
		Genres:        []string{},
		Developers:    []string{},
		Publishers:    []string{},
		ExternalLinks: []string{},
	}
}

func scanGameMetadataSource(row scanner) (domain.GameMetadataSource, error) {
	var source domain.GameMetadataSource
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&source.ID,
		&source.GameID,
		&source.Source,
		&source.SourceID,
		&source.MatchedBy,
		&source.Confidence,
		&source.RawJSON,
		&createdAt,
		&updatedAt,
	); err != nil {
		return source, err
	}
	source.CreatedAt = parseTime(createdAt)
	source.UpdatedAt = parseTime(updatedAt)
	return source, nil
}

func scanGameArtwork(row scanner) (domain.GameArtwork, error) {
	var artwork domain.GameArtwork
	var selected int
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&artwork.ID,
		&artwork.GameID,
		&artwork.Source,
		&artwork.Kind,
		&artwork.URL,
		&artwork.CachePath,
		&artwork.Width,
		&artwork.Height,
		&selected,
		&artwork.Confidence,
		&createdAt,
		&updatedAt,
	); err != nil {
		return artwork, err
	}
	artwork.Selected = selected != 0
	artwork.CreatedAt = parseTime(createdAt)
	artwork.UpdatedAt = parseTime(updatedAt)
	return artwork, nil
}

func videoSelectSQL() string {
	return `SELECT id, library_id, title, format, file_path, rel_path, size, mtime, duration_seconds, width, height, video_codec, audio_codec, thumbnail_status, last_played_at, created_at, updated_at FROM videos`
}

func scanVideos(rows *sql.Rows) ([]domain.VideoAsset, error) {
	out := make([]domain.VideoAsset, 0)
	for rows.Next() {
		video, err := scanVideo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, video)
	}
	return out, rows.Err()
}

func scanVideo(row scanner) (domain.VideoAsset, error) {
	var video domain.VideoAsset
	var mtime string
	var lastPlayedAt string
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&video.ID,
		&video.LibraryID,
		&video.Title,
		&video.Format,
		&video.FilePath,
		&video.RelPath,
		&video.Size,
		&mtime,
		&video.DurationSeconds,
		&video.Width,
		&video.Height,
		&video.VideoCodec,
		&video.AudioCodec,
		&video.ThumbnailStatus,
		&lastPlayedAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return video, err
	}
	video.MTime = parseTime(mtime)
	video.LastPlayedAt = parseTime(lastPlayedAt)
	video.CreatedAt = parseTime(createdAt)
	video.UpdatedAt = parseTime(updatedAt)
	video.DirectPlayable, video.PlaybackMode, video.PlaybackReason = videoPlaybackCompatibility(video)
	return video, nil
}

func videoPlaybackCompatibility(video domain.VideoAsset) (bool, string, string) {
	format := strings.ToLower(strings.TrimSpace(video.Format))
	videoCodec := strings.ToLower(strings.TrimSpace(video.VideoCodec))
	audioCodec := strings.ToLower(strings.TrimSpace(video.AudioCodec))
	switch format {
	case "mp4", "m4v":
		if videoCodec == "" && looksLikeHEVCVideo(video) {
			return false, "hls", "filename indicates HEVC/H.265 video"
		}
		if videoCodec == "" {
			return true, "direct", ""
		}
		if isH264Codec(videoCodec) && (audioCodec == "" || audioCodec == "aac" || audioCodec == "mp3") {
			return true, "direct", ""
		}
		return false, "hls", "mp4 codecs may need browser transcode"
	case "webm":
		if videoCodec == "" {
			return true, "direct", ""
		}
		if (videoCodec == "vp8" || videoCodec == "vp9" || videoCodec == "av1") && (audioCodec == "" || audioCodec == "opus" || audioCodec == "vorbis") {
			return true, "direct", ""
		}
		return false, "hls", "webm codecs may need browser transcode"
	default:
		return false, "hls", "container or codecs need browser transcode"
	}
}

func isH264Codec(codec string) bool {
	return codec == "h264" || codec == "avc1" || codec == "avc"
}

func looksLikeHEVCVideo(video domain.VideoAsset) bool {
	haystack := strings.ToLower(strings.Join([]string{video.Title, video.RelPath, video.FilePath}, " "))
	hevcMarkers := []string{"h265", "h.265", "hevc", "x265", "10bit", "10-bit", "hdr", "dolby vision", "dv"}
	for _, marker := range hevcMarkers {
		if strings.Contains(haystack, marker) {
			return true
		}
	}
	return false
}

func scanJob(row scanner) (domain.ScanJob, error) {
	var job domain.ScanJob
	var started string
	var finished string
	if err := row.Scan(&job.ID, &job.LibraryID, &job.Status, &job.TargetPath, &job.CurrentPath, &job.DiscoveredFiles, &job.IndexedFiles, &job.SkippedFiles, &job.ErrorCount, &job.MetadataUpdatedFiles, &job.ReclassifiedFiles, &started, &finished); err != nil {
		return job, err
	}
	job.StartedAt = parseTime(started)
	if finished != "" {
		job.FinishedAt = parseTime(finished)
	}
	return job, nil
}

func parseTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339Nano)
}

func NotFound(err error) bool {
	return err == sql.ErrNoRows
}

func WrapNotFound(name string, err error) error {
	if err == sql.ErrNoRows {
		return fmt.Errorf("%s not found", name)
	}
	return err
}
