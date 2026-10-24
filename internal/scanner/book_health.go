package scanner

import (
	"database/sql"
	"errors"
	"foliospace-reader/internal/archive"
	"foliospace-reader/internal/bookmeta"
	"foliospace-reader/internal/domain"
	"io/fs"
)

func bookHealthErrorCode(err error) domain.ErrorCode {
	var issue *archive.BookFileIssue
	if errors.As(err, &issue) {
		return domain.ErrorCode(issue.Code)
	}
	return classifyWalkError(err)
}

func (s *Scanner) checkBookFile(library domain.Library, path string, info fs.FileInfo, ext string, resolver *bookmeta.Resolver) (indexedBookResult, error) {
	result := indexedBookResult{}
	err := archive.ValidateBookFile(path, ext)
	var issue *archive.BookFileIssue
	if err != nil && !errors.As(err, &issue) {
		return result, err
	}
	if err == nil && !archive.SupportsBookValidation(ext) {
		return result, nil
	}
	index, lookupErr := s.store.FileIndexByPath(path)
	if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
		return result, lookupErr
	}
	if issue != nil && info.Size() > 0 {
		// Calibre sidecars remain usable even when the book bytes are damaged.
		// Keep the existing merge rules protecting manual metadata and identity.
		if resolver != nil {
			if metadata, found, _ := resolver.Lookup(path); found {
				var importErr error
				result, importErr = s.indexResolvedFileMetadata(library, path, info, ext, importedBookMetadata(path, metadata))
				if importErr != nil {
					return result, importErr
				}
				index, lookupErr = s.store.FileIndexByPath(path)
			}
		}
		if errors.Is(lookupErr, sql.ErrNoRows) {
			var importErr error
			result, importErr = s.indexBasicFileMetadata(library, path, info, ext, nil)
			if importErr != nil {
				return result, importErr
			}
			index, lookupErr = s.store.FileIndexByPath(path)
		}
	}
	if lookupErr == nil {
		code, message := "", ""
		if issue != nil {
			code, message = issue.Code, issue.Message
		}
		if saveErr := s.store.SetBookFileHealth(index.Book.ID, code, message); saveErr != nil {
			return result, saveErr
		}
	}
	return result, err
}
