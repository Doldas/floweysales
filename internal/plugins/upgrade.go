package plugins

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Outcomes of the post-upgrade report, one per plugin.
const (
	// OutcomeVerified: unchanged since before the upgrade and a handshake worked.
	OutcomeVerified = "verified"
	// OutcomeNeedsAttention: kept, but compatibility could not be verified or
	// a required setting is missing.
	OutcomeNeedsAttention = "needs_attention"
	// OutcomeIncompatible: kept with its settings and data, not run.
	OutcomeIncompatible = "incompatible"
	// OutcomeUpdated: a different version than before the upgrade (an
	// explicitly authorized update happened in between).
	OutcomeUpdated = "updated"
	// OutcomeRemoved: was installed before the upgrade and is not any more
	// (explicitly uninstalled in between).
	OutcomeRemoved = "removed"
)

// UpgradeEntry is one plugin's result after an application upgrade.
type UpgradeEntry struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Version         string `json:"version"`
	PreviousVersion string `json:"previous_version,omitempty"`
	Outcome         string `json:"outcome"`
	// Retained is true when the very same binary (version and checksum) is installed as before.
	Retained bool `json:"retained"`
	// Disabled is true when the plugin is kept but not run.
	Disabled bool   `json:"disabled"`
	Reason   string `json:"reason"`
}

// UpgradeReport tells the administrator what happened to the installed
// plugins when FloweySales itself was upgraded. It is built from evidence
// (a handshake with each kept binary), never from assumption.
type UpgradeReport struct {
	At          time.Time      `json:"at"`
	FromBuild   string         `json:"from_build,omitempty"`
	ToBuild     string         `json:"to_build,omitempty"`
	FromVersion string         `json:"from_version,omitempty"`
	ToVersion   string         `json:"to_version,omitempty"`
	Plugins     []UpgradeEntry `json:"plugins"`
}

// runtimeState is what the plugin directory remembers about the last run, so a
// new application build can tell what it found.
type runtimeState struct {
	Schema     int       `json:"schema"`
	AppBuild   string    `json:"app_build"`
	AppVersion string    `json:"app_version,omitempty"`
	RecordedAt time.Time `json:"recorded_at"`
	Plugins    []struct {
		ID      string `json:"id"`
		Version string `json:"version"`
		SHA256  string `json:"sha256"`
	} `json:"plugins"`
}

func (m *Manager) runtimePath() string { return filepath.Join(m.cfg.Dir, "runtime.json") }
func (m *Manager) reportPath() string  { return filepath.Join(m.cfg.Dir, "upgrade-report.json") }

func writeJSONAtomic(path string, v any) error {
	data, _ := json.MarshalIndent(v, "", "  ")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// loadUpgradeState reads the remembered run and the last report; unreadable
// files are ignored (they only feed a report, never the installation).
func (m *Manager) loadUpgradeState() {
	if raw, err := os.ReadFile(m.runtimePath()); err == nil {
		var st runtimeState
		if json.Unmarshal(raw, &st) == nil {
			m.lastRun = &st
		}
	}
	if raw, err := os.ReadFile(m.reportPath()); err == nil {
		var r UpgradeReport
		if json.Unmarshal(raw, &r) == nil {
			m.report = &r
		}
	}
}

// LastUpgradeReport returns the report for the most recent application upgrade, if any.
func (m *Manager) LastUpgradeReport() *UpgradeReport {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.report == nil {
		return nil
	}
	r := *m.report
	return &r
}

// CheckAfterStart verifies every installed plugin with a handshake and, when
// this is a different application build than the one that last ran against
// this plugin directory, records a report of what was retained, verified,
// needs attention, is incompatible or was changed. It then remembers the
// current run. It changes no installation.
func (m *Manager) CheckAfterStart(ctx context.Context) {
	m.VerifyAll(ctx)
	if ctx.Err() != nil {
		return
	}
	installed := m.InstalledAll()
	m.mu.Lock()
	last := m.lastRun
	m.mu.Unlock()

	if last != nil && m.cfg.AppBuild != "" && last.AppBuild != m.cfg.AppBuild {
		report := m.buildUpgradeReport(last, installed)
		if writeJSONAtomic(m.reportPath(), report) == nil {
			m.mu.Lock()
			m.report = &report
			m.mu.Unlock()
		}
	}
	if m.cfg.AppBuild == "" {
		return
	}
	cur := runtimeState{Schema: 1, AppBuild: m.cfg.AppBuild, AppVersion: m.cfg.AppVersion, RecordedAt: time.Now().UTC()}
	for _, p := range installed {
		cur.Plugins = append(cur.Plugins, struct {
			ID      string `json:"id"`
			Version string `json:"version"`
			SHA256  string `json:"sha256"`
		}{p.Manifest.ID, p.Manifest.Version, p.SHA256})
	}
	if writeJSONAtomic(m.runtimePath(), cur) == nil {
		m.mu.Lock()
		m.lastRun = &cur
		m.mu.Unlock()
	}
}

func (m *Manager) buildUpgradeReport(last *runtimeState, installed []InstalledPlugin) UpgradeReport {
	r := UpgradeReport{At: time.Now().UTC(), FromBuild: last.AppBuild, ToBuild: m.cfg.AppBuild,
		FromVersion: last.AppVersion, ToVersion: m.cfg.AppVersion, Plugins: []UpgradeEntry{}}
	before := map[string]struct{ version, sha string }{}
	for _, p := range last.Plugins {
		before[p.ID] = struct{ version, sha string }{p.Version, p.SHA256}
	}
	snap := m.cachedStore()
	for _, p := range installed {
		e := UpgradeEntry{ID: p.Manifest.ID, Name: p.Manifest.Name, Version: p.Manifest.Version}
		prev, had := before[p.Manifest.ID]
		delete(before, p.Manifest.ID)
		e.Retained = had && prev.sha == p.SHA256 && prev.version == p.Manifest.Version
		if had && !e.Retained {
			e.PreviousVersion = prev.version
		}
		c := m.assess(p, snap)
		_, missing := m.env(&p.Manifest)
		switch {
		case c.Status == CompatIncompatible || c.Status == CompatNeedsUpdate:
			e.Outcome, e.Disabled = OutcomeIncompatible, true
			e.Reason = c.Reason + " Its files, settings and data are kept."
		case had && !e.Retained:
			e.Outcome = OutcomeUpdated
			e.Reason = "Updated from " + prev.version + " to " + p.Manifest.Version + " by an explicit update; its settings were kept."
		case c.Status == CompatUnknown:
			e.Outcome = OutcomeNeedsAttention
			e.Reason = c.Reason
		case len(missing) > 0:
			e.Outcome = OutcomeNeedsAttention
			e.Reason = "Compatible, but required settings are not configured: " + joinNames(missing) + "."
		case !had:
			e.Outcome = OutcomeVerified
			e.Reason = "Not recorded before the upgrade; the handshake works with this FloweySales."
		default:
			e.Outcome = OutcomeVerified
			e.Reason = "Kept unchanged (same version and checksum) and the handshake works with this FloweySales."
		}
		r.Plugins = append(r.Plugins, e)
	}
	for id, b := range before {
		r.Plugins = append(r.Plugins, UpgradeEntry{ID: id, Name: id, Version: b.version, Outcome: OutcomeRemoved,
			Reason: "It was installed before the upgrade and has been uninstalled since."})
	}
	sort.Slice(r.Plugins, func(i, j int) bool { return r.Plugins[i].ID < r.Plugins[j].ID })
	return r
}

func joinNames(names []string) string {
	out := ""
	for i, n := range names {
		if i > 0 {
			out += ", "
		}
		out += n
	}
	return out
}
