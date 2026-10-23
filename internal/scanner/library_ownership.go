package scanner

import (
	"path/filepath"
	"sort"
	"strings"

	"foliospace-reader/internal/domain"
)

// A scan takes one configuration snapshot, so all workers use the same owner.
// Existing records from another library are left for a separate reconciliation
// preview; scanning must not silently migrate identity or private state.
type scanOwnership struct {
	libraries    []domain.Library
	foreignPaths map[string]bool
}

func (s *Scanner) scanOwnership(library domain.Library, rootPath string) (*scanOwnership, error) {
	libraries, err := s.store.ListLibraries()
	if err != nil {
		return nil, err
	}
	for i := range libraries {
		libraries[i].RootPath = filepath.Clean(libraries[i].RootPath)
	}
	sort.Slice(libraries, func(i, j int) bool {
		if len(libraries[i].RootPath) != len(libraries[j].RootPath) {
			return len(libraries[i].RootPath) > len(libraries[j].RootPath)
		}
		return libraries[i].ID < libraries[j].ID
	})
	foreignPaths, err := s.store.ForeignScanPaths(library.ID, rootPath)
	if err != nil {
		return nil, err
	}
	// Correctly owned child paths are already excluded by the root gate. Only
	// count legacy conflicts within this library's own scope in the job notice.
	for path := range foreignPaths {
		for _, candidate := range libraries {
			if scanPathWithin(candidate.RootPath, path) {
				if candidate.ID != library.ID {
					delete(foreignPaths, path)
				}
				break
			}
		}
	}
	return &scanOwnership{libraries: libraries, foreignPaths: foreignPaths}, nil
}

func (o *scanOwnership) ownsPath(libraryID int64, path string) bool {
	if o == nil { // Narrow metadata/index helpers can be used outside a scan.
		return true
	}
	path = filepath.Clean(path)
	if o.foreignPaths[path] {
		return false
	}
	for _, library := range o.libraries {
		if scanPathWithin(library.RootPath, path) {
			return library.ID == libraryID
		}
	}
	return false
}

func (o *scanOwnership) hasNestedRoots(library domain.Library) bool {
	if o == nil {
		return false
	}
	for _, candidate := range o.libraries {
		if candidate.ID != library.ID && scanPathWithin(library.RootPath, candidate.RootPath) {
			return true
		}
	}
	return false
}

func scanPathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
