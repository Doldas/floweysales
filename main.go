package main

import (
	"embed"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"context"
	"floweysales/internal/app"
	"floweysales/internal/identity"
	"floweysales/internal/market"
	"floweysales/internal/notify"
	"floweysales/internal/server"
	"floweysales/internal/storage"
	"floweysales/internal/tracker"
	"floweysales/internal/tracking"
	"floweysales/internal/trading"
	"os/signal"
	"syscall"
)

// The web dashboard is compiled into the binary so assets render regardless
// of the working directory the executable is launched from. The all: prefix
// includes the .gitkeep placeholder so the build succeeds after `make clean`
// without a fresh Angular build present.
//
//go:embed all:www
var wwwFS embed.FS

// parseFloat parses a float from a resolved config string.
func parseFloat(s string, fallback float64) float64 {
	var v float64
	if _, err := fmt.Sscanf(s, "%f", &v); err != nil {
		return fallback
	}
	return v
}

// parseInt parses an int from a resolved config string.
func parseInt(s string, fallback int) int {
	if s == "" {
		return fallback
	}
	v := 0
	neg := false
	for i, c := range s {
		switch {
		case c == '-' && i == 0:
			neg = true
		case c >= '0' && c <= '9':
			v = v*10 + int(c-'0')
		default:
			return fallback
		}
	}
	if neg {
		v = -v
	}
	return v
}

func main() {
	defaults := app.LoadProperties("config.properties")

	appMail := app.ResolveConfig("SMTP_APP_MAIL", "server@mail.nu", defaults)
	appMailPassword := app.ResolveConfig("SMTP_APP_MAIL_PSW", "xxxx xxxx xxxx xxxx", defaults)
	emailCooldownDuration := time.Duration(parseInt(
		app.ResolveConfig("EMAIL_COOLDOWN_DURATION_MIN", "10", defaults), 10)) * time.Minute
	receiveAlertsAt := app.ResolveConfig("DEFAULT_SMTP_MAIL_RECIPIENT", "user1@mail.nu", defaults)

	sharedConfig := &server.SharedMarketConfig{}
	sharedConfig.Symbol = app.ResolveConfig("DEFAULT_MARKET_SYMBOL", "GIB", defaults)
	sharedConfig.Provider = market.ParseProvider(
		app.ResolveConfig("DEFAULT_MARKET_PROVIDER", "YAHOO", defaults))

	restApiPort := app.ResolveConfig("REST_API_PORT", "8080", defaults)
	restApiHost := app.ResolveConfig("REST_API_HOST", "0.0.0.0", defaults)

	// The update rate is expressed in milliseconds (as in the C++ version),
	// but clamped to a sane minimum to avoid hammering the data providers.
	updateRateMs := parseInt(app.ResolveConfig("REST_API_UPDATE_RATE", "1000", defaults), 1000)
	if updateRateMs < 250 {
		log.Printf("[CONFIG] REST_API_UPDATE_RATE=%dms is below the safe minimum; clamping to 250ms.", updateRateMs)
		updateRateMs = 250
	}
	updateRate := time.Duration(updateRateMs) * time.Millisecond

	cfg := tracker.DefaultConfig()
	cfg.BaseOffset = parseFloat(app.ResolveConfig("DEFAULT_BASE_OFFSET", "0.02", defaults), cfg.BaseOffset)
	cfg.MinOffset = parseFloat(app.ResolveConfig("DEFAULT_MIN_OFFSET", "0.005", defaults), cfg.MinOffset)
	cfg.MaxOffset = parseFloat(app.ResolveConfig("DEFAULT_MAX_OFFSET", "0.05", defaults), cfg.MaxOffset)
	cfg.FastPeriod = parseFloat(app.ResolveConfig("DEFAULT_FAST_PERIOD", "12.0", defaults), cfg.FastPeriod)
	cfg.SlowPeriod = parseFloat(app.ResolveConfig("DEFAULT_SLOW_PERIOD", "26.0", defaults), cfg.SlowPeriod)
	cfg.WarmupTicks = parseInt(app.ResolveConfig("DEFAULT_WARMUP_TICKS", "10", defaults), cfg.WarmupTicks)
	cfg.TransitionTicks = parseInt(app.ResolveConfig("DEFAULT_TRANSITION_TICKS", "30", defaults), cfg.TransitionTicks)

	portfolio := trading.DefaultConfig()
	portfolio.BaseCash = parseFloat(app.ResolveConfig("PORTFOLIO_CASH", "100000", defaults), portfolio.BaseCash)
	portfolio.Commission.Pct = parseFloat(app.ResolveConfig("TRADE_COMMISSION_PCT", "0", defaults), 0)
	portfolio.Commission.Fixed = parseFloat(app.ResolveConfig("TRADE_COMMISSION_FIXED", "0", defaults), 0)
	portfolio.AllowShort = strings.EqualFold(
		app.ResolveConfig("ALLOW_SHORT_SELLING", "false", defaults), "true")

	// MySQL is the ONLY live data store; absent/unreachable DB is fatal.
	mysqlDSN := app.ResolveConfig("MYSQL_DSN", os.Getenv("AUTH_MYSQL_DSN"), defaults)
	if mysqlDSN == "" {
		log.Fatal("[DB] MYSQL_DSN (or AUTH_MYSQL_DSN) is required. Run Liquibase before startup.")
	}
	db, err := storage.Open(mysqlDSN)
	if err != nil {
		log.Fatalf("[DB] %v", err)
	}
	defer db.Close()

	restAPI := server.NewEngine(db, sharedConfig, wwwFS,
		app.ResolveConfig("FINNHUB_API_KEY", "", defaults), cfg)
	if strings.EqualFold(app.ResolveConfig("AUTH_ENABLED", "false", defaults), "true") {
		identityDB, err := identity.Open(mysqlDSN)
		if err != nil {
			log.Fatalf("[AUTH] %v", err)
		}
		defer identityDB.Close()
		email, password := os.Getenv("AUTH_BOOTSTRAP_EMAIL"), os.Getenv("AUTH_BOOTSTRAP_PASSWORD")
		if (email == "") != (password == "") {
			log.Fatal("[AUTH] Set both bootstrap credentials or neither")
		}
		if email != "" {
			if err := identityDB.BootstrapOwner(context.Background(), email, password); err != nil {
				log.Fatalf("[AUTH] Owner bootstrap failed: %v", err)
			}
		}
		exists, err := identityDB.OwnerExists(context.Background())
		if err != nil || !exists {
			log.Fatal("[AUTH] No owner exists; bootstrap credentials required for first start")
		}
		secureCookie := !strings.EqualFold(app.ResolveConfig("AUTH_COOKIE_SECURE", "true", defaults), "false")
		restAPI.ConfigureAuth(identityDB, secureCookie)
	}
	restAPI.ConfigureAgentLimits(
		parseInt(app.ResolveConfig("AI_MAX_STEPS", "12", defaults), 12),
		parseInt(app.ResolveConfig("AI_MAX_TOOL_CALLS", "48", defaults), 48))
	restAPI.ConfigureAI(
		app.ResolveConfig("OPENAI_BASE_URL", "", defaults),
		app.ResolveConfig("OPENAI_API_KEY", "", defaults),
		app.ResolveConfig("OPENAI_MODEL", "", defaults))
	if err := restAPI.RestoreAISettings(); err != nil {
		log.Fatalf("[AI] Restore settings: %v", err)
	}
	restAPI.ConfigureTrading(portfolio)
	api := market.NewMarketApi(app.ResolveConfig("FINNHUB_API_KEY", "", defaults))
	engine, err := tracking.NewManager(db, api, cfg, sharedConfig.Symbol, sharedConfig.Provider)
	if err != nil {
		log.Fatalf("[TRACKERS] Cannot restore tracked assets: %v", err)
	}
	restAPI.SetTrackerManager(engine)
	db.SetOnRecordInserted(restAPI.BroadcastTick)
	notifier := notify.NewEmailNotifier(appMail, appMailPassword, receiveAlertsAt)
	engine.Alerts(func(subject, body string) { notifier.SendEmail(subject, body) }, emailCooldownDuration)
	if err := restAPI.Start(restApiHost, restApiPort); err != nil {
		log.Fatalf("[API] Bind: %v", err)
	}
	defer restAPI.Stop()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Exactly one poll loop, regardless of tracker count. No per-tick logs.
	engine.Run(ctx, updateRate)
}
