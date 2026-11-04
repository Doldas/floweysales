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

Karaktärernas regelstrategier (`family: persona`, med `persona` satt) ligger bland de inbyggda. Agenter kör dem med verktyget `run_rule_strategy` (`strategy_id`, `symbol`, `years` 3/5/10, `cost_bps`).

En strategi är data: villkor (pris, EMA/SMA, RSI, ATR, högsta/lägsta, Fibonacci-nivåer), stop, mål, tidsgräns och storlek. Fibonacci-nivåer mäts på den senaste *bekräftade* svängen (pivå med `swing_bars` staplar på var sida), så en nivå flyttas aldrig bakåt i tiden. Signaler på stängning fylls på nästa öppning; träffar en stapel både stop och mål antas stoppet först.

## Omvärld (World)

- `GET /api/world[?refresh=1]` – rubriker sorterade i teman (olja, räntor, geopolitik, tullar, AI/halvledare, Kina, kredit, tillväxt, valuta, metaller, mat/väder, hälsopolitik, reglering) med vilka sektorer och råvaror varje tema berör. Källor: en nyhets-plugin (`news`-capability) och den sparade webbsökningsnyckeln. Cachas i 15 minuter.
- `GET /api/world/company?symbol=XOM[&sector=Energy]` – hur temana når ett bolag, utifrån dess sektor (vald, `PAPER_SYMBOL_SECTORS` eller inbyggd lista) och dess egna rubriker.

Kopplingen tema → sektor/råvara är en fast tabell i `internal/market/world.go`, inte modellutdata: den säger vem som är exponerad och åt vilket håll om temats drivkraft stiger, inte vad som kommer att hända. Inga rubrikers tonalitet tolkas.

## Investeringsanalys

`GET /api/market/leaderboard?universe=…&strategy=…` rankar ett urval bolag på
sex faktorer. `GET /api/market/backtest?universe=…&strategy=…&top=10&rebalance=monthly|quarterly&cost_bps=10&years=3|5|10` (or `from=YYYY-MM-DD&to=YYYY-MM-DD`, UTC; the response carries `from`, `to`, `data_start`, `data_end` and a `notice` when the data is shorter than asked). `GET /api/market/backtest/batch?strategy=…&universes=a,b&periods=3,5,10` runs it over up to 4 universes and 6 periods (12 backtests at most) and adds a spread summary; see investor-automations. `GET /api/market/backtest/sensitivity?…&costs=0,10,25,50&tops=5,10,15` changes trading cost, holdings and rebalance one at a time around a backtest. Saved results: `GET|POST /api/market/backtest/runs`, `GET|DELETE /api/market/backtest/runs/{id}` and `GET …/{id}/export[?format=csv]` and `POST …/{id}/summary` (a model summary whose figures are checked against the stored result)
spelar upp samma strategi över historiska kurser och rapporter (rapporter
används först 60 dagar efter periodens slut) mot ett index och returnerar
nyckeltal, kurva, ombalanseringar och varningar. Båda läses med
`research.read`; se [investor automations](investor-automations.md#backtest).

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
