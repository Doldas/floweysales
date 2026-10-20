package server

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"floweysales/internal/storage"
	"floweysales/internal/trading"
)

// ConfigureTrading stores the account settings used by the order routes and
// the match loop. It is called once at boot, before any tick is broadcast.
func (e *Engine) ConfigureTrading(cfg trading.Config) {
	e.tradingMu.Lock()
	defer e.tradingMu.Unlock()
	e.tradingCfg = cfg
}

// tradingConfig returns the current account settings.
func (e *Engine) tradingConfig() trading.Config {
	e.tradingMu.Lock()
	defer e.tradingMu.Unlock()
	return e.tradingCfg
}

// portfolioState loads every order plus the derived positions and summary.
func (e *Engine) portfolioState() (orders, working []storage.Order, positions []trading.Position, summary trading.Summary) {
	cfg := e.tradingConfig()
	orders = e.db.GetOrders("", "")
	working = e.db.GetOpenOrders("")

	marks := map[string]float64{}
	for _, symbol := range e.db.GetDistinctSymbols() {
		marks[symbol] = e.db.GetSymbolStats(symbol).LatestPrice
	}

	positions, summary = trading.BuildPositions(orders, working, marks, cfg)
	return orders, working, positions, summary
}

// matchOrders runs the execution engine against a fresh tick and persists
// anything that fills or triggers.
func (e *Engine) matchOrders(symbol string, price float64) {
	if e.tradingConfig().HaltNewOrders {
		return // Do not fill existing working orders while the kill switch is set.
	}
	if symbol == "" || price <= 0 {
		return
	}

	e.matchMu.Lock()
	defer e.matchMu.Unlock()

	working := e.db.GetOpenOrders(symbol)
	if len(working) == 0 {
		return
	}

	cfg := e.tradingConfig()
	filled, triggered := trading.Match(working, symbol, price, cfg.Commission, time.Now().Unix())

	for _, order := range filled {
		if e.db.FillOrder(order) {
			log.Printf("[API] Order #%d filled: %s %.8g %s @ %.8g (fee %.2f)",
				order.ID, order.Side, order.Quantity, order.Symbol, order.FillPrice, order.Fee)
		}
	}
	for _, order := range triggered {
		if e.db.SetOrderType(order.ID, order.Type, order.LimitPrice) {
			log.Printf("[API] Order #%d stop triggered, working as %s at %.8g",
				order.ID, order.Type, order.LimitPrice)
		}
	}
}

// registerOrderRoutes wires the order ticket, lifecycle and portfolio views.
func (e *Engine) registerOrderRoutes() {
	// --- SUBMIT ---

	e.mux.HandleFunc("POST /api/orders", func(w http.ResponseWriter, r *http.Request) {
		corsHeader(w)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeOrderError(w, http.StatusBadRequest, err.Error())
			return
		}

		var j map[string]any
		if err := jsonUnmarshal(body, &j); err != nil {
			writeOrderError(w, http.StatusBadRequest, err.Error())
			return
		}

		key := stringField(j, "client_order_id")
		digest := ""
		if key != "" {
			if len(key) > 128 || !regexp.MustCompile("^[A-Za-z0-9._:-]+$").MatchString(key) {
				writeOrderError(w, http.StatusBadRequest, "invalid client_order_id")
				return
			}
			canonical := make(map[string]any, len(j))
			for name, value := range j {
				if name != "client_order_id" {
					canonical[name] = value
				}
			}
			bytes, err := json.Marshal(canonical)
			if err != nil {
				writeOrderError(w, http.StatusBadRequest, "invalid order parameters")
				return
			}
			digest = fmt.Sprintf("%x", sha256.Sum256(bytes))
			existing, found, err := e.db.FindPaperOrderRequest(key, digest)
			if err != nil {
				writeOrderError(w, http.StatusConflict, err.Error())
				return
			}
			if found {
				writeJSON(w, http.StatusOK, map[string]any{"status": "success", "order_id": existing.ID, "order": existing, "replayed": true})
				return
			}
		}

		symbol := strings.ToUpper(strings.TrimSpace(stringField(j, "symbol")))
		now := time.Now().Unix()

		// Reference price: explicit override first, then the latest tick.
		reference := floatField(j, "price")
		if reference <= 0 && symbol != "" {
			reference = e.db.GetSymbolStats(symbol).LatestPrice
		}

		orders, _, _, summary := e.portfolioState()

		req := trading.SubmitRequest{
			Symbol:         symbol,
			Side:           stringField(j, "side"),
			Type:           stringFieldOr(j, "type", trading.TypeMarket),
			TIF:            stringFieldOr(j, "tif", trading.TIFGTC),
			Quantity:       floatField(j, "quantity"),
			LimitPrice:     floatField(j, "limit_price"),
			StopPrice:      floatField(j, "stop_price"),
			ReferencePrice: reference,
			HeldQuantity:   trading.HeldQuantity(orders, symbol),
			BuyingPower:    summary.BuyingPower,
			ClientID:       stringField(j, "client_order_id"),
			Now:            now,
			Cfg:            e.tradingConfig(),
		}
		// Legacy callers send the share count as `amount`.
		if req.Quantity <= 0 {
			req.Quantity = floatField(j, "amount")
		}

		order, err := trading.NewOrder(req)
		if err != nil {
			log.Printf("[API] Order rejected for %s: %v", symbol, err)
			writeOrderError(w, http.StatusBadRequest, err.Error())
			return
		}

		replayed := false
		if key != "" {
			order, replayed, err = e.db.CreatePaperOrderOnce(order, key, digest)
			if err != nil {
				writeOrderError(w, http.StatusConflict, err.Error())
				return
			}
		} else {
			order.ID = e.db.CreateOrder(order)
			if order.ID <= 0 {
				writeOrderError(w, http.StatusInternalServerError, "failed to store order")
				return
			}
		}
		id := order.ID
		if replayed {
			writeJSON(w, http.StatusOK, map[string]any{"status": "success", "order_id": id, "order": order, "replayed": true})
			return
		}

		if order.Status == trading.StatusFilled {
			log.Printf("[API] Order #%d executed: %s %.8g %s @ %.8g (fee %.2f)",
				order.ID, order.Side, order.Quantity, order.Symbol, order.FillPrice, order.Fee)
		} else {
			log.Printf("[API] Order #%d accepted (%s): %s %.8g %s limit %.8g stop %.8g",
				order.ID, order.Type, order.Side, order.Quantity, order.Symbol, order.LimitPrice, order.StopPrice)
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"status":   "success",
			"order_id": id,
			"order":    order,
			"summary":  summary,
		})
	})

	// --- QUERY ---

	e.mux.HandleFunc("GET /api/orders", func(w http.ResponseWriter, r *http.Request) {
		corsHeader(w)
		symbol := queryParam(r, "symbol", "")
		status := queryParam(r, "status", "")
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		writeJSON(w, http.StatusOK, e.db.GetOrdersPage(symbol, status, limit, offset))
	})

	// --- LIFECYCLE ---

	e.mux.HandleFunc("PATCH /api/orders", func(w http.ResponseWriter, r *http.Request) {
		corsHeader(w)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeOrderError(w, http.StatusBadRequest, err.Error())
			return
		}

		var j map[string]any
		if err := jsonUnmarshal(body, &j); err != nil {
			writeOrderError(w, http.StatusBadRequest, err.Error())
			return
		}

		orderID := int(floatField(j, "id"))
		newStatus := strings.ToUpper(strings.TrimSpace(stringField(j, "status")))
		reason := strings.TrimSpace(stringField(j, "reason"))

		order, ok := e.db.GetOrder(orderID)
		if !ok {
			writeOrderError(w, http.StatusNotFound, "order not found")
			return
		}
		if !trading.CanTransition(order.Status, newStatus) {
			writeOrderError(w, http.StatusConflict,
				"cannot move order #"+strconv.Itoa(orderID)+" from "+order.Status+" to "+newStatus)
			return
		}

		switch newStatus {
		case trading.StatusCancelled:
			if reason == "" {
				reason = "cancelled by user"
			}
		case trading.StatusRejected:
			if reason == "" {
				reason = "rejected"
			}
		case trading.StatusFilled:
			writeOrderError(w, http.StatusConflict, "manual fills are disabled; execution requires a market tick")
			return
		}

		if !e.db.UpdateOrderStatus(orderID, newStatus, reason) {
			writeOrderError(w, http.StatusConflict, "order is no longer open; concurrent transition rejected")
			return
		}
		log.Printf("[API] Order #%d %s (%s)", orderID, strings.ToLower(newStatus), reason)

		updated, _ := e.db.GetOrder(orderID)
		writeJSON(w, http.StatusOK, map[string]any{"status": "success", "order": updated})
	})

	// --- PORTFOLIO ---

	e.mux.HandleFunc("GET /api/positions", func(w http.ResponseWriter, r *http.Request) {
		corsHeader(w)
		_, _, positions, summary := e.portfolioState()
		writeJSON(w, http.StatusOK, map[string]any{
			"positions": positions,
			"summary":   summary,
		})
	})
}

// writeOrderError keeps the error shape consistent for the order ticket.
func writeOrderError(w http.ResponseWriter, code int, message string) {
	writeJSON(w, code, map[string]any{"status": "error", "error": message})
}
