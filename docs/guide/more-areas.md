---
title: More areas
order: 10
summary: Command Center, Wealth plan, Paper portfolio, Inbox, Files, Community and Notifications in brief.
last-reviewed: 2026-11-06
---

# More areas

These screens are smaller than the ones with their own pages. Each is described briefly and, where one exists, links to a technical page.

## Command Center

The home page. A **Today** briefing is built from figures only, with no model running on load: your wealth-plan phase and chance of reaching your goal, the macro regime, the paper portfolio's day change and drawdown where you have access to it, and a few things worth a look. If nothing is available it says so and suggests setting up your plan, tracking some symbols or installing a provider plugin with events. **Brief me** sends the figures to the [Research Agent](agents.md) for a written summary, which is AI-generated.

Below it are key figures for agent runs over the last 24 hours, agent activity across flows, automations, live trackers, starter flow templates and recent files.

## Wealth plan

Where you are, what that phase needs and how likely your goal is. You enter your situation and goal, and the page shows which phase of building wealth you are in (Foundation, Starter, Accumulation, Acceleration, Preservation or Income), a target allocation with a capped high-risk satellite, a seeded Monte Carlo projection with the probability of your goal, a checklist and suggested automations. The assumptions are shown on the page. It is a plan to discuss, not advice. See [Wealth plan](../wealth-plan.md) for the rules.

## Paper portfolio

For administrators. Practice decisions against live marks with **simulated fills only**; nothing reaches a broker. The **Desk** view has the order ticket, positions and blotter, with position sizing by risk. The **Risk** view shows concentration, volatility, beta, correlation, drift from your plan and rebalancing suggestions, which prefill the ticket without submitting it. Paper-trade proposals made by analysts or flows need approval from a different administrator than the requester. Limits and costs are set under Settings, Trading limits.

## Inbox

Every decision that agents are waiting on. **Flow approvals** lists flow steps that need a decision before something changes outside the run; **Paper trade proposals** lists proposals that need a second administrator. Review the exact content, then approve or reject. A rejection needs a reason. See [Flow Studio](flows.md#approvals-and-the-inbox).

## Files

Your private research space, for signed-in accounts. Flows read their inputs from it and save reports to it. You can edit, preview and download files, and organize them in folders. Deleting a folder is confirmed and covers its contents.

**Markdown files** open in an editor with these views:

- **Edit source** is the plain text of the file.
- **Preview** shows the rendered result: headings, emphasis, nested and task lists, quotes, tables and fenced code. Footnotes and syntax coloring in code blocks are not supported, and images are never loaded; an image appears as a labeled link.
- **Rich text** edits visually with a toolbar (headings, bold, italic, strikethrough, inline code, links, lists, quotes, rules, undo and redo) and saves Markdown into the same `.md` file. Formatting is rewritten only when you change something in this view. A file with content rich text cannot represent, such as raw HTML, front matter, footnotes, reference-style links, table alignment or indented code, stays in source mode and the page says why, so nothing is dropped.
- **Split** shows source and preview side by side. It appears only on wide screens.

All views edit the same text, so switching views keeps your unsaved changes. A status next to **Save** shows Saved, Unsaved changes, Saving or the reason a save failed, and Ctrl+S or Cmd+S saves. Previews treat file content as untrusted: HTML is shown as text, only `https` links and links to other Workspace files work, and a link that points outside the Workspace is shown as plain text.

**README overview.** When a folder contains `README.md`, it is shown rendered under the file list as that folder's **Overview**. Only that folder's own README is used, never one from a parent or sibling folder. The name `README.md` wins; otherwise `readme.md` in any letter case, first in alphabetical order. A long README is clipped with **Show full README**, and **Open README** edits it like any other file. Relative links open the other Workspace file or folder. A folder without a README shows a hint and **Create README.md**; nothing is generated for you.

## Community

For administrators. Theses and evidence shared by everyone on this workspace. Pick a topic to focus on one market.

## Notifications

Email alerts for tracker signals: golden and death crosses and stop-loss events. You choose which trackers and events send an email; nothing is sent until you subscribe. Needs the administrator's mail configuration. See [Settings and roles](settings.md).


### Workspace rich-text tables

The shared Markdown editor now includes **Insert table** in Rich text mode. It creates an editable two-column starter table; changing its cells saves ordinary GitHub-Flavored Markdown in the original `.md` file. Use **Edit source** for advanced table alignment, merged cells, footnotes or unsupported structures: the editor intentionally refuses lossy rich-text editing for those constructs rather than silently discarding them.


### Mathematical Markdown and source fidelity

When a Workspace Markdown file contains TeX formulas (`\(...\)`, `\[...\]`, `$x^2$`, or `$$...$$`), Rich text is disabled to protect the original LaTeX from a lossy HTML-to-Markdown conversion. **Preview** continues to use the shared math renderer; **Edit source** preserves every delimiter and equation. Ordinary prices such as `$3.07 bn` are not treated as TeX. Code fences and inline code remain editable.
