package server

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnsureAPISecretsKey initializes the secret encryption key before any stored
// credentials are restored. Explicit environment configuration takes priority.
// Otherwise, a randomly generated key is persisted across server restarts.
func EnsureAPISecretsKey() error {
	if value := strings.TrimSpace(os.Getenv(apiSecretEnv)); value != "" {
		if !validAPISecretsKey(value) {
			return fmt.Errorf("%s must contain 32 random bytes encoded as standard base64", apiSecretEnv)
		}
		return nil
	}

	path := strings.TrimSpace(os.Getenv("FLOWEYSALES_SECRETS_KEY_FILE"))
	if path == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return fmt.Errorf("find secrets key directory: %w", err)
		}
		path = filepath.Join(configDir, "floweysales", "secrets.key")
	}

	// Prefer an existing key; never silently replace it if it is corrupt.
	data, err := os.ReadFile(path)
	if err == nil {
		return usePersistedAPISecretsKey(data)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read persisted secrets key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create secrets key directory: %w", err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return fmt.Errorf("generate secrets key: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(key)
	// O_EXCL prevents accidentally overwriting an existing key.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read concurrently created secrets key: %w", err)
		}
		return usePersistedAPISecretsKey(data)
	}
	if err != nil {
		return fmt.Errorf("create secrets key file: %w", err)
	}
	if _, err = file.WriteString(encoded + "\n"); err != nil {
		file.Close()
		return fmt.Errorf("write secrets key file: %w", err)
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync secrets key file: %w", err)
	}
	if err = file.Close(); err != nil {
		return fmt.Errorf("close secrets key file: %w", err)
	}
	if err := os.Setenv(apiSecretEnv, encoded); err != nil {
		return err
	}
	return nil
}

func validAPISecretsKey(value string) bool {
	key, err := base64.StdEncoding.DecodeString(value)
	return err == nil && len(key) == 32
}

func usePersistedAPISecretsKey(data []byte) error {
	value := strings.TrimSpace(string(data))
	if !validAPISecretsKey(value) {
		return errors.New("persisted secrets key is invalid; restore the original key instead of regenerating it")
	}
	return os.Setenv(apiSecretEnv, value)
}
