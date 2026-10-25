// Package redact masks credentials in text that is persisted for diagnosis,
// such as flow run traces. It combines patterns for common credential
// formats with the exact values of secrets the server itself holds.
package redact

import (
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

// Mask replaces every redacted value.
const Mask = "[REDACTED]"

// minSecretLen keeps short registered values (which would also match
// ordinary words) from being masked.
const minSecretLen = 8

type rule struct {
	re   *regexp.Regexp
	repl string
}

var rules = []rule{
	{regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`), Mask},
	// An Authorization header value is masked whole, scheme included.
	{regexp.MustCompile(`(?i)(\bauthorization"?\s*[:=]\s*"?)[^"\r\n]{4,}`), "${1}" + Mask},
	{regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]{8,}`), "$1 " + Mask},
	{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`), Mask},
	{regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{16,}`), Mask},
	{regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,})`), Mask},
	{regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`), Mask},
	{regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`), Mask},
	// Secret-looking query parameters keep their name.
	{regexp.MustCompile(`(?i)([?&](?:api[_-]?key|apikey|key|token|access[_-]?token|secret|password|sig|signature)=)[^&\s"'<>]+`), "${1}" + Mask},
	// key: value / key=value pairs, quoted or not, keep the key.
	{regexp.MustCompile(`(?i)("?\b(?:api[_-]?key|apikey|access[_-]?token|refresh[_-]?token|auth[_-]?token|secret[_-]?key|client[_-]?secret|secret|password|passwd|pwd)"?\s*[:=]\s*)("?)[^\s"',;&]{4,}`), "${1}${2}" + Mask},
}

var (
	mu      sync.RWMutex
	secrets []string // longest first, so overlapping values mask fully
)

// RegisterSecret adds an exact value to mask, such as a configured API key.
// Values shorter than 8 characters are ignored.
func RegisterSecret(value string) {
	value = strings.TrimSpace(value)
	if len(value) < minSecretLen {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	for _, s := range secrets {
		if s == value {
			return
		}
	}
	secrets = append(secrets, value)
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
}

// String masks credentials in s.
func String(s string) string {
	if s == "" {
		return s
	}
	mu.RLock()
	for _, secret := range secrets {
		s = strings.ReplaceAll(s, secret, Mask)
	}
	mu.RUnlock()
	for _, r := range rules {
		s = r.re.ReplaceAllString(s, r.repl)
	}
	return s
}

// Truncate cuts s to at most max bytes without splitting a UTF-8 character.
func Truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// Preview redacts s and bounds it to max bytes. Redacting first ensures a
// secret cut by the limit is still masked.
func Preview(s string, max int) string {
	return Truncate(String(s), max)
}
