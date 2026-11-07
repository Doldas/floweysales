# Acceptance verification for issue #208

This change fixes provenance being built with the requested/default provider instead of the *actual provider that supplied each row*. When leaderboard rows arrive from distinct providers, the provider badge, record URL and provenance fact must all describe the row's actual provider.

The new Go regression covers two tickers with the same company root but different exchanges, mixed per-row providers, an unknown listing with no invented exchange and preservation of old price/as-of dates. Previous API/UI tests cover separate discovery-versus-metric sources and unknown disclosure.

## Validation

`go test ./internal/server -run 'Test(Discovery|Selection|LeaderboardResponse)' -count=1`, then full repository CI.

The remaining wider claim (real merger of multiple upstream provider feeds) requires end-to-end provider fixtures. This PR is not itself a production live-data verification.
