package app

import (
	"bufio"
	"os"
	"strings"
)

// LoadProperties parses a Java-style .properties file.
// Lines starting with '#' or '!' are comments. Keys/values are trimmed.
func LoadProperties(filepath string) map[string]string {
	props := make(map[string]string)

	file, err := os.Open(filepath)
	if err != nil {
		return props
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line[0] == '#' || line[0] == '!' {
			continue
		}
		if idx := strings.Index(line, "="); idx != -1 {
			key := strings.TrimSpace(line[:idx])
			value := strings.TrimSpace(line[idx+1:])
			props[key] = value
		}
	}
	return props
}

// ResolveConfig resolves a setting: environment variable first, then the
// properties file, then the hard fallback.
func ResolveConfig(key, fallback string, defaults map[string]string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	if v, ok := defaults[key]; ok {
		return v
	}
	return fallback
}
