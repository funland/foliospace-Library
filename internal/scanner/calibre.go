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
	Revision string `json:"revision"`
	Title    string `json:"title"`
}

func importedBookStateKey(bookID int64) string { return fmt.Sprintf("book-local-metadata:%d", bookID) }

func (s *Scanner) importedBookUnchanged(existing store.FileIndex, metadata bookMetadata, seriesTitle string) bool {
	if metadata.SourceRevision == "" || existing.Book.CollectionTitle != seriesTitle {
		return false
	}
	raw, err := s.store.Setting(importedBookStateKey(existing.Book.ID))
	var state importedBookState
	if err != nil || json.Unmarshal([]byte(raw), &state) != nil || state.Revision != metadata.SourceRevision {
		return false
	}
	shared, err := s.store.BookSharedMetadata(existing.Book.ID)
	return err == nil && shared.Title == state.Title && shared.Creator == metadata.Creator && shared.Description == metadata.Description && sameStringList(shared.Tags, metadata.Tags)
}

func (s *Scanner) rememberImportedBook(book domain.Book, metadata bookMetadata) error {
	if metadata.SourceRevision == "" {
		return nil
	}
	data, err := json.Marshal(importedBookState{Revision: metadata.SourceRevision, Title: book.Title})
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
	if json.Unmarshal([]byte(raw), &state) != nil || state.Revision == "" || state.Title == title {
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
