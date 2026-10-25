package updates

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// The control directory is shared with the host updater (scripts/auto-update.sh):
//
//	agent.json          written by the host when the updater is installed or runs
//	status.json         written by the host while and after it updates
//	inbox/request.json  written by the application to ask for an update
//
// The request only triggers the host's fixed update procedure; nothing in it
// is executed, so a compromised application cannot run commands on the host.
const (
	agentFile   = "agent.json"
	statusFile  = "status.json"
	inboxDir    = "inbox"
	requestFile = "request.json"
	maxCtlFile  = 64 << 10
)

// Update states reported by the host updater.
const (
	StateIdle      = "idle"
	StateQueued    = "queued"
	StateBackingUp = "backing_up"
	StatePulling   = "pulling"
	StateMigrating = "migrating"
	StateRestart   = "restarting"
	StateVerifying = "verifying"
	StateSucceeded = "succeeded"
	StateUpToDate  = "up_to_date"
	StateFailed    = "failed"
	StateRolled    = "rolled_back"
)

// Running reports whether an update is between request and completion.
func Running(state string) bool {
	switch state {
	case StateQueued, StateBackingUp, StatePulling, StateMigrating, StateRestart, StateVerifying:
		return true
	}
	return false
}

var (
	// ErrNoAgent means the host updater is not installed for this deployment.
	ErrNoAgent = errors.New("the host updater is not installed")
	// ErrInProgress means an update was already requested or is running.
	ErrInProgress = errors.New("an update is already queued or running")
)

// Agent describes the installed host updater.
type Agent struct {
	Engine      string `json:"engine"`
	InstalledAt string `json:"installed_at,omitempty"`
	LastRunAt   string `json:"last_run_at,omitempty"`
	Timer       bool   `json:"timer"`
	Backups     bool   `json:"backups"`
}

// Status is the host updater's progress report.
type Status struct {
	State      string   `json:"state"`
	Message    string   `json:"message,omitempty"`
	RequestID  string   `json:"request_id,omitempty"`
	Trigger    string   `json:"trigger,omitempty"`
	StartedAt  string   `json:"started_at,omitempty"`
	FinishedAt string   `json:"finished_at,omitempty"`
	FromCommit string   `json:"from_commit,omitempty"`
	ToCommit   string   `json:"to_commit,omitempty"`
	Backup     string   `json:"backup,omitempty"`
	LogTail    []string `json:"log_tail,omitempty"`
}

// Request is what the application writes to ask for an update.
type Request struct {
	ID             string `json:"id"`
	RequestedBy    int64  `json:"requested_by"`
	RequestedAt    string `json:"requested_at"`
	ExpectedCommit string `json:"expected_commit,omitempty"`
}

// Control reads and writes the shared control directory. An empty Dir
// disables applying updates; checks still work.
type Control struct {
	Dir string
}

func readJSONFile(path string, out any) (bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxCtlFile+1))
	if err != nil {
		return false, err
	}
	if len(raw) > maxCtlFile {
		return false, fmt.Errorf("%s is too large", filepath.Base(path))
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return false, fmt.Errorf("%s is not valid JSON: %w", filepath.Base(path), err)
	}
	return true, nil
}

// Agent returns the installed updater, or ErrNoAgent.
func (c Control) Agent() (Agent, error) {
	var a Agent
	if c.Dir == "" {
		return a, ErrNoAgent
	}
	found, err := readJSONFile(filepath.Join(c.Dir, agentFile), &a)
	if err != nil {
		return a, err
	}
	if !found {
		return a, ErrNoAgent
	}
	return a, nil
}

// Status returns the latest progress report. A pending request that the
// host has not picked up yet is reported as queued.
func (c Control) Status() (Status, error) {
	s := Status{State: StateIdle}
	if c.Dir == "" {
		return s, nil
	}
	if _, err := readJSONFile(filepath.Join(c.Dir, statusFile), &s); err != nil {
		return s, err
	}
	if len(s.LogTail) > 40 {
		s.LogTail = s.LogTail[len(s.LogTail)-40:]
	}
	var pending Request
	found, err := readJSONFile(filepath.Join(c.Dir, inboxDir, requestFile), &pending)
	if err != nil {
		return s, err
	}
	if found && !Running(s.State) {
		return Status{State: StateQueued, RequestID: pending.ID, Message: "Waiting for the host updater to start", StartedAt: pending.RequestedAt}, nil
	}
	return s, nil
}

// RequestUpdate atomically places a request in the inbox. It refuses while
// another request is pending or an update is running.
func (c Control) RequestUpdate(requestedBy int64, expectedCommit string, now time.Time) (Request, error) {
	if _, err := c.Agent(); err != nil {
		return Request{}, err
	}
	status, err := c.Status()
	if err != nil {
		return Request{}, err
	}
	if Running(status.State) {
		return Request{}, ErrInProgress
	}
	id := make([]byte, 8)
	if _, err := rand.Read(id); err != nil {
		return Request{}, err
	}
	req := Request{ID: hex.EncodeToString(id), RequestedBy: requestedBy, RequestedAt: now.UTC().Format(time.RFC3339), ExpectedCommit: expectedCommit}
	raw, err := json.Marshal(req)
	if err != nil {
		return Request{}, err
	}
	dir := filepath.Join(c.Dir, inboxDir)
	tmp, err := os.CreateTemp(dir, ".request-*.tmp")
	if err != nil {
		return Request{}, fmt.Errorf("update inbox is not writable: %w", err)
	}
	defer os.Remove(tmp.Name())
	// The container user differs from the host user; let the host read it.
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return Request{}, err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return Request{}, err
	}
	if err := tmp.Close(); err != nil {
		return Request{}, err
	}
	// Linking fails if a request already exists, so two admins cannot queue twice.
	if err := os.Link(tmp.Name(), filepath.Join(dir, requestFile)); err != nil {
		if errors.Is(err, os.ErrExist) {
			return Request{}, ErrInProgress
		}
		return Request{}, err
	}
	return req, nil
}
