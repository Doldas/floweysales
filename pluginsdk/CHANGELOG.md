# Plugin SDK changelog

Every change to the exported API, the wire protocol types or the manifest
schema adds an entry under **Unreleased**, labeled **Breaking**, **Additive**
or **Fix**, with migration notes for breaking changes. The release workflow
publishes the section for the tagged version as the release notes. See
`docs/releasing.md` and `pluginsdk/compat.json`.

## Unreleased

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
