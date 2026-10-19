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

`GET /api/provider/endpoints` returnerar den faktiska whitelisten.
`GET /api/provider/research?provider=FINNHUB&operation=company_profile&symbol=NVDA`
är Finnhub bolagsprofil. Finnhub-nyckeln konfigureras på servern via
`FINNHUB_API_KEY` och skickas i headern `X-Finnhub-Token`.
`GET /api/provider/research?provider=BINANCE&operation=order_book&symbol=BTCUSDT`
returnerar ett litet publikt orderdjup utan API-nyckel.

Nya MCP-verktyg: `get_provider_endpoints` och `get_provider_research`.
Andra tillåtna operationer visas direkt i katalogen.

Inofficiella extra anrop: `YAHOO/market_news` använder en publikt nåbar,
odokumenterad Yahoo Finance-sökfunktion. `AVANZA/instrument_snapshot`
använder bara den redan existerande publika read-only adaptern och
fungerar enbart när `AVANZA_UNOFFICIAL_ENABLED=true`.

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

## Underhåll och tester

- `go test ./internal/market ./internal/mcp`: säkerställer att det som
  dokumenteras som ny tillgänglig operation existerar och att alla
  aktuella MCP-verktyg faktiskt finns i katalogen.
- `npm run smoke:api-pages --prefix frontend` kör Playwright med
  API Pages på lokal server för mobil (360px), surfplatta och desktop.
- Validering av HTML, CSS, JS och JSON ingår i GitHub Actions.
- Källa för leverantörsinformation och fullständiga referenslistor:
  Binance Spot, Finnhub Swagger, yahoo-finance2 och den inofficiella
  avanza-sdk-go – samtliga länkade längst ned i API-hjälpen.

Obs: Webbplatsens listor är en dokumenterad inventering av relevant
research-API, **inte en uttömmande lista över varje privat eller
handelsrelaterad endpoint** som varje extern leverantör kan ha.
