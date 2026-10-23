package store

import (
	"path/filepath"
	"sort"
	"strings"
)

type LibraryOwnershipRoot struct {
	LibraryID int64  `json:"libraryId"`
	RootPath  string `json:"rootPath"`
}

type LibraryOwnershipConflict struct {
	AssetType         string `json:"assetType"`
	AssetID           int64  `json:"assetId"`
	Path              string `json:"path"`
	CurrentLibraryID  int64  `json:"currentLibraryId"`
	ExpectedLibraryID int64  `json:"expectedLibraryId"`
}

type LibraryOwnershipPreview struct {
	LibraryID          int64                      `json:"libraryId"`
	ExcludedRoots      []LibraryOwnershipRoot     `json:"excludedRoots"`
	ConflictingRecords int                        `json:"conflictingRecords"`
	Examples           []LibraryOwnershipConflict `json:"examples"`
}

// PreviewLibraryOwnership is intentionally read-only. Reconciliation needs a
// separate reviewed plan because moving an existing collection can affect its
// private state and several formats may share a book ID.
func (s *Store) PreviewLibraryOwnership(libraryID int64) (LibraryOwnershipPreview, error) {
	out := LibraryOwnershipPreview{LibraryID: libraryID, ExcludedRoots: []LibraryOwnershipRoot{}, Examples: []LibraryOwnershipConflict{}}
	library, err := s.LibraryByID(libraryID)
	if err != nil {
		return out, err
	}
	libraries, err := s.ListLibraries()
	if err != nil {
		return out, err
	}
	for i := range libraries {
		libraries[i].RootPath = filepath.Clean(libraries[i].RootPath)
		if libraries[i].ID != libraryID && libraries[i].RootPath != filepath.Clean(library.RootPath) && libraryPathWithin(library.RootPath, libraries[i].RootPath) {
			out.ExcludedRoots = append(out.ExcludedRoots, LibraryOwnershipRoot{LibraryID: libraries[i].ID, RootPath: libraries[i].RootPath})
		}
	}
	sort.Slice(libraries, func(i, j int) bool {
		if len(libraries[i].RootPath) != len(libraries[j].RootPath) {
			return len(libraries[i].RootPath) > len(libraries[j].RootPath)
		}
		return libraries[i].ID < libraries[j].ID
	})
	sort.Slice(out.ExcludedRoots, func(i, j int) bool { return out.ExcludedRoots[i].RootPath < out.ExcludedRoots[j].RootPath })
	root := filepath.Clean(library.RootPath)
	prefix := strings.TrimRight(root, string(filepath.Separator)) + string(filepath.Separator)
	rows, err := s.db.Query(`SELECT asset_type, asset_id, library_id, path FROM (
		SELECT 'book' AS asset_type, book_id AS asset_id, library_id, abs_path AS path FROM files
		UNION ALL SELECT 'game', id, library_id, file_path FROM games
		UNION ALL SELECT 'video', id, library_id, file_path FROM videos
		UNION ALL SELECT 'game_source', id, library_id, file_path FROM game_sources
	) WHERE path = ? OR substr(path, 1, length(?)) = ? ORDER BY path, asset_type, asset_id`, root, prefix, prefix)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var item LibraryOwnershipConflict
		if err := rows.Scan(&item.AssetType, &item.AssetID, &item.CurrentLibraryID, &item.Path); err != nil {
			return out, err
		}
		for _, candidate := range libraries {
			if libraryPathWithin(candidate.RootPath, item.Path) {
				item.ExpectedLibraryID = candidate.ID
				break
			}
		}
		if item.CurrentLibraryID != item.ExpectedLibraryID {
			out.ConflictingRecords++
			if len(out.Examples) < 12 {
				out.Examples = append(out.Examples, item)
			}
		}
	}
	return out, rows.Err()
}

func libraryPathWithin(root, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// ForeignScanPaths records only path conflicts, without changing ownership,
// deleting rows, or comparing content hashes. Prefix matching uses a directory
// boundary and treats SQL wildcard characters in real filenames literally.
func (s *Store) ForeignScanPaths(libraryID int64, rootPath string) (map[string]bool, error) {
	rootPath = filepath.Clean(rootPath)
	prefix := strings.TrimRight(rootPath, string(filepath.Separator)) + string(filepath.Separator)
	rows, err := s.db.Query(`SELECT path FROM (
		SELECT library_id, abs_path AS path FROM files
		UNION ALL SELECT library_id, file_path FROM games
		UNION ALL SELECT library_id, file_path FROM videos
		UNION ALL SELECT library_id, file_path FROM game_sources
	) WHERE library_id <> ? AND (path = ? OR substr(path, 1, length(?)) = ?)`, libraryID, rootPath, prefix, prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	paths := map[string]bool{}
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		paths[filepath.Clean(path)] = true
	}
	return paths, rows.Err()
}
