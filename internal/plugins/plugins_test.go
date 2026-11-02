package plugins

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"floweysales/internal/market"
)

// The fake plugin is this test binary. Each published "release" is the
// binary with a manifest appended after fakeMarker, so releases differ in
// checksum and identity like real builds; executables ignore trailing bytes.
const fakeMarker = "\n--FLOWEYSALES-FAKE-PLUGIN-MANIFEST--\n"

func TestMain(m *testing.M) {
	if os.Getenv("FLOWEYSALES_PLUGIN") == "1" {
		runFakePlugin()
		return
	}
	os.Exit(m.Run())
}

func runFakePlugin() {
	self, _ := os.Executable()
	raw, _ := os.ReadFile(self)
	i := bytes.LastIndex(raw, []byte(fakeMarker))
	if i < 0 {
		fmt.Fprintln(os.Stderr, "no fake manifest appended")
		os.Exit(2)
	}
	var manifest Manifest
	_ = json.Unmarshal(raw[i+len(fakeMarker):], &manifest)
	fmt.Fprintln(os.Stderr, "fake plugin", manifest.Version, "starting; key", os.Getenv("FAKE_API_KEY"))
	if os.Getenv("MYSQL_DSN") != "" {
		fmt.Fprintln(os.Stderr, "LEAKED BACKEND ENV")
		os.Exit(4)
	}
	out := json.NewEncoder(os.Stdout)
	var mu sync.Mutex
	send := func(v any) { mu.Lock(); _ = out.Encode(v); mu.Unlock() }
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		var req struct {
			ID     uint64            `json:"id"`
			Method string            `json:"method"`
			Params map[string]any    `json:"params"`
			_      map[string]string `json:"-"`
		}
		_ = json.Unmarshal(in.Bytes(), &req)
		reply := func(result any) { send(map[string]any{"id": req.ID, "result": result}) }
		fail := func(code, msg string) {
			send(map[string]any{"id": req.ID, "error": map[string]string{"code": code, "message": msg}})
		}
		symbol, _ := req.Params["symbol"].(string)
		switch req.Method {
		case "describe":
			reply(manifest)
		case "quote":
			if os.Getenv("FAKE_API_KEY") == "" {
				fail(CodeNotConfigured, "FAKE_API_KEY missing")
				continue
			}
			if symbol == "CRASH" {
				os.Exit(3)
			}
			reply(map[string]any{"symbol": symbol, "price": 42.5, "currency": "USD", "data": map[string]any{"version": manifest.Version}})
		case "history":
			reply(map[string]any{"symbol": symbol, "range": req.Params["range"], "interval": "1d", "currency": "USD",
				"points": []map[string]any{{"t": time.Now().Unix() - 60, "o": 1, "h": 1, "l": 1, "c": 1}}})
		case "financials":
			reply(map[string]any{"symbol": symbol, "provider": manifest.Provider.ID, "source": "fake",
				"reports": []map[string]any{{"period": "a", "period_end": "2020-03-31"}, {"period": "b", "period_end": "2024-03-31"}}})
		case "research":
			reply(map[string]any{"provider": manifest.Provider.ID, "operation": req.Params["operation"], "data": map[string]any{"ok": true}})
		case "events":
			reply([]map[string]any{
				{"kind": "earnings", "date": "2026-11-20", "confirmed": false},
				{"symbol": symbol, "kind": "dividend", "date": "2026-11-14", "amount": 0.24, "currency": "USD", "confirmed": true},
				{"kind": "rumour", "date": "2026-11-15"},
				{"kind": "split", "date": "next week"},
			})
		case "forum_posts":
			reply([]map[string]any{{"id": 1, "title": "hello", "symbol": symbol}})
		case "tool":
			input, _ := req.Params["input"].(map[string]any)
			if req.Params["tool"] == "broken" {
				reply(map[string]any{"blocks": []map[string]any{{"type": "chart"}}})
				continue
			}
			reply(map[string]any{"title": "Echo", "blocks": []map[string]any{{"type": "text", "text": fmt.Sprint(input["text"], "/", input["files"])}}})
		default:
			fail(CodeUnsupported, "unknown method")
		}
	}
}

var (
	testBinaryOnce sync.Once
	testBinary     []byte
)

func fakeManifest(version string) Manifest {
	return Manifest{
		Protocol: 1, ID: "fake", Name: "Fake provider", Version: version, Description: "Test double.", Publisher: "Tests",
		Provider: ProviderInfo{ID: "FAKE", Label: "Fake", Capabilities: []string{CapQuote, CapHistory, CapFinancials, CapResearch, CapForum, CapEvents},
			QuoteKind: "ohlc_bar_close", MinPollSeconds: 3,
			Research: []ResearchOperation{{Operation: "profile", Path: "/profile", Description: "Profile", SymbolRequired: true}}},
		Env: []EnvVar{{Name: "FAKE_API_KEY", Secret: true, Required: true, Description: "Key"}},
	}
}

// fakeRelease returns a plugin binary that describes itself with manifest.
func fakeRelease(t *testing.T, manifest Manifest) []byte {
	t.Helper()
	testBinaryOnce.Do(func() {
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		testBinary, err = os.ReadFile(self)
		if err != nil {
			t.Fatal(err)
		}
	})
	encoded, _ := json.Marshal(manifest)
	return append(append(append([]byte{}, testBinary...), fakeMarker...), encoded...)
}

// fakeStore serves an index over TLS with one artifact per release.
type fakeStore struct {
	srv      *httptest.Server
	mu       sync.Mutex
	index    Index
	files    map[string][]byte
	requests int
}

func newFakeStore(t *testing.T) *fakeStore {
	s := &fakeStore{index: Index{Schema: 1}, files: map[string][]byte{}}
	s.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.requests++
		if r.URL.Path == "/index.json" {
			_ = json.NewEncoder(w).Encode(s.index)
			return
		}
		if r.URL.Path == "/redirect-to-http" {
			http.Redirect(w, r, "http://example.test/fake-1.0.0", http.StatusFound)
			return
		}
		data, ok := s.files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

// publish adds a release; binary may describe a different manifest than the
// index claims (to test the handshake check).
func (s *fakeStore) publish(listed Manifest, binary []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := "/" + listed.ID + "-" + listed.Version
	s.files[path] = binary
	sum := sha256.Sum256(binary)
	v := IndexVersion{Manifest: listed, ReleasedAt: "2026-10-31", Artifacts: map[string]Artifact{
		Platform(): {URL: s.srv.URL + path, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(binary))},
	}}
	for i := range s.index.Plugins {
		if s.index.Plugins[i].ID == listed.ID {
			s.index.Plugins[i].Versions = append(s.index.Plugins[i].Versions, v)
			return
		}
	}
	s.index.Plugins = append(s.index.Plugins, IndexPlugin{ID: listed.ID, Versions: []IndexVersion{v}})
}

func openManager(t *testing.T, store *fakeStore, dir string, key *string) *Manager {
	t.Helper()
	m, err := Open(Config{Dir: dir, StoreURL: store.srv.URL + "/index.json", HTTPClient: store.srv.Client(),
		LookupEnv: func(name string) string {
			if name == "FAKE_API_KEY" && key != nil {
				return *key
			}
			return ""
		}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m
}

func TestInstallCheckUpdateAndUninstallFromStore(t *testing.T) {
	t.Setenv("MYSQL_DSN", "must-not-reach-plugins")
	store := newFakeStore(t)
	store.publish(fakeManifest("1.0.0"), fakeRelease(t, fakeManifest("1.0.0")))
	key := ""
	dir := t.TempDir()
	m := openManager(t, store, dir, &key)
	ctx := context.Background()

	if _, err := m.Quote(ctx, "FAKE", "NVDA"); !errors.Is(err, market.ErrProviderNotInstalled) {
		t.Fatalf("uninstalled provider: %v", err)
	}
	installed, err := m.Install(ctx, "fake", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if installed.Manifest.Version != "1.0.0" || installed.Source != m.StoreURL() {
		t.Fatalf("%+v", installed)
	}
	caps := m.Providers()
	if len(caps) != 1 || caps[0].ID != "FAKE" || caps[0].Enabled || !caps[0].RequiresKey || caps[0].MinPollSeconds != 3 {
		t.Fatalf("missing key must disable the provider: %+v", caps)
	}
	if _, err := m.Quote(ctx, "FAKE", "NVDA"); err == nil {
		t.Fatal("quote without key succeeded")
	}

	// Configuring the key takes effect on the next process start.
	key = "fake-secret-key"
	m.mu.Lock()
	m.stopLocked("fake")
	m.mu.Unlock()
	asset, err := m.Quote(ctx, "FAKE", "NVDA")
	if err != nil || asset.Price != 42.5 || asset.Provider != "FAKE" || asset.Data["version"] != "1.0.0" {
		t.Fatalf("asset=%+v err=%v", asset, err)
	}
	ov := m.Overview()
	if len(ov.Installed) != 1 || !ov.Installed[0].Running || !ov.Installed[0].Ready || ov.Installed[0].UpdateAvailable {
		t.Fatalf("overview: %+v", ov.Installed)
	}
	for _, line := range ov.Installed[0].Log {
		if strings.Contains(line, "fake-secret-key") {
			t.Fatalf("secret leaked into plugin log: %q", line)
		}
	}

	// A new release shows up as an update after a store check.
	store.publish(fakeManifest("1.1.0"), fakeRelease(t, fakeManifest("1.1.0")))
	if _, err := m.RefreshStore(ctx); err != nil {
		t.Fatal(err)
	}
	ov = m.Overview()
	if !ov.Installed[0].UpdateAvailable || ov.Installed[0].LatestVersion != "1.1.0" || !ov.Store.Plugins[0].UpdateAvailable {
		t.Fatalf("update not detected: %+v / %+v", ov.Installed[0], ov.Store.Plugins)
	}
	if _, err := m.Install(ctx, "fake", "", false); err != nil {
		t.Fatal(err)
	}
	asset, err = m.Quote(ctx, "FAKE", "NVDA")
	if err != nil || asset.Data["version"] != "1.1.0" {
		t.Fatalf("update did not switch the running plugin: %+v err=%v", asset, err)
	}

	// The installation survives a restart of the backend.
	m.Close()
	reopened := openManager(t, store, dir, &key)
	if got := reopened.Installed(); len(got) != 1 || got[0].Manifest.Version != "1.1.0" {
		t.Fatalf("state not persisted: %+v", got)
	}

	if err := reopened.Uninstall("fake"); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Quote(ctx, "FAKE", "NVDA"); !errors.Is(err, market.ErrProviderNotInstalled) {
		t.Fatalf("uninstalled plugin still answers: %v", err)
	}
	if err := reopened.Uninstall("fake"); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("double uninstall: %v", err)
	}
}

func TestBackendRoutingEnforcesDeclaredCapabilitiesAndRanges(t *testing.T) {
	store := newFakeStore(t)
	store.publish(fakeManifest("1.0.0"), fakeRelease(t, fakeManifest("1.0.0")))
	key := "k"
	m := openManager(t, store, t.TempDir(), &key)
	ctx := context.Background()
	if _, err := m.Install(ctx, "fake", "1.0.0", false); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Search(ctx, "FAKE", "nv"); !errors.Is(err, market.ErrUnsupported) {
		t.Fatalf("undeclared capability must not reach the plugin: %v", err)
	}
	fin, err := m.Financials(ctx, "FAKE", "NVDA", market.FinancialYearRange{FromYear: 2023, ToYear: 2025})
	if err != nil || len(fin.Reports) != 1 || fin.Reports[0].PeriodEnd != "2024-03-31" {
		t.Fatalf("host must enforce the year range: %+v err=%v", fin, err)
	}
	if _, err := m.Research(ctx, "FAKE", "order_book", "NVDA", "", ""); !errors.Is(err, market.ErrUnsupported) {
		t.Fatalf("undeclared research operation: %v", err)
	}
	if res, err := m.Research(ctx, "FAKE", "PROFILE", "NVDA", "", ""); err != nil || res.Operation != "profile" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if ops := m.ResearchOperations(); len(ops) != 1 || ops[0].Provider != "FAKE" {
		t.Fatalf("%+v", ops)
	}
	if posts := m.ForumPosts(ctx, "NVDA", 5); len(posts) != 1 || posts[0].Title != "hello" {
		t.Fatalf("%+v", posts)
	}
	events, err := m.Events(ctx, "FAKE", "nvda", "2026-11-01", "2026-12-31")
	if err != nil || len(events) != 2 || events[0].Kind != "dividend" || events[0].Provider != "FAKE" || events[1].Symbol != "NVDA" || events[1].Confirmed {
		t.Fatalf("events must be cleaned, labelled and ordered by date: %+v err=%v", events, err)
	}
	if got := m.EventProviders(); len(got) != 1 || got[0] != "FAKE" {
		t.Fatalf("event providers %v", got)
	}
	if _, err := m.Events(ctx, "FAKE", "NVDA", "2026-01-01", "2027-12-31"); err == nil {
		t.Fatal("a window over 400 days must be refused before reaching the plugin")
	}
	series, err := m.History(ctx, "FAKE", "NVDA", "bogus")
	if err != nil || series.Range != market.Range1M || series.Provider != "FAKE" {
		t.Fatalf("series=%+v err=%v", series, err)
	}
}

func TestInstallRefusesTamperedOrMislabelledArtifacts(t *testing.T) {
	ctx := context.Background()
	t.Run("checksum", func(t *testing.T) {
		store := newFakeStore(t)
		store.publish(fakeManifest("1.0.0"), fakeRelease(t, fakeManifest("1.0.0")))
		store.mu.Lock()
		store.files["/fake-1.0.0"] = append(store.files["/fake-1.0.0"][:len(store.files["/fake-1.0.0"])-1], ' ')
		store.mu.Unlock()
		m := openManager(t, store, t.TempDir(), nil)
		if _, err := m.Install(ctx, "fake", "1.0.0", false); err == nil || !strings.Contains(err.Error(), "SHA-256") {
			t.Fatalf("tampered artifact installed: %v", err)
		}
		if len(m.Installed()) != 0 {
			t.Fatal("failed install left state behind")
		}
	})
	t.Run("identity", func(t *testing.T) {
		store := newFakeStore(t)
		store.publish(fakeManifest("1.0.0"), fakeRelease(t, fakeManifest("6.6.6")))
		m := openManager(t, store, t.TempDir(), nil)
		if _, err := m.Install(ctx, "fake", "1.0.0", false); err == nil || !strings.Contains(err.Error(), "describes itself") {
			t.Fatalf("mislabelled binary installed: %v", err)
		}
	})
	t.Run("terms", func(t *testing.T) {
		store := newFakeStore(t)
		withTerms := fakeManifest("1.0.0")
		withTerms.Terms = "Unofficial endpoints."
		store.publish(withTerms, fakeRelease(t, withTerms))
		m := openManager(t, store, t.TempDir(), nil)
		var terms *TermsError
		if _, err := m.Install(ctx, "fake", "", false); !errors.As(err, &terms) || terms.Terms != "Unofficial endpoints." {
			t.Fatalf("terms not enforced: %v", err)
		}
		if _, err := m.Install(ctx, "fake", "", true); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("future protocol", func(t *testing.T) {
		store := newFakeStore(t)
		future := fakeManifest("2.0.0")
		future.Protocol = 2
		store.publish(future, fakeRelease(t, future))
		m := openManager(t, store, t.TempDir(), nil)
		if _, err := m.Install(ctx, "fake", "", false); err == nil {
			t.Fatal("future protocol installed")
		}
		ov := m.Overview()
		if len(ov.Store.Plugins) != 1 || ov.Store.Plugins[0].Installable || !strings.Contains(ov.Store.Plugins[0].Reason, "newer FloweySales") {
			t.Fatalf("%+v", ov.Store.Plugins)
		}
	})
}

func TestTamperedInstalledBinaryDoesNotStartAndCrashesRestart(t *testing.T) {
	store := newFakeStore(t)
	store.publish(fakeManifest("1.0.0"), fakeRelease(t, fakeManifest("1.0.0")))
	key := "k"
	m := openManager(t, store, t.TempDir(), &key)
	ctx := context.Background()
	installed, err := m.Install(ctx, "fake", "", false)
	if err != nil {
		t.Fatal(err)
	}
	// A crash fails the call in flight; the next call starts a new process.
	if _, err := m.Quote(ctx, "FAKE", "CRASH"); !errors.Is(err, errProcessExited) {
		t.Fatalf("crash: %v", err)
	}
	if _, err := m.Quote(ctx, "FAKE", "NVDA"); err != nil {
		t.Fatalf("restart after crash: %v", err)
	}

	m.mu.Lock()
	m.stopLocked("fake")
	m.mu.Unlock()
	path := filepath.Join(m.Dir(), filepath.FromSlash(installed.Binary))
	// The stopped process may still hold the executable for a moment.
	deadline := time.Now().Add(10 * time.Second)
	for {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		if err == nil {
			_, err = f.WriteString("x")
			f.Close()
		}
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := m.Quote(ctx, "FAKE", "NVDA"); err == nil || !strings.Contains(err.Error(), "changed on disk") {
		t.Fatalf("modified binary started: %v", err)
	}
}

func TestInstallFileSideloadsAndRefusesProviderClash(t *testing.T) {
	store := newFakeStore(t)
	m := openManager(t, store, t.TempDir(), nil)
	path := filepath.Join(t.TempDir(), "fake-plugin")
	if err := os.WriteFile(path, fakeRelease(t, fakeManifest("0.9.0-dev.1")), 0o750); err != nil {
		t.Fatal(err)
	}
	got, err := m.InstallFile(context.Background(), path, false)
	if err != nil || got.Source != "file" || got.Manifest.Version != "0.9.0-dev.1" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	clash := fakeManifest("1.0.0")
	clash.ID = "other-fake"
	if err := os.WriteFile(path, fakeRelease(t, clash), 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := m.InstallFile(context.Background(), path, false); err == nil || !strings.Contains(err.Error(), "already provided") {
		t.Fatalf("second plugin for FAKE installed: %v", err)
	}
}

func TestDownloadsNeverFollowRedirectsOffHTTPS(t *testing.T) {
	store := newFakeStore(t)
	store.publish(fakeManifest("1.0.0"), fakeRelease(t, fakeManifest("1.0.0")))
	store.mu.Lock()
	artifact := store.index.Plugins[0].Versions[0].Artifacts[Platform()]
	artifact.URL = store.srv.URL + "/redirect-to-http"
	store.index.Plugins[0].Versions[0].Artifacts[Platform()] = artifact
	store.mu.Unlock()
	m := openManager(t, store, t.TempDir(), nil)
	if _, err := m.Install(context.Background(), "fake", "", false); err == nil || !strings.Contains(err.Error(), "refusing redirect") {
		t.Fatalf("followed a redirect to plain HTTP: %v", err)
	}
}

func TestStoreURLAndIndexValidation(t *testing.T) {
	for _, bad := range []string{"http://example.test/index.json", "ftp://x", ""} {
		if _, err := parseStoreURL(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	remote, _ := parseStoreURL("https://example.test/index.json")
	if _, err := remote.fetch(context.Background(), http.DefaultClient, "file:///etc/passwd", 10); err == nil {
		t.Fatal("remote store reached a local file")
	}
	good := fakeManifest("1.0.0")
	sum := strings.Repeat("ab", 32)
	idx := Index{Schema: 1, Plugins: []IndexPlugin{
		{ID: "fake", Versions: []IndexVersion{
			{Manifest: good, Artifacts: map[string]Artifact{Platform(): {URL: "https://x/a", SHA256: sum}}},
			{Manifest: fakeManifest("1.0.1"), Artifacts: map[string]Artifact{Platform(): {URL: "https://x/b", SHA256: "nope"}}},
		}},
		{ID: "../escape", Versions: []IndexVersion{{Manifest: good}}},
	}}
	raw, _ := json.Marshal(idx)
	parsed, warnings, err := parseIndex(raw)
	if err != nil || len(parsed.Plugins) != 1 || len(parsed.Plugins[0].Versions) != 1 || len(warnings) != 2 {
		t.Fatalf("parsed=%+v warnings=%v err=%v", parsed, warnings, err)
	}
}

func TestCompareVersions(t *testing.T) {
	ordered := []string{"0.9.9", "1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "1.2.0", "1.10.0", "10.0.0"}
	for i := 0; i < len(ordered); i++ {
		for j := 0; j < len(ordered); j++ {
			want := sign(i - j)
			if got := CompareVersions(ordered[i], ordered[j]); got != want {
				t.Errorf("Compare(%s, %s) = %d, want %d", ordered[i], ordered[j], got, want)
			}
		}
	}
	if ValidVersion("1.0") || ValidVersion("01.0.0") || !ValidVersion("1.0.0-rc.1") {
		t.Fatal("version validation")
	}
}

func TestManifestRejectsBackendSecretsAndBadIDs(t *testing.T) {
	for name, mutate := range map[string]func(*Manifest){
		"secret env":  func(m *Manifest) { m.Env = []EnvVar{{Name: "MYSQL_DSN"}} },
		"backend env": func(m *Manifest) { m.Env = []EnvVar{{Name: "FLOWEYSALES_SECRETS_KEY_FILE"}} },
		"path id":     func(m *Manifest) { m.ID = "../x" },
		"provider id": func(m *Manifest) { m.Provider.ID = "fake" },
		"protocol":    func(m *Manifest) { m.Protocol = 0 },
	} {
		m := fakeManifest("1.0.0")
		mutate(&m)
		if m.Validate() == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if validBinaryPath("../x") || validBinaryPath("/abs") || !validBinaryPath("fake/1.0.0-ab/plugin") {
		t.Fatal("binary path validation")
	}
}

func toolsManifest(version string) Manifest {
	return Manifest{Protocol: 1, ID: "tools", Name: "Tools", Version: version, Description: "Test double.", Publisher: "Tests",
		Tools: &ToolsInfo{Tab: ToolsTab{Label: "Tax", Icon: "bi-receipt"}, Tools: []Tool{
			{ID: "echo", Label: "Echo", Inputs: []ToolInput{
				{Name: "text", Label: "Text", Type: "text", Required: true},
				{Name: "mode", Label: "Mode", Type: "select", Options: []ToolOption{{Value: "a", Label: "A"}}},
				{Name: "files", Label: "Files", Type: "files"},
			}},
			{ID: "broken", Label: "Broken"},
		}}}
}

func TestToolsOnlyPluginInstallsRoutesAndStaysOutOfMarketData(t *testing.T) {
	store := newFakeStore(t)
	store.publish(toolsManifest("1.0.0"), fakeRelease(t, toolsManifest("1.0.0")))
	store.publish(fakeManifest("1.0.0"), fakeRelease(t, fakeManifest("1.0.0")))
	key := "k"
	m := openManager(t, store, t.TempDir(), &key)
	ctx := context.Background()
	if _, err := m.Install(ctx, "tools", "1.0.0", false); err != nil {
		t.Fatal(err)
	}
	// A provider-less plugin must not appear as an empty provider, and it must
	// not block installing a real provider.
	if got := m.Providers(); len(got) != 0 {
		t.Fatalf("tools plugin leaked into providers: %+v", got)
	}
	if _, err := m.Install(ctx, "fake", "1.0.0", false); err != nil {
		t.Fatal(err)
	}
	if got := m.Providers(); len(got) != 1 || got[0].ID != "FAKE" {
		t.Fatalf("%+v", got)
	}
	list := m.ToolPlugins()
	if len(list) != 1 || list[0].PluginID != "tools" || !list[0].Ready || list[0].Tools.Tab.Label != "Tax" {
		t.Fatalf("%+v", list)
	}

	files := []any{map[string]any{"name": "a.csv", "content": "x"}}
	res, err := m.CallTool(ctx, "tools", "echo", map[string]any{"text": "hej", "mode": "a", "files": files, "stray": "dropped"})
	if err != nil || len(res.Blocks) != 1 || !strings.HasPrefix(res.Blocks[0].Text, "hej/") {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	for name, input := range map[string]map[string]any{
		"missing required": {},
		"bad select":       {"text": "x", "mode": "zzz"},
		"wrong type":       {"text": 5},
		"bad files":        {"text": "x", "files": "nope"},
	} {
		if _, err := m.CallTool(ctx, "tools", "echo", input); !errors.Is(err, ErrToolInput) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := m.CallTool(ctx, "tools", "nope", nil); !errors.Is(err, ErrNoSuchTool) {
		t.Errorf("unknown tool: %v", err)
	}
	if _, err := m.CallTool(ctx, "fake", "echo", nil); !errors.Is(err, ErrNoSuchTool) {
		t.Errorf("a provider plugin has no tools: %v", err)
	}
	if _, err := m.CallTool(ctx, "tools", "broken", nil); err == nil || !strings.Contains(err.Error(), "invalid result") {
		t.Errorf("an invalid plugin result must be refused: %v", err)
	}
	if _, err := m.CallTool(ctx, "tools", "echo", map[string]any{"text": strings.Repeat("x", MaxToolInputBytes+1)}); !errors.Is(err, ErrToolInput) {
		t.Errorf("oversized input: %v", err)
	}
}

func TestManifestToolsAreValidated(t *testing.T) {
	good := toolsManifest("1.0.0")
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Manifest){
		"no provider and no tools": func(m *Manifest) { m.Tools = nil },
		"half-declared provider":   func(m *Manifest) { m.Provider.ID = "TOOLS" },
		"duplicate tool":           func(m *Manifest) { m.Tools.Tools = append(m.Tools.Tools, m.Tools.Tools[0]) },
		"unknown input type":       func(m *Manifest) { m.Tools.Tools[0].Inputs[0].Type = "slider" },
		"select without options":   func(m *Manifest) { m.Tools.Tools[0].Inputs[1].Options = nil },
		"bad icon":                 func(m *Manifest) { m.Tools.Tab.Icon = "x" },
	} {
		m := toolsManifest("1.0.0")
		mutate(&m)
		if err := m.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
