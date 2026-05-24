package scanner

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"foliospace-reader/internal/archive"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/store"
)

type Scanner struct {
	store *store.Store
}

func New(store *store.Store) *Scanner {
	return &Scanner{store: store}
}

func (s *Scanner) ScanLibrary(library domain.Library) (domain.ScanJob, error) {
	job, err := s.store.StartScanJob(library.ID)
	if err != nil {
		return job, err
	}
	_ = s.store.AddJobEvent(job.ID, "info", "scan started")

	walkErr := filepath.WalkDir(library.RootPath, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			job.ErrorCount++
			_ = s.recordPathError(library.ID, job.ID, path, classifyWalkError(walkErr), walkErr.Error())
			return nil
		}
		if entry.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".cbz" && ext != ".zip" {
			return nil
		}
		job.DiscoveredFiles++

		info, err := entry.Info()
		if err != nil {
			job.ErrorCount++
			_ = s.recordPathError(library.ID, job.ID, path, domain.ErrorUnknownIO, err.Error())
			return nil
		}
		if info.Size() == 0 {
			job.ErrorCount++
			_ = s.recordPathError(library.ID, job.ID, path, domain.ErrorEmptyFile, "empty file")
			return nil
		}

		if err := s.indexFile(library, job.ID, path, info, ext); err != nil {
			job.ErrorCount++
			_ = s.recordPathError(library.ID, job.ID, path, domain.ErrorArchiveOpenFailed, err.Error())
			return nil
		}
		job.IndexedFiles++
		return nil
	})
	if walkErr != nil {
		job.Status = "failed"
		job.FinishedAt = time.Now()
		_ = s.store.UpdateScanJob(job)
		return job, walkErr
	}

	job.Status = "completed"
	job.FinishedAt = time.Now()
	if err := s.store.UpdateScanJob(job); err != nil {
		return job, err
	}
	_ = s.store.AddJobEvent(job.ID, "info", "scan completed")
	return job, nil
}

func (s *Scanner) indexFile(library domain.Library, jobID int64, path string, info fs.FileInfo, ext string) error {
	relPath, err := filepath.Rel(library.RootPath, path)
	if err != nil {
		return fmt.Errorf("relative path: %w", err)
	}

	seriesTitle := seriesTitleForRelPath(relPath)
	bookTitle := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	format := strings.TrimPrefix(ext, ".")

	series, err := s.store.UpsertSeries(library.ID, seriesTitle)
	if err != nil {
		return err
	}
	book, err := s.store.UpsertBook(series.ID, bookTitle, format)
	if err != nil {
		return err
	}
	_, err = s.store.UpsertFile(book.ID, library.ID, path, relPath, info.Size(), info.ModTime(), ext)
	if err != nil {
		return err
	}

	pages, err := archive.ListPages(path)
	if err != nil {
		return err
	}
	if err := s.store.ReplacePages(book.ID, pages); err != nil {
		return err
	}
	return nil
}

func (s *Scanner) recordPathError(libraryID int64, jobID int64, path string, code domain.ErrorCode, message string) error {
	return s.store.RecordFileError(domain.FileErrorInput{
		LibraryID: libraryID,
		JobID:     jobID,
		Path:      path,
		Code:      code,
		Message:   message,
	})
}

func seriesTitleForRelPath(relPath string) string {
	dir := filepath.Dir(relPath)
	if dir == "." || dir == "/" {
		return "Unsorted"
	}
	return filepath.Base(dir)
}

func classifyWalkError(err error) domain.ErrorCode {
	if strings.Contains(strings.ToLower(err.Error()), "permission") {
		return domain.ErrorPermissionDenied
	}
	return domain.ErrorUnknownIO
}
