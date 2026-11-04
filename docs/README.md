# Documentation index

## User guide

Written for people who use FloweySales. It describes what is built, and is published at https://doldas.github.io/floweysales/guide/. Build it locally with `node scripts/build-docs.mjs --check`.

| Page | Covers |
| --- | --- |
| [What is FloweySales?](guide/index.md) | Purpose, what is available, limits |
| [Getting started](guide/getting-started.md) | Setup, roles, a first session |
| [Markets](guide/markets.md) | Chart, watchlist, feed status, fundamentals, financials, World |
| [Leaderboard](guide/leaderboard.md) | Lists, strategies, method, data origin |
| [Strategies and backtests](guide/strategies.md) | Rule strategies, leaderboard backtests, saved runs |
| [Flow Studio](guide/flows.md) | Automations, versions, approvals |
| [Agents, teams and evidence](guide/agents.md) | Research Agent, teams, evidence modes |
| [Provenance and trust](guide/provenance.md) | Source badges, coverage, what checks prove |
| [Settings and roles](guide/settings.md) | Administrator setup and personal choices |
| [More areas](guide/more-areas.md) | Command Center, Wealth plan, Paper portfolio, Inbox, Files, Community, Notifications |
| [Help and About](guide/help.md) | In-app help, the documentation site |

Every guide page has `last-reviewed` front matter. Change the guide in the same PR as the behavior it describes.

## Technical notes

For people who build or operate FloweySales: [architecture](architecture.md), [roadmap](roadmap.md), [vision](vision.md), [containers](containers.md), [database and authentication](database-and-auth.md), [provider plugins](plugins.md), [model profiles](model-profiles.md), [agent evidence](agent-evidence.md), [Flow Studio internals](agent-flow-studio.md), [provenance coverage](provenance-coverage.md), [automatic updates](auto-updates.md), [GitHub operations](github-operations.md), [decision records](adr/) and [issue plans](plans/issues/README.md). Planned work is in the roadmap and the plans; the user guide does not describe it.
