# Agent personas and teams

An agent can answer as one of six **fictional investor characters**, each following a well-known school of thought, and several of them can work as a **team** on the same question. The characters are invented and carry no real person's name. They change what the agent looks at and how it talks; they do not change what it may do. Tools, grounding rules and the research-only boundary stay the same, and nothing here places an order.

| Character | Follows | Horizon | Looks at |
| --- | --- | --- | --- |
| Walter Ashgrove (`owner`) | The patient business owner | Decades | Moat, return on capital, owner earnings, debt, a conservative DCF |
| Bernhard Holm (`teacher`) | The classic defensive value investor | Years | Margin of safety; balance sheet, earnings record, P/E and P/B tests, net-net |
| Marty Fairbanks (`pickerupper`) | The everyday stock-picker | Years | Category, PEG, inventories, insider buying, repeatable growth |
| Dr. Isak Voss (`quant`) | The mathematician-quant | Short to medium | Price statistics, backtests, sample size, costs, overfitting |
| Margit Lindholm (`allocator`) | The institutional allocator | Decades | Macro regime, structural themes, resilience, portfolio fit |
| Cora Whitfield (`innovator`) | The disruption investor | About five years | Growth, cost curves, market size, runway, what must go right |

Every answer ends with a `STANCE:` line (`fits`, `partly`, `does_not_fit` or `insufficient_data`) that says how well the company fits that approach, never whether to buy it. The prompts forbid presenting as a real investor, reporting what a real investor holds or thinks, and telling the user to buy or sell. The definitions are in `internal/persona`.

## Process, portfolio and sources

Each character carries the documented **decision process** of its school of thought (in order), its **portfolio rules** (concentration, sizing, cash, turnover), the **sources** it rests on and a **reference portfolio**: the holdings its public counterpart has disclosed, dated and sourced. Portfolios are taken from SEC 13F filings and ARK's published daily holdings (`internal/persona/data`, refreshed by `scripts/refresh_reference_portfolios.py`). For the two characters whose counterpart has no portfolio to cite, the reference is the approach's portfolio rules.

The agent tool `get_reference_portfolio` returns a portfolio with its filer, as-of date, concentration and largest positions, and looks up whether a symbol is held. It always answers that this is a disclosure, not a current position or a view. The leaderboard universes `ref_owner`, `ref_quant`, `ref_allocator` and `ref_innovator` rank a company against a character's disclosed holdings; a backtest on them has hindsight bias, and their description says so. The Agent team page has a profile panel for each character. The full analysis, including what each source does and does not show, is in [investor-profiles.md](investor-profiles.md).

## Their strategies

Every character has strategies of its own and is told to **run them with its tools** and use the results as evidence, rather than just describing them. Two kinds exist:

- **Ranking strategies** weigh the leaderboard's six factors (value, quality, growth, momentum, timing, risk) across a list of companies. The agent runs `get_leaderboard` and `run_strategy_backtest` with the strategy id. These are factor approximations: they cannot see moats, management, ten-year records or the shelves of a shop, and the characters are told to say so.
- **Rule strategies** are entry, exit and stop rules on one instrument, replayed by the strategy engine over daily prices. The agent runs `run_rule_strategy` with the strategy id and a ticker, and gets the figures, the signal on the last bar and the bias warnings.

| Character | Ranking strategy | Rule strategy |
| --- | --- | --- |
| Walter Ashgrove (`owner`) | `owner_compounder`: steady margins first, then a fair price and calm volatility | `persona_owner_dip`: a washed-out dip in a company already judged wonderful, inside an uptrend |
| Bernhard Holm (`teacher`) | `teacher_defensive`: cheap earnings, profitable, low volatility | none (his tests are fundamental) |
| Marty Fairbanks (`pickerupper`) | `pickerupper_garp`: growth that is not over-priced | none |
| Dr. Isak Voss (`quant`) | `momentum` | `persona_quant_breakout` (100-day high breakout in an uptrend), `persona_quant_meanrev` (two-day washout in an uptrend) |
| Margit Lindholm (`allocator`) | `quality` | `persona_allocator_trend` (200-day trend filter) |
| Cora Whitfield (`innovator`) | `growth` | `persona_innovator_leader` (new 250-day high on rising volume) |

The rule strategies appear on the Strategies page under their character, where they can be run on any instrument, copied and changed. The ranking strategies appear in the leaderboard, and each has a Flow Studio playbook (`owner-compounders`, `teacher-defensive`, `pickerupper-garp`) whose memo is written by the matching character. A team lists the strategies its members will run. Like every replay, these are tests of an idea on past prices, not forecasts; the characters must report the trade count, the return against simply holding and the warnings, and say when the sample is too small.

## Where to use them

- **Research Agent:** choose *Answer as* above the message box. Replies are labelled with the character.
- **Agent team** (`/team`): pick two to six characters and ask one question. Members run side by side and never see each other's answers. A neutral **chair**, which has no tools, then compares what they wrote: where they agree, where they differ and what to check next. The chair also receives the stances as read from each member's closing line, so it cannot mislabel them.
- **Flow Studio:** an agent node can take a *character* (not with JSON output). A node without granted tools cannot run strategies, so the character is told to judge from the input only. Several agent nodes with different characters and a Merge node make a panel inside a flow.

## API

- `GET /api/ai/personas` lists the characters.
- `POST /api/ai/chat` takes an optional `persona` id.
- `POST /api/ai/team` takes `{question, personas: [...], chair?, symbol?, provider?, range?, price?}` and streams events tagged with `agent` (`member_start`, `delta`, `tool_start`, `tool_result`, `member_done` with `stance`, then the chair's, then `done`). It is rate limited to 4 runs a minute, gives each member at most 16 tool calls, and follows the same rules as chat about who may use tools (see [Agent tools by role](agent-tasks.md#agent-tools-by-role)).

Each character of a team can use its own model profile, and teams can be saved: see [Model profiles and saved teams](model-profiles.md).

## Delegating to a saved team

In chat, the research agent has a `delegate_to_team` tool that hands a goal to one of the signed-in user's saved teams. The agent passes the team's id or name; an unknown name returns the list of the user's teams so it can try again. The team runs on the same model endpoint as the chat, with each member's own profile, and the chair summary is included when the saved team has a chair.

- **One delegation per run, and no recursion.** A second call in the same run is refused. Members are started with a tool list that does not include `delegate_to_team`, and a nested run also refuses to claim a delegation, so a wrong tool list cannot make a team call a team.
- **Cancellation and budget.** The team stops when the chat is cancelled or times out (5 minutes at most). The call counts as one tool call of the parent and is metered like other shared tools; the members' own tool calls count against the same per-minute budget of the user. If the saved team has token, cost or tool-call ceilings, the delegated run stops at them and the result says which one ended it (see [Budget ceilings](model-profiles.md#budget-ceilings)).
- **What comes back.** Each member's name, stance, answer, failure and the sources it retrieved (each with a `source_id`); the chair summary; a count of failures. A member that fails leaves a partial result; if no member answers, the call is an error. The result carries a note telling the agent that the answers are model output, to attribute claims to the team and member by name, and to pass disagreement on rather than average it.
- **What the chat shows.** While the team works, the delegating step shows which member is working or has answered. Events of delegated work carry `parent_call_id`, and only member start and finish are forwarded.
- Not available through the MCP server. A flow agent node can delegate too, see below.

**Member sources in the citation registry.** The sources a member retrieved (search hits, pages it opened, SEC sections) are added to the chat's source registry when the delegation returns. They get ids (`[S5]`) like the agent's own, so the answer can cite them and the citation check treats them the same way: a member's search hit is only a snippet unless the member opened it, a filing keeps its class, and a figure or quotation must appear in the text the member received (up to 2,000 characters of it are kept). The Sources panel names the member who retrieved each one. The agent itself never saw that text, only the member's answer, so a sentence that repeats a member's claim cites the member's source only if the figure is in it. At most 12 sources per member are kept.

## Flow agent nodes

An agent node of a flow can choose a **model profile** (the same administrator-prepared profiles as chat and teams; see [Model profiles](model-profiles.md)) and **delegate to one of the flow owner's saved teams**. The node can reach only that team: the team is pinned in code, and a request for another team is refused. The team's members get the research tools a flow agent can be granted (never the owner's personal data or server state), and each runs on its own profile. The node needs a model that can call tools. A flow with these choices is saved only if the profile is enabled and the team is the saver's own, and both are checked again at every run; a disabled or deleted profile fails the node rather than switching models. A team node is allowed for ordinary users (it grants no admin tools).
