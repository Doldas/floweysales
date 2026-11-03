# Plan 8: Help, docs and brand

**Issues:** #205, #206.

**Why one plan:** they share brand assets, tokens and content. #206 produces the content and the tokens that #205 reuses.

## What happens today

**Brand: three marks.**

| Where | Mark |
| --- | --- |
| App favicon and PWA (`frontend/public/icons/icon.svg`) | White pulse line on a purple gradient |
| App sidebar and login | The same pulse, drawn with the icon font (`bi-activity`) and CSS, not from the file |
| Docs site (`site/index.html`) | Text `F.` in a green-accented box, with no favicon |

The site's design is also separate: dark only, green accent `#9befcc`, its own tokens in minified CSS. The app has light and dark themes, with purple accent `#5b5ff0`, in `frontend/src/styles/_tokens.scss`.

**Docs.**
- The site is hand-written HTML: a landing page and an API explorer. Its text is **Swedish**, while the app and `docs/` are **English**.
- **`docs/` never reaches the site.** `pages.yml` triggers on `docs/**` but uploads only `site/`, so its 27 top-level Markdown files aren't published, and the trigger only redeploys the same pages.
- `docs/` mixes user guidance with internal notes and plans.
- CI checks only that the files exist and contain the word FloweySales. There is no link check.

**App.**
- No Help or About route; navigation is in `shell.ts`.
- Build info exists (`internal/app/buildinfo.go`), and the update service already reads commit and build time (`internal/updates/service.go`).
- `markdown.ts` already renders Markdown safely, and the command palette is a natural way in.

**License.** There is no `LICENSE` file yet. The owner chose MIT; see Plan 9 WP1.

## Work packages

### WP1: One brand source (S, `feature/brand-source`) — #205, #206

- `frontend/public/icons/` stays canonical. Sidebar and login use `icon.svg` in place of the icon-font approximation.
- `scripts/sync-brand.mjs` copies the icons, and generates a `tokens.css` from `_tokens.scss`, into the docs site.
- CI fails if the generated copies differ from the source, which is the documented mechanism against drift the issue asks for.
- Add favicon and Open Graph image to the site.

### WP2: Docs site (L, `docs/user-docs`) — #206

**Tool: VitePress.** It takes Markdown, has built-in local search and dark mode, and needs only Node, which the project already uses. Its theme takes CSS variables, so WP1's tokens map directly. The API
explorer moves over as a page and keeps its data file.

**Structure** (`docs/site/`, published; internal notes and plans stay in `docs/` and unpublished):

```
index                 What FloweySales is, who it's for, its limits, what's available now
getting-started/      Install, connect a model, install providers, first research question
features/             One page per module: Research Agent, Agent team, Flows, Markets and
                      Leaderboard, Strategies, World, Wealth plan, Financials, Paper portfolio,
                      Files, Community, Plugins and tools
guides/               End-to-end tasks: research a company, screen and shortlist,
                      test a strategy, automate a weekly memo
reference/            Ranking definitions, backtest method, agent tools and limits,
                      output contracts, data sources, configuration, API
trust/                Sources and freshness, what AI verification can and can't do,
                      coverage, investment-risk disclaimer
contribute/           Architecture, local setup, tests, plugins, releases
changelog
```

- **Each feature page has a fixed shape:** purpose, workflow, inputs, outputs, limitations, status (`Available`, `Experimental`, `Planned`). The status is the mechanism separating current from upcoming.
- **Audit first.** List every statement on the current site and in `docs/`, marked keep, fix or remove. For example, check the site's provider claims against what's shipped now.
- **Screenshots:** a Playwright script against a seeded demo instance, in both themes, so they can be regenerated. No real accounts or holdings in them.
- **Pages workflow:** build the site, check links (internal on every PR, external on a schedule, so outside sites can't block a merge), and deploy.
- **Language:** US English only, with no other locale (Plan 9 WP3). VitePress is set to `en-US`, and dates on the site are UTC.
- **Keeping it current:** a `Docs` line in the existing PR template, and a `CODEOWNERS` entry for `docs/site`. `last-reviewed` appears in each page's frontmatter.

**Order:** skeleton and audit, then landing and getting started with one tutorial, then feature pages. Pages for features that other plans will change, such as the leaderboard, are updated with those PRs.

### WP3: In-app Help and About (M, `feature/help-about`) — #205

- **Routes and navigation.** `/help` and `/about`, open to every signed-in user, linked in the sidebar footer and user menu and the command palette. Works in the mobile drawer.
- **One source of content.** Short help articles live in `docs/site/help/`, and the build copies them into the app, so docs and in-app help can't diverge. Rendered with `markdown.ts`.
- **Contextual help.** An `fs-help-button [topic]` on Strategy, Leaderboard, Research Agent, Agent team and Flows opens a side panel, reusing the agent dock's layout, without leaving the page. Each topic links
  to the full docs page.
- **Search.** Simple client-side search over titles and headings. No extra library needed at this size.
- **Clear labels.** "Documentation" is the written guide. "Ask the agent" is a separate button, labeled AI-generated. The two never mix in a single answer.
- **About.** Icon, name and purpose; key capabilities; version, commit and build date (UTC) from a new `GET /api/about`; MIT license with a link to the file; links to repository, issues, releases and `https://doldas.github.io/floweysales/`; and the "research aid, not investment advice" line.
  Limit `/api/about` to signed-in users, since it reveals the exact build. Unknown build values show "Development build".

**Tests:** spec for search and topic lookup; a test that every help topic used by the app exists; link check over the rendered URLs; Playwright visual checks of Help and About in light, dark and mobile; a keyboard pass with axe.

## Acceptance mapping

| Issue | Covered by |
| --- | --- |
| #206 brand | WP1 |
| #206 content, site, checks | WP2 |
| #205 Help, About, context | WP3 |
| #205 brand and theme | WP1, plus WP3 using the app's own components |

## Depends on

Plan 9 WP1 (license) comes before the About page, and Plan 9 WP3 (language) runs together with WP2's audit.
