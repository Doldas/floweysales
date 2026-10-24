package storage

import "testing"

func TestCleanUserspacePath(t *testing.T) {
	good := map[string]string{"/research/report.md": "/research/report.md", "/data//records.csv": "/data/records.csv"}
	for in, want := range good {
		got, err := CleanUserspacePath(in)
		if err != nil || got != want {
			t.Errorf("%q => %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "research/report.md", "/", "/../secrets", "/a/./b", "/a/../b", "/x\\y", "/x\x00y"} {
		if _, err := CleanUserspacePath(bad); err == nil {
			t.Errorf("unsafe path accepted: %q", bad)
		}
	}
}

func TestUserspaceFileSizeLimit(t *testing.T) {
	if UserspaceMaxFileBytes != 2*1024*1024 {
		t.Fatalf("unexpected userspace file limit: %d", UserspaceMaxFileBytes)
	}
	if len(make([]byte, UserspaceMaxFileBytes)) > UserspaceMaxFileBytes {
		t.Fatal("maximum-size file should be accepted by size guard")
	}
	if len(make([]byte, UserspaceMaxFileBytes+1)) <= UserspaceMaxFileBytes {
		t.Fatal("oversized file should be rejected by size guard")
	}
}

func TestUserspaceFolderPrefix(t *testing.T) {
	for _, bad := range []string{"/", "", "/../secret", "/x/./y", "relative"} {
		if _, err := userspaceFolderPrefix(bad); err == nil {
			t.Errorf("unsafe folder path accepted: %q", bad)
		}
	}
	prefix, err := userspaceFolderPrefix("/reports")
	if err != nil || prefix != "/reports/" {
		t.Fatalf("unexpected prefix: %q, %v", prefix, err)
	}
	// Folder deletion must not target a sibling with a shared name.
	for path, want := range map[string]bool{
		"/reports/.folder":            true,
		"/reports/2026/report.md":     true,
		"/reports-old/2026/report.md": false,
		"/reports":                    false,
		"/other/reports/file":         false,
	} {
		if got := len(path) >= len(prefix) && path[:len(prefix)] == prefix; got != want {
			t.Errorf("path %q matched %q: got %v want %v", path, prefix, got, want)
		}
	}
}
