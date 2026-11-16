package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"floweysales/internal/brokers"
	"floweysales/internal/identity"
	"floweysales/internal/plugins"
	"floweysales/internal/provenance"
)

// The broker registry (#444). Broker extensions and trading hosts are
// plugins (internal/plugins/trading.go); this file is the only place that
// calls them. Rules it enforces, in this order, for every order:
//
//   - the route is explicit: broker, connection and account are named, never
//     defaulted, and nothing falls back to another broker;
//   - the account the broker reports must be a paper account, and so must
//     the order that comes back (brokers.CheckEnvironment);
//   - the kill switch and the order-value limit of the trading settings apply;
//   - the order is reserved in the ledger before the plugin is called, so a
//     repeated client order id never places a second order.
//
// Live-money execution is refused by the constant brokers.LiveExecutionApproved,
// which no setting changes. The AI agent has no tool for any of this.

// brokerHost is the part of the plugin manager the registry uses; tests
// replace it.
type brokerHost interface {
	TradingProviders(providerType string) []plugins.TradingProvider
	CallTrading(ctx context.Context, providerID, operation string, input map[string]any) (json.RawMessage, error)
}

const (
	brokerPermission     = "trading.paper"
	brokerBodyLimit      = 64 * 1024
	maxBrokerConnections = 10
	maxConnectionValue   = 4096
	connectionsKindFmt   = "broker_conns:%d"
	connectionKindFmt    = "broker_conn:%d:%s"
)

var connectionNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,39}$`)

// isBrokerRoute matches the routes of the broker registry. A USER needs the
// trading.paper permission; no role has it unless an administrator grants it.
func isBrokerRoute(path string) bool {
	return path == "/api/brokers" || path == "/api/broker-accounts" || path == "/api/broker-positions" ||
		path == "/api/trading/intents" || strings.HasPrefix(path, "/api/broker-connections") || strings.HasPrefix(path, "/api/broker-orders")
}

func (e *Engine) brokerHost() brokerHost {
	if e.brokerHostOverride != nil {
		return e.brokerHostOverride
	}
	if e.plugins != nil {
		return e.plugins
	}
	return nil
}

func (e *Engine) brokerLedgerStore() brokers.Store {
	if e.brokerLedger != nil {
		return e.brokerLedger
	}
	if e.db != nil {
		return e.db
	}
	return nil
}

// brokerUser is the signed-in caller. Without accounts the one user is a
// SYS_ADMIN with id 0, who can only use the system's own broker settings.
func (e *Engine) brokerUser(w http.ResponseWriter, r *http.Request) (identity.User, bool) {
	u, ok := e.planUser(w, r)
	if !ok {
		return u, false
	}
	if u.Role != identity.RoleSysAdmin && e.authService != nil && !e.authService.HasPermission(r.Context(), u.Role, brokerPermission) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "Paper trading through brokers is not enabled for your role"})
		return u, false
	}
	return u, true
}

// --- connections -----------------------------------------------------------

// brokerConnection is a user's saved connection to a broker extension: a name
// and the values of the plugin's secret variables, sealed. The values are
// never shown again.
type brokerConnection struct {
	Name   string   `json:"name"`
	Broker string   `json:"broker"`
	Label  string   `json:"label"`
	Saved  []string `json:"saved"`
}

func (e *Engine) loadConnections(ctx context.Context, userID int64) []brokerConnection {
	var list []brokerConnection
	e.loadSealedJSON(ctx, fmt.Sprintf(connectionsKindFmt, userID), &list)
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}

func (e *Engine) loadConnectionValues(ctx context.Context, userID int64, name string) map[string]string {
	values := map[string]string{}
	e.loadSealedJSON(ctx, fmt.Sprintf(connectionKindFmt, userID, name), &values)
	return values
}

func (e *Engine) findBrokerProvider(broker string) (plugins.TradingProvider, bool) {
	host := e.brokerHost()
	if host == nil {
		return plugins.TradingProvider{}, false
	}
	for _, p := range host.TradingProviders(plugins.TypeBroker) {
		if strings.EqualFold(p.ProviderID, broker) {
			return p, true
		}
	}
	return plugins.TradingProvider{}, false
}

// brokerProblems says why an installed broker cannot be used, in plain words.
func brokerProblems(p plugins.TradingProvider) []string {
	var problems []string
	if p.Problem != "" {
		problems = append(problems, p.Problem)
	}
	if p.Broker == nil {
		return append(problems, "The plugin does not describe itself as a broker.")
	}
	return append(problems, brokers.Negotiate(p.Contract, p.Broker.Environments, p.Capabilities)...)
}

var (
	errConnectionNotFound = errors.New("connection not found")
	errNoSecretStore      = errors.New("saving broker connections needs the database and the secrets key")
)

// saveConnection creates or updates a connection. An empty value removes that
// variable; a connection must end up with every required variable.
func (e *Engine) saveConnection(ctx context.Context, userID int64, name, broker, label string, values map[string]string) error {
	if userID <= 0 {
		return errors.New("broker connections need a signed-in account")
	}
	if !connectionNamePattern.MatchString(name) {
		return errors.New("name the connection with 2-40 characters: lower-case letters, digits and hyphens, starting with a letter")
	}
	provider, ok := e.findBrokerProvider(broker)
	if !ok {
		return fmt.Errorf("%w: broker %q is not installed", brokers.ErrRoute, broker)
	}
	if problems := brokerProblems(provider); len(problems) > 0 {
		return fmt.Errorf("%w: %s", brokers.ErrRoute, problems[0])
	}
	label = strings.TrimSpace(label)
	if len(label) > 80 {
		return errors.New("label is at most 80 characters")
	}
	declared := map[string]plugins.EnvVar{}
	for _, v := range provider.ConnectionVars {
		declared[v.Name] = v
	}
	conns := e.loadConnections(ctx, userID)
	index := -1
	for i, c := range conns {
		if c.Name == name {
			index = i
		}
	}
	if index >= 0 && !strings.EqualFold(conns[index].Broker, provider.ProviderID) {
		return errors.New("a connection cannot change its broker; remove it and create another")
	}
	if index < 0 && len(conns) >= maxBrokerConnections {
		return fmt.Errorf("at most %d connections per user", maxBrokerConnections)
	}
	saved := map[string]string{}
	if index >= 0 {
		saved = e.loadConnectionValues(ctx, userID, name)
	}
	for key, value := range values {
		if _, ok := declared[key]; !ok {
			return fmt.Errorf("%s is not a value this broker declares", key)
		}
		value = strings.TrimSpace(value)
		if len(value) > maxConnectionValue || strings.ContainsAny(value, "\r\n\x00") {
			return fmt.Errorf("%s must be one line of at most %d characters", key, maxConnectionValue)
		}
		if value == "" {
			delete(saved, key)
		} else {
			saved[key] = value
		}
	}
	for _, v := range provider.ConnectionVars {
		if v.Required && saved[v.Name] == "" {
			return fmt.Errorf("%s is required", v.Name)
		}
	}
	meta := brokerConnection{Name: name, Broker: provider.ProviderID, Label: label, Saved: []string{}}
	for key := range saved {
		meta.Saved = append(meta.Saved, key)
	}
	sort.Strings(meta.Saved)
	if index >= 0 {
		conns[index] = meta
	} else {
		conns = append(conns, meta)
	}
	if e.routingStore() == nil {
		return errNoSecretStore
	}
	if err := e.saveSealedJSON(ctx, fmt.Sprintf(connectionKindFmt, userID, name), "broker:"+provider.ProviderID, saved); err != nil {
		return err
	}
	return e.saveSealedJSON(ctx, fmt.Sprintf(connectionsKindFmt, userID), "broker-connections", conns)
}

func (e *Engine) deleteConnection(ctx context.Context, userID int64, name string) error {
	conns := e.loadConnections(ctx, userID)
	kept := conns[:0]
	found := false
	for _, c := range conns {
		if c.Name == name {
			found = true
			continue
		}
		kept = append(kept, c)
	}
	if !found {
		return errConnectionNotFound
	}
	store := e.routingStore()
	if store == nil {
		return errNoSecretStore
	}
	if err := store.DeleteAPIIntegrationSecret(ctx, fmt.Sprintf(connectionKindFmt, userID, name)); err != nil {
		return err
	}
	if len(kept) == 0 {
		return store.DeleteAPIIntegrationSecret(ctx, fmt.Sprintf(connectionsKindFmt, userID))
	}
	return e.saveSealedJSON(ctx, fmt.Sprintf(connectionsKindFmt, userID), "broker-connections", kept)
}

// --- routing ---------------------------------------------------------------

// brokerRoute is a resolved broker and connection: the context to call it
// with and the declaration it was negotiated against.
type brokerRoute struct {
	ctx        context.Context
	provider   plugins.TradingProvider
	connection string
}

// resolveBroker finds the broker and the connection a request names. Without
// a connection the system's own settings are used, which only a SYS_ADMIN
// may do; a USER always goes through a connection of their own, so their
// values never mix with another user's.
func (e *Engine) resolveBroker(ctx context.Context, u identity.User, broker, connection string) (brokerRoute, error) {
	return e.resolveBrokerFor(ctx, u.ID, u.Role == identity.RoleSysAdmin, broker, connection)
}

// resolveBrokerFor is resolveBroker for a user known by id: the background
// reconciler has no session, only the ledger row of an order that was allowed
// when it was routed. system says whether the system's own settings may be used.
func (e *Engine) resolveBrokerFor(ctx context.Context, userID int64, system bool, broker, connection string) (brokerRoute, error) {
	provider, ok := e.findBrokerProvider(broker)
	if !ok {
		return brokerRoute{}, fmt.Errorf("%w: broker %q is not installed", errBrokerUnknown, broker)
	}
	if problems := brokerProblems(provider); len(problems) > 0 {
		return brokerRoute{}, fmt.Errorf("%w: %s", brokers.ErrRoute, problems[0])
	}
	route := brokerRoute{ctx: ctx, provider: provider, connection: connection}
	if connection == "" {
		if !system {
			return brokerRoute{}, fmt.Errorf("%w: name one of your connections; only an administrator can use the system's own settings", brokers.ErrRoute)
		}
		return route, nil
	}
	if userID <= 0 {
		return brokerRoute{}, fmt.Errorf("%w: connections need a signed-in account", brokers.ErrRoute)
	}
	for _, c := range e.loadConnections(ctx, userID) {
		if c.Name == connection {
			if !strings.EqualFold(c.Broker, provider.ProviderID) {
				return brokerRoute{}, fmt.Errorf("%w: connection %q is for %s, not %s", brokers.ErrRoute, connection, c.Broker, provider.ProviderID)
			}
			route.ctx = plugins.WithUserInstanceNamed(ctx, userID, connection, e.loadConnectionValues(ctx, userID, connection))
			return route, nil
		}
	}
	return brokerRoute{}, fmt.Errorf("%w: %q", errConnectionNotFound, connection)
}

var errBrokerUnknown = errors.New("unknown broker")

func (rt brokerRoute) supports(capability string) bool {
	for _, c := range rt.provider.Capabilities {
		if c == capability {
			return true
		}
	}
	return false
}

func (e *Engine) callBroker(rt brokerRoute, capability string, input map[string]any) (json.RawMessage, error) {
	if !rt.supports(capability) {
		return nil, fmt.Errorf("%w: %s does not offer %s", brokers.ErrRoute, rt.provider.ProviderID, capability)
	}
	return e.brokerHost().CallTrading(rt.ctx, rt.provider.ProviderID, capability, input)
}

func (e *Engine) brokerAccounts(rt brokerRoute) ([]brokers.Account, error) {
	raw, err := e.callBroker(rt, plugins.CapBrokerAccounts, nil)
	if err != nil {
		return nil, err
	}
	return brokers.DecodeAccounts(raw)
}

// paperAccount is the paper gate for an account: the account must exist at
// the broker and the broker itself must say it is a paper account.
func (e *Engine) brokerPaperAccount(rt brokerRoute, id string) (brokers.Account, error) {
	accounts, err := e.brokerAccounts(rt)
	if err != nil {
		return brokers.Account{}, err
	}
	for _, a := range accounts {
		if a.ID != id {
			continue
		}
		if err := brokers.CheckEnvironment(a.Environment); err != nil {
			return brokers.Account{}, fmt.Errorf("%w (account %s is a %s account)", err, a.ID, a.Environment)
		}
		return a, nil
	}
	return brokers.Account{}, fmt.Errorf("%w: %s has no account %q on this connection", brokers.ErrRoute, rt.provider.ProviderID, id)
}

// brokerErrorStatus maps registry errors to HTTP statuses. Plugin errors keep
// their message (which never holds the input) and anything else is generic.
func brokerErrorStatus(err error) (int, string) {
	var coded *plugins.CallError
	switch {
	case errors.Is(err, brokers.ErrLiveRefused):
		return http.StatusForbidden, err.Error()
	case errors.Is(err, brokers.ErrInvalid), errors.Is(err, plugins.ErrDomainInput):
		return http.StatusBadRequest, strings.TrimPrefix(err.Error(), plugins.ErrDomainInput.Error()+": ")
	case errors.Is(err, errBrokerUnknown), errors.Is(err, errConnectionNotFound), errors.Is(err, brokers.ErrNotFound), errors.Is(err, plugins.ErrNoSuchDomain):
		return http.StatusNotFound, err.Error()
	case errors.Is(err, brokers.ErrRoute), errors.Is(err, brokers.ErrRisk):
		return http.StatusUnprocessableEntity, err.Error()
	case errors.Is(err, brokers.ErrConflict), errors.Is(err, brokers.ErrInFlight):
		return http.StatusConflict, err.Error()
	case errors.Is(err, brokers.ErrBrokerAnswer):
		return http.StatusBadGateway, err.Error()
	case errors.As(err, &coded) && coded.Code == plugins.CodeNotConfigured:
		return http.StatusServiceUnavailable, coded.Message
	case errors.As(err, &coded) && coded.Code == plugins.CodeInvalidArgument:
		return http.StatusBadRequest, coded.Message
	case errors.As(err, &coded):
		return http.StatusBadGateway, coded.Message
	case errors.Is(err, errNoSecretStore):
		return http.StatusServiceUnavailable, err.Error()
	}
	log.Printf("[BROKER] %v", err)
	return http.StatusBadGateway, "The broker did not answer"
}

func writeBrokerError(w http.ResponseWriter, err error) {
	status, msg := brokerErrorStatus(err)
	writeJSON(w, status, map[string]any{"status": "error", "error": msg})
}

// --- orders ----------------------------------------------------------------

func (e *Engine) brokerLimits() brokers.Limits {
	cfg := e.tradingConfig()
	return brokers.Limits{Halted: cfg.HaltNewOrders, MaxOrderNotional: cfg.MaxOrderNotional}
}

// submitBrokerOrder routes one order. The returned record is the ledger row.
func (e *Engine) submitBrokerOrder(ctx context.Context, u identity.User, req brokers.OrderRequest) (brokers.Record, bool, error) {
	store := e.brokerLedgerStore()
	if store == nil {
		return brokers.Record{}, false, errors.New("the order ledger is unavailable")
	}
	if err := req.Normalize(); err != nil {
		return brokers.Record{}, false, err
	}
	if req.Broker == brokers.BuiltinBroker {
		return brokers.Record{}, false, fmt.Errorf("%w: the built-in paper account takes orders at /api/orders, which keeps its approvals and audit trail", brokers.ErrRoute)
	}
	rt, err := e.resolveBroker(ctx, u, req.Broker, req.Connection)
	if err != nil {
		return brokers.Record{}, false, err
	}
	if !rt.supports(plugins.CapBrokerSubmitOrder) {
		return brokers.Record{}, false, fmt.Errorf("%w: %s does not take orders", brokers.ErrRoute, rt.provider.ProviderID)
	}
	if info := rt.provider.Broker; !contains(info.OrderTypes, req.Type) || !contains(info.TimeInForce, req.TIF) {
		return brokers.Record{}, false, fmt.Errorf("%w: %s accepts order types %s and time in force %s", brokers.ErrRoute, rt.provider.ProviderID,
			strings.Join(info.OrderTypes, ", "), strings.Join(info.TimeInForce, ", "))
	}
	account, err := e.brokerPaperAccount(rt, req.Account)
	if err != nil {
		return brokers.Record{}, false, err
	}
	if err := brokers.CheckRisk(req, account, e.brokerLimits()); err != nil {
		return brokers.Record{}, false, err
	}

	rec, existing, err := store.ReserveBrokerOrder(ctx, brokers.Record{UserID: u.ID, Connection: req.Connection, Broker: req.Broker, Account: req.Account,
		Environment: account.Environment, ClientOrderID: req.ClientOrderID, Digest: req.Digest(), Request: req})
	if err != nil {
		return brokers.Record{}, false, err
	}
	if existing {
		switch rec.Status {
		case brokers.StatusSubmitted, brokers.StatusFailed:
			return rec, true, nil // The same order again is answered from the ledger.
		}
		claimed, err := store.ClaimBrokerOrder(ctx, u.ID, rec.ID, time.Now().Add(-brokers.PendingStaleAfter))
		if err != nil {
			return brokers.Record{}, false, err
		}
		if !claimed {
			return brokers.Record{}, false, brokers.ErrInFlight
		}
	}

	raw, err := e.callBroker(rt, plugins.CapBrokerSubmitOrder, req.Input(account.Environment))
	if err != nil {
		var coded *plugins.CallError
		if errors.As(err, &coded) && coded.Code != plugins.CodeInternal {
			// The plugin answered that it did not place the order.
			_ = store.FinishBrokerOrder(ctx, u.ID, rec.ID, brokers.StatusFailed, nil, coded.Message)
			return brokers.Record{}, false, err
		}
		// No clear answer: the order may exist. The ledger says so, and sending
		// the same client order id again is safe.
		_ = store.FinishBrokerOrder(ctx, u.ID, rec.ID, brokers.StatusUnknown, nil, "no answer from the broker")
		return brokers.Record{}, false, fmt.Errorf("%w: %v", errOutcomeUnknown, err)
	}
	order, err := brokers.DecodeSubmitted(raw, req)
	if err != nil {
		_ = store.FinishBrokerOrder(ctx, u.ID, rec.ID, brokers.StatusUnknown, nil, err.Error())
		return brokers.Record{}, false, err
	}
	if err := store.FinishBrokerOrder(ctx, u.ID, rec.ID, brokers.StatusSubmitted, &order, ""); err != nil {
		return brokers.Record{}, false, err
	}
	log.Printf("[BROKER] user %d order %d routed to %s/%s: %s %.8g %s (%s)", u.ID, rec.ID, req.Broker, req.Account, req.Side, req.Quantity, req.Symbol, order.Status)
	rec, err = store.GetBrokerOrder(ctx, u.ID, rec.ID)
	return rec, false, err
}

var errOutcomeUnknown = errors.New("the broker gave no clear answer, so the order may or may not exist; send the same client_order_id again to settle it")

func decodeBrokerBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, brokerBodyLimit)
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "The request body is not valid JSON of the expected shape"})
		return false
	}
	return true
}

// --- read-only aggregation -------------------------------------------------

// brokerSource names where a number came from. Nothing from different
// sources is added together or converted: the app shows each source's
// accounts and positions in the source's own currency.
type brokerSource struct {
	Broker     string `json:"broker"`
	Connection string `json:"connection"`
	Label      string `json:"label"`
	Error      string `json:"error,omitempty"`
	// Provenance says the numbers are the broker's report, retrieved now.
	Provenance provenance.Fact `json:"provenance"`
}

type sourceAccount struct {
	brokers.Account
	Broker     string `json:"broker"`
	Connection string `json:"connection"`
	Tradable   bool   `json:"tradable"`
}

type sourcePosition struct {
	brokers.Position
	Broker     string `json:"broker"`
	Connection string `json:"connection"`
}

// readSources lists the connections a read covers: the user's own, narrowed
// by the optional broker and connection filters, and the system's settings
// for an administrator who asks.
func (e *Engine) readSources(ctx context.Context, u identity.User, broker, connection string, includeSystem bool) []brokerRoute {
	var routes []brokerRoute
	add := func(b, c string) {
		if broker != "" && !strings.EqualFold(broker, b) {
			return
		}
		if connection != "" && connection != c {
			return
		}
		if rt, err := e.resolveBroker(ctx, u, b, c); err == nil {
			routes = append(routes, rt)
		}
	}
	for _, c := range e.loadConnections(ctx, u.ID) {
		add(c.Broker, c.Name)
	}
	if includeSystem && u.Role == identity.RoleSysAdmin {
		if host := e.brokerHost(); host != nil {
			for _, p := range host.TradingProviders(plugins.TypeBroker) {
				add(p.ProviderID, "")
			}
		}
	}
	return routes
}

func connectionLabel(rt brokerRoute) string {
	if rt.connection == "" {
		return rt.provider.Label + " (system settings)"
	}
	return rt.provider.Label + " · " + rt.connection
}

// --- routes ----------------------------------------------------------------

func (e *Engine) registerBrokerRoutes() {
	e.mux.HandleFunc("GET /api/brokers", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.brokerUser(w, r)
		if !ok {
			return
		}
		type broker struct {
			plugins.TradingProvider
			Usable   bool     `json:"usable"`
			Problems []string `json:"problems,omitempty"`
		}
		var list []broker
		var hosts []plugins.TradingProvider
		if host := e.brokerHost(); host != nil {
			for _, p := range host.TradingProviders("") {
				if p.Type == plugins.TypeTradingHost {
					hosts = append(hosts, p)
					continue
				}
				problems := brokerProblems(p)
				list = append(list, broker{TradingProvider: p, Usable: len(problems) == 0, Problems: problems})
			}
		}
		connections := e.loadConnections(r.Context(), u.ID)
		if connections == nil {
			connections = []brokerConnection{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"contract":       brokers.ContractVersion,
			"live_execution": brokers.LiveExecutionApproved,
			"notice":         "FloweySales sends orders to paper accounts only. Live accounts are listed and never traded.",
			"builtin":        map[string]any{"broker": brokers.BuiltinBroker, "account": brokers.BuiltinAccount, "environment": brokers.EnvironmentPaper, "orders_at": "/api/orders"},
			"brokers":        nonNil(list),
			"trading_hosts":  hosts,
			"connections":    connections,
		})
	})

	e.mux.HandleFunc("PUT /api/broker-connections/{name}", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.brokerUser(w, r)
		if !ok {
			return
		}
		var body struct {
			Broker string            `json:"broker"`
			Label  string            `json:"label"`
			Values map[string]string `json:"values"`
		}
		if !decodeBrokerBody(w, r, &body) {
			return
		}
		if err := e.saveConnection(r.Context(), u.ID, r.PathValue("name"), body.Broker, body.Label, body.Values); err != nil {
			if status, _ := brokerErrorStatus(err); status != http.StatusBadGateway {
				writeBrokerError(w, err)
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"connections": nonNil(e.loadConnections(r.Context(), u.ID))})
	})

	e.mux.HandleFunc("DELETE /api/broker-connections/{name}", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.brokerUser(w, r)
		if !ok {
			return
		}
		if err := e.deleteConnection(r.Context(), u.ID, r.PathValue("name")); err != nil {
			writeBrokerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"connections": nonNil(e.loadConnections(r.Context(), u.ID))})
	})

	e.mux.HandleFunc("GET /api/broker-accounts", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.brokerUser(w, r)
		if !ok {
			return
		}
		q := r.URL.Query()
		accounts, sources := []sourceAccount{}, []brokerSource{}
		for _, rt := range e.readSources(r.Context(), u, q.Get("broker"), q.Get("connection"), q.Get("include_system") == "true") {
			src := brokerSource{Broker: rt.provider.ProviderID, Connection: rt.connection, Label: connectionLabel(rt), Provenance: pluginProvenance(rt.provider.PluginID, plugins.CapBrokerAccounts, time.Now().UTC())}
			list, err := e.brokerAccounts(rt)
			if err != nil {
				_, src.Error = brokerErrorStatus(err)
			}
			for _, a := range list {
				accounts = append(accounts, sourceAccount{Account: a, Broker: rt.provider.ProviderID, Connection: rt.connection, Tradable: a.Environment == brokers.EnvironmentPaper})
			}
			sources = append(sources, src)
		}
		writeJSON(w, http.StatusOK, map[string]any{"accounts": accounts, "sources": sources, "mixed_currencies": "Amounts are shown per account in its own currency and are never added together or converted."})
	})

	e.mux.HandleFunc("GET /api/broker-positions", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.brokerUser(w, r)
		if !ok {
			return
		}
		q := r.URL.Query()
		positions, sources := []sourcePosition{}, []brokerSource{}
		for _, rt := range e.readSources(r.Context(), u, q.Get("broker"), q.Get("connection"), q.Get("include_system") == "true") {
			src := brokerSource{Broker: rt.provider.ProviderID, Connection: rt.connection, Label: connectionLabel(rt), Provenance: pluginProvenance(rt.provider.PluginID, plugins.CapBrokerPositions, time.Now().UTC())}
			input := map[string]any{}
			if account := q.Get("account"); account != "" {
				input["account"] = account
			}
			raw, err := e.callBroker(rt, plugins.CapBrokerPositions, input)
			if err == nil {
				var list []brokers.Position
				if list, err = brokers.DecodePositions(raw); err == nil {
					for _, p := range list {
						positions = append(positions, sourcePosition{Position: p, Broker: rt.provider.ProviderID, Connection: rt.connection})
					}
				}
			}
			if err != nil {
				_, src.Error = brokerErrorStatus(err)
			}
			sources = append(sources, src)
		}
		writeJSON(w, http.StatusOK, map[string]any{"positions": positions, "sources": sources})
	})

	e.mux.HandleFunc("POST /api/broker-orders", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.brokerUser(w, r)
		if !ok {
			return
		}
		var req brokers.OrderRequest
		if !decodeBrokerBody(w, r, &req) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		rec, replayed, err := e.submitBrokerOrder(ctx, u, req)
		if err != nil {
			if errors.Is(err, errOutcomeUnknown) {
				writeJSON(w, http.StatusBadGateway, map[string]any{"status": "unknown", "error": errOutcomeUnknown.Error(), "client_order_id": strings.TrimSpace(req.ClientOrderID)})
				return
			}
			writeBrokerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "success", "order": rec, "replayed": replayed})
	})

	e.mux.HandleFunc("GET /api/broker-orders", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.brokerUser(w, r)
		if !ok {
			return
		}
		store := e.brokerLedgerStore()
		if store == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "The order ledger is unavailable"})
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		list, err := store.ListBrokerOrders(r.Context(), u.ID, limit)
		if err != nil {
			writeBrokerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"orders": nonNil(list)})
	})

	// Refresh asks the broker for the order by its client order id, which
	// also settles an order whose outcome was unknown.
	e.mux.HandleFunc("GET /api/broker-orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.brokerUser(w, r)
		if !ok {
			return
		}
		rec, store, err := e.ownedBrokerOrder(r, u)
		if err != nil {
			writeBrokerError(w, err)
			return
		}
		if r.URL.Query().Get("refresh") == "true" {
			if rec, err = e.reconcileBrokerOrder(r.Context(), store, rec); err != nil {
				writeBrokerError(w, err)
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"order": rec})
	})

	e.mux.HandleFunc("POST /api/broker-orders/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.brokerUser(w, r)
		if !ok {
			return
		}
		rec, store, err := e.ownedBrokerOrder(r, u)
		if err != nil {
			writeBrokerError(w, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		rec, err = e.cancelBrokerOrder(ctx, u, store, rec)
		if err != nil {
			writeBrokerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "success", "order": rec})
	})

	e.mux.HandleFunc("POST /api/trading/intents", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.brokerUser(w, r)
		if !ok {
			return
		}
		var body struct {
			Host       string                 `json:"host"`
			Broker     string                 `json:"broker"`
			Connection string                 `json:"connection"`
			Account    string                 `json:"account"`
			Targets    []pluginsTradingTarget `json:"targets"`
		}
		if !decodeBrokerBody(w, r, &body) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		out, err := e.proposeIntents(ctx, u, body.Host, body.Broker, body.Connection, body.Account, body.Targets)
		if err != nil {
			writeBrokerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
}

func nonNil[T any](list []T) []T {
	if list == nil {
		return []T{}
	}
	return list
}

func (e *Engine) ownedBrokerOrder(r *http.Request, u identity.User) (brokers.Record, brokers.Store, error) {
	store := e.brokerLedgerStore()
	if store == nil {
		return brokers.Record{}, nil, errors.New("the order ledger is unavailable")
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return brokers.Record{}, nil, fmt.Errorf("%w: invalid order id", brokers.ErrInvalid)
	}
	rec, err := store.GetBrokerOrder(r.Context(), u.ID, id)
	return rec, store, err
}

func (e *Engine) cancelBrokerOrder(ctx context.Context, u identity.User, store brokers.Store, rec brokers.Record) (brokers.Record, error) {
	if rec.Order == nil || rec.BrokerOrderID == "" {
		return rec, fmt.Errorf("%w: the broker has not confirmed this order, so there is nothing to cancel yet; refresh it first", brokers.ErrRoute)
	}
	if !rec.Order.Working() {
		return rec, fmt.Errorf("%w: the order is %s and cannot be cancelled", brokers.ErrRoute, strings.ToLower(rec.Order.Status))
	}
	rt, err := e.resolveBroker(ctx, u, rec.Broker, rec.Connection)
	if err != nil {
		return rec, err
	}
	if _, err := e.brokerPaperAccount(rt, rec.Account); err != nil {
		return rec, err
	}
	raw, err := e.callBroker(rt, plugins.CapBrokerCancelOrder, map[string]any{"account": rec.Account, "order_id": rec.BrokerOrderID})
	if err != nil {
		return rec, err
	}
	order, err := brokers.DecodeCancelled(raw, *rec.Order)
	if err != nil {
		return rec, err
	}
	if err := brokers.NextOrderState(rec.Order, order, rec.Request); err != nil {
		return rec, err
	}
	if err := store.FinishBrokerOrder(ctx, u.ID, rec.ID, rec.Status, &order, ""); err != nil {
		return rec, err
	}
	return store.GetBrokerOrder(ctx, u.ID, rec.ID)
}

// --- trading hosts ---------------------------------------------------------

// pluginsTradingTarget is one target weight handed to a trading host.
type pluginsTradingTarget struct {
	Symbol    string  `json:"symbol"`
	WeightPct float64 `json:"weight_pct"`
	Price     float64 `json:"price,omitempty"`
}

// proposeIntents asks a trading host for order intents that move one paper
// account towards target weights. Nothing is sent to a broker: the user
// reviews the intents and submits the ones they accept as ordinary orders.
func (e *Engine) proposeIntents(ctx context.Context, u identity.User, hostID, broker, connection, account string, targets []pluginsTradingTarget) (map[string]any, error) {
	host := e.brokerHost()
	if host == nil {
		return nil, fmt.Errorf("%w: plugins are not configured", errBrokerUnknown)
	}
	var hostProvider plugins.TradingProvider
	found := false
	for _, p := range host.TradingProviders(plugins.TypeTradingHost) {
		if strings.EqualFold(p.ProviderID, hostID) {
			hostProvider, found = p, true
		}
	}
	if !found {
		return nil, fmt.Errorf("%w: trading host %q is not installed", errBrokerUnknown, hostID)
	}
	if hostProvider.Contract != brokers.ContractVersion || hostProvider.Problem != "" {
		return nil, fmt.Errorf("%w: %s speaks trading contract %d, this FloweySales speaks %d", brokers.ErrRoute, hostProvider.ProviderID, hostProvider.Contract, brokers.ContractVersion)
	}
	if len(targets) == 0 || len(targets) > 200 {
		return nil, fmt.Errorf("%w: give between 1 and 200 target weights", brokers.ErrInvalid)
	}
	broker = strings.ToUpper(strings.TrimSpace(broker))
	account = strings.TrimSpace(account)
	if broker == "" || account == "" {
		return nil, fmt.Errorf("%w: name the broker and the account the intents are for", brokers.ErrRoute)
	}
	rt, err := e.resolveBroker(ctx, u, broker, strings.TrimSpace(connection))
	if err != nil {
		return nil, err
	}
	acct, err := e.brokerPaperAccount(rt, account)
	if err != nil {
		return nil, err
	}
	positions := []brokers.Position{}
	if rt.supports(plugins.CapBrokerPositions) {
		raw, err := e.callBroker(rt, plugins.CapBrokerPositions, map[string]any{"account": account})
		if err != nil {
			return nil, err
		}
		if positions, err = brokers.DecodePositions(raw); err != nil {
			return nil, err
		}
	}
	targetsJSON, _ := json.Marshal(targets)
	positionsJSON, _ := json.Marshal(positions)
	raw, err := host.CallTrading(ctx, hostProvider.ProviderID, plugins.CapTradingOrderIntents, map[string]any{
		"broker": rt.provider.ProviderID, "account": account, "currency": acct.Currency, "cash": acct.Cash,
		"positions": string(positionsJSON), "targets": string(targetsJSON),
	})
	if err != nil {
		return nil, err
	}
	intents, warnings, err := brokers.DecodeIntents(raw, rt.provider.ProviderID, account)
	if err != nil {
		return nil, err
	}
	// Each intent is judged by the rules an order would meet, so the user sees
	// at once which ones would be refused.
	type judged struct {
		brokers.Intent
		Refused string `json:"refused,omitempty"`
	}
	list := make([]judged, 0, len(intents))
	for _, it := range intents {
		j := judged{Intent: it}
		req := brokers.OrderRequest{Connection: connection, Broker: it.Broker, Account: it.Account, ClientOrderID: "intent", Symbol: it.Symbol, Side: it.Side,
			Type: it.Type, TIF: it.TIF, Quantity: it.Quantity, LimitPrice: it.LimitPrice, StopPrice: it.StopPrice}
		if err := req.Normalize(); err != nil {
			j.Refused = err.Error()
		} else if !contains(rt.provider.Broker.OrderTypes, req.Type) || !contains(rt.provider.Broker.TimeInForce, req.TIF) {
			j.Refused = rt.provider.ProviderID + " does not accept this order type or time in force"
		} else if err := brokers.CheckRisk(req, acct, e.brokerLimits()); err != nil {
			j.Refused = err.Error()
		}
		list = append(list, j)
	}
	return map[string]any{
		"host": hostProvider.ProviderID, "broker": rt.provider.ProviderID, "connection": connection, "account": account, "environment": acct.Environment,
		"intents": list, "warnings": nonNil(warnings), "executed": false,
		"note":       "These are proposals. Nothing was sent to the broker; submit the ones you accept as orders.",
		"provenance": pluginProvenance(hostProvider.PluginID, plugins.CapTradingOrderIntents, time.Now().UTC()),
	}, nil
}
