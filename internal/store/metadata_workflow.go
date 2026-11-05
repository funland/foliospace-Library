package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"foliospace-reader/internal/domain"
	"reflect"
	"strconv"
	"strings"
	"time"
)

var (
	ErrMetadataArguments      = errors.New("metadata-invalid-arguments")
	ErrMetadataProfile        = errors.New("metadata-profile-not-found")
	ErrMetadataNotReady       = errors.New("metadata-job-not-ready")
	ErrMetadataJobStale       = errors.New("metadata-job-stale")
	ErrMetadataPreviewStale   = errors.New("metadata-preview-stale")
	ErrMetadataSharedWork     = errors.New("metadata-shared-work-conflict")
	ErrMetadataReceiptExpired = errors.New("metadata-receipt-expired")
)

type VideoMetadataWorkflowSnapshot struct {
	Preview     domain.VideoMetadataPreview
	Fingerprint string
}

// VideoMetadataWorkflowJob is a bounded-scope read and does not purge caches.
func (s *Store) VideoMetadataWorkflowJob(profileID, videoID, jobID int64) (domain.VideoMetadataJob, error) {
	if profileID <= 0 || videoID <= 0 || jobID <= 0 {
		return domain.VideoMetadataJob{}, ErrMetadataArguments
	}
	tx, err := s.db.Begin()
	if err != nil {
		return domain.VideoMetadataJob{}, err
	}
	defer tx.Rollback()
	var found int64
	if err = tx.QueryRow(`SELECT id FROM profiles WHERE id=?`, profileID).Scan(&found); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrMetadataProfile
		}
		return domain.VideoMetadataJob{}, err
	}
	job, err := scanVideoJob(tx.QueryRow(videoJobSelect+` WHERE id=? AND video_id=?`, jobID, videoID))
	if err != nil {
		return domain.VideoMetadataJob{}, err
	}
	if job.Kind != "movie" && job.Kind != "series" {
		return domain.VideoMetadataJob{}, ErrMetadataArguments
	}
	var generation int64
	if err = tx.QueryRow(`SELECT COALESCE((SELECT generation FROM video_match_state WHERE video_id=?),0)`, videoID).Scan(&generation); err != nil {
		return domain.VideoMetadataJob{}, err
	}
	if generation != job.Generation {
		job.Status = "stale"
	}
	var expired bool
	if err = tx.QueryRow(`SELECT created_at < datetime('now','-150 days') FROM video_metadata_jobs WHERE id=?`, jobID).Scan(&expired); err != nil {
		return domain.VideoMetadataJob{}, err
	}
	if expired {
		return domain.VideoMetadataJob{}, ErrMetadataJobStale
	}
	return job, nil
}

// PreviewVideoMetadataWorkflow uses only SELECTs. In particular, it does not
// initialize match state or purge expired provider caches.
func (s *Store) PreviewVideoMetadataWorkflow(profileID int64, in domain.VideoMetadataPreviewInput) (VideoMetadataWorkflowSnapshot, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return VideoMetadataWorkflowSnapshot{}, err
	}
	defer tx.Rollback()
	snap, _, err := videoMetadataWorkflowSnapshot(tx, profileID, in)
	return snap, err
}

// ApplyVideoMetadataWorkflow repeats every preview check in the same transaction
// that associates the file. Concurrent metadata writes either invalidate the
// fingerprint or cause the transaction to fail, never a stale overwrite.
func (s *Store) ApplyVideoMetadataWorkflow(profileID int64, in domain.VideoMetadataPreviewInput, fingerprint string, expiresAt time.Time) (domain.VideoMetadataApplyResult, error) {
	result := domain.VideoMetadataApplyResult{}
	tx, err := s.db.Begin()
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if !time.Now().Before(expiresAt) {
		return result, ErrMetadataReceiptExpired
	}
	snap, match, err := videoMetadataWorkflowSnapshot(tx, profileID, in)
	if err != nil {
		return result, err
	}
	if fingerprint == "" || snap.Fingerprint != fingerprint {
		return result, ErrMetadataPreviewStale
	}
	if err = matchVideoTx(tx, in.VideoID, match, true, true); err != nil {
		return result, err
	}
	result.VideoID, result.Scope = in.VideoID, "shared-library"
	if err = tx.QueryRow(`SELECT v.work_id,m.generation FROM video_file_links f JOIN video_versions v ON v.id=f.version_id JOIN video_match_state m ON m.video_id=f.video_id WHERE f.video_id=?`, in.VideoID).Scan(&result.WorkID, &result.Generation); err != nil {
		return result, err
	}
	if !time.Now().Before(expiresAt) {
		return domain.VideoMetadataApplyResult{}, ErrMetadataReceiptExpired
	}
	if err = tx.Commit(); err != nil {
		return domain.VideoMetadataApplyResult{}, err
	}
	return result, nil
}

func videoMetadataWorkflowSnapshot(tx *sql.Tx, profileID int64, in domain.VideoMetadataPreviewInput) (VideoMetadataWorkflowSnapshot, domain.VideoMatchInput, error) {
	snap := VideoMetadataWorkflowSnapshot{}
	match := domain.VideoMatchInput{}
	if profileID <= 0 || in.VideoID <= 0 || in.JobID <= 0 || in.CandidateIndex < 0 || in.CandidateIndex > 999 || len(in.Version) > 500 ||
		(in.Season == nil) != (in.Episode == nil) || in.Season != nil && (*in.Season < 0 || *in.Season > 999 || *in.Episode < 1 || *in.Episode > 99999) {
		return snap, match, ErrMetadataArguments
	}
	var profile int64
	if err := tx.QueryRow(`SELECT id FROM profiles WHERE id=?`, profileID).Scan(&profile); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrMetadataProfile
		}
		return snap, match, err
	}
	p := domain.VideoMetadataPreview{ProfileID: profileID, VideoID: in.VideoID, JobID: in.JobID, CandidateIndex: in.CandidateIndex, Scope: "shared-library", Changes: []domain.VideoMetadataChange{}, LockedFields: []string{}}
	var format string
	if err := tx.QueryRow(`SELECT library_id,title,format FROM videos WHERE id=?`, in.VideoID).Scan(&p.LibraryID, &p.Current.Title, &format); err != nil {
		return snap, match, err
	}
	if err := tx.QueryRow(`SELECT COALESCE((SELECT generation FROM video_match_state WHERE video_id=?),0)`, in.VideoID).Scan(&p.Generation); err != nil {
		return snap, match, err
	}
	job, err := scanVideoJob(tx.QueryRow(videoJobSelect+` WHERE id=?`, in.JobID))
	if err != nil {
		return snap, match, err
	}
	if job.VideoID != in.VideoID {
		return snap, match, ErrMetadataArguments
	}
	if job.Generation != p.Generation {
		return snap, match, ErrMetadataJobStale
	}
	if job.Status != "ready" {
		return snap, match, ErrMetadataNotReady
	}
	var expired bool
	if err = tx.QueryRow(`SELECT created_at < datetime('now','-150 days') FROM video_metadata_jobs WHERE id=?`, in.JobID).Scan(&expired); err != nil {
		return snap, match, err
	}
	if expired {
		return snap, match, ErrMetadataJobStale
	}
	if in.CandidateIndex >= len(job.Candidates) {
		return snap, match, ErrMetadataArguments
	}
	candidate := job.Candidates[in.CandidateIndex]
	providerID, parseErr := strconv.ParseInt(candidate.ID, 10, 64)
	if parseErr != nil || providerID <= 0 || strconv.FormatInt(providerID, 10) != candidate.ID ||
		candidate.Kind != job.Kind || (candidate.Kind != "movie" && candidate.Kind != "series") ||
		strings.TrimSpace(candidate.Title) == "" || len(candidate.Title) > 1000 || candidate.Year < 0 || candidate.Year > 9999 || len(candidate.Overview) > 65536 ||
		(candidate.Kind == "series" && in.Season == nil) || (candidate.Kind == "movie" && in.Season != nil) {
		return snap, match, ErrMetadataArguments
	}
	// Local observations supply the pre-match values; an existing association
	// supplies its work values. Overrides below always win, including empty values.
	var localRaw string
	err = tx.QueryRow(`SELECT local_json FROM video_metadata_inputs WHERE video_id=?`, in.VideoID).Scan(&localRaw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return snap, match, err
	}
	if err == nil {
		var local domain.VideoLocalMetadata
		if err = json.Unmarshal([]byte(localRaw), &local); err != nil {
			return snap, match, err
		}
		p.Current.Title, p.Current.Kind, p.Current.Year, p.Current.Overview = local.Title, local.Kind, local.Year, local.Overview
		p.Current.Season, p.Current.Episode = local.Season, local.Episode
	}
	var linkCount int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM video_file_links WHERE video_id=?`, in.VideoID).Scan(&linkCount); err != nil {
		return snap, match, err
	}
	// The first wave deliberately excludes multi-association files.
	if linkCount > 1 {
		return snap, match, ErrMetadataArguments
	}
	err = tx.QueryRow(`SELECT w.id,w.title,w.kind,w.year,d.overview,d.provider,d.provider_id,s.number,e.number,v.label FROM video_file_links f JOIN video_versions v ON v.id=f.version_id JOIN video_works w ON w.id=v.work_id JOIN video_work_details d ON d.work_id=w.id LEFT JOIN video_episodes e ON e.id=f.episode_id LEFT JOIN video_seasons s ON s.id=e.season_id WHERE f.video_id=?`, in.VideoID).Scan(&p.Current.WorkID, &p.Current.Title, &p.Current.Kind, &p.Current.Year, &p.Current.Overview, &p.Current.Provider, &p.Current.ProviderID, &p.Current.Season, &p.Current.Episode, &p.Current.Version)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return snap, match, err
	}
	identity := fmt.Sprintf("%d/tmdb/%s/%s", p.LibraryID, candidate.Kind, candidate.ID)
	p.Proposed = domain.VideoMetadataSelection{Title: strings.TrimSpace(candidate.Title), Kind: candidate.Kind, Year: candidate.Year, Overview: candidate.Overview, Provider: "tmdb", ProviderID: candidate.ID, Season: in.Season, Episode: in.Episode, Version: strings.TrimSpace(in.Version)}
	if p.Proposed.Version == "" {
		p.Proposed.Version = fmt.Sprintf("%s · file %d", strings.ToUpper(format), in.VideoID)
	}
	err = tx.QueryRow(`SELECT w.id,w.title,w.kind,w.year,d.overview,d.provider,d.provider_id FROM video_work_details d JOIN video_works w ON w.id=d.work_id WHERE d.identity_key=? AND w.library_id=?`, identity, p.LibraryID).Scan(&p.Proposed.WorkID, &p.Proposed.Title, &p.Proposed.Kind, &p.Proposed.Year, &p.Proposed.Overview, &p.Proposed.Provider, &p.Proposed.ProviderID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return snap, match, err
	}
	p.ReusesWork = err == nil
	if p.ReusesWork {
		var expired bool
		if err = tx.QueryRow(`SELECT expires_at<=CAST(strftime('%s','now') AS INTEGER) FROM video_work_details WHERE work_id=?`, p.Proposed.WorkID).Scan(&expired); err != nil {
			return snap, match, err
		}
		if expired {
			return snap, match, ErrMetadataJobStale
		}
	}
	if err = metadataWorkflowSharedDisplayGuard(tx, in.VideoID, p.Current.WorkID, p.Proposed.WorkID); err != nil {
		return snap, match, err
	}
	if err = metadataWorkflowSharedEpisodeGuard(tx, in.VideoID, p.Proposed.WorkID, in.Season, in.Episode); err != nil {
		return snap, match, err
	}
	p.PlaybackIdentityMayChange = !p.ReusesWork || p.Current.WorkID != p.Proposed.WorkID || p.Current.Version != p.Proposed.Version || !reflect.DeepEqual(p.Current.Season, p.Proposed.Season) || !reflect.DeepEqual(p.Current.Episode, p.Proposed.Episode)
	rows, err := tx.Query(`SELECT field,value_json FROM video_metadata_overrides WHERE video_id=? ORDER BY field`, in.VideoID)
	if err != nil {
		return snap, match, err
	}
	for rows.Next() {
		var field, value string
		if err = rows.Scan(&field, &value); err != nil {
			rows.Close()
			return snap, match, err
		}
		for _, selection := range []*domain.VideoMetadataSelection{&p.Current, &p.Proposed} {
			m := domain.VideoLocalMetadata{Title: selection.Title, Year: selection.Year, Overview: selection.Overview}
			if err = applyVideoOverride(&m, field, value); err != nil {
				rows.Close()
				return snap, match, err
			}
			selection.Title, selection.Year, selection.Overview = m.Title, m.Year, m.Overview
		}
		p.LockedFields = append(p.LockedFields, field)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return snap, match, err
	}
	rows.Close()
	before, after := reflect.ValueOf(p.Current), reflect.ValueOf(p.Proposed)
	for i := 0; i < before.NumField(); i++ {
		if !reflect.DeepEqual(before.Field(i).Interface(), after.Field(i).Interface()) {
			field := strings.Split(before.Type().Field(i).Tag.Get("json"), ",")[0]
			p.Changes = append(p.Changes, domain.VideoMetadataChange{Field: field, Before: before.Field(i).Interface(), After: after.Field(i).Interface()})
		}
	}
	// Bind the entire candidate job and all current or reused work rows, not just
	// the generation counter: shared metadata can change independently of it.
	queries := []struct {
		sql  string
		args []any
	}{
		{`SELECT * FROM videos WHERE id=?`, []any{in.VideoID}},
		{`SELECT * FROM video_match_state WHERE video_id=?`, []any{in.VideoID}},
		{`SELECT * FROM video_metadata_jobs WHERE id=?`, []any{in.JobID}},
		{`SELECT * FROM video_metadata_inputs WHERE video_id=?`, []any{in.VideoID}},
		{`SELECT * FROM video_metadata_overrides WHERE video_id=? ORDER BY field`, []any{in.VideoID}},
		{`SELECT f.*,v.*,e.*,s.*,w.*,d.* FROM video_file_links f JOIN video_versions v ON v.id=f.version_id JOIN video_works w ON w.id=v.work_id JOIN video_work_details d ON d.work_id=w.id LEFT JOIN video_episodes e ON e.id=f.episode_id LEFT JOIN video_seasons s ON s.id=e.season_id WHERE f.video_id=? ORDER BY f.version_id`, []any{in.VideoID}},
		{`SELECT w.*,d.* FROM video_work_details d JOIN video_works w ON w.id=d.work_id WHERE d.identity_key=?`, []any{identity}},
		// Work cards can use a sibling file's overrides and local observations.
		// Bind those rows, including all sibling associations used by title fallback.
		{`SELECT f.*,v.*,a.*,m.*,o.*,w.*,d.*,e.*,s.* FROM video_file_links f JOIN video_versions v ON v.id=f.version_id JOIN videos a ON a.id=f.video_id JOIN video_works w ON w.id=v.work_id JOIN video_work_details d ON d.work_id=w.id LEFT JOIN video_metadata_inputs m ON m.video_id=a.id LEFT JOIN video_metadata_overrides o ON o.video_id=a.id LEFT JOIN video_episodes e ON e.id=f.episode_id LEFT JOIN video_seasons s ON s.id=e.season_id WHERE a.id IN (SELECT l.video_id FROM video_file_links l JOIN video_versions z ON z.id=l.version_id WHERE z.work_id IN (?,?)) ORDER BY a.id,f.version_id,o.field`, []any{p.Current.WorkID, p.Proposed.WorkID}},
	}
	bound := []any{profileID, in, p}
	for _, query := range queries {
		values, e := metadataWorkflowRows(tx, query.sql, query.args...)
		if e != nil {
			return snap, match, e
		}
		bound = append(bound, values)
	}
	raw, err := json.Marshal(bound)
	if err != nil {
		return snap, match, err
	}
	hash := sha256.Sum256(raw)
	snap.Preview, snap.Fingerprint = p, hex.EncodeToString(hash[:])
	match = domain.VideoMatchInput{Title: strings.TrimSpace(candidate.Title), Kind: candidate.Kind, Year: candidate.Year, Overview: candidate.Overview, Provider: "tmdb", ProviderID: candidate.ID, Generation: p.Generation, Season: in.Season, Episode: in.Episode, Version: in.Version}
	return snap, match, nil
}

type metadataWorkflowDisplay struct {
	Title    string
	Year     int
	Overview string
}

// Episode cards inherit a file's local/override title too. The first wave
// conservatively retains the representative of every episode with siblings.
func metadataWorkflowSharedEpisodeGuard(tx *sql.Tx, videoID, destinationWorkID int64, season, episode *int) error {
	var sourceID, destinationID int64
	if err := tx.QueryRow(`SELECT COALESCE((SELECT episode_id FROM video_file_links WHERE video_id=? LIMIT 1),0)`, videoID).Scan(&sourceID); err != nil {
		return err
	}
	if destinationWorkID > 0 && season != nil && episode != nil {
		if err := tx.QueryRow(`SELECT COALESCE((SELECT e.id FROM video_episodes e JOIN video_seasons s ON s.id=e.season_id WHERE s.work_id=? AND s.number=? AND e.number=?),0)`, destinationWorkID, *season, *episode).Scan(&destinationID); err != nil {
			return err
		}
	}
	for _, episodeID := range []int64{sourceID, destinationID} {
		if episodeID == 0 {
			continue
		}
		var before int64
		var sibling sql.NullInt64
		if err := tx.QueryRow(`SELECT COALESCE(MIN(video_id),0),MIN(CASE WHEN video_id<>? THEN video_id END) FROM video_file_links WHERE episode_id=?`, videoID, episodeID).Scan(&before, &sibling); err != nil {
			return err
		}
		if !sibling.Valid {
			continue
		}
		after := sibling.Int64
		if episodeID == destinationID && videoID < after {
			after = videoID
		}
		if before != after {
			return ErrMetadataSharedWork
		}
	}
	return nil
}

// A work card inherits overrides from its lowest-ID file. Changing a single
// association must not silently change the metadata displayed for its siblings.
func metadataWorkflowSharedDisplayGuard(tx *sql.Tx, videoID, sourceID, destinationID int64) error {
	for _, workID := range []int64{sourceID, destinationID} {
		if workID == 0 {
			continue
		}
		var beforeRepresentative int64
		var siblingRepresentative sql.NullInt64
		if err := tx.QueryRow(`SELECT COALESCE(MIN(f.video_id),0),MIN(CASE WHEN f.video_id<>? THEN f.video_id END) FROM video_file_links f JOIN video_versions v ON v.id=f.version_id WHERE v.work_id=?`, videoID, workID).Scan(&beforeRepresentative, &siblingRepresentative); err != nil {
			return err
		}
		if !siblingRepresentative.Valid {
			continue
		}
		afterRepresentative := siblingRepresentative.Int64
		if workID == destinationID && videoID < afterRepresentative {
			afterRepresentative = videoID
		}
		before, err := metadataWorkflowWorkDisplay(tx, workID, beforeRepresentative, false)
		if err != nil {
			return err
		}
		after, err := metadataWorkflowWorkDisplay(tx, workID, afterRepresentative, workID == destinationID && afterRepresentative == videoID)
		if err != nil {
			return err
		}
		if before != after {
			return ErrMetadataSharedWork
		}
	}
	return nil
}

func metadataWorkflowWorkDisplay(tx *sql.Tx, workID, representative int64, targetAssociation bool) (metadataWorkflowDisplay, error) {
	title := strings.ReplaceAll(videoWorkDisplayTitle, videoWorkRepresentative, "?")
	args := []any{representative}
	if targetAssociation {
		// The new target's work fallback must use its future association, not
		// the old link still present while this read-only preview is computed.
		title = `CASE WHEN w.kind='movie' THEN COALESCE((SELECT json_extract(o.value_json,'$') FROM video_metadata_overrides o WHERE o.video_id=? AND o.field='title'),NULLIF(w.title,''),(SELECT NULLIF(json_extract(m.local_json,'$.title'),'') FROM video_metadata_inputs m WHERE m.video_id=?),(SELECT title FROM videos WHERE id=?)) ELSE w.title END`
		args = []any{representative, representative, representative}
	}
	year := strings.ReplaceAll(videoWorkDisplayYear, videoWorkRepresentative, "?")
	args = append(args, representative, representative, representative, workID)
	query := `SELECT ` + title + `,` + year + `,COALESCE((SELECT json_extract(o.value_json,'$') FROM video_metadata_overrides o WHERE o.video_id=? AND o.field='overview'),d.overview) FROM video_works w JOIN video_work_details d ON d.work_id=w.id WHERE w.id=?`
	var result metadataWorkflowDisplay
	err := tx.QueryRow(query, args...).Scan(&result.Title, &result.Year, &result.Overview)
	return result, err
}

func metadataWorkflowRows(tx *sql.Tx, query string, args ...any) ([][]any, error) {
	rows, err := tx.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := [][]any{}
	for rows.Next() {
		values, refs := make([]any, len(columns)), make([]any, len(columns))
		for i := range values {
			refs[i] = &values[i]
		}
		if err = rows.Scan(refs...); err != nil {
			return nil, err
		}
		result = append(result, values)
	}
	return result, rows.Err()
}
