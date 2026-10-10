package store

import (
	"fmt"
	"foliospace-reader/internal/domain"
	"time"
)

func (s *Store) VideoExpectedFresh(work int64, season int) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM video_expected_seasons WHERE work_id=? AND season=? AND expires_at>?`, work, season, time.Now().Unix()).Scan(&n)
	return n > 0, err
}
func (s *Store) SaveVideoExpected(work int64, season int, providerID string, items []domain.VideoExpectedEpisode) error {
	if len(items) > 2000 || season < 0 || season > 999 {
		return fmt.Errorf("invalid expected episodes")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current string
	if err = tx.QueryRow(`SELECT d.provider_id FROM video_work_details d JOIN video_works w ON w.id=d.work_id WHERE w.id=? AND w.kind='series' AND d.provider='tmdb' AND d.expires_at>?`, work, time.Now().Unix()).Scan(&current); err != nil {
		return err
	}
	if current != providerID {
		return ErrVideoRevision
	}
	if _, err = tx.Exec(`DELETE FROM video_expected_episodes WHERE work_id=? AND season=?`, work, season); err != nil {
		return err
	}
	expires := time.Now().Add(24 * time.Hour).Unix()
	for _, e := range items {
		if e.Season != season || e.Number < 0 || e.Number > 99999 || len(e.Title) > 2000 {
			return fmt.Errorf("invalid expected episode")
		}
		if e.AirDate != "" {
			if _, err = time.Parse("2006-01-02", e.AirDate); err != nil {
				return fmt.Errorf("invalid air date")
			}
		}
		if _, err = tx.Exec(`INSERT INTO video_expected_episodes VALUES(?,?,?,?,?,?)`, work, season, e.Number, e.Title, e.AirDate, expires); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`INSERT INTO video_expected_seasons VALUES(?,?,?) ON CONFLICT(work_id,season) DO UPDATE SET expires_at=excluded.expires_at`, work, season, expires); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) VideoAvailability(work int64, season int, enabled bool, limit, offset int) (domain.VideoAvailability, error) {
	limit, offset = videoPage(limit, offset)
	out := domain.VideoAvailability{VideoPage: domain.VideoPage[domain.VideoExpectedEpisode]{Items: []domain.VideoExpectedEpisode{}, Limit: limit, Offset: offset}, Source: "local", Ordering: "season-number", DateBasis: "UTC-date"}
	if _, err := s.VideoWork(work); err != nil {
		return out, err
	}
	fresh, err := s.VideoExpectedFresh(work, season)
	if err != nil {
		return out, err
	}
	fresh = fresh && enabled
	if fresh {
		out.Source = "tmdb"
		out.Ordering = "tmdb-aired"
		var count int
		if err = s.db.QueryRow(`SELECT COUNT(*) FROM video_expected_episodes WHERE work_id=? AND season=?`, work, season).Scan(&count); err != nil {
			return out, err
		}
		out.ExpectedCount = &count
	}
	cte := `WITH local AS (SELECT e.id,e.number,e.title,MIN(l.video_id) video_id FROM video_seasons s JOIN video_episodes e ON e.season_id=s.id JOIN video_file_links l ON l.episode_id=e.id WHERE s.work_id=? AND s.number=? GROUP BY e.id),
 merged AS (SELECT x.number,x.title,x.air_date,l.id episode_id,l.video_id FROM video_expected_episodes x LEFT JOIN local l ON l.number=x.number WHERE x.work_id=? AND x.season=? AND ?
 UNION ALL SELECT l.number,l.title,'' air_date,l.id,l.video_id FROM local l WHERE NOT EXISTS(SELECT 1 FROM video_expected_episodes x WHERE x.work_id=? AND x.season=? AND x.number=l.number AND ?)) `
	args := []any{work, season, work, season, fresh, work, season, fresh}
	if err = s.db.QueryRow(cte+`SELECT COUNT(*) FROM merged`, args...).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := s.db.Query(cte+`SELECT number,title,air_date,episode_id,video_id FROM merged ORDER BY number LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	today := time.Now().UTC().Format("2006-01-02")
	for rows.Next() {
		item := domain.VideoExpectedEpisode{Season: season, Availability: "unknown"}
		if err = rows.Scan(&item.Number, &item.Title, &item.AirDate, &item.EpisodeID, &item.VideoID); err != nil {
			return out, err
		}
		if item.VideoID != nil {
			item.Availability = "available"
		} else if item.AirDate > today {
			item.Availability = "unreleased"
		} else if item.AirDate != "" {
			item.Availability = "missing"
		}
		out.Items = append(out.Items, item)
	}
	out.HasMore = offset+len(out.Items) < out.Total
	return out, rows.Err()
}
