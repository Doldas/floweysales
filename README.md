# FloweySales

Go engine + Angular (Material) web terminal. One binary serves the API, the
SSE price stream and the web UI.

## Requirements

- Go 1.27.1+ (see `go.mod`)
- Node.js + npm (only needed to build/develop the frontend)

## Quick start

```bash
make run          # builds frontend -> www/, builds Go binary, launches on :8080
```

Then open http://localhost:8080.

## Project status and roadmap

FloweySales is an open-source, local-first research application with Go market adapters, a paper-trading engine, an Angular UI and an OpenAI-compatible agent. See [container setup](docs/containers.md), [database & roles](docs/database-and-auth.md), [vision](docs/vision.md), [roadmap](docs/roadmap.md), [architecture](docs/architecture.md), [contributing](CONTRIBUTING.md) and [GitHub setup](docs/github-operations.md). The hosted [GitHub Pages site](https://doldas.github.io/floweysales/) is **static project documentation**, not a remote brokerage app.

Data and model outputs are research aids, not investment advice. Fully automatic live trading is **not supported**; future work requires separate security, compliance and risk gates.

## Common commands

| Command            | What it does                                        |
|--------------------|-----------------------------------------------------|
| `make deps`        | Install npm deps + download Go modules              |
| `make frontend`    | Build the Angular app into `www/browser`            |
| `make backend`     | Build the Go binary (`floweysales_backend`)         |
| `make build`       | Both of the above                                   |
| `make run`         | Build everything and start the server on :8080      |
| `make dev`         | Rebuild only the Go binary and start the server     |
| `make clean`       | Remove the binary and Angular output                |

## Frontend development (hot reload)

1. Start the API:
   ```bash
   make dev
   ```
2. In a second terminal:
   ```bash
   cd frontend
   npm start
   ```
3. Open http://localhost:4200 — `/api/*` requests are proxied to :8080 and the
   UI hot-reloads on save.

## UI smoke test

With the server running on :8080:

```bash
cd frontend
npx playwright install chromium   # first time only
npm run smoke              # boot, search, track, themes, error snackbar
npm run smoke:history      # historical chart seeding + range toggles
npm run smoke:stop         # stop-loss line spans the whole range
npm run smoke:analysis     # indicator chips, RSI/MACD panels, insights
npm run smoke:financials   # quarterly statements card
npm run smoke:ai           # AI chat panel, tools, markdown rendering
npm run smoke:trading      # order ticket, review/confirm, positions, cancel
```

Verifies with a headless browser that the page boots with zero console
errors, ticker search/autocomplete works, tracking switches the symbol,
theme switching applies, a wrong ticker shows the error snackbar, and the
chart is seeded with provider market history immediately.

## Market history

`GET /api/history?symbol=MSFT&provider=YAHOO&range=MAX` returns provider OHLC
bars (`1D/5D/1M/3M/1Y/3Y/5Y/10Y/MAX`) used to seed the chart and the EMA
tracker when an asset is tracked. `MAX` reaches as far back as the listing
allows — for old stocks that means decades (e.g. MSFT back to 1986):

- **Yahoo** — v8 chart API (`5m/15m/1h/4h/1d` intervals)
- **Binance** — klines with `endTime` pagination for multi-year ranges
  (`BTC` auto-normalizes to `BTCUSDT`; data starts at the 2017 listing)
- **Avanza** — market-guide ticker mapping with Yahoo fallback
  (e.g. `VOLV B` → `VOLV-B.ST`); funds have no public OHLC
- **Finnhub** — candles (requires `FINNHUB_API_KEY`)

Series are cached for 60 s server-side. The backend warms the trailing-stop
tracker from 1M of history on startup and on every asset switch, so EMAs and
the stop-loss start realistic instead of cold.

## Analysis studies & insights

The chart has a chip row of toggleable studies (persisted in localStorage):

- **Overlays** — EMA 9/21/50/100/200, Bollinger Bands (20, 2σ with fill),
  VWAP (volume-weighted, running-mean fallback without volume), volume bars
  scaled to the bottom quarter of the price axis.
- **Sub-panels** — dedicated RSI (14) panel with 30/70 bands and MACD
  (12, 26, 9) panel with a green/red histogram; panels mount/unmount with
  their chips.
- **Analysis Insights card** — recomputed for every loaded range: RSI state,
  MACD histogram direction, EMA stack alignment (9/21/50/200), distance to
  EMA 200, annualized volatility, 1W/1M/3M/1Y returns, 52-week range
  position and ATR (14) %.

All indicators are computed client-side over the full close series
(`frontend/src/app/indicators.ts`) — no backend round-trip per toggle.

## Quarterly financials

`GET /api/financials?symbol=MSFT&provider=YAHOO` returns the most recent
quarterly income statements (revenue, gross/operating profit, net income,
diluted EPS, free cash flow, currency) rendered in the "Quarterly Financials"
card:

- **Yahoo** — `ws/fundamentals-timeseries` endpoint (last ~5 quarters)
- **Avanza** — resolved through the market-guide ticker mapping to Yahoo
  (e.g. `5269` → `VOLV-B.ST`, SEK figures)
- **Finnhub** — `stock/financials-reported` (SEC 10-Q/10-K). 10-Q income
  statements report year-to-date numbers, so per-quarter values are derived
  by subtracting the previous filing's YTD figures
- **Binance** — returns a friendly note; crypto has no financial statements

Responses are cached for 30 minutes server-side (statements only change when
a company files). The card computes QoQ revenue growth and gross/net margins
client-side.

## AI Chat & OpenAI-compatible models

The web UI has a floating **AI Chat** panel (bottom-right) backed by the Go server. The backend routes to OpenAI-compatible endpoints and exposes an agent that can call the live market tools:

**Endpoints**
- `GET /api/ai/config` — presets, configured flags, tools (7), no API key leaked
- `POST /api/ai/chat` — SSE: `delta`, `tool_start`, `tool_result`, `error`, `done`

**Tools (7)**
- `search_symbols`, `get_price_history`, `get_technical_analysis`, `get_quarterly_financials`, `get_market_stats`, `get_recent_ticks`, `get_engine_config`

**Config** (`config.properties` / env)
- `OPENAI_BASE_URL` (e.g. `https://api.openai.com/v1` or `http://localhost:11434/v1` for Ollama/LM Studio/llama.cpp/vLLM/OpenRouter)
- `OPENAI_API_KEY` (blank for local models)
- `OPENAI_MODEL` (e.g. `gpt-4o-mini`, `llama3.1`, `local-model`)

The UI can override `base_url`, `api_key`, `model` and persists `ai_chat_config`/`ai_system_prompt` in `localStorage`. Presets cover common local/cloud endpoints.

Smoke test: `cd frontend && node smoke-ai.mjs`.

## MCP server

A standalone Model Context Protocol server (`floweysales_mcp`) is built from `cmd/mcp/main.go` and `internal/mcp/mcp.go`. It exposes the same tools to MCP clients.

**Build/run**
- `make build` or `make mcp` → builds `floweysales_mcp`
- `make run-mcp` → runs stdio transport
- Flags/env: `-transport=stdio|http` (`FLOWEY_MCP_TRANSPORT`), `-addr=...` (`FLOWEY_MCP_ADDR`, default `:8090`), `-backend=...` (`FLOWEY_BACKEND_URL`, default `http://localhost:8080`), `-key=...` (`FLOWEY_BACKEND_KEY`)
- MCP protocol version `2024-11-05`, serverInfo `floweysales-mcp` v1.0.0
- Transports: stdio (recommended), `POST /mcp` (streamable HTTP), legacy `GET /sse` + `POST /messages`
.

## Trading & portfolio API

Paper-trading engine (`internal/trading`) with full order lifecycle, wired into
the **Trading Desk** card of the UI (order ticket + positions/blotter).

**Endpoints**

| Endpoint | Purpose |
|---|---|
| `POST /api/orders` | Submit an order — `{"symbol","side","type","tif","quantity","limit_price?","stop_price?"}` |
| `GET /api/orders?symbol=&status=` | Order book (default: every symbol) |
| `PATCH /api/orders` | Transition: `{"id","action":"cancel"}` or `{"action":"fill","fill_price"}` (409 on illegal transitions) |
| `GET /api/positions` | `{positions: [...], summary: {equity, cash, buying_power, market_value, unrealized_pnl, realized_pnl, open_orders}}` |

**Order model**

- Types: `MARKET` (fills immediately at the current price), `LIMIT` (rests until
  a tick crosses it), `STOP` (fires as market once touched), `STOP_LIMIT`
  (becomes a resting limit once the stop is touched)
- Time in force: `GTC` (rests), `DAY`, `IOC` (immediate else cancelled)
- Statuses: `OPEN → FILLED | CANCELLED | REJECTED` with `reason` on non-fill exits
- Validation (HTTP 400 with `{"status":"error","error":...}`): quantity > 0,
  limit price required for limit orders, stop price required for stop orders,
  no market price accepted, shorting blocked unless allowed, and a
  buying-power check against **equity − market value − open-order reservations**
- Every engine tick (`BroadcastTick`) runs `matchOrders`, so resting orders fill
  live; commissions are applied per fill and orders moved to `FILLED` keep
  `fill_price`, `fee`, `filled_at`

Positions are net per symbol (realized P&L survives closing, closed symbols
drop out), with unrealized P&L marked from the latest price and an average
entry fallback when a mark is unavailable.

Smoke test: `cd frontend && node smoke-trading.mjs`.

## Configuration

`config.properties` is read at startup; every key can be overridden with an
environment variable of the same name.

| Key | Purpose |
|---|---|
| `REST_API_HOST` / `REST_API_PORT` | Server bind address (default `0.0.0.0:8080`) |
| `REST_API_UPDATE_RATE` | Ticker poll interval in ms (min 250) |
| `SQLITE_STORAGE_PATH` | `:memory:` or a file path |
| `DEFAULT_MARKET_SYMBOL` / `DEFAULT_MARKET_PROVIDER` | Initial asset (`YAHOO`/`BINANCE`/`FINNHUB`/`AVANZA`) |
| `FINNHUB_API_KEY` | Required for the Finnhub provider |
| `SMTP_APP_MAIL` + `SMTP_APP_MAIL_PSW` | Alert sender credentials |
| `DEFAULT_SMTP_MAIL_RECIPIENT` | Alert recipient |
| `EMAIL_COOLDOWN_DURATION_MIN` | Minutes between alert emails |
| `DEFAULT_BASE_OFFSET` etc. | Trailing-stop / EMA tuning knobs |
| `OPENAI_BASE_URL` / `OPENAI_API_KEY` / `OPENAI_MODEL` | OpenAI-compatible endpoint configuration (blank for local models) |
| `PORTFOLIO_CASH` | Starting paper cash / equity (default `100000`) |
| `TRADE_COMMISSION_PCT` / `TRADE_COMMISSION_FIXED` | Commission per fill, percent of notional + fixed amount (default `0`) |
| `ALLOW_SHORT_SELLING` | Allow selling more than you hold (default `false`) |


### Research engine and updates

- `SYS_ADMIN` configures bounded AI model rounds and tool calls in the UI (persisted by Liquibase).
- Optional `search_web` tool uses the Brave Search API with a server-side `BRAVE_SEARCH_API_KEY`; returned public URLs should be independently verified.
- P/E uses matching TTM per-share EPS and share price or market cap and TTM earnings; P/FCF requires market cap divided by TTM total FCF.
- For unattended Docker / Podman updates from **successful CI-published GHCR images**, see [auto-updates](docs/auto-updates.md). The scheduled updater preserves `.env`, MySQL and SQLite volumes and does not auto-upgrade MySQL.


### System Admin: AI and web-search API credentials

The **API Settings** tab (available only to `SYS_ADMIN`) manages the AI endpoint,
default model and agent budgets, AI model choices, and encrypted credentials for
OpenAI-compatible AI and Brave/Tavily web search. API keys are never returned by
the GET endpoints; only configured/not-configured status is shown.

Persistent credentials require a stable 32-byte server-side AES-256-GCM master
key provided through `FLOWEYSALES_SECRETS_KEY` (standard base64). Generate one
with `openssl rand -base64 32` and install it in the deployment's secret
manager, **not** in Git, frontend configuration or browser storage. Preserve it
across server restarts and backups: losing it means existing encrypted API keys
cannot be recovered. Without it, the admin page remains accessible but saving
API keys is disabled. Encryption uses random per-secret nonces and authenticated
integration identifiers. Existing environment-only AI keys still work.

Web search credentials can be saved ahead of the upcoming search-engine node;
this settings page alone does not activate a search endpoint or crawler.
