package domain

import "time"

type ErrorCode string

const (
	ErrorFileMissing             ErrorCode = "file_missing"
	ErrorPermissionDenied        ErrorCode = "permission_denied"
	ErrorEmptyFile               ErrorCode = "empty_file"
	ErrorUnsupportedFormat       ErrorCode = "unsupported_format"
	ErrorArchiveOpenFailed       ErrorCode = "archive_open_failed"
	ErrorArchiveEmpty            ErrorCode = "archive_empty"
	ErrorArchivePageDecodeFailed ErrorCode = "archive_page_decode_failed"
	ErrorPathEncoding            ErrorCode = "path_encoding_error"
	ErrorCaseConflict            ErrorCode = "case_conflict"
	ErrorMountMissing            ErrorCode = "mount_missing"
	ErrorUnknownIO               ErrorCode = "unknown_io_error"
)

type Library struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	RootPath  string    `json:"rootPath"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Series struct {
	ID        int64  `json:"id"`
	LibraryID int64  `json:"libraryId"`
	Title     string `json:"title"`
	BookCount int64  `json:"bookCount"`
}

type Book struct {
	ID          int64  `json:"id"`
	SeriesID    int64  `json:"seriesId"`
	Title       string `json:"title"`
	Format      string `json:"format"`
	PageCount   int    `json:"pageCount"`
	CoverStatus string `json:"coverStatus"`
	Analyzed    bool   `json:"analyzed"`
	FilePath    string `json:"filePath,omitempty"`
}

type File struct {
	ID        int64     `json:"id"`
	BookID    int64     `json:"bookId"`
	LibraryID int64     `json:"libraryId"`
	AbsPath   string    `json:"absPath"`
	RelPath   string    `json:"relPath"`
	Size      int64     `json:"size"`
	MTime     time.Time `json:"mtime"`
	Ext       string    `json:"ext"`
}

type Page struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
}

type ScanJob struct {
	ID              int64     `json:"id"`
	LibraryID       int64     `json:"libraryId"`
	Status          string    `json:"status"`
	DiscoveredFiles int       `json:"discoveredFiles"`
	IndexedFiles    int       `json:"indexedFiles"`
	SkippedFiles    int       `json:"skippedFiles"`
	ErrorCount      int       `json:"errorCount"`
	StartedAt       time.Time `json:"startedAt"`
	FinishedAt      time.Time `json:"finishedAt,omitempty"`
}

type JobEvent struct {
	ID        int64     `json:"id"`
	JobID     int64     `json:"jobId"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"createdAt"`
}

type ReadProgress struct {
	BookID    int64     `json:"bookId"`
	PageIndex int       `json:"pageIndex"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type FileError struct {
	ID        int64     `json:"id"`
	LibraryID int64     `json:"libraryId"`
	BookID    int64     `json:"bookId,omitempty"`
	FileID    int64     `json:"fileId,omitempty"`
	JobID     int64     `json:"jobId,omitempty"`
	Path      string    `json:"path"`
	Code      ErrorCode `json:"code"`
	Message   string    `json:"message"`
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
}

type FileErrorInput struct {
	LibraryID int64
	BookID    int64
	FileID    int64
	JobID     int64
	Path      string
	Code      ErrorCode
	Message   string
}
