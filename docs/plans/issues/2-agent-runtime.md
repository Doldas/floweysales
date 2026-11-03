# Plan 2: Agent runtime

**Issues:** #194, #198, #202, #199.

**Why one plan:** all four depend on the same event stream from `internal/ai` and
the same team runner. The stream needs real identities (which call, which attempt,
which member, which model) before #198 and #199 can be correct.

## What happens today

**#194: the root cause is one line of frontend.** `AgentSession.handleChunk`
(`frontend/src/app/core/agent-session.ts`) handles `notice` with
`this.error.set(ev.content)`. A "Retrying, attempt 2 of 3" notice therefore becomes
the red error, and only the next `send()` clears it. There is no close button. Also:

- Task-incomplete and context-window messages use the same `notice` type, so the
  fix must not hide those.
- `completeWithRetry` streams deltas as they arrive, so text from a failed attempt
  can stay in the bubble and the retry appends to it. **To verify** by test.
- The notice contains the raw error, including the endpoint URL and part of the
  model's text (`internal/ai/agent.go:391`).
- Team runs drop notices: `applyTeamEvent` has no `notice` case.

**The 400 in the report cannot be prevented by validation here.** The endpoint's
own parser rejected the model's output, so that output never reached us. Retrying
with feedback, which `completeWithRetry` already does, is the right response.
Preflight helps with the calls we do receive, where we only check `json.Valid`
today: no check of required fields, types or unknown tool names against the schema.

**#198: the data already reaches the browser.** The team stream carries `arguments`,
`result` and `tool_error`, but `applyTeamEvent` keeps only name and status
(`MemberStep` in `team-view.ts`). Chat shows them. Neither view can correlate
reliably: events have no call id, so `finishStep` matches by tool name, and parallel
calls to one tool can swap results. Retry attempts and duration are not sent.
Results over `MaxToolResultBytes` (24,000) are replaced with a wrapper that does
record the original size.

**#202:** `RunTeam` takes a single `*Client`, but `Run` already copies the client,
so a per-member model is a small runtime change. The larger gaps: **teams are not
saved anywhere** (only a persona selection in memory, `TeamSession.selected`);
`ai_model_catalog` holds only names, with no capabilities; only `SYS_ADMIN` may pick
a model; and there is one endpoint per server. Tools are off for everyone else.

**#199:** tools return a string through `Execute`, so there is no way to stream a
nested run. `RunTeam` already has limits to reuse (6 members, 3 in parallel, chair
without tools, 10-minute timeout in the handler).

## Solution

A correlated event model first, then the UI on top of it, then tools for everyone,
then model profiles and saved teams, then delegation as a tool.

## Work packages

### WP1: Run status and error recovery (M, `fix/agent-run-status`) — #194

**Backend (`internal/ai`):**
- Add to `Event`: `Severity` (`info`, `warning`, `error`), `Code` (`model_output_rejected`,
  `tool_error`, `http_error`, `rate_limited`, `context_full`, `task_incomplete`), and
  `Attempt`/`MaxAttempts`.
- New `attempt_start` and `attempt_failed` events, and `attempt_recovered` when a
  retry succeeds. Sent `attempt_failed` also tells the UI to discard that attempt's
  partial text.
- Clean messages through `internal/redact` and drop the endpoint host from what
  users see. Keep the detail in the server log.
- Preflight in the tool loop, before dispatch: known tool name, valid JSON, and the
  tool's JSON schema (required fields, basic types). Failures go back to the model
  as the tool reply, using the mechanism that already handles invalid JSON. Keep the
  validator small; the schemas are simple `obj(...)` maps.

**Frontend:**
- Split `error` into `runStatus` (one line, replaced as state changes) and `notices`
  (a list that persists until dismissed). A pure reducer, tested with `node --test`.
- A retry shows as a quiet status line, and on success becomes "Recovered after 2
  attempts" in the steps list, with detail on expand. No red.
- `error` severity keeps the alert, with a close button (a real `<button>`, keyboard
  reachable, labeled). `task_incomplete` stays a warning: it is not a transient error.
- The final state comes from the run's end event.
- `applyTeamEvent` handles the same events per member.

**Tests:**
- Go: malformed payload, then 400, then success gives ordered events and one final
  answer; three failures give a terminal error; a preflight rejection does not
  execute the tool; the message contains no endpoint URL or key.
- Spec: the reducer clears retry state on success, keeps terminal errors, and drops
  partial text from a failed attempt.

### WP2: Tool-call inspection (M, `feature/tool-call-inspector`) — #198

- Add `CallID`, `Seq`, `Attempt`, `StartedAt` and `DurationMS` to `tool_start` and
  `tool_result`, using the model's `tc.ID` or a generated id. Add `Truncated` and
  `OriginalBytes`.
- `finishStep` and `applyTeamEvent` match by call id. This also fixes the
  swapped-results risk in chat.
- One shared component, `fs-tool-call`, extracted from `agent-chat.html`, used in both
  views: formatted and raw JSON views, copy, search within the output, status, timing,
  actor (member), and labels for truncated and redacted parts.
- Run tool arguments and results through `internal/redact` before they are sent.
- For "full output": hold full results of the current run in the browser only.
  Results above the model's limit are not retained today. Keeping them on the server
  belongs with the source registry (Plan 3 WP3), so link there later.

**Tests:** parallel calls to one tool stay matched; secrets are masked; the spec covers
the truncation label. **Depends on:** nothing. Ship it right after WP1.

### WP3: Tools for all users (M, `feature/user-tool-catalog`) — owner decision 1

Tools today are off for everyone but `SYS_ADMIN` (`ai.go:374`, `ai_team.go:135`), because some of them read
shared engine data. The code comment asks for a scoped catalog first, so this builds it.

| Class | Tools | For users |
| --- | --- | --- |
| Research data | Quotes, history, technicals, financials, SEC, provider research, calculators, `search_web`, leaderboard, backtests, strategies, macro, insider, events | Yes |
| Already per user | `get_wealth_plan`, domain providers (saved transactions) | Yes, reading only the caller's data through `agentUser` |
| Shared paper book | `get_portfolio_analytics` | Refuses non-admins today (`ai_investor_tools.go:152`). Keep it, with a clear message, until portfolios are per user |
| Server state | `get_engine_config`, `get_recent_ticks`, `get_market_stats` | No. They show the server's trackers and stop levels. Admin only |

- Replace `opts.DisableTools = true` with an `AllowedTools` list per class, in one function used by chat, teams and tasks.
- Per-user rate limits for costly tools. `search_web` uses one shared key, so one user must not be able to drain it. `SYS_ADMIN` sets the limits.
- Remove the wording that says users have no tools, and update `docs/` and `chatAgentOptions`.
- **Security review before merge:** check each tool in the first two classes for server-wide data, and `call_domain_provider` for cross-user access.

**Tests:** RBAC table in `internal/server/rbac_test.go`, covering each class per role. Adversarial tests alongside `internal/ai/adversarial_test.go`: user A's agent cannot read user B's plan or transactions, and a model calling a withheld tool is refused without running it.

### WP4: Model profiles and saved teams (L, `feature/model-profiles`) — #202, owner decision 6

`SYS_ADMIN` prepares the choices once; each user picks among them.

**Profiles (`SYS_ADMIN`, in Settings):**
- Table `ai_model_profiles (id, name, description, model, fallback_model, supports_tools, context_window, cost_input_per_mtok, cost_output_per_mtok, max_steps, max_tool_calls, enabled, is_default, position)`. Capability and cost fields are nullable, since unknown stays unknown.
- Examples an admin might create: "Fast and cheap", "Deep research", "Private (local)". Names and descriptions are the admin's.
- `model` must be in `ai_model_catalog`. A later `endpoint_id` column can add several endpoints without any change for users.

**Choosing (every user):**
- `GET /api/ai/profiles` returns enabled profiles: name, description, capabilities. Never the endpoint URL or key.
- `POST /api/ai/chat` and `/api/ai/team` take `profile_id`. `resolveChatEndpoint` allows any enabled profile for any signed-in user. Choosing a raw `model` stays admin-only.
- A profile select in the agent composer, next to the existing persona select. Users already choose personas; that stays.
- A new `/preferences` page for every signed-in user, since Settings is admin-only today: default profile, default persona, evidence policy (Plan 3 WP4).
- Flow agent nodes get `profile_id` too, using the same check. Today a node's `model` field is only checked against a name pattern (`flows.go:276`) and then used (`flows.go:1472`), so anyone who can edit a flow can pick a raw model, which chat forbids. Existing flows keep working: a saved raw model stays valid until an admin or the owner of the flow changes it. New choices use profiles.

**Saved teams (every user):**
- `agent_teams (id, user_id, name, chair, created_at, updated_at)` and `agent_team_members (team_id, position, persona_id, profile_id)`.
- CRUD under `/api/ai/teams`, scoped to the owner. `POST /api/ai/team` takes `team_id` as well as personas.

**Runtime:**
- `TeamMember` gets `Model` and `FallbackModel`, resolved from the profile. `runMember` copies the client and sets the model.
- Fallback triggers only on model-unavailable errors, is announced with a notice, and is never used to get around a budget.
- A disabled or deleted profile is never swapped silently: the run says so and offers the default.
- A profile without tool support on a member that needs tools is refused when saving, with a clear message.
- Limits: the profile's limits apply, plus a per-team `max_tool_calls_total`. A cost ceiling needs token usage, which the client does not read yet: add `usage` parsing and show "cost unknown" for endpoints that send none.
- Trace: `member_start` and the chat's status carry the profile and the actual model, so #202's trace works. The endpoint is not shown.

**Tests:** a normal user can use an enabled profile and is refused a disabled one or a raw model; the API never returns endpoint or key; mixed-profile integration test with a fake endpoint that records the model per request; fallback; a disabled profile on a saved team; budget stops the run; one user cannot read another's team.

### WP5: Research Agent delegates to a team (L, `feature/agent-delegate-team`) — #199

- New tool `delegate_to_team {team_id, goal, constraints, context}`, listed only for a user's own saved teams.
- `Execute` gains an emit path for nested events, so a tool can stream. Nested events
  carry `ParentCallID`, and the chat shows them under the delegating step.
- Safety, enforced in code rather than in the prompt:
  - Depth limit of 1: nested runs get a tool list without `delegate_to_team`, and a
    context value refuses recursion even if a tool list is wrong.
  - One delegation per run by default, charged to the parent's budget.
  - Cancellation follows the parent context.
  - The team's result is untrusted model output, delivered as data, like the chair's input.
- Result to the parent: per-member stance, answer, failures, and sources. The parent
  must attribute claims to the team, and disagreement is passed on, not averaged.
- A failed member gives a partial result; an all-failed team gives a tool error.

**Tests:** recursion refused; budget exhausted; cancel stops members; partial failure;
attribution present. **Depends on:** WP2, WP3, WP4, and Plan 3 WP3 for citations.

### WP6: Review what is admin-only (S, `docs/role-review`) — owner decision 10

The rule: `SYS_ADMIN` does what setting up the system requires, and the rest is the user's choice. This WP makes a table for the owner to decide on, and changes nothing by itself, since some items need data isolation first.

| Admin-only today | Proposed | Why |
| --- | --- | --- |
| AI endpoint, keys, catalog, profiles | Keep | Setup |
| Plugin install, updates, SMTP, user management | Keep | Setup |
| Whole Settings page | Split | User choices move to `/preferences` |
| Paper portfolio and live tracking (`liveMarkets`) | Decide | The paper book and trackers are shared. Opening them needs per-user data first |
| Community (`liveMarkets`) | Decide | May be gated by accident, tied to the same flag |
| Paper approvals, flow side-effect approval | Decide | Safety control. Do not widen without a review |

**To verify:** the full list, using each `RoleSysAdmin` check in `internal/server` and `Access` in the frontend.

## Acceptance mapping

| Issue | Covered by |
| --- | --- |
| #194 | WP1 entirely |
| #198 | WP2. "Linkage to source citations" comes with Plan 3 WP3 |
| #202 | WP4. Users choose among admin-made profiles, per the owner's decision |
| #199 | WP5, with WP3 for tools and Plan 3 WP3 for citations |

## Out of scope

Several AI endpoints per server: profiles leave room for it later. Persisting chat conversations on the
server, beyond what tasks already do.
