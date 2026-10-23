package server

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureAPISecretsKeyPersistsAcrossRestarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets", "key")
	t.Setenv(apiSecretEnv, "")
	t.Setenv("FLOWEYSALES_SECRETS_KEY_FILE", path)
	if err := EnsureAPISecretsKey(); err != nil {
		t.Fatal(err)
	}
	first := os.Getenv(apiSecretEnv)
	if !validAPISecretsKey(first) {
		t.Fatal("generated key must have 32 bytes")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("unexpected key file permissions: %o", info.Mode().Perm())
	}
	t.Setenv(apiSecretEnv, "")
	if err := EnsureAPISecretsKey(); err != nil {
		t.Fatal(err)
	}
	if os.Getenv(apiSecretEnv) != first {
		t.Fatal("key changed after restart")
	}
}

func TestEnsureAPISecretsKeyPreservesEnvironmentOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	existing := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 32)))
	override := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("y", 32)))
	if err := os.WriteFile(path, []byte(existing), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FLOWEYSALES_SECRETS_KEY_FILE", path)
	t.Setenv(apiSecretEnv, override)
	if err := EnsureAPISecretsKey(); err != nil {
		t.Fatal(err)
	}
	if os.Getenv(apiSecretEnv) != override {
		t.Fatal("environment override was changed")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != existing {
		t.Fatal("persisted key was modified by environment override")
	}
}

func TestEnsureAPISecretsKeyRejectsInvalidKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	t.Setenv("FLOWEYSALES_SECRETS_KEY_FILE", path)
	t.Setenv(apiSecretEnv, "invalid")
	if err := EnsureAPISecretsKey(); err == nil {
		t.Fatal("invalid environment key accepted")
	}
	t.Setenv(apiSecretEnv, "")
	if err := os.WriteFile(path, []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureAPISecretsKey(); err == nil {
		t.Fatal("invalid persisted key was silently replaced")
	}
}
