# ADR 0002: Market-data providers as installable plugins

- **Status:** Accepted
- **Date:** 2026-10-31

## Context

Yahoo Finance, Binance, Finnhub and Avanza were compiled into the backend as
`internal/market` adapters behind a closed `Provider` enum. Every provider
change meant a backend release; provider ids were hard-coded in the REST
validation, the tracker loop's rate limits, the AI and MCP tool schemas and
the UI; the unofficial Avanza adapter shipped to every installation behind an
environment switch; and third parties could not add a source.

## Decision

1. **Providers are plugins, in their own repository.** Each provider is a
   standalone executable in
   [Doldas/floweysales-plugins](https://github.com/Doldas/floweysales-plugins),
   built on the SDK module `github.com/Doldas/floweysales/pluginsdk`, which
   stays here with the rest of the interfaces (protocol, manifest, store
   format, contract fixtures, conformance kit). The backend imports neither
   the SDK nor any plugin; CI fails if it does. The backend keeps
   only the provider-agnostic model (`internal/market`: types, indicators,
   provenance rules) and a `market.Backend` interface.
2. **Out-of-process, JSON over stdio.** Go's `plugin` package needs cgo,
   identical toolchains and does not work on Windows; WebAssembly would need a
   host networking API. A child process speaking newline-delimited JSON works
   on every platform the backend ships for, from any language, and a plugin
   crash cannot take the backend down.
3. **One manifest.** `plugin.json` declares identity, version, capabilities,
   allowlisted research operations, minimum poll interval, required settings
   and install terms. It is embedded in the binary, returned by `describe`,
   uploaded with the release and copied into the store index, so the store,
   installer and running process describe the same thing.
4. **Store = static index pinned by SHA-256.** The index is generated from
   the plugins repository's GitHub releases (`<id>-v<version>` tags) and
   published with its GitHub Pages site. Installing verifies size and checksum, handshakes with
   the binary in a staging directory and requires its manifest to match the
   store entry before swapping it in. The checksum is re-verified on every
   start.
5. **Least environment.** Plugins get a minimal environment plus only the
   variables they declare; backend secrets cannot be declared.
6. **Terms instead of opt-in flags.** Plugins using unofficial interfaces
   (Yahoo, Avanza) carry terms that must be accepted at installation,
   replacing `AVANZA_UNOFFICIAL_ENABLED`.
7. **Management.** SYS_ADMIN-only Settings → Plugins (install, update,
   version pinning/rollback, uninstall, update checks) requiring
   authentication, plus a `floweysales_backend plugins` CLI for servers
   without authentication. Updates are detected (optionally on a schedule)
   but never installed automatically.

## Consequences

- A fresh installation has no providers until an administrator installs
  some. Existing trackers stay in the database and resume once their
  provider's plugin is installed; until then they are polled every 30 s and
  produce no ticks.
- Plugins release independently of the backend; the backend and plugins
  only have to agree on the protocol version. Contract changes are caught by
  the shared fixtures in `pluginsdk/testdata/contract/` and by
  `plugintest.Conformance` in each plugin repository.
- Provider ids are open strings validated by pattern. The REST API, AI tools
  and MCP tools discover providers from `GET /api/providers` instead of fixed
  enums; adding a tracker requires an installed provider that offers quotes.
- Finnhub calls now send the token as the `X-Finnhub-Token` header only,
  never in URLs.
- Plugins run as the backend's OS user with network access. Checksums prove
  integrity relative to the index, not trustworthiness of its publisher;
  signing the index is a possible follow-up.
- Container deployments need the new `plugins_data` volume (`/plugins`); the
  read-only root filesystem is unchanged.
- Each plugin binary is ~7 MB; four plugins add ~28 MB to `PLUGIN_DIR` but
  nothing to the image.
