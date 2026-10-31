package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"floweysales/internal/market"
	"floweysales/internal/server"
	"floweysales/internal/storage"
	"floweysales/internal/tracker"
)

// Tracker loops are independently cancellable; no shared mutable EMA state,
// no per-tick console logging, and a finite maximum of 16 workers.
func runActiveTrackers(ctx context.Context, config *server.SharedMarketConfig, db *storage.Database,
	providers market.Backend, dispatch func(context.Context, string, string, string, string, string), cfg tracker.Config,
	requestedInterval, cooldown time.Duration) {
	_ = cooldown // Legacy global cooldown is superseded by per-user preferences.
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	workers := map[string]context.CancelFunc{}
	var wg sync.WaitGroup
	defer func() {
		for _, cancel := range workers {
			cancel()
		}
		wg.Wait()
	}()
	reconcile := func() {
		desired := map[string]server.MarketTarget{}
		for _, item := range config.Targets() {
			key := item.ProviderID + "|" + item.Symbol
			desired[key] = item
		}
		for key, cancel := range workers {
			if _, exists := desired[key]; !exists {
				cancel()
				delete(workers, key)
			}
		}
		for key, item := range desired {
			if _, exists := workers[key]; exists {
				continue
			}
			workerCtx, cancel := context.WithCancel(ctx)
			workers[key] = cancel
			wg.Add(1)
			go func(t server.MarketTarget) {
				defer wg.Done()
				runOneTracker(workerCtx, t, db, providers, dispatch, cfg, requestedInterval, cooldown)
			}(item)
		}
	}
	reconcile()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reconcile()
		}
	}
}

// rateForProvider respects the plugin's declared minimum poll interval, so
// tracking many assets cannot exhaust a provider's quota. Providers that are
// not installed are rechecked at a relaxed pace until their plugin returns.
func rateForProvider(requested time.Duration, providers market.Backend, provider market.Provider) time.Duration {
	min := 5 * time.Second
	if c, ok := market.Capability(providers, provider); !ok {
		min = 30 * time.Second
	} else if c.MinPollSeconds > 0 {
		min = time.Duration(c.MinPollSeconds) * time.Second
	}
	if requested < min {
		return min
	}
	return requested
}
func runOneTracker(ctx context.Context, target server.MarketTarget, db *storage.Database, providers market.Backend,
	dispatch func(context.Context, string, string, string, string, string), cfg tracker.Config, requestedInterval, cooldown time.Duration) {
	t := tracker.NewTracker(cfg)
	seeded := seedTrackerFromHistory(ctx, t, providers, target.Symbol, target.Provider)
	interval := rateForProvider(requestedInterval, providers, target.Provider)
	intervalTicker := time.NewTicker(interval)
	defer intervalTicker.Stop()
	var firstPrice float64
	var previousRelation int
	// Immediate first poll, then fixed rate. Errors are deliberately not logged per
	// tick; the provider status and previous successful tick remain queryable.
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		// A provider installed, updated or removed while running is picked up here.
		if next := rateForProvider(requestedInterval, providers, target.Provider); next != interval {
			interval = next
			intervalTicker.Reset(interval)
		}
		asset, err := providers.Quote(ctx, target.Provider, target.Symbol)
		if err == nil && asset.Price > 0 {
			if !seeded && firstPrice <= 0 {
				// The provider was not available at start (plugin installed later).
				seeded = seedTrackerFromHistory(ctx, t, providers, target.Symbol, target.Provider)
			}
			price := asset.Price
			if firstPrice <= 0 {
				firstPrice = price
			}
			decision := t.Update(price)
			fast, slow := t.EMAs()
			state := "ACTIVE"
			if fast > 0 && slow > 0 {
				relation := 1
				if fast > slow {
					relation = 2
				}
				if previousRelation != 0 && previousRelation != relation {
					if relation == 2 {
						state = "GOLDEN_CROSS"
					} else {
						state = "DEATH_CROSS"
					}
					if dispatch != nil {
						label := "GOLDEN CROSS"
						if relation == 1 {
							label = "DEATH CROSS"
						}
						dispatch(ctx, target.Symbol, target.ProviderID, state, label+": "+target.Symbol, fmt.Sprintf("%s for %s. Fast EMA %.4f Slow EMA %.4f",
							label, target.Symbol, fast, slow))
					}
				}
				previousRelation = relation
			}
			if decision == tracker.Sell {
				state = "STOP_LOSS"
				if dispatch != nil {
					dispatch(ctx, target.Symbol, target.ProviderID, "STOP_LOSS", "STOP LOSS: "+target.Symbol, fmt.Sprintf("Stop triggered for %s at %.4f (initial %.4f)", target.Symbol, t.StopLoss(), firstPrice))
				}
			}
			record := storage.TradingRecord{
				Timestamp: time.Now().Unix(), Symbol: target.Symbol, Provider: target.ProviderID, Price: price,
				StopLoss: t.StopLoss(), FastEMA: fast, SlowEMA: slow,
				Currency: asset.Currency, Status: state,
				RawMarketJSON: rawMarketJSON(asset),
			}
			db.InsertRecord(record)
			if decision == tracker.Sell {
				t = tracker.NewTracker(cfg)
				firstPrice = price
				previousRelation = 0
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-intervalTicker.C:
		}
	}
}

// rawMarketJSON is the provider payload stored with a tick ("{}" when none).
func rawMarketJSON(asset market.Asset) []byte {
	if asset.Data == nil {
		return []byte("{}")
	}
	return mustJSON(asset.Data)
}
