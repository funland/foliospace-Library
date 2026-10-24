package service

import (
	"errors"
	"foliospace-reader/internal/archive"
	"foliospace-reader/internal/domain"
	"path/filepath"
)

// ValidateBookFile refreshes diagnostic state only, so existing/manual metadata,
// page lists and private state are unaffected by checking or repairing a file.
func (s *Service) ValidateBookFile(id int64) (domain.Book, error) {
	book, err := s.store.BookByID(id)
	if err != nil {
		return book, err
	}
	index, err := s.store.FileIndexByPath(book.FilePath)
	if err != nil {
		return book, err
	}
	err = archive.ValidateBookFile(book.FilePath, filepath.Ext(book.FilePath))
	var issue *archive.BookFileIssue
	if err != nil && !errors.As(err, &issue) {
		return book, err
	}
	if err == nil && !archive.SupportsBookValidation(filepath.Ext(book.FilePath)) {
		return book, nil
	}
	code, message := "", ""
	if issue != nil {
		code, message = issue.Code, issue.Message
	}
	if err = s.store.SetBookFileHealth(id, code, message); err != nil {
		return book, err
	}
	if issue != nil {
		err = s.store.RecordFileError(domain.FileErrorInput{LibraryID: index.File.LibraryID, BookID: id, FileID: index.File.ID, Path: book.FilePath, Code: domain.ErrorCode(code), Message: message})
		if err != nil {
			return book, err
		}
	}
	return s.store.BookByID(id)
}
