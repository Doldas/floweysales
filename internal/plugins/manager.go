package plugins

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"

	"floweysales/internal/market"
	"floweysales/internal/redact"
)

// Config configures a Manager.
type Config struct {
	// Dir holds installed plugins and installed.json; it must be writable
	// and allow executing files.
	Dir string
	// StoreURL is the plugin index (https:// or file://).
	StoreURL string
	// HTTPClient downloads the index and artifacts; nil uses a 60s client.
	HTTPClient *http.Client
	// LookupEnv resolves a variable a plugin declares (environment, then
	// config.properties). Nil forwards nothing.
	LookupEnv func(name string) string
	// CallTimeout bounds one plugin call; zero means 45s.
	CallTimeout time.Duration
}

// InstalledPlugin is one entry of installed.json.
type InstalledPlugin struct {
	Manifest    Manifest  `json:"manifest"`
	Binary      string    `json:"binary"` // relative to Dir, slash-separated
	SHA256      string    `json:"sha256"`
	InstalledAt time.Time `json:"installed_at"`
	Source      string    `json:"source"` // store URL or "file"
}

type stateFile struct {
	Schema  int               `json:"schema"`
	Plugins []InstalledPlugin `json:"plugins"`
}

// startRetryDelay keeps a crashing plugin from being restarted on every call.
const startRetryDelay = 15 * time.Second

type startFailure struct {
	at  time.Time
	err error
}

// Manager installs plugins, runs them on demand and implements
// market.Backend on top of them.
type Manager struct {
	cfg   Config
	store storeLocation
	http  *http.Client

	installMu sync.Mutex // serializes install, update and uninstall

	mu        sync.Mutex
	installed map[string]InstalledPlugin // by plugin id
	procs     map[string]*process
	failures  map[string]startFailure

	storeMu  sync.Mutex
	snapshot *storeSnapshot
}

// Open loads installed.json from cfg.Dir, creating the directory if needed.
func Open(cfg Config) (*Manager, error) {
	if cfg.Dir == "" {
		return nil, errors.New("plugin directory is not configured")
	}
	if cfg.StoreURL == "" {
		cfg.StoreURL = DefaultStoreURL
	}
	store, err := parseStoreURL(cfg.StoreURL)
	if err != nil {
		return nil, err
	}
	if cfg.CallTimeout <= 0 {
		cfg.CallTimeout = 45 * time.Second
	}
	client := &http.Client{Timeout: 60 * time.Second}
	if cfg.HTTPClient != nil {
		copied := *cfg.HTTPClient
		client = &copied
	}
	// Release downloads redirect to a CDN; never follow one off HTTPS.
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return fmt.Errorf("refusing redirect to %s URL", req.URL.Scheme)
		}
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return nil
	}
	abs, err := filepath.Abs(cfg.Dir)
	if err != nil {
		return nil, err
	}
	cfg.Dir = abs
	if err := os.MkdirAll(cfg.Dir, 0o750); err != nil {
		return nil, fmt.Errorf("create plugin directory: %w", err)
	}
	m := &Manager{cfg: cfg, store: store, http: client, installed: map[string]InstalledPlugin{},
		procs: map[string]*process{}, failures: map[string]startFailure{}}
	raw, err := os.ReadFile(m.statePath())
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, fmt.Errorf("read installed plugins: %w", err)
	default:
		var st stateFile
		if err := json.Unmarshal(raw, &st); err != nil {
			return nil, fmt.Errorf("installed.json is corrupt: %w", err)
		}
		for _, p := range st.Plugins {
			if p.Manifest.Validate() == nil && validBinaryPath(p.Binary) {
				m.installed[p.Manifest.ID] = p
			}
		}
	}
	return m, nil
}

// Dir is the absolute plugin directory.
func (m *Manager) Dir() string { return m.cfg.Dir }

// StoreURL is the configured plugin index.
func (m *Manager) StoreURL() string { return m.cfg.StoreURL }

func (m *Manager) statePath() string { return filepath.Join(m.cfg.Dir, "installed.json") }

func validBinaryPath(rel string) bool {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
	return rel != "" && clean == rel && !filepath.IsAbs(filepath.FromSlash(rel)) && rel[0] != '.' && rel[0] != '/'
}

// saveLocked writes installed.json atomically. Callers hold m.mu.
func (m *Manager) saveLocked() error {
	st := stateFile{Schema: 1, Plugins: make([]InstalledPlugin, 0, len(m.installed))}
	for _, p := range m.installed {
		st.Plugins = append(st.Plugins, p)
	}
	sort.Slice(st.Plugins, func(i, j int) bool { return st.Plugins[i].Manifest.ID < st.Plugins[j].Manifest.ID })
	data, _ := json.MarshalIndent(st, "", "  ")
	tmp := m.statePath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, m.statePath())
}

// Installed lists installed plugins sorted by id.
func (m *Manager) Installed() []InstalledPlugin {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]InstalledPlugin, 0, len(m.installed))
	for _, p := range m.installed {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Manifest.ID < out[j].Manifest.ID })
	return out
}

// byProviderLocked finds the plugin providing a provider id.
func (m *Manager) byProviderLocked(provider string) (InstalledPlugin, bool) {
	for _, p := range m.installed {
		if p.Manifest.Provider.ID == provider {
			return p, true
		}
	}
	return InstalledPlugin{}, false
}

// baseEnv is what every plugin process gets: enough to resolve DNS, reach
// proxies and find CA certificates, and nothing of the backend's secrets.
var baseEnv = []string{
	"PATH", "SystemRoot", "SYSTEMROOT", "WINDIR", "TEMP", "TMP", "TMPDIR", "HOME", "USERPROFILE", "TZ", "ZONEINFO",
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy", "SSL_CERT_FILE", "SSL_CERT_DIR",
}

// env builds the plugin environment and reports declared variables that are
// required but unset.
func (m *Manager) env(manifest *Manifest) (env []string, missing []string) {
	for _, name := range baseEnv {
		if v, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+v)
		}
	}
	env = append(env, "FLOWEYSALES_PLUGIN=1", fmt.Sprintf("FLOWEYSALES_PLUGIN_PROTOCOL=%d", ProtocolVersion))
	if manifest == nil {
		return env, nil
	}
	for _, e := range manifest.Env {
		value := ""
		if m.cfg.LookupEnv != nil {
			value = m.cfg.LookupEnv(e.Name)
		}
		if value == "" {
			if e.Required {
				missing = append(missing, e.Name)
			}
			continue
		}
		if e.Secret {
			redact.RegisterSecret(value)
		}
		env = append(env, e.Name+"="+value)
	}
	return env, missing
}

// process returns the running process for an installed plugin, starting it
// (after re-verifying the binary's checksum) when needed.
func (m *Manager) process(p InstalledPlugin) (*process, error) {
	id := p.Manifest.ID
	m.mu.Lock()
	defer m.mu.Unlock()
	if proc := m.procs[id]; proc != nil && proc.alive() {
		return proc, nil
	}
	if f, ok := m.failures[id]; ok && time.Since(f.at) < startRetryDelay {
		return nil, f.err
	}
	path := filepath.Join(m.cfg.Dir, filepath.FromSlash(p.Binary))
	fail := func(err error) (*process, error) {
		m.failures[id] = startFailure{at: time.Now(), err: err}
		return nil, err
	}
	sum, err := fileSHA256(path)
	if err != nil {
		return fail(fmt.Errorf("plugin %s binary unreadable: %w", id, err))
	}
	if sum != p.SHA256 {
		return fail(fmt.Errorf("plugin %s binary changed on disk since installation; reinstall it", id))
	}
	env, _ := m.env(&p.Manifest)
	proc, err := startProcess(path, filepath.Dir(path), env)
	if err != nil {
		return fail(fmt.Errorf("plugin %s: %w", id, err))
	}
	delete(m.failures, id)
	m.procs[id] = proc
	return proc, nil
}

// stopLocked stops a plugin process. Callers hold m.mu.
func (m *Manager) stopLocked(id string) {
	if proc := m.procs[id]; proc != nil {
		delete(m.procs, id)
		go proc.stop(5 * time.Second)
	}
	delete(m.failures, id)
}

// Close stops every plugin process.
func (m *Manager) Close() {
	m.mu.Lock()
	procs := m.procs
	m.procs = map[string]*process{}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, proc := range procs {
		wg.Add(1)
		go func(p *process) { defer wg.Done(); p.stop(3 * time.Second) }(proc)
	}
	wg.Wait()
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

// RefreshStore fetches the index now.
func (m *Manager) RefreshStore(ctx context.Context) (Index, error) {
	data, err := m.store.fetch(ctx, m.http, m.cfg.StoreURL, maxIndexBytes)
	var snap storeSnapshot
	snap.checkedAt = time.Now().UTC()
	if err == nil {
		snap.index, snap.warnings, err = parseIndex(data)
	}
	snap.err = err
	m.storeMu.Lock()
	// A failed refresh keeps the last good catalogue but reports the error.
	if err != nil && m.snapshot != nil {
		snap.index, snap.warnings = m.snapshot.index, m.snapshot.warnings
	}
	m.snapshot = &snap
	m.storeMu.Unlock()
	if err != nil {
		return Index{}, fmt.Errorf("plugin store: %w", err)
	}
	return snap.index, nil
}

// Run refreshes the store index every interval until ctx ends; zero disables
// background checks.
func (m *Manager) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		_, _ = m.RefreshStore(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (m *Manager) cachedStore() *storeSnapshot {
	m.storeMu.Lock()
	defer m.storeMu.Unlock()
	return m.snapshot
}

// ---------------------------------------------------------------------------
// Install, update, uninstall
// ---------------------------------------------------------------------------

// TermsError is returned when a plugin's terms have not been accepted.
type TermsError struct {
	PluginID string
	Terms    string
}

func (e *TermsError) Error() string {
	return fmt.Sprintf("plugin %s requires accepting its terms before installation", e.PluginID)
}

// ErrNotInstalled is returned for operations on a plugin that is not installed.
var ErrNotInstalled = errors.New("plugin is not installed")

// ErrNotInStore is returned when the store does not list the plugin or version.
var ErrNotInStore = errors.New("plugin or version is not in the store")

// Install installs (or updates to) a store version. An empty version means
// the newest installable version.
func (m *Manager) Install(ctx context.Context, id, version string, acceptTerms bool) (InstalledPlugin, error) {
	m.installMu.Lock()
	defer m.installMu.Unlock()
	idx, err := m.RefreshStore(ctx)
	if err != nil {
		return InstalledPlugin{}, err
	}
	entry, ok := idx.find(id)
	if !ok {
		return InstalledPlugin{}, fmt.Errorf("%w: %s", ErrNotInStore, id)
	}
	m.mu.Lock()
	current, isInstalled := m.installed[id]
	m.mu.Unlock()
	var v IndexVersion
	if version == "" {
		v, ok = entry.Latest(isInstalled && IsPrerelease(current.Manifest.Version))
		if !ok {
			return InstalledPlugin{}, fmt.Errorf("%s has no version for %s using plugin protocol %d", id, Platform(), ProtocolVersion)
		}
	} else if v, ok = entry.Version(version); !ok {
		return InstalledPlugin{}, fmt.Errorf("%w: %s %s", ErrNotInStore, id, version)
	} else if !v.installable() {
		return InstalledPlugin{}, fmt.Errorf("%s %s is not available for %s or needs a newer FloweySales", id, version, Platform())
	}
	if isInstalled && current.Manifest.Version == v.Manifest.Version {
		return current, nil
	}
	if v.Manifest.Terms != "" && !acceptTerms {
		return InstalledPlugin{}, &TermsError{PluginID: id, Terms: v.Manifest.Terms}
	}
	if err := m.checkProviderFree(v.Manifest); err != nil {
		return InstalledPlugin{}, err
	}
	artifact := v.Artifacts[Platform()]
	data, err := m.store.fetch(ctx, m.http, artifact.URL, maxArtifactBytes)
	if err != nil {
		return InstalledPlugin{}, fmt.Errorf("download %s %s: %w", id, v.Manifest.Version, err)
	}
	if artifact.Size > 0 && int64(len(data)) != artifact.Size {
		return InstalledPlugin{}, fmt.Errorf("download %s %s: size %d does not match the store (%d)", id, v.Manifest.Version, len(data), artifact.Size)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != artifact.SHA256 {
		return InstalledPlugin{}, fmt.Errorf("download %s %s: SHA-256 mismatch; refusing to install", id, v.Manifest.Version)
	}
	return m.installBinary(ctx, data, &v.Manifest, m.cfg.StoreURL)
}

// InstallFile sideloads a local plugin binary (development and air-gapped
// installs). Its manifest comes from the binary itself.
func (m *Manager) InstallFile(ctx context.Context, path string, acceptTerms bool) (InstalledPlugin, error) {
	m.installMu.Lock()
	defer m.installMu.Unlock()
	f, err := os.Open(path)
	if err != nil {
		return InstalledPlugin{}, err
	}
	data, err := readCapped(f, maxArtifactBytes)
	f.Close()
	if err != nil {
		return InstalledPlugin{}, err
	}
	return m.installBinary(ctx, data, nil, "file", func(described Manifest) error {
		if described.Terms != "" && !acceptTerms {
			return &TermsError{PluginID: described.ID, Terms: described.Terms}
		}
		return m.checkProviderFree(described)
	})
}

// checkProviderFree refuses a second plugin for an already provided provider id.
func (m *Manager) checkProviderFree(manifest Manifest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if other, ok := m.byProviderLocked(manifest.Provider.ID); ok && other.Manifest.ID != manifest.ID {
		return fmt.Errorf("provider %s is already provided by plugin %s; uninstall it first", manifest.Provider.ID, other.Manifest.ID)
	}
	return nil
}

// installBinary stages data, handshakes with it and only then swaps it in.
// expected, when set, must match the binary's own manifest exactly.
func (m *Manager) installBinary(ctx context.Context, data []byte, expected *Manifest, source string, checks ...func(Manifest) error) (InstalledPlugin, error) {
	staging, err := os.MkdirTemp(m.cfg.Dir, ".staging-")
	if err != nil {
		return InstalledPlugin{}, err
	}
	defer os.RemoveAll(staging)
	name := "plugin"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	stagedPath := filepath.Join(staging, name)
	if err := os.WriteFile(stagedPath, data, 0o750); err != nil {
		return InstalledPlugin{}, err
	}
	described, err := m.handshake(ctx, stagedPath)
	if err != nil {
		return InstalledPlugin{}, err
	}
	if expected != nil && (described.ID != expected.ID || described.Version != expected.Version || described.Provider.ID != expected.Provider.ID) {
		return InstalledPlugin{}, fmt.Errorf("downloaded binary describes itself as %s %s (%s), not %s %s (%s)",
			described.ID, described.Version, described.Provider.ID, expected.ID, expected.Version, expected.Provider.ID)
	}
	for _, check := range checks {
		if err := check(described); err != nil {
			return InstalledPlugin{}, err
		}
	}
	id := described.ID
	versionDir := filepath.Join(m.cfg.Dir, id, described.Version+"-"+randomSuffix())
	if err := os.MkdirAll(filepath.Dir(versionDir), 0o750); err != nil {
		return InstalledPlugin{}, err
	}
	if err := os.Rename(staging, versionDir); err != nil {
		return InstalledPlugin{}, fmt.Errorf("move plugin into place: %w", err)
	}
	sum := sha256.Sum256(data)
	rel, _ := filepath.Rel(m.cfg.Dir, filepath.Join(versionDir, name))
	entry := InstalledPlugin{Manifest: described, Binary: filepath.ToSlash(rel), SHA256: hex.EncodeToString(sum[:]),
		InstalledAt: time.Now().UTC(), Source: source}

	m.mu.Lock()
	previous, hadPrevious := m.installed[id]
	m.installed[id] = entry
	if err := m.saveLocked(); err != nil {
		if hadPrevious {
			m.installed[id] = previous
		} else {
			delete(m.installed, id)
		}
		m.mu.Unlock()
		_ = os.RemoveAll(versionDir)
		return InstalledPlugin{}, fmt.Errorf("record installation: %w", err)
	}
	m.stopLocked(id)
	m.mu.Unlock()
	if hadPrevious {
		// Windows refuses to delete the executable while the old process stops.
		removeSoon(filepath.Join(m.cfg.Dir, filepath.Dir(filepath.FromSlash(previous.Binary))))
	}
	return entry, nil
}

// removeSoon deletes dir now, or keeps retrying in the background while a
// stopping process still holds it open (Windows).
func removeSoon(dir string) {
	if os.RemoveAll(dir) == nil {
		return
	}
	go func() {
		for attempt := 0; attempt < 5; attempt++ {
			time.Sleep(time.Duration(attempt+1) * time.Second)
			if os.RemoveAll(dir) == nil {
				return
			}
		}
	}()
}

func randomSuffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// handshake starts a staged binary without any declared secrets and asks it
// to describe itself.
func (m *Manager) handshake(ctx context.Context, path string) (Manifest, error) {
	env, _ := m.env(nil)
	proc, err := startProcess(path, filepath.Dir(path), env)
	if err != nil {
		return Manifest{}, fmt.Errorf("plugin does not start on %s: %w", Platform(), err)
	}
	defer proc.stop(2 * time.Second)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var described Manifest
	if err := proc.call(ctx, "describe", struct{}{}, &described); err != nil {
		return Manifest{}, fmt.Errorf("plugin handshake failed: %w", err)
	}
	if err := described.Validate(); err != nil {
		return Manifest{}, fmt.Errorf("plugin manifest rejected: %w", err)
	}
	return described, nil
}

// Uninstall stops and removes a plugin.
func (m *Manager) Uninstall(id string) error {
	m.installMu.Lock()
	defer m.installMu.Unlock()
	m.mu.Lock()
	previous, ok := m.installed[id]
	if !ok {
		m.mu.Unlock()
		return ErrNotInstalled
	}
	delete(m.installed, id)
	if err := m.saveLocked(); err != nil {
		m.installed[id] = previous
		m.mu.Unlock()
		return fmt.Errorf("record uninstallation: %w", err)
	}
	m.stopLocked(id)
	m.mu.Unlock()
	// Only this version's directory: a reinstall gets a fresh one.
	removeSoon(filepath.Join(m.cfg.Dir, filepath.Dir(filepath.FromSlash(previous.Binary))))
	return nil
}

// ---------------------------------------------------------------------------
// Overview for Settings → Plugins
// ---------------------------------------------------------------------------

// EnvStatus says whether a declared variable is configured (never its value).
type EnvStatus struct {
	EnvVar
	Configured bool `json:"configured"`
}

// InstalledStatus is an installed plugin with its runtime state.
type InstalledStatus struct {
	Manifest        Manifest    `json:"manifest"`
	InstalledAt     time.Time   `json:"installed_at"`
	Source          string      `json:"source"`
	SHA256          string      `json:"sha256"`
	Running         bool        `json:"running"`
	LastError       string      `json:"last_error,omitempty"`
	Env             []EnvStatus `json:"env"`
	Ready           bool        `json:"ready"` // every required variable is configured
	LatestVersion   string      `json:"latest_version,omitempty"`
	UpdateAvailable bool        `json:"update_available"`
	Log             []string    `json:"log"`
}

// StorePlugin is a store entry annotated for this backend.
type StorePlugin struct {
	Manifest         Manifest `json:"manifest"` // newest version
	ReleasedAt       string   `json:"released_at,omitempty"`
	Notes            string   `json:"notes,omitempty"`
	Versions         []string `json:"versions"`
	LatestVersion    string   `json:"latest_version,omitempty"` // newest installable here
	Installable      bool     `json:"installable"`
	Reason           string   `json:"reason,omitempty"`
	InstalledVersion string   `json:"installed_version,omitempty"`
	UpdateAvailable  bool     `json:"update_available"`
}

// StoreStatus is the last store check.
type StoreStatus struct {
	CheckedAt *time.Time    `json:"checked_at,omitempty"`
	Error     string        `json:"error,omitempty"`
	Warnings  []string      `json:"warnings,omitempty"`
	Plugins   []StorePlugin `json:"plugins"`
}

// Overview is everything Settings → Plugins shows.
type Overview struct {
	Platform  string            `json:"platform"`
	Protocol  int               `json:"protocol"`
	StoreURL  string            `json:"store_url"`
	Dir       string            `json:"dir"`
	Installed []InstalledStatus `json:"installed"`
	Store     StoreStatus       `json:"store"`
}

// Overview combines installed state with the cached store index; it never
// fetches.
func (m *Manager) Overview() Overview {
	out := Overview{Platform: Platform(), Protocol: ProtocolVersion, StoreURL: m.cfg.StoreURL, Dir: m.cfg.Dir,
		Installed: []InstalledStatus{}, Store: StoreStatus{Plugins: []StorePlugin{}}}
	snap := m.cachedStore()
	installed := m.Installed()
	installedVersion := map[string]string{}
	for _, p := range installed {
		installedVersion[p.Manifest.ID] = p.Manifest.Version
	}
	if snap != nil {
		at := snap.checkedAt
		out.Store.CheckedAt = &at
		if snap.err != nil {
			out.Store.Error = snap.err.Error()
		}
		out.Store.Warnings = snap.warnings
		for _, p := range snap.index.Plugins {
			newest := p.newest()
			sp := StorePlugin{Manifest: newest.Manifest, ReleasedAt: newest.ReleasedAt, Notes: newest.Notes, InstalledVersion: installedVersion[p.ID]}
			for _, v := range p.Versions {
				sp.Versions = append(sp.Versions, v.Manifest.Version)
			}
			sort.Slice(sp.Versions, func(i, j int) bool { return CompareVersions(sp.Versions[i], sp.Versions[j]) > 0 })
			if latest, ok := p.Latest(IsPrerelease(sp.InstalledVersion)); ok {
				sp.LatestVersion, sp.Installable = latest.Manifest.Version, true
				sp.UpdateAvailable = sp.InstalledVersion != "" && CompareVersions(latest.Manifest.Version, sp.InstalledVersion) > 0
			} else if newest.Manifest.Protocol > ProtocolVersion {
				sp.Reason = "Needs a newer FloweySales (plugin protocol " + fmt.Sprint(newest.Manifest.Protocol) + ")."
			} else {
				sp.Reason = "No build for " + Platform() + "."
			}
			out.Store.Plugins = append(out.Store.Plugins, sp)
		}
	}
	latest := map[string]StorePlugin{}
	for _, sp := range out.Store.Plugins {
		latest[sp.Manifest.ID] = sp
	}
	for _, p := range installed {
		st := InstalledStatus{Manifest: p.Manifest, InstalledAt: p.InstalledAt, Source: p.Source, SHA256: p.SHA256, Env: []EnvStatus{}, Log: []string{}}
		_, missing := m.env(&p.Manifest)
		st.Ready = len(missing) == 0
		for _, e := range p.Manifest.Env {
			st.Env = append(st.Env, EnvStatus{EnvVar: e, Configured: m.cfg.LookupEnv != nil && m.cfg.LookupEnv(e.Name) != ""})
		}
		m.mu.Lock()
		if proc := m.procs[p.Manifest.ID]; proc != nil {
			st.Running = proc.alive()
			for _, line := range proc.stderr.tail() {
				st.Log = append(st.Log, redact.String(line))
			}
		}
		if f, ok := m.failures[p.Manifest.ID]; ok {
			st.LastError = redact.String(f.err.Error())
		}
		m.mu.Unlock()
		if sp, ok := latest[p.Manifest.ID]; ok {
			st.LatestVersion, st.UpdateAvailable = sp.LatestVersion, sp.UpdateAvailable
		}
		out.Installed = append(out.Installed, st)
	}
	return out
}

// Provider returns the plugin providing a provider id.
func (m *Manager) Provider(p market.Provider) (InstalledPlugin, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.byProviderLocked(p.ID())
}
