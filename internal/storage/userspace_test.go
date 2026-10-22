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
