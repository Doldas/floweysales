import { defineConfig, devices } from '@playwright/test';

/**
 * Browser tests for the compiled Angular app. They run against the production
 * build (`npm run build`, output in ../www/browser) served by e2e/static-server.mjs,
 * with the deterministic mock backend of smoke-fixtures.mjs: no database,
 * credentials, provider calls or trading. Dates and numbers are fixed to
 * en-US and UTC, like the app.
 */
const port = Number(process.env.E2E_PORT ?? 4173);
const ci = !!process.env.CI;

export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: ci,
  retries: ci ? 1 : 0,
  workers: ci ? 2 : undefined,
  reporter: ci ? [['list'], ['github'], ['html', { open: 'never', outputFolder: 'playwright-report' }]] : [['list']],
  outputDir: 'test-results',
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    // The mocked API must stay in charge, so the service worker is blocked.
    serviceWorkers: 'block',
    locale: 'en-US',
    timezoneId: 'UTC',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [
    { name: 'desktop', use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 } } },
    { name: 'mobile', use: { ...devices['Pixel 7'] } },
  ],
  webServer: {
    command: 'node e2e/static-server.mjs',
    url: `http://127.0.0.1:${port}/`,
    reuseExistingServer: !ci,
    timeout: 30_000,
    env: { E2E_PORT: String(port) },
  },
});
