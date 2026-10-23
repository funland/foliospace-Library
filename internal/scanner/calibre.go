package scanner

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"foliospace-reader/internal/bookmeta"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/store"
)

func importedBookMetadata(path string, external bookmeta.Metadata) bookMetadata {
	title := external.Title
	if strings.TrimSpace(title) == "" {
		title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	ext := strings.ToLower(filepath.Ext(path))
	data, _ := json.Marshal(external)
	sum := sha256.Sum256(data)
	return bookMetadata{Title: title, Creator: external.Creator, Description: external.Description, Tags: external.Tags, DirectoryCollection: ext != ".epub" && ext != ".pdf", SourceRevision: hex.EncodeToString(sum[:])}
}

type importedBookState struct {
	Revision        string   `json:"revision"`
	Title           string   `json:"title"`
	Version         int      `json:"version,omitempty"`
	Creator         string   `json:"creator,omitempty"`
	Description     string   `json:"description,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	SeriesTitle     string   `json:"seriesTitle,omitempty"`
	PreservedFields []string `json:"preservedFields,omitempty"`
	LegacyBaseline  string   `json:"legacyBaseline,omitempty"`
}

func importedBookStateKey(bookID int64) string { return fmt.Sprintf("book-local-metadata:%d", bookID) }

func (state importedBookState) preserves(field string) bool {
	for _, preserved := range state.PreservedFields {
		if preserved == field {
			return true
		}
	}
	return false
}

func (state *importedBookState) preserve(field string) {
	if !state.preserves(field) {
		state.PreservedFields = append(state.PreservedFields, field)
	}
}

func (s *Scanner) importedBookUnchanged(existing store.FileIndex, metadata bookMetadata, seriesTitle string) bool {
	if metadata.SourceRevision == "" {
		return false
	}
	raw, err := s.store.Setting(importedBookStateKey(existing.Book.ID))
	var state importedBookState
	if err != nil || json.Unmarshal([]byte(raw), &state) != nil || state.Version != 2 || state.Revision != metadata.SourceRevision {
		return false
	}
	shared, err := s.store.BookSharedMetadata(existing.Book.ID)
	return err == nil &&
		(state.preserves("title") || shared.Title == state.Title) &&
		(state.preserves("creator") || shared.Creator == state.Creator) &&
		(state.preserves("description") || shared.Description == state.Description) &&
		(state.preserves("tags") || sameStringList(shared.Tags, state.Tags)) &&
		(state.preserves("series") || (existing.Book.CollectionTitle == state.SeriesTitle && seriesTitle == state.SeriesTitle))
}

// The baseline always describes automatic input, never the preserved user value.
// Persistent preserved fields also prevent a later coincidentally equal source
// value from taking ownership of a user's edit.
func (s *Scanner) mergeImportedBookMetadata(library domain.Library, path, relPath, ext string, existing store.FileIndex, incoming bookMetadata) (bookMetadata, *importedBookState, error) {
	current, err := s.store.BookSharedMetadata(existing.Book.ID)
	if err != nil {
		return incoming, nil, err
	}
	var old importedBookState
	raw, err := s.store.Setting(importedBookStateKey(existing.Book.ID))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return incoming, nil, err
	}
	_ = json.Unmarshal([]byte(raw), &old)
	legacy := old.Version != 2
	baseline := bookMetadata{Title: old.Title, Creator: old.Creator, Description: old.Description, Tags: old.Tags}
	baselineKnown := true
	filenameBaseline := false
	if legacy {
		baseline, err = bookMetadataForPath(path, ext)
		baselineKnown = err == nil
		if !baselineKnown {
			baseline = bookMetadata{}
		}
		if old.Revision != "" && old.Revision == incoming.SourceRevision {
			baseline = incoming
			baselineKnown = true
		}
		if old.Title != "" {
			baseline.Title = old.Title
		}
		// Early scans indexed only the filename and directory, even when the
		// embedded PDF metadata was readable. Recognize that complete generated
		// shape; a custom title, author, description, tags or collection opts out.
		fallback := bookMetadata{Title: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))}
		fallbackSeries, _ := bookSeriesIdentity(library, relPath, fallback)
		if old.Revision == "" && current.Title == fallback.Title && current.Creator == "" && current.Description == "" && len(current.Tags) == 0 && existing.Book.CollectionTitle == fallbackSeries {
			baseline, baselineKnown, filenameBaseline = fallback, true, true
		}
		old.SeriesTitle, _ = bookSeriesIdentity(library, relPath, baseline)
	}
	seriesTitle, _ := bookSeriesIdentity(library, relPath, incoming)
	state := &importedBookState{Version: 2, Revision: incoming.SourceRevision, Title: incoming.Title,
		Creator: incoming.Creator, Description: incoming.Description, Tags: append([]string(nil), incoming.Tags...),
		SeriesTitle: seriesTitle, PreservedFields: append([]string(nil), old.PreservedFields...), LegacyBaseline: old.LegacyBaseline}
	if legacy {
		state.LegacyBaseline = "embedded"
		if filenameBaseline {
			state.LegacyBaseline = "filename-fallback"
		}
		if !baselineKnown {
			state.LegacyBaseline = "unavailable-preserve-nonempty"
		}
	}
	merged := incoming
	mergeText := func(field, value, previous, next string) string {
		if state.preserves(field) || ((!legacy || baselineKnown || value != "") && (!baselineKnown || value != previous)) {
			state.preserve(field)
			return value
		}
		return next
	}
	// Older scanners may already have disambiguated an automatically parsed title.
	if legacy && baselineKnown && current.Title == disambiguatedTitle(baseline.Title, relPath) {
		baseline.Title = current.Title
	}
	merged.Title = mergeText("title", current.Title, baseline.Title, incoming.Title)
	merged.Creator = mergeText("creator", current.Creator, baseline.Creator, incoming.Creator)
	merged.Description = mergeText("description", current.Description, baseline.Description, incoming.Description)
	if state.preserves("tags") || ((!legacy || baselineKnown || len(current.Tags) != 0) && (!baselineKnown || !sameStringList(current.Tags, baseline.Tags))) {
		state.preserve("tags")
		merged.Tags = current.Tags
	}
	if state.preserves("title") || state.preserves("creator") || existing.Book.CollectionTitle != old.SeriesTitle {
		state.preserve("series")
	}
	return merged, state, nil
}

func (s *Scanner) rememberImportedBook(book domain.Book, metadata bookMetadata, state *importedBookState) error {
	if metadata.SourceRevision == "" {
		return nil
	}
	if state == nil {
		state = &importedBookState{Version: 2, Revision: metadata.SourceRevision, Creator: metadata.Creator, Description: metadata.Description, Tags: metadata.Tags}
	}
	if !state.preserves("title") {
		state.Title = book.Title
	}
	if !state.preserves("series") {
		state.SeriesTitle = book.CollectionTitle
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return s.store.UpsertSetting(importedBookStateKey(book.ID), string(data))
}

func (s *Scanner) rememberImportedBookTitle(bookID int64, title string) error {
	raw, err := s.store.Setting(importedBookStateKey(bookID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var state importedBookState
	if json.Unmarshal([]byte(raw), &state) != nil || state.Revision == "" || state.Title == title || state.preserves("title") {
		return nil
	}
	state.Title = title
	data, _ := json.Marshal(state)
	return s.store.UpsertSetting(importedBookStateKey(bookID), string(data))
}

func (s *Scanner) recordBookMetadataWarning(jobID int64, scope scanScope, err error) {
	if scope.metadataWarnings != nil {
		if _, loaded := scope.metadataWarnings.LoadOrStore(err.Error(), true); loaded {
			return
		}
	}
	_ = s.store.AddJobEvent(jobID, "warning", "local book metadata: "+err.Error())
}

func (s *Scanner) refreshRecentBookMetadata(library domain.Library, job *domain.ScanJob, scope scanScope, indexes map[string]store.FileIndex) error {
	paths := make([]string, 0, len(indexes))
	for path := range indexes {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := s.applyScanControl(job); err != nil {
			return err
		}
		rel, err := filepath.Rel(scope.rootPath, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		ext := strings.ToLower(filepath.Ext(path))
		if _, unchanged := s.unchangedMetadataOnlyFile(indexes, path, info, ext); !unchanged {
			continue
		}
		metadata, found, metadataErr := scope.bookMetadata.Lookup(path)
		if metadataErr != nil {
			s.recordBookMetadataWarning(job.ID, scope, metadataErr)
		}
		if !found {
			continue
		}
		result, err := s.indexResolvedFileMetadata(library, path, info, ext, importedBookMetadata(path, metadata))
		if err != nil {
			job.ErrorCount++
			_ = s.store.AddJobEvent(job.ID, "error", "local book metadata update failed: "+err.Error())
			continue
		}
		if result.MetadataUpdated {
			job.MetadataUpdatedFiles++
		}
		if result.Reclassified {
			job.ReclassifiedFiles++
		}
	}
	return s.store.UpdateScanJob(*job)
}
