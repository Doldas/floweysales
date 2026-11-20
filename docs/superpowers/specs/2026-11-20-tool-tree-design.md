# Tool tree: show the AI only the tools it needs

Status: draft, awaiting approval.

## Why

Local models are the target. gpt-oss runs here with a 32k context window, and today
`ai.ToolDefs()` sends all 37 tool schemas on every request. That leaves less room for
the conversation, and every tool or skill added makes it worse. The goal is to add many
more tools and skills while the model still sees only what the current message needs.

## Goals

- The tool schemas sent per request stay small and roughly constant as the catalogue grows.
- On a page, the AI already has that page's tools, with no extra round trip.
- A request that crosses pages still works.
- Skills use the same mechanism: a one-line entry is always visible, and the body loads on demand.

## Non-goals

- No new tools or skills in this work. It only reorganises the existing 37.
- No change to which tools a user may use. The existing per-user scoping (`scopeAgentTools`) still applies on top.
- No automatic guessing of categories from the user's message.

## Anti-goals

- A tool that works today must not become unreachable.
- Running without a page (API, MCP, flows, tasks) must behave exactly as today unless it opts in.

## Approaches considered

- **A. The AI browses the tree itself.** Same on every page, but each lookup costs a round trip, and weak local models may pick wrongly or skip the lookup.
- **B. The page decides.** No lookups, but cross-page requests fail.
- **C. The page decides the default, plus one `more_tools(category)` call.** Chosen by the user. Most messages cost nothing extra, and cross-page requests still work.

## Design (approach C)

1. **Tree.** Each tool gets a category path such as `collections`, `research/sec`, `valuation`, `market`, `providers`. Sub-categories are allowed. A tool belongs to exactly one category. The tree is a Go table in `internal/ai`, checked by a test that every tool in `ToolDefs()` has a category and every category exists.
2. **Always-on set.** A few cross-cutting tools (symbol search, `more_tools`) are always sent. Everything else is page-gated.
3. **Page awareness.** The chat request gains an optional `page` field (a route name from the frontend). The backend maps page to categories. No page, or an unknown page, means no gating, which is today's behaviour.
4. **Escape hatch.** `more_tools(category)` returns a short description of the category's tools and adds them to the set sent on the following turns of that run. The prompt carries a one-line-per-category index so the model knows what exists.
5. **Enforcement.** The agent refuses a call to a tool that was never offered in the run, so an invented call cannot bypass the gating. This builds on the existing `AllowedTools` filter in `agent.go`.
6. **Skills.** The same table holds skills as entries: name, one-line description and category. `more_tools` can open one, and its full text then enters the context. No skill system exists in the backend yet, so this part is a hook only, built once there is a first skill to load.

## Measuring

A test records the serialized size of the tools sent for each page, with a ceiling, so growth past the budget fails CI. Starting point to check: the always-on part plus the largest page should fit well inside 32k alongside the system prompt and history.

## Left to the builder

- Exact category names and which page maps to which.
- Wording of the index line and the `more_tools` result.
- Whether MCP clients (`internal/mcp`) and plugin-provided tools join the tree in the first slice or the second. My suggestion is the second.
