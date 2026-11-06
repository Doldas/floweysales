---
title: Settings and roles
order: 9
summary: What administrators set up on the Settings page, and what each person chooses for themselves.
last-reviewed: 2026-11-06
---

# Settings and roles

FloweySales divides the work between two kinds of choice:

- **Setting up the system** belongs to administrators (role SYS_ADMIN). It is done once and affects everyone: the AI endpoint, search and SEC settings, market-data plugins, people, mail and updates.
- **Choosing how you work** belongs to each user: the instrument you follow, the investor characters you ask, the model profile and evidence mode for an answer, your saved teams, strategies, flows and saved backtests.

The Settings page appears in the sidebar only for administrators. When authentication is turned off, the single local user has administrator access.

## Roles and permissions

There are two built-in roles. A **USER** gets research with public market data: Markets in the research view with a watchlist, Strategies, Leaderboard, Agents, World, Wealth plan and Financials, plus whatever flow permissions the role grants. A **SYS_ADMIN** also gets the live-market tools, the paper portfolio, Community, flow administration and Settings. Private files and notifications need a signed-in account.

Flows use three permissions that an administrator can assign through roles: **flow read**, **flow write** and **flow run**. See [Flow Studio](flows.md).

## The Settings tabs

| Tab | What it holds |
| --- | --- |
| **API integrations** | The AI provider (an OpenAI-compatible endpoint, a default model, the maximum model rounds and tool calls per answer, and a catalog of models with display labels), the web search provider and key, the SEC EDGAR contact that SEC requests must carry, and the optional **Vision model** that reads scanned PDF pages and charts or tables that are pictures (see [Reading PDFs and scanned reports](../pdf-vision.md)). Credentials are encrypted on the server and never shown again; the screen only says whether a key is configured. |
| **Model profiles** | Named model choices such as "Fast and cheap" or "Deep research". Users and agent teams pick a profile by name; they never see the endpoint or the key. Each profile can name a fallback model, whether the model supports tools, its context size and costs (unknown stays unknown), and lower step and tool-call budgets. See [Model profiles and saved teams](../model-profiles.md). |
| **User management** | Create, invite and manage accounts. |
| **Role management** | Define roles. |
| **Permission management** | Assign permissions to roles. |
| **Mail configuration** | The SMTP server used to send email, such as invitations, tracker alerts and flow emails. Every message uses the same FloweySales design, with a plain-text version for clients that cannot show HTML. See [Email rendering](../email-rendering.md). |
| **Trading limits** | Limits and costs for the **paper** portfolio only, and a reset of the simulated account. These apply to simulated orders. |
| **Plugins** | Installed market-data providers and the plugin store. Install, update, restore the version an update replaced, uninstall, and check for updates. Each plugin shows whether it is compatible with this FloweySales, and after an app upgrade a report lists which plugins were kept and verified, need attention or are not run. A plugin that does not fit this version is kept with its settings and data but not run, and an incompatible version cannot be installed. Installing a provider that relies on an unofficial interface asks you to accept its terms first. See [Provider plugins](../plugins.md). |
| **Updates** | The installed version, whether a newer one is available, what changed, and, when the host updater is installed, install with backup and automatic rollback. See [Automatic updates](../auto-updates.md). |

## What users set for themselves

- **Notifications** (sidebar, for signed-in accounts): email alerts for tracker signals. You subscribe to the trackers you follow and choose which events send an email. Nothing is sent until you subscribe. Alerts need the administrator's mail configuration.
- **Evidence mode** and **model profile** in the chat; see [Agents, teams and evidence](agents.md).
- **Saved teams**, **saved strategies**, saved periods and **saved backtest runs**, each owned by you.
- **Theme:** light or dark, in the top bar. The choice is stored in your browser.

## Time and language

Every date and time in the app and in exports is shown in UTC, whatever your computer's time zone is. The interface is in US English. There is no per-user time zone or language setting.
