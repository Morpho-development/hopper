package bootstrap

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"strings"

	"hopper/backend/internal/auth"
	"hopper/backend/internal/config"
)

type Result struct {
	Created           bool
	Login             string
	Password          string
	PasswordGenerated bool
}

func EnsureFirstUser(ctx context.Context, cfg config.Config, database *sql.DB) (Result, error) {
	store := auth.NewStore(database)

	count, err := store.CountUsers(ctx)
	if err != nil {
		return Result{}, err
	}
	if count > 0 {
		return Result{}, nil
	}

	login := strings.TrimSpace(strings.ToLower(cfg.FirstRunAdminLogin))
	password := cfg.FirstRunAdminPassword
	passwordGenerated := false
	if password == "" {
		if cfg.AppEnv == "production" {
			return Result{}, fmt.Errorf("FIRST_RUN_ADMIN_PASSWORD is required for an empty production instance")
		}
		var err error
		password, err = randomPassword()
		if err != nil {
			return Result{}, err
		}
		passwordGenerated = true
	}

	user, err := store.CreateUser(ctx, login, password)
	if err != nil {
		return Result{}, fmt.Errorf("create first-run admin user: %w", err)
	}

	return Result{
		Created:           true,
		Login:             user.Login,
		Password:          password,
		PasswordGenerated: passwordGenerated,
	}, nil
}

func randomPassword() (string, error) {
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate first-run admin password: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}
