import { defineConfig, devices } from '@playwright/test';

// Separate from playwright.config.ts: never start the mock server and never
// run this suite from untrusted PRs. The target is deliberately hard-coded.
const target = 'https://floweysales.gamitronservers.com/';
const ci = Boolean(process.env.CI);

export default defineConfig({
  testDir: './live-e2e',
  fullyParallel: false,
  forbidOnly: ci,
  workers: 1,
  retries: 0,
  timeout: 45_000,
  expect: { timeout: 12_000 },
  reporter: ci
    ? [['list'], ['github'], ['junit', { outputFile: 'live-report/results.xml' }], ['html', { outputFolder: 'live-report/html', open: 'never' }]]
    : [['list']],
  outputDir: 'live-test-results',
  use: {
    baseURL: target,
    serviceWorkers: 'block',
    locale: 'en-US',
    timezoneId: 'UTC',
    navigationTimeout: 25_000,
    actionTimeout: 15_000,
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
    // Browser traces can contain auth cookies, request bodies and model
    // responses. Do not collect them on the real remote instance.
    trace: 'off',
  },
  projects: [
    { name: 'live-desktop', use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 } } },
    { name: 'live-mobile', use: { ...devices['Pixel 7'], browserName: 'chromium' } },
  ],
});
