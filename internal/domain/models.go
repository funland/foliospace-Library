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
	AssetType string    `json:"assetType"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type DirectoryEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type DirectoryListing struct {
	Path    string           `json:"path"`
	Parent  string           `json:"parent,omitempty"`
	Entries []DirectoryEntry `json:"entries"`
}

type Profile struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Avatar    string    `json:"avatar"`
	Color     string    `json:"color"`
	IsDefault bool      `json:"isDefault"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Series struct {
	ID             int64  `json:"id"`
	LibraryID      int64  `json:"libraryId"`
	Title          string `json:"title"`
	DirectoryPath  string `json:"directoryPath"`
	CollectionType string `json:"collectionType"`
	PrimaryType    string `json:"primaryType"`
	BookCount      int64  `json:"bookCount"`
	CoverBookID    int64  `json:"coverBookId,omitempty"`
	Favorite       bool   `json:"favorite"`
	Liked          bool   `json:"liked"`
}

type Book struct {
	ID               int64     `json:"id"`
	SeriesID         int64     `json:"seriesId"`
	CollectionTitle  string    `json:"collectionTitle,omitempty"`
	Title            string    `json:"title"`
	Creator          string    `json:"creator,omitempty"`
	Description      string    `json:"description,omitempty"`
	BookType         string    `json:"bookType"`
	Format           string    `json:"format"`
	PageCount        int       `json:"pageCount"`
	CoverStatus      string    `json:"coverStatus"`
	ThumbnailStatus  string    `json:"thumbnailStatus"`
	ThumbnailURL     string    `json:"thumbnailUrl,omitempty"`
	Analyzed         bool      `json:"analyzed"`
	FilePath         string    `json:"filePath,omitempty"`
	AddedAt          time.Time `json:"addedAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
	CurrentPage      int       `json:"currentPage"`
	ProgressFraction float64   `json:"progressFraction"`
	LastReadAt       time.Time `json:"lastReadAt"`
	PrivateStatus    string    `json:"privateStatus"`
	Favorite         bool      `json:"favorite"`
	Rating           int       `json:"rating"`
	Tags             []string  `json:"tags"`
	Summary          string    `json:"summary"`
}

type BookPrivateState struct {
	Status   string   `json:"status"`
	Favorite bool     `json:"favorite"`
	Rating   int      `json:"rating"`
	Tags     []string `json:"tags"`
	Summary  string   `json:"summary"`
}

type CollectionPrivateState struct {
	Favorite bool `json:"favorite"`
	Liked    bool `json:"liked"`
}

type ClientPreferences struct {
	Locale         string `json:"locale"`
	ReaderPageMode string `json:"readerPageMode"`
	EPUBPageMode   string `json:"epubPageMode"`
	EPUBTheme      string `json:"epubTheme"`
	EPUBFontSize   int    `json:"epubFontSize"`
}

type GameAsset struct {
	ID            int64     `json:"id"`
	LibraryID     int64     `json:"libraryId"`
	Title         string    `json:"title"`
	Platform      string    `json:"platform"`
	ROMSetName    string    `json:"romSetName"`
	Region        string    `json:"region"`
	Format        string    `json:"format"`
	FilePath      string    `json:"filePath,omitempty"`
	RelPath       string    `json:"relPath,omitempty"`
	Size          int64     `json:"size"`
	MTime         time.Time `json:"mtime"`
	CRC32         string    `json:"crc32"`
	SHA1          string    `json:"sha1"`
	EmulatorHint  string    `json:"emulatorHint"`
	Compatibility string    `json:"compatibility"`
	CoverURL      string    `json:"coverUrl,omitempty"`
	LastPlayedAt  time.Time `json:"lastPlayedAt,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type VideoAsset struct {
	ID              int64     `json:"id"`
	LibraryID       int64     `json:"libraryId"`
	Title           string    `json:"title"`
	Format          string    `json:"format"`
	FilePath        string    `json:"filePath,omitempty"`
	RelPath         string    `json:"relPath,omitempty"`
	Size            int64     `json:"size"`
	MTime           time.Time `json:"mtime"`
	DurationSeconds float64   `json:"durationSeconds"`
	Width           int       `json:"width"`
	Height          int       `json:"height"`
	VideoCodec      string    `json:"videoCodec"`
	AudioCodec      string    `json:"audioCodec"`
	ThumbnailStatus string    `json:"thumbnailStatus"`
	ThumbnailURL    string    `json:"thumbnailUrl,omitempty"`
	DirectPlayable  bool      `json:"directPlayable"`
	PlaybackMode    string    `json:"playbackMode"`
	PlaybackReason  string    `json:"playbackReason,omitempty"`
	LastPlayedAt    time.Time `json:"lastPlayedAt,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type BookListOptions struct {
	SeriesID int64
	Limit    int
	Offset   int
	Query    string
	Sort     string
}

type BookListPage struct {
	Items   []Book `json:"items"`
	Total   int64  `json:"total"`
	Limit   int    `json:"limit"`
	Offset  int    `json:"offset"`
	HasMore bool   `json:"hasMore"`
}

type GameListOptions struct {
	Limit    int
	Offset   int
	Query    string
	Platform string
	Format   string
	Sort     string
}

type GameListPage struct {
	Items   []GameAsset `json:"items"`
	Total   int64       `json:"total"`
	Limit   int         `json:"limit"`
	Offset  int         `json:"offset"`
	HasMore bool        `json:"hasMore"`
}

type VideoListOptions struct {
	Limit  int
	Offset int
	Query  string
	Format string
	Sort   string
}

type VideoListPage struct {
	Items   []VideoAsset `json:"items"`
	Total   int64        `json:"total"`
	Limit   int          `json:"limit"`
	Offset  int          `json:"offset"`
	HasMore bool         `json:"hasMore"`
}

type CollectionAssets struct {
	Books  []Book       `json:"books"`
	Games  []GameAsset  `json:"games"`
	Videos []VideoAsset `json:"videos"`
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
	Index   int    `json:"index"`
	Name    string `json:"name"`
	PageKey string `json:"pageKey,omitempty"`
}

type EPUBManifest struct {
	Title       string          `json:"title"`
	Creator     string          `json:"creator"`
	Description string          `json:"description"`
	CoverHref   string          `json:"coverHref"`
	Spine       []EPUBSpineItem `json:"spine"`
	TOC         []EPUBTOCItem   `json:"toc"`
	TOCTree     []EPUBTOCItem   `json:"tocTree,omitempty"`
}

type EPUBSpineItem struct {
	Index     int    `json:"index"`
	ID        string `json:"id"`
	Href      string `json:"href"`
	MediaType string `json:"mediaType"`
}

type EPUBTOCItem struct {
	Label    string        `json:"label"`
	Href     string        `json:"href"`
	Index    int           `json:"index"`
	Children []EPUBTOCItem `json:"children,omitempty"`
}

type ScanJob struct {
	ID                   int64     `json:"id"`
	LibraryID            int64     `json:"libraryId"`
	Status               string    `json:"status"`
	TargetPath           string    `json:"targetPath"`
	CurrentPath          string    `json:"currentPath"`
	DiscoveredFiles      int       `json:"discoveredFiles"`
	IndexedFiles         int       `json:"indexedFiles"`
	SkippedFiles         int       `json:"skippedFiles"`
	ErrorCount           int       `json:"errorCount"`
	MetadataUpdatedFiles int       `json:"metadataUpdatedFiles"`
	ReclassifiedFiles    int       `json:"reclassifiedFiles"`
	StartedAt            time.Time `json:"startedAt"`
	FinishedAt           time.Time `json:"finishedAt,omitempty"`
}

type ThumbnailJobInput struct {
	BookID   int64
	Size     string
	CacheKey string
	Priority int
}

type ThumbnailJob struct {
	ID           int64     `json:"id"`
	BookID       int64     `json:"bookId"`
	BookTitle    string    `json:"bookTitle,omitempty"`
	Size         string    `json:"size"`
	Status       string    `json:"status"`
	Priority     int       `json:"priority"`
	CacheKey     string    `json:"cacheKey"`
	CachePath    string    `json:"cachePath,omitempty"`
	ContentType  string    `json:"contentType,omitempty"`
	Width        int       `json:"width,omitempty"`
	Height       int       `json:"height,omitempty"`
	ByteSize     int64     `json:"byteSize,omitempty"`
	ErrorMessage string    `json:"errorMessage,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	StartedAt    time.Time `json:"startedAt,omitempty"`
	FinishedAt   time.Time `json:"finishedAt,omitempty"`
}

type ThumbnailCacheEntry struct {
	BookID    int64
	Size      string
	CacheKey  string
	CachePath string
	ByteSize  int64
}

type ThumbnailCacheStatus struct {
	Files            int     `json:"files"`
	Bytes            int64   `json:"bytes"`
	ReadyFiles       int     `json:"readyFiles"`
	ReadyBytes       int64   `json:"readyBytes"`
	MissingFiles     int     `json:"missingFiles"`
	StaleFiles       int     `json:"staleFiles"`
	StaleBytes       int64   `json:"staleBytes"`
	OrphanFiles      int     `json:"orphanFiles"`
	OrphanBytes      int64   `json:"orphanBytes"`
	AlgorithmVersion string  `json:"algorithmVersion"`
	SmallWidth       int     `json:"smallWidth"`
	MediumWidth      int     `json:"mediumWidth"`
	TargetAspect     float64 `json:"targetAspect"`
}

type ThumbnailCacheCleanupResult struct {
	DeletedFiles int   `json:"deletedFiles"`
	DeletedBytes int64 `json:"deletedBytes"`
	FailedFiles  int   `json:"failedFiles"`
}

type ThumbnailQueueStatus struct {
	Status        string               `json:"status"`
	Queued        int                  `json:"queued"`
	Running       int                  `json:"running"`
	Ready         int                  `json:"ready"`
	Failed        int                  `json:"failed"`
	Cancelled     int                  `json:"cancelled"`
	Processed     int                  `json:"processed"`
	ActiveJob     *ThumbnailJob        `json:"activeJob,omitempty"`
	LastError     string               `json:"lastError,omitempty"`
	Paused        bool                 `json:"paused"`
	WorkerEnabled bool                 `json:"workerEnabled"`
	Cache         ThumbnailCacheStatus `json:"cache"`
}

type JobEvent struct {
	ID        int64     `json:"id"`
	JobID     int64     `json:"jobId"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"createdAt"`
}

type ReadProgress struct {
	BookID           int64     `json:"bookId"`
	PageIndex        int       `json:"pageIndex"`
	Locator          string    `json:"locator"`
	ProgressFraction float64   `json:"progressFraction"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

const WebtoonPositionSchema = "webtoon-position-v1"

type ReadingPosition struct {
	BookID              int64     `json:"bookId,omitempty"`
	ReaderMode          string    `json:"readerMode,omitempty"`
	Schema              string    `json:"schema"`
	PageIndex           int       `json:"pageIndex"`
	PageKey             string    `json:"pageKey"`
	PageYOffsetRatio    float64   `json:"pageYOffsetRatio"`
	ViewportAnchorRatio float64   `json:"viewportAnchorRatio"`
	DocumentProgress    float64   `json:"documentProgress"`
	PageCount           int       `json:"pageCount"`
	ContentSignature    string    `json:"contentSignature,omitempty"`
	PayloadJSON         string    `json:"payloadJson,omitempty"`
	UpdatedAt           time.Time `json:"updatedAt,omitempty"`
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
