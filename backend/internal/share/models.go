package share

import (
	"errors"
	"time"
)

var (
	ErrInvalidInput = errors.New("invalid input")
	ErrNotFound     = errors.New("not found")
)

type CreateInput struct {
	FileID           string
	OwnerID          string
	ExpiresInSeconds int64
	MaxDownloads     *int
}

type CreatedShare struct {
	Token        string
	ExpiresAt    time.Time
	MaxDownloads *int
}

type PublicFile struct {
	OriginalName string
	StorageName  string
	MimeType     string
	SizeBytes    int64
}

type createRequest struct {
	ExpiresInSeconds int64 `json:"expiresInSeconds"`
	MaxDownloads     *int  `json:"maxDownloads"`
}

type createResponse struct {
	URL          string `json:"url"`
	ExpiresAt    string `json:"expiresAt"`
	MaxDownloads *int   `json:"maxDownloads"`
}
