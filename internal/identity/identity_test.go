package identity

import (
    "strings"
    "testing"
)

func TestPasswordHashAndVerify(t *testing.T) {
    hashed, err := HashPassword("correct horse battery stable")
    if err != nil { t.Fatal(err) }
    if strings.Contains(hashed, "correct horse") { t.Fatal("plaintext stored") }
    if !VerifyPassword(hashed, "correct horse battery stable") { t.Fatal("valid password rejected") }
    if VerifyPassword(hashed, "wrong password") { t.Fatal("bad password accepted") }
    if VerifyPassword("invalid", "anything") { t.Fatal("invalid hash accepted") }
}

func TestTokenDigest(t *testing.T) {
    if TokenDigest("secret") == "secret" { t.Fatal("raw session token retained") }
    if TokenDigest("secret") != TokenDigest("secret") { t.Fatal("digest is not deterministic") }
}

func TestValidateIdentityInputs(t *testing.T) {
    if _, err := HashPassword("short"); err == nil { t.Fatal("weak password accepted") }
    if email, err := NormalizeEmail("  OWNER@Example.COM  "); err != nil || email != "owner@example.com" { t.Fatalf("email = %s err = %v", email, err) }
    if _, err := NormalizeEmail("not an address"); err == nil { t.Fatal("invalid email accepted") }
}
