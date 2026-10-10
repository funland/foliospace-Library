package service

import "foliospace-reader/internal/domain"

func (s *Service) VideoWorks(q, kind string, limit, offset int) (domain.VideoWorkPage, error) {
	if err := s.PurgeVideoProviderCache(false); err != nil {
		return domain.VideoWorkPage{}, err
	}
	return s.store.VideoWorks(q, kind, limit, offset)
}
func (s *Service) VideoWork(id int64) (domain.VideoWork, error) {
	if err := s.PurgeVideoProviderCache(false); err != nil {
		return domain.VideoWork{}, err
	}
	return s.store.VideoWork(id)
}
func (s *Service) VideoWorkFiles(id int64, season *int, limit, offset int) (domain.VideoWorkFiles, error) {
	return s.store.VideoWorkFiles(id, season, limit, offset)
}
func (s *Service) MatchVideo(id int64, in domain.VideoMatchInput) error {
	in.Provider = ""
	in.ProviderID = ""
	return s.store.MatchVideo(id, in)
}
func (s *Service) UnlinkVideo(id, generation int64) error { return s.store.UnlinkVideo(id, generation) }

func (s *Service) VideoMetadata(id int64) (domain.VideoMetadata, error) {
	return s.store.VideoMetadata(id)
}
func (s *Service) SetVideoMetadataOverride(id int64, field, value string) error {
	if _, err := s.store.VideoByID(id); err != nil {
		return err
	}
	return s.store.SetVideoMetadataOverride(id, field, value)
}
func (s *Service) ResetVideoMetadataOverride(id int64, field string) error {
	if _, err := s.store.VideoByID(id); err != nil {
		return err
	}
	return s.store.DeleteVideoMetadataOverride(id, field)
}
