package archive

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// BookFileIssue describes a confirmed structural problem, not an access failure.
type BookFileIssue struct{ Code, Message string }

func (e *BookFileIssue) Error() string { return e.Message }

func SupportsBookValidation(ext string) bool {
	switch strings.ToLower(ext) {
	case ".epub", ".pdf", ".zip", ".cbz":
		return true
	}
	return false
}

// ValidateBookFile checks the container and EPUB spine without decoding every
// comic page or rewriting source bytes. Operational I/O errors remain distinct.
func ValidateBookFile(path, ext string) error {
	ext = strings.ToLower(ext)
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return &BookFileIssue{"empty_file", "File is empty (0 bytes)."}
	}
	header := make([]byte, 1024)
	n, err := f.Read(header)
	if err != nil && err != io.EOF {
		return err
	}
	header = header[:n]
	if bytes.Count(header, []byte{0}) == len(header) {
		allZero := true
		buffer := make([]byte, 64*1024)
		for {
			n, err = f.Read(buffer)
			if bytes.Count(buffer[:n], []byte{0}) != n {
				allZero = false
				break
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
		}
		if allZero {
			return &BookFileIssue{"zero_filled_file", "File contains only zero bytes; no readable book content."}
		}
	}
	if !SupportsBookValidation(ext) {
		return nil
	}
	if ext == ".pdf" {
		if !bytes.Contains(header, []byte("%PDF-")) {
			return &BookFileIssue{"invalid_book_container", "PDF header is missing; the file is not a readable PDF."}
		}
		return nil
	}
	z, err := zip.OpenReader(path)
	if err != nil {
		var pe *os.PathError
		if errors.As(err, &pe) {
			return err
		}
		return &BookFileIssue{"invalid_book_container", "Cannot read ZIP directory; the file may be truncated or damaged: " + err.Error()}
	}
	z.Close()
	if ext == ".epub" {
		if _, err = ListEPUBSpine(path); err != nil {
			var pe *os.PathError
			if errors.As(err, &pe) {
				return err
			}
			return &BookFileIssue{"invalid_epub", fmt.Sprintf("Cannot read EPUB package or reading order: %v", err)}
		}
	}
	return nil
}
