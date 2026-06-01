package archive

import (
	"archive/zip"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"foliospace-reader/internal/domain"
)

var imageExts = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".webp": "image/webp",
	".gif":  "image/gif",
}

func ListPages(path string) ([]domain.Page, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}
	defer reader.Close()

	files := imageFiles(reader.File)
	if len(files) == 0 {
		return nil, fmt.Errorf("archive has no image pages")
	}

	pages := make([]domain.Page, 0, len(files))
	for i, file := range files {
		pages = append(pages, domain.Page{Index: i, Name: file.Name})
	}
	return pages, nil
}

func OpenPage(path string, pageIndex int) (io.ReadCloser, string, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, "", fmt.Errorf("open zip: %w", err)
	}

	files := imageFiles(reader.File)
	if pageIndex < 0 || pageIndex >= len(files) {
		_ = reader.Close()
		return nil, "", fmt.Errorf("page index %d out of range", pageIndex)
	}

	page, err := files[pageIndex].Open()
	if err != nil {
		_ = reader.Close()
		return nil, "", fmt.Errorf("open page: %w", err)
	}

	return &zipPageReadCloser{ReadCloser: page, closeReader: reader.Close}, contentType(files[pageIndex].Name), nil
}

func imageFiles(files []*zip.File) []*zip.File {
	out := make([]*zip.File, 0, len(files))
	for _, file := range files {
		if file.FileInfo().IsDir() {
			continue
		}
		if _, ok := imageExts[strings.ToLower(filepath.Ext(file.Name))]; ok {
			out = append(out, file)
		}
	}
	sort.SliceStable(out, func(i int, j int) bool {
		return normalizeEntryName(out[i].Name) < normalizeEntryName(out[j].Name)
	})
	return out
}

func contentType(name string) string {
	if value, ok := imageExts[strings.ToLower(filepath.Ext(name))]; ok {
		return value
	}
	return "application/octet-stream"
}

func normalizeEntryName(value string) string {
	return strings.ToLower(strings.ReplaceAll(value, "\\", "/"))
}

type zipPageReadCloser struct {
	io.ReadCloser
	closeReader func() error
}

func (r *zipPageReadCloser) Close() error {
	pageErr := r.ReadCloser.Close()
	readerErr := r.closeReader()
	if pageErr != nil {
		return pageErr
	}
	return readerErr
}
