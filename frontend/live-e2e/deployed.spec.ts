import { expect, test, type Page } from '@playwright/test';

// Deliberately restricted to a disposable read-only account. Credentials are
// injected through protected GitHub Environment secrets, never test fixtures.
const email = process.env.LIVE_E2E_EMAIL;
const password = process.env.LIVE_E2E_PASSWORD;

async function login(page: Page): Promise<void> {
  if (!email || !password) throw new Error('Missing protected LIVE_E2E_EMAIL / LIVE_E2E_PASSWORD secrets');

  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible();

  // Authenticate through the real API to avoid typing the password into a
  // browser recording. page.request shares the browser context cookie jar.
  const response = await page.request.post('/api/auth/login', {
    data: { email, password },
    failOnStatusCode: false,
  });
  expect(response.ok(), `Autotest login failed (HTTP ${response.status()}). Check test account and credentials.`).toBeTruthy();

  const session = await page.request.get('/api/auth/me');
  expect(session.ok(), 'Autotest account session was not accepted').toBeTruthy();
  const data = await session.json();
  expect(data?.user?.email, 'Authenticated user must be the designated autotest account').toBe(email);

  await page.goto('/#/');
  await expect(page.getByRole('heading', { name: 'Sign in' })).toHaveCount(0);
}

test.describe('Deployed FloweySales — real backend, read-only account', () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
  });

  test('auth session and app navigation work', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', e => errors.push(e.message.slice(0, 250)));

    await page.goto('/#/help');
    await expect(page.getByRole('heading', { name: 'Help', exact: true }).first()).toBeVisible();

    await page.goto('/#/agent');
    await expect(page.getByRole('heading', { name: 'Research Agent' })).toBeVisible();
    await expect(page.getByRole('textbox', { name: 'Message the research agent' })).toBeVisible();

    await page.goto('/#/markets');
    await expect(page).toHaveTitle(/Markets.*FloweySales/i);

    expect(errors, 'Uncaught browser exceptions during read-only navigation').toEqual([]);
  });

  test('research agent is configured on the deployed instance', async ({ page }) => {
    await page.goto('/#/agent');
    await expect(page.getByRole('heading', { name: 'Research Agent' })).toBeVisible();
    await expect(page.getByText('Model not configured', { exact: false })).toHaveCount(0);
    await expect(page.getByRole('textbox', { name: 'Message the research agent' })).toBeEnabled();
  });

  test('optional bounded real-model question — explicitly opted in', async ({ page }, testInfo) => {
    test.skip(process.env.LIVE_E2E_AI !== 'true', 'Real AI requests are opt-in only');
    test.skip(testInfo.project.name !== 'live-desktop', 'Run AI probe on desktop once, not on both devices');

    test.setTimeout(120_000);
    await page.goto('/#/agent');
    const composer = page.getByRole('textbox', { name: 'Message the research agent' });
    await expect(composer).toBeEnabled();

    // A short question that should not need provider tools or write actions.
    // Use a test-only user with no permission to run flows, modify settings,
    // send notifications or access sensitive workspaces.
    await composer.fill('Autotest check: explain in one brief sentence what a moving average is. Do not call tools, modify data, or start any workflows.');
    await page.getByRole('button', { name: 'Send', exact: true }).click();

    await expect(page.locator('.msg.msg-agent .bubble').last()).toContainText(/\S/, { timeout: 95_000 });
    await expect(page.locator('[role="alert"].alert-danger')).toHaveCount(0);
  });
});
