package domain

type VideoPage[T any] struct {
	Items   []T  `json:"items"`
	Total   int  `json:"total"`
	Limit   int  `json:"limit"`
	Offset  int  `json:"offset"`
	HasMore bool `json:"hasMore"`
}
type VideoSeason struct {
	ID                    int64 `json:"id"`
	WorkID                int64 `json:"workId"`
	Number                int   `json:"number"`
	AvailableEpisodeCount int   `json:"availableEpisodeCount"`
	FileCount             int   `json:"fileCount"`
	ExpectedCount         *int  `json:"expectedCount"`
}
type VideoEpisode struct {
	ID           int64  `json:"id"`
	SeasonID     int64  `json:"seasonId"`
	WorkID       int64  `json:"workId"`
	Season       int    `json:"season"`
	Number       int    `json:"number"`
	Title        string `json:"title"`
	FileCount    int    `json:"fileCount"`
	VideoID      int64  `json:"videoId"`
	Availability string `json:"availability"`
}
type VideoPlaybackState struct {
	VideoID         int64   `json:"videoId"`
	ProfileID       int64   `json:"profileId"`
	MediaKey        string  `json:"mediaKey"`
	Revision        int64   `json:"revision"`
	PositionSeconds float64 `json:"positionSeconds"`
	DurationSeconds float64 `json:"durationSeconds"`
	Completed       bool    `json:"completed"`
	ServerUpdatedAt int64   `json:"serverUpdatedAt"`
	IdentityChanged bool    `json:"identityChanged"`
}
type VideoPlaybackEvent struct {
	EventID         string  `json:"eventId"`
	SessionID       string  `json:"sessionId"`
	ClientSequence  int64   `json:"clientSequence"`
	BaseRevision    int64   `json:"baseRevision"`
	MediaKey        string  `json:"mediaKey"`
	EventKind       string  `json:"eventKind"`
	PositionSeconds float64 `json:"positionSeconds"`
	DurationSeconds float64 `json:"durationSeconds"`
}
type VideoPlaybackItem struct {
	VideoPlaybackState
	WorkID    int64  `json:"workId"`
	Title     string `json:"title"`
	PosterURL string `json:"posterUrl"`
	Season    *int   `json:"season"`
	Episode   *int   `json:"episode"`
	Reason    string `json:"reason"`
}
type VideoExpectedEpisode struct {
	Season       int    `json:"season"`
	Number       int    `json:"number"`
	Title        string `json:"title"`
	AirDate      string `json:"airDate"`
	Availability string `json:"availability"`
	EpisodeID    *int64 `json:"episodeId"`
	VideoID      *int64 `json:"videoId"`
}
type VideoAvailability struct {
	VideoPage[VideoExpectedEpisode]
	Source        string `json:"source"`
	Ordering      string `json:"ordering"`
	DateBasis     string `json:"dateBasis"`
	ExpectedCount *int   `json:"expectedCount"`
}
