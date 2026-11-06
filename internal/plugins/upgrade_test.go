package plugins

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func openAt(t *testing.T, store *fakeStore, dir, appVersion, build string, key *string) *Manager {
	t.Helper()
	m, err := Open(Config{Dir: dir, StoreURL: store.srv.URL + "/index.json", HTTPClient: store.srv.Client(), AppVersion: appVersion, AppBuild: build,
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

func ranged(version, min, max string) Manifest {
	m := fakeManifest(version)
	m.MinAppVersion, m.MaxAppVersion = min, max
	return m
}

func TestManifestAppRangeValidationAndRequirement(t *testing.T) {
	for _, bad := range []Manifest{ranged("1.0.0", "latest", ""), ranged("1.0.0", "2.0.0", "1.0.0")} {
		if err := bad.Validate(); err == nil {
			t.Errorf("accepted %q..%q", bad.MinAppVersion, bad.MaxAppVersion)
		}
	}
	m := ranged("1.0.0", "0.5.0", "0.9.0")
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		app       string
		ok, known bool
	}{{"0.6.0", true, true}, {"v0.5.0", true, true}, {"0.9.1", false, true}, {"0.4.9", false, true}, {"", true, false}, {"dev", true, false}}
	for _, c := range cases {
		ok, known, why := m.AppRequirement(c.app)
		if ok != c.ok || known != c.known || (!c.ok && why == "") {
			t.Errorf("app %q: ok=%v known=%v why=%q", c.app, ok, known, why)
		}
	}
}

func TestInstallGateBlocksKnownIncompatibleAndKeepsWorkingPlugin(t *testing.T) {
	store := newFakeStore(t)
	store.publish(ranged("1.0.0", "0.5.0", ""), fakeRelease(t, ranged("1.0.0", "0.5.0", "")))
	store.publish(ranged("2.0.0", "0.9.0", ""), fakeRelease(t, ranged("2.0.0", "0.9.0", "")))
	store.publish(ranged("0.9.0", "", "0.4.0"), fakeRelease(t, ranged("0.9.0", "", "0.4.0")))
	m := openAt(t, store, t.TempDir(), "0.6.0", "build-a", nil)
	ctx := context.Background()

	// Latest skips the version that needs a newer app; an explicit pick is never silently substituted.
	got, err := m.Install(ctx, "fake", "", false)
	if err != nil || got.Manifest.Version != "1.0.0" {
		t.Fatalf("latest: %+v %v", got, err)
	}
	for _, v := range []string{"2.0.0", "0.9.0"} { // upgrade and downgrade
		if _, err := m.Install(ctx, "fake", v, false); err == nil || !strings.Contains(err.Error(), "cannot be installed here") || !strings.Contains(err.Error(), "not changed") {
			t.Fatalf("%s: %v", v, err)
		}
	}
	if cur := m.InstalledAll()[0]; cur.Manifest.Version != "1.0.0" {
		t.Fatalf("a refused install replaced the working plugin: %+v", cur)
	}
	_, _ = m.RefreshStore(ctx)
	for _, sp := range m.Overview().Store.Plugins {
		if sp.LatestVersion != "1.0.0" {
			t.Fatalf("store latest: %+v", sp)
		}
	}
}

func TestSideloadIsGatedByTheBinarysOwnRange(t *testing.T) {
	store := newFakeStore(t)
	m := openAt(t, store, t.TempDir(), "0.6.0", "a", nil)
	bin := filepath.Join(t.TempDir(), "plugin")
	if err := os.WriteFile(bin, fakeRelease(t, ranged("1.0.0", "0.9.0", "")), 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := m.InstallFile(context.Background(), bin, false); err == nil || !strings.Contains(err.Error(), "needs FloweySales 0.9.0") {
		t.Fatalf("sideload: %v", err)
	}
	if len(m.InstalledAll()) != 0 {
		t.Fatal("refused sideload left an installation")
	}
}

func TestStoreListingThatDisagreesWithBinaryRangeIsRefused(t *testing.T) {
	store := newFakeStore(t)
	store.publish(ranged("1.0.0", "", ""), fakeRelease(t, ranged("1.0.0", "0.9.0", "")))
	m := openAt(t, store, t.TempDir(), "0.6.0", "a", nil)
	if _, err := m.Install(context.Background(), "fake", "", false); err == nil {
		t.Fatal("installed a binary whose own range excludes this app")
	}
}

func TestUnreleasedBuildReportsRangeAsUnverified(t *testing.T) {
	store := newFakeStore(t)
	store.publish(ranged("1.0.0", "0.5.0", ""), fakeRelease(t, ranged("1.0.0", "0.5.0", "")))
	m := openAt(t, store, t.TempDir(), "", "a", nil)
	ctx := context.Background()
	if _, err := m.Install(ctx, "fake", "", false); err != nil {
		t.Fatal(err) // not claimed compatible, not blocked: the staged handshake still ran
	}
	c, err := m.Verify(ctx, "fake")
	if err != nil || c.Status != CompatUnknown || !strings.Contains(c.Reason, "does not report a release version") {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestInstalledPluginOutsideNewAppRangeIsKeptNotRunAndRecovers(t *testing.T) {
	store := newFakeStore(t)
	store.publish(ranged("1.0.0", "0.5.0", "0.6.0"), fakeRelease(t, ranged("1.0.0", "0.5.0", "0.6.0")))
	key := "k"
	dir := t.TempDir()
	ctx := context.Background()
	m := openAt(t, store, dir, "0.6.0", "a", &key)
	if _, err := m.Install(ctx, "fake", "", false); err != nil {
		t.Fatal(err)
	}
	// Preflight for a target app version says so before the upgrade.
	pre := m.Preflight(ProtocolVersion, "0.7.0")
	if pre[0].Target != CompatIncompatible || !strings.Contains(pre[0].Reason, "up to 0.6.0") {
		t.Fatalf("preflight: %+v", pre[0])
	}
	if pre := m.Preflight(ProtocolVersion, "0.6.0"); pre[0].Target != CompatCompatible {
		t.Fatalf("preflight same: %+v", pre[0])
	}
	if pre := m.Preflight(ProtocolVersion, ""); pre[0].Target != CompatUnknown {
		t.Fatalf("preflight undeclared: %+v", pre[0])
	}
	m.Close()

	// Upgrade to 0.7.0: kept, reported, not run.
	up := openAt(t, store, dir, "0.7.0", "b", &key)
	if len(up.Installed()) != 0 || len(up.InstalledAll()) != 1 {
		t.Fatal("plugin dropped or still runnable")
	}
	if _, err := up.Quote(ctx, "FAKE", "X"); err == nil || !strings.Contains(err.Error(), "up to 0.6.0") {
		t.Fatalf("ran: %v", err)
	}
	up.Close()
	// App rolled back to 0.6.0 with the same plugin directory: usable again, untouched.
	back := openAt(t, store, dir, "0.6.0", "a", &key)
	if len(back.Installed()) != 1 {
		t.Fatal("plugin not usable after app rollback")
	}
	if _, err := back.Quote(ctx, "FAKE", "X"); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateKeepsPreviousVersionAndRollbackRestoresIt(t *testing.T) {
	store := newFakeStore(t)
	store.publish(fakeManifest("1.0.0"), fakeRelease(t, fakeManifest("1.0.0")))
	store.publish(fakeManifest("1.1.0"), fakeRelease(t, fakeManifest("1.1.0")))
	store.publish(fakeManifest("1.2.0"), fakeRelease(t, fakeManifest("1.2.0")))
	key := "secret-key-value"
	dir := t.TempDir()
	m := openAt(t, store, dir, "0.6.0", "a", &key)
	ctx := context.Background()
	if _, err := m.Rollback(ctx, "fake"); err != ErrNotInstalled {
		t.Fatalf("rollback of nothing: %v", err)
	}
	first, err := m.Install(ctx, "fake", "1.0.0", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Rollback(ctx, "fake"); err != ErrNoPrevious {
		t.Fatalf("no previous: %v", err)
	}
	if _, err := m.Install(ctx, "fake", "1.1.0", false); err != nil {
		t.Fatal(err)
	}
	ov := m.Overview().Installed[0]
	if ov.Manifest.Version != "1.1.0" || ov.PreviousVersion != "1.0.0" {
		t.Fatalf("overview: %+v", ov)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(first.Binary))); err != nil {
		t.Fatalf("previous binary was deleted: %v", err)
	}
	if _, err := m.Install(ctx, "fake", "1.2.0", false); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Rollback(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	cur := m.InstalledAll()[0]
	if cur.Manifest.Version != "1.1.0" || cur.Previous == nil || cur.Previous.Manifest.Version != "1.2.0" {
		t.Fatalf("after rollback: %+v", cur)
	}
	if _, err := m.Quote(ctx, "FAKE", "X"); err != nil {
		t.Fatalf("rolled back plugin does not run with the saved key: %v", err)
	}
	// Persisted: a reopened manager (container recreated on the same volume) sees the same.
	m.Close()
	again := openAt(t, store, dir, "0.6.0", "a", &key)
	if got := again.InstalledAll()[0]; got.Manifest.Version != "1.1.0" || got.Previous == nil {
		t.Fatalf("not persisted: %+v", got)
	}
	// A tampered previous binary is refused and nothing changes.
	prev := again.InstalledAll()[0].Previous
	if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(prev.Binary)), []byte("tampered"), 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := again.Rollback(ctx, "fake"); err == nil || !strings.Contains(err.Error(), "changed on disk") {
		t.Fatalf("tampered: %v", err)
	}
	if again.InstalledAll()[0].Manifest.Version != "1.1.0" {
		t.Fatal("failed rollback changed the installation")
	}
	if err := again.Uninstall("fake"); err != nil {
		t.Fatal(err)
	}
}

func TestFailedOrInterruptedUpdateLeavesWorkingPluginAndCleansStaging(t *testing.T) {
	store := newFakeStore(t)
	store.publish(fakeManifest("1.0.0"), fakeRelease(t, fakeManifest("1.0.0")))
	bad := fakeManifest("1.1.0")
	store.publish(bad, fakeRelease(t, bad))
	store.mu.Lock()
	store.files["/fake-1.1.0"] = []byte("truncated download")
	store.mu.Unlock()
	key := "k"
	dir := t.TempDir()
	m := openAt(t, store, dir, "0.6.0", "a", &key)
	ctx := context.Background()
	if _, err := m.Install(ctx, "fake", "1.0.0", false); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Install(ctx, "fake", "1.1.0", false); err == nil {
		t.Fatal("corrupt update accepted")
	}
	if m.InstalledAll()[0].Manifest.Version != "1.0.0" {
		t.Fatal("failed update replaced the plugin")
	}
	if _, err := m.Quote(ctx, "FAKE", "X"); err != nil {
		t.Fatal(err)
	}
	// A crash mid-update leaves a staging directory and a temp state file.
	m.Close()
	stale := filepath.Join(dir, ".staging-crashed")
	_ = os.MkdirAll(stale, 0o750)
	_ = os.WriteFile(filepath.Join(dir, "installed.json.tmp"), []byte("{broken"), 0o640)
	reopened := openAt(t, store, dir, "0.6.0", "a", &key)
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("stale staging directory kept")
	}
	if len(reopened.Installed()) != 1 {
		t.Fatal("plugin lost after interrupted update")
	}
}

func TestUpgradeReportAfterContainerRecreationOnSameVolume(t *testing.T) {
	store := newFakeStore(t)
	store.publish(fakeManifest("1.0.0"), fakeRelease(t, fakeManifest("1.0.0")))
	tools := toolsManifest("1.0.0")
	store.publish(tools, fakeRelease(t, tools))
	key := "very-secret-api-key"
	dir := t.TempDir()
	ctx := context.Background()
	a := openAt(t, store, dir, "0.6.0", "build-a", &key)
	for _, id := range []string{"fake", tools.ID} {
		if _, err := a.Install(ctx, id, "", false); err != nil {
			t.Fatal(err)
		}
	}
	a.CheckAfterStart(ctx)
	if a.LastUpgradeReport() != nil {
		t.Fatal("first start must not claim an upgrade")
	}
	before := map[string]string{}
	for _, p := range a.InstalledAll() {
		before[p.Manifest.ID] = p.SHA256
	}
	a.Close()

	// New container: new build, same plugin volume, the key is no longer configured.
	b := openAt(t, store, dir, "0.7.0", "build-b", nil)
	b.CheckAfterStart(ctx)
	report := b.LastUpgradeReport()
	if report == nil || report.FromBuild != "build-a" || report.ToBuild != "build-b" || report.FromVersion != "0.6.0" || report.ToVersion != "0.7.0" {
		t.Fatalf("report: %+v", report)
	}
	byID := map[string]UpgradeEntry{}
	for _, e := range report.Plugins {
		byID[e.ID] = e
	}
	if e := byID[tools.ID]; e.Outcome != OutcomeVerified || !e.Retained || e.Disabled {
		t.Fatalf("tools plugin: %+v", e)
	}
	if e := byID["fake"]; e.Outcome != OutcomeNeedsAttention || !e.Retained || !strings.Contains(e.Reason, "FAKE_API_KEY") {
		t.Fatalf("fake plugin: %+v", e)
	}
	for _, p := range b.InstalledAll() {
		if p.SHA256 != before[p.Manifest.ID] {
			t.Fatalf("%s changed across the upgrade", p.Manifest.ID)
		}
	}
	// No secret is stored in the plugin directory or in what the API returns.
	overview, _ := json.Marshal(b.Overview())
	if strings.Contains(string(overview), key) {
		t.Fatal("overview leaks the key")
	}
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && info.Size() < 1<<20 {
			if raw, _ := os.ReadFile(path); strings.Contains(string(raw), key) {
				t.Errorf("%s contains the secret", path)
			}
		}
		return nil
	})
	// The report survives a restart of the same build.
	b.Close()
	c := openAt(t, store, dir, "0.7.0", "build-b", nil)
	c.CheckAfterStart(ctx)
	if r := c.LastUpgradeReport(); r == nil || r.ToBuild != "build-b" {
		t.Fatalf("report lost: %+v", r)
	}

	// A plugin that cannot run after the next upgrade is reported as disabled, kept.
	c.Close()
	rewriteInstalledProtocol(t, dir, 7)
	d := openAt(t, store, dir, "0.8.0", "build-c", nil)
	d.CheckAfterStart(ctx)
	for _, e := range d.LastUpgradeReport().Plugins {
		if e.Outcome != OutcomeIncompatible || !e.Disabled || !e.Retained {
			t.Fatalf("incompatible: %+v", e)
		}
	}
}
