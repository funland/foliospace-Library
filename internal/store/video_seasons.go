package store

import "foliospace-reader/internal/domain"

func (s *Store) VideoSeasons(workID int64, limit, offset int) (domain.VideoPage[domain.VideoSeason], error) {
	limit, offset = videoPage(limit, offset)
	p := domain.VideoPage[domain.VideoSeason]{Items: []domain.VideoSeason{}, Limit: limit, Offset: offset}
	if _, err := s.VideoWork(workID); err != nil {
		return p, err
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM video_seasons WHERE work_id=?`, workID).Scan(&p.Total); err != nil {
		return p, err
	}
	rows, err := s.db.Query(`SELECT s.id,s.work_id,s.number,COUNT(DISTINCT CASE WHEN l.video_id IS NOT NULL THEN e.id END),COUNT(DISTINCT l.video_id)
 FROM video_seasons s LEFT JOIN video_episodes e ON e.season_id=s.id LEFT JOIN video_file_links l ON l.episode_id=e.id
 WHERE s.work_id=? GROUP BY s.id ORDER BY s.number,s.id LIMIT ? OFFSET ?`, workID, limit, offset)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var v domain.VideoSeason
		if err = rows.Scan(&v.ID, &v.WorkID, &v.Number, &v.AvailableEpisodeCount, &v.FileCount); err != nil {
			return p, err
		}
		p.Items = append(p.Items, v)
	}
	p.HasMore = offset+len(p.Items) < p.Total
	return p, rows.Err()
}
func (s *Store) VideoEpisodes(workID int64, season *int, limit, offset int) (domain.VideoPage[domain.VideoEpisode], error) {
	limit, offset = videoPage(limit, offset)
	p := domain.VideoPage[domain.VideoEpisode]{Items: []domain.VideoEpisode{}, Limit: limit, Offset: offset}
	if _, err := s.VideoWork(workID); err != nil {
		return p, err
	}
	from := ` FROM video_episodes e JOIN video_seasons s ON s.id=e.season_id WHERE s.work_id=? AND EXISTS(SELECT 1 FROM video_file_links l WHERE l.episode_id=e.id)`
	args := []any{workID}
	if season != nil {
		from += ` AND s.number=?`
		args = append(args, *season)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*)`+from, args...).Scan(&p.Total); err != nil {
		return p, err
	}
	rows, err := s.db.Query(`SELECT e.id,s.id,s.work_id,s.number,e.number,COALESCE(NULLIF(e.title,''),
 (SELECT COALESCE(json_extract(o.value_json,'$'),json_extract(m.local_json,'$.title'),a.title) FROM video_file_links l JOIN videos a ON a.id=l.video_id LEFT JOIN video_metadata_inputs m ON m.video_id=a.id LEFT JOIN video_metadata_overrides o ON o.video_id=a.id AND o.field='title' WHERE l.episode_id=e.id ORDER BY a.id LIMIT 1),''),
 (SELECT COUNT(*) FROM video_file_links l WHERE l.episode_id=e.id),
 (SELECT MIN(l.video_id) FROM video_file_links l WHERE l.episode_id=e.id)`+from+` ORDER BY s.number,e.number,e.id LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var v domain.VideoEpisode
		if err = rows.Scan(&v.ID, &v.SeasonID, &v.WorkID, &v.Season, &v.Number, &v.Title, &v.FileCount, &v.VideoID); err != nil {
			return p, err
		}
		v.Availability = "available"
		p.Items = append(p.Items, v)
	}
	p.HasMore = offset+len(p.Items) < p.Total
	return p, rows.Err()
}
