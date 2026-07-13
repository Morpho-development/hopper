package ws

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"hopper/backend/internal/auth"
	"hopper/backend/internal/config"
	"hopper/backend/internal/response"

	"github.com/gorilla/websocket"
)

type Handler struct {
	hub      *Hub
	store    *auth.Store
	tokens   *auth.TokenService
	upgrader websocket.Upgrader
}

func NewHandler(cfg config.Config, database *sql.DB, hub *Hub) *Handler {
	return &Handler{
		hub:    hub,
		store:  auth.NewStore(database),
		tokens: auth.NewTokenService(cfg.JWTAccessSecret, cfg.AccessTokenTTL),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return sameOrigin(r)
			},
		},
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token := bearerToken(r)
	if token == "" {
		token = websocketProtocolToken(r)
	}
	if token == "" {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", "missing websocket token")
		return
	}

	claims, err := h.tokens.ParseAccessToken(token)
	if err != nil {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", "invalid websocket token")
		return
	}

	user, err := h.store.UserByID(r.Context(), claims.UserID)
	if err != nil {
		if errors.Is(err, auth.ErrNotFound) {
			response.WriteError(w, http.StatusUnauthorized, "unauthorized", "user not found")
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to load user")
		return
	}
	if user.AuthVersion != claims.AuthVersion {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", "session has been revoked")
		return
	}

	responseHeaders := http.Header{}
	if websocketProtocolToken(r) != "" {
		responseHeaders.Set("Sec-WebSocket-Protocol", "hopper")
	}
	conn, err := h.upgrader.Upgrade(w, r, responseHeaders)
	if err != nil {
		return
	}

	client := NewClient(conn)
	h.hub.Register(user.ID, client)
	defer h.hub.Unregister(user.ID, client)

	go client.WriteLoop()
	client.ReadLoop()
}

func websocketProtocolToken(r *http.Request) string {
	protocols := websocket.Subprotocols(r)
	if len(protocols) != 2 || protocols[0] != "hopper" || strings.Count(protocols[1], ".") != 2 {
		return ""
	}
	return protocols[1]
}

func sameOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host)
}

func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
}
