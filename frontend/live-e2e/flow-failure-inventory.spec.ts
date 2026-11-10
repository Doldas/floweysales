import { expect, test, type Page } from '@playwright/test';
import { mkdirSync, writeFileSync } from 'node:fs';
import { classifyFailure, safeAnalysisInput, summarizeFailures, type FailureRecord } from './flow-failure-triage';

// Read-only inspection of ALL flows visible to a disposable, low-privilege
// test account. Never launches, edits or retries a real user flow.
async function login(page: Page): Promise<void> {
  const email = process.env.LIVE_E2E_EMAIL;
  const password = process.env.LIVE_E2E_PASSWORD;
  if (!email || !password) throw new Error('Missing protected live-e2e credentials');
  await page.goto('/');
  const auth = await page.request.post('/api/auth/login', { data: { email, password }, failOnStatusCode: false });
  expect(auth.ok(), 'Live test account authentication failed').toBe(true);
  expect((await (await page.request.get('/api/auth/me')).json())?.user?.email).toBe(email);
  await page.reload();
  await expect(page.getByRole('heading', { name: 'Sign in' })).toHaveCount(0);
}

test('inventory every visible flow and classify recent failures without executing any', async ({ page }, info) => {
  test.skip(info.project.name !== 'live-desktop', 'Read once, avoid duplicate load');
  test.setTimeout(360_000);
  await login(page);
  const list = await page.request.get('/api/admin/flows', { failOnStatusCode: false });
  expect(list.ok(), 'Test account needs flows.read permission').toBe(true);
  const flows: { id: number }[] = (await list.json()).flows ?? [];
  // Run history API currently returns a bounded recent list. Do not pretend
  // the inspection covered runs outside that window.
  expect(flows.length, 'More than 500 flows requires paginated inventory support').toBeLessThanOrEqual(500);
  const failures: FailureRecord[] = [];
  let inspectedRuns = 0;
  let unavailableHistories = 0;
  for (const [index, flow] of flows.entries()) {
    const history = await page.request.get(`/api/admin/flows/${flow.id}/runs`, { failOnStatusCode: false });
    if (!history.ok()) {
      unavailableHistories++;
      continue;
    }
    const runs: { id: number; status: string; error?: string; failed_node?: string | null }[] = (await history.json()).runs ?? [];
    inspectedRuns += runs.length;
    for (const run of runs) {
      if (!['FAILED', 'DEAD', 'CANCELLED', 'AWAITING_APPROVAL'].includes(run.status)) continue;
      failures.push({
        flow: index + 1, run: run.id, status: run.status,
        category: classifyFailure(run.error, run.status, run.failed_node ?? undefined),
      });
    }
  }
  const summary = summarizeFailures(flows.length, inspectedRuns, failures);
  mkdirSync('live-report', { recursive: true });
  // Only aggregate counters leave the test process. Individual flow IDs,
  // errors, prompts, step inputs/outputs and names are not retained.
  const safe = safeAnalysisInput(summary);
  writeFileSync('live-report/flow-failure-summary.json', JSON.stringify(safe, null, 2) + '\n', { mode: 0o600 });
  const lines = [
    '# Live Flow failure inventory (read-only)',
    '',
    `Flows inspected: ${safe.inspected_flows}`,
    `Recent runs inspected: ${safe.inspected_runs}`,
    `Failed/blocked runs found: ${safe.failed_runs}`,
    `Histories unavailable: ${unavailableHistories}`,
    '',
    '| Category | Count |',
    '| --- | ---: |',
    ...Object.entries(safe.categories).map(([key, count]) => `| ${key} | ${count} |`),
    '',
    'This is a bounded recent-history sample from a dedicated test account, not all production runs.',
    'Error classification is heuristic; permission failures are not proof that a commercial license is missing.',
  ];
  writeFileSync('live-report/flow-failure-summary.md', lines.join('\n') + '\n', { mode: 0o600 });
  info.annotations.push({ type: 'flow-failures', description: `${safe.failed_runs} of ${safe.inspected_runs} recent runs` });
  console.log(`Read-only flow inventory: ${flows.length} flows, ${inspectedRuns} recent runs, ${failures.length} failures, ${unavailableHistories} unavailable histories`);
  expect(unavailableHistories, 'All visible flow histories should be accessible to test account').toBe(0);
});
