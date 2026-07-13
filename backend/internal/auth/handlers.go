package auth

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"hopper/backend/internal/config"
	"hopper/backend/internal/response"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	cfg    config.Config
	store  *Store
	tokens *TokenService
	limit  *RateLimiter
}

const refreshCookieName = "hopper_refresh"

func NewHandler(cfg config.Config, database *sql.DB) *Handler {
	return &Handler{
		cfg:    cfg,
		store:  NewStore(database),
		tokens: NewTokenService(cfg.JWTAccessSecret, cfg.AccessTokenTTL),
		limit:  NewRateLimiter(cfg.AuthRateLimitPerMinute),
	}
}

func (h *Handler) Routes() http.Handler {
	router := chi.NewRouter()
	router.With(h.limit.Middleware).Post("/register", h.register)
	router.With(h.limit.Middleware).Post("/login", h.login)
	router.With(h.limit.Middleware).Post("/refresh", h.refresh)
	router.With(h.limit.Middleware).Post("/logout", h.logout)
	router.With(h.AuthMiddleware).Get("/me", h.me)
	router.With(h.limit.Middleware, h.AuthMiddleware).Patch("/me", h.updateCredentials)
	return router
}

func (h *Handler) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			response.WriteError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
			return
		}

		claims, err := h.tokens.ParseAccessToken(strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
		if err != nil {
			response.WriteError(w, http.StatusUnauthorized, "unauthorized", "invalid bearer token")
			return
		}

		user, err := h.store.UserByID(r.Context(), claims.UserID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
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

		next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
	})
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.RegistrationEnabled {
		response.WriteError(w, http.StatusForbidden, "forbidden", "registration is disabled")
		return
	}

	var req credentialsRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	user, err := h.store.CreateUser(r.Context(), normalizeLogin(req.Login), req.Password)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			response.WriteError(w, http.StatusBadRequest, "validation_error", err.Error())
		case errors.Is(err, ErrConflict):
			response.WriteError(w, http.StatusConflict, "conflict", "login already exists")
		default:
			response.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to create user")
		}
		return
	}

	response.WriteJSON(w, http.StatusCreated, userResponse{User: user})
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	user, err := h.store.Authenticate(r.Context(), normalizeLogin(req.Login), req.Password)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) || errors.Is(err, ErrInvalidInput) {
			response.WriteError(w, http.StatusUnauthorized, "unauthorized", "invalid login or password")
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to authenticate")
		return
	}

	h.writeTokenPair(w, r, user)
}

func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	if !requestOriginAllowed(r) {
		response.WriteError(w, http.StatusForbidden, "forbidden", "cross-origin session request rejected")
		return
	}
	cookie, err := r.Cookie(refreshCookieName)
	if err != nil || cookie.Value == "" {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", "missing refresh session")
		return
	}

	user, refreshToken, err := h.store.RotateRefreshToken(r.Context(), cookie.Value, h.cfg.RefreshTokenTTL)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			response.WriteError(w, http.StatusUnauthorized, "unauthorized", "invalid refresh token")
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to refresh token")
		return
	}

	accessToken, err := h.tokens.NewAccessToken(user)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to issue access token")
		return
	}
	h.setRefreshCookie(w, r, refreshToken)

	response.WriteJSON(w, http.StatusOK, tokenResponse{
		AccessToken: accessToken,
		ExpiresIn:   int(h.cfg.AccessTokenTTL.Seconds()),
		User:        user,
	})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if !requestOriginAllowed(r) {
		response.WriteError(w, http.StatusForbidden, "forbidden", "cross-origin session request rejected")
		return
	}
	if cookie, err := r.Cookie(refreshCookieName); err == nil {
		if err := h.store.RevokeRefreshToken(r.Context(), cookie.Value); err != nil {
			response.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to end session")
			return
		}
	}
	h.clearRefreshCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", "missing user context")
		return
	}
	response.WriteJSON(w, http.StatusOK, userResponse{User: user})
}

func (h *Handler) updateCredentials(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", "missing user context")
		return
	}

	var req updateCredentialsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.NewLogin != nil {
		normalized := normalizeLogin(*req.NewLogin)
		req.NewLogin = &normalized
	}
	if req.NewPassword != nil && *req.NewPassword == "" {
		req.NewPassword = nil
	}

	updated, err := h.store.UpdateCredentials(r.Context(), user.ID, req.CurrentPassword, req.NewLogin, req.NewPassword)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidCredentials):
			response.WriteError(w, http.StatusUnauthorized, "unauthorized", "current password is incorrect")
		case errors.Is(err, ErrInvalidInput):
			response.WriteError(w, http.StatusBadRequest, "validation_error", err.Error())
		case errors.Is(err, ErrConflict):
			response.WriteError(w, http.StatusConflict, "conflict", "login already exists")
		default:
			response.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to update credentials")
		}
		return
	}

	h.writeTokenPair(w, r, updated)
}

func (h *Handler) writeTokenPair(w http.ResponseWriter, r *http.Request, user User) {
	accessToken, err := h.tokens.NewAccessToken(user)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to issue access token")
		return
	}

	refreshToken, err := h.store.CreateRefreshToken(r.Context(), user.ID, h.cfg.RefreshTokenTTL)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to issue refresh token")
		return
	}
	h.setRefreshCookie(w, r, refreshToken)

	response.WriteJSON(w, http.StatusOK, tokenResponse{
		AccessToken: accessToken,
		ExpiresIn:   int(h.cfg.AccessTokenTTL.Seconds()),
		User:        user,
	})
}

func (h *Handler) setRefreshCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    token,
		Path:     "/api/auth",
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(h.cfg.RefreshTokenTTL.Seconds()),
		Expires:  time.Now().Add(h.cfg.RefreshTokenTTL),
	})
}

func (h *Handler) clearRefreshCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     "/api/auth",
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
	})
}

func requestIsHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

func requestOriginAllowed(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && parsed.Host != "" && strings.EqualFold(parsed.Host, r.Host)
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

func normalizeLogin(login string) string {
	return strings.ToLower(strings.TrimSpace(login))
}
