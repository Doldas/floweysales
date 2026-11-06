---
title: What is FloweySales?
order: 1
summary: What FloweySales is for, who it is for, what is available now and what it does not do.
last-reviewed: 2026-11-06
---

# What is FloweySales?

FloweySales is an open source, local-first workspace for investment research. You run it yourself; one program serves the web app, the data and the AI agent. It brings together:

- **Market data** for the instruments you follow: price charts, indicators, fundamentals and news themes.
- **A transparent company leaderboard** that ranks a list of companies and shows how each rank was reached.
- **Strategy tests**: rules and leaderboard strategies replayed over past prices.
- **An AI research agent** that calls data tools, shows every call, and lists the sources it used.
- **Flows**: saved, repeatable chains of data steps, agents and approvals.

It is a research aid. It does not give personal investment advice, and it does not place real orders. Live trading is not supported.

## Who it is for

Individual investors and small teams who want to look at companies with evidence in front of them: where a number came from, how a ranking was made, which sources an answer rests on. It is also for people who want to automate a routine, such as a weekly watchlist memo, without handing the decision to a machine.

## What is available now

This guide covers only what is built. Each page says what a screen does, what you can put in, what comes out and where the limits are.

| Area | What it does | Guide page |
| --- | --- | --- |
| Markets | Price chart, indicators, watchlist, fundamentals, quarterly financials, World news themes | [Markets](markets.md) |
| Leaderboard | Ranks a list of companies on six factors, with the method and data shown | [Leaderboard](leaderboard.md) |
| Strategies and backtests | Rule strategies on one instrument; leaderboard strategy backtests with batch checks, settings checks, saved runs and exports | [Strategies and backtests](strategies.md) |
| Flow Studio | Saved automations with versions, schedules, approvals and an audit trail | [Flow Studio](flows.md) |
| Agents | The Research Agent, agent teams of fictional investor characters, model choice and evidence modes | [Agents, teams and evidence](agents.md) |
| Provenance | How sources, dates and methods are shown, and what a check does not prove | [Provenance and trust](provenance.md) |
| Settings | Integrations, model profiles, people and roles, plugins and updates | [Settings and roles](settings.md) |
| Help and About | In-app help, contextual help panels, build information | [Help and About](help.md) |
| More areas | Command Center, Wealth plan, Paper portfolio, Inbox, Files, Community, Notifications | [More areas](more-areas.md) |
| Install and run | For administrators: ways to run, accounts, backups and updates | [Install, run and back up](install-and-run.md) |
| Glossary | Plain definitions of the terms used in the guide | [Glossary](glossary.md) |

## What to keep in mind

- **Research, not advice.** A high leaderboard score means a company is worth a closer look. A backtest tests an idea on the past. Neither is a recommendation.
- **AI can be wrong.** The agent summarizes what its tools returned. A citation check can confirm that a figure or a quoted passage appears in a cited source. It cannot confirm that the source is right or that the reasoning is sound.
- **Missing data is shown as missing.** Where a figure is unknown the screen says so. It is never replaced by zero.
- **All times are UTC.** Dates and times shown anywhere in the app, and in exports, are in UTC, whatever your computer's time zone is.
- **Some areas are only partly covered.** For example, not every screen shows its sources yet. [Provenance and trust](provenance.md) lists what is covered.

## Where to go next

New here? Start with [Getting started](getting-started.md). The project's own notes, such as the [roadmap](../roadmap.md), the [architecture](../architecture.md) and the [contribution guide](../../CONTRIBUTING.md), live in the repository and are for people who build or run FloweySales.
