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
- `POST /api/ai/team` takes `{question, personas: [...], chair?, symbol?, provider?, range?, price?}` and streams events tagged with `agent` (`member_start`, `delta`, `tool_start`, `tool_result`, `member_done` with `stance`, then the chair's, then `done`). It is rate limited to 4 runs a minute, gives each member at most 16 tool calls, and follows the same rules as chat about who may use tools (accounts other than SYS_ADMIN get no tools).
