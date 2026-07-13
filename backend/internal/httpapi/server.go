package httpapi

import (
	"database/sql"
	"net/http"

	"hopper/backend/internal/auth"
	"hopper/backend/internal/config"
	"hopper/backend/internal/files"
	"hopper/backend/internal/messages"
	"hopper/backend/internal/response"
	"hopper/backend/internal/share"
	"hopper/backend/internal/ws"

	"github.com/go-chi/chi/v5"
)

func NewRouter(cfg config.Config, database *sql.DB) http.Handler {
	router := chi.NewRouter()
	hub := ws.NewHub()
	authHandler := auth.NewHandler(cfg, database)
	filesHandler := files.NewHandler(cfg, database, hub)
	shareHandler := share.NewHandler(cfg, database)
	wsHandler := ws.NewHandler(cfg, database, hub)
	messagesHandler := messages.NewHandler(database, hub)

	router.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		status := "ok"
		if err := database.PingContext(r.Context()); err != nil {
			status = "database_error"
			response.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": status})
			return
		}

		response.WriteJSON(w, http.StatusOK, map[string]string{"status": status})
	})

	router.Mount("/api/auth", authHandler.Routes())
	router.Mount("/api/ws", wsHandler)
	router.With(authHandler.AuthMiddleware).Route("/api/files", func(r chi.Router) {
		r.Mount("/", filesHandler.Routes())
		r.Mount("/{id}/share", shareHandler.CreateRoutes())
	})
	router.With(authHandler.AuthMiddleware).Mount("/api/folders", filesHandler.FolderRoutes())
	router.With(authHandler.AuthMiddleware).Mount("/api/messages", messagesHandler.Routes())
	router.Mount("/s", shareHandler.PublicRoutes())

	return router
}
