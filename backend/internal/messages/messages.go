package messages

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"hopper/backend/internal/auth"
	"hopper/backend/internal/response"
	"hopper/backend/internal/ws"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

var (
	errInvalid  = errors.New("invalid message")
	errNotFound = errors.New("message not found")
)

type Message struct {
	ID        string `json:"id"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type Store struct{ db *sql.DB }

func (s *Store) List(ctx context.Context, ownerID string) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, body, created_at, updated_at
		FROM messages WHERE owner_id = ?
		ORDER BY created_at DESC LIMIT 100
	`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()
	messages := make([]Message, 0)
	for rows.Next() {
		var message Message
		if err := rows.Scan(&message.ID, &message.Body, &message.CreatedAt, &message.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate messages: %w", err)
	}
	return messages, nil
}

func (s *Store) Create(ctx context.Context, ownerID, body string) (Message, error) {
	body = strings.TrimSpace(body)
	if body == "" || len([]rune(body)) > 4000 {
		return Message{}, errInvalid
	}
	message := Message{ID: uuid.NewString(), Body: body}
	if _, err := s.db.ExecContext(ctx, "INSERT INTO messages(id, owner_id, body) VALUES (?, ?, ?)", message.ID, ownerID, message.Body); err != nil {
		return Message{}, fmt.Errorf("insert message: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, "SELECT created_at, updated_at FROM messages WHERE id = ?", message.ID).Scan(&message.CreatedAt, &message.UpdatedAt); err != nil {
		return Message{}, fmt.Errorf("load created message: %w", err)
	}
	return message, nil
}

func (s *Store) Delete(ctx context.Context, ownerID, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM messages WHERE id = ? AND owner_id = ?", id, ownerID)
	if err != nil {
		return fmt.Errorf("delete message: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("message rows affected: %w", err)
	}
	if rows != 1 {
		return errNotFound
	}
	return nil
}

type Broadcaster interface{ Broadcast(string, ws.Event) }

type Handler struct {
	store       *Store
	broadcaster Broadcaster
}

func NewHandler(database *sql.DB, broadcaster Broadcaster) *Handler {
	return &Handler{store: &Store{db: database}, broadcaster: broadcaster}
}

func (h *Handler) Routes() http.Handler {
	router := chi.NewRouter()
	router.Get("/", h.list)
	router.Post("/", h.create)
	router.Delete("/{id}", h.delete)
	return router
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", "missing user context")
		return
	}
	messages, err := h.store.List(r.Context(), user.ID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to load messages")
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{"messages": messages})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", "missing user context")
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "validation_error", "invalid JSON body")
		return
	}
	message, err := h.store.Create(r.Context(), user.ID, req.Body)
	if err != nil {
		if errors.Is(err, errInvalid) {
			response.WriteError(w, http.StatusBadRequest, "validation_error", "message must contain 1-4000 characters")
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to create message")
		return
	}
	h.broadcaster.Broadcast(user.ID, ws.Event{Type: "message.created", Payload: message})
	response.WriteJSON(w, http.StatusCreated, message)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", "missing user context")
		return
	}
	id := chi.URLParam(r, "id")
	if err := h.store.Delete(r.Context(), user.ID, id); err != nil {
		if errors.Is(err, errNotFound) {
			response.WriteError(w, http.StatusNotFound, "not_found", "message not found")
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to delete message")
		return
	}
	h.broadcaster.Broadcast(user.ID, ws.Event{Type: "message.deleted", Payload: map[string]string{"id": id}})
	w.WriteHeader(http.StatusNoContent)
}
