# Documentation ownership

The user guide (`docs/guide/`) and the Pages site describe what the application does today. They are owned by whoever changes the behavior: the pull request that changes a screen, a limit, a setting or a data source also changes the page that describes it.

## Checklist for a feature pull request

- The page that covers the feature says what it does now, and only what exists. Planned work is labeled as planned or left out.
- Limits, data sources and known biases are stated next to the feature, not only in the code.
- Times are shown in UTC and the text is US English.
- `last-reviewed` in the page's front matter is set to the date you checked the page against the app.
- A new page has `title`, `order` and `summary` and is linked from a neighbor page.
- Screenshots, if any, are retaken from the running app.
- `node --test scripts/build-docs.test.mjs`, `node scripts/sync-brand.mjs --check` and `node scripts/build-docs.mjs --check` pass.

## Upkeep

- Internal links are checked on every pull request. Links to other sites are fetched once a week by the "Documentation external links" workflow; a failure there is a prompt to fix or remove the link, not a merge gate. Run it by hand with `node scripts/build-docs.mjs --check --external`.
- Pages whose `last-reviewed` date is more than six months old should be re-read against the app at the next release.
- Brand assets and colors come from the app. Change them there and run `node scripts/sync-brand.mjs`.
