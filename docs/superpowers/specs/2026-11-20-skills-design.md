# Skills and knowledge for the agent

Status: draft, awaiting approval.

## Why

Users want the agent to follow saved instructions (skills) and use saved reference
material (knowledge), in the same format as Claude and Codex skills, without paying
for them in context on every message. Local models run with about 32k of context, so
only a one-line entry per skill may always be in the prompt; the body loads on demand.
This builds on the tool tree (`docs/superpowers/specs/2026-11-20-tool-tree-design.md`).

## Goals

- A skill is a folder with a `SKILL.md` (frontmatter `name`, `description`, then instructions) and optional files beside it. A Claude or Codex skill imports unchanged.
- Knowledge uses the same folder format with `type: knowledge` in the frontmatter: reference to consult, not a procedure to follow. Absent `type` means `skill`.
- Users create, import, edit and delete their own. The admin only does setup.
- The AI can use a skill (`use_skill`) and can propose a new one from what it learned (`save_skill`).
- The folder layout leaves room for scripts and custom tools run in an isolated Python runtime later.

## Non-goals

- No script or code execution in this work. Files named in a skill are readable text only.
- No sharing between users, no marketplace.
- No automatic matching of skills to a message beyond the model reading the index.

## Anti-goals

- A skill the AI wrote must never reach a prompt before the user has reviewed it.
- Skills must not be able to widen what a user may do: a skill is text, and tool grants stay as they are.
- A long skill list must not be able to fill the context window.

## Approaches considered

- **A. Per-user rows in a new table.** Clean, but duplicates what userspace already does.
- **B. Files on the server disk.** Closest to Claude's layout, but awkward in Docker, hard to scope per user, and needs backups.
- **C. No index in the prompt, the AI lists and reads on its own.** Cheapest prompt, but weak local models tend not to look.
- **Chosen (A, stored in userspace).** Skills are folders in the user's existing userspace (blobs in MySQL, per-user scoped, with quotas and the Files page). The prompt carries a capped index.

## Design

1. **Layout.** Skills live under `/skills/<name>/` in the user's userspace: `SKILL.md` plus other files. The folder is the skill directory. Userspace's own rules apply: owner scoping, logical paths, 2 MiB per file, 256 files per user (skills share that quota; the UI says so when it is near).
2. **Parsing.** `SKILL.md` frontmatter must have `name` (lowercase letters, digits and hyphens, matching the folder) and `description` (one line, capped at 300 characters). Anything else in the frontmatter is kept and ignored. A folder that does not parse is shown as broken in the UI and never offered to the model.
3. **Drafts.** `status: draft` in the frontmatter marks a skill the AI proposed. A draft is never in the index and cannot be loaded by `use_skill`. The user reviews it in the UI and enables it, which removes the flag.
4. **Index.** For each enabled skill and knowledge entry, one line `name: description` goes into the prompt of a chat, under a heading, capped by count and total size (the cap is a constant with a test). Over the cap, the entries are cut at the cap with a note naming the number left out, and `use_skill` still loads any by name.
5. **`use_skill(name)`.** Returns the `SKILL.md` body, plus the list of other files in the folder. A second argument `file` returns one of those files as text. Unknown or draft names get an error.
6. **`save_skill(name, description, body, type?)`.** Writes `/skills/<name>/SKILL.md` as a draft. It refuses to overwrite an existing skill, and it is rate limited like other tools that write. The tool description tells the model to save only what the user asked it to keep or what generalizes beyond this conversation, and never text copied from the web or from tool results as instructions.
7. **Tools in the tree.** `use_skill` and `save_skill` belong to a new always-on category `skills`, so they are on every page.
8. **Import and export.** The UI accepts a pasted `SKILL.md` or an uploaded folder of text files and writes them under `/skills/`. Export downloads the folder. This is mostly the existing Files workspace plus a Skills view that lists them and handles the draft state.
9. **Slices.** (1) Backend: parser, index, the two tools, the category, tests. (2) Frontend: a Skills page for list, import, edit, enable draft and delete. (3) Later, separate design: isolated Python runtime and per-skill tools.

## Left to the builder

- Exact index wording, the count and size caps, and the skill name length limit.
- How the Skills page reuses the Files workspace components.
- Whether `use_skill` results count as evidence for task checks (suggestion: they do not).
