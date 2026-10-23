package service

import "foliospace-reader/internal/store"

func (s *Service) PreviewLibraryOwnership(libraryID int64) (store.LibraryOwnershipPreview, error) {
	return s.store.PreviewLibraryOwnership(libraryID)
}
