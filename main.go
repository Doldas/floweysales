package main

import (
	"embed"
	"encoding/json"
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
	"floweysales/internal/trading"
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

	mysqlDSN:=os.Getenv("MYSQL_DSN")
 if mysqlDSN==""{mysqlDSN=os.Getenv("AUTH_MYSQL_DSN")}
 if mysqlDSN==""{log.Fatal("MYSQL_DSN (or AUTH_MYSQL_DSN) must be set; SQLite runtime storage has been removed")}

	portfolio := trading.DefaultConfig()
	portfolio.BaseCash = parseFloat(app.ResolveConfig("PORTFOLIO_CASH", "100000", defaults), portfolio.BaseCash)
	portfolio.Commission.Pct = parseFloat(app.ResolveConfig("TRADE_COMMISSION_PCT", "0", defaults), 0)
	portfolio.Commission.Fixed = parseFloat(app.ResolveConfig("TRADE_COMMISSION_FIXED", "0", defaults), 0)
	portfolio.AllowShort = strings.EqualFold(
		app.ResolveConfig("ALLOW_SHORT_SELLING", "false", defaults), "true")

	fmt.Println("==================================================")
	fmt.Println("         FLOWEYSALES ENGINE CONFIGURATION         ")
	fmt.Printf("EMA Periods:      Fast: %.1fs | Slow: %.1fs\n", cfg.FastPeriod, cfg.SlowPeriod)
	fmt.Printf("Ticker Interval:  %s\n", updateRate)
	fmt.Printf("Portfolio:        Cash %.0f | Commission %.2f%% + %.2f | Short %v\n",
		portfolio.BaseCash, portfolio.Commission.Pct, portfolio.Commission.Fixed, portfolio.AllowShort)
	fmt.Println("==================================================")

	api := market.NewMarketApi(app.ResolveConfig("FINNHUB_API_KEY", "", defaults))
	finnhubAPIKey := app.ResolveConfig("FINNHUB_API_KEY", "", defaults)

	db, err := storage.Open(mysqlDSN)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	defer db.Close()
 // Preserve legacy data: fail closed if an old SQLite file exists but the new
 // database has not received an explicit migration. Never auto-delete it.
 legacyPath:=os.Getenv("LEGACY_SQLITE_PATH")
 if legacyPath!=""{
  if fi,err:=os.Stat(legacyPath);err==nil && fi.Size()>0 && !db.HasAnyData(){
    log.Fatalf("Legacy SQLite file %s still contains potential data while MySQL trading tables are empty. Stop and run cmd/migrate-sqlite; file will not be deleted automatically.",legacyPath)
  }
 }

	restAPI := server.NewEngine(db, sharedConfig, wwwFS, finnhubAPIKey, cfg)
	if strings.EqualFold(app.ResolveConfig("AUTH_ENABLED", "false", defaults), "true") {
		// Auth credentials are deliberately environment-only, never committed properties.
		identityDB, err := identity.Open(mysqlDSN)
		if err != nil {
			log.Fatalf("[AUTH] %v", err)
		}
		defer identityDB.Close()
		email, password := os.Getenv("AUTH_BOOTSTRAP_EMAIL"), os.Getenv("AUTH_BOOTSTRAP_PASSWORD")
		if (email == "") != (password == "") {
			log.Fatal("[AUTH] Set both AUTH_BOOTSTRAP_EMAIL and AUTH_BOOTSTRAP_PASSWORD, or neither")
		}
		if email != "" {
			if err := identityDB.BootstrapOwner(context.Background(), email, password); err != nil {
				log.Fatalf("[AUTH] Cannot bootstrap owner: %v", err)
			}
		}
		exists, err := identityDB.OwnerExists(context.Background())
		if err != nil || !exists {
			log.Fatal("[AUTH] No owner exists; provide bootstrap credentials on first start")
		}
		secureCookie := !strings.EqualFold(app.ResolveConfig("AUTH_COOKIE_SECURE", "true", defaults), "false")
		restAPI.ConfigureAuth(identityDB, secureCookie)
		log.Printf("[AUTH] Owner authentication active (secure cookie=%v)", secureCookie)
	}

	restAPI.ConfigureAgentLimits(
		parseInt(app.ResolveConfig("AI_MAX_STEPS", "12", defaults), 12),
		parseInt(app.ResolveConfig("AI_MAX_TOOL_CALLS", "48", defaults), 48),
	)
	restAPI.ConfigureAI(
		app.ResolveConfig("OPENAI_BASE_URL", "", defaults),
		app.ResolveConfig("OPENAI_API_KEY", "", defaults),
		app.ResolveConfig("OPENAI_MODEL", "", defaults),
	)
	if err := restAPI.RestoreAISettings(); err != nil {
		log.Fatalf("[AUTH] Could not load AI configuration: %v", err)
	}
	restAPI.ConfigureTrading(portfolio)
	if err := restAPI.Start(restApiHost, restApiPort); err != nil {
		log.Fatalf("[FATAL ERROR] Could not bind to %s:%s - Port in use! (%v)", restApiHost, restApiPort, err)
	}
	db.SetOnRecordInserted(restAPI.BroadcastTick)

	notifier := notify.NewEmailNotifier(appMail, appMailPassword, receiveAlertsAt)

	// Active state to catch runtime configuration switches.
	activeSymbol, activeProvider := sharedConfig.Get()

	tkr := tracker.NewTracker(cfg)
	seedTrackerFromHistory(tkr, activeSymbol, activeProvider, finnhubAPIKey)
	tickCounter := 0
	lastEmailSentTime := time.Time{}
	hasSentFirstEmail := false
	previousEMARelation := 0

	initialPrice := api.GetLivePrice(activeSymbol, activeProvider)

	for {
		tickCounter++

		// 1. Reset trigger: check if the user swapped symbol/provider via REST API
		currentSymbol, currentProvider := sharedConfig.Get()
		if currentSymbol != activeSymbol || currentProvider != activeProvider {
			fmt.Printf("\n🔄 [CONFIG CHANGED] Asset swapped from %s (%s) to %s (%s)\n",
				activeSymbol, activeProvider, currentSymbol, currentProvider)

			activeSymbol = currentSymbol
			activeProvider = currentProvider

			// Clear trend memory completely to prevent ghost email triggers.
			previousEMARelation = 0
			tkr = tracker.NewTracker(cfg)
			seedTrackerFromHistory(tkr, activeSymbol, activeProvider, finnhubAPIKey)
			initialPrice = 0.0

			time.Sleep(updateRate)
			continue
		}

		liveAsset, err := api.FetchAsset(activeSymbol, activeProvider)
		if err != nil {
			log.Printf("[Tick] fetch failed for %s via %s: %v", activeSymbol, activeProvider, err)
			time.Sleep(time.Second)
			continue
		}

		currentPrice := liveAsset.Price()
		if currentPrice <= 0.0 {
			log.Printf("[Tick] Price not available yet: %s via %s", activeSymbol, activeProvider)
			time.Sleep(time.Second)
			continue
		}

		decision := tkr.Update(currentPrice)
		currentFastEMA, currentSlowEMA := tkr.EMAs()
		currentStop := tkr.StopLoss()
		timestamp := time.Now().Unix()

		visualStatus := "🟢 ACTIVE (Tracking)"

		// 2. Evaluate crossovers safely.
		if currentFastEMA > 0.0 && currentSlowEMA > 0.0 {
			currentEMARelation := 1
			if currentFastEMA > currentSlowEMA {
				currentEMARelation = 2
			}

			if previousEMARelation != 0 {
				currentTime := time.Now()
				cooldownActive := hasSentFirstEmail && currentTime.Sub(lastEmailSentTime) < emailCooldownDuration

				if previousEMARelation == 1 && currentEMARelation == 2 {
					visualStatus = "🔶 GOLDEN CROSS (Buy Momentum)"
					fmt.Printf("\n🔶 [TREND] Golden Cross detected for %s!\n", activeSymbol)

					if !cooldownActive {
						notifier.SendEmail(
							"🔶 GOLDEN CROSS: Bullish Trend Reversal for "+activeSymbol,
							fmt.Sprintf("Your trading bot has detected a Golden Cross for %s.\nFast EMA (%s:%f) crossed above Slow EMA (%s:%f).",
								activeSymbol, liveAsset.Currency(), currentFastEMA, liveAsset.Currency(), currentSlowEMA))
						lastEmailSentTime = currentTime
						hasSentFirstEmail = true
					}
				} else if previousEMARelation == 2 && currentEMARelation == 1 {
					visualStatus = "📉 DEATH CROSS (Bearish Reversal)"
					fmt.Printf("\n📉 [TREND] Death Cross detected for %s!\n", activeSymbol)

					if !cooldownActive {
						notifier.SendEmail(
							"📉 DEATH CROSS WARNING: Bearish Trend Reversal for "+activeSymbol,
							fmt.Sprintf("Your trading bot has detected a Death Cross for %s.\nFast EMA (%s:%f) fell below Slow EMA (%s:%f).",
								activeSymbol, liveAsset.Currency(), currentFastEMA, liveAsset.Currency(), currentSlowEMA))
						lastEmailSentTime = currentTime
						hasSentFirstEmail = true
					}
				}
			}
			previousEMARelation = currentEMARelation
		}

		record := storage.TradingRecord{
			Timestamp:     timestamp,
			Symbol:        activeSymbol,
			Price:         currentPrice,
			StopLoss:      currentStop,
			FastEMA:       currentFastEMA,
			SlowEMA:       currentSlowEMA,
			Currency:      liveAsset.Currency(),
			Status:        visualStatus,
			RawMarketJSON: mustJSON(liveAsset.ToJSON()),
		}

		// 3. Stop-loss triggered.
		if decision == tracker.Sell {
			actualExitPrice := tkr.StopLoss()
			visualStatus = "🔴 TERMINATED (Stop-Loss)"
			record.Status = visualStatus

			fmt.Printf("\n⚠️ [ALERT] Stop-Loss triggered for %s at %s:%f!\n",
				activeSymbol, liveAsset.Currency(), actualExitPrice)

			currentTime := time.Now()
			cooldownActive := hasSentFirstEmail && currentTime.Sub(lastEmailSentTime) < emailCooldownDuration

			if !cooldownActive {
				notifier.SendEmail(
					"⚠️ SELL ALERT: Stop-Loss Triggered for "+activeSymbol,
					fmt.Sprintf("Dynamic stop-loss breached.\n\nAsset: %s\nEntry Price: %s:%f\nExit Price: %s:%f",
						activeSymbol, liveAsset.Currency(), initialPrice, liveAsset.Currency(), actualExitPrice))
				lastEmailSentTime = currentTime
				hasSentFirstEmail = true
			}

			db.InsertRecord(record)

			// Reset execution step.
			fmt.Printf("🔄 Resetting tracker parameters for %s...\n\n", activeSymbol)
			tkr = tracker.NewTracker(cfg)
			initialPrice = currentPrice
			previousEMARelation = 0

			time.Sleep(updateRate)
			continue
		}

		// 4. Standard tick preservation.
		db.InsertRecord(record)

		time.Sleep(updateRate)
	}
}

// mustJSON marshals an asset payload; on failure an empty object is stored.
func mustJSON(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return raw
}

// seedTrackerFromHistory warms the tracker with recent market data (1M bars)
// so EMAs and the stop-loss start realistic instead of cold. Best-effort:
// when the provider has no history the tracker starts normally.
func seedTrackerFromHistory(tkr *tracker.Tracker, symbol string, provider market.Provider, finnhubKey string) {
	series, err := market.FetchHistory(symbol, provider, market.Range1M, finnhubKey)
	if err != nil {
		log.Printf("[SEED] No historical data for %s (%s): %v — cold start", symbol, provider, err)
		return
	}
	closes := series.Closes()
	if len(closes) < 2 {
		return
	}
	tkr.Seed(closes)
	fast, slow := tkr.EMAs()
	log.Printf("[SEED] Warmed tracker for %s from %d historical bars (FastEMA=%.4f SlowEMA=%.4f Stop=%.4f)",
		symbol, len(closes), fast, slow, tkr.StopLoss())
}
