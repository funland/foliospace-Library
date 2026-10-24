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
	ID              int64     `json:"id"`
	Name            string    `json:"name"`
	RootPath        string    `json:"rootPath"`
	AssetType       string    `json:"assetType"`
	ExcludePatterns []string  `json:"excludePatterns"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
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
	ID             int64     `json:"id"`
	LibraryID      int64     `json:"libraryId"`
	Title          string    `json:"title"`
	DisplayTitle   string    `json:"displayTitle,omitempty"`
	DirectoryPath  string    `json:"directoryPath"`
	CollectionType string    `json:"collectionType"`
	PrimaryType    string    `json:"primaryType"`
	BookCount      int64     `json:"bookCount"`
	CoverBookID    int64     `json:"coverBookId,omitempty"`
	AddedAt        time.Time `json:"addedAt"`
	Favorite       bool      `json:"favorite"`
	Liked          bool      `json:"liked"`
}

type CollectionListOptions struct {
	Limit       int
	Offset      int
	PrimaryType string
	LibraryID   int64
	Format      string
	Sort        string
	Direction   string
	Query       string
}

type CollectionListPage struct {
	Items   []Series `json:"items"`
	Total   int64    `json:"total"`
	Limit   int      `json:"limit"`
	Offset  int      `json:"offset"`
	HasMore bool     `json:"hasMore"`
}

type Book struct {
	FileErrorCode          string    `json:"fileErrorCode,omitempty"`
	FileErrorMessage       string    `json:"fileErrorMessage,omitempty"`
	ID                     int64     `json:"id"`
	SeriesID               int64     `json:"seriesId"`
	CollectionTitle        string    `json:"collectionTitle,omitempty"`
	CollectionDisplayTitle string    `json:"collectionDisplayTitle,omitempty"`
	Title                  string    `json:"title"`
	Creator                string    `json:"creator,omitempty"`
	DisplayCreator         string    `json:"displayCreator,omitempty"`
	Description            string    `json:"description,omitempty"`
	BookType               string    `json:"bookType"`
	Format                 string    `json:"format"`
	PageCount              int       `json:"pageCount"`
	CoverStatus            string    `json:"coverStatus"`
	ThumbnailStatus        string    `json:"thumbnailStatus"`
	ThumbnailURL           string    `json:"thumbnailUrl,omitempty"`
	Analyzed               bool      `json:"analyzed"`
	FilePath               string    `json:"filePath,omitempty"`
	FileSize               int64     `json:"fileSize"`
	FileMTime              time.Time `json:"-"`
	ContentHash            *string   `json:"contentHash"`
	ContentHashAlgorithm   *string   `json:"contentHashAlgorithm"`
	ContentRevision        *string   `json:"contentRevision"`
	AddedAt                time.Time `json:"addedAt"`
	UpdatedAt              time.Time `json:"updatedAt"`
	CurrentPage            int       `json:"currentPage"`
	ProgressLocator        string    `json:"progressLocator"`
	ProgressFraction       float64   `json:"progressFraction"`
	LastReadAt             time.Time `json:"lastReadAt"`
	PrivateStatus          string    `json:"privateStatus"`
	Favorite               bool      `json:"favorite"`
	Rating                 int       `json:"rating"`
	Tags                   []string  `json:"tags"`
	Summary                string    `json:"summary"`
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

type ManualCollection struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	ItemCount   int64     `json:"itemCount"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type ManualCollectionItem struct {
	CollectionID int64     `json:"collectionId,omitempty"`
	AssetType    string    `json:"assetType"`
	AssetID      int64     `json:"assetId"`
	Title        string    `json:"title,omitempty"`
	Subtitle     string    `json:"subtitle,omitempty"`
	CoverURL     string    `json:"coverUrl,omitempty"`
	ManifestURL  string    `json:"manifestUrl,omitempty"`
	CreatedAt    time.Time `json:"createdAt,omitempty"`
}

type ManualCollectionDetails struct {
	Collection ManualCollection       `json:"collection"`
	Items      []ManualCollectionItem `json:"items"`
}

type GamePrivateState struct {
	Favorite bool `json:"favorite"`
	Liked    bool `json:"liked"`
}

type GamePlayStats struct {
	GameID           int64      `json:"gameId"`
	ProfileID        int64      `json:"profileId"`
	FirstPlayedAt    *time.Time `json:"firstPlayedAt"`
	LastPlayedAt     *time.Time `json:"lastPlayedAt"`
	TotalPlaySeconds int64      `json:"totalPlaySeconds"`
	LaunchCount      int64      `json:"launchCount"`
}

// GamePlaySessionReport carries the cumulative elapsed time for one client-generated session id.
// Repeating a report is safe because the store only adds the increase since the previous report.
type GamePlaySessionReport struct {
	SessionID      string     `json:"sessionId"`
	StartedAt      *time.Time `json:"startedAt,omitempty"`
	ElapsedSeconds int64      `json:"elapsedSeconds"`
	EndedAt        *time.Time `json:"endedAt,omitempty"`
}

type GamePlayReportResult struct {
	Stats              GamePlayStats `json:"stats"`
	SessionID          string        `json:"sessionId"`
	SessionPlaySeconds int64         `json:"sessionPlaySeconds"`
	Ended              bool          `json:"ended"`
}

type PlayedGame struct {
	Game  GameAsset     `json:"game"`
	Stats GamePlayStats `json:"stats"`
}

type PlayedGameListOptions struct {
	Limit     int
	Offset    int
	Query     string
	Platform  string
	Sort      string
	Direction string
}

type PlayedGameListPage struct {
	Items   []PlayedGame `json:"items"`
	Total   int64        `json:"total"`
	Limit   int          `json:"limit"`
	Offset  int          `json:"offset"`
	HasMore bool         `json:"hasMore"`
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
	CatalogRole   string    `json:"catalogRole,omitempty"`
	CoverURL      string    `json:"coverUrl,omitempty"`
	Favorite      bool      `json:"favorite"`
	Liked         bool      `json:"liked"`
	LastPlayedAt  time.Time `json:"lastPlayedAt,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type GameFile struct {
	ID       int64     `json:"id"`
	GameID   int64     `json:"gameId"`
	Name     string    `json:"name"`
	FilePath string    `json:"filePath,omitempty"`
	Size     int64     `json:"size"`
	MTime    time.Time `json:"mtime"`
	SHA1     string    `json:"sha1,omitempty"`
	Role     string    `json:"role"`
	Position int       `json:"position"`
}

type GameLaunchClient struct {
	Name         string `json:"name"`
	Version      string `json:"version"`
	Platform     string `json:"platform"`
	Architecture string `json:"architecture"`
}

type GameRuntimeDescriptor struct {
	ID          string `json:"id"`
	Version     string `json:"version,omitempty"`
	ContentSet  string `json:"contentSet,omitempty"`
	CoreID      string `json:"coreId,omitempty"`
	CoreBuildID string `json:"coreBuildId,omitempty"`
	CoreSHA256  string `json:"coreSha256,omitempty"`
}

type GameLaunchResolveRequest struct {
	Client   GameLaunchClient        `json:"client"`
	Runtimes []GameRuntimeDescriptor `json:"runtimes"`
}

// GameContentActionRequest negotiates a non-launch action for indexed game
// content. Capabilities are action-specific protocol identifiers rather than
// emulator runtime identities.
type GameContentActionRequest struct {
	Client       GameLaunchClient `json:"client"`
	Capabilities []string         `json:"capabilities"`
}

type GameContentActionResolution struct {
	Action      string     `json:"action"`
	ContentMode string     `json:"contentMode"`
	Validation  string     `json:"validation"`
	Game        GameAsset  `json:"game"`
	EntryFile   string     `json:"entryFile"`
	Files       []GameFile `json:"files"`
}

type GameLaunchResolvedFile struct {
	SourceGameID int64  `json:"sourceGameId"`
	Position     *int   `json:"position,omitempty"`
	Name         string `json:"name"`
	Size         int64  `json:"size"`
	Role         string `json:"role"`
	SHA1         string `json:"sha1,omitempty"`
}

type GameLaunchResolution struct {
	ContentAudit    *GameLaunchContentAudit  `json:"contentAudit,omitempty"`
	LaunchProfileID string                   `json:"launchProfileId"`
	ProfileRevision int                      `json:"profileRevision"`
	Runtime         GameRuntimeDescriptor    `json:"runtime"`
	Game            GameAsset                `json:"game"`
	EntryFile       string                   `json:"entryFile"`
	Files           []GameLaunchResolvedFile `json:"files"`
	DOSLaunch       *DOSLaunch               `json:"dosLaunch,omitempty"`
}

// ContentAudit identifies the unchanged content definition used across MAME builds.
type GameLaunchContentAudit struct {
	Method                 string                `json:"method"`
	Policy                 string                `json:"policy"`
	SourceRuntime          GameRuntimeDescriptor `json:"sourceRuntime"`
	SourceListXMLSHA256    string                `json:"sourceListxmlSha256"`
	RequestedListXMLSHA256 string                `json:"requestedListxmlSha256"`
	DefinitionSHA256       string                `json:"definitionSha256"`
}

// GameLaunchProfile is a persisted, runtime-specific launch decision produced
// by an explicit catalog audit. It is intentionally separate from games so a
// ROM can have more than one verified runtime profile.
type GameLaunchProfile struct {
	GameID           int64
	ID               string
	Revision         int
	Priority         int
	Policy           string
	ClientName       string
	MinClientVersion string
	ClientPlatform   string
	Architecture     string
	Runtime          GameRuntimeDescriptor
	EntryFile        string
	CanonicalSet     string
	Status           string
	Files            []GameLaunchProfileFile
}

type GameLaunchProfileFile struct {
	Position     int
	SourceGameID int64
	SourceSHA1   string
	SourceName   string
	Name         string
	Size         int64
	Role         string
}

type GameLaunchCatalogUpdate struct {
	GameID       int64
	Platform     string
	ROMSetName   string
	EmulatorHint string
	CatalogRole  string
}

type GameLaunchProfileRebuildResult struct {
	ProfilesWritten int `json:"profilesWritten"`
	FilesWritten    int `json:"filesWritten"`
	GamesReady      int `json:"gamesReady"`
	GamesRejected   int `json:"gamesRejected"`
}

type DOSLaunchCandidate struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

type DOSLaunch struct {
	GameID           int64                `json:"gameId"`
	EntryFile        string               `json:"entryFile,omitempty"`
	EntrySource      string               `json:"entrySource"`
	InstallDirectory string               `json:"installDirectory,omitempty"`
	WorkingDirectory string               `json:"workingDirectory,omitempty"`
	DOSBoxConfig     string               `json:"dosboxConfig,omitempty"`
	Arguments        []string             `json:"arguments"`
	Candidates       []DOSLaunchCandidate `json:"candidates"`
	KeymapHints      map[string]string    `json:"keymapHints,omitempty"`
	SourceIdentifier string               `json:"sourceIdentifier,omitempty"`
	SourceSHA256     string               `json:"sourceSha256,omitempty"`
	CatalogRevision  string               `json:"catalogRevision,omitempty"`
	AuditStatus      string               `json:"auditStatus,omitempty"`
	UpdatedAt        time.Time            `json:"updatedAt"`
}

type GameSource struct {
	ID                 int64     `json:"id"`
	GameID             int64     `json:"gameId"`
	LibraryID          int64     `json:"libraryId"`
	Title              string    `json:"title"`
	FilePath           string    `json:"filePath"`
	RelPath            string    `json:"relPath"`
	EntryName          string    `json:"entryName"`
	Format             string    `json:"format"`
	Size               int64     `json:"size"`
	ContainerSize      int64     `json:"containerSize"`
	MTime              time.Time `json:"mtime"`
	CRC32              string    `json:"crc32"`
	SHA1               string    `json:"sha1"`
	GroupKey           string    `json:"groupKey"`
	DiskOrder          int       `json:"diskOrder"`
	Compatibility      string    `json:"compatibility"`
	BootabilityChecked bool      `json:"bootabilityChecked"`
}

type GameMetadata struct {
	GameID        int64     `json:"gameId"`
	DisplayTitle  string    `json:"displayTitle"`
	Summary       string    `json:"summary"`
	ReleaseDate   string    `json:"releaseDate"`
	Genres        []string  `json:"genres"`
	Developers    []string  `json:"developers"`
	Publishers    []string  `json:"publishers"`
	Players       string    `json:"players"`
	Rating        float64   `json:"rating"`
	ExternalLinks []string  `json:"externalLinks"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type GameMetadataSource struct {
	ID         int64     `json:"id"`
	GameID     int64     `json:"gameId"`
	Source     string    `json:"source"`
	SourceID   string    `json:"sourceId"`
	MatchedBy  string    `json:"matchedBy"`
	Confidence float64   `json:"confidence"`
	RawJSON    string    `json:"rawJson,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type GameArtwork struct {
	ID         int64     `json:"id"`
	GameID     int64     `json:"gameId"`
	Source     string    `json:"source"`
	Kind       string    `json:"kind"`
	URL        string    `json:"url,omitempty"`
	CachePath  string    `json:"cachePath,omitempty"`
	Width      int       `json:"width"`
	Height     int       `json:"height"`
	Selected   bool      `json:"selected"`
	Confidence float64   `json:"confidence"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type GameDetails struct {
	Game           GameAsset            `json:"game"`
	MetadataStatus string               `json:"metadataStatus"`
	Metadata       GameMetadata         `json:"metadata"`
	Sources        []GameMetadataSource `json:"sources"`
	Artwork        []GameArtwork        `json:"artwork"`
}

type GameMetadataProviderStatus struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Enabled             bool     `json:"enabled"`
	Configured          bool     `json:"configured"`
	RequiresCredentials bool     `json:"requiresCredentials"`
	Free                bool     `json:"free"`
	Network             bool     `json:"network"`
	Capabilities        []string `json:"capabilities"`
	Message             string   `json:"message,omitempty"`
}

type GameMetadataActionResult struct {
	GameID         int64                        `json:"gameId"`
	Action         string                       `json:"action"`
	Status         string                       `json:"status"`
	Message        string                       `json:"message"`
	MetadataStatus string                       `json:"metadataStatus"`
	Sources        []GameMetadataSource         `json:"sources"`
	Providers      []GameMetadataProviderStatus `json:"providers"`
}

type VideoAsset struct {
	ID              int64     `json:"id"`
	LibraryID       int64     `json:"libraryId"`
	Title           string    `json:"title"`
	DisplayTitle    string    `json:"displayTitle"`
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
	SeriesID  int64
	Limit     int
	Offset    int
	Query     string
	Sort      string
	Direction string
	Format    string
}

type BookListPage struct {
	Items   []Book `json:"items"`
	Total   int64  `json:"total"`
	Limit   int    `json:"limit"`
	Offset  int    `json:"offset"`
	HasMore bool   `json:"hasMore"`
}

type GameListOptions struct {
	Limit               int
	Offset              int
	Query               string
	Platform            string
	ROMSetName          string
	Format              string
	Sort                string
	BasePath            string
	ClientVisibleOnly   bool
	CatalogRole         string
	IncludeDependencies bool
}

type GameListPage struct {
	Items   []GameAsset `json:"items"`
	Total   int64       `json:"total"`
	Limit   int         `json:"limit"`
	Offset  int         `json:"offset"`
	HasMore bool        `json:"hasMore"`
}

type GameCatalogSettings struct {
	AutoAnalyzeAfterScan bool   `json:"autoAnalyzeAfterScan"`
	EnableLibretroCovers bool   `json:"enableLibretroCovers"`
	FBNeoDATPath         string `json:"fbneoDatPath"`
	MAMEListXMLPath      string `json:"mameListXmlPath"`
	FBNeoTargetsPath     string `json:"fbneoTargetsPath"`
	MAMETargetsPath      string `json:"mameTargetsPath"`
	LaunchTargetsPath    string `json:"launchTargetsPath"`
	MAMEPlatforms        string `json:"mamePlatforms"`
	MetadataProvider     string `json:"metadataProvider"`
}

type GameCatalogPolicyStatus struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Available bool   `json:"available"`
	Message   string `json:"message,omitempty"`
}

type GameCurationSummary struct {
	Total           int64                     `json:"total"`
	Ready           int64                     `json:"ready"`
	NeedsCuration   int64                     `json:"needsCuration"`
	Dependencies    int64                     `json:"dependencies"`
	MetadataReady   int64                     `json:"metadataReady"`
	ArtworkReady    int64                     `json:"artworkReady"`
	FileCount       int64                     `json:"fileCount"`
	Checksummed     int64                     `json:"checksummed"`
	ChecksumPending int64                     `json:"checksumPending"`
	Policies        []GameCatalogPolicyStatus `json:"policies"`
	LastTask        GameCatalogTaskStatus     `json:"lastTask"`
}

type GameCurationItem struct {
	Game           GameAsset `json:"game"`
	MetadataStatus string    `json:"metadataStatus"`
	ArtworkStatus  string    `json:"artworkStatus"`
	ReadyProfiles  int       `json:"readyProfiles"`
	FileCount      int       `json:"fileCount"`
	Checksummed    int       `json:"checksummed"`
	MobileReady    bool      `json:"mobileReady"`
	IssueCode      string    `json:"issueCode,omitempty"`
	IssueMessage   string    `json:"issueMessage,omitempty"`
}

type GameCurationStats struct {
	MetadataStatus string
	ArtworkStatus  string
	ReadyProfiles  int
	FileCount      int
	Checksummed    int
}

type GameCurationPage struct {
	Items   []GameCurationItem `json:"items"`
	Total   int64              `json:"total"`
	Limit   int                `json:"limit"`
	Offset  int                `json:"offset"`
	HasMore bool               `json:"hasMore"`
}

type GameCompatibilityRebuildRequest struct {
	Scope    string `json:"scope"`
	GameID   int64  `json:"gameId,omitempty"`
	Platform string `json:"platform,omitempty"`
	Force    bool   `json:"force"`
}

type GameCatalogTaskStatus struct {
	ID        string         `json:"id,omitempty"`
	Action    string         `json:"action,omitempty"`
	Scope     string         `json:"scope,omitempty"`
	GameID    int64          `json:"gameId,omitempty"`
	Platform  string         `json:"platform,omitempty"`
	Force     bool           `json:"force,omitempty"`
	Status    string         `json:"status,omitempty"`
	Message   string         `json:"message,omitempty"`
	Processed int64          `json:"processed"`
	Total     int64          `json:"total"`
	Matched   int64          `json:"matched"`
	Skipped   int64          `json:"skipped"`
	Failed    int64          `json:"failed"`
	Errors    []string       `json:"errors,omitempty"`
	StartedAt *time.Time     `json:"startedAt,omitempty"`
	EndedAt   *time.Time     `json:"endedAt,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
}

type GameListFacets struct {
	Total     int64               `json:"total"`
	Platforms []GamePlatformFacet `json:"platforms"`
}

type GamePlatformFacet struct {
	Platform     string `json:"platform"`
	Title        string `json:"title,omitempty"`
	ROMSetName   string `json:"romSetName,omitempty"`
	Format       string `json:"format,omitempty"`
	EmulatorHint string `json:"emulatorHint,omitempty"`
	Count        int64  `json:"count"`
}

type GamePlatformCatalog struct {
	Items []GamePlatformDefinition `json:"items"`
	Total int                      `json:"total"`
}

type GamePlatformDefinition struct {
	Platform  string   `json:"platform"`
	Title     string   `json:"title"`
	Aliases   []string `json:"aliases,omitempty"`
	Count     int64    `json:"count"`
	Available bool     `json:"available"`
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
}

type EPUBSpineItem struct {
	Index     int    `json:"index"`
	ID        string `json:"id"`
	Href      string `json:"href"`
	MediaType string `json:"mediaType"`
}

type EPUBTOCItem struct {
	Label string `json:"label"`
	Href  string `json:"href"`
	Index int    `json:"index"`
}

type ScanJob struct {
	// Completion-only observation; deliberately not persisted in scan_jobs.
	GameFiles            int       `json:"-"`
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
	Resolved  bool      `json:"resolved"`
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
