package domain

// VideoLocalMetadata is a bounded observation, never an authoritative match.
// Provider identifiers from NFO are hints until validated/confirmed.
type VideoLocalMetadata struct {
	Title      string `json:"title"`
	Overview   string `json:"overview"`
	Year       int    `json:"year"`
	Kind       string `json:"kind"`
	ShowTitle  string `json:"showTitle,omitempty"`
	Season     *int   `json:"season,omitempty"`
	Episode    *int   `json:"episode,omitempty"`
	Provider   string `json:"provider,omitempty"`
	ProviderID string `json:"providerId,omitempty"`
	Source     string `json:"source"`
	Warning    string `json:"warning,omitempty"`
}

type VideoMetadata struct {
	WorkID int64 `json:"workId,omitempty"`
	VideoLocalMetadata
	Revision     string   `json:"revision"`
	LockedFields []string `json:"lockedFields"`
	Generation   int64    `json:"generation"`
	Status       string   `json:"status"`
}

type VideoWork struct {
	ID         int64  `json:"id"`
	LibraryID  int64  `json:"libraryId"`
	Kind       string `json:"kind"`
	Title      string `json:"title"`
	Year       int    `json:"year"`
	Overview   string `json:"overview"`
	Provider   string `json:"provider,omitempty"`
	ProviderID string `json:"providerId,omitempty"`
	PosterURL  string `json:"posterUrl"`
	VideoID    int64  `json:"videoId"`
}
type VideoWorkPage struct {
	Items   []VideoWork `json:"items"`
	Total   int         `json:"total"`
	Limit   int         `json:"limit"`
	Offset  int         `json:"offset"`
	HasMore bool        `json:"hasMore"`
}
type VideoWorkFile struct {
	VideoID int64  `json:"videoId"`
	Title   string `json:"title"`
	Version string `json:"version"`
	Season  *int   `json:"season"`
	Episode *int   `json:"episode"`
	FileURL string `json:"fileUrl"`
	Format  string `json:"format"`
}
type VideoWorkFiles struct {
	Items   []VideoWorkFile `json:"items"`
	Total   int             `json:"total"`
	Limit   int             `json:"limit"`
	Offset  int             `json:"offset"`
	HasMore bool            `json:"hasMore"`
}
type VideoMatchInput struct {
	WorkID     int64  `json:"workId"`
	Title      string `json:"title"`
	Kind       string `json:"kind"`
	Year       int    `json:"year"`
	Overview   string `json:"overview"`
	Season     *int   `json:"season"`
	Episode    *int   `json:"episode"`
	Version    string `json:"version"`
	Generation int64  `json:"generation"`
	Provider   string `json:"provider,omitempty"`
	ProviderID string `json:"providerId,omitempty"`
}
type VideoCandidate struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Kind         string `json:"kind"`
	Year         int    `json:"year"`
	Overview     string `json:"overview"`
	PosterPath   string `json:"posterPath,omitempty"`
	BackdropPath string `json:"backdropPath,omitempty"`
}
type VideoMetadataJob struct {
	ID         int64            `json:"id"`
	VideoID    int64            `json:"videoId"`
	Generation int64            `json:"generation"`
	Query      string           `json:"query"`
	Kind       string           `json:"kind"`
	Status     string           `json:"status"`
	Attempts   int              `json:"attempts"`
	NextAt     int64            `json:"nextAt"`
	ErrorCode  string           `json:"errorCode,omitempty"`
	Candidates []VideoCandidate `json:"candidates"`
}
