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
| **API integrations** | The AI provider (an OpenAI-compatible endpoint, a default model, the maximum model rounds and tool calls per answer, and a catalog of models with display labels), web search (see below), and the SEC EDGAR contact that SEC requests must carry. Credentials are encrypted on the server and never shown again; the screen only says whether a key is configured. |
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

## Web search

Under **API integrations → Web search** an administrator can set up several search providers at once. The research agent, agent teams and the Flow Studio web search node all use the same order and limits.

- **SearXNG** is free and keyless: point it at a SearXNG instance you run (for example `http://searxng:8080`) that allows the JSON format. You host it, and the upstream search engines' limits and terms apply.
- **Tavily** and **Brave Search** need an API key. Keys saved before this page existed keep working.
- **Google Search through Gemini** costs money. It asks Gemini, with Google Search grounding, to answer; FloweySales shows Gemini's answer (marked as written by the model) and the pages Gemini cited as their own kind of result. Google lists 5,000 grounded queries free each month on eligible paid plans and then $14 per 1,000 searches, plus Gemini tokens, and one prompt can run several searches. Check the current prices with Google: the numbers on the page are estimates you can edit. It is only used after you approve it and set a daily or monthly ceiling.
- **Google Custom Search** is a legacy option for existing customers only; Google closed it to new customers and ends it on January 1, 2027.

Each provider has an on/off switch, a **Test connection** button and a position in the order (arrows). Choose **Automatic** to try providers in order, with a limited number of fallbacks, or **Manual** to always use one. A provider that is rate limited or rejects its key is paused for a while. **Recent decisions** shows which provider answered and which were skipped and why (never the query). Testing a paid provider asks first, because the test is billed. The page shows searches and Gemini tokens separately, with an estimated cost.

A search result is only a title and a snippet. It is not a page the agent has read, and not a verified fact. In the sources list under an answer, such a result says which provider found it and that only a snippet was seen; a page Gemini cited says it was cited by Gemini and not opened. If no provider is set up, the agent says web search is unavailable and users are told to ask an administrator. Users never see keys or addresses.
