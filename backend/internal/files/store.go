package files

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Store struct {
	db          *sql.DB
	storagePath string
}

func NewStore(db *sql.DB, storagePath string) *Store {
	return &Store{db: db, storagePath: storagePath}
}

func (s *Store) UsedBytes(ctx context.Context, ownerID string) (int64, error) {
	var used sql.NullInt64
	if err := s.db.QueryRowContext(ctx, "SELECT SUM(size_bytes) FROM files WHERE owner_id = ?", ownerID).Scan(&used); err != nil {
		return 0, fmt.Errorf("sum used bytes: %w", err)
	}
	if !used.Valid {
		return 0, nil
	}
	return used.Int64, nil
}

func (s *Store) Create(ctx context.Context, input CreateInput) (Record, error) {
	if err := os.MkdirAll(s.storagePath, 0o700); err != nil {
		return Record{}, fmt.Errorf("create storage dir: %w", err)
	}

	record := Record{
		ID:           uuid.NewString(),
		OwnerID:      input.OwnerID,
		FolderID:     input.FolderID,
		OriginalName: input.OriginalName,
		StorageName:  uuid.NewString(),
		MimeType:     input.MimeType,
		SizeBytes:    input.SizeBytes,
	}

	path, err := SafeJoin(s.storagePath, record.StorageName)
	if err != nil {
		return Record{}, err
	}

	destination, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return Record{}, fmt.Errorf("create storage file: %w", err)
	}

	hasher := sha256.New()
	written, copyErr := io.Copy(destination, io.TeeReader(input.Content, hasher))
	closeErr := destination.Close()
	if copyErr != nil {
		_ = os.Remove(path)
		return Record{}, fmt.Errorf("write storage file: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return Record{}, fmt.Errorf("close storage file: %w", closeErr)
	}
	if written != input.SizeBytes {
		_ = os.Remove(path)
		return Record{}, fmt.Errorf("written size mismatch: expected %d, got %d", input.SizeBytes, written)
	}

	checksum := hex.EncodeToString(hasher.Sum(nil))
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO files(id, owner_id, folder_id, original_name, storage_name, mime_type, size_bytes, checksum_sha256)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, record.ID, record.OwnerID, record.FolderID, record.OriginalName, record.StorageName, record.MimeType, record.SizeBytes, checksum)
	if err != nil {
		_ = os.Remove(path)
		return Record{}, fmt.Errorf("insert file record: %w", err)
	}

	return record, nil
}

func (s *Store) ByID(ctx context.Context, ownerID, id string) (Record, error) {
	var record Record
	var createdAt string
	var updatedAt string
	var folderID sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT id, owner_id, folder_id, original_name, storage_name, mime_type, size_bytes, created_at, updated_at
		FROM files
		WHERE id = ? AND owner_id = ?
	`, id, ownerID).Scan(
		&record.ID,
		&record.OwnerID,
		&folderID,
		&record.OriginalName,
		&record.StorageName,
		&record.MimeType,
		&record.SizeBytes,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Record{}, ErrNotFound
		}
		return Record{}, fmt.Errorf("select file: %w", err)
	}

	if folderID.Valid {
		record.FolderID = &folderID.String
	}
	record.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	record.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return record, nil
}

func (s *Store) CreateFolder(ctx context.Context, ownerID, name string, parentID *string) (Folder, error) {
	if _, err := CleanDisplayName(name); err != nil {
		return Folder{}, err
	}
	if err := s.EnsureFolderOwned(ctx, ownerID, parentID); err != nil {
		return Folder{}, err
	}

	folder := Folder{
		ID:       uuid.NewString(),
		OwnerID:  ownerID,
		ParentID: parentID,
		Name:     name,
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO folders(id, owner_id, parent_id, name)
		VALUES (?, ?, ?, ?)
	`, folder.ID, folder.OwnerID, folder.ParentID, folder.Name)
	if err != nil {
		if isUniqueConstraint(err) {
			return Folder{}, ErrConflict
		}
		return Folder{}, fmt.Errorf("insert folder: %w", err)
	}

	return folder, nil
}

func (s *Store) List(ctx context.Context, ownerID string, folderID *string, page, pageSize int) ([]ListItem, int, error) {
	if err := s.EnsureFolderOwned(ctx, ownerID, folderID); err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	items := make([]ListItem, 0, pageSize)

	foldersQuery := `
		SELECT id, 'folder', name, '', 0, created_at, updated_at
		FROM folders
		WHERE owner_id = ? AND `
	filesQuery := `
		SELECT id, 'file', original_name, mime_type, size_bytes, created_at, updated_at
		FROM files
		WHERE owner_id = ? AND `
	countFoldersQuery := "SELECT COUNT(*) FROM folders WHERE owner_id = ? AND "
	countFilesQuery := "SELECT COUNT(*) FROM files WHERE owner_id = ? AND "

	args := []any{ownerID}
	if folderID == nil {
		foldersQuery += "parent_id IS NULL"
		filesQuery += "folder_id IS NULL"
		countFoldersQuery += "parent_id IS NULL"
		countFilesQuery += "folder_id IS NULL"
	} else {
		foldersQuery += "parent_id = ?"
		filesQuery += "folder_id = ?"
		countFoldersQuery += "parent_id = ?"
		countFilesQuery += "folder_id = ?"
		args = append(args, *folderID)
	}

	query := foldersQuery + " UNION ALL " + filesQuery + " ORDER BY 2 DESC, 3 COLLATE NOCASE ASC LIMIT ? OFFSET ?"
	queryArgs := append(append([]any{}, args...), args...)
	queryArgs = append(queryArgs, pageSize, offset)

	rows, err := s.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list files: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var item ListItem
		if err := rows.Scan(&item.ID, &item.Type, &item.Name, &item.MimeType, &item.SizeBytes, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan list item: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate list items: %w", err)
	}

	total, err := s.countListItems(ctx, countFoldersQuery, countFilesQuery, args)
	if err != nil {
		return nil, 0, err
	}

	return items, total, nil
}

func (s *Store) Update(ctx context.Context, ownerID, id string, input UpdateInput) (Record, error) {
	record, err := s.ByID(ctx, ownerID, id)
	if err != nil {
		return Record{}, err
	}

	if input.Name != nil {
		name, err := CleanDisplayName(*input.Name)
		if err != nil {
			return Record{}, err
		}
		record.OriginalName = name
	}

	if input.FolderIDSet {
		if err := s.EnsureFolderOwned(ctx, ownerID, input.FolderID); err != nil {
			return Record{}, err
		}
		record.FolderID = input.FolderID
	}

	_, err = s.db.ExecContext(ctx, `
		UPDATE files
		SET original_name = ?, folder_id = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND owner_id = ?
	`, record.OriginalName, record.FolderID, record.ID, ownerID)
	if err != nil {
		return Record{}, fmt.Errorf("update file: %w", err)
	}

	return s.ByID(ctx, ownerID, id)
}

func (s *Store) Delete(ctx context.Context, ownerID, id string) error {
	record, err := s.ByID(ctx, ownerID, id)
	if err != nil {
		return err
	}

	path, err := SafeJoin(s.storagePath, record.StorageName)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, "DELETE FROM files WHERE id = ? AND owner_id = ?", id, ownerID)
	if err != nil {
		return fmt.Errorf("delete file record: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete storage file: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete: %w", err)
	}

	return nil
}

func (s *Store) Purge(ctx context.Context, ownerID string) (int, int64, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT storage_name, size_bytes FROM files WHERE owner_id = ?", ownerID)
	if err != nil {
		return 0, 0, fmt.Errorf("list purge files: %w", err)
	}
	var storageNames []string
	var freedBytes int64
	for rows.Next() {
		var storageName string
		var size int64
		if err := rows.Scan(&storageName, &size); err != nil {
			rows.Close()
			return 0, 0, fmt.Errorf("scan purge file: %w", err)
		}
		storageNames = append(storageNames, storageName)
		freedBytes += size
	}
	if err := rows.Close(); err != nil {
		return 0, 0, fmt.Errorf("close purge files: %w", err)
	}
	if err := rows.Err(); err != nil {
		return 0, 0, fmt.Errorf("iterate purge files: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("begin purge: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM files WHERE owner_id = ?", ownerID); err != nil {
		return 0, 0, fmt.Errorf("purge file records: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM folders WHERE owner_id = ?", ownerID); err != nil {
		return 0, 0, fmt.Errorf("purge folders: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("commit purge: %w", err)
	}

	for _, storageName := range storageNames {
		path, err := SafeJoin(s.storagePath, storageName)
		if err != nil {
			return len(storageNames), freedBytes, fmt.Errorf("resolve purged file: %w", err)
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return len(storageNames), freedBytes, fmt.Errorf("remove purged file: %w", err)
		}
	}
	return len(storageNames), freedBytes, nil
}

func (s *Store) EnsureFolderOwned(ctx context.Context, ownerID string, folderID *string) error {
	if folderID == nil {
		return nil
	}

	var exists int
	err := s.db.QueryRowContext(ctx, "SELECT 1 FROM folders WHERE id = ? AND owner_id = ? LIMIT 1", *folderID, ownerID).Scan(&exists)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("check folder ownership: %w", err)
	}
	return nil
}

func (s *Store) countListItems(ctx context.Context, foldersQuery, filesQuery string, args []any) (int, error) {
	var folderCount int
	if err := s.db.QueryRowContext(ctx, foldersQuery, args...).Scan(&folderCount); err != nil {
		return 0, fmt.Errorf("count folders: %w", err)
	}

	var fileCount int
	if err := s.db.QueryRowContext(ctx, filesQuery, args...).Scan(&fileCount); err != nil {
		return 0, fmt.Errorf("count files: %w", err)
	}

	return folderCount + fileCount, nil
}

func isUniqueConstraint(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "unique")
}
