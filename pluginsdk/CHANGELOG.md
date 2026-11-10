# Plugin SDK changelog

Every change to the exported API, the wire protocol types or the manifest
schema adds an entry under **Unreleased**, labeled **Breaking**, **Additive**
or **Fix**, with migration notes for breaking changes. The release workflow
publishes the section for the tagged version as the release notes. See
`docs/releasing.md` and `pluginsdk/compat.json`.

## Unreleased

- Additive: capability `macro`. It has no method of its own: it says the
  provider's `history` answers the macro series ids listed in `docs/plugins.md`,
  and it requires `history`. Existing plugins keep working. A plugin that
  declares it must set `min_app_version` to the first FloweySales release that
  knows it, since an older backend refuses an unknown capability.

## v0.2.0

- Additive: package `feed` parses RSS 2.0 and Atom headline feeds (DTDs and
  external entities are never loaded, 2 MiB cap) and converts them to
  `NewsItem`s with the feed's publication date. An unreadable date stays 0 and
  is never guessed. Plugins can use it where a source's JSON search omits dates.
- Additive: capabilities `news` (method `news`, `NewsItem`) and `events`
  (method `events`, `CorporateEvent`), the `tools` capability (plugin-contributed
  UI tab, method `tool`) and domain providers with saved-transaction exports.
  Existing plugins keep working; a plugin declares only what it implements.
- Fix: `httpx` waits for a 429 `Retry-After` hint of up to 5 seconds before
  retrying; a longer hint fails fast with the value in `StatusError.RetryAfter`.
- Fix: `httpx.ToFloat` is strict. It rejects trailing text such as `12abc`,
  NaN and Inf instead of returning a partial number. Plugins that relied on the
  lenient parse must clean the value first.
- Additive: release management gates (`compat.json`, API compatibility check,
  changelog-driven release notes). No API or protocol change.

## v0.1.0

- First tagged SDK. Wire protocol 1. Packages `pluginsdk`, `httpx`,
  `research` and `plugintest`.
