import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import type { Page, Route } from '@playwright/test';
import { expect, open, test } from './fixtures';

const axeSource = readFileSync(join(process.cwd(), 'node_modules', 'axe-core', 'axe.min.js'), 'utf8');

// #444: broker extensions, the user's own connections, read-only accounts and the
// paper order ticket. Live accounts are listed and never offered for trading.
const json = (body: unknown, status = 200) => ({ status, contentType: 'application/json', body: JSON.stringify(body) });

const paperBroker = {
  provider_id: 'PAPER_X', label: 'Paper X', plugin_id: 'paperx', version: '1.0.0', contract: 1,
  capabilities: ['broker_accounts', 'broker_submit_order'], usable: true,
  broker: { contract: 1, environments: ['paper'], order_types: ['MARKET', 'LIMIT'], time_in_force: ['DAY'] },
  connection_vars: [{ name: 'PAPER_KEY', description: 'Paper API key', required: true, secret: true }],
};

function brokersView(over: Record<string, unknown> = {}) {
  return {
    contract: 1, live_execution: false,
    notice: 'FloweySales sends orders to paper accounts only. Live accounts are listed and never traded.',
    builtin: { broker: 'PAPER', account: 'paper', environment: 'paper', orders_at: '/api/orders' },
    brokers: [paperBroker], trading_hosts: [], connections: [],
    ...over,
  };
}

const accountsView = {
  accounts: [
    { id: 'PA-1', label: 'Paper account', currency: 'USD', environment: 'paper', cash: 10000, buying_power: 20000, equity: 10000, as_of: 1, broker: 'PAPER_X', connection: 'main', tradable: true },
    { id: 'LIVE-1', label: 'Live account', currency: 'SEK', environment: 'live', cash: 500, buying_power: 500, equity: 500, as_of: 1, broker: 'PAPER_X', connection: 'main', tradable: false },
  ],
  sources: [{ broker: 'PAPER_X', connection: 'main', label: 'Paper X · main' }],
  mixed_currencies: 'Amounts are shown per account in its own currency and are never added together or converted.',
};

interface Call { method: string; path: string; body: any }

async function mock(page: Page, opts: { view?: unknown; viewStatus?: number; orders?: unknown[]; events?: unknown[]; cancelAll?: unknown; submit?: (n: number, body: any) => { status: number; body: unknown } } = {}) {
  const calls: Call[] = [];
  let view: any = opts.view ?? brokersView({ connections: [{ name: 'main', broker: 'PAPER_X', label: 'main', saved: ['PAPER_KEY'] }] });
  let submits = 0;
  await page.route('**/api/brokers', route => route.fulfill(json(opts.viewStatus ? { error: 'x' } : view, opts.viewStatus ?? 200)));
  await page.route('**/api/broker-accounts', route => route.fulfill(json(accountsView)));
  await page.route(/\/api\/broker-connections\/[^/]+$/, async (route: Route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname;
    calls.push({ method: req.method(), path, body: req.method() === 'DELETE' ? null : req.postDataJSON() });
    if (req.method() === 'DELETE') view = { ...view, connections: [] };
    else view = { ...view, connections: [{ name: path.split('/').pop(), broker: 'PAPER_X', label: '', saved: ['PAPER_KEY'] }] };
    return route.fulfill(json({ connections: view.connections }));
  });
  await page.route(/\/api\/broker-orders\/\d+\/events$/, route => route.fulfill(json({ events: opts.events ?? [] })));
  await page.route('**/api/broker-orders/cancel-working', route => {
    calls.push({ method: 'POST', path: '/api/broker-orders/cancel-working', body: null });
    return route.fulfill(json(opts.cancelAll ?? { results: [], cancelled: 0, remaining: 0 }));
  });
  await page.route('**/api/broker-orders', route => {
    const req = route.request();
    if (req.method() === 'GET') return route.fulfill(json({ orders: opts.orders ?? [] }));
    const body = req.postDataJSON();
    calls.push({ method: 'POST', path: '/api/broker-orders', body });
    const reply = opts.submit?.(++submits, body) ?? { status: 200, body: { status: 'success', replayed: false, order: { id: 5 } } };
    return route.fulfill(json(reply.body, reply.status));
  });
  return calls;
}

async function openBrokers(page: Page) {
  await open(page, 'settings');
  await page.getByRole('tab', { name: 'Trading limits', exact: true }).click();
  return page.locator('fse-broker-center');
}

async function fillTicket(panel: ReturnType<Page['locator']>, over: { symbol?: string; quantity?: string; limit?: string } = {}) {
  await panel.getByRole('button', { name: 'Load accounts' }).click();
  await panel.getByRole('combobox', { name: 'Account' }).selectOption({ label: 'PAPER_X · main · PA-1 (USD)' });
  await panel.getByLabel('Symbol').fill(over.symbol ?? 'nvda');
  await panel.getByLabel('Quantity').fill(over.quantity ?? '2');
  await panel.getByLabel('Limit price').fill(over.limit ?? '100');
}

test.describe('Settings → Trading → Brokers', () => {
  test('says the app trades on paper accounts only', async ({ page }) => {
    await mock(page);
    const panel = await openBrokers(page);
    await expect(panel.getByRole('note')).toContainText('Paper trading only.');
    await expect(panel.getByRole('article', { name: 'Built-in paper account' })).toBeVisible();
    const broker = panel.getByRole('article', { name: 'Paper X' });
    await expect(broker).toContainText('contract 1');
    await expect(broker.getByText('paper', { exact: true })).toBeVisible();
    await expect(broker.getByText('live', { exact: true })).toHaveCount(0);
  });

  test('a connection is saved sealed, never shown again, and can be removed', async ({ page }) => {
    const calls = await mock(page, { view: brokersView() });
    const panel = await openBrokers(page);
    const broker = panel.getByRole('article', { name: 'Paper X' });
    const key = broker.getByLabel('PAPER_KEY');
    await expect(key).toHaveAttribute('type', 'password');
    await expect(key).toHaveAttribute('autocomplete', 'off');
    await broker.getByRole('button', { name: 'Save connection' }).click();
    await expect(broker.getByRole('alert')).toContainText('Name the connection.');
    expect(calls).toEqual([]);

    await broker.getByLabel('Connection name').fill('main');
    await key.fill('my-paper-key');
    await broker.getByRole('button', { name: 'Save connection' }).click();
    await expect(panel.getByRole('status')).toContainText('Connection main is saved.');
    expect(calls).toEqual([{ method: 'PUT', path: '/api/broker-connections/main', body: { broker: 'PAPER_X', label: 'main', values: { PAPER_KEY: 'my-paper-key' } } }]);
    await expect(key).toHaveValue('');
    expect(await panel.innerText()).not.toContain('my-paper-key');
    await expect(broker.getByRole('list', { name: 'Paper X connections' })).toContainText('1 value(s) saved');

    await broker.getByRole('button', { name: 'Remove main' }).click();
    await expect(panel.getByRole('status')).toContainText('Connection main is removed.');
    expect(calls.at(-1)).toEqual({ method: 'DELETE', path: '/api/broker-connections/main', body: null });
  });

  test('live accounts are listed and cannot be chosen for an order', async ({ page }) => {
    await mock(page);
    const panel = await openBrokers(page);
    await panel.getByRole('button', { name: 'Load accounts' }).click();
    const accounts = panel.getByRole('list', { name: 'Broker accounts' });
    await expect(accounts).toContainText('Paper account');
    await expect(accounts).toContainText('Live account — listed only, never traded.');
    await expect(panel.getByText('Amounts are shown per account in its own currency')).toBeVisible();
    const options = await panel.getByRole('combobox', { name: 'Account' }).locator('option').allTextContents();
    expect(options.join('|')).toContain('PA-1');
    expect(options.join('|')).not.toContain('LIVE-1');
  });

  test('the ticket says what is missing and sends nothing until it is complete', async ({ page }) => {
    const calls = await mock(page);
    const panel = await openBrokers(page);
    await panel.getByRole('button', { name: 'Send paper order' }).click();
    await expect(panel.getByRole('alert').filter({ hasText: 'Choose the paper account' })).toBeVisible();
    await expect(panel.getByRole('alert').filter({ hasText: 'greater than zero' })).toBeVisible();
    expect(calls).toEqual([]);
  });

  test('an order names its connection, broker and account', async ({ page }) => {
    const calls = await mock(page);
    const panel = await openBrokers(page);
    await fillTicket(panel);
    await panel.getByRole('button', { name: 'Send paper order' }).click();
    await expect(panel.getByRole('status')).toContainText('Order #5 was sent to PAPER_X.');
    expect(calls).toHaveLength(1);
    expect(calls[0].body).toMatchObject({ connection: 'main', broker: 'PAPER_X', account: 'PA-1', symbol: 'NVDA', side: 'BUY', type: 'LIMIT', tif: 'DAY', quantity: 2, limit_price: 100 });
    expect(calls[0].body.client_order_id).toMatch(/^ui-/);
    await expect(panel.getByLabel('Symbol')).toHaveValue('');
  });

  test('an unknown outcome is retried under the same client order id, a changed order is not', async ({ page }) => {
    const calls = await mock(page, {
      submit: n => n === 1
        ? { status: 502, body: { status: 'unknown', error: 'the broker gave no clear answer', client_order_id: 'x' } }
        : { status: 200, body: { status: 'success', replayed: false, order: { id: 8 } } },
    });
    const panel = await openBrokers(page);
    await fillTicket(panel);
    await panel.getByRole('button', { name: 'Send paper order' }).click();
    await expect(panel.getByRole('alert').filter({ hasText: 'no clear answer' })).toBeVisible();
    await panel.getByRole('button', { name: 'Send paper order' }).click();
    await expect(panel.getByRole('status')).toContainText('Order #8');
    expect(calls).toHaveLength(2);
    expect(calls[1].body.client_order_id).toBe(calls[0].body.client_order_id);
  });

  test('a refused order shows the reason, and the next one gets a new id', async ({ page }) => {
    const calls = await mock(page, { submit: n => n === 1 ? { status: 403, body: { status: 'error', error: 'live-money execution is refused' } } : { status: 200, body: { status: 'success', replayed: false, order: { id: 9 } } } });
    const panel = await openBrokers(page);
    await fillTicket(panel);
    await panel.getByRole('button', { name: 'Send paper order' }).click();
    await expect(panel.getByRole('alert').filter({ hasText: 'live-money execution is refused' })).toBeVisible();
    await panel.getByRole('button', { name: 'Send paper order' }).click();
    await expect(panel.getByRole('status')).toContainText('Order #9');
    expect(calls[1].body.client_order_id).not.toBe(calls[0].body.client_order_id);
  });

  test('routed orders show the broker\'s status, the last check in UTC and why a check failed', async ({ page }) => {
    const request = { symbol: 'NVDA', side: 'BUY', type: 'LIMIT', quantity: 2 };
    const base = { connection: 'main', broker: 'PAPER_X', account: 'PA-1', created_at: '2026-11-16T09:00:00Z', updated_at: '2026-11-16T09:05:00Z', request };
    await mock(page, { orders: [
      { ...base, id: 3, client_order_id: 'a', status: 'SUBMITTED', reconciled_at: '2026-11-16T09:05:44Z',
        order: { id: 'B-3', client_order_id: 'a', account: 'PA-1', environment: 'paper', symbol: 'NVDA', side: 'BUY', type: 'LIMIT', tif: 'DAY', quantity: 2, filled_quantity: 1, status: 'PARTIALLY_FILLED' } },
      { ...base, id: 2, client_order_id: 'b', status: 'UNKNOWN', error: 'no answer from the broker', reconcile_error: 'the broker is unavailable' },
    ] });
    const panel = await openBrokers(page);
    const orders = panel.getByRole('list', { name: 'Routed orders' });
    await expect(orders).toContainText('PARTIALLY_FILLED (1/2)');
    await expect(orders).toContainText('Checked 2026-11-16 09:05 UTC');
    await expect(orders).toContainText('Not checked yet');
    await expect(orders).toContainText('The last check could not settle it: the broker is unavailable');
    await expect(orders).toContainText('Outcome unknown');
  });

  test('the history of an order lists each step in UTC', async ({ page }) => {
    const request = { symbol: 'NVDA', side: 'BUY', type: 'LIMIT', quantity: 2 };
    await mock(page, {
      orders: [{ id: 3, connection: 'main', broker: 'PAPER_X', account: 'PA-1', client_order_id: 'a', status: 'SUBMITTED', created_at: '2026-11-16T09:00:00Z', updated_at: '2026-11-16T09:05:00Z', request }],
      events: [
        { id: 1, at: '2026-11-16T09:00:01Z', kind: 'routed', status: 'PENDING', detail: 'BUY 2 NVDA, LIMIT, limit 100' },
        { id: 2, at: '2026-11-16T09:00:02Z', kind: 'acknowledged', status: 'SUBMITTED', order_status: 'OPEN' },
        { id: 3, at: '2026-11-16T09:04:10Z', kind: 'update', status: 'SUBMITTED', order_status: 'PARTIALLY_FILLED', filled_quantity: 1 },
      ],
    });
    const panel = await openBrokers(page);
    const toggle = panel.getByRole('button', { name: 'History #3' });
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
    await toggle.click();
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');
    const steps = panel.getByRole('list', { name: 'History of order 3' }).getByRole('listitem');
    await expect(steps).toHaveCount(3);
    await expect(steps.nth(0)).toHaveText('2026-11-16 09:00 UTC Routed to the broker · BUY 2 NVDA, LIMIT, limit 100');
    await expect(steps.nth(2)).toContainText('PARTIALLY_FILLED (1 filled)');
    await toggle.click();
    await expect(panel.getByRole('list', { name: 'History of order 3' })).toHaveCount(0);
  });

  test('a kill switch that is on says working broker orders are not cancelled, and the user can cancel them all', async ({ page }) => {
    const calls = await mock(page, {
      view: brokersView({ halted: true, working_orders: 2 }),
      cancelAll: { results: [{ id: 3, client_order_id: 'a', cancelled: true, status: 'CANCELLED' }, { id: 4, client_order_id: 'b', cancelled: false, error: 'no such order' }], cancelled: 1, remaining: 0 },
    });
    const panel = await openBrokers(page);
    await expect(panel.getByRole('alert').filter({ hasText: 'The kill switch is on' })).toContainText('2 orders are still working at the broker; it does not cancel them.');
    expect(calls).toEqual([]);
    await panel.getByRole('button', { name: 'Cancel all working orders' }).click();
    await expect(panel.getByRole('status')).toContainText('1 order(s) cancelled, 1 could not be cancelled');
    expect(calls).toEqual([{ method: 'POST', path: '/api/broker-orders/cancel-working', body: null }]);
  });

  test('without working orders there is nothing to cancel', async ({ page }) => {
    await mock(page, { view: brokersView({ halted: true, working_orders: 0 }) });
    const panel = await openBrokers(page);
    await expect(panel.getByRole('button', { name: 'Cancel all working orders' })).toHaveCount(0);
    await expect(panel.getByText('The kill switch is on')).toHaveCount(0);
  });

  test('a broker the app cannot use says why and offers no connection', async ({ page }) => {
    await mock(page, { view: brokersView({ brokers: [{ ...paperBroker, usable: false, problems: ['The plugin connects to live accounts only. Live-money execution is not approved, so it is never called.'] }] }) });
    const panel = await openBrokers(page);
    const broker = panel.getByRole('article', { name: 'Paper X' });
    await expect(broker.getByRole('alert')).toContainText('never called');
    await expect(broker.getByRole('button', { name: 'Save connection' })).toHaveCount(0);
  });

  test('is absent for an account without the permission', async ({ page }) => {
    await mock(page, { viewStatus: 403 });
    const panel = await openBrokers(page);
    await expect(panel.getByRole('heading', { name: 'Brokers' })).toHaveCount(0);
  });

  test('has no WCAG A/AA violations in both themes', async ({ page }) => {
    const request = { symbol: 'NVDA', side: 'BUY', type: 'LIMIT', quantity: 2 };
    await mock(page, {
      view: brokersView({ halted: true, working_orders: 1, connections: [{ name: 'main', broker: 'PAPER_X', label: 'main', saved: ['PAPER_KEY'] }] }),
      orders: [{ id: 3, connection: 'main', broker: 'PAPER_X', account: 'PA-1', client_order_id: 'a', status: 'SUBMITTED', reconciled_at: '2026-11-16T09:05:44Z',
        reconcile_error: 'the broker is unavailable', created_at: '2026-11-16T09:00:00Z', updated_at: '2026-11-16T09:05:00Z', request,
        order: { id: 'B-3', client_order_id: 'a', account: 'PA-1', environment: 'paper', symbol: 'NVDA', side: 'BUY', type: 'LIMIT', tif: 'DAY', quantity: 2, filled_quantity: 1, status: 'PARTIALLY_FILLED' } }],
      events: [{ id: 1, at: '2026-11-16T09:00:01Z', kind: 'routed', status: 'PENDING', detail: 'BUY 2 NVDA, LIMIT, limit 100' }],
    });
    const panel = await openBrokers(page);
    await panel.getByRole('button', { name: 'Load accounts' }).click();
    await expect(panel.getByRole('list', { name: 'Broker accounts' })).toBeVisible();
    await panel.getByRole('button', { name: 'History #3' }).click();
    await expect(panel.getByRole('list', { name: 'History of order 3' })).toBeVisible();
    await expect(panel.getByRole('button', { name: 'Cancel all working orders' })).toBeVisible();
    await page.addScriptTag({ content: axeSource });
    await page.addStyleTag({ content: '*, *::before, *::after { transition: none !important; animation: none !important; }' });
    for (const theme of ['light', 'dark']) {
      await page.evaluate(t => document.documentElement.setAttribute('data-bs-theme', t), theme);
      const found = await page.evaluate(async () => {
        // @ts-expect-error axe is injected above
        const r = await window.axe.run(document.querySelector('fse-broker-center'), { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'] } });
        return r.violations.map((v: { id: string; nodes: { html: string }[] }) => `${v.id}: ${v.nodes[0]?.html.slice(0, 110)}`);
      });
      expect(found, `axe in ${theme} theme`).toEqual([]);
    }
  });

  test('stays inside a phone screen', async ({ page }, testInfo) => {
    await mock(page);
    const panel = await openBrokers(page);
    await panel.getByRole('button', { name: 'Load accounts' }).click();
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    expect(overflow, testInfo.project.name).toBeLessThanOrEqual(1);
  });
});
