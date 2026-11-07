import { defineConfig, devices } from '@playwright/test';

// Documentation is built from the same checked Markdown that Pages deploys.
// It is served from a local throwaway static directory: no live accounts,
// external API keys or network providers are used.
const port = 4281;
export default defineConfig({
  testDir: './docs-e2e',
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never', outputFolder: 'playwright-docs-report' }]] : [['list']],
  outputDir: 'docs-test-results',
  use: { baseURL: `http://127.0.0.1:${port}`, screenshot: 'only-on-failure' },
  projects: [
    { name: 'docs-desktop', use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 } } },
    { name: 'docs-phone', use: { ...devices['Pixel 7'] } },
  ],
  webServer: {
    command: `node ../scripts/build-docs.mjs --out ../_site && python3 -m http.server ${port} --bind 127.0.0.1 --directory ../_site`,
    url: `http://127.0.0.1:${port}/guide/index.html`,
    reuseExistingServer: !process.env.CI,
    timeout: 30_000,
  },
});
