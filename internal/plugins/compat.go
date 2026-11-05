package plugins

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"time"
)

// Compatibility states of an installed plugin against the running FloweySales.
const (
	// CompatCompatible: the declared protocol matches and the plugin answered
	// a handshake that agrees with what is installed.
	CompatCompatible = "compatible"
	// CompatIncompatible: the plugin cannot run here and no compatible
	// version is known in the store.
	CompatIncompatible = "incompatible"
	// CompatNeedsUpdate: incompatible, but the store lists a version that runs here.
	CompatNeedsUpdate = "needs_update"
	// CompatUnknown: nothing contradicts compatibility, but it has not been verified.
	CompatUnknown = "unknown"
)

// Compatibility explains whether an installed plugin works with the running
// FloweySales and what the claim rests on.
type Compatibility struct {
	Status         string `json:"status"`
	Reason         string `json:"reason"`
	PluginProtocol int    `json:"plugin_protocol"`
	AppProtocol    int    `json:"app_protocol"`
	// Evidence says how the status was established.
	Evidence  string     `json:"evidence"`
	CheckedAt *time.Time `json:"checked_at,omitempty"`
	// Affects lists what stops working while the plugin is not run.
	Affects []string `json:"affects,omitempty"`
	// CompatibleVersions are store versions that can be installed here, newest first.
	CompatibleVersions []string `json:"compatible_versions,omitempty"`
}

// compatCheck is the result of the last handshake with an installed plugin.
type compatCheck struct {
	at     time.Time
	reason string // empty when the handshake succeeded
}

// declaredProblem reports why an installed plugin must not run, judged from
// its recorded manifest alone; empty means nothing declared contradicts this build.
func declaredProblem(p InstalledPlugin) string {
	if p.Manifest.Protocol != ProtocolVersion {
		return fmt.Sprintf("The plugin speaks protocol %d; this FloweySales speaks protocol %d.", p.Manifest.Protocol, ProtocolVersion)
	}
	if err := p.Manifest.Validate(); err != nil {
		return "Its manifest is no longer accepted: " + err.Error() + "."
	}
	return ""
}

// affects names what is lost while the plugin is not run.
func affects(p InstalledPlugin) []string {
	var out []string
	if p.Manifest.Provider.ID != "" {
		out = append(out, "The "+p.Manifest.Provider.ID+" provider is unavailable.")
	}
	if p.Manifest.Tools != nil {
		out = append(out, "Its tools tab is unavailable.")
	}
	return out
}

// assess builds the compatibility statement for one installed plugin. The
// cached store index, when there is one, supplies compatible versions.
func (m *Manager) assess(p InstalledPlugin, snap *storeSnapshot) Compatibility {
	c := Compatibility{PluginProtocol: p.Manifest.Protocol, AppProtocol: ProtocolVersion}
	var compatible []string
	if snap != nil {
		if entry, ok := snap.index.find(p.Manifest.ID); ok {
			for _, v := range entry.Versions {
				if v.installable() {
					compatible = append(compatible, v.Manifest.Version)
				}
			}
			sort.Slice(compatible, func(i, j int) bool { return CompareVersions(compatible[i], compatible[j]) > 0 })
		}
	}
	m.mu.Lock()
	check, checked := m.checks[p.Manifest.ID]
	m.mu.Unlock()
	if checked {
		at := check.at
		c.CheckedAt = &at
	}

	if problem := declaredProblem(p); problem != "" {
		c.Status, c.Reason, c.Evidence = CompatIncompatible, problem, "Declared manifest compared with this build."
		c.Affects = affects(p)
		if len(compatible) > 0 {
			c.Status, c.CompatibleVersions = CompatNeedsUpdate, compatible
			c.Reason += " The store has a version that runs here. The installed copy is kept, not run, until you update it."
		} else {
			c.Reason += " It stays installed with its settings but is not run."
		}
		return c
	}
	switch {
	case !checked:
		c.Status, c.Evidence = CompatUnknown, "Declared protocol matches; no handshake has been run since FloweySales started."
		c.Reason = "Compatibility is not verified yet."
	case check.reason != "":
		c.Status, c.Evidence = CompatIncompatible, "Handshake with the installed binary failed."
		c.Reason = check.reason + " It stays installed with its settings but is not run."
		c.Affects = affects(p)
		if len(compatible) > 0 {
			c.Status, c.CompatibleVersions = CompatNeedsUpdate, compatible
		}
	default:
		c.Status, c.Evidence = CompatCompatible, "Declared protocol matches and the plugin answered a handshake."
		c.Reason = "Works with this FloweySales."
	}
	return c
}

// Verify handshakes with the installed binary, without any of the plugin's
// secrets, and records whether it still describes itself as what is installed.
// A failure is recorded, never acted on: the plugin stays installed.
func (m *Manager) Verify(ctx context.Context, id string) (Compatibility, error) {
	m.mu.Lock()
	p, ok := m.installed[id]
	m.mu.Unlock()
	if !ok {
		return Compatibility{}, ErrNotInstalled
	}
	result := compatCheck{at: time.Now().UTC()}
	if problem := declaredProblem(p); problem != "" {
		result.reason = problem
	} else if sum, err := fileSHA256(filepath.Join(m.cfg.Dir, filepath.FromSlash(p.Binary))); err != nil {
		result.reason = "The binary cannot be read: " + err.Error() + "."
	} else if sum != p.SHA256 {
		result.reason = "The binary changed on disk since installation."
	} else if described, err := m.handshake(ctx, filepath.Join(m.cfg.Dir, filepath.FromSlash(p.Binary))); err != nil {
		result.reason = err.Error() + "."
	} else if described.ID != p.Manifest.ID || described.Version != p.Manifest.Version {
		result.reason = fmt.Sprintf("It describes itself as %s %s, not the installed %s %s.", described.ID, described.Version, p.Manifest.ID, p.Manifest.Version)
	}
	m.mu.Lock()
	m.checks[id] = result
	m.mu.Unlock()
	return m.assess(p, m.cachedStore()), nil
}

// VerifyAll verifies every installed plugin; failures are recorded per plugin.
func (m *Manager) VerifyAll(ctx context.Context) {
	for _, p := range m.InstalledAll() {
		if ctx.Err() != nil {
			return
		}
		_, _ = m.Verify(ctx, p.Manifest.ID)
	}
}
