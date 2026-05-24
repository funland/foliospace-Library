package service

import (
	"fmt"
	"io"
	"strings"

	"foliospace-reader/internal/archive"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/scanner"
	"foliospace-reader/internal/store"
)

type Service struct {
	store   *store.Store
	scanner *scanner.Scanner
}

func New(store *store.Store) *Service {
	return &Service{
		store:   store,
		scanner: scanner.New(store),
	}
}

func (s *Service) CreateLibrary(name string, rootPath string) (domain.Library, error) {
	name = strings.TrimSpace(name)
	rootPath = strings.TrimSpace(rootPath)
	if rootPath == "" {
		return domain.Library{}, fmt.Errorf("library root path is required")
	}
	if name == "" {
		name = rootPath
	}
	return s.store.CreateLibrary(name, rootPath)
}

func (s *Service) ListLibraries() ([]domain.Library, error) {
	return s.store.ListLibraries()
}

func (s *Service) DeleteLibrary(id int64) error {
	return s.store.DeleteLibrary(id)
}

func (s *Service) ScanLibrary(id int64) (domain.ScanJob, error) {
	lib, err := s.store.LibraryByID(id)
	if err != nil {
		return domain.ScanJob{}, err
	}
	return s.scanner.StartScanJob(lib)
}

func (s *Service) ListSeries() ([]domain.Series, error) {
	return s.store.ListSeries()
}

func (s *Service) ListBooks(seriesID int64) ([]domain.Book, error) {
	return s.store.ListBooks(seriesID)
}

func (s *Service) Book(id int64) (domain.Book, error) {
	return s.store.BookByID(id)
}

func (s *Service) AnalyzeBook(id int64) ([]domain.Page, error) {
	book, err := s.store.BookByID(id)
	if err != nil {
		return nil, err
	}
	if book.FilePath == "" {
		return nil, fmt.Errorf("book has no indexed file")
	}
	pages, err := archive.ListPages(book.FilePath)
	if err != nil {
		return nil, err
	}
	if err := s.store.ReplacePages(id, pages); err != nil {
		return nil, err
	}
	return pages, nil
}

func (s *Service) Pages(bookID int64) ([]domain.Page, error) {
	pages, err := s.store.ListPages(bookID)
	if err != nil {
		return nil, err
	}
	if len(pages) > 0 {
		return pages, nil
	}
	return s.AnalyzeBook(bookID)
}

func (s *Service) OpenPage(bookID int64, pageIndex int) (PageStream, error) {
	book, err := s.store.BookByID(bookID)
	if err != nil {
		return PageStream{}, err
	}
	if book.FilePath == "" {
		return PageStream{}, fmt.Errorf("book has no indexed file")
	}
	body, contentType, err := archive.OpenPage(book.FilePath, pageIndex)
	if err != nil {
		return PageStream{}, err
	}
	return PageStream{Body: body, ContentType: contentType}, nil
}

func (s *Service) SaveProgress(bookID int64, pageIndex int) error {
	return s.store.SaveProgress(bookID, pageIndex)
}

func (s *Service) ListJobs() ([]domain.ScanJob, error) {
	return s.store.ListScanJobs()
}

func (s *Service) JobEvents(jobID int64) ([]domain.JobEvent, error) {
	return s.store.ListJobEvents(jobID)
}

func (s *Service) ListErrors() ([]domain.FileError, error) {
	return s.store.ListFileErrors()
}

func (s *Service) ListErrorsByJob(jobID int64) ([]domain.FileError, error) {
	return s.store.ListFileErrorsByJob(jobID)
}

type PageStream struct {
	Body        io.ReadCloser
	ContentType string
}
