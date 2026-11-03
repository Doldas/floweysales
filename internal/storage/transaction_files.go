package storage

import (
	"context"
	"errors"
	"time"
)

// Bounds on one user's saved transaction exports.
const (
	TransactionFileMaxBytes  = 2 << 20 // one file, plain text
	TransactionFilesMaxCount = 10
	TransactionFilesMaxTotal = 3 << 20 // all files of a user, plain text; fits one domain call
)

var (
	ErrTransactionFileNotFound = errors.New("transaction file not found")
	ErrTransactionFileLimit    = errors.New("too many or too large saved transaction files; delete one first")
)

// TransactionFile is the metadata of a saved export; the content is only
// ever returned sealed, by TransactionFileContents.
type TransactionFile struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	SizeBytes int       `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

// SealedTransactionFile is a saved export with its encrypted content.
type SealedTransactionFile struct {
	TransactionFile
	Ciphertext []byte
}

// ListTransactionFiles returns a user's saved exports, oldest first.
func (d *Database) ListTransactionFiles(ctx context.Context, userID int64) ([]TransactionFile, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT id,name,size_bytes,created_at FROM user_transaction_files WHERE user_id=? ORDER BY created_at, id", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TransactionFile{}
	for rows.Next() {
		var f TransactionFile
		if err := rows.Scan(&f.ID, &f.Name, &f.SizeBytes, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// TransactionFileContents returns a user's exports with their sealed content, oldest first.
func (d *Database) TransactionFileContents(ctx context.Context, userID int64) ([]SealedTransactionFile, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT id,name,size_bytes,created_at,ciphertext FROM user_transaction_files WHERE user_id=? ORDER BY created_at, id", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SealedTransactionFile{}
	for rows.Next() {
		var f SealedTransactionFile
		if err := rows.Scan(&f.ID, &f.Name, &f.SizeBytes, &f.CreatedAt, &f.Ciphertext); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// SaveTransactionFile stores an export, replacing one of the same name. The
// limits count the plain size, which the caller reports in sizeBytes.
func (d *Database) SaveTransactionFile(ctx context.Context, userID int64, name string, sizeBytes int, ciphertext []byte) (int64, error) {
	if sizeBytes > TransactionFileMaxBytes {
		return 0, ErrTransactionFileLimit
	}
	var count, total int
	if err := d.db.QueryRowContext(ctx, "SELECT COUNT(*), COALESCE(SUM(size_bytes),0) FROM user_transaction_files WHERE user_id=? AND name<>?", userID, name).Scan(&count, &total); err != nil {
		return 0, err
	}
	if count >= TransactionFilesMaxCount || total+sizeBytes > TransactionFilesMaxTotal {
		return 0, ErrTransactionFileLimit
	}
	if _, err := d.db.ExecContext(ctx,
		"INSERT INTO user_transaction_files(user_id,name,size_bytes,ciphertext) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE size_bytes=VALUES(size_bytes),ciphertext=VALUES(ciphertext),created_at=CURRENT_TIMESTAMP(6)",
		userID, name, sizeBytes, ciphertext); err != nil {
		return 0, err
	}
	var id int64
	err := d.db.QueryRowContext(ctx, "SELECT id FROM user_transaction_files WHERE user_id=? AND name=?", userID, name).Scan(&id)
	return id, err
}

// DeleteTransactionFile removes one of a user's exports.
func (d *Database) DeleteTransactionFile(ctx context.Context, userID, id int64) error {
	res, err := d.db.ExecContext(ctx, "DELETE FROM user_transaction_files WHERE user_id=? AND id=?", userID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTransactionFileNotFound
	}
	return nil
}
