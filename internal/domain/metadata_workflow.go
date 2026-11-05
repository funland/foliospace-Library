package domain

import "time"

// A preview selects one provider result already stored by the server. It never
// accepts provider metadata from the caller.
type VideoMetadataPreviewInput struct {
	VideoID        int64  `json:"videoId"`
	JobID          int64  `json:"jobId"`
	CandidateIndex int    `json:"candidateIndex"`
	Season         *int   `json:"season,omitempty"`
	Episode        *int   `json:"episode,omitempty"`
	Version        string `json:"version,omitempty"`
}

type VideoMetadataApplyInput struct {
	VideoID int64  `json:"videoId"`
	Receipt string `json:"receipt"`
	Confirm bool   `json:"confirm"`
}

type VideoMetadataSelection struct {
	WorkID     int64  `json:"workId"`
	Title      string `json:"title"`
	Kind       string `json:"kind"`
	Year       int    `json:"year"`
	Overview   string `json:"overview"`
	Provider   string `json:"provider,omitempty"`
	ProviderID string `json:"providerId,omitempty"`
	Season     *int   `json:"season,omitempty"`
	Episode    *int   `json:"episode,omitempty"`
	Version    string `json:"version"`
}

type VideoMetadataChange struct {
	Field  string `json:"field"`
	Before any    `json:"before"`
	After  any    `json:"after"`
}

type VideoMetadataPreview struct {
	ProfileID                 int64                  `json:"profileId"`
	VideoID                   int64                  `json:"videoId"`
	LibraryID                 int64                  `json:"libraryId"`
	JobID                     int64                  `json:"jobId"`
	CandidateIndex            int                    `json:"candidateIndex"`
	Generation                int64                  `json:"generation"`
	Scope                     string                 `json:"scope"`
	Current                   VideoMetadataSelection `json:"current"`
	Proposed                  VideoMetadataSelection `json:"proposed"`
	Changes                   []VideoMetadataChange  `json:"changes"`
	LockedFields              []string               `json:"lockedFields"`
	ReusesWork                bool                   `json:"reusesWork"`
	PlaybackIdentityMayChange bool                   `json:"playbackIdentityMayChange"`
	Receipt                   string                 `json:"receipt"`
	ExpiresAt                 time.Time              `json:"expiresAt"`
}

type VideoMetadataApplyResult struct {
	VideoID    int64  `json:"videoId"`
	WorkID     int64  `json:"workId"`
	Generation int64  `json:"generation"`
	Scope      string `json:"scope"`
}
