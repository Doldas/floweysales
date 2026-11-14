package plugins

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func installedFake(t *testing.T, key string, max int) (*Manager, context.Context) {
	t.Helper()
	store := newFakeStore(t)
	store.publish(fakeManifest("1.0.0"), fakeRelease(t, fakeManifest("1.0.0")))
	m := openManager(t, store, t.TempDir(), &key)
	m.cfg.MaxUserProcesses = max
	ctx := context.Background()
	if _, err := m.Install(ctx, "fake", "", false); err != nil {
		t.Fatal(err)
	}
	return m, ctx
}

func keyOf(t *testing.T, m *Manager, ctx context.Context) string {
	t.Helper()
	a, err := m.Quote(ctx, "FAKE", "NVDA")
	if err != nil {
		t.Fatal(err)
	}
	k, _ := a.Data["key"].(string)
	return k
}

func userProcs(m *Manager) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for id := range m.procs {
		if strings.Contains(id, "#u") {
			n++
		}
	}
	return n
}

func TestAUsersInstanceRunsWithTheirOwnKey(t *testing.T) {
	m, ctx := installedFake(t, "system-key", 0)
	if got := keyOf(t, m, ctx); got != "system-key" {
		t.Fatalf("system call: %q", got)
	}
	if got := keyOf(t, m, WithUserInstance(ctx, 7, map[string]string{"FAKE_API_KEY": "mine-7"})); got != "mine-7" {
		t.Errorf("user 7: %q", got)
	}
	if got := keyOf(t, m, WithUserInstance(ctx, 8, map[string]string{"FAKE_API_KEY": "mine-8"})); got != "mine-8" {
		t.Errorf("user 8 has a process of their own: %q", got)
	}
	if got := keyOf(t, m, ctx); got != "system-key" {
		t.Errorf("the shared process is unaffected: %q", got)
	}
	if got := keyOf(t, m, WithUserInstance(ctx, 7, map[string]string{"FAKE_API_KEY": "new-7"})); got != "new-7" {
		t.Errorf("a changed key restarts that user's process: %q", got)
	}
}

func TestAUsersInstanceNeverInheritsTheSystemSecret(t *testing.T) {
	m, ctx := installedFake(t, "system-key", 0)
	_, err := m.Quote(WithUserInstance(ctx, 7, map[string]string{}), "FAKE", "NVDA")
	var coded *CallError
	if !errors.As(err, &coded) || coded.Code != CodeNotConfigured {
		t.Errorf("without the user's own key the call fails, it does not borrow the system key: %v", err)
	}
}

func TestAUserCanSetOnlyDeclaredSecrets(t *testing.T) {
	t.Setenv("MYSQL_DSN", "") // the fake plugin exits when it sees a backend variable
	m, ctx := installedFake(t, "system-key", 0)
	values := map[string]string{"FAKE_API_KEY": "mine", "MYSQL_DSN": "leak", "PATH": "/evil"}
	if got := keyOf(t, m, WithUserInstance(ctx, 7, values)); got != "mine" {
		t.Errorf("%q", got)
	}
	if got := m.UserSecrets("fake"); len(got) != 1 || got[0].Name != "FAKE_API_KEY" {
		t.Errorf("only declared secret variables can be supplied: %+v", got)
	}
	if got := m.UserSecrets("nope"); len(got) != 0 {
		t.Errorf("%+v", got)
	}
}

func TestUserInstancesAreBoundedAndStopWithThePlugin(t *testing.T) {
	m, ctx := installedFake(t, "system-key", 2)
	for _, u := range []int64{1, 2, 3} {
		keyOf(t, m, WithUserInstance(ctx, u, map[string]string{"FAKE_API_KEY": "k"}))
	}
	if n := userProcs(m); n != 2 {
		t.Errorf("at most two user processes: %d", n)
	}
	if err := m.Uninstall("fake"); err != nil {
		t.Fatal(err)
	}
	if n := userProcs(m); n != 0 {
		t.Errorf("uninstalling stops every user process: %d", n)
	}
}
