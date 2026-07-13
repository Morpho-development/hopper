package auth

import "errors"

var (
	ErrConflict           = errors.New("conflict")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidInput       = errors.New("invalid input")
	ErrNotFound           = errors.New("not found")
)

type User struct {
	ID          string `json:"id"`
	Login       string `json:"login"`
	AuthVersion int64  `json:"-"`
}

type credentialsRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

type updateCredentialsRequest struct {
	CurrentPassword string  `json:"currentPassword"`
	NewLogin        *string `json:"newLogin"`
	NewPassword     *string `json:"newPassword"`
}

type userResponse struct {
	User User `json:"user"`
}

type tokenResponse struct {
	AccessToken string `json:"accessToken"`
	ExpiresIn   int    `json:"expiresIn"`
	User        User   `json:"user"`
}
