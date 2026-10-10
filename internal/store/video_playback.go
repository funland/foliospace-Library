package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"foliospace-reader/internal/domain"
	"math"
	"strings"
	"time"
)

type VideoPlaybackConflict struct {
	Code  string
	State domain.VideoPlaybackState
}

func (e *VideoPlaybackConflict) Error() string { return e.Code }

// Identity uses indexed content attributes and the exact version/episode link.
// Never return disk paths or transfer the playhead between different cuts.
const videoMediaKeySQL = `printf('%d:%d:%s:%d:%d',a.id,a.size,a.mtime,COALESCE(v.id,0),COALESCE(e.id,0))`
const videoIdentityFrom = ` FROM videos a LEFT JOIN video_file_links l ON l.video_id=a.id LEFT JOIN video_versions v ON v.id=l.version_id LEFT JOIN video_episodes e ON e.id=l.episode_id`

func (s *Store) VideoProfileID(id int64) (int64, error) {
	if id > 0 {
		p, err := s.ProfileByID(id)
		return p.ID, err
	}
	return s.ResolveProfileID(0)
}
func playbackState(tx *sql.Tx, id, profile int64) (domain.VideoPlaybackState, error) {
	out := domain.VideoPlaybackState{VideoID: id, ProfileID: profile}
	if err := tx.QueryRow(`SELECT `+videoMediaKeySQL+videoIdentityFrom+` WHERE a.id=?`, id).Scan(&out.MediaKey); err != nil {
		return out, err
	}
	var oldKey string
	err := tx.QueryRow(`SELECT media_key,revision,position_seconds,duration_seconds,completed,updated_at FROM video_playback_state WHERE profile_id=? AND video_id=?`, profile, id).Scan(&oldKey, &out.Revision, &out.PositionSeconds, &out.DurationSeconds, &out.Completed, &out.ServerUpdatedAt)
	if err == sql.ErrNoRows {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if oldKey != out.MediaKey {
		out.IdentityChanged = true
		out.PositionSeconds = 0
		out.DurationSeconds = 0
		out.Completed = false
		out.ServerUpdatedAt = 0
	}
	return out, nil
}
func (s *Store) VideoPlaybackState(id, profile int64) (domain.VideoPlaybackState, error) {
	p, err := s.VideoProfileID(profile)
	if err != nil {
		return domain.VideoPlaybackState{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return domain.VideoPlaybackState{}, err
	}
	defer tx.Rollback()
	return playbackState(tx, id, p)
}
func (s *Store) ApplyVideoPlayback(id, profile int64, in domain.VideoPlaybackEvent) (domain.VideoPlaybackState, error) {
	out := domain.VideoPlaybackState{}
	finite := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 365*24*3600 }
	if strings.TrimSpace(in.EventID) == "" || len(in.EventID) > 128 || strings.TrimSpace(in.SessionID) == "" || len(in.SessionID) > 128 || in.ClientSequence < 1 || in.BaseRevision < 0 || len(in.MediaKey) > 512 || !finite(in.PositionSeconds) || !finite(in.DurationSeconds) || (in.DurationSeconds > 0 && in.PositionSeconds > in.DurationSeconds+1) {
		return out, fmt.Errorf("invalid playback event")
	}
	if in.EventKind != "progress" && in.EventKind != "completed" && in.EventKind != "unwatched" {
		return out, fmt.Errorf("invalid playback kind")
	}
	p, err := s.VideoProfileID(profile)
	if err != nil {
		return out, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	out, err = playbackState(tx, id, p)
	if err != nil {
		return out, err
	}
	conflict := func(code string) (domain.VideoPlaybackState, error) { return out, &VideoPlaybackConflict{code, out} }
	raw, _ := json.Marshal(in)
	hash := fmt.Sprintf("%x", sha256.Sum256(append([]byte(fmt.Sprintf("%d:", id)), raw...)))
	var savedHash, savedJSON string
	err = tx.QueryRow(`SELECT request_hash,response_json FROM video_playback_events WHERE profile_id=? AND event_id=?`, p, in.EventID).Scan(&savedHash, &savedJSON)
	if err == nil {
		if hash != savedHash {
			return conflict("playback-event-conflict")
		}
		err = json.Unmarshal([]byte(savedJSON), &out)
		return out, err
	}
	if err != sql.ErrNoRows {
		return out, err
	}
	if in.MediaKey != out.MediaKey {
		return conflict("playback-identity-conflict")
	}
	var seq int64
	err = tx.QueryRow(`SELECT sequence FROM video_playback_sessions WHERE profile_id=? AND video_id=? AND session_id=?`, p, id, in.SessionID).Scan(&seq)
	if err != nil && err != sql.ErrNoRows {
		return out, err
	}
	if in.ClientSequence <= seq {
		return conflict("playback-sequence-conflict")
	}
	if in.BaseRevision != out.Revision {
		return conflict("playback-revision-conflict")
	}
	out.PositionSeconds = in.PositionSeconds
	out.DurationSeconds = in.DurationSeconds
	out.Revision++
	out.IdentityChanged = false
	out.ServerUpdatedAt = time.Now().Unix()
	if in.EventKind == "completed" {
		out.Completed = true
	}
	if in.EventKind == "unwatched" {
		out.Completed = false
		out.PositionSeconds = 0
	}
	if _, err = tx.Exec(`INSERT INTO video_playback_state(profile_id,video_id,media_key,revision,position_seconds,duration_seconds,completed,updated_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(profile_id,video_id) DO UPDATE SET media_key=excluded.media_key,revision=excluded.revision,position_seconds=excluded.position_seconds,duration_seconds=excluded.duration_seconds,completed=excluded.completed,updated_at=excluded.updated_at`, p, id, out.MediaKey, out.Revision, out.PositionSeconds, out.DurationSeconds, out.Completed, out.ServerUpdatedAt); err != nil {
		return out, err
	}
	if _, err = tx.Exec(`INSERT INTO video_playback_sessions VALUES(?,?,?,?) ON CONFLICT(profile_id,video_id,session_id) DO UPDATE SET sequence=excluded.sequence`, p, id, in.SessionID, in.ClientSequence); err != nil {
		return out, err
	}
	response, _ := json.Marshal(out)
	if _, err = tx.Exec(`INSERT INTO video_playback_events VALUES(?,?,?,?,?)`, p, in.EventID, id, hash, string(response)); err != nil {
		return out, err
	}
	return out, tx.Commit()
}

// One bounded page query plus count. No per-card state or metadata requests.
func (s *Store) VideoPlaybackFeed(profile, library int64, kind string, limit, offset int) (domain.VideoPage[domain.VideoPlaybackItem], error) {
	limit, offset = videoPage(limit, offset)
	page := domain.VideoPage[domain.VideoPlaybackItem]{Items: []domain.VideoPlaybackItem{}, Limit: limit, Offset: offset}
	p, err := s.VideoProfileID(profile)
	if err != nil {
		return page, err
	}
	cte := `WITH files AS (SELECT a.id video_id,a.library_id,COALESCE(v.work_id,0) work_id,a.title,
 ss.number season,e.number episode,e.id episode_id,` + videoMediaKeySQL + ` media_key
 ` + videoIdentityFrom + ` LEFT JOIN video_seasons ss ON ss.id=e.season_id),
 valid AS (SELECT f.*,p.position_seconds,p.duration_seconds,p.completed,p.revision,p.updated_at
 FROM files f JOIN video_playback_state p ON p.video_id=f.video_id AND p.media_key=f.media_key WHERE p.profile_id=?),
 watched AS (SELECT *,ROW_NUMBER() OVER(PARTITION BY work_id ORDER BY updated_at DESC,revision DESC,video_id DESC) rank FROM valid WHERE completed=1 AND season>0),
 candidates AS (SELECT f.*,ROW_NUMBER() OVER(PARTITION BY f.work_id ORDER BY f.season,f.episode,f.video_id) rank,w.updated_at
 FROM watched w JOIN files f ON f.work_id=w.work_id WHERE w.rank=1 AND f.season>0 AND (f.season>w.season OR (f.season=w.season AND f.episode>w.episode))
 AND NOT EXISTS(SELECT 1 FROM valid x WHERE x.episode_id=f.episode_id AND x.completed=1)),
 feed AS (`
	if kind == "continue" {
		cte += `SELECT video_id,work_id,title,season,episode,media_key,position_seconds,duration_seconds,completed,revision,updated_at,library_id FROM valid WHERE completed=0 AND position_seconds>0`
	} else if kind == "next-up" {
		cte += `SELECT c.video_id,c.work_id,c.title,c.season,c.episode,c.media_key,COALESCE(p.position_seconds,0),COALESCE(p.duration_seconds,0),0,COALESCE(p.revision,0),c.updated_at,c.library_id FROM candidates c LEFT JOIN valid p ON p.video_id=c.video_id WHERE c.rank=1`
	} else {
		return page, fmt.Errorf("invalid feed kind")
	}
	// Explicit names ensure identical column names for both unions/query variants.
	cte += `) `
	where := ` FROM feed WHERE (?=0 OR library_id=?)`
	args := []any{p, library, library}
	if err = s.db.QueryRow(cte+`SELECT COUNT(*)`+where, args...).Scan(&page.Total); err != nil {
		return page, err
	}
	rows, err := s.db.Query(cte+`SELECT *`+where+` ORDER BY updated_at DESC,video_id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var item domain.VideoPlaybackItem
		var lib int64
		if err = rows.Scan(&item.VideoID, &item.WorkID, &item.Title, &item.Season, &item.Episode, &item.MediaKey, &item.PositionSeconds, &item.DurationSeconds, &item.Completed, &item.Revision, &item.ServerUpdatedAt, &lib); err != nil {
			return page, err
		}
		item.ProfileID = p
		item.PosterURL = fmt.Sprintf("/api/videos/%d/artwork/poster", item.VideoID)
		item.Reason = "unfinished-file"
		if kind == "next-up" {
			item.Reason = "next-available-episode"
		}
		page.Items = append(page.Items, item)
	}
	page.HasMore = offset+len(page.Items) < page.Total
	return page, rows.Err()
}
