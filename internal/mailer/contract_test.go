package mailer

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoMailOutsideRenderer fails when another package sends mail itself. All
// producers must hand a Message to this package so every email gets the shared
// branded shell, a plain-text alternative and sanitized content.
func TestNoMailOutsideRenderer(t *testing.T) {
	root := filepath.Join("..", "..")
	forbidden := []string{`"net/smtp"`, `"Subject: `, `\r\nSubject:`, `Subject: "+`, `Subject: %s`, `smtp.SendMail`, `"text/html; charset`}
	scanned := 0
	for _, dir := range []string{"internal", "cmd", "pluginsdk"} {
		base := filepath.Join(root, dir)
		if _, err := os.Stat(base); err != nil {
			continue
		}
		err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && (d.Name() == "node_modules" || path == filepath.Join(root, "internal", "mailer")) {
				return filepath.SkipDir
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			scanned++
			for _, f := range forbidden {
				if strings.Contains(string(data), f) {
					t.Errorf("%s contains %s: send mail through internal/mailer (mailer.Send with a mailer.Message)", path, f)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if scanned < 20 {
		t.Fatalf("contract test scanned only %d files; is the path wrong?", scanned)
	}
}
