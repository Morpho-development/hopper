package files

import (
	"errors"
	"io"
	"time"
)

var (
	ErrConflict    = errors.New("conflict")
	ErrInvalidPath = errors.New("invalid path")
	ErrNotFound    = errors.New("not found")
)

type Record struct {
	ID           string
	OwnerID      string
	FolderID     *string
	OriginalName string
	StorageName  string
	MimeType     string
	SizeBytes    int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type CreateInput struct {
	OwnerID      string
	FolderID     *string
	OriginalName string
	MimeType     string
	SizeBytes    int64
	Content      io.Reader
}

type UpdateInput struct {
	Name        *string
	FolderID    *string
	FolderIDSet bool
}

type Folder struct {
	ID        string
	OwnerID   string
	ParentID  *string
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type ListItem struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	MimeType  string `json:"mimeType"`
	SizeBytes int64  `json:"sizeBytes"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type fileResponse struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	FolderID  *string `json:"folderId"`
	MimeType  string  `json:"mimeType"`
	SizeBytes int64   `json:"sizeBytes"`
}

type folderResponse struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	ParentID *string `json:"parentId"`
}

type listResponse struct {
	Items    []ListItem    `json:"items"`
	Page     int           `json:"page"`
	PageSize int           `json:"pageSize"`
	Total    int           `json:"total"`
	Storage  storageStatus `json:"storage"`
}

type storageStatus struct {
	UsedBytes      int64 `json:"usedBytes"`
	QuotaBytes     int64 `json:"quotaBytes"`
	AvailableBytes int64 `json:"availableBytes"`
}

type createFolderRequest struct {
	Name     string  `json:"name"`
	ParentID *string `json:"parentId"`
}

type updateFileRequest struct {
	Name     *string `json:"name"`
	FolderID *string `json:"folderId"`
}

type purgeRequest struct {
	CurrentPassword string `json:"currentPassword"`
	Confirmation    string `json:"confirmation"`
}

type purgeResponse struct {
	DeletedFiles int   `json:"deletedFiles"`
	FreedBytes   int64 `json:"freedBytes"`
}

func fileResponseFromRecord(record Record) fileResponse {
	return fileResponse{
		ID:        record.ID,
		Name:      record.OriginalName,
		FolderID:  record.FolderID,
		MimeType:  record.MimeType,
		SizeBytes: record.SizeBytes,
	}
}

func folderResponseFromRecord(folder Folder) folderResponse {
	return folderResponse{
		ID:       folder.ID,
		Name:     folder.Name,
		ParentID: folder.ParentID,
	}
}
