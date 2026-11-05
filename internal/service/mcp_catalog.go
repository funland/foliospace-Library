package service

import (
	"context"
	"foliospace-reader/internal/store"
)

func (s *Service) CatalogBooks(ctx context.Context, options store.CatalogOptions) (store.CatalogPage, error) {
	return s.store.CatalogBooks(ctx, options)
}

func (s *Service) SearchCatalog(ctx context.Context, options store.CatalogOptions) (store.CatalogPage, error) {
	return s.store.SearchCatalog(ctx, options)
}

// ComicCatalog returns directory structure without collection-private state.
func (s *Service) ComicCatalog(ctx context.Context, profileID int64, options store.ComicBrowserOptions) (store.ComicBrowserPage, int64, error) {
	resolved, err := s.store.CatalogProfile(ctx, profileID)
	if err != nil {
		return store.ComicBrowserPage{}, 0, err
	}
	page, err := s.store.ComicBrowser(resolved, options)
	return page, resolved, err
}
