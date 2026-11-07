package server

import (
	"context"
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"

	"floweysales/internal/storage"
	"floweysales/internal/tracker"
	"floweysales/internal/websearch"
)

// Provider settings, the legacy Brave/Tavily row and the usage ledger on the
// real storage. It needs the disposable Liquibase-migrated MySQL CI starts.
func TestWebSearchSettingsAndLedgerOnMySQL(t *testing.T) {
	dsn := os.Getenv("STORAGE_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("requires disposable Liquibase-migrated MySQL")
	}
	t.Setenv(apiSecretEnv, base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	db, err := storage.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	for _, kind := range []string{"web_search", "ws_policy", "ws_searxng", "ws_brave", "ws_tavily", "ws_google_grounding", "ws_google_cse"} {
		_ = db.DeleteAPIIntegrationSecret(ctx, kind)
	}

	// An install from before this feature: one legacy Brave key.
	sealed, err := sealAPISecret("web_search", "legacy-brave-key")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveAPIIntegrationSecret(ctx, "web_search", "brave", sealed); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(db, &SharedMarketConfig{}, nil, tracker.DefaultConfig())
	if err := e.restoreWebSearch(ctx); err != nil {
		t.Fatal(err)
	}
	cfg, ok := e.webSearchRouter().Config(websearch.Brave)
	if !ok || !cfg.Enabled || cfg.APIKey != "legacy-brave-key" || e.webSearchRouter().Policy().Order[0] != websearch.Brave {
		t.Fatalf("the legacy key must carry over as the first enabled provider: %+v", cfg)
	}
	if p, has := e.legacyWebSearchInfo(); p != "brave" || !has {
		t.Fatalf("legacy view: %q %v", p, has)
	}

	// New settings survive a restart, encrypted.
	if err := e.saveWebSearchConfig(ctx, websearch.ProviderConfig{ID: websearch.GoogleGrounding, Enabled: true, APIKey: "g-key",
		PaidApproved: true, MonthlyBudgetUSD: 25, Model: "gemini-flash-latest"}); err != nil {
		t.Fatal(err)
	}
	raw, err := db.GetAPIIntegrationSecret(ctx, "ws_google_grounding")
	if err != nil || strings.Contains(string(raw.Ciphertext), "g-key") {
		t.Fatalf("stored settings must be ciphertext: %v", err)
	}
	policy := websearch.Policy{Mode: websearch.ModeManual, Selected: websearch.SearXNG}
	if err := policy.Normalize(); err != nil {
		t.Fatal(err)
	}
	sealedPolicy, _ := sealAPISecret("ws_policy", `{"mode":"manual","selected":"searxng","max_attempts":2}`)
	if err := db.SaveAPIIntegrationSecret(ctx, "ws_policy", "policy", sealedPolicy); err != nil {
		t.Fatal(err)
	}
	restarted := NewEngine(db, &SharedMarketConfig{}, nil, tracker.DefaultConfig())
	if err := restarted.restoreWebSearch(ctx); err != nil {
		t.Fatal(err)
	}
	g, ok := restarted.webSearchRouter().Config(websearch.GoogleGrounding)
	if !ok || g.APIKey != "g-key" || !g.PaidApproved || g.MonthlyBudgetUSD != 25 {
		t.Fatalf("restored: %+v", g)
	}
	if p := restarted.webSearchRouter().Policy(); p.Mode != websearch.ModeManual || p.Selected != websearch.SearXNG || p.MaxAttempts != 2 {
		t.Fatalf("policy: %+v", p)
	}

	// Removing the legacy credential removes both rows.
	if err := restarted.deleteLegacyWebSearch(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetAPIIntegrationSecret(ctx, "web_search"); !storage.IsMissingAPIIntegration(err) {
		t.Fatalf("legacy row remains: %v", err)
	}

	// Ledger: sums by UTC day and month.
	led := dbLedger{db: db}
	day := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		if err := led.Add(ctx, "test_ledger", day, websearch.Totals{Searches: 3, PromptTokens: 100, OutputTokens: 50, CostMicros: 42_000}); err != nil {
			t.Fatal(err)
		}
	}
	if err := led.Add(ctx, "test_ledger", day.AddDate(0, 0, 1), websearch.Totals{Searches: 1, Failures: 1}); err != nil {
		t.Fatal(err)
	}
	d, err := led.Sum(ctx, "test_ledger", day, day)
	if err != nil || d.Searches != 6 || d.PromptTokens != 200 || d.CostMicros != 84_000 {
		t.Fatalf("day: %+v %v", d, err)
	}
	m, err := led.Sum(ctx, "test_ledger", time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC))
	if err != nil || m.Searches != 7 || m.Failures != 1 {
		t.Fatalf("month: %+v %v", m, err)
	}
}
