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
	return s.RunScanJob(library, job)
}

func (s *Scanner) StartScanJob(library domain.Library) (domain.ScanJob, error) {
	job, err := s.store.StartScanJob(library.ID)
	if err != nil {
		return job, err
	}
	go func() {
		_, _ = s.RunScanJob(library, job)
	}()
	return job, nil
}

func (s *Scanner) RunScanJob(library domain.Library, job domain.ScanJob) (domain.ScanJob, error) {
	_ = s.store.AddJobEvent(job.ID, "info", "scan started")
	_ = s.store.AddJobEvent(job.ID, "info", "walking "+library.RootPath)

	walkErr := filepath.WalkDir(library.RootPath, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			job.CurrentPath = path
			job.ErrorCount++
			_ = s.recordPathError(library.ID, job.ID, path, classifyWalkError(walkErr), walkErr.Error())
			_ = s.store.AddJobEvent(job.ID, "error", "walk failed: "+path)
			_ = s.store.UpdateScanJob(job)
			return nil
		}
		if entry.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".cbz" && ext != ".zip" {
			return nil
		}
		job.CurrentPath = path
		job.DiscoveredFiles++

		info, err := entry.Info()
		if err != nil {
			job.ErrorCount++
			_ = s.recordPathError(library.ID, job.ID, path, domain.ErrorUnknownIO, err.Error())
			_ = s.store.AddJobEvent(job.ID, "error", "stat failed: "+path)
			_ = s.store.UpdateScanJob(job)
			return nil
		}
		if info.Size() == 0 {
			job.ErrorCount++
			_ = s.recordPathError(library.ID, job.ID, path, domain.ErrorEmptyFile, "empty file")
			_ = s.store.AddJobEvent(job.ID, "error", "empty file: "+path)
			_ = s.store.UpdateScanJob(job)
			return nil
		}
		if s.canSkipUnchanged(path, info, ext) {
			if _, err := s.indexFileMetadata(library, path, info, ext); err != nil {
				job.ErrorCount++
				_ = s.recordPathError(library.ID, job.ID, path, domain.ErrorUnknownIO, err.Error())
				_ = s.store.AddJobEvent(job.ID, "error", "metadata update failed: "+path)
				_ = s.store.UpdateScanJob(job)
				return nil
			}
			job.SkippedFiles++
			_ = s.store.AddJobEvent(job.ID, "debug", "skipped unchanged: "+path)
			_ = s.store.UpdateScanJob(job)
			return nil
		}

		if err := s.indexFile(library, job.ID, path, info, ext); err != nil {
			job.ErrorCount++
			_ = s.recordPathError(library.ID, job.ID, path, domain.ErrorArchiveOpenFailed, err.Error())
			_ = s.store.AddJobEvent(job.ID, "error", "archive failed: "+path)
			_ = s.store.UpdateScanJob(job)
			return nil
		}
		job.IndexedFiles++
		_ = s.store.AddJobEvent(job.ID, "info", "indexed: "+path)
		_ = s.store.UpdateScanJob(job)
		return nil
	})
	if walkErr != nil {
		job.Status = "failed"
		job.CurrentPath = ""
		job.FinishedAt = time.Now()
		_ = s.store.UpdateScanJob(job)
		_ = s.store.AddJobEvent(job.ID, "error", "scan failed: "+walkErr.Error())
		return job, walkErr
	}
	if err := s.store.DeleteEmptySeries(library.ID); err != nil {
		job.Status = "failed"
		job.CurrentPath = ""
		job.FinishedAt = time.Now()
		_ = s.store.UpdateScanJob(job)
		_ = s.store.AddJobEvent(job.ID, "error", "cleanup failed: "+err.Error())
		return job, err
	}

	job.Status = "completed"
	job.CurrentPath = ""
	job.FinishedAt = time.Now()
	if err := s.store.UpdateScanJob(job); err != nil {
		return job, err
	}
	_ = s.store.AddJobEvent(job.ID, "info", "scan completed")
	return job, nil
}

func (s *Scanner) canSkipUnchanged(path string, info fs.FileInfo, ext string) bool {
	index, err := s.store.FileIndexByPath(path)
	if err != nil {
		return false
	}
	return index.File.Size == info.Size() &&
		index.File.Ext == ext &&
		index.File.MTime.Equal(info.ModTime()) &&
		index.Analyzed &&
		index.PageCount > 0
}

func (s *Scanner) indexFile(library domain.Library, jobID int64, path string, info fs.FileInfo, ext string) error {
	book, err := s.indexFileMetadata(library, path, info, ext)
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

func (s *Scanner) indexFileMetadata(library domain.Library, path string, info fs.FileInfo, ext string) (domain.Book, error) {
	relPath, err := filepath.Rel(library.RootPath, path)
	if err != nil {
		return domain.Book{}, fmt.Errorf("relative path: %w", err)
	}

	seriesTitle := seriesTitleForRelPath(library.RootPath, relPath)
	bookTitle := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	format := strings.TrimPrefix(ext, ".")

	series, err := s.store.UpsertSeries(library.ID, seriesTitle)
	if err != nil {
		return domain.Book{}, err
	}

	var book domain.Book
	existing, err := s.store.FileIndexByPath(path)
	if err == nil {
		book, err = s.store.UpdateBookIdentity(existing.File.BookID, series.ID, bookTitle, format)
		if err != nil {
			return domain.Book{}, err
		}
	} else {
		book, err = s.store.UpsertBook(series.ID, bookTitle, format)
		if err != nil {
			return domain.Book{}, err
		}
	}
	_, err = s.store.UpsertFile(book.ID, library.ID, path, relPath, info.Size(), info.ModTime(), ext)
	if err != nil {
		return domain.Book{}, err
	}
	return book, nil
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

func seriesTitleForRelPath(rootPath string, relPath string) string {
	dir := filepath.Dir(relPath)
	if dir == "." || dir == "/" {
		rootName := filepath.Base(filepath.Clean(rootPath))
		if rootName != "." && rootName != string(filepath.Separator) && rootName != "" {
			return rootName
		}
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
