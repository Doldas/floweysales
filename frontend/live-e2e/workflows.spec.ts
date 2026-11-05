import { expect, test, type Page } from '@playwright/test';

// Real end-to-end workflow runs use ONLY explicit, disabled, disposable [E2E]
// fixtures in the autotest account. No arbitrary saved flow is ever executed.
// Configure their IDs in the protected live-e2e environment variable.
const flowIds = (process.env.LIVE_E2E_FLOW_IDS ?? '')
  .split(',').map(s => s.trim()).filter(Boolean);

type Flow = {
  id: number;
  name: string;
  enabled: boolean;
  interval_minutes: number;
  event_key: string;
  graph?: { nodes?: Array<{ type: string; tools?: string[]; flow_id?: number; function_id?: number }> };
};
type Run = { id: number; status: string; error?: string; output?: string };

// Explicit allowlist: no writes, notifications, HTTP calls, nested flows,
// plugin executables, unsafe operations, scheduled triggers or trading.
const allowedNodeTypes = new Set(['trigger', 'constant', 'transform', 'json_get', 'condition', 'merge', 'output', 'agent']);
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
}

test.describe('Real saved workflows — isolated test fixtures only', () => {
  test.skip(process.env.LIVE_E2E_WORKFLOWS !== 'true', 'Real workflow execution requires manual opt-in');
  test.skip(flowIds.length === 0, 'Add LIVE_E2E_FLOW_IDS to the protected live-e2e environment');

  test('execute and verify up to two isolated workflows via UI and real run history', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'live-desktop', 'Execute real workflows on desktop only to prevent duplicate runs');
    expect(flowIds.length).toBeLessThanOrEqual(2);
    expect(new Set(flowIds).size).toBe(flowIds.length);

    test.setTimeout(360_000);
    await login(page);
    const list = await page.request.get('/api/admin/flows');
    expect(list.ok(), 'Account needs permission to read its [E2E] flow fixtures').toBe(true);
    const flows: Flow[] = (await list.json()).flows ?? [];

    for (const rawId of flowIds) {
      expect(rawId).toMatch(/^[1-9][0-9]*$/);
      const id = Number(rawId);
      const flow = flows.find(f => f.id === id);
      expect(flow, `Fixture ${id} is missing; nothing will be created automatically`).toBeTruthy();
      if (!flow) return;
      expect(flow.name, 'Only explicit [E2E] prefixed test fixtures may run').toMatch(/^\[E2E\] /);
      expect(flow.enabled, 'Fixtures must not be enabled as automations').toBe(false);
      expect(flow.interval_minutes).toBe(0);
      expect(flow.event_key ?? '').toBe('');
      expect(flow.graph?.nodes?.length ?? 0, 'Fixture must contain nodes').toBeGreaterThan(1);
      for (const node of flow.graph?.nodes ?? []) {
        expect(allowedNodeTypes.has(node.type), `Node type ${node.type} not approved for live E2E`).toBe(true);
        expect(node.tools ?? [], 'AI workflow fixtures must not have tool grants').toEqual([]);
      }

      const historyPath = `/api/admin/flows/${id}/runs`;
      const previous = await page.request.get(historyPath);
      expect(previous.ok()).toBe(true);
      const oldIds = new Set<number>(((await previous.json()).runs ?? []).map((r: Run) => r.id));

      await page.goto('/#/flows');
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

      // No model outputs, financial data, prompts or node payloads in CI logs.
      console.log(`[E2E] workflow fixture ${id}: success, node trace present`);
    }
  });
});
