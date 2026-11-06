---
title: Getting started
order: 2
summary: Set up FloweySales, connect a model and a data provider, and ask a first research question.
last-reviewed: 2026-11-06
---

# Getting started

This page takes you from a fresh install to a first answer you can check. If someone else runs FloweySales for you, skip to [Your first session](#your-first-session).

## Who does what

FloweySales separates setting up the system from using it.

- An **administrator** (role SYS_ADMIN) sets up the system: the AI endpoint, the search and SEC settings, market-data plugins, people and roles, mail and updates. These are on the Settings page, which only administrators see.
- **Everyone** chooses how they work: which instrument to follow, which investor characters to ask, which model profile an answer uses, which evidence mode the agent follows. These choices are yours and do not depend on the administrator.

When authentication is turned off, a single local user has all the access of an administrator.

## Set up the system (administrators)

**Requirements:** Go 1.27.1 or newer and Node.js with npm (Node is only needed to build the web app). Alternatively, run the container stack described in [Docker and Podman development stack](../containers.md).

1. From the repository root, run `make run`. It builds the web app, builds the Go program and starts the server on port 8080.
2. Open `http://localhost:8080`.
3. Open **Settings**, then **API integrations**, and connect an AI endpoint that speaks the OpenAI-compatible format. Credentials are encrypted on the server and never shown again.
4. Open **Settings**, then **Plugins**, and install at least one market-data provider. Without a provider there are no quotes, history, search or statements. Some providers rely on unofficial interfaces; installing one asks you to accept its terms first. See [Provider plugins](../plugins.md).
5. Optionally, open **Model profiles** and prepare a few named model choices, such as "Fast and cheap" and "Deep research", so that users can pick by name. See [Model profiles](../model-profiles.md).
6. To expose FloweySales to other people, turn on authentication and create their accounts first. See [Database and authentication](../database-and-auth.md). Do not expose the server to the internet without HTTPS.

[Settings and roles](settings.md) describes each tab. For backups, updates and other ways to run FloweySales, see [Install, run and back up](install-and-run.md).

## Your first session

1. **Pick an instrument.** Use the search box in the top bar. Type a company name or ticker. The provider is shown next to each result, because the same ticker can mean different companies on different exchanges.
2. **Open Markets.** You see the price history, key figures and indicators for the instrument. The status badge in the top bar tells you how fresh the prices are. See [Markets](markets.md).
3. **Open Research Agent** and ask something specific, such as "What changed in the latest quarterly report?". The agent calls data tools; each call appears as a step you can open.
4. **Check the sources.** Open the Sources list under the answer. Look at the dates, and read the passage that a number comes from before you rely on it. See [Agents, teams and evidence](agents.md).
5. **Look at the Leaderboard** on the Markets page. Pick a list of companies and a strategy, then open "How this ranking works". See [Leaderboard](leaderboard.md).

## Finding your way around

- The **sidebar** groups the screens: Command Center, Research Agent and Agent team; Flows and Inbox under Automation; Markets, Strategies, World, Wealth plan, Financials and Paper portfolio under Markets; Files and Community under Workspace. Help, About, Notifications and Settings sit at the bottom. Which entries you see depends on your role and permissions.
- **Ctrl/⌘ K** opens the command palette. It jumps to a screen or an action.
- **Ctrl/⌘ J** opens the agent dock, a side panel with the agent on every page. The "Ask the agent" button at the top of the sidebar does the same.
- The **"?" buttons** next to some page titles open a short help panel for that screen without leaving it. See [Help and About](help.md).
- Light and dark themes follow your system setting and can be switched in the top bar.

## What you will not find

FloweySales does not place real orders and does not offer advice. The Paper portfolio uses simulated fills only. Screens that rank or score say so, and say what the score is relative to.

Next: the [tutorial from ranking to backtest](tutorial-research-a-company.md).
