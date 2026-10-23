package identity

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"time"
)

type ManagedUser struct {
	ID     int64  `json:"id"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	Active bool   `json:"active"`
}

func (s *Service) ListUsers(ctx context.Context) ([]ManagedUser, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,email,role,active FROM auth_users ORDER BY id DESC LIMIT 200")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []ManagedUser{}
	for rows.Next() {
		var u ManagedUser
		if err := rows.Scan(&u.ID, &u.Email, &u.Role, &u.Active); err != nil {
			return nil, err
		}
		list = append(list, u)
	}
	return list, rows.Err()
}
func (s *Service) InviteUser(ctx context.Context, rawEmail string) (string, error) {
	email, err := NormalizeEmail(rawEmail)
	if err != nil {
		return "", err
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	// Invited users cannot log in until their password is set.
	result, err := tx.ExecContext(ctx, "INSERT INTO auth_users(email,password_hash,role,active,created_at) VALUES(?,?,'USER',FALSE,UTC_TIMESTAMP(6))", email, "INVITED")
	if err != nil {
		return "", err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO auth_invitations(token_hash,user_id,expires_at) VALUES(?,?,?)", TokenDigest(token), id, time.Now().UTC().Add(48*time.Hour))
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return token, nil
}
func (s *Service) AcceptInvitation(ctx context.Context, token, password string) error {
	if len(token) != 43 {
		return ErrInvalidCredentials
	}
	encoded, err := HashPassword(password)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id int64
	err = tx.QueryRowContext(ctx, "SELECT user_id FROM auth_invitations WHERE token_hash=? AND expires_at>UTC_TIMESTAMP(6) FOR UPDATE", TokenDigest(token)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInvalidCredentials
	}
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, "UPDATE auth_users SET password_hash=?,active=TRUE WHERE id=? AND active=FALSE", encoded, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrInvalidCredentials
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM auth_invitations WHERE user_id=?", id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Service) UpdateUserRole(ctx context.Context, id int64, role string) error {
	if id <= 0 || !s.RoleExists(ctx, role) {
		return ErrInvalidBootstrap
	}
	_, err := s.db.ExecContext(ctx, "UPDATE auth_users SET role=? WHERE id=? AND active=TRUE", role, id)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, "DELETE FROM auth_sessions WHERE user_id=?", id)
	return err
}
func (s *Service) SetUserActive(ctx context.Context, id int64, active bool) error {
	if id <= 0 {
		return ErrInvalidBootstrap
	}
	_, err := s.db.ExecContext(ctx, "UPDATE auth_users SET active=? WHERE id=?", active, id)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, "DELETE FROM auth_sessions WHERE user_id=?", id)
	return err
}

func (s *Service) RevokeInvitation(ctx context.Context, token string) error {
	if len(token) != 43 {
		return ErrInvalidCredentials
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id int64
	err = tx.QueryRowContext(ctx, "SELECT user_id FROM auth_invitations WHERE token_hash=? FOR UPDATE", TokenDigest(token)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM auth_invitations WHERE user_id=?", id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM auth_users WHERE id=? AND active=FALSE", id); err != nil {
		return err
	}
	return tx.Commit()
}
