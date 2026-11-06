import { expect, test, type Page } from '@playwright/test';
import {
  classifyScenarios, fixtureLabel, fixtureProblems, maxWorkflows, optedIn, pickFixtures, redact, stepProblems, type FlowDef,
} from './workflow-scenarios';

// Real end-to-end workflow runs use ONLY explicit, disabled, disposable [E2E]
// fixtures in the autotest account. No arbitrary saved flow is ever executed.
// Workflow fixtures are discovered from the real account on each run.
// No manually maintained GitHub flow IDs or names are necessary.

type Flow = FlowDef;
type Run = { id: number; status: string; error?: string; output?: string };

const terminal = new Set(['SUCCESS', 'FAILED', 'DEAD', 'CANCELLED', 'AWAITING_APPROVAL']);

async function login(page: Page): Promise<void> {
  const email = process.env.LIVE_E2E_EMAIL;
  const password = process.env.LIVE_E2E_PASSWORD;
  if (!email || !password) throw new Error('Missing protected live-e2e credentials');

  await page.goto('/');
  const response = await page.request.post('/api/auth/login', { data: { email, password } });
  expect(response.ok(), `Autotest login failed: HTTP ${response.status()}`).toBe(true);
  const session = await page.request.get('/api/auth/me');
  expect(session.ok()).toBe(true);
  expect((await session.json())?.user?.email).toBe(email);
  // A hash-route navigation alone leaves the SPA in the signed-out state
  // established on initial load. Force a document reload after API login.
  await page.reload();
  await expect(page.getByRole('heading', { name: 'Sign in' })).toHaveCount(0, { timeout: 20_000 });
}

test.describe('Discover and inspect real Flow Studio workflows', () => {
  test('browse every visible flow in the UI without running it', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'live-desktop', 'Browse all flows once on desktop');
    test.setTimeout(360_000);
    await login(page);
    const listing = await page.request.get('/api/admin/flows');
    expect(listing.ok(), 'Autotest account must be able to list its saved flows').toBe(true);
    const listedFlows: Flow[] = (await listing.json()).flows ?? [];
    await page.goto('/#/flows');
    await expect(page.getByRole('tab', { name: 'My Flows' })).toBeVisible();
    await page.getByRole('tab', { name: 'My Flows' }).click();
    const cards = page.locator('.overview-flow-card');
    await expect(cards).toHaveCount(listedFlows.length, { timeout: 20_000 });
    const total = listedFlows.length;
    console.log(`Flow Studio discovery: ${total} visible saved flow(s)`);

    for (let i = 0; i < total; i++) {
      const card = cards.nth(i);
      const name = (await card.locator('h3').innerText()).trim();
      // Open via the same Edit/History actions a human uses. Nothing is
      // saved, published or executed during the discovery walkthrough.
      await card.getByRole('button', { name: 'Edit', exact: true }).click();
      await expect(page.getByRole('tab', { name: 'Flow editor' })).toHaveAttribute('aria-selected', 'true');
      await expect(page.getByLabel('Flow name')).toHaveValue(name);
      await expect(page.getByRole('group', { name: 'Flow canvas: nodes and directed connections' })).toBeVisible();
      await page.getByRole('tab', { name: 'My Flows' }).click();
      await expect(cards).toHaveCount(total);

      await cards.nth(i).getByRole('button', { name: 'History', exact: true }).click();
      await expect(page.getByRole('tab', { name: 'Run history' })).toHaveAttribute('aria-selected', 'true');
      await expect(page.getByLabel('Select flow for history')).toBeVisible();
      await page.getByRole('tab', { name: 'My Flows' }).click();
      // Angular re-creates overview cards after tab switching; wait for
      // rendering rather than reading their count synchronously.
      await expect(cards).toHaveCount(total);
    }
    await expect(cards).toHaveCount(total);
    console.log(`Flow Studio walkthrough: inspected editor and history for ${total} flow(s)`);
  });
});

test.describe('Execute explicitly safe [E2E] workflows discovered automatically', () => {
  test.skip(!optedIn(process.env.LIVE_E2E_WORKFLOWS), 'Real workflow execution requires manual opt-in');

  test('execute and verify up to two isolated workflows via UI and real run history', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'live-desktop', 'Execute real workflows on desktop only to prevent duplicate runs');
    test.setTimeout(360_000);
    await login(page);
    const list = await page.request.get('/api/admin/flows');
    expect(list.ok(), 'Account needs permission to read its [E2E] flow fixtures').toBe(true);
    const flows: Flow[] = (await list.json()).flows ?? [];
    const limit = maxWorkflows(process.env.LIVE_E2E_MAX_WORKFLOWS);
    const fixtures = pickFixtures(flows);
    expect(fixtures.length, 'Create at least one disabled [E2E] flow in My Flows before opting in to live executions').toBeGreaterThan(0);
    expect(fixtures.length, `At most ${limit} [E2E] flows may be executed per run (LIVE_E2E_MAX_WORKFLOWS, hard ceiling 6) to bound model usage`).toBeLessThanOrEqual(limit);
    const names = fixtures.map(f => f.name);
    expect(new Set(names).size, 'Each [E2E] fixture name must be unique for unambiguous UI selection').toBe(names.length);

    for (const flow of fixtures) {
      const id = flow.id;
      expect(fixtureProblems(flow), `${fixtureLabel(flow)} is not safe to execute`).toEqual([]);

      const historyPath = `/api/admin/flows/${id}/runs`;
      const previous = await page.request.get(historyPath);
      expect(previous.ok()).toBe(true);
      const oldIds = new Set<number>(((await previous.json()).runs ?? []).map((r: Run) => r.id));

      await page.goto('/#/flows');
      await page.getByRole('tab', { name: 'My Flows' }).click();
      const card = page.locator('.overview-flow-card').filter({ has: page.getByRole('heading', { name: flow.name, exact: true }) });
      await expect(card).toBeVisible();
      await card.getByRole('button', { name: 'Run now' }).click();
      await expect(page.getByText('Run queued', { exact: false })).toBeVisible();

      let newRun: Run | undefined;
      await expect.poll(async () => {
        const response = await page.request.get(historyPath);
        if (!response.ok()) return 'HISTORY_UNAVAILABLE';
        const runs: Run[] = (await response.json()).runs ?? [];
        const current = runs.find(r => !oldIds.has(r.id));
        if (current) newRun = current;
        return current?.status ?? 'WAITING';
      }, { timeout: 180_000, intervals: [1_000, 2_000, 4_000] }).toBe('SUCCESS');

      expect(newRun, 'A new workflow run must be recorded').toBeTruthy();
      expect(terminal.has(newRun?.status ?? '')).toBe(true);

      // Verify the actual trace endpoint exists and is associated with this
      // specific new run, not merely a stale "success" banner in the UI.
      const trace = await page.request.get(`/api/admin/flows/${id}/runs/${newRun?.id}/steps`);
      expect(trace.ok(), 'Real node execution trace should be accessible').toBe(true);
      const result = await trace.json();
      expect(Array.isArray(result.steps), 'Run history must contain a steps array').toBe(true);
      expect(result.steps.length, 'At least one node must be recorded').toBeGreaterThan(0);
      // Structure/state invariants per scenario; never exact model wording.
      expect(stepProblems(flow.graph, result.steps, classifyScenarios(flow.graph)), `${fixtureLabel(flow)} trace invariants`).toEqual([]);

      // No model outputs, financial data, prompts or node payloads in CI logs.
      console.log(redact(`[E2E] ${fixtureLabel(flow)}: success, node trace present`));
    }
  });
});
