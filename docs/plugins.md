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
any other key — environment first, then `config.properties` — and passed
**only to that plugin**. A plugin whose required settings are missing is
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
