# Plan 9: License, language and time

**Issues:** #205 and #206 for the license and language. UTC has no issue yet, and `CONTRIBUTING.md` requires one for each PR, so **open one first**.

**Why one plan:** these are the owner's project-wide decisions from 2026-11-03. They are small, and doing them early prevents rework on every other plan.

## What is there today

**License.** No `LICENSE` file, and no license named in the README, `SECURITY.md` or `CONTRIBUTING.md`, though `CONTRIBUTING.md` calls the project open source. `frontend/package.json` has `"private": true` and no `license` field.

**Language.**
- The app is English, with `lang="en"`.
- Swedish remains in `site/index.html`, `site/api.html` (both `lang="sv"`), `site/api.js`, `site/api-catalog.json`, `docs/api-reference.md`, `docs/mysql-migration.md` and `docs/investor-profiles.md`. **To verify:** the last two might contain only some Swedish; the search matched Swedish letters.
- Some test files contain Swedish letters too (`redact_test.go`, `flow_events_test.go`, `transaction_files_test.go`, `plugin-tools.spec.ts`). They probably test non-ASCII input on purpose, so read them before changing anything.
- Some British spelling exists in comments and possibly in UI text (`authorisation` in `access.ts`).

**Time.** About 30 display sites use the browser's locale or time zone:

| Where | Today |
| --- | --- |
| `toLocaleString()` in `plugin-settings`, `update-settings`, `flow-studio`, `flow-side-effects`, `flow-event-adapters` | Browser zone and locale |
| Angular `date` pipe in `ledger`, `inbox-page`, `forum`, `saved-transactions-panel` | Browser zone |
| `timeAgo` in `ui/format.ts` (older dates) | Browser zone and locale |
| `command-center.ts` "today" | Browser zone and locale |
| `chart.ts` axis labels (lines 237, 245, 397, 403) | Browser zone |
| `chart.ts` tooltip (line 694) | **UTC with `en-GB`** |
| Numbers via `toLocaleString(undefined)`, `formatMoney` without a locale | Browser locale, so decimals can differ per user |

So a chart's axis and its own tooltip can disagree today. The backend already works in UTC (`UTC_TIMESTAMP`, `.UTC()` formats).

## Work packages

### WP1: MIT license (S, `chore/mit-license`)

- Add a root `LICENSE` with the standard MIT text, the current year and the copyright holder. **The owner confirms the exact name to use**, such as a legal name rather than a GitHub handle.
- Check `pluginsdk/` and any separately distributed module, and add a `license` field to `frontend/package.json`. Keep `"private": true`, which only stops npm publishing.
- Add a License section to the README and link it from `CONTRIBUTING.md`.
- Check dependencies are compatible with distribution under MIT. The build already emits `3rdpartylicenses.txt`. A GPL dependency in the shipped binary or bundle would need attention.
- `Doldas/floweysales-plugins` is a separate repository and needs its own decision.

Commits so far come from one human email address and Dependabot, so no other contributor needs to agree to the license.

### WP2: UTC and en-US formatting (M, `fix/utc-en-us-formatting`)

**One place to format, in `frontend/src/app/ui/format.ts`:**

```ts
formatDateTime(value)  // "Nov 3, 2026, 14:32 UTC"
formatDate(value)      // "Nov 3, 2026"
formatTime(value)      // "14:32 UTC"
formatNumber(value)    // en-US separators
```

- All use `Intl.DateTimeFormat('en-US', { timeZone: 'UTC', hourCycle: 'h23' })`. The 24-hour clock is a suggestion: it reads unambiguously next to "UTC" and suits market data. It takes one line to change.
- Values show "UTC" whenever a time appears. A date alone doesn't need it.
- Set `DATE_PIPE_DEFAULT_OPTIONS` to `{ timezone: 'UTC' }` and `LOCALE_ID` to `en-US` in `app.config.ts`, so existing pipes follow.
- Replace each site in the table, and make the chart's axis and tooltip agree, with the axis named "UTC".
- Date inputs are read and sent as UTC, with a "UTC" label.
- **Daily bars:** a bar's day comes from its UTC timestamp. A daily bar stamped near midnight could show the neighboring date. **To verify** per provider, and fix at the formatter or the provider boundary, with a test.
- **`greeting()`** uses the local hour for "Good morning", and that has no meaning in UTC. Use one neutral greeting, "Welcome back".
- **Output too:** dates in emails, flow reports, exports and agent context are UTC and say so. Check the agent's system prompt states that displayed times are UTC.
- **Relative ages** ("3 h ago") stay as a supplement, since they don't depend on a zone.

**Enforcement:** a small CI script fails on `toLocale*String()` without `'en-US'`, on `undefined` as a locale, and on `new Date(...).getHours()`-style local getters in `frontend/src/app`. This stops the problem from coming back.

**Tests:** the spec runs under `TZ=America/Los_Angeles` and `TZ=Asia/Tokyo` and gets identical output, and it covers a timestamp around midnight UTC. Add it to the `node --test` list.

### WP3: US English everywhere (M, `docs/en-us`)

- Translate the Swedish files listed above, and the catalog text in `site/api-catalog.json`. The catalog is also read by a backend comparison per `docs/api-reference.md`, so change text fields only and keep identifiers.
- Do the site as part of Plan 8 WP2, so that pages aren't translated and then replaced. If the new site is delayed, translate the current one so it isn't left in Swedish.
- Set `lang="en-US"` on `frontend/src/index.html` and the site. Set `Content-Language` if the server sets language headers.
- Pass over UI strings and comments for British spelling (`authorisation`, `behaviour`, `colour`, `-ise`), with a spell checker set to `en-US` in CI for `docs/`, `site/` and the frontend's templates. Keep an allowlist for finance terms and code names. Identifiers in code and APIs change only if it's safe: **a rename of a public JSON field is out of scope**.
- Agent answers: the system prompt requires US English, including when a user writes in another language.
- Add a line to `CONTRIBUTING.md`: user-facing text is US English, displayed times are UTC.

**Tests:** CI fails on `lang="sv"` and on the spell check. A review of the diff confirms that test fixtures with Swedish letters that exercise non-ASCII input are still in place.

## Acceptance

- [ ] `LICENSE` holds MIT, and the README and About link to it.
- [ ] No displayed date or time depends on the browser's zone, and times say UTC.
- [ ] Number formats don't depend on the browser's locale.
- [ ] No Swedish in the site, docs or UI, apart from test data that needs it.
- [ ] CI guards all of the above.

## Out of scope

A per-user time-zone or language setting. Owner's decision is one convention for everyone.
