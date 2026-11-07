# Playwright regression: trigger node identity (#262)

Actions run 42395297708, job 128471280696 failed for both desktop and mobile because `flow-diagrams.spec.ts` selected an obsolete `event:filing.published` SVG node id after trigger diagram nodes were made unique by database id (`event:<id>`).

The test now selects the event node with a stable-id prefix and its visible `filing.published` label, asserts a unique match and checks that clicking still exposes the typed payload. This preserves the intentional duplicate-event-name protection in the shared diagram model.

Re-run normal frontend Playwright CI on this PR; validate the fixed test on desktop and mobile. No production graph persistence or runtime behavior changes.
