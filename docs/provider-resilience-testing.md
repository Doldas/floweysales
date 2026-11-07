# Provider resilience testing

Market data comes from provider plugins that can time out, rate-limit, return
partial or malformed data, or serve old bars. This page describes how the
repository tests those cases without calling any real service, and what is
still open. It is part of issue #256.

## What CI covers (no network)

| Layer | Where | Covers |
| --- | --- | --- |
| Fixtures | `internal/market/markettest/testdata/` | Synthetic payloads in the exact plugin contract shape: healthy hourly bars, out-of-order bars, unfinished trailing bar, missing currency, financials with a missing quarter and a missing revenue, a symbol collision (`SHOP` on two exchanges). No vendor data, so they are redistributable (MIT). |
| Fault harness | `internal/market/markettest` | A `market.Backend` that serves fixtures and scripts faults per operation: error, 429 with a Retry-After hint, a provider that never answers (`Hang`, ended by the caller's context) and `Mutate` for partial or stale data. It counts calls so tests can assert on retries. |
| Replay tests | `internal/market/replay_test.go` | Quote provenance under outage, stale data, out-of-order and partial bars, missing metadata, clock skew, symbol collisions, recovery after faults, and empty-not-null financial reports. |
| HTTP client | `pluginsdk/httpx` tests | Bounded retries, Retry-After honored and capped, cancellation during the wait, request timeout, strict number parsing. Uses local `httptest` servers only. |

Run them with `go test -race ./internal/market/...` and, from `pluginsdk/`,
`go test -race ./httpx`.

## Behavior these tests pin down

- A quote is the newest complete bar by timestamp, not the last array element.
  Bars with a zero or missing close or time are skipped.
- A quote whose series names a different ticker than requested is refused
  (`BRK.B` and `BRK-B` count as the same instrument).
- A quote without a currency, with no usable bar, or dated in the future is
  refused. There is never a zero or synthetic price.
- Old data is served but flagged stale with its age; it is never presented as
  refreshed.
- On HTTP 429 the plugin HTTP helper waits for the provider's `Retry-After`
  (up to 5 seconds, `Client.MaxRetryWait`). A longer wait fails the call at once
  with the hint in the error (`StatusError.RetryAfter`) instead of retrying
  early. Auth errors are never retried.
- Numeric strings must be fully numeric: `12abc`, `NaN` and `Inf` are treated
  as missing, not as 12 or infinity.

## Adding a failure case

1. Add a small JSON file to `markettest/testdata/` (one shape per file).
2. Load it with `markettest.History` or `markettest.Load`, serve it with
   `markettest.New(...).Serve(...)`, and script faults with `Inject`.
3. Assert on what the user would see: an error, a stale flag, or a labeled
   fallback, never a silent zero.

## Not covered yet

Tracked on #256: delayed or out-of-order event streams and reconnects,
conflicting currencies or timestamps between two sources, a user-facing status
that separates unavailable, stale, delayed and refreshed data in every module,
instrument-switch cancellation tests in the frontend, opt-in live provider
checks (credentials, terms, rate limits), freshness and rate-limit metrics
with alerting, and an end-to-end quote, leaderboard, strategy, research test.


### Recorded fixture isolation

The `internal/market/markettest` fixture backend copies history bars when they are registered and each time they are returned. Mutating the caller's input slice or a fault-injected response therefore cannot change the original recording or subsequent replay reads. Search result slices are likewise snapshotted on registration. Live-provider checks remain opt-in and must respect upstream quotas.
