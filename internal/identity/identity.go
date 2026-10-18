// Package identity provides single-owner database-backed authentication.
// Auth schema is managed by Liquibase; this package never creates tables.
package identity

import (
    "context"
    "crypto/rand"
    "crypto/sha256"
    "crypto/subtle"
    "crypto/pbkdf2"
    "database/sql"
    "encoding/base64"
    "encoding/hex"
    "errors"
    "fmt"
    "net/mail"
    "strings"
    "time"

    _ "github.com/go-sql-driver/mysql"
)

const (
    PasswordIterations = 600000 // OWASP PBKDF2-HMAC-SHA256 FIPS-compatible minimum.
    SessionTTL = 12 * time.Hour
)

var ErrInvalidCredentials = errors.New("invalid credentials")
var ErrInvalidBootstrap = errors.New("invalid owner bootstrap settings")

type User struct {
    ID int64 `json:"id"`
    Email string `json:"email"`
    Role string `json:"role"`
}

type Service struct { db *sql.DB }

func Open(dsn string) (*Service, error) {
    if strings.TrimSpace(dsn) == "" { return nil, errors.New("AUTH_MYSQL_DSN is required when AUTH_ENABLED=true") }
    db, err := sql.Open("mysql", dsn)
    if err != nil { return nil, fmt.Errorf("open identity MySQL: %w", err) }
    db.SetMaxOpenConns(10)
    db.SetMaxIdleConns(5)
    db.SetConnMaxLifetime(15 * time.Minute)
    ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
    defer cancel()
    if err := db.PingContext(ctx); err != nil { db.Close(); return nil, fmt.Errorf("identity MySQL unreachable: %w", err) }
    // Fail closed if the Liquibase schema has not been applied.
    var count int
    if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_users").Scan(&count); err != nil {
        db.Close()
        return nil, fmt.Errorf("auth_users missing: run Liquibase migrations first: %w", err)
    }
    return &Service{db: db}, nil
}

func (s *Service) Close() error { return s.db.Close() }

func NormalizeEmail(raw string) (string, error) {
    email := strings.ToLower(strings.TrimSpace(raw))
    if len(email) > 254 || len(email) < 3 { return "", ErrInvalidBootstrap }
    parsed, err := mail.ParseAddress(email)
    if err != nil || parsed.Address != email { return "", ErrInvalidBootstrap }
    return email, nil
}

func HashPassword(password string) (string, error) {
    if len(password) < 14 || len(password) > 1024 { return "", ErrInvalidBootstrap }
    salt := make([]byte, 16)
    if _, err := rand.Read(salt); err != nil { return "", err }
    digest, err := pbkdf2.Key(sha256.New, password, salt, PasswordIterations, 32)
    if err != nil { return "", err }
    return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", PasswordIterations,
        base64.RawURLEncoding.EncodeToString(salt), base64.RawURLEncoding.EncodeToString(digest)), nil
}

func VerifyPassword(encoded, password string) bool {
    parts := strings.Split(encoded, "$")
    if len(parts) != 4 || parts[0] != "pbkdf2-sha256" || parts[1] != "600000" { return false }
    salt, err1 := base64.RawURLEncoding.DecodeString(parts[2])
    expected, err2 := base64.RawURLEncoding.DecodeString(parts[3])
    if err1 != nil || err2 != nil || len(salt) != 16 || len(expected) != 32 { return false }
    actual, err := pbkdf2.Key(sha256.New, password, salt, PasswordIterations, 32)
    return err == nil && subtle.ConstantTimeCompare(actual, expected) == 1
}

// BootstrapOwner creates the FIRST owner only. Existing accounts are never
// reset by configuration, and the app intentionally does not offer sign-up.
func (s *Service) BootstrapOwner(ctx context.Context, rawEmail, password string) error {
    email, err := NormalizeEmail(rawEmail)
    if err != nil { return err }
    if len(password) < 14 { return ErrInvalidBootstrap }
    var count int
    if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_users").Scan(&count); err != nil { return err }
    if count > 0 { return nil } // Never rotate credentials on a restart.
    encoded, err := HashPassword(password)
    if err != nil { return err }
    _, err = s.db.ExecContext(ctx,
        "INSERT INTO auth_users (email, password_hash, role, active, created_at) VALUES (?, ?, 'owner', TRUE, ?)",
        email, encoded, time.Now().UTC())
    return err
}

func (s *Service) OwnerExists(ctx context.Context) (bool, error) {
    var count int
    err := s.db.QueryRowContext(ctx,"SELECT COUNT(*) FROM auth_users").Scan(&count)
    return count != 0, err
}

func (s *Service) Login(ctx context.Context, rawEmail, password string) (string, User, error) {
    email, err := NormalizeEmail(rawEmail)
    if err != nil || password == "" { return "", User{}, ErrInvalidCredentials }
    var u User
    var stored string
    var active bool
    err = s.db.QueryRowContext(ctx,
        "SELECT id, email, role, password_hash, active FROM auth_users WHERE email = ? LIMIT 1", email).
        Scan(&u.ID, &u.Email, &u.Role, &stored, &active)
    if errors.Is(err, sql.ErrNoRows) {
        // Spend comparable derivation work for unknown accounts to avoid easy enumeration.
        _, _ = pbkdf2.Key(sha256.New, password, []byte("flowey-invalid!!"), PasswordIterations, 32)
        return "", User{}, ErrInvalidCredentials
    }
    if err != nil { return "", User{}, err }
    if !VerifyPassword(stored, password) || !active || u.Role != "owner" { return "", User{}, ErrInvalidCredentials }
    secret := make([]byte, 32)
    if _, err := rand.Read(secret); err != nil { return "", User{}, err }
    token := base64.RawURLEncoding.EncodeToString(secret)
    digest := TokenDigest(token)
    now := time.Now().UTC()
    _, err = s.db.ExecContext(ctx,
        "INSERT INTO auth_sessions (token_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)",
        digest, u.ID, now, now.Add(SessionTTL))
    if err != nil { return "", User{}, err }
    return token, u, nil
}

func TokenDigest(token string) string {
    sum := sha256.Sum256([]byte(token))
    return hex.EncodeToString(sum[:])
}

func (s *Service) Authenticate(ctx context.Context, token string) (User, bool) {
    if len(token) != 43 { return User{}, false }
    var u User
    err := s.db.QueryRowContext(ctx,
        `SELECT u.id, u.email, u.role FROM auth_sessions s
           JOIN auth_users u ON s.user_id = u.id
          WHERE s.token_hash = ? AND s.expires_at > ? AND u.active = TRUE AND u.role = 'owner'`,
        TokenDigest(token), time.Now().UTC()).Scan(&u.ID, &u.Email, &u.Role)
    return u, err == nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
    if len(token) != 43 { return nil }
    _, err := s.db.ExecContext(ctx, "DELETE FROM auth_sessions WHERE token_hash = ?", TokenDigest(token))
    return err
}
