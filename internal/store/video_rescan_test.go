package store

import (
	"database/sql"
	"fmt"
	"foliospace-reader/internal/domain"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type videoRescanLink struct {
	work, version, position int64
	episode                 sql.NullInt64
}

func rescanLink(t *testing.T, c *sql.DB, id int64) videoRescanLink {
	t.Helper()
	var link videoRescanLink
	if err := c.QueryRow(`SELECT v.work_id,l.version_id,l.episode_id,l.position FROM video_file_links l JOIN video_versions v ON v.id=l.version_id WHERE l.video_id=?`, id).
		Scan(&link.work, &link.version, &link.episode, &link.position); err != nil {
		t.Fatal(err)
	}
	return link
}

func TestVideoLocalRescanPreservesPlaybackAndOverrides(t *testing.T) {
	for _, kind := range []string{"movie", "episode"} {
		t.Run(kind, func(t *testing.T) {
			s, c, v := videoFixture(t)
			season, episode := 1, 2
			local := domain.VideoLocalMetadata{Kind: kind, Title: "Original", Year: 2001, Overview: "Original description"}
			if kind == "episode" {
				local.ShowTitle, local.Season, local.Episode = "Original show", &season, &episode
			}
			if err := s.SaveVideoLocalMetadata(v.ID, "before", local); err != nil {
				t.Fatal(err)
			}
			for field, value := range map[string]string{"title": `"My title"`, "year": `1999`, "overview": `"My description"`} {
				if err := s.SetVideoMetadataOverride(v.ID, field, value); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := c.Exec(`UPDATE video_file_links SET position=3 WHERE video_id=?`, v.ID); err != nil {
				t.Fatal(err)
			}
			beforeLink := rescanLink(t, c, v.ID)
			beforeMetadata, err := s.VideoMetadata(v.ID)
			if err != nil {
				t.Fatal(err)
			}
			profile, err := s.CreateProfile("Other viewer", "", "")
			if err != nil {
				t.Fatal(err)
			}
			beforeStates := []domain.VideoPlaybackState{
				playbackEvent(t, s, v.ID, 0, "resume", "progress", 123),
				playbackEvent(t, s, v.ID, profile.ID, "watched", "completed", 1000),
			}
			local.Title, local.ShowTitle, local.Year, local.Overview = "Corrected title", "Corrected show", 2002, "Corrected description"
			for _, revision := range []string{"after", "after-again"} {
				if err := s.SaveVideoLocalMetadata(v.ID, revision, local); err != nil {
					t.Fatal(err)
				}
				if afterLink := rescanLink(t, c, v.ID); afterLink != beforeLink {
					t.Fatalf("rescan replaced link: before=%+v after=%+v", beforeLink, afterLink)
				}
				for _, before := range beforeStates {
					after, err := s.VideoPlaybackState(v.ID, before.ProfileID)
					if err != nil || after != before {
						t.Fatalf("rescan lost playback: before=%+v after=%+v err=%v", before, after, err)
					}
				}
			}
			afterMetadata, err := s.VideoMetadata(v.ID)
			if err != nil || afterMetadata.Title != "My title" || afterMetadata.Year != 1999 || afterMetadata.Overview != "My description" ||
				afterMetadata.WorkID != beforeMetadata.WorkID || afterMetadata.Generation != beforeMetadata.Generation+2 || afterMetadata.Status != "local" ||
				!reflect.DeepEqual(afterMetadata.LockedFields, beforeMetadata.LockedFields) {
				t.Fatalf("rescan changed manual metadata: %+v err=%v", afterMetadata, err)
			}
			var title, overview, identity string
			var year int
			if err = c.QueryRow(`SELECT w.title,w.year,d.overview,d.identity_key FROM video_works w JOIN video_work_details d ON d.work_id=w.id WHERE w.id=?`, beforeLink.work).
				Scan(&title, &year, &overview, &identity); err != nil {
				t.Fatal(err)
			}
			wantTitle, wantKind, identityYear := local.Title, "movie", local.Year
			if kind == "episode" {
				wantTitle, wantKind, identityYear = local.ShowTitle, "series", 0
			}
			wantIdentity := fmt.Sprintf("%d/local/%s/%s/%d", v.LibraryID, wantKind, strings.ToLower(wantTitle), identityYear)
			if title != wantTitle || year != local.Year || overview != local.Overview || identity != wantIdentity {
				t.Fatalf("local work was not refreshed: title=%q year=%d overview=%q identity=%q", title, year, overview, identity)
			}
			feed, err := s.VideoPlaybackFeed(beforeStates[0].ProfileID, v.LibraryID, "continue", 10, 0)
			if err != nil || feed.Total != 1 || feed.Items[0].PositionSeconds != 123 {
				t.Fatalf("resume feed lost rescan item: %+v err=%v", feed, err)
			}
			// Stable grouping must not hide an actual replacement of the media file.
			if _, err = c.Exec(`UPDATE videos SET size=size+1 WHERE id=?`, v.ID); err != nil {
				t.Fatal(err)
			}
			after, err := s.VideoPlaybackState(v.ID, beforeStates[0].ProfileID)
			if err != nil || !after.IdentityChanged || after.PositionSeconds != 0 {
				t.Fatalf("changed content reused old progress: %+v err=%v", after, err)
			}
		})
	}
}

func TestVideoLocalRescanPreservesAmbiguousGrouping(t *testing.T) {
	for _, shared := range []bool{false, true} {
		for _, confirmed := range []bool{false, true} {
			t.Run(fmt.Sprintf("shared=%v/confirmed=%v", shared, confirmed), func(t *testing.T) {
				s, c, v := videoFixture(t)
				local := domain.VideoLocalMetadata{Kind: "movie", Title: "Original", Year: 2001, Overview: "Original description"}
				if err := s.SaveVideoLocalMetadata(v.ID, "before", local); err != nil {
					t.Fatal(err)
				}
				peer, err := s.UpsertVideo(domain.VideoAsset{LibraryID: v.LibraryID, FilePath: filepath.Join(filepath.Dir(v.FilePath), "other.mkv"), Title: "Other", Format: "mkv", MTime: v.MTime})
				if err != nil {
					t.Fatal(err)
				}
				peerLocal := local
				if !shared {
					peerLocal.Title, peerLocal.Year = "Corrected", 2002
				}
				if err = s.SaveVideoLocalMetadata(peer.ID, "peer", peerLocal); err != nil {
					t.Fatal(err)
				}
				if confirmed {
					metadata, err := s.VideoMetadata(peer.ID)
					if err != nil {
						t.Fatal(err)
					}
					if err = s.MatchVideo(peer.ID, domain.VideoMatchInput{WorkID: metadata.WorkID, Generation: metadata.Generation}); err != nil {
						t.Fatal(err)
					}
				}
				beforeLink, peerLink := rescanLink(t, c, v.ID), rescanLink(t, c, peer.ID)
				before := playbackEvent(t, s, v.ID, 0, "resume", "progress", 123)
				peerBefore := playbackEvent(t, s, peer.ID, 0, "peer-resume", "progress", 456)
				local.Title, local.Year, local.Overview = "Corrected", 2002, "Changed description"
				if err = s.SaveVideoLocalMetadata(v.ID, "after", local); err != nil {
					t.Fatal(err)
				}
				if rescanLink(t, c, v.ID) != beforeLink || rescanLink(t, c, peer.ID) != peerLink {
					t.Fatal("ambiguous rescan regrouped files")
				}
				for _, expected := range []domain.VideoPlaybackState{before, peerBefore} {
					after, err := s.VideoPlaybackState(expected.VideoID, expected.ProfileID)
					if err != nil || after != expected {
						t.Fatalf("ambiguous rescan changed playback: %+v err=%v", after, err)
					}
				}
				var title, overview string
				var year int
				if err = c.QueryRow(`SELECT w.title,w.year,d.overview FROM video_works w JOIN video_work_details d ON d.work_id=w.id WHERE w.id=?`, beforeLink.work).
					Scan(&title, &year, &overview); err != nil || title != "Original" || year != 2001 || overview != "Original description" {
					t.Fatalf("ambiguous rescan changed original work: %q %d %q err=%v", title, year, overview, err)
				}
				metadata, err := s.VideoMetadata(peer.ID)
				if err != nil || (confirmed && metadata.Status != "confirmed") {
					t.Fatalf("rescan changed peer confirmation: %+v err=%v", metadata, err)
				}
			})
		}
	}
}

func TestVideoLocalRescanPreservesGroupingUntilConfirmed(t *testing.T) {
	for _, change := range []string{"kind", "season", "episode", "unknown"} {
		t.Run(change, func(t *testing.T) {
			s, c, v := videoFixture(t)
			season, episode := 1, 2
			local := domain.VideoLocalMetadata{Kind: "episode", Title: "Episode", ShowTitle: "Show", Season: &season, Episode: &episode}
			if err := s.SaveVideoLocalMetadata(v.ID, "before", local); err != nil {
				t.Fatal(err)
			}
			beforeLink := rescanLink(t, c, v.ID)
			beforeState := playbackEvent(t, s, v.ID, 0, "resume", "progress", 123)
			switch change {
			case "kind":
				local.Kind, local.Season, local.Episode = "movie", nil, nil
			case "season":
				season = 2
			case "episode":
				episode = 3
			case "unknown":
				local.Kind = "unknown"
			}
			if err := s.SaveVideoLocalMetadata(v.ID, "after", local); err != nil {
				t.Fatal(err)
			}
			after, err := s.VideoPlaybackState(v.ID, 0)
			if err != nil || after != beforeState || rescanLink(t, c, v.ID) != beforeLink {
				t.Fatalf("rescan changed grouping or progress without confirmation: %+v err=%v", after, err)
			}
			metadata, err := s.VideoMetadata(v.ID)
			if err != nil || metadata.Status != "local" || metadata.Kind != local.Kind {
				t.Fatalf("rescan failed to preserve observation and existing status: %+v err=%v", metadata, err)
			}
			if err = s.MatchVideo(v.ID, domain.VideoMatchInput{Title: "Confirmed correction", Kind: "movie", Generation: metadata.Generation}); err != nil {
				t.Fatal(err)
			}
			after, err = s.VideoPlaybackState(v.ID, 0)
			if err != nil || !after.IdentityChanged || after.PositionSeconds != 0 {
				t.Fatalf("explicit reassociation reused old progress: %+v err=%v", after, err)
			}
		})
	}
}

func TestVideoLocalRescanPreservesProtectedAssociation(t *testing.T) {
	for _, status := range []string{"confirmed", "unlinked"} {
		t.Run(status, func(t *testing.T) {
			s, _, v := videoFixture(t)
			if err := s.MatchVideo(v.ID, domain.VideoMatchInput{Title: "Chosen", Kind: "movie", Year: 2001}); err != nil {
				t.Fatal(err)
			}
			if status == "unlinked" {
				metadata, err := s.VideoMetadata(v.ID)
				if err != nil {
					t.Fatal(err)
				}
				if err = s.UnlinkVideo(v.ID, metadata.Generation); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.SetVideoMetadataOverride(v.ID, "title", `"My choice"`); err != nil {
				t.Fatal(err)
			}
			beforeMetadata, err := s.VideoMetadata(v.ID)
			if err != nil {
				t.Fatal(err)
			}
			beforeState := playbackEvent(t, s, v.ID, 0, "resume", "progress", 123)
			for _, kind := range []string{"movie", "unknown"} {
				if err = s.SaveVideoLocalMetadata(v.ID, kind, domain.VideoLocalMetadata{Kind: kind, Title: "Observed", Year: 2002}); err != nil {
					t.Fatal(err)
				}
				afterMetadata, err := s.VideoMetadata(v.ID)
				if err != nil || afterMetadata.WorkID != beforeMetadata.WorkID || afterMetadata.Status != status || afterMetadata.Title != "My choice" {
					t.Fatalf("protected metadata changed: %+v err=%v", afterMetadata, err)
				}
				afterState, err := s.VideoPlaybackState(v.ID, beforeState.ProfileID)
				if err != nil || afterState != beforeState {
					t.Fatalf("protected playback changed: %+v err=%v", afterState, err)
				}
			}
		})
	}
}
