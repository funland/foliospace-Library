package service

import "foliospace-reader/internal/domain"

func (s *Service) presentBooks(books []domain.Book, err error) ([]domain.Book, error) {
	if err != nil {
		return nil, err
	}
	return s.store.ApplyBookCollectionPresentation(books)
}

func (s *Service) presentBookPage(page domain.BookListPage, err error) (domain.BookListPage, error) {
	if err != nil {
		return page, err
	}
	page.Items, err = s.store.ApplyBookCollectionPresentation(page.Items)
	return page, err
}

func (s *Service) presentBook(book domain.Book, err error) (domain.Book, error) {
	if err != nil {
		return book, err
	}
	books, err := s.store.ApplyBookCollectionPresentation([]domain.Book{book})
	if err != nil {
		return book, err
	}
	return books[0], nil
}
