package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"foliospace-reader/internal/domain"
	"strings"
	"time"
)

var ErrVideoRevision = errors.New("video-metadata-revision-conflict")

func videoGeneration(tx *sql.Tx, id int64) (int64, string, error) {
	if _, err := tx.Exec(`INSERT OR IGNORE INTO video_match_state(video_id) VALUES(?)`, id); err != nil {
		return 0, "", err
	}
	var generation int64
	var status string
	err := tx.QueryRow(`SELECT generation,status FROM video_match_state WHERE video_id=?`, id).Scan(&generation, &status)
	return generation, status, err
}

// Local observations may change provisional grouping, never confirmed/unlinked grouping.
func (s *Store) SyncLocalVideoWork(id int64, m domain.VideoLocalMetadata) error {
	if m.Kind != "movie" && m.Kind != "episode" {
		// Never retain a provisional group after conflicting observations.
		// Explicit confirmations and unlinks remain protected.
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err = tx.Exec(`DELETE FROM video_file_links WHERE video_id=? AND EXISTS(SELECT 1 FROM video_match_state WHERE video_id=? AND status='local')`, id, id); err != nil {
			return err
		}
		if _, err = tx.Exec(`UPDATE video_match_state SET status='unmatched' WHERE video_id=? AND status='local'`, id); err != nil {
			return err
		}
		return tx.Commit()
	}
	title := m.Title
	kind := m.Kind
	if kind == "episode" {
		kind = "series"
		title = m.ShowTitle
		if title == "" || m.Season == nil || m.Episode == nil {
			return nil
		}
	}
	return s.matchVideo(id, domain.VideoMatchInput{Title: title, Kind: kind, Year: m.Year, Overview: m.Overview, Season: m.Season, Episode: m.Episode}, false)
}
func (s *Store) MatchVideo(id int64, in domain.VideoMatchInput) error {
	return s.matchVideo(id, in, true)
}
func (s *Store) matchVideo(id int64, in domain.VideoMatchInput, manual bool) error {
	in.Title = strings.TrimSpace(in.Title)
	if in.WorkID == 0 && (in.Title == "" || len(in.Title) > 1000 || (in.Kind != "movie" && in.Kind != "series")) {
		return fmt.Errorf("invalid work title/kind")
	}
	if in.Year < 0 || in.Year > 9999 || len(in.Overview) > 65536 || len(in.Version) > 500 {
		return fmt.Errorf("invalid work metadata")
	}
	if (in.Season == nil) != (in.Episode == nil) || (in.Season != nil && (*in.Season < 0 || *in.Season > 999 || *in.Episode < 0 || *in.Episode > 99999)) {
		return fmt.Errorf("invalid season/episode")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var libraryID int64
	var format string
	if err = tx.QueryRow(`SELECT library_id,format FROM videos WHERE id=?`, id).Scan(&libraryID, &format); err != nil {
		return err
	}
	generation, status, err := videoGeneration(tx, id)
	if err != nil {
		return err
	}
	if !manual && (status == "confirmed" || status == "unlinked") {
		return tx.Commit()
	}
	if manual && generation != in.Generation {
		return ErrVideoRevision
	}
	workID := in.WorkID
	if workID != 0 {
		var existingLib int64
		if err = tx.QueryRow(`SELECT library_id,kind FROM video_works WHERE id=?`, workID).Scan(&existingLib, &in.Kind); err != nil {
			return err
		}
		if existingLib != libraryID {
			return fmt.Errorf("cross-library association is not allowed")
		}
	} else {
		identityYear := in.Year
		if in.Kind == "series" {
			identityYear = 0
		} // Episode air years must not split one series.
		identity := fmt.Sprintf("%d/local/%s/%s/%d", libraryID, in.Kind, strings.ToLower(in.Title), identityYear)
		if in.Provider != "" {
			if in.Provider != "tmdb" || in.ProviderID == "" {
				return fmt.Errorf("invalid provider")
			}
			identity = fmt.Sprintf("%d/tmdb/%s/%s", libraryID, in.Kind, in.ProviderID)
		}
		err = tx.QueryRow(`SELECT work_id FROM video_work_details WHERE identity_key=?`, identity).Scan(&workID)
		if err == sql.ErrNoRows {
			r, e := tx.Exec(`INSERT INTO video_works(library_id,kind,title,year) VALUES(?,?,?,?)`, libraryID, in.Kind, in.Title, in.Year)
			if e != nil {
				return e
			}
			workID, _ = r.LastInsertId()
			raw, _ := json.Marshal(in)
			expires := int64(0)
			if in.Provider == "tmdb" {
				expires = time.Now().Add(150 * 24 * time.Hour).Unix()
			}
			_, err = tx.Exec(`INSERT INTO video_work_details(work_id,identity_key,overview,provider,provider_id,raw_json,expires_at) VALUES(?,?,?,?,?,?,?)`, workID, identity, in.Overview, in.Provider, in.ProviderID, string(raw), expires)
		}
		if err != nil {
			return err
		}
		// Refresh provisional observations; never replace a confirmed work on rescan.
		var protected int
		if err = tx.QueryRow(`SELECT COUNT(*) FROM video_file_links l JOIN video_versions v ON v.id=l.version_id JOIN video_match_state m ON m.video_id=l.video_id WHERE v.work_id=? AND m.status='confirmed'`, workID).Scan(&protected); err != nil {
			return err
		}
		if manual || protected == 0 {
			expires := int64(0)
			if in.Provider == "tmdb" {
				expires = time.Now().Add(150 * 24 * time.Hour).Unix()
			}
			if _, err = tx.Exec(`UPDATE video_work_details SET overview=?,expires_at=? WHERE work_id=?`, in.Overview, expires, workID); err != nil {
				return err
			}
		}
	}
	if (in.Kind == "series" && in.Season == nil) || (in.Kind == "movie" && in.Season != nil) {
		return fmt.Errorf("series requires season/episode; movie must not include them")
	}
	var episodeID any
	if in.Kind == "series" {
		if _, err = tx.Exec(`INSERT OR IGNORE INTO video_seasons(work_id,number) VALUES(?,?)`, workID, *in.Season); err != nil {
			return err
		}
		var seasonID, eid int64
		if err = tx.QueryRow(`SELECT id FROM video_seasons WHERE work_id=? AND number=?`, workID, *in.Season).Scan(&seasonID); err != nil {
			return err
		}
		if _, err = tx.Exec(`INSERT OR IGNORE INTO video_episodes(season_id,number) VALUES(?,?)`, seasonID, *in.Episode); err != nil {
			return err
		}
		if err = tx.QueryRow(`SELECT id FROM video_episodes WHERE season_id=? AND number=?`, seasonID, *in.Episode).Scan(&eid); err != nil {
			return err
		}
		episodeID = eid
	}
	label := strings.TrimSpace(in.Version)
	if label == "" {
		label = fmt.Sprintf("%s · file %d", strings.ToUpper(format), id)
	}
	if _, err = tx.Exec(`INSERT OR IGNORE INTO video_versions(work_id,label) VALUES(?,?)`, workID, label); err != nil {
		return err
	}
	var versionID int64
	if err = tx.QueryRow(`SELECT id FROM video_versions WHERE work_id=? AND label=?`, workID, label).Scan(&versionID); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM video_file_links WHERE video_id=?`, id); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO video_file_links(video_id,version_id,episode_id) VALUES(?,?,?)`, id, versionID, episodeID); err != nil {
		return err
	}
	if manual {
		_, err = tx.Exec(`UPDATE video_match_state SET generation=generation+1,status='confirmed' WHERE video_id=?`, id)
	} else {
		_, err = tx.Exec(`UPDATE video_match_state SET status='local' WHERE video_id=?`, id)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) UnlinkVideo(id, generation int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	g, _, err := videoGeneration(tx, id)
	if err != nil {
		return err
	}
	if g != generation {
		return ErrVideoRevision
	}
	if _, err = tx.Exec(`DELETE FROM video_file_links WHERE video_id=?`, id); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE video_match_state SET generation=generation+1,status='unlinked' WHERE video_id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}
func videoPage(limit, offset int) (int, int) {
	if limit < 1 {
		limit = 30
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

const videoWorkFrom = ` FROM video_works w JOIN video_work_details d ON d.work_id=w.id
 WHERE EXISTS(SELECT 1 FROM video_versions v JOIN video_file_links f ON f.version_id=v.id WHERE v.work_id=w.id)`
const videoWorkAdded = `COALESCE((SELECT MAX(a.created_at) FROM video_versions v JOIN video_file_links f ON f.version_id=v.id JOIN videos a ON a.id=f.video_id WHERE v.work_id=w.id),w.created_at)`
const videoWorkSelect = `SELECT w.id,w.library_id,w.kind,w.title,w.year,d.overview,d.provider,d.provider_id,
 (SELECT MIN(f.video_id) FROM video_versions v JOIN video_file_links f ON f.version_id=v.id WHERE v.work_id=w.id),
 (SELECT COUNT(DISTINCT f.video_id) FROM video_versions v JOIN video_file_links f ON f.version_id=v.id WHERE v.work_id=w.id),
 (SELECT COUNT(DISTINCT f.version_id) FROM video_versions v JOIN video_file_links f ON f.version_id=v.id WHERE v.work_id=w.id),
 (SELECT COUNT(DISTINCT f.episode_id) FROM video_versions v JOIN video_file_links f ON f.version_id=v.id WHERE v.work_id=w.id),` + videoWorkAdded

func scanVideoWork(row scanner) (domain.VideoWork, error) {
	var w domain.VideoWork
	err := row.Scan(&w.ID, &w.LibraryID, &w.Kind, &w.Title, &w.Year, &w.Overview, &w.Provider, &w.ProviderID, &w.VideoID, &w.FileCount, &w.VersionCount, &w.AvailableEpisodeCount, &w.AddedAt)
	w.PosterURL = fmt.Sprintf("/api/videos/%d/artwork/poster", w.VideoID)
	return w, err
}
func (s *Store) VideoWorks(q, kind string, limit, offset int) (domain.VideoWorkPage, error) {
	return s.VideoWorksFiltered(q, kind, 0, "title", limit, offset)
}
func (s *Store) VideoWorksFiltered(q, kind string, libraryID int64, sort string, limit, offset int) (domain.VideoWorkPage, error) {
	limit, offset = videoPage(limit, offset)
	p := domain.VideoWorkPage{Items: []domain.VideoWork{}, Limit: limit, Offset: offset}
	where := videoWorkFrom + ` AND (?='' OR w.kind=?) AND (?='' OR instr(lower(w.title),lower(?))>0) AND (?=0 OR w.library_id=?)`
	args := []any{kind, kind, q, q, libraryID, libraryID}
	if err := s.db.QueryRow(`SELECT COUNT(*)`+where, args...).Scan(&p.Total); err != nil {
		return p, err
	}
	order := `w.title COLLATE NOCASE,w.id`
	if sort == "recent" {
		order = videoWorkAdded + ` DESC,w.id DESC`
	}
	rows, err := s.db.Query(videoWorkSelect+where+` ORDER BY `+order+` LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		w, e := scanVideoWork(rows)
		if e != nil {
			return p, e
		}
		p.Items = append(p.Items, w)
	}
	p.HasMore = offset+len(p.Items) < p.Total
	return p, rows.Err()
}
func (s *Store) VideoWork(id int64) (domain.VideoWork, error) {
	return scanVideoWork(s.db.QueryRow(videoWorkSelect+videoWorkFrom+` AND w.id=?`, id))
}
func (s *Store) VideoWorkFiles(id int64, season *int, limit, offset int) (domain.VideoWorkFiles, error) {
	return s.VideoWorkEpisodeFiles(id, season, 0, limit, offset)
}
func (s *Store) VideoWorkEpisodeFiles(id int64, season *int, episodeID int64, limit, offset int) (domain.VideoWorkFiles, error) {
	limit, offset = videoPage(limit, offset)
	p := domain.VideoWorkFiles{Items: []domain.VideoWorkFile{}, Limit: limit, Offset: offset}
	if _, err := s.VideoWork(id); err != nil {
		return p, err
	}
	from := ` FROM video_file_links f JOIN videos a ON a.id=f.video_id JOIN video_versions v ON v.id=f.version_id LEFT JOIN video_episodes e ON e.id=f.episode_id LEFT JOIN video_seasons s ON s.id=e.season_id WHERE v.work_id=?`
	args := []any{id}
	if episodeID > 0 {
		from += ` AND e.id=?`
		args = append(args, episodeID)
	}
	if season != nil {
		from += ` AND s.number=?`
		args = append(args, *season)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*)`+from, args...).Scan(&p.Total); err != nil {
		return p, err
	}
	rows, err := s.db.Query(`SELECT a.id,COALESCE((SELECT json_extract(o.value_json,'$') FROM video_metadata_overrides o WHERE o.video_id=a.id AND o.field='title'),a.title),v.label,s.number,e.number,a.format,s.id,e.id,v.id`+from+` ORDER BY s.number,e.number,v.label,a.id LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var f domain.VideoWorkFile
		if err = rows.Scan(&f.VideoID, &f.Title, &f.Version, &f.Season, &f.Episode, &f.Format, &f.SeasonID, &f.EpisodeID, &f.VersionID); err != nil {
			return p, err
		}
		f.FileURL = fmt.Sprintf("/api/client/videos/%d/file", f.VideoID)
		p.Items = append(p.Items, f)
	}
	p.HasMore = offset+len(p.Items) < p.Total
	return p, rows.Err()
}
