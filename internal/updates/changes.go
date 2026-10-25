package updates

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	commitPattern = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
	repoPattern   = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
)

// ValidRepository reports whether repo looks like "owner/name".
func ValidRepository(repo string) bool { return repoPattern.MatchString(repo) }

// maxListedCommits bounds the change list shown to administrators.
const maxListedCommits = 50

// Commit is one change between the running and the available build.
type Commit struct {
	SHA   string `json:"sha"`
	Title string `json:"title"`
	URL   string `json:"url,omitempty"`
}

// Changes summarises what an update brings.
type Changes struct {
	// Status is GitHub's comparison of the available build to the running
	// one: "ahead" (newer), "behind" (older), "identical" or "diverged".
	Status       string   `json:"status"`
	Commits      []Commit `json:"commits"`
	TotalCommits int      `json:"total_commits"`
	// Migrations lists Liquibase changelog files the update adds; the
	// host updater applies them before restarting the application.
	Migrations []string `json:"migrations"`
	CompareURL string   `json:"compare_url,omitempty"`
}

// GitHub reads public commit history. BaseURL overrides https://api.github.com for tests.
type GitHub struct {
	Client  *http.Client
	BaseURL string
}

func (g GitHub) client() *http.Client {
	if g.Client != nil {
		return g.Client
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// Compare lists commits and added migration files between two commits.
func (g GitHub) Compare(ctx context.Context, repo, base, head string) (Changes, error) {
	if !ValidRepository(repo) || !commitPattern.MatchString(base) || !commitPattern.MatchString(head) {
		return Changes{}, fmt.Errorf("invalid repository or commit")
	}
	apiBase := "https://api.github.com"
	if g.BaseURL != "" {
		apiBase = strings.TrimRight(g.BaseURL, "/")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/repos/"+repo+"/compare/"+base+"..."+head, nil)
	if err != nil {
		return Changes{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := g.client().Do(req)
	if err != nil {
		return Changes{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Changes{}, fmt.Errorf("GitHub compare returned HTTP %d", res.StatusCode)
	}
	var body struct {
		Status       string `json:"status"`
		HTMLURL      string `json:"html_url"`
		TotalCommits int    `json:"total_commits"`
		Commits      []struct {
			SHA     string `json:"sha"`
			HTMLURL string `json:"html_url"`
			Commit  struct {
				Message string `json:"message"`
			} `json:"commit"`
		} `json:"commits"`
		Files []struct {
			Filename string `json:"filename"`
			Status   string `json:"status"`
		} `json:"files"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&body); err != nil {
		return Changes{}, fmt.Errorf("invalid GitHub response: %w", err)
	}
	out := Changes{Status: body.Status, TotalCommits: body.TotalCommits, CompareURL: body.HTMLURL, Commits: []Commit{}, Migrations: []string{}}
	// GitHub lists commits oldest first; show the newest first.
	for i := len(body.Commits) - 1; i >= 0 && len(out.Commits) < maxListedCommits; i-- {
		c := body.Commits[i]
		title, _, _ := strings.Cut(c.Commit.Message, "\n")
		out.Commits = append(out.Commits, Commit{SHA: c.SHA, Title: strings.TrimSpace(title), URL: c.HTMLURL})
	}
	for _, f := range body.Files {
		if f.Status == "added" && IsMigrationFile(f.Filename) {
			out.Migrations = append(out.Migrations, path.Base(f.Filename))
		}
	}
	sort.Strings(out.Migrations)
	return out, nil
}

// IsMigrationFile reports whether name is a Liquibase changeset file
// included by the master changelog.
func IsMigrationFile(name string) bool {
	return strings.HasPrefix(name, "db/changelog/") && strings.HasSuffix(name, ".xml") &&
		path.Base(name) != "db.changelog-master.xml"
}
