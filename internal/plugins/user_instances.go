package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"floweysales/internal/redact"
)

// A user's own provider instance (#421): an installed plugin run in a process
// of its own with the secret variables that user supplied. The process never
// sees the system's value of a secret variable, and no other user's call
// reaches it. Variables that are not secret come from the system as usual.

const (
	userKeySep          = "#u"
	defaultUserProcs    = 8
	userInstanceIdleTTL = 10 * time.Minute
)

type userInstanceKey struct{}

type userInstance struct {
	userID int64
	values map[string]string
}

type userUse struct {
	fingerprint string
	lastUsed    time.Time
}

// WithUserInstance makes plugin calls made with ctx run in the user's own
// process with values, the user's settings for the plugin's secret variables.
func WithUserInstance(ctx context.Context, userID int64, values map[string]string) context.Context {
	return context.WithValue(ctx, userInstanceKey{}, userInstance{userID: userID, values: values})
}

// UserInstanceOf reports whose own instance calls made with ctx use.
func UserInstanceOf(ctx context.Context) (userID int64, values map[string]string, ok bool) {
	inst, ok := userInstanceFrom(ctx)
	return inst.userID, inst.values, ok
}

func userInstanceFrom(ctx context.Context) (userInstance, bool) {
	inst, ok := ctx.Value(userInstanceKey{}).(userInstance)
	return inst, ok && inst.userID > 0
}

// UserSecrets lists the variables a user may supply for an installed plugin:
// the secret ones it declares, never an endpoint or other setting.
func (m *Manager) UserSecrets(pluginID string) []EnvVar {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.installed[pluginID]
	if !ok {
		return nil
	}
	var out []EnvVar
	for _, e := range p.Manifest.Env {
		if e.Secret {
			out = append(out, e)
		}
	}
	return out
}

// userEnv is the environment of a user's process: the user's values for the
// declared secret variables and the system's for the rest.
func (m *Manager) userEnv(manifest *Manifest, values map[string]string) (env []string, fingerprint string) {
	base, _ := m.env(nil)
	env = base
	h := sha256.New()
	names := make([]string, 0, len(manifest.Env))
	for _, e := range manifest.Env {
		names = append(names, e.Name)
	}
	sort.Strings(names)
	byName := map[string]EnvVar{}
	for _, e := range manifest.Env {
		byName[e.Name] = e
	}
	for _, name := range names {
		e := byName[name]
		var value string
		if e.Secret {
			value = values[name]
			if value != "" {
				redact.RegisterSecret(value)
			}
		} else {
			value, _ = m.lookup(name)
		}
		if value == "" {
			continue
		}
		fmt.Fprintf(h, "%s=%s\n", name, value)
		env = append(env, name+"="+value)
	}
	return env, hex.EncodeToString(h.Sum(nil))
}

// userProcess returns the user's process for an installed plugin, starting it
// (after re-verifying the binary) when none runs or the user's values changed.
func (m *Manager) userProcess(p InstalledPlugin, inst userInstance) (*process, error) {
	id := p.Manifest.ID
	if problem := m.declaredProblem(p); problem != "" {
		return nil, fmt.Errorf("plugin %s is incompatible with this FloweySales and is not run (it stays installed): %s", id, problem)
	}
	key := id + userKeySep + strconv.FormatInt(inst.userID, 10)
	env, fingerprint := m.userEnv(&p.Manifest, inst.values)
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if proc := m.procs[key]; proc != nil && proc.alive() && m.userUse[key].fingerprint == fingerprint {
		m.userUse[key] = userUse{fingerprint: fingerprint, lastUsed: now}
		return proc, nil
	}
	if proc := m.procs[key]; proc != nil {
		delete(m.procs, key)
		go proc.stop(5 * time.Second)
	}
	m.trimUserProcsLocked(now)
	path := filepath.Join(m.cfg.Dir, filepath.FromSlash(p.Binary))
	sum, err := fileSHA256(path)
	if err != nil {
		return nil, fmt.Errorf("plugin %s binary unreadable: %w", id, err)
	}
	if sum != p.SHA256 {
		return nil, fmt.Errorf("plugin %s binary changed on disk since installation; reinstall it", id)
	}
	proc, err := startProcess(path, filepath.Dir(path), env)
	if err != nil {
		return nil, fmt.Errorf("plugin %s: %w", id, err)
	}
	m.procs[key] = proc
	m.userUse[key] = userUse{fingerprint: fingerprint, lastUsed: now}
	return proc, nil
}

// trimUserProcsLocked stops idle user processes and, at the bound, the least
// recently used one, so that one more can start. Callers hold m.mu.
func (m *Manager) trimUserProcsLocked(now time.Time) {
	limit := m.cfg.MaxUserProcesses
	if limit <= 0 {
		limit = defaultUserProcs
	}
	stop := func(key string) {
		if proc := m.procs[key]; proc != nil {
			go proc.stop(5 * time.Second)
		}
		delete(m.procs, key)
		delete(m.userUse, key)
	}
	for key, use := range m.userUse {
		if now.Sub(use.lastUsed) > userInstanceIdleTTL {
			stop(key)
		}
	}
	for len(m.userUse) >= limit {
		oldest, at := "", now
		for key, use := range m.userUse {
			if oldest == "" || use.lastUsed.Before(at) {
				oldest, at = key, use.lastUsed
			}
		}
		stop(oldest)
	}
}
