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

## Where to use them

- **Research Agent:** choose *Answer as* above the message box. Replies are labelled with the character.
- **Agent team** (`/team`): pick two to six characters and ask one question. Members run side by side and never see each other's answers. A neutral **chair**, which has no tools, then compares what they wrote: where they agree, where they differ and what to check next. The chair also receives the stances as read from each member's closing line, so it cannot mislabel them.
- **Flow Studio:** an agent node can take a *character* (not with JSON output). Several agent nodes with different characters and a Merge node make a panel inside a flow.

## API

- `GET /api/ai/personas` lists the characters.
- `POST /api/ai/chat` takes an optional `persona` id.
- `POST /api/ai/team` takes `{question, personas: [...], chair?, symbol?, provider?, range?, price?}` and streams events tagged with `agent` (`member_start`, `delta`, `tool_start`, `tool_result`, `member_done` with `stance`, then the chair's, then `done`). It is rate limited to 4 runs a minute, gives each member at most 16 tool calls, and follows the same rules as chat about who may use tools (accounts other than SYS_ADMIN get no tools).
