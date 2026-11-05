# Provider plugins and the plugin store

FloweySales has no built-in market-data providers. Yahoo Finance, Binance,
Finnhub and Avanza are **plugins**: separate executables that the backend
downloads from a plugin store, verifies, starts on demand and talks to over
stdin/stdout. Without an installed plugin there are no quotes, history,
search, statements or trackers for that provider.

The work is split across two repositories:

| Repository | Owns |
|---|---|
| **Doldas/floweysales** (this one) | The interfaces: the wire protocol, the `plugin.json` manifest, the store index format, the plugin manager, and the Go SDK `github.com/Doldas/floweysales/pluginsdk` with helpers and a conformance test kit |
| **[Doldas/floweysales-plugins](https://github.com/Doldas/floweysales-plugins)** | The official plugins (Yahoo, Binance, Finnhub, Avanza), their releases and the official store index |

Anyone can publish plugins and stores the same way the official repository
does. See [ADR 0002](adr/0002-provider-plugins.md) for why it works this way.

## Managing plugins

### Settings → Plugins (SYS_ADMIN)

Lists installed plugins and the store side by side: install, update (one or
**Update all**), install a specific version (also to roll back), uninstall,
and **Check for updates**. Each installed plugin shows whether its required
settings are configured, which trackers depend on it, its checksum and the
last lines it logged.

- Installing a plugin whose manifest has **terms** (unofficial interfaces
  such as Yahoo and Avanza) opens a dialog; the install only proceeds once the
  terms are accepted. This replaces the old `AVANZA_UNOFFICIAL_ENABLED` switch.
- Uninstalling a plugin that active trackers use asks first. Those trackers
  keep their history and resume when the provider is installed again.
- Because installing runs downloaded code, these routes require
  authentication (`AUTH_ENABLED=true`) and a SYS_ADMIN session, like updates.

### Compatibility with this FloweySales

Every installed plugin shows one of four states in **Settings → Plugins**, with the reason, the protocol on both sides, what the claim rests on and when it was last checked:

- **Compatible**: the declared protocol matches and the plugin answered a handshake (without any of its secrets) that agrees with what is installed.
- **Not verified**: nothing contradicts compatibility, but no handshake has run since FloweySales started. Installed or running is not treated as verified. The server verifies all plugins in the background after startup, and **Verify now** (`POST /api/admin/plugins/{id}/verify`) checks one on demand.
- **Needs update**: the plugin cannot run here, and the store lists a version that can. Nothing is updated without your action.
- **Incompatible**: the plugin cannot run here (for example it speaks another protocol, its manifest is no longer accepted, or the binary changed on disk) and no compatible version is known.

An incompatible plugin is **kept, not run**. FloweySales never removes it, its settings or its data on its own, and `installed.json` keeps it, so a rollback or a later update brings it back. Its provider and tools tab are unavailable meanwhile, and it can still be uninstalled. Installing, updating, choosing another version and sideloading all refuse a version that does not speak this protocol, in the backend and the CLI as well as the UI.

#### Preview before an app update

When **Settings → Updates** finds a newer version, it lists every installed plugin with its status **now** and **after** the update, the reason, and the store versions that speak the new protocol. The comparison uses the plugin protocol the new image declares in its `org.floweysales.plugin-protocol` label (the `PLUGIN_PROTOCOL` build argument, kept equal to the backend's protocol by a test). An image without the label is shown as **Unknown** for every plugin rather than assumed compatible, and a protocol-1 plugin is never marked compatible with a protocol-2 release. The preview changes nothing: no plugin is run, downloaded, removed or updated, and each one's binary, settings, data and approvals stay as they are. After the update runs, the status in Settings → Plugins is again established by a handshake.

Not yet covered: minimum and maximum app versions in the manifest, staged updates with configuration rollback, and a post-upgrade report.

**Update all** tries each plugin on its own and ends with a per-plugin result (updated, skipped or failed, with the reason). One failure or a terms prompt never stops the others, and a plugin that is not updated keeps its installed version and settings. Incompatible versions are never offered, and the backend refuses them even when called directly.

### Command line

On servers without authentication, or without a browser, use the backend
binary. It edits the same plugin directory; restart the backend afterwards.

```sh
floweysales_backend plugins store                       # what the store offers (checks now)
floweysales_backend plugins install yahoo --accept-terms
floweysales_backend plugins install binance 1.0.0       # a specific version
floweysales_backend plugins install --file ./my-plugin  # sideload a local binary
floweysales_backend plugins list                        # installed, status, available updates
floweysales_backend plugins update                      # update everything
floweysales_backend plugins uninstall avanza
```

In containers: `docker compose exec app /app/floweysales_backend plugins list`.

### Configuration

| Key | Default | Purpose |
|---|---|---|
| `PLUGIN_DIR` | `<user config dir>/floweysales/plugins` (`/plugins` in containers) | Installed plugins and `installed.json`. Must be writable and allow executing files. |
| `PLUGIN_STORE_URL` | `https://doldas.github.io/floweysales-plugins/index.json` | Store index; `https://` or, for offline/air-gapped stores, `file://`. |
| `PLUGIN_CHECK_INTERVAL_HOURS` | `0` (on request only) | Background store checks so Settings shows available updates. Updates are never installed automatically. |
| `DEFAULT_MARKET_PROVIDER` | `YAHOO` | Provider used when a request names none. |

Settings a plugin declares (for example `FINNHUB_API_KEY`) are resolved like
any other key — environment first, then `config.properties`, then a value
entered under **Settings → Plugins** — and passed **only to that plugin**.
A value entered in Settings is stored encrypted (it needs
`FLOWEYSALES_SECRETS_KEY`), is never shown again, and restarts the plugin when
saved or removed (`PUT`/`DELETE /api/admin/plugins/{id}/env/{name}`, SYS_ADMIN).
A variable set in the environment cannot be overridden from Settings. A plugin whose required settings are missing is
listed as *Needs configuration* and its provider reports `enabled: false`.

## How a plugin runs

1. On the first call for its provider the backend checks the binary's SHA-256
   against the one recorded at install time (a changed file is refused), then
   starts it with its own directory as working directory.
2. The process environment is minimal: `PATH`, temp and home directories,
   proxy and CA-certificate variables, `FLOWEYSALES_PLUGIN=1`,
   `FLOWEYSALES_PLUGIN_PROTOCOL=1` and the variables its manifest declares.
   Database DSNs, the secrets key, AI and SMTP credentials can never be
   declared. Declared secrets are registered for redaction.
3. Calls are multiplexed over one process; a slow history download does not
   block a quote. Each call times out after 45 s.
4. A crashed plugin fails the calls in flight and is restarted on the next
   call (at most once per 15 s if it keeps failing).
5. The backend only sends a method when the manifest declares the matching
   capability, and only research operations the manifest lists. It also
   re-applies the requested financial year range to whatever comes back.
6. The tracker loop polls a provider no faster than its
   `min_poll_seconds`, and picks up a plugin installed, updated or removed
   while it runs.

Plugins run with the backend's operating-system user. A plugin is software you
install: the store pins each binary by SHA-256 and the backend handshakes with
it before it replaces anything, but only install plugins from a store you
trust.

## Wire protocol (version 1)

Newline-delimited JSON on stdin (requests) and stdout (responses); stderr is
free-form log output (the last 40 lines are shown in Settings). The plugin
exits when stdin closes.

```json
{"id":7,"method":"history","params":{"symbol":"NVDA","range":"1M"}}
{"id":7,"result":{"symbol":"NVDA","provider":"YAHOO","range":"1M","interval":"1h","currency":"USD","points":[{"t":1761900000,"o":1,"h":1,"l":1,"c":1,"v":0}]}}
{"id":8,"error":{"code":"not_configured","message":"FINNHUB_API_KEY must be configured on the backend"}}
```

Responses may arrive in any order. Lines are limited to 1 MiB (requests) and
16 MiB (responses).

| Method | Params | Result | Capability |
|---|---|---|---|
| `describe` | – | manifest | always |
| `quote` | `symbol` | asset (`symbol`, `name`, `price`, `currency`, `provider`, `data`) | `quote` |
| `history` | `symbol`, `range` (`1D` … `MAX`) | OHLC series | `history` |
| `search` | `query` | `[{symbol,name,type,exchange}]` | `search` |
| `financials` | `symbol`, `from_year`, `to_year` | quarterly reports | `financials` |
| `research` | `operation`, `symbol`, `from`, `to` | research envelope | `research` |
| `forum_posts` | `symbol`, `limit` | `[{id,title,content,symbol,upvotes,author,timestamp}]` | `forum` |
| `news` | `symbol`, `limit` (1–20) | `[{title,url,source,published,summary,symbol,provider}]` (`published` unix seconds, optional) | `news` |
| `events` | `symbol`, `from`, `to` (ISO dates, window at most 400 days) | `[{symbol,kind,date,amount,currency,ratio,confirmed,provider}]` | `events` |
| `tool` | `tool`, `input` | `{title?, blocks:[…]}` | manifest `tools` |

Error codes: `unsupported`, `invalid_argument` (HTTP 400), `not_configured`
(503), `upstream` and `internal` (502). An uninstalled provider is HTTP 404.

The exact JSON shapes are pinned by golden files in
`pluginsdk/testdata/contract/`, tested by both the SDK and the backend.

The backend keeps news items with a title and a public http(s) URL only and
labels the provider; the Flow Studio **News headlines** node reads them.

`events` returns dividends (`kind: "dividend"`, `amount` per share), splits
(`ratio` like `"4:1"`) and earnings dates (`confirmed: false` for estimated
ones). The backend keeps events with a known kind and a valid date, drops
negative or non-finite amounts (an unknown amount is absent, never `0`),
labels the provider and orders by date. `GET /api/events?symbol=&from=&to=&provider=`
serves them (default window: today and 90 days ahead) and the Financials page
shows an **Upcoming** strip.

Provider rules from [CONTRIBUTING](../CONTRIBUTING.md) apply to plugins too:
a missing figure stays absent rather than `0`; currency is never guessed; a
fallback source is named in `provider` (the Avanza plugin labels its Yahoo
history fallback `YAHOO`); research is read-only and allowlisted.

## Domain providers: tax and other typed providers

A provider has a **type**. No type (or `"market"`) is a market-data provider
with the capabilities above. Any other type is a **domain provider**: the type
fixes which capabilities exist, and each capability is an *operation* with a
declared input that the app and the AI agent call without special code.

```json
"provider": {
  "id": "SE_TAX", "label": "Swedish tax", "type": "tax",
  "capabilities": ["tax_report", "tax_estimate"],
  "operations": [
    {"capability": "tax_report", "description": "K4 rows, totals and a tax estimate for an income year",
     "inputs": [{"name": "year", "type": "integer", "description": "Income year", "required": true},
                {"name": "csv", "type": "string", "description": "Transactions as CSV text", "required": true}]},
    {"capability": "tax_estimate", "description": "Tax on gains and losses you already know"}
  ]
}
```

| Type | Capabilities |
| --- | --- |
| `tax` | `tax_report` (sales of the income year with cost basis and totals), `tax_holdings` (holdings and average cost at a date), `tax_estimate` (tax on known gains and losses) |

- One wire method, `domain`, with `{operation, input}`; the result is any JSON of at most 1 MB. A plugin sets `Plugin.Domain` in the Go SDK and `plugintest.Conformance` checks it.
- The manifest needs a provider id, only capabilities of its type, and one described operation per capability. Input fields are `string`, `number`, `integer` or `boolean` (strings may list an `enum`). The backend rejects undeclared fields, wrong types and missing required fields before the plugin sees anything.
- A string input may name a `source`, `user_transactions`: if the caller leaves it empty, the backend fills it with the caller's own **saved transaction files** (Tax tab → Saved transactions, or `POST /api/me/transactions`), each file preceded by a `=== file: NAME ===` line. A value the caller passes always wins. Saved files are encrypted with the server secret key, belong to one user, and are limited to 10 files, 2 MB each and 3 MB in all. The AI agent's `call_domain_provider` result lists `used_saved_files`, so the AI can calculate tax without the user pasting anything.
- A domain provider is never a market-data provider: it does not appear in provider lists, trackers or polling.
- `GET /api/domain-providers[?type=tax]` lists installed ones with their operations; `POST /api/domain-providers/{provider}/{operation}` with `{"input": {...}}` runs one. Calls are stateless, nothing is stored or logged, and any signed-in user may make them.
- The AI agent gets `list_domain_providers` and `call_domain_provider`. The agent must not invent input such as transactions, and results are calculations from the data given, not advice. These tools are not grantable to flow agents.

## Tool plugins: a tab of tools

A plugin can add a **tab of interactive tools** to the app, with or without
market data. The manifest's `tools` section declares the tab and each tool's
form; the backend renders it, sends the values with the `tool` method and
shows the blocks the plugin returns. A plugin that only declares `tools` has
no `provider` (a half-declared provider is rejected).

A tools plugin may describe what it does with a **tool capability**:
`"provider": {"capabilities": ["tax"]}`. `tax` has no protocol method (the
tools use `tool`), needs `tools`, and leaves the provider id empty, so the
plugin is still not a market-data provider. Settings → Plugins shows it as a
"Tax" chip. Mixing `tax` with a market-data capability requires a full
provider declaration.

```json
{
  "protocol": 1, "id": "se-tax", "name": "Swedish tax", "version": "1.0.0",
  "tools": {
    "tab": {"label": "Tax", "icon": "bi-receipt", "description": "…"},
    "tools": [{
      "id": "k4", "label": "K4 report", "description": "…", "submit": "Calculate",
      "inputs": [
        {"name": "year", "label": "Income year", "type": "number", "required": true},
        {"name": "files", "label": "Transactions", "type": "files", "accept": ".csv"}
      ]
    }]
  }
}
```

Input types: `text`, `textarea`, `number`, `date`, `select` (with `options`),
`checkbox`, `file` (the value is the file's text) and `files` (a list of
`{name, content}`). The backend checks required fields, select choices and
types, drops undeclared fields and caps all input at 8 MB before the plugin
sees it.

A result is a list of blocks: `text`, `notice` (`info`, `success`, `warning`,
`error`), `table` (`columns` and string `rows`), `stats` (label/value tiles)
and `file` (a download: `name`, `mime`, `encoding` `utf8` or `base64`,
`content`; use base64 for other character sets). The backend rejects a result
that breaks this contract.

Tools are **stateless**: the user's data travels in the request and the
backend neither stores nor logs it. Any signed-in user may call
`GET /api/plugin-tools` (installed tool plugins) and
`POST /api/plugin-tools/{plugin}/{tool}` with `{"input": {…}}`. In Go set
`Plugin.Tool`; `plugintest.Conformance` checks that declared tools have a
handler and that an undeclared tool is refused.

## Writing a plugin

Any language works. In Go, use the SDK
(`go get github.com/Doldas/floweysales/pluginsdk`):

```go
package main

import (
	"context"
	_ "embed"

	sdk "github.com/Doldas/floweysales/pluginsdk"
)

//go:embed plugin.json
var manifestJSON []byte

func main() {
	manifest, err := sdk.ParseManifest(manifestJSON)
	if err != nil {
		panic(err)
	}
	sdk.Main(sdk.Plugin{
		Manifest: manifest,
		Quote: func(ctx context.Context, p sdk.QuoteParams) (sdk.Asset, error) {
			return sdk.Asset{Symbol: p.Symbol, Price: 1, Currency: "USD", Provider: manifest.Provider.ID}, nil
		},
	})
}
```

`plugin.json` is the single description of a plugin: embedded in the binary,
returned by `describe`, uploaded with each release and copied into the store.

```json
{
  "protocol": 1,
  "id": "example",
  "name": "Example data",
  "version": "1.0.0",
  "description": "What it provides.",
  "publisher": "You",
  "terms": "Optional: shown and accepted before installation.",
  "provider": {
    "id": "EXAMPLE",
    "label": "Example",
    "official_api": true,
    "experimental": false,
    "capabilities": ["quote", "history"],
    "quote_kind": "ohlc_bar_close",
    "min_poll_seconds": 5,
    "cache_ttl_seconds": 60,
    "notes": "Access, licensing and freshness caveats."
  },
  "env": [{"name": "EXAMPLE_API_KEY", "secret": true, "required": true, "description": "API token"}]
}
```

Plugin ids are lowercase (`[a-z][a-z0-9-]{1,39}`), provider ids uppercase.
One plugin provides one provider, and two installed plugins cannot provide the
same provider id. Run `my-plugin --manifest` to print the embedded manifest.

Try a plugin locally with `floweysales_backend plugins install --file`, or
build a local store (below).

### Telling the price feed what a quote means

A `quote` result's `data` object may state, only when the provider really knows:
`quoteTimeUnix` (the quote's own time, Unix seconds), `delaySeconds` (how far behind the market it is) and `marketState` (`open` or `closed`).
Leave a key out when unknown; never send `0` for unknown. The feed status shows "Delayed 15 min", the quote time and "Market closed" from these,
and says "Real time" only when `quote_kind` is `realtime`. Without `marketState`, the server falls back to the exchange's regular weekday hours for
known tickers (suffix such as `.ST`, `.L`, `.T`, or a US exchange code in `data.exchange`); that fallback does not know holidays and the UI says so.

### What the SDK gives you

| Package | Use |
|---|---|
| `pluginsdk` | Protocol types, `ParseManifest`, `Main` (serves the protocol, adds `--manifest` and `--version`), `Errorf` with protocol error codes, `FilterFinancialYears` |
| `pluginsdk/httpx` | Provider HTTP client: response cap, optional request spacing per process, retries only for 429/502/503/504 (never auth failures) |
| `pluginsdk/research` | The `research` method done right: only operations declared in `plugin.json`, validated symbols and dates, redirect-free bounded fetches that never echo tokens, the standard result envelope |
| `pluginsdk/plugintest` | `plugintest.Conformance(t, plugin)` checks over the real wire protocol that `describe` returns the manifest, every declared capability has a handler (and no undeclared one does), and undeclared methods and research operations answer `unsupported`. `plugintest.Start` drives a plugin in-process for your own tests |

Every plugin should run `plugintest.Conformance` in its tests; the official
plugins do.

### SDK versions

The SDK is a Go module inside this repository, versioned with tags
`pluginsdk/v<semver>`. Its protocol version only changes when the backend's
does, so a plugin built against any SDK v0.x/v1.x speaking protocol 1 keeps
working with backends that speak protocol 1.

## The store

The store is a JSON index:

```json
{
  "schema": 1,
  "generated_at": "2026-10-31T08:00:00Z",
  "plugins": [{
    "id": "yahoo",
    "versions": [{
      "manifest": { "...": "plugin.json of this version" },
      "released_at": "2026-10-31T07:00:00Z",
      "notes": "Release notes",
      "artifacts": {
        "linux-amd64": {"url": "https://…/floweysales-plugin-yahoo-linux-amd64", "sha256": "…", "size": 7114912},
        "windows-amd64": {"url": "https://…/floweysales-plugin-yahoo-windows-amd64.exe", "sha256": "…", "size": 7301120}
      }
    }]
  }]
}
```

- An `https://` store may only reference `https://` artifacts; only a `file://`
  store may reference local files.
- The index is capped at 2 MiB and each artifact at 96 MiB. Invalid entries
  are skipped and listed as warnings rather than failing the whole store.
- *Latest* means the newest stable version that has a build for this
  platform and speaks this backend's protocol. Pre-releases are offered only
  to an installed pre-release. Versions needing a newer protocol are listed
  as "Needs a newer FloweySales".

Installing downloads the artifact, checks its size and SHA-256, starts it in
a staging directory without any declared settings, and requires its
`describe` answer to match the store entry (id, version, provider) before it
is moved into `PLUGIN_DIR/<id>/<version>-<random>/` and recorded in
`installed.json`. An update passes all of these checks before the old
version is stopped and removed, so a failed update leaves the installed
version in place.

### Publishing (official store)

The official plugins are released from
[Doldas/floweysales-plugins](https://github.com/Doldas/floweysales-plugins):

1. Bump `version` in `<id>/plugin.json` and merge.
2. Push the tag `<id>-v<version>`. Its release workflow tests every plugin
   (including SDK conformance), builds linux/amd64, linux/arm64,
   windows/amd64, darwin/amd64 and darwin/arm64 binaries, and creates a
   GitHub release with them, `plugin.json` and `SHA256SUMS`.
3. It then rebuilds and publishes the store index
   (`https://doldas.github.io/floweysales-plugins/index.json`) from every
   plugin release.

### Local or private store

The plugins repository has the build and index tooling:

```sh
scripts/build-plugins.sh                       # this platform → dist/plugins/index.json
scripts/build-plugins.sh -p linux/amd64,windows/amd64 -o /srv/store
PLUGIN_STORE_URL=file://$PWD/dist/plugins/index.json floweysales_backend plugins store
```

A private HTTPS store is the same directory layout behind a web server with
an index whose URLs point at it (`node scripts/plugin-index.mjs local <dir>`
writes `file://` URLs; rewrite them for hosting). Your own plugin repository
can copy those scripts and workflows; only the index format above is fixed.
