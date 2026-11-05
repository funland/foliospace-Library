package service

import (
	"database/sql"
	"errors"
	"strings"

	"foliospace-reader/internal/domain"
)

var (
	ErrDiagnosticInvalidArgument = errors.New("invalid diagnostic argument")
	ErrDiagnosticProfileNotFound = errors.New("diagnostic profile not found")
)

// Aliases keep the service contract while the standalone MCP depends only on DTOs.
type BookDiagnostic = domain.BookDiagnostic
type BookDiagnosticHealth = domain.BookDiagnosticHealth
type BookDiagnosticIndex = domain.BookDiagnosticIndex
type GameDiagnostic = domain.GameDiagnostic
type GameDiagnosticFiles = domain.GameDiagnosticFiles
type GameDiagnosticLaunch = domain.GameDiagnosticLaunch

func (s *Service) diagnosticProfileID(profileID int64) (int64, error) {
	if profileID < 0 {
		return 0, ErrDiagnosticInvalidArgument
	}
	if profileID == 0 {
		profile, err := s.store.DefaultProfile()
		return profile.ID, err
	}
	if _, err := s.store.ProfileByID(profileID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrDiagnosticProfileNotFound
		}
		return 0, err
	}
	return profileID, nil
}

func (s *Service) BookDiagnostics(bookID, profileID int64) (BookDiagnostic, error) {
	var out BookDiagnostic
	if bookID <= 0 {
		return out, ErrDiagnosticInvalidArgument
	}
	profileID, err := s.diagnosticProfileID(profileID)
	if err != nil {
		return out, err
	}
	book, err := s.store.BookByIDForProfile(bookID, profileID)
	if err != nil {
		return out, err
	}
	pages, err := s.store.ListPages(bookID)
	if err != nil {
		return out, err
	}
	out = BookDiagnostic{
		BookID: book.ID, ProfileID: profileID, Source: "stored",
		Health: BookDiagnosticHealth{Status: "unknown"},
		Index:  BookDiagnosticIndex{Analyzed: book.Analyzed, DeclaredPages: book.PageCount, IndexedPages: len(pages), Status: "unknown"},
		Issues: []string{},
	}
	// Absence of a recorded problem is not evidence of a currently healthy file.
	if book.FileErrorCode != "" {
		out.Health.Code = diagnosticEnum(book.FileErrorCode, "stored-file-issue", "empty_file", "zero_filled_file", "invalid_book_container", "invalid_epub", "archive_open_failed", "archive_empty")
		if out.Health.Code != "stored-file-issue" {
			out.Health.Status = "damaged"
		}
		out.Issues = append(out.Issues, "stored-file-issue")
	}
	if !book.Analyzed {
		out.Issues = append(out.Issues, "analysis-not-recorded")
	} else if len(pages) == 0 {
		out.Index.Status = "incomplete"
		out.Issues = append(out.Issues, "page-index-empty")
	} else if book.PageCount != len(pages) {
		out.Index.Status = "incomplete"
		out.Issues = append(out.Issues, "page-count-mismatch")
	} else {
		out.Index.Status = "indexed"
	}
	return out, nil
}

func (s *Service) GameDiagnostics(gameID, profileID int64) (GameDiagnostic, error) {
	var out GameDiagnostic
	if gameID <= 0 {
		return out, ErrDiagnosticInvalidArgument
	}
	profileID, err := s.diagnosticProfileID(profileID)
	if err != nil {
		return out, err
	}
	game, err := s.store.GameByIDForProfile(gameID, profileID)
	if err != nil {
		return out, err
	}
	files, err := s.store.GameFiles(gameID)
	if err != nil {
		return out, err
	}
	profiles, err := s.store.GameLaunchProfiles(gameID)
	if err != nil {
		return out, err
	}
	stats, err := s.store.GameCurationStats([]int64{gameID})
	if err != nil {
		return out, err
	}
	stat := stats[gameID]
	out = GameDiagnostic{
		GameID: game.ID, ProfileID: profileID, Source: "stored",
		CatalogRole:    diagnosticEnum(game.CatalogRole, "unknown", "game", "dependency", "needs-curation"),
		Files:          GameDiagnosticFiles{Count: len(files), Checksummed: stat.Checksummed, ChecksumPending: len(files) - stat.Checksummed},
		Launch:         GameDiagnosticLaunch{Status: "unknown", ReadyProfiles: len(profiles)},
		MetadataStatus: diagnosticEnum(stat.MetadataStatus, "unknown", "matched", "unmatched"),
		ArtworkStatus:  diagnosticEnum(stat.ArtworkStatus, "unknown", "ready", "missing"),
		Issues:         []string{},
	}
	// Ready means a stored audited profile exists, not a live runtime check.
	if len(profiles) > 0 {
		out.Launch.Status = "ready"
	} else {
		out.Issues = append(out.Issues, "launch-profile-not-recorded")
	}
	if out.CatalogRole == "dependency" {
		out.Launch.Status = "unavailable"
		out.Issues = append(out.Issues, "dependency-only")
	} else if out.CatalogRole == "needs-curation" {
		out.Issues = append(out.Issues, "curation-required")
	}
	if len(files) == 0 {
		out.Issues = append(out.Issues, "file-index-empty")
	} else if out.Files.ChecksumPending > 0 {
		out.Issues = append(out.Issues, "checksums-incomplete")
	}
	return out, nil
}

func diagnosticEnum(value, fallback string, allowed ...string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, candidate := range allowed {
		if value == candidate {
			return candidate
		}
	}
	return fallback
}
