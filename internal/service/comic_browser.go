package service

import "foliospace-reader/internal/store"

func (s *Service) ComicBrowser(profileID int64, options store.ComicBrowserOptions) (store.ComicBrowserPage, error) {
	return s.store.ComicBrowser(profileID, options)
}

func (s *Service) ComicTitleRules(libraryID int64) (store.ComicTitleRules, error) {
	return s.store.ComicTitleRules(libraryID)
}

func (s *Service) PreviewComicTitleRules(libraryID int64, rules []store.ComicTitleRule) (store.ComicTitleRules, error) {
	return s.store.PreviewComicTitleRules(libraryID, rules)
}

func (s *Service) SaveComicTitleRules(libraryID int64, rules []store.ComicTitleRule) (store.ComicTitleRules, error) {
	return s.store.SaveComicTitleRules(libraryID, rules)
}
