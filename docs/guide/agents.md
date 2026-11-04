---
title: Agents, teams and evidence
order: 7
summary: The Research Agent, agent teams of fictional investor characters, choosing a model, and the evidence modes that hold an answer until its citations check out.
last-reviewed: 2026-11-04
---

# Agents, teams and evidence

FloweySales has two ways to ask an AI model a research question: the **Research Agent**, one conversation with data tools, and the **Agent team**, several fictional investor characters answering the same question side by side. Both are AI-generated. They can be wrong, and neither gives advice or places orders.

An administrator must connect an AI endpoint first (see [Settings and roles](settings.md)). Without one, the agent has no model and says so.

## Research Agent

The Research Agent answers by calling tools for prices, indicators, financial statements, filings, news and calculators, then writing up what it found. Open it from the sidebar, or press Ctrl/⌘ J anywhere to open the same chat as a side panel (the dock).

- **Header:** shows the model in use and the budget of the answer: the maximum number of reasoning rounds and tool calls. If the agent runs out, it says so. Ask a narrower question.
- **Tool calls** appear as steps in the conversation. Open a step to see exactly what was asked and what came back, with timing.
- **Context:** the instrument you selected in the header, its provider, range and last price are sent with your question. The chips above the input show what is sent.
- **Tools panel:** on the full page, the right-hand panel lists the tools enabled for your role, such as price history, technical analysis, quarterly financials, SEC filings and facts, calculators for discounted cash flow, WACC and ROIC, web search, and the leaderboard and backtest tools. If none are enabled, the agent answers from its own knowledge.
- **Answer as:** by default the agent answers as a neutral research analyst. You can choose one of the fictional characters instead, to get that approach on the same data.
- **Model:** if your administrator prepared model profiles, you pick one by name, for example one for speed and one for depth. The note under the picker says what the profile is for and what is known about it. Unknown costs and limits are shown as unknown.
- **New chat** clears the conversation.

The agent has a tool that hands a goal to one of your saved teams by name (see below). A run may delegate once; a delegated team cannot delegate again. The team's answers come back as data that the agent must attribute to the team and to each member by name.

### Sources and the citation check

During a run the agent keeps a list of sources. After an answer, open **Sources** under it:

- **Read by the agent:** pages and filings it opened, with a link. Each has an id such as [S2], which the agent uses to mark the sentences that rest on that source.
- **Search results only:** hits it saw only as a title and a snippet. They are leads, not evidence.

A mechanical check goes through the final answer sentence by sentence. It reports:

| Finding | Meaning |
| --- | --- |
| Fabricated | A source marker or address that no tool returned in this run |
| Snippet only | A figure that rests on a search result the agent never opened |
| Figure mismatch | A figure that is not in the cited excerpt |
| Uncited | A figure that appears in a retrieved source but is stated without a marker |
| Quote mismatch | A passage in quotation marks (five words or more) that is not in the text of the cited source |
| Not primary | A secondary source cited in the primary-sources mode |

A passing check says that every cited source was retrieved and its figures appear in the text. It does **not** say the claims are true, that the source is right, or that the prose supports the claim. Always read the passage a number comes from before you rely on it.

The agent can open a search hit by its id. It cannot be made to fetch an address that no search returned, and it only reads public pages. Pages that need a login or JavaScript, and scanned PDFs, cannot be read; the source then stays "search result only". Reading a page does not make it a primary source.

### Evidence mode

The **Evidence** selector in the chat sets how strictly the agent is held to its citations. It is your own choice, saved in your browser, and administrators have no say in it.

| Mode | What happens |
| --- | --- |
| **Standard** (default) | The answer streams as written. The check runs afterward and flags problems. |
| **Evidence only** | The answer is held until the check passes. A draft that fails goes back to the model with its findings, at most twice. If problems remain, you see the last draft with a warning that the flagged sentences are unverified. The agent is asked to say "insufficient verifiable evidence" instead of filling gaps from memory. |
| **Primary sources only** | As Evidence only, and only primary filings, such as SEC filings the agent opened, count as support. A web page does not. |

Evidence mode applies to the Research Agent. It is not yet available for agent teams or flow nodes.

## Agent team

The Agent team page puts several fictional investor characters on one question. Each answers in its own style, using the same real data. A neutral chair can then compare the answers.

1. **Choose the team.** Up to six characters. Each card shows the character's archetype, tagline and time horizon.
2. **Read a profile.** "Profile and portfolio" shows how the character decides, how it sizes positions, the portfolio it follows (with the source and date of the disclosure where one exists) and the documents it is based on. A disclosure of past holdings is not a current position, a view or a recommendation. The character is invented; it follows an approach, not a person.
3. **Check the strategies.** "Strategies the team will run" lists the rule strategies and rankings each chosen character runs on real data before answering. The answer cites the trade count and warnings.
4. **Pick a model per character** if profiles are available, or leave the default.
5. **Ask a question,** or press "Suggest a question". Optionally tick "Neutral chair compares the answers" (needs at least two characters).
6. **Read the results.** Each character's card shows its answer, the tool calls it made, the model it used and a stance: Fits, Partly fits, Does not fit or Not enough data. A tally shows how the team landed. The chair compares the answers and adds no facts of its own.

Each character sees only your question and its own tool results, never another character's answer.

### Saved teams

Name a team and press **Save team** to keep the characters, the chair setting and each character's model profile. You can keep up to 20 saved teams. Load or delete one from the list. If a profile has since been disabled or deleted, the team shows which character lost it, and that character uses the default.

## Limits

- Models are served from one endpoint set up by the administrator. A profile chooses among the models that endpoint offers.
- Costs of profiles are shown, not enforced.
- The citation check covers web search and SEC section sources. Teams, flows and other tools are not registered in the source list yet.
- There is no reachability check: a cited link may have changed since the agent read it.
- An answer is a summary of what the tools returned. Check dates, especially for numbers.
