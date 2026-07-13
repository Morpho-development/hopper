package files

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"hopper/backend/internal/auth"
	"hopper/backend/internal/config"
	"hopper/backend/internal/response"
	"hopper/backend/internal/ws"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	cfg         config.Config
	store       *Store
	broadcaster Broadcaster
	uploadMu    sync.Mutex
}

type Broadcaster interface {
	Broadcast(userID string, event ws.Event)
}

type noopBroadcaster struct{}

func (noopBroadcaster) Broadcast(string, ws.Event) {}

func NewHandler(cfg config.Config, database *sql.DB, broadcaster Broadcaster) *Handler {
	if broadcaster == nil {
		broadcaster = noopBroadcaster{}
	}
	return &Handler{
		cfg:         cfg,
		store:       NewStore(database, cfg.StoragePath),
		broadcaster: broadcaster,
	}
}

func (h *Handler) Routes() http.Handler {
	router := chi.NewRouter()
	router.Get("/", h.list)
	router.Delete("/", h.purge)
	router.Post("/upload", h.upload)
	router.Get("/{id}/preview", h.preview)
	router.Get("/{id}", h.download)
	router.Delete("/{id}", h.delete)
	router.Patch("/{id}", h.update)
	return router
}

func (h *Handler) preview(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", "missing user context")
		return
	}
	record, err := h.store.ByID(r.Context(), user.ID, chi.URLParam(r, "id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	path, err := SafeJoin(h.cfg.StoragePath, record.StorageName)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "internal_error", "invalid storage path")
		return
	}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			response.WriteError(w, http.StatusNotFound, "not_found", "stored file not found")
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to open file")
		return
	}
	defer file.Close()

	buffer := make([]byte, 512)
	read, err := file.Read(buffer)
	if err != nil && !errors.Is(err, io.EOF) {
		response.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to inspect file")
		return
	}
	contentType := http.DetectContentType(buffer[:read])
	if !previewAllowed(contentType, record.SizeBytes) {
		response.WriteError(w, http.StatusUnsupportedMediaType, "preview_unavailable", "preview is not available for this file type")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		response.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to prepare preview")
		return
	}
	info, err := file.Stat()
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to inspect file")
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": record.OriginalName}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, record.OriginalName, info.ModTime(), file)
}

func previewAllowed(contentType string, size int64) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	switch mediaType {
	case "image/jpeg", "image/png", "image/gif", "image/webp", "application/pdf",
		"video/mp4", "video/webm", "audio/mpeg", "audio/ogg", "audio/wav", "audio/x-wav":
		return true
	case "text/plain":
		return size <= 1<<20
	default:
		return false
	}
}

func (h *Handler) purge(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", "missing user context")
		return
	}
	var req purgeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Confirmation != "DELETE ALL FILES" {
		response.WriteError(w, http.StatusBadRequest, "validation_error", "confirmation phrase does not match")
		return
	}
	if _, err := auth.NewStore(h.store.db).Authenticate(r.Context(), user.Login, req.CurrentPassword); err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) || errors.Is(err, auth.ErrInvalidInput) {
			response.WriteError(w, http.StatusUnauthorized, "unauthorized", "current password is incorrect")
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to verify password")
		return
	}

	h.uploadMu.Lock()
	defer h.uploadMu.Unlock()
	deleted, freed, err := h.store.Purge(r.Context(), user.ID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to clear storage")
		return
	}
	h.broadcaster.Broadcast(user.ID, ws.Event{Type: "storage.cleared", Payload: purgeResponse{DeletedFiles: deleted, FreedBytes: freed}})
	response.WriteJSON(w, http.StatusOK, purgeResponse{DeletedFiles: deleted, FreedBytes: freed})
}

func (h *Handler) FolderRoutes() http.Handler {
	router := chi.NewRouter()
	router.Post("/", h.createFolder)
	return router
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", "missing user context")
		return
	}

	folderID := optionalQueryString(r, "folderId")
	page := queryInt(r, "page", 1, 1, 100000)
	pageSize := queryInt(r, "pageSize", 50, 1, 200)

	items, total, err := h.store.List(r.Context(), user.ID, folderID, page, pageSize)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	usedBytes, err := h.store.UsedBytes(r.Context(), user.ID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to load storage usage")
		return
	}
	availableBytes := h.cfg.InstanceQuotaBytes - usedBytes
	if availableBytes < 0 {
		availableBytes = 0
	}

	response.WriteJSON(w, http.StatusOK, listResponse{
		Items:    items,
		Page:     page,
		PageSize: pageSize,
		Total:    total,
		Storage: storageStatus{
			UsedBytes:      usedBytes,
			QuotaBytes:     h.cfg.InstanceQuotaBytes,
			AvailableBytes: availableBytes,
		},
	})
}

func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", "missing user context")
		return
	}

	bodyLimit := h.cfg.MaxUploadBytes + 1<<20
	if bodyLimit < h.cfg.MaxUploadBytes {
		bodyLimit = h.cfg.MaxUploadBytes
	}
	r.Body = http.MaxBytesReader(w, r.Body, bodyLimit)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		response.WriteError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "uploaded file is too large")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "validation_error", "multipart field file is required")
		return
	}
	defer file.Close()

	rawName := header.Filename
	if dispositionName := multipartFileName(header.Header.Get("Content-Disposition")); dispositionName != "" {
		rawName = dispositionName
	}

	originalName, err := CleanDisplayName(rawName)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "validation_error", "invalid file name")
		return
	}

	if header.Size < 0 || header.Size > h.cfg.MaxUploadBytes {
		response.WriteError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "uploaded file is too large")
		return
	}

	folderID := optionalFormString(r, "folderId")
	if err := h.store.EnsureFolderOwned(r.Context(), user.ID, folderID); err != nil {
		writeStoreError(w, err)
		return
	}

	h.uploadMu.Lock()
	defer h.uploadMu.Unlock()

	usedBytes, err := h.store.UsedBytes(r.Context(), user.ID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to check quota")
		return
	}
	if usedBytes+header.Size > h.cfg.InstanceQuotaBytes {
		response.WriteError(w, http.StatusConflict, "quota_exceeded", "storage quota exceeded")
		return
	}

	record, err := h.store.Create(r.Context(), CreateInput{
		OwnerID:      user.ID,
		FolderID:     folderID,
		OriginalName: originalName,
		MimeType:     detectMimeType(header.Header.Get("Content-Type"), originalName),
		SizeBytes:    header.Size,
		Content:      file,
	})
	if err != nil {
		if errors.Is(err, ErrInvalidPath) {
			response.WriteError(w, http.StatusBadRequest, "validation_error", "invalid storage path")
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to store file")
		return
	}

	h.broadcaster.Broadcast(user.ID, ws.Event{
		Type:    "file.created",
		Payload: fileResponseFromRecord(record),
	})
	response.WriteJSON(w, http.StatusCreated, fileResponseFromRecord(record))
}

func (h *Handler) createFolder(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", "missing user context")
		return
	}

	var req createFolderRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	name, err := CleanDisplayName(req.Name)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "validation_error", "invalid folder name")
		return
	}

	folder, err := h.store.CreateFolder(r.Context(), user.ID, name, req.ParentID)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	h.broadcaster.Broadcast(user.ID, ws.Event{
		Type:    "folder.created",
		Payload: folderResponseFromRecord(folder),
	})
	response.WriteJSON(w, http.StatusCreated, folderResponseFromRecord(folder))
}

func (h *Handler) download(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", "missing user context")
		return
	}

	record, err := h.store.ByID(r.Context(), user.ID, chi.URLParam(r, "id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}

	path, err := SafeJoin(h.cfg.StoragePath, record.StorageName)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "internal_error", "invalid storage path")
		return
	}

	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			response.WriteError(w, http.StatusNotFound, "not_found", "stored file not found")
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to open file")
		return
	}
	defer file.Close()

	w.Header().Set("Content-Type", record.MimeType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", record.SizeBytes))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": record.OriginalName}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	if _, err := io.Copy(w, file); err != nil {
		return
	}
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", "missing user context")
		return
	}

	if err := h.store.Delete(r.Context(), user.ID, chi.URLParam(r, "id")); err != nil {
		writeStoreError(w, err)
		return
	}

	h.broadcaster.Broadcast(user.ID, ws.Event{
		Type: "file.deleted",
		Payload: map[string]string{
			"id": chi.URLParam(r, "id"),
		},
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", "missing user context")
		return
	}

	req, ok := decodeUpdateFileRequest(w, r)
	if !ok {
		return
	}

	input := UpdateInput{Name: req.Name, FolderID: req.FolderID, FolderIDSet: req.FolderIDSet}
	record, err := h.store.Update(r.Context(), user.ID, chi.URLParam(r, "id"), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	h.broadcaster.Broadcast(user.ID, ws.Event{
		Type:    "file.updated",
		Payload: fileResponseFromRecord(record),
	})
	response.WriteJSON(w, http.StatusOK, fileResponseFromRecord(record))
}

func detectMimeType(contentType, name string) string {
	if contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err == nil && mediaType != "" {
			return mediaType
		}
	}

	if ext := filepath.Ext(name); ext != "" {
		if mediaType := mime.TypeByExtension(ext); mediaType != "" {
			return mediaType
		}
	}

	return "application/octet-stream"
}

func multipartFileName(contentDisposition string) string {
	_, params, err := mime.ParseMediaType(contentDisposition)
	if err != nil {
		return ""
	}
	return params["filename"]
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrConflict):
		response.WriteError(w, http.StatusConflict, "conflict", "resource already exists")
	case errors.Is(err, ErrNotFound):
		response.WriteError(w, http.StatusNotFound, "not_found", "file not found")
	case errors.Is(err, ErrInvalidPath):
		response.WriteError(w, http.StatusBadRequest, "validation_error", "invalid path")
	default:
		response.WriteError(w, http.StatusInternalServerError, "internal_error", "file operation failed")
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		response.WriteError(w, http.StatusBadRequest, "validation_error", "invalid JSON body")
		return false
	}
	return true
}

type parsedUpdateFileRequest struct {
	Name        *string
	FolderID    *string
	FolderIDSet bool
}

func decodeUpdateFileRequest(w http.ResponseWriter, r *http.Request) (parsedUpdateFileRequest, bool) {
	var raw map[string]json.RawMessage
	if !decodeJSON(w, r, &raw) {
		return parsedUpdateFileRequest{}, false
	}

	var req parsedUpdateFileRequest
	if rawName, ok := raw["name"]; ok {
		var name string
		if err := json.Unmarshal(rawName, &name); err != nil {
			response.WriteError(w, http.StatusBadRequest, "validation_error", "invalid file name")
			return parsedUpdateFileRequest{}, false
		}
		req.Name = &name
	}

	if rawFolderID, ok := raw["folderId"]; ok {
		req.FolderIDSet = true
		if string(rawFolderID) != "null" {
			var folderID string
			if err := json.Unmarshal(rawFolderID, &folderID); err != nil {
				response.WriteError(w, http.StatusBadRequest, "validation_error", "invalid folder id")
				return parsedUpdateFileRequest{}, false
			}
			req.FolderID = &folderID
		}
	}

	if req.Name == nil && !req.FolderIDSet {
		response.WriteError(w, http.StatusBadRequest, "validation_error", "nothing to update")
		return parsedUpdateFileRequest{}, false
	}

	return req, true
}

func optionalFormString(r *http.Request, key string) *string {
	value := r.FormValue(key)
	if value == "" {
		return nil
	}
	return &value
}

func optionalQueryString(r *http.Request, key string) *string {
	value := r.URL.Query().Get(key)
	if value == "" {
		return nil
	}
	return &value
}

func queryInt(r *http.Request, key string, fallback, minValue, maxValue int) int {
	value := r.URL.Query().Get(key)
	if value == "" {
		return fallback
	}

	var parsed int
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil {
		return fallback
	}
	if parsed < minValue {
		return minValue
	}
	if parsed > maxValue {
		return maxValue
	}
	return parsed
}
