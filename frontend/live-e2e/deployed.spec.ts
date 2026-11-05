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

  test('the deployed build is healthy and identifies itself', async ({ page }, testInfo) => {
    const health = await page.request.get('/healthz', { failOnStatusCode: false });
    expect(health.ok(), `Health check failed (HTTP ${health.status()}); the deployment or its database is down`).toBeTruthy();

    // The checkout SHA only says which test code ran. The commit baked into the
    // deployed image says what these results are about.
    const about = await (await page.request.get('/api/about')).json();
    expect(about.name).toBe('FloweySales');
    const commit = String(about.commit ?? '');
    expect(commit, 'The deployed build reports no commit (development build?), so the results cannot be tied to a version').toMatch(/^[0-9a-f]{7,40}$/);
    testInfo.annotations.push({ type: 'deployed-commit', description: commit });
    console.log(`Deployed commit: ${commit}${about.build_time ? ` (built ${about.build_time})` : ''}`);

    // Optional gate for after-deploy runs: fail if the instance is not on the expected version.
    const expected = (process.env.LIVE_E2E_EXPECT_COMMIT ?? '').trim().toLowerCase();
    if (expected) {
      expect(commit.startsWith(expected) || expected.startsWith(commit), `Deployed commit ${commit} is not the expected ${expected}`).toBeTruthy();
    }
  });

  test('research agent is configured on the deployed instance', async ({ page }) => {
    await page.goto('/#/agent');
    await expect(page.getByRole('heading', { name: 'Research Agent' })).toBeVisible();
    await expect(page.getByText('Model not configured', { exact: false })).toHaveCount(0);
    await expect(page.getByRole('textbox', { name: 'Message the research agent' })).toBeEnabled();
  });

  test('real multi-turn Research Agent conversation — explicitly opted in', async ({ page }, testInfo) => {
    test.skip(process.env.LIVE_E2E_AI !== 'true', 'Real AI conversation tests require explicit opt-in');
    test.skip(testInfo.project.name !== 'live-desktop', 'Send real AI messages on desktop only to avoid duplicate costs');

    test.setTimeout(240_000);
    await page.goto('/#/agent');
    await expect(page.getByRole('heading', { name: 'Research Agent' })).toBeVisible();
    const composer = page.getByRole('textbox', { name: 'Message the research agent' });
    await expect(composer).toBeEnabled();

    // These are real submitted messages to the configured model. The account
    // must lack permissions for side-effecting tools and see only test data.
    // Tests verify response relevance, a follow-up in the same conversation,
    // completion, and the absence of UI-visible errors without requiring an
    // exact (necessarily nondeterministic) AI answer.
    async function sendAndExpectAnswer(prompt: string, expected: RegExp, sequence: number): Promise<void> {
      const messages = page.locator('.msg.msg-agent .bubble');
      const before = await messages.count();
      await composer.fill(prompt);
      await page.getByRole('button', { name: 'Send', exact: true }).click();
      await expect(page.locator('.msg.msg-user .bubble').last()).toContainText(prompt, { timeout: 10_000 });
      await expect.poll(() => messages.count(), { timeout: 100_000 }).toBeGreaterThan(before);
      const answer = messages.last();
      await expect(answer).toContainText(expected, { timeout: 100_000 });
      await expect(page.getByRole('button', { name: 'Stop the agent' })).toHaveCount(0, { timeout: 100_000 });
      await expect(page.locator('[role="alert"].alert-danger')).toHaveCount(0);
      // Report only a generic success marker; never print model replies or
      // private tool outputs into GitHub Actions logs.
      console.log(`Real AI conversation turn ${sequence}: response received and completed`);
    }

    await sendAndExpectAnswer(
      'In a short paragraph, explain what a simple moving average (SMA) means for stock prices. This is a test conversation. Do not use tools or change any data.',
      /moving average|\bSMA\b|average.*price/i,
      1,
    );
    await sendAndExpectAnswer(
      'Follow-up to my previous question: how does a 20-day SMA differ from a 50-day SMA? Answer briefly. Do not use tools or change any data.',
      /20.day|50.day|twenty.day|fifty.day/i,
      2,
    );
    await expect(page.locator('.msg.msg-user .bubble')).toHaveCount(2);
    await expect(page.locator('.msg.msg-agent .bubble')).toHaveCount(2);
  });
});
