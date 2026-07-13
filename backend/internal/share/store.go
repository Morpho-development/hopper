package share

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Create(ctx context.Context, input CreateInput) (CreatedShare, error) {
	if input.ExpiresInSeconds <= 0 || input.ExpiresInSeconds > 365*24*60*60 {
		return CreatedShare{}, ErrInvalidInput
	}
	if input.MaxDownloads != nil && *input.MaxDownloads <= 0 {
		return CreatedShare{}, ErrInvalidInput
	}

	token, err := randomToken()
	if err != nil {
		return CreatedShare{}, err
	}

	expiresAt := time.Now().UTC().Add(time.Duration(input.ExpiresInSeconds) * time.Second)
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO shares(id, file_id, owner_id, token_hash, expires_at, max_downloads)
		VALUES (?, ?, ?, ?, ?, ?)
	`, uuid.NewString(), input.FileID, input.OwnerID, tokenHash(token), expiresAt.Format(time.RFC3339), input.MaxDownloads)
	if err != nil {
		return CreatedShare{}, fmt.Errorf("insert share: %w", err)
	}

	return CreatedShare{
		Token:        token,
		ExpiresAt:    expiresAt,
		MaxDownloads: input.MaxDownloads,
	}, nil
}

func (s *Store) Resolve(ctx context.Context, token string) (PublicFile, error) {
	if token == "" {
		return PublicFile{}, ErrNotFound
	}

	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PublicFile{}, fmt.Errorf("begin resolve share: %w", err)
	}
	defer tx.Rollback()

	var shareID string
	var file PublicFile
	err = tx.QueryRowContext(ctx, `
		SELECT shares.id,
		       files.original_name, files.storage_name, files.mime_type, files.size_bytes
		FROM shares
		JOIN files ON files.id = shares.file_id
		WHERE shares.token_hash = ?
		  AND shares.revoked_at IS NULL
		  AND shares.expires_at > ?
	`, tokenHash(token), now).Scan(
		&shareID,
		&file.OriginalName,
		&file.StorageName,
		&file.MimeType,
		&file.SizeBytes,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PublicFile{}, ErrNotFound
		}
		return PublicFile{}, fmt.Errorf("select share: %w", err)
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE shares
		SET download_count = download_count + 1
		WHERE id = ?
		  AND (max_downloads IS NULL OR download_count < max_downloads)
	`, shareID)
	if err != nil {
		return PublicFile{}, fmt.Errorf("increment download count: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return PublicFile{}, fmt.Errorf("count updated share rows: %w", err)
	}
	if rows != 1 {
		return PublicFile{}, ErrNotFound
	}

	if err := tx.Commit(); err != nil {
		return PublicFile{}, fmt.Errorf("commit resolve share: %w", err)
	}

	return file, nil
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
