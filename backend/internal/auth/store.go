package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return count, nil
}

func (s *Store) CreateUser(ctx context.Context, login, password string) (User, error) {
	if !validLogin(login) || len(password) < 12 {
		return User{}, fmt.Errorf("%w: login must be 3-32 characters (letters, numbers, dot, dash or underscore) and password must be at least 12 characters", ErrInvalidInput)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, fmt.Errorf("hash password: %w", err)
	}

	user := User{ID: uuid.NewString(), Login: login, AuthVersion: 1}
	_, err = s.db.ExecContext(ctx,
		"INSERT INTO users(id, login, password_hash) VALUES (?, ?, ?)",
		user.ID,
		user.Login,
		string(hash),
	)
	if err != nil {
		if isUniqueConstraint(err) {
			return User{}, ErrConflict
		}
		return User{}, fmt.Errorf("insert user: %w", err)
	}

	return user, nil
}

func (s *Store) Authenticate(ctx context.Context, login, password string) (User, error) {
	if !validLogin(login) || password == "" {
		return User{}, ErrInvalidInput
	}

	var user User
	var passwordHash string
	err := s.db.QueryRowContext(ctx,
		"SELECT id, login, password_hash, auth_version FROM users WHERE login = ?",
		login,
	).Scan(&user.ID, &user.Login, &passwordHash, &user.AuthVersion)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrInvalidCredentials
		}
		return User{}, fmt.Errorf("select user: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)); err != nil {
		return User{}, ErrInvalidCredentials
	}

	return user, nil
}

func (s *Store) UserByID(ctx context.Context, id string) (User, error) {
	var user User
	err := s.db.QueryRowContext(ctx,
		"SELECT id, login, auth_version FROM users WHERE id = ?",
		id,
	).Scan(&user.ID, &user.Login, &user.AuthVersion)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, fmt.Errorf("select user by id: %w", err)
	}
	return user, nil
}

func (s *Store) UpdateCredentials(ctx context.Context, userID, currentPassword string, newLogin, newPassword *string) (User, error) {
	if currentPassword == "" || (newLogin == nil && newPassword == nil) {
		return User{}, ErrInvalidInput
	}
	if newLogin != nil && !validLogin(*newLogin) {
		return User{}, fmt.Errorf("%w: login must be 3-32 characters (letters, numbers, dot, dash or underscore)", ErrInvalidInput)
	}
	if newPassword != nil && len(*newPassword) < 12 {
		return User{}, fmt.Errorf("%w: password must be at least 12 characters", ErrInvalidInput)
	}

	var user User
	var passwordHash string
	if err := s.db.QueryRowContext(ctx, "SELECT id, login, password_hash, auth_version FROM users WHERE id = ?", userID).Scan(&user.ID, &user.Login, &passwordHash, &user.AuthVersion); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, fmt.Errorf("select credentials: %w", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(currentPassword)); err != nil {
		return User{}, ErrInvalidCredentials
	}

	if newLogin != nil {
		user.Login = *newLogin
	}
	if newPassword != nil {
		hash, err := bcrypt.GenerateFromPassword([]byte(*newPassword), bcrypt.DefaultCost)
		if err != nil {
			return User{}, fmt.Errorf("hash new password: %w", err)
		}
		passwordHash = string(hash)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, fmt.Errorf("begin credentials update: %w", err)
	}
	defer tx.Rollback()

	user.AuthVersion++
	if _, err := tx.ExecContext(ctx, "UPDATE users SET login = ?, password_hash = ?, auth_version = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", user.Login, passwordHash, user.AuthVersion, user.ID); err != nil {
		if isUniqueConstraint(err) {
			return User{}, ErrConflict
		}
		return User{}, fmt.Errorf("update credentials: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE refresh_tokens SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL", time.Now().UTC().Format(time.RFC3339), user.ID); err != nil {
		return User{}, fmt.Errorf("revoke refresh tokens: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return User{}, fmt.Errorf("commit credentials update: %w", err)
	}
	return user, nil
}

func (s *Store) CreateRefreshToken(ctx context.Context, userID string, ttl time.Duration) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}

	_, err = s.db.ExecContext(ctx,
		"INSERT INTO refresh_tokens(id, user_id, token_hash, expires_at) VALUES (?, ?, ?, ?)",
		uuid.NewString(),
		userID,
		tokenHash(token),
		time.Now().Add(ttl).UTC().Format(time.RFC3339),
	)
	if err != nil {
		return "", fmt.Errorf("insert refresh token: %w", err)
	}

	return token, nil
}

func (s *Store) RevokeRefreshToken(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE refresh_tokens SET revoked_at = ? WHERE token_hash = ? AND revoked_at IS NULL", time.Now().UTC().Format(time.RFC3339), tokenHash(token)); err != nil {
		return fmt.Errorf("revoke refresh token: %w", err)
	}
	return nil
}

func (s *Store) RotateRefreshToken(ctx context.Context, token string, ttl time.Duration) (User, string, error) {
	hash := tokenHash(token)
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, "", fmt.Errorf("begin refresh rotation: %w", err)
	}
	defer tx.Rollback()

	var refreshID string
	var user User
	err = tx.QueryRowContext(ctx, `
		SELECT refresh_tokens.id, users.id, users.login, users.auth_version
		FROM refresh_tokens
		JOIN users ON users.id = refresh_tokens.user_id
		WHERE refresh_tokens.token_hash = ?
		  AND refresh_tokens.revoked_at IS NULL
		  AND refresh_tokens.expires_at > ?
	`, hash, now).Scan(&refreshID, &user.ID, &user.Login, &user.AuthVersion)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, "", ErrInvalidCredentials
		}
		return User{}, "", fmt.Errorf("select refresh token: %w", err)
	}

	if _, err := tx.ExecContext(ctx, "UPDATE refresh_tokens SET revoked_at = ? WHERE id = ?", now, refreshID); err != nil {
		return User{}, "", fmt.Errorf("revoke refresh token: %w", err)
	}

	newToken, err := randomToken()
	if err != nil {
		return User{}, "", err
	}

	if _, err := tx.ExecContext(ctx,
		"INSERT INTO refresh_tokens(id, user_id, token_hash, expires_at) VALUES (?, ?, ?, ?)",
		uuid.NewString(),
		user.ID,
		tokenHash(newToken),
		time.Now().Add(ttl).UTC().Format(time.RFC3339),
	); err != nil {
		return User{}, "", fmt.Errorf("insert rotated refresh token: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return User{}, "", fmt.Errorf("commit refresh rotation: %w", err)
	}

	return user, newToken, nil
}

func validLogin(login string) bool {
	if len(login) < 3 || len(login) > 32 {
		return false
	}
	for _, char := range login {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '.' && char != '-' && char != '_' {
			return false
		}
	}
	return true
}

func randomToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func isUniqueConstraint(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "unique")
}
