package service

import "foliospace-reader/internal/domain"

func (s *Service) VideoWorksFiltered(q, kind string, library int64, sort string, limit, offset int) (domain.VideoWorkPage, error) {
	if err := s.PurgeVideoProviderCache(false); err != nil {
		return domain.VideoWorkPage{}, err
	}
	return s.store.VideoWorksFiltered(q, kind, library, sort, limit, offset)
}
func (s *Service) VideoSeasons(id int64, limit, offset int) (domain.VideoPage[domain.VideoSeason], error) {
	return s.store.VideoSeasons(id, limit, offset)
}
func (s *Service) VideoEpisodes(id int64, season *int, limit, offset int) (domain.VideoPage[domain.VideoEpisode], error) {
	return s.store.VideoEpisodes(id, season, limit, offset)
}
func (s *Service) VideoWorkEpisodeFiles(id int64, season *int, episodeID int64, limit, offset int) (domain.VideoWorkFiles, error) {
	return s.store.VideoWorkEpisodeFiles(id, season, episodeID, limit, offset)
}
func (s *Service) VideoPlaybackState(id, profile int64) (domain.VideoPlaybackState, error) {
	return s.store.VideoPlaybackState(id, profile)
}
func (s *Service) ApplyVideoPlayback(id, profile int64, in domain.VideoPlaybackEvent) (domain.VideoPlaybackState, error) {
	return s.store.ApplyVideoPlayback(id, profile, in)
}
func (s *Service) VideoPlaybackFeed(profile, library int64, kind string, limit, offset int) (domain.VideoPage[domain.VideoPlaybackItem], error) {
	return s.store.VideoPlaybackFeed(profile, library, kind, limit, offset)
}
func (s *Service) VideoProfileID(id int64) (int64, error) { return s.store.VideoProfileID(id) }
