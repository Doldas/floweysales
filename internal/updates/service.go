package updates

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Comparison outcomes between the running and the available build.
const (
	UpToDate        = "up_to_date"
	UpdateAvailable = "update_available"
	NewerThanLatest = "newer_than_latest"
	UnknownCurrent  = "unknown_current"
	UnknownLatest   = "unknown_latest"
	CheckFailed     = "check_failed"
)

// minCheckGap protects the anonymous registry and GitHub rate limits from
// repeated clicks.
const minCheckGap = 30 * time.Second

// Config selects which image and repository updates come from.
type Config struct {
	Image      string // e.g. ghcr.io/doldas/floweysales
	Tag        string // e.g. stable or v1.2.3
	Repository string // e.g. Doldas/floweysales
	ControlDir string // shared with the host updater; empty disables applying
}

// Build identifies the running binary.
type Build struct {
	Commit    string `json:"commit,omitempty"`
	BuildTime string `json:"build_time,omitempty"`
	Image     string `json:"image"`
	Tag       string `json:"tag"`
}

// CheckResult is the outcome of the most recent check.
type CheckResult struct {
	Comparison   string     `json:"comparison"`
	Latest       *ImageInfo `json:"latest,omitempty"`
	Changes      *Changes   `json:"changes,omitempty"`
	ChangesError string     `json:"changes_error,omitempty"`
	Error        string     `json:"error,omitempty"`
	CheckedAt    string     `json:"checked_at"`
}

// CanApply reports whether installing the available image is meaningful.
// Unknown versions may be installed after explicit confirmation, since old
// images carry no revision label.
func (r CheckResult) CanApply() bool {
	switch r.Comparison {
	case UpdateAvailable, UnknownCurrent, UnknownLatest:
		return true
	}
	return false
}

// Service checks for updates and queues them with the host updater.
type Service struct {
	cfg      Config
	build    Build
	registry Registry
	github   GitHub
	control  Control

	mu        sync.Mutex
	last      *CheckResult
	lastStart time.Time
	now       func() time.Time
}

// NewService validates cfg. commit and buildTime describe the running binary.
func NewService(cfg Config, commit, buildTime string, registry Registry, github GitHub) (*Service, error) {
	if _, _, err := ParseImage(cfg.Image); err != nil {
		return nil, err
	}
	if !ValidTag(cfg.Tag) {
		return nil, fmt.Errorf("invalid update tag %q", cfg.Tag)
	}
	if !ValidRepository(cfg.Repository) {
		return nil, fmt.Errorf("invalid update repository %q", cfg.Repository)
	}
	if !commitPattern.MatchString(commit) {
		commit = ""
	}
	return &Service{
		cfg:      cfg,
		build:    Build{Commit: commit, BuildTime: buildTime, Image: cfg.Image, Tag: cfg.Tag},
		registry: registry,
		github:   github,
		control:  Control{Dir: cfg.ControlDir},
		now:      time.Now,
	}, nil
}

// Build returns the running binary's identity.
func (s *Service) Build() Build { return s.build }

// Control returns the host updater handoff.
func (s *Service) Control() Control { return s.control }

// Last returns the cached check result, if any.
func (s *Service) Last() *CheckResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil {
		return nil
	}
	copied := *s.last
	return &copied
}

// sameCommit compares full or abbreviated hashes.
func sameCommit(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	return strings.HasPrefix(b, a)
}

// Check queries the registry and GitHub. Calls within minCheckGap of the
// previous check return the cached result.
func (s *Service) Check(ctx context.Context) CheckResult {
	s.mu.Lock()
	if s.last != nil && s.now().Sub(s.lastStart) < minCheckGap {
		cached := *s.last
		s.mu.Unlock()
		return cached
	}
	s.lastStart = s.now()
	s.mu.Unlock()

	result := s.evaluate(ctx)
	s.mu.Lock()
	s.last = &result
	s.mu.Unlock()
	return result
}

func (s *Service) evaluate(ctx context.Context) CheckResult {
	result := CheckResult{CheckedAt: s.now().UTC().Format(time.RFC3339)}
	latest, err := s.registry.Lookup(ctx, s.cfg.Image, s.cfg.Tag)
	if err != nil {
		result.Comparison = CheckFailed
		result.Error = err.Error()
		return result
	}
	result.Latest = &latest
	switch {
	case latest.Commit == "":
		result.Comparison = UnknownLatest
		return result
	case s.build.Commit == "":
		result.Comparison = UnknownCurrent
		return result
	case sameCommit(s.build.Commit, latest.Commit):
		result.Comparison = UpToDate
		return result
	}
	changes, err := s.github.Compare(ctx, s.cfg.Repository, s.build.Commit, latest.Commit)
	if err != nil {
		// The registry already proved the builds differ; the change list is optional.
		result.Comparison = UpdateAvailable
		result.ChangesError = err.Error()
		return result
	}
	result.Changes = &changes
	switch changes.Status {
	case "behind":
		result.Comparison = NewerThanLatest
	case "identical":
		result.Comparison = UpToDate
	default:
		result.Comparison = UpdateAvailable
	}
	return result
}

// ErrNothingToApply means the last check found no installable update.
var ErrNothingToApply = errors.New("no update is available; check for updates first")

// ErrStale means a newer image appeared after the administrator reviewed the update.
var ErrStale = errors.New("the available update changed since it was reviewed; check again")

// Apply queues the reviewed update. expectedCommit must match the image the
// administrator saw, when that image declares a commit.
func (s *Service) Apply(requestedBy int64, expectedCommit string) (Request, error) {
	last := s.Last()
	if last == nil || !last.CanApply() {
		return Request{}, ErrNothingToApply
	}
	if last.Latest != nil && last.Latest.Commit != "" && last.Latest.Commit != expectedCommit {
		return Request{}, ErrStale
	}
	return s.control.RequestUpdate(requestedBy, expectedCommit, s.now())
}

// Run checks periodically until ctx ends. interval <= 0 disables it.
func (s *Service) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	timer := time.NewTimer(time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			s.Check(checkCtx)
			cancel()
			timer.Reset(interval)
		}
	}
}
