---
title: Agents, teams and evidence
order: 7
summary: The Research Agent, agent teams of fictional investor characters, choosing a model, and the evidence modes that hold an answer until its citations check out.
last-reviewed: 2026-11-06
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

A second check, the **claim check**, reads each cited sentence against the cited text and gives it a verdict:

| Verdict | Meaning |
| --- | --- |
| Supported | The cited text states the figure with the same company, metric, period, unit and direction. Proof of what the source says, not that the source is right. |
| Contradicted | The number is in the source, but for another metric, period, unit or direction, or the arithmetic does not follow. |
| Not proven | The evidence is insufficient: the company is not named in the source, the figure is adjusted or guidance but stated as reported, a stated cause is not in the source, another source disagrees, or the source was only a search result. This is uncertainty, not a finding that the claim is false. |
| Not checked | No fixed rule applies, for example a recommendation, a forecast or an opinion. |

Under **Sources**, each claim opens to **Why is this claim supported?** It shows how the sentence was read (company, metric, period, unit, direction), the calculation when there is one, the exact passage of the source with the matched number highlighted and when it was retrieved (UTC), and any other source that gives a different figure. The rules run in the app with no model call, so they are repeatable and cost nothing; at most 40 sentences are checked per answer. Text inside a source that reads as an instruction to an AI is ignored and never counts as evidence. Judgements the agent should label as such ("Forecast:", "Opinion:") are flagged in the evidence modes when they are not.

A passing check says that every cited source was retrieved and its figures appear in the text, and the claim check says what each source states. Neither says the claims are true, that the source is right, or that the prose supports the claim. Always read the passage a number comes from before you rely on it.

The agent can open a search hit by its id. It cannot be made to fetch an address that no search returned, and it only reads public pages. Pages that need a login or JavaScript cannot be read; the source then stays "search result only". A PDF is read page by page; a scanned page or a chart or table that is a picture is read only if an administrator has set up a vision model, and otherwise the result lists those pages as unread. The tool-call view shows a Document evidence panel with the pages, how each was read and which figures passed the automatic checks. See [Reading PDFs and scanned reports](../pdf-vision.md). Reading a page does not make it a primary source.

### Evidence mode

The **Evidence** selector in the chat sets how strictly the agent is held to its citations. It is your own choice, and administrators have no say in it. **Save as my default** stores your settings on the server, so new chats and team runs start from them; **Remove default** goes back to Standard. A choice made in the chat itself always wins over the saved default, and a team the agent delegates to answers under the chat's choice.

| Mode | What happens |
| --- | --- |
| **Standard** (default) | The answer streams as written. The check runs afterward and flags problems. |
| **Evidence only** | The answer is held until the check passes. A draft that fails goes back to the model with its findings, at most twice. If problems remain, you see the last draft with a warning that the flagged sentences are unverified. The agent is asked to say "insufficient verifiable evidence" instead of filling gaps from memory. |
| **Primary sources only** | As Evidence only, and only primary filings, such as SEC filings the agent opened, count as support. A web page does not. |

Optional limits apply to the two strict modes:

- **If a draft still fails:** show it with a warning (the default) or **block** it. Blocking removes the sentences that still fail after the corrections and says how many were removed and why; if nothing is left, the answer is "Insufficient verifiable evidence".
- **Newest source age:** a source older than the limit, or with no publication date, does not count as support. Web search results carry no machine-readable date today, so under an age limit only filings can support a sentence.
- **Source types:** allow or deny primary filings and web pages. Deny wins.
- **Reachability:** a source the answer cites but the agent never opened is checked with a short request. A dead link is flagged and the answer goes back for correction. A link the safety checks refuse is not requested. Links that answer with a login or rate-limit status count as reachable.

Evidence mode also applies to agent teams (every member answers under it; a chair's summary has no sources and is not checked), to teams the Research Agent delegates to, and to flow agent nodes (a failing node attempt is retried, or the failing sentences are blocked). The check finds unsupported figures, links and quotes. It does not judge whether a claim follows from its source, and the labels the model puts on claims are not verified.

### Exporting an answer

Under an answer, the Sources panel has **Export Markdown** and **Export JSON**. The file starts with the evidence policy the answer ran under and the citation check result, then the answer, then each source with its type, whether the agent opened it, its address, the search it came from, the document id and the UTC times it was published and retrieved, with a short excerpt. Addresses are stripped of credentials and secret-looking parameters. The export says what the check did not establish: it is research decision support, not a guarantee and not an instruction to trade.

In the two strict modes the agent is also asked to mark calculations, estimates, assumptions, interpretations and opinions, to show the method and inputs of a calculation, and to say plainly when the allowed sources cannot answer. The server does not verify those labels.

## Agent team

The Agent team page puts several fictional investor characters on one question. Each answers in its own style, using the same real data. A neutral chair can then compare the answers.

1. **Choose the team.** Up to six characters. Each card shows the character's archetype, tagline and time horizon.
2. **Read a profile.** "Profile and portfolio" shows how the character decides, how it sizes positions, the portfolio it follows (with the source and date of the disclosure where one exists) and the documents it is based on. A disclosure of past holdings is not a current position, a view or a recommendation. The character is invented; it follows an approach, not a person.
3. **Check the strategies.** "Strategies the team will run" lists the rule strategies and rankings each chosen character runs on real data before answering. The answer cites the trade count and warnings.
4. **Pick a model per character** if profiles are available, or leave the default.
5. **Ask a question,** or press "Suggest a question". Optionally tick "Neutral chair compares the answers" (needs at least two characters).
6. **Read the results.** Each character's card shows its answer, the tool calls it made, the model it used and a stance: Fits, Partly fits, Does not fit or Not enough data. A tally shows how the team landed. The chair compares the answers and adds no facts of its own.

Each character sees only your question and its own tool results, never another character's answer.

### Budget

Set optional ceilings for a run: tokens, cost in US dollars and tool calls, counted across all characters and the chair. A run that reaches one stops and names it; finished answers are kept and the page shows what the run used. Cost comes from the prices an administrator entered in the model profiles. If a model has no prices, or the endpoint does not report token usage, cost is shown as unknown rather than zero, and the token and cost ceilings cannot be enforced (the tool-call ceiling and time limits still hold, and you are told). A ceiling is checked before each model round, so a run can end one round past it. Ceilings are saved with a saved team.

### Saved teams

Name a team and press **Save team** to keep the characters, the chair setting and each character's model profile. You can keep up to 20 saved teams. Load or delete one from the list. If a profile has since been disabled or deleted, the team shows which character lost it, and that character uses the default.

## Limits

- Models are served from one endpoint set up by the administrator. A profile chooses among the models that endpoint offers.
- Cost ceilings need prices in the model profile and a usage report from the endpoint; without them only the tool-call ceiling is enforced.
- The citation check covers web search, opened pages and SEC section sources. Other tools are not registered in the source list yet.
- The claim check understands English filing phrasing and a fixed list of metrics. It reads a figure's meaning from the words around it in the source; a table the agent received as plain text, or a metric it does not know, is reported as not checked, not as supported.
- A link that was reachable when checked may change later, and a page can be reachable and still not say what the answer claims.
- An answer is a summary of what the tools returned. Check dates, especially for numbers.

## Required parts and saved tasks

In the chat, list **Required parts** (one per line, up to 12) to turn a question into a saved task. A checklist shows each part as done, reported missing or not yet reported, and a task with missing parts is not reported as finished. **Saved tasks** lists your tasks with status, rounds, tool calls and the last update in UTC. **Resume** continues from the server's checkpoint, with the sources gathered so far; the chat shows only the continuation and uses the default model and persona. **Delete** removes a task.

## Teams and profiles in flows

A flow agent node can use one of the model profiles an administrator prepared (instead of a raw model) and can hand its question to one of your own saved teams. A node with a team can only delegate to that team. The team's members get the research tools flows may grant, never your personal data. A disabled or deleted profile fails the node instead of silently switching models. See [Flow Studio](flows.md).

### Mathematical expressions in research answers

The shared Markdown renderer displays common financial expressions such as fractions, multiplication, powers, roots and labels. Use `\(...\)` for inline math and `\[...\]` or `$$...$$` for display math. Tables and long display equations stay scrollable inside their panel on small screens. The underlying numbers and formulas are **not evaluated** or verified by the renderer: use calculation and citation tools for correctness. Unsupported TeX commands display as escaped literal source instead of untrusted HTML. The same rendering pipeline is shared by Research Agent, Agent Teams, Flow reports and Workspace preview; email uses its own safe content adapter.


The shared Markdown renderer accepts both standalone `$$...$$` equations and multiline `$$` blocks, in addition to `\[...\]` and inline `\(...\)`. Price strings such as `$3.07 bn` remain currency, not TeX. Unsupported commands remain visibly escaped; this is not full LaTeX syntax support.
