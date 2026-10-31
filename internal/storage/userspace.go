package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"
	"strings"
)

const UserspaceMaxFileBytes = 2 * 1024 * 1024
const UserspaceMaxFiles = 256

type UserspaceFile struct {
	Path        string `json:"path"`
	ContentType string `json:"content_type"`
	Size        int    `json:"size"`
}

// CleanUserspacePath is a logical POSIX path, never an OS filesystem path.
func CleanUserspacePath(input string) (string, error) {
	if input == "" || len(input) > 512 || strings.ContainsAny(input, "\\\x00") || !strings.HasPrefix(input, "/") {
		return "", errors.New("invalid userspace path")
	}
	for _, part := range strings.Split(input, "/") {
		if part == "." || part == ".." {
			return "", errors.New("relative path segments are forbidden")
		}
	}
	cleaned := path.Clean(input)
	if cleaned == "/" {
		return "", errors.New("root is not a file")
	}
	return cleaned, nil
}

func (d *Database) ListUserspaceFiles(ctx context.Context, owner int64) ([]UserspaceFile, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT path,content_type,byte_size FROM userspace_files WHERE owner_id=? ORDER BY path LIMIT ?", owner, UserspaceMaxFiles)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	files := []UserspaceFile{}
	for rows.Next() {
		var f UserspaceFile
		if err := rows.Scan(&f.Path, &f.ContentType, &f.Size); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}
func (d *Database) ReadUserspaceFile(ctx context.Context, owner int64, rawPath string) ([]byte, string, error) {
	p, err := CleanUserspacePath(rawPath)
	if err != nil {
		return nil, "", err
	}
	var content []byte
	var mime string
	err = d.db.QueryRowContext(ctx, "SELECT content,content_type FROM userspace_files WHERE owner_id=? AND path=?", owner, p).Scan(&content, &mime)
	return content, mime, err
}
func (d *Database) WriteUserspaceFile(ctx context.Context, owner int64, rawPath, mime string, content []byte) error {
	p, err := CleanUserspacePath(rawPath)
	if err != nil {
		return err
	}
	if owner <= 0 || len(content) > UserspaceMaxFileBytes {
		return errors.New("userspace file quota exceeded or invalid owner")
	}
	if mime != "text/plain" && mime != "text/markdown" && mime != "text/csv" && mime != "application/json" && mime != "application/xml" {
		return errors.New("invalid content type")
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Lock owner via account row, serializing concurrent writes and quota checks.
	var id int64
	if err = tx.QueryRowContext(ctx, "SELECT id FROM auth_users WHERE id=? FOR UPDATE", owner).Scan(&id); err != nil {
		return err
	}
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM userspace_files WHERE owner_id=?", owner).Scan(&count); err != nil {
		return err
	}
	var exists int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM userspace_files WHERE owner_id=? AND path=?", owner, p).Scan(&exists); err != nil {
		return err
	}
	if count >= UserspaceMaxFiles && exists == 0 {
		return fmt.Errorf("maximum %d files per userspace", UserspaceMaxFiles)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO userspace_files(owner_id,path,content_type,content,byte_size)
 VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE content_type=VALUES(content_type),content=VALUES(content),byte_size=VALUES(byte_size),updated_at=UTC_TIMESTAMP(6)`, owner, p, mime, content, len(content))
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (d *Database) DeleteUserspaceFile(ctx context.Context, owner int64, rawPath string) error {
	p, err := CleanUserspacePath(rawPath)
	if err != nil {
		return err
	}
	result, err := d.db.ExecContext(ctx, "DELETE FROM userspace_files WHERE owner_id=? AND path=?", owner, p)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// TransferUserspaceFile atomically moves or copies a file within the same owner's workspace.
// Existing destinations are never overwritten.
func (d *Database) TransferUserspaceFile(ctx context.Context, owner int64, source, destination string, copyFile bool) error {
	src, err := CleanUserspacePath(source)
	if err != nil {
		return err
	}
	dst, err := CleanUserspacePath(destination)
	if err != nil {
		return err
	}
	if owner <= 0 || src == dst || strings.HasSuffix(src, "/.folder") || strings.HasSuffix(dst, "/.folder") {
		return errors.New("invalid source or destination")
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id int64
	if err = tx.QueryRowContext(ctx, "SELECT id FROM auth_users WHERE id=? FOR UPDATE", owner).Scan(&id); err != nil {
		return err
	}
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM userspace_files WHERE owner_id=? AND path=?", owner, src).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM userspace_files WHERE owner_id=? AND path=?", owner, dst).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return errors.New("destination already exists")
	}
	if copyFile {
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM userspace_files WHERE owner_id=?", owner).Scan(&count); err != nil {
			return err
		}
		if count >= UserspaceMaxFiles {
			return fmt.Errorf("maximum %d files per userspace", UserspaceMaxFiles)
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO userspace_files(owner_id,path,content_type,content,byte_size) SELECT owner_id,?,content_type,content,byte_size FROM userspace_files WHERE owner_id=? AND path=?", dst, owner, src)
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE userspace_files SET path=?,updated_at=UTC_TIMESTAMP(6) WHERE owner_id=? AND path=?", dst, owner, src)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func userspaceFolderPrefix(rawPath string) (string, error) {
	p, err := CleanUserspacePath(rawPath)
	if err != nil {
		return "", err
	}
	return p + "/", nil
}

// DeleteUserspaceFolder removes a folder marker and every file below the
// exact logical path. The prefix comparison deliberately avoids SQL LIKE,
// where folder names containing '_' or '%' would otherwise act as wildcards.
func (d *Database) DeleteUserspaceFolder(ctx context.Context, owner int64, rawPath string) (int64, error) {
	prefix, err := userspaceFolderPrefix(rawPath)
	if err != nil {
		return 0, err
	}
	if owner <= 0 {
		return 0, errors.New("invalid owner")
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	// Share the account lock with concurrent file writes to serialize deletion.
	var id int64
	if err := tx.QueryRowContext(ctx, "SELECT id FROM auth_users WHERE id=? FOR UPDATE", owner).Scan(&id); err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx,
		"DELETE FROM userspace_files WHERE owner_id=? AND LEFT(path,CHAR_LENGTH(?))=?",
		owner, prefix, prefix)
	if err != nil {
		return 0, err
	}
	deleted, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if deleted == 0 {
		return 0, sql.ErrNoRows
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return deleted, nil
}

func (d *Database) FlowOwner(ctx context.Context, flowID int64) (int64, error) {
	var owner int64
	err := d.db.QueryRowContext(ctx, "SELECT created_by FROM agent_flows WHERE id=?", flowID).Scan(&owner)
	return owner, err
}
