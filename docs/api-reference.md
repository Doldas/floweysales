# API- och providerkatalog (GitHub Pages)

[Öppna den publika API-hjälpen](https://doldas.github.io/floweysales/api.html).

Katalogen är lagrad i `site/api-catalog.json` och används av
`site/api.html` / `site/api.js`. De **implementerade** MCP-verktygen
och den nya provider-gatewayens whitelist jämförs automatiskt med
katalogen av Go-tester. GitHub Pages publicerar webbplatsen när `main`
uppdateras. Den hostar inte själva REST-backenden eller MCP-servern.

## Skillnad mellan olika endpoints

1. **Application REST** (`/api/*`): endpoint finns faktiskt i Go-servern,
   är normalt sessionsskyddad och styrs av rollen USER/SYS_ADMIN.
2. **MCP** (`tools/list`, `tools/call`): samma gemensamma AI-verktygskatalog
   som agenten. En extern MCP-server behöver nätverks- och backendåtkomst
   samt giltig autentisering där sådan krävs.
3. **Upstream provider**: en extern leverantör har annonserat eller
   dokumenterat API-operationen. Den är **inte automatiskt implementerad**
   i FloweySales. Läs statusfältet på varje rad; `upstream` betyder
   uttryckligen att appen/MCP **inte** kan anropa den.

## Nytillkomna anslutna read-only operationer

Operationerna deklareras av de installerade provider-pluginen (deras
`plugin.json`, se [plugins](plugins.md)); en provider utan installerat
plugin har inga operationer. `GET /api/provider/endpoints` returnerar den
faktiska whitelisten för installerade plugin.
`GET /api/provider/research?provider=FINNHUB&operation=company_profile&symbol=NVDA`
är Finnhub bolagsprofil. Finnhub-nyckeln konfigureras på servern via
`FINNHUB_API_KEY` och skickas i headern `X-Finnhub-Token`.
`GET /api/provider/research?provider=BINANCE&operation=order_book&symbol=BTCUSDT`
returnerar ett litet publikt orderdjup utan API-nyckel.

Nya MCP-verktyg: `get_provider_endpoints` och `get_provider_research`.
Andra tillåtna operationer visas direkt i katalogen.

Inofficiella extra anrop: `YAHOO/market_news` använder en publikt nåbar,
odokumenterad Yahoo Finance-sökfunktion. `AVANZA/instrument_snapshot`
använder Avanza-pluginens publika read-only ögonblicksbild och finns bara
när Avanza-pluginet är installerat (installationen kräver att villkoren
godkänns).

## Strategier (Strategies)

Regelbaserade strategier för ett instrument, återspelade över dagliga kurser (`internal/strategy`). Inga order läggs.

- `GET /api/strategies` – inbyggda Fibonacci-strategier (`presets`) och användarens sparade (`custom`).
- `POST /api/strategies`, `PUT /api/strategies/{id}`, `DELETE /api/strategies/{id}` – spara, ändra och ta bort egna strategier (högst 30 per användare, tabellen `custom_strategies`).
- `GET|PUT|DELETE /api/admin/trading-settings` – (SYS_ADMIN) pappershandelns gränser och avgifter som sparats i Settings; ersätter `PAPER_*` och `TRADE_COMMISSION_*` från miljön, gäller direkt, DELETE återgår till miljön. `POST /api/admin/trading-settings/reset-account` med `{"confirm":"RESET"}` raderar all pappershandelshistorik.
- `PUT|DELETE /api/admin/plugins/{id}/env/{name}` – (SYS_ADMIN) spara eller ta bort en inställning som pluginet deklarerar, krypterat.
- `POST /api/strategies/backtest` – `{strategy_id | definition, symbol, provider?, range: 3Y|5Y|10Y, cost_bps?}`. Svaret innehåller nyckeltal, affärer, kurvor och vad reglerna säger på sista stapeln, inklusive Fibonacci-nivåerna för den senaste svängen. `walk_forward` delar historiken i fyra följande fönster och redovisar affärer, träffsäkerhet och avkastning per fönster mot buy and hold, så att man ser om resultatet bara hänger på en period.
- `POST /api/strategies/montecarlo` – the backtest body plus `{method: trade_bootstrap|trade_shuffle, runs, trades, seed, drawdown_limit, extra_cost_bps, dispersion_scale, delay_bars, regime_share_pct, regime_scale, position_scale}`. Replays the strategy once and resamples its closed trades with a seed into percentile bands, drawdown distribution and probabilities of loss, ruin and drawdown breach. The replay comes back unchanged as `observed`; refused with 422 below 20 closed trades. Details in investor-automations.

Karaktärernas regelstrategier (`family: persona`, med `persona` satt) ligger bland de inbyggda. Agenter kör dem med verktyget `run_rule_strategy` (`strategy_id`, `symbol`, `years` 3/5/10, `cost_bps`).

En strategi är data: villkor (pris, EMA/SMA, RSI, ATR, högsta/lägsta, Fibonacci-nivåer), stop, mål, tidsgräns och storlek. Fibonacci-nivåer mäts på den senaste *bekräftade* svängen (pivå med `swing_bars` staplar på var sida), så en nivå flyttas aldrig bakåt i tiden. Signaler på stängning fylls på nästa öppning; träffar en stapel både stop och mål antas stoppet först.

## Omvärld (World)

- `GET /api/world[?refresh=1]` – rubriker sorterade i teman (olja, räntor, geopolitik, tullar, AI/halvledare, Kina, kredit, tillväxt, valuta, metaller, mat/väder, hälsopolitik, reglering) med vilka sektorer och råvaror varje tema berör. Källor: en nyhets-plugin (`news`-capability) och den sparade webbsökningsnyckeln. Cachas i 15 minuter.
- `GET /api/world/company?symbol=XOM[&sector=Energy]` – hur temana når ett bolag, utifrån dess sektor (vald, `PAPER_SYMBOL_SECTORS` eller inbyggd lista) och dess egna rubriker.

Kopplingen tema → sektor/råvara är en fast tabell i `internal/market/world.go`, inte modellutdata: den säger vem som är exponerad och åt vilket håll om temats drivkraft stiger, inte vad som kommer att hända. Inga rubrikers tonalitet tolkas.

## Investeringsanalys

`GET /api/market/leaderboard?universe=…&strategy=…` rankar ett urval bolag på
sex faktorer. `GET /api/market/backtest?universe=…&strategy=…&top=10&rebalance=monthly|quarterly&cost_bps=10&slippage_bps=0&years=3|5|10` (or `from=YYYY-MM-DD&to=YYYY-MM-DD`, UTC; the response carries `from`, `to`, `data_start`, `data_end` and a `notice` when the data is shorter than asked). `GET /api/market/backtest/batch?strategy=…&universes=a,b&periods=3,5,10` runs it over up to 4 universes and 6 periods (12 backtests at most) and adds a spread summary; see investor-automations. Add `progress_id=<your own id>` and poll `GET /api/market/backtest/progress?id=…` for `{stage, done, total, finished}` while it runs (404 when unknown). `GET /api/market/backtest/sensitivity?…&costs=0,10,25,50&tops=5,10,15` changes trading cost, holdings and rebalance one at a time around a backtest. `GET /api/market/backtest/montecarlo?…&method=block&runs=2000&seed=42` resamples the backtest's daily returns into seeded simulated paths and returns percentile bands and downside probabilities next to the unchanged backtest metrics. Saved results: `GET|POST /api/market/backtest/runs`, `GET|DELETE /api/market/backtest/runs/{id}` and `GET …/{id}/export[?format=csv]` and `POST …/{id}/summary` (a model summary whose figures are checked against the stored result)
spelar upp samma strategi över historiska kurser och rapporter (rapporter
används först 60 dagar efter periodens slut) mot ett index och returnerar
nyckeltal, kurva, ombalanseringar och varningar. Båda läses med
`research.read`; se [investor automations](investor-automations.md#backtest).

`GET|PUT /api/market/leaderboard/preferences` reads and saves the signed-in user's own leaderboard objective and constraints; see [investor automations](investor-automations.md#your-objective-and-constraints). `GET /api/market/leaderboard?preferences=off` ranks without them.

`GET /api/plan`, `PUT /api/plan/profile` och `POST /api/plan/preview` hanterar
den inloggade användarens egen förmögenhetsplan (fas, målallokering och
simulering); se [wealth plan](wealth-plan.md). `GET /api/portfolio/analytics` (SYS_ADMIN)
mäter pappersportföljens risk och avvikelse från planen.

## Säkerhet och tillgänglighet

- En **whitelist** ersätter godtycklig extern URL-forwarding.
- Binance använder bara `data-api.binance.vision` offentliga spot-data.
- Finnhub-nycklar förvaras på servern, skickas via header, aldrig i
  API-svaret eller en query-param i FloweySales.
- Externa omdirigeringar blockeras; svar är max 512 KiB. Listor kapas till
  50 poster. Symboler och datum valideras före nätverksanrop.
- Varken Avanza BankID, kontodata, orderläggning eller privata endpoints
  exponeras av denna gateway.
- Upstream-begränsningar såsom 429, betalda nivåer, geografiska regler,
  åldrad data, börsens öppettider och licenser gäller fortfarande.
- Att en upstream endpoint finns i officiell Swagger betyder inte att
  den är tillgänglig inom användarens avtal.
- Svar är *provider-sourced*, inte verifierade SEC-tal, revisionsdata
  eller rekommendationer om köp/sälj.

## Plugin-administration (SYS_ADMIN, kräver inloggning)

| Metod | Sökväg | Beskrivning |
|---|---|---|
| GET | `/api/providers` | Installerade providers: kapaciteter, om de är konfigurerade, pollintervall |
| GET | `/api/admin/plugins` | Installerade plugin, senast hämtade butikskatalog och trackers per plugin |
| POST | `/api/admin/plugins/check` | Hämtar butikskatalogen nu (sök efter uppdateringar) |
| POST | `/api/admin/plugins/install` | `{"id":"yahoo","version":"","accept_terms":true}` installerar eller uppdaterar; tom version = senaste. 409 med `terms` om villkor måste godkännas |
| POST | `/api/admin/plugins/{id}/rollback` | Återställer versionen som senaste uppdatering ersatte (ingen nedladdning); 409 om det inte finns någon. `GET /api/admin/plugins` ger även `upgrade_report` efter en appuppgradering |
| DELETE | `/api/admin/plugins/{id}` | Avinstallerar; 409 med `trackers` om aktiva trackers använder providern, `?force=true` bekräftar |

En provider vars plugin inte är installerat ger HTTP 404 på `/api/history`,
`/api/financials`, `/api/quote` och `/api/provider/research`; saknad
konfiguration (t.ex. API-nyckel) ger 503.

## Underhåll och tester

- `go test ./internal/mcp`: säkerställer att alla MCP-verktyg finns i
  katalogen. Providerdelen av katalogen beskriver de officiella pluginen i
  [floweysales-plugins](https://github.com/Doldas/floweysales-plugins), vars
  `plugin.json` är den bindande listan över operationer.
- `npm run smoke:api-pages --prefix frontend` kör Playwright med
  API Pages på lokal server för mobil (360px), surfplatta och desktop.
- Validering av HTML, CSS, JS och JSON ingår i GitHub Actions.
- Källa för leverantörsinformation och fullständiga referenslistor:
  Binance Spot, Finnhub Swagger, yahoo-finance2 och den inofficiella
  avanza-sdk-go – samtliga länkade längst ned i API-hjälpen.

Obs: Webbplatsens listor är en dokumenterad inventering av relevant
research-API, **inte en uttömmande lista över varje privat eller
handelsrelaterad endpoint** som varje extern leverantör kan ha.

`GET /api/market/indices` lists the index registry (S&P 500, Nasdaq-100, OMXS30) **for the signed-in user**: provider, methodology, constituent source, as-of date (UTC), counts, exclusions, licensing, history, warnings, `available`, `sources` (`csv`, and `fmp` where Financial Modeling Prep is supported) and `source` (what the user supplied: provider, source name, as-of, import time, count, the licence text they confirmed, `point_in_time`, `history_from`, `unverified`). FloweySales ships no constituent list, so an index is `unavailable` until the user supplies one; `GET /api/market/leaderboard?universe=<index id>` and the backtest then answer 422 (leaderboard) or 400 (backtest) with the reason. See [provenance coverage](provenance-coverage.md#index-universes-258).

The user supplies data through their own routes, open to every signed-in user and scoped to them (an administrator has no access to another user's list or key): `PUT /api/market/indices/{id}/csv` (body: `csv`, `as_of`, `source_name`, `license_terms`, `license_confirmed`; 422 with `problems` when refused), `POST /api/market/indices/{id}/fmp` (body: `license_terms`, `license_confirmed`; uses the saved key; 502 when FMP refuses the key or plan), `DELETE /api/market/indices/{id}/source`, and `GET|PUT|DELETE /api/market/indices/fmp-key` (the key is write-only: `GET` returns `has_key` only, and the key is stored encrypted and never logged). A backtest on an index returns `membership` (`mode` of `point_in_time`, `point_in_time_with_fallback` or `current_members`, rebalance counts and a note).
