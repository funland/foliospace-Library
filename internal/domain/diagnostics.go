package domain

// These DTOs deliberately contain no raw domain objects, filenames, error
// messages, provider values, or profile private state. Source is always stored:
// these methods do not verify current filesystem availability or launchability.
type BookDiagnostic struct {
	BookID             int64                `json:"bookId"`
	ProfileID          int64                `json:"profileId"`
	Source             string               `json:"source"`
	FilesystemVerified bool                 `json:"filesystemVerified"`
	Health             BookDiagnosticHealth `json:"health"`
	Index              BookDiagnosticIndex  `json:"index"`
	Issues             []string             `json:"issues"`
}

type BookDiagnosticHealth struct {
	Status string `json:"status"`
	Code   string `json:"code,omitempty"`
}

type BookDiagnosticIndex struct {
	Analyzed      bool   `json:"analyzed"`
	DeclaredPages int    `json:"declaredPages"`
	IndexedPages  int    `json:"indexedPages"`
	Status        string `json:"status"`
}

type GameDiagnostic struct {
	GameID             int64                `json:"gameId"`
	ProfileID          int64                `json:"profileId"`
	Source             string               `json:"source"`
	FilesystemVerified bool                 `json:"filesystemVerified"`
	CatalogRole        string               `json:"catalogRole"`
	Files              GameDiagnosticFiles  `json:"files"`
	Launch             GameDiagnosticLaunch `json:"launch"`
	MetadataStatus     string               `json:"metadataStatus"`
	ArtworkStatus      string               `json:"artworkStatus"`
	Issues             []string             `json:"issues"`
}

type GameDiagnosticFiles struct {
	Count           int `json:"count"`
	Checksummed     int `json:"checksummed"`
	ChecksumPending int `json:"checksumPending"`
}

type GameDiagnosticLaunch struct {
	Status        string `json:"status"`
	ReadyProfiles int    `json:"readyProfiles"`
}
