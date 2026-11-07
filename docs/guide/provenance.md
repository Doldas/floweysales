---
title: Provenance and trust
order: 8
summary: How FloweySales shows where data comes from, which screens are covered today, and what a check or a score does not prove.
last-reviewed: 2026-11-04
---

# Provenance and trust

Provenance means the origin of a number or a sentence: who reported it, when, how it was calculated, and whether an AI model wrote it. FloweySales tries to answer "where does this come from?" on the screen where you see the value, instead of in a separate document.

This page describes what exists today and what does not.

## The source badge

Where a screen is covered, a small badge sits next to the value. Select it with the mouse or the keyboard to open the details; press Escape or click elsewhere to close it. Depending on what is known, the details show:

| Line | Meaning |
| --- | --- |
| What this is | **Reported by a source**, **Calculated**, **Assumed**, **Estimated**, or **Written by an AI model, not a source** |
| Provider | The data source, marked official or unofficial where that is known |
| Document | A filing or document identifier, such as an accession number |
| Published, As of, Retrieved | Times in UTC: when the source published it, the date the value applies to, and when FloweySales fetched it |
| How calculated | The name and version of the method, and its formula |
| Built from | The values a calculated figure was made from |
| Note | Anything the reader should know, such as what is partial |

The badge text tells you how much is known. **Source** means the origin is recorded; **Source (partial)** means some is; **Source unknown** means no origin was recorded for the value. FloweySales never fills the gap. A link appears only for ordinary web addresses.

Three rules apply everywhere:

- **Unknown is shown as unknown.** A missing price, date or source is a dash, "N/A" or "Source unknown", never zero and never a guess.
- **Dates are not invented.** If a search result only says "2 days ago", the screen shows it as approximate and does not turn it into a date.
- **AI text is not a source.** Text written by a model is labeled as such and never counts as evidence for itself.

## What is covered today

| Surface | State |
| --- | --- |
| World headlines and the company impact panel | Covered: each headline has a badge. Web search results claim no publication time. |
| Saved backtest runs | Partly covered: method and version, time produced, provider and last price date where recorded. Batch and settings checks do not record a provider, so they show as partial. Unsaved backtests and the Strategies page are not covered yet. |
| Leaderboard | Not in the shared format yet, but it has its own detailed method, data and origin panels. See below. |
| Agent answers (chat) | Partly covered by a source list and a citation check for web search and SEC sections. See [Agents, teams and evidence](agents.md). |
| Flow news rows, quotes and feed status, strategy results, financial statements | Not covered yet. Some show a provider or a date in their own way. |

The current state is kept in [Provenance coverage](../provenance-coverage.md). The design is explained in [ADR 0003](../adr/0003-provenance-contract.md).

## The leaderboard's own disclosures

Until the leaderboard adopts the badge, it explains itself in three panels (see [Leaderboard](leaderboard.md)): where the companies come from, how the ranking works, and what each company's score is made of. The footer shows a version and a fingerprint of the method, a fingerprint of the input data and the oldest and newest price dates, so a ranking can be compared or reproduced.

## What checks do and do not prove

- **The citation check** confirms that cited sources were retrieved and that figures and quoted passages appear in the cited text. It does not confirm that the source is right.
- **The claim check** compares a cited sentence with the cited text: company, metric, period, unit, direction, stated causes and simple arithmetic. "Supported" means the source states it that way. "Not proven" and "not checked" are uncertainty, never confirmation, and the check does not judge forecasts, opinions or whether the source is correct.
- **"Require grounded figures"** on a flow's agent node confirms that figures in its answer match a number in its input or tool results. A number that appears for an unrelated reason would pass.
- **The summary of a saved backtest** is checked against the stored figures. The check does not judge the wording.
- **A leaderboard score** says how a company compares within a list. It does not predict anything.
- **A backtest** replays the past with stated biases. It is not evidence of future results.

## Where to find the source

Next to prices, history, financial statements, filings, valuations, backtests, strategy results, your plan, portfolio risk and each part of the Today briefing there is a **Source** button. It lists the provider, whether the value was reported, calculated, assumed or estimated, the method and its inputs for calculated values, and the as-of and retrieval times in UTC. "Source unknown" means nothing recorded where the value came from. Rows that flow nodes produce carry the same information in a `provenance` field, and a report table ends with a Sources line.

## Data freshness and coverage

Prices and reports come from the provider plugins your administrator installed. Delayed data is labeled as such in the feed badge, and stale prices or old fundamentals are flagged on the leaderboard. Coverage differs by provider and market. The leaderboard lists only regions whose lists were checked against the provider. When data is missing, the screen says so.

## Investment-risk notice

FloweySales is a research aid. Data and model output can be incomplete, delayed or wrong. Nothing here is personal investment advice, and nothing knows your goals, taxes or risk tolerance. Fully automatic live trading is not supported.


### Evidence link integrity

Source detail panels only offer navigable HTTP(S) URLs with a valid hostname and no embedded credentials or control characters. Links from agents and providers remain untrusted evidence, not proof that their contents have been read or verified.
