---
title: Tutorial - from ranking to backtest
order: 12
summary: A start-to-finish walk through the leaderboard, one company, a backtest and a saved run, with what to expect at each step.
last-reviewed: 2026-11-04
---

# Tutorial: from ranking to backtest

This walk-through takes about 20 minutes and uses only screens that exist today. It needs at least one market-data provider plugin (see [Getting started](getting-started.md)). Nothing here places an order, and nothing is a recommendation.

## 1. Rank a list

1. Open **Markets** and scroll to the leaderboard, "Leaderboard: worth a look now".
2. Pick the list **US mega caps** and the strategy **Best buy now**.
3. Wait for the table. The first load of a list takes a while; later switches are quick.

**Expect:** a table with rank, grade, score, six factor bars and a "Why it ranks" column. Footer lines give the price dates. If a warning says the data has mixed ages, believe it. See [Leaderboard](leaderboard.md).

## 2. Check where the list and numbers come from

1. Open **Where do these companies come from?** and read why the list exists, its limits and its date.
2. Open **How this ranking works** and note the method version.

**Expect:** the list is hand-picked and today's, so it has survivorship bias. A top rank means "better than most of these", not "will rise".

## 3. Look at one company

1. Select the top row. Markets opens that company.
2. Set the range to **1Y** and switch on **EMA 50** and **EMA 200**.
3. Read the feed status badge in the top bar before trusting a price. See [Markets](markets.md).

**Expect:** a price chart in UTC. A missing figure shows a dash, not zero.

## 4. Backtest the strategy

1. Return to the leaderboard and press **Backtest this strategy**.
2. Choose **Top 10**, **monthly** and **5Y**.
3. Read **Read this before trusting the result** before looking at the return tiles.

**Expect:** return per year after costs, the difference against the benchmark (SPY for US lists), worst fall and turnover, plus stated biases. The result is a replay of the past, not a forecast. See [Strategies and backtests](strategies.md).

## 5. See how fragile it is

1. Press **Run sensitivity**.
2. Look at how far the return moves when cost, holdings and rebalance interval change.

**Expect:** a result that falls apart with small changes is weak evidence. Settings were chosen by someone who had seen the past.

## 6. Save it and ask the agent

1. Press **Save backtest** and give the run a name.
2. Open it under saved runs and press **Summarize**.
3. Download the run as JSON if you want the parameters and provenance.

**Expect:** the summary is checked mechanically against the stored figures, but it is still AI-generated text. If no model is configured, summarizing is unavailable. See [Research Agent](agents.md).

## What to do next

- Try another list or strategy and compare, remembering that comparing many variants and picking the best fools you.
- Read [Provenance and trust](provenance.md) to see how sources are labeled.


## 7. Reproduce a source-backed AI answer

This optional part uses the existing **Research Agent** page and does not require any paid search API. An administrator must already have configured an AI model; for web search, a search provider must also be enabled.

1. Open **Research Agent** for the company you selected in step 3.
2. Ask: **“List the latest quarter's revenue, currency and report period. Use only documents you successfully opened; cite each source URL and page if available. If anything is unavailable, say so.”**
3. Expand the tool steps. A search hit is only a listing, not evidence that the article or PDF was read. A blocked filing (HTTP 403) is *not* read evidence.
4. For each reported figure, compare the revenue value, unit, company, period and publication date with its **original quarterly report**. An updated page can still contain numbers for an older quarter.
5. Ask the agent separately: **“Summarize recent investor sentiment, labeled as sentiment, not as financial facts.”** Reddit and forum discussions may help explain reactions but cannot independently establish company revenue.
6. Save the report in Workspace as Markdown. Reopen Preview and make sure monetary values, equations and source links are legible. The renderer formats math; it does not verify calculations.

**Expected outcome:** a source-backed research note with clear limitations, not a fabricated report. If your market provider lacks quarterly financials, the result may legitimately contain **unknown** or **unavailable** fields. Do not substitute an old annual value or an unverified blog number. This exercise has no live trade or order side effect.

**When something fails:** check [Troubleshooting](troubleshooting.md), [Sources and provenance](provenance.md), and [Agents and evidence](agents.md). Provider access restrictions and missing model credentials are not problems to circumvent.
