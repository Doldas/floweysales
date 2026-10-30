# ADR 0001: Agentic workspace shell on Bootstrap 5

- **Status:** Accepted
- **Date:** 2026-10-30

## Context

The web UI was a single root component (`app.ts`, ~900 lines with its template) that switched between ten tabs, styled with Angular Material and four neon "cyber" themes. Agent features were secondary: the assistant was a floating panel tied to one tab, Flow Studio was an admin tab, and human-in-the-loop decisions (flow side-effect approvals, four-eyes paper-trade approvals) were either buried in Flow Studio or had no UI at all. Material's dense component styling also made a calm, professional financial look hard to reach, and the components carried hundreds of hard-coded colours.

## Decision

1. **Bootstrap 5.3 (SCSS) and Bootstrap Icons replace Angular Material and the CDK.** Only the Bootstrap modules in use are compiled. No Bootstrap JavaScript or Popper: dialogs use the native `<dialog>` element (focus trap and Escape for free), and toasts, menus and the command palette are small signal-based Angular components.
2. **Design tokens.** Every colour is a `--fs-*` custom property defined for light and dark (`data-bs-theme`). Components never use raw colours; SVG canvases use classes because CSS variables do not work in SVG presentation attributes.
3. **Router with lazy pages and hash URLs.** Each workspace is a lazy-loaded route (`#/flows`, `#/markets`, …). Hash URLs work from the embedded Go file server and any static host without a server-side SPA fallback. The session role is resolved by an app initializer, so `canMatch` guards mirror the server's authorization synchronously.
4. **Agent-first information architecture.**
   - *Command Center* (home): ask-the-agent hero, KPIs, agent run activity across flows, automations, live trackers, flow templates and recent workspace files.
   - *Research Agent*: one `AgentSession` service owns the conversation, rendered by both the dock (available on every page) and a full page, so a question continues wherever the user goes. Tool calls render as an expandable step timeline with arguments, results and timing.
   - *Inbox*: every decision an agent waits on, i.e. flow side effects that need approval and paper-trade proposals that need a second SYS_ADMIN.
   - *Command palette* (Ctrl/⌘ K): navigate, ask the agent, open a ticker, open or run a flow, start from a template.
   - Agent hand-offs from context: "Analyze with agent" on Markets and Financials, "Ask agent about this file" in Workspace.
5. **State services instead of a god component.** `MarketState` (instrument, trackers, history, ticks, indicators), `ActivityStore` (flows, runs, approvals), `AgentSession`, `NotifyService` and `Access` (role capabilities) are shared by pages.
6. **Role-aware research mode.** USER accounts cannot use trackers, the SSE stream or the paper book, so `MarketState` runs in research mode with a local watchlist and provider history. USER accounts now get the chart, indicators and insights instead of statements only.

## Consequences

- Initial bundle is ~760 kB raw (~155 kB transferred); Markets and Flows load lazily.
- Feature components kept their logic and public selectors (`fse-flow-studio`, `fse-ledger`, …). Their templates and styles were converted, so Flow Studio's pure helpers and their `node --test` suites are unchanged.
- Browser tests share one mock backend (`frontend/smoke-fixtures.mjs`). `smoke:a11y` now audits every page in both themes plus the dock and palette (WCAG 2.1 A/AA), and `smoke:responsive` checks every page at seven viewports and the USER research path.
- Deep links such as `#/flows?flow=7&tab=runs` and `#/flows?template=risk` are consumed on arrival, so a reload does not re-apply a template over edits.
- Two pre-existing defects surfaced and were fixed: the chart's slow EMA used the fast EMA's smoothing factor, and the markdown renderer italicised text between underscores inside identifiers such as `get_sec_facts`.
