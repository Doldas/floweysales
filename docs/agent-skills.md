# Agent skills and knowledge

A **skill** is a saved set of instructions the Research Agent follows when a request fits it. **Knowledge** is saved reference material the agent consults. Both use the folder format of Claude and Codex skills, so a skill written for those tools can be imported unchanged.

## Where they live

In your own files (the Files page), under `/skills/<name>/`:

```
/skills/dcf-walkthrough/SKILL.md
/skills/dcf-walkthrough/notes/wacc.md     (optional text files beside it)
```

`SKILL.md` starts with a frontmatter block:

```
---
name: dcf-walkthrough
description: Value a company with a DCF, step by step
type: skill
---

The instructions, in Markdown.
```

- `name` must match the folder and use lowercase letters, digits and hyphens (at most 64 characters).
- `description` is one line that says what the skill does and when to use it. Only the first 300 characters are shown to the agent.
- `type` is `skill` (a procedure to follow, the default) or `knowledge` (reference to consult).
- Other frontmatter keys are kept and ignored. Bundled scripts are never run.
- A folder that does not parse is skipped, so the agent never sees it.

Skills share the file limits of your files: 2 MiB per file and 256 files in all.

## How the agent uses them

Each message carries one line per enabled skill (name and description), capped at 40 entries and about 4 KB so it cannot fill a small context window. The agent loads the full text with `use_skill(name)`, and can read one of the folder's other files with `use_skill(name, file)`. A skill is text: loading one gives the agent no tool and no data it did not have.

## Drafts

Ask the agent to keep something and it calls `save_skill`, which writes `/skills/<name>/SKILL.md` with `status: draft`. A draft is never listed to the agent and cannot be loaded. Open it in Files, read it, and delete the `status: draft` line to enable it. The agent cannot replace an existing skill.

Skills need a signed-in account, because they live in your files.

Design: `docs/superpowers/specs/2026-11-20-skills-design.md`. Related: [architecture](architecture.md) (the tool tree that keeps the tool list small).
