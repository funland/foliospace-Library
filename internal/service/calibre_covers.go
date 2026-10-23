package service

import (
	"foliospace-reader/internal/bookmeta"
	"foliospace-reader/internal/domain"
)

func (s *Service) bookLibraryRoot(book domain.Book) (string, error) {
	return s.store.BookLibraryRoot(book.ID)
}

func (s *Service) calibreCover(book domain.Book) (PageStream, error) {
	root, err := s.bookLibraryRoot(book)
	if err != nil {
		return PageStream{}, err
	}
	body, contentType, _, err := bookmeta.OpenCover(root, book.FilePath)
	return PageStream{Body: body, ContentType: contentType}, err
}

func (s *Service) BookArtworkRevisions(ids []int64) map[int64]string {
	return s.store.BookArtworkRevisions(ids)
}
