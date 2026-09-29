package server

import "time"

type Capabilities struct {
	Transcoding bool `json:"transcoding"`
	Movies      bool `json:"movies"`
	Series      bool `json:"series"`
	Music       bool `json:"music"`
	Audio       bool `json:"audio"`
	YouTube     bool `json:"youtube"`
	MusicBrainz bool `json:"musicBrainz"`
	Plugins     bool `json:"plugins"`
	GPU         bool `json:"gpu"`
	Metadata    bool `json:"metadata"`
	Playback    bool `json:"playback"`
}
type SystemDTO struct {
	Name          string       `json:"name"`
	Version       string       `json:"version"`
	SetupRequired bool         `json:"setupRequired"`
	Capabilities  Capabilities `json:"capabilities"`
}
type GeneralSettingsDTO struct {
	ServerName       string `json:"serverName"`
	AutoMetadata     bool   `json:"autoMetadata"`
	MetadataLanguage string `json:"metadataLanguage"`
	CastImages       bool   `json:"castImages"`
	WatchedPercent   int    `json:"watchedPercent"`
	TMDBConfigured   bool   `json:"tmdbConfigured"`
}
type GeneralSettingsPatch struct {
	ServerName       *string `json:"serverName,omitempty" jsonschema:"minLength=1,maxLength=80"`
	AutoMetadata     *bool   `json:"autoMetadata,omitempty"`
	MetadataLanguage *string `json:"metadataLanguage,omitempty" jsonschema:"enum=en-US,enum=vi-VN"`
	CastImages       *bool   `json:"castImages,omitempty"`
	WatchedPercent   *int    `json:"watchedPercent,omitempty" jsonschema:"minimum=50,maximum=100"`
}
type LoginRequest struct {
	Username string `json:"username" jsonschema:"minLength=1,maxLength=64"`
	Password string `json:"password" jsonschema:"minLength=1,maxLength=72"`
}
type SetupRequest struct {
	Username string `json:"username" jsonschema:"minLength=1,maxLength=64"`
	Password string `json:"password" jsonschema:"minLength=8,maxLength=72"`
}
type UserDTO struct {
	ID               string   `json:"id"`
	Username         string   `json:"username"`
	Role             string   `json:"role"`
	Disabled         bool     `json:"disabled"`
	LibraryIDs       []string `json:"libraryIds"`
	ImportLibraryIDs []string `json:"importLibraryIds,omitempty"`
}
type LoginDTO struct {
	Token string  `json:"token"`
	User  UserDTO `json:"user"`
}
type UsersDTO struct {
	Items []UserDTO `json:"items"`
}
type UserRequest struct {
	Username         string    `json:"username" jsonschema:"minLength=1,maxLength=64"`
	Password         string    `json:"password,omitempty" jsonschema:"minLength=8,maxLength=72"`
	Role             string    `json:"role" jsonschema:"enum=admin,enum=user"`
	Disabled         bool      `json:"disabled"`
	LibraryIDs       []string  `json:"libraryIds"`
	ImportLibraryIDs *[]string `json:"importLibraryIds,omitempty"`
}
type PasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword" jsonschema:"minLength=8,maxLength=72"`
}
type IDParams struct {
	ID string `json:"id" jsonschema:"format=uuid"`
}
type LibraryRequest struct {
	Name              string   `json:"name" jsonschema:"minLength=1,maxLength=100"`
	Kind              string   `json:"kind" jsonschema:"enum=movies,enum=series,enum=music,enum=podcasts,enum=audiobooks"`
	Paths             []string `json:"paths" jsonschema:"minItems=0"`
	ScanIntervalHours int      `json:"scanIntervalHours" jsonschema:"minimum=0,maximum=8760"`
}
type ScanQuery struct {
	ForceMetadata bool `json:"forceMetadata,omitempty"`
}
type CastImageParams struct {
	ID       string `json:"id" jsonschema:"format=uuid"`
	PersonID string `json:"personId" jsonschema:"format=uuid"`
}
type LibraryDTO struct {
	FileCount         int64     `json:"fileCount"`
	Scan              *JobDTO   `json:"scan,omitempty"`
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	Kind              string    `json:"kind"`
	Paths             []string  `json:"paths"`
	ScanIntervalHours int       `json:"scanIntervalHours"`
	LastScanAt        time.Time `json:"lastScanAt"`
}
type LibrariesDTO struct {
	Items []LibraryDTO `json:"items"`
}
type CatalogQuery struct {
	Artist    string `json:"artist,omitempty" jsonschema:"maxLength=500"`
	TopLevel  bool   `json:"topLevel,omitempty"`
	PersonID  string `json:"personId,omitempty" jsonschema:"format=uuid"`
	LibraryID string `json:"libraryId,omitempty"`
	ParentID  string `json:"parentId,omitempty"`
	Search    string `json:"search,omitempty" jsonschema:"maxLength=200"`
	Kind      string `json:"kind,omitempty" jsonschema:"enum=movie,enum=series,enum=episode,enum=album,enum=track,enum=podcast,enum=podcast_episode,enum=audiobook,enum=book_part"`
	Favorites bool   `json:"favorites,omitempty"`
	Resume    bool   `json:"resume,omitempty"`
	Cursor    string `json:"cursor,omitempty" jsonschema:"maxLength=2048"`
	Limit     int    `json:"limit,omitempty" jsonschema:"minimum=1,maximum=100"`
}
type ItemDTO struct {
	ID             string  `json:"id"`
	LibraryID      string  `json:"libraryId"`
	ParentID       string  `json:"parentId"`
	Kind           string  `json:"kind"`
	Title          string  `json:"title"`
	Year           int     `json:"year"`
	Season         int     `json:"season"`
	Episode        int     `json:"episode"`
	Overview       string  `json:"overview"`
	Poster         string  `json:"poster"`
	ProviderID     string  `json:"providerId"`
	MetadataLocked bool    `json:"metadataLocked"`
	Favorite       bool    `json:"favorite"`
	Watched        bool    `json:"watched"`
	Position       float64 `json:"position"`
}
type ItemsDTO struct {
	Items      []ItemDTO `json:"items"`
	NextCursor string    `json:"nextCursor"`
	HasMore    bool      `json:"hasMore"`
}
type TrackDTO struct {
	Index    int    `json:"index"`
	Type     string `json:"type"`
	Codec    string `json:"codec"`
	Language string `json:"language"`
	Title    string `json:"title"`
}
type FileDTO struct {
	Size      int64      `json:"size"`
	Width     int        `json:"width"`
	Height    int        `json:"height"`
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Duration  float64    `json:"duration"`
	Available bool       `json:"available"`
	Tracks    []TrackDTO `json:"tracks"`
}
type DetailDTO struct {
	Cast     []CastDTO         `json:"cast"`
	Item     ItemDTO           `json:"item"`
	Files    []FileDTO         `json:"files"`
	NextID   string            `json:"nextId"`
	Audio    *AudioMetadataDTO `json:"audio,omitempty"`
	Children []ItemDTO         `json:"children,omitempty"`
}
type CastDTO struct {
	Image     string `json:"image"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	Character string `json:"character"`
}
type StateRequest struct {
	Favorite bool `json:"favorite"`
	Watched  bool `json:"watched"`
}
type MetadataRequest struct {
	Title      string `json:"title" jsonschema:"minLength=1"`
	Year       int    `json:"year" jsonschema:"minimum=0,maximum=9999"`
	Overview   string `json:"overview"`
	ProviderID string `json:"providerId"`
	Locked     bool   `json:"locked"`
}
type MetadataSearchQuery struct {
	Year   int    `json:"year,omitempty" jsonschema:"minimum=0,maximum=9999"`
	Search string `json:"search" jsonschema:"minLength=1"`
	Kind   string `json:"kind" jsonschema:"enum=movie,enum=series"`
}
type MetadataMatch struct {
	Score       int    `json:"score"`
	Recommended bool   `json:"recommended"`
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Year        string `json:"year"`
	Overview    string `json:"overview"`
}
type MetadataMatches struct {
	Search string          `json:"search"`
	Year   int             `json:"year"`
	Items  []MetadataMatch `json:"items"`
}
type IdentifyRequest struct {
	ProviderID int `json:"providerId" jsonschema:"minimum=1"`
}
type JobDTO struct {
	ResourceName    string    `json:"resourceName"`
	ItemID          string    `json:"itemId"`
	LibraryID       string    `json:"libraryId"`
	Role            string    `json:"role"`
	Attempts        int       `json:"attempts"`
	CancelRequested bool      `json:"cancelRequested"`
	UpdatedAt       time.Time `json:"updatedAt"`
	TotalFiles      int       `json:"totalFiles"`
	ProcessedFiles  int       `json:"processedFiles"`
	ID              string    `json:"id"`
	Kind            string    `json:"kind"`
	ResourceID      string    `json:"resourceId"`
	State           string    `json:"state"`
	Progress        int       `json:"progress"`
	Error           string    `json:"error"`
	CreatedAt       time.Time `json:"createdAt"`
}
type JobsDTO struct {
	Items []JobDTO `json:"items"`
}
type WorkerDTO struct {
	ID          string    `json:"id"`
	Role        string    `json:"role"`
	HeartbeatAt time.Time `json:"heartbeatAt"`
}
type WorkersDTO struct {
	Items []WorkerDTO `json:"items"`
}
type EncodingDTO struct {
	Mode                      string `json:"mode" jsonschema:"enum=disabled,enum=nvidia"`
	CQ                        int    `json:"cq" jsonschema:"minimum=0,maximum=51"`
	Device                    int    `json:"device" jsonschema:"minimum=0,maximum=31"`
	MaxConcurrent             int    `json:"maxConcurrent" jsonschema:"minimum=1,maximum=16"`
	Preset                    string `json:"preset" jsonschema:"enum=p1,enum=p2,enum=p3,enum=p4,enum=p5,enum=p6,enum=p7"`
	VideoCodec                string `json:"videoCodec" jsonschema:"enum=h264,enum=hevc"`
	Bitrate720                int    `json:"bitrate720" jsonschema:"minimum=100000,maximum=100000000"`
	Bitrate1080               int    `json:"bitrate1080" jsonschema:"minimum=100000,maximum=100000000"`
	Bitrate2160               int    `json:"bitrate2160" jsonschema:"minimum=100000,maximum=100000000"`
	AudioCodec                string `json:"audioCodec" jsonschema:"enum=aac,enum=ac3"`
	AudioBitrate              int    `json:"audioBitrate" jsonschema:"minimum=64000,maximum=1024000"`
	SubtitleSize              int    `json:"subtitleSize" jsonschema:"minimum=12,maximum=72"`
	SubtitleOutline           int    `json:"subtitleOutline" jsonschema:"minimum=0,maximum=10"`
	SubtitleMargin            int    `json:"subtitleMargin" jsonschema:"minimum=0,maximum=200"`
	SubtitleFont              string `json:"subtitleFont" jsonschema:"enum=Arial,enum=Noto Sans,enum=Noto Sans CJK"`
	SubtitleColor             string `json:"subtitleColor" jsonschema:"pattern=^#[0-9A-Fa-f]{6}$"`
	SubtitleBackground        string `json:"subtitleBackground" jsonschema:"pattern=^#[0-9A-Fa-f]{6}$"`
	SubtitleBackgroundOpacity int    `json:"subtitleBackgroundOpacity" jsonschema:"minimum=0,maximum=100"`
	SubtitleBorderColor       string `json:"subtitleBorderColor" jsonschema:"pattern=^#[0-9A-Fa-f]{6}$"`
}
type PlaybackRequest struct {
	ForceTranscode bool    `json:"forceTranscode,omitempty"`
	SubtitleDelay  float64 `json:"subtitleDelay,omitempty" jsonschema:"minimum=-600,maximum=600"`
	MaxHeight      int     `json:"maxHeight,omitempty" jsonschema:"enum=0,enum=720,enum=1080,enum=2160"`
	FileID         string  `json:"fileId" jsonschema:"format=uuid"`
	Position       float64 `json:"position" jsonschema:"minimum=0"`
	DirectPlay     bool    `json:"directPlay"`
	AudioIndex     int     `json:"audioIndex" jsonschema:"minimum=-1"`
	SubtitleIndex  int     `json:"subtitleIndex" jsonschema:"minimum=-1"`
	MaxBitrate     int     `json:"maxBitrate" jsonschema:"minimum=0,maximum=100000000"`
}
type PlaybackDTO struct {
	Stream      *PlaybackStreamDTO `json:"stream,omitempty"`
	ID          string             `json:"id"`
	Method      string             `json:"method"`
	State       string             `json:"state"`
	URL         string             `json:"url"`
	Position    float64            `json:"position"`
	Duration    float64            `json:"duration"`
	StreamToken string             `json:"streamToken,omitempty"`
}
type PlaybackStreamDTO struct {
	VideoTranscoded bool   `json:"videoTranscoded"`
	VideoCodec      string `json:"videoCodec"`
	AudioCodec      string `json:"audioCodec"`
	VideoBitrate    int64  `json:"videoBitrate"`
	AudioBitrate    int64  `json:"audioBitrate"`
	TotalBitrate    int64  `json:"totalBitrate"`
	BitrateSource   string `json:"bitrateSource"`
}
type ProgressRequest struct {
	Sequence int64   `json:"sequence" jsonschema:"minimum=1"`
	Position float64 `json:"position" jsonschema:"minimum=0"`
}
type StreamParams struct {
	ID   string `json:"id" jsonschema:"format=uuid"`
	File string `json:"file" jsonschema:"pattern=^(original|index[.]m3u8|segment-[0-9]{6}[.]ts)$"`
}
type TokenQuery struct {
	Token string `json:"token" jsonschema:"minLength=1"`
}
type StreamDTO struct{}
type HealthDTO struct {
	Status string `json:"status"`
}
