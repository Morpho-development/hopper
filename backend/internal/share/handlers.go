package share

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"time"

	"hopper/backend/internal/auth"
	"hopper/backend/internal/config"
	"hopper/backend/internal/files"
	"hopper/backend/internal/response"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	cfg        config.Config
	store      *Store
	filesStore *files.Store
}

func NewHandler(cfg config.Config, database *sql.DB) *Handler {
	return &Handler{
		cfg:        cfg,
		store:      NewStore(database),
		filesStore: files.NewStore(database, cfg.StoragePath),
	}
}

func (h *Handler) CreateRoutes() http.Handler {
	router := chi.NewRouter()
	router.Post("/", h.create)
	return router
}

func (h *Handler) PublicRoutes() http.Handler {
	router := chi.NewRouter()
	router.Get("/{token}", h.download)
	return router
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", "missing user context")
		return
	}

	req, ok := decodeCreateRequest(w, r)
	if !ok {
		return
	}

	fileID := chi.URLParam(r, "id")
	if _, err := h.filesStore.ByID(r.Context(), user.ID, fileID); err != nil {
		writeError(w, err)
		return
	}

	created, err := h.store.Create(r.Context(), CreateInput{
		FileID:           fileID,
		OwnerID:          user.ID,
		ExpiresInSeconds: req.ExpiresInSeconds,
		MaxDownloads:     req.MaxDownloads,
	})
	if err != nil {
		writeError(w, err)
		return
	}

	response.WriteJSON(w, http.StatusCreated, createResponse{
		URL:          h.publicURL(r, created.Token),
		ExpiresAt:    created.ExpiresAt.Format(time.RFC3339),
		MaxDownloads: created.MaxDownloads,
	})
}

func (h *Handler) download(w http.ResponseWriter, r *http.Request) {
	record, err := h.store.Resolve(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		writeError(w, err)
		return
	}

	path, err := files.SafeJoin(h.cfg.StoragePath, record.StorageName)
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
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.Copy(w, file)
}

func (h *Handler) publicURL(r *http.Request, token string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto == "http" || proto == "https" {
		scheme = proto
	}

	host := h.cfg.AppDomain
	if h.cfg.AppEnv != "production" && r.Host != "" {
		host = r.Host
	}
	return fmt.Sprintf("%s://%s/s/%s", scheme, host, token)
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		response.WriteError(w, http.StatusBadRequest, "validation_error", err.Error())
	case errors.Is(err, ErrNotFound), errors.Is(err, files.ErrNotFound):
		response.WriteError(w, http.StatusNotFound, "not_found", "share not found")
	default:
		response.WriteError(w, http.StatusInternalServerError, "internal_error", "share operation failed")
	}
}
