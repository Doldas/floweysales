import { expect, test as base, type Locator, type Page } from '@playwright/test';

// The shared mock backend of the smoke tests (plain JavaScript).
// @ts-ignore: no type declarations for the .mjs fixtures
import { installMocks } from '../smoke-fixtures.mjs';

export type Role = 'SYS_ADMIN' | 'USER';

interface Fixtures {
  /** Role the mocked session has. */
  role: Role;
  /** Permissions granted to a USER session (not browser permissions). */
  grants: string[];
  /** Uncaught page errors; every test fails if one occurs. */
  pageErrors: string[];
  /** API paths the page called that the mock backend does not know. */
  unmocked: Set<string>;
}

export const test = base.extend<Fixtures>({
  role: ['SYS_ADMIN', { option: true }],
  grants: [['flows.read'], { option: true }],
  pageErrors: [async ({ page }, use) => {
    const errors: string[] = [];
    page.on('pageerror', e => errors.push(e.message));
    await use(errors);
    expect(errors, 'uncaught page errors').toEqual([]);
  }, { auto: true }],
  // Installed for every test, whether or not it reads the set of unknown calls.
  unmocked: [async ({ page, role, grants }, use) => {
    await use(await installMocks(page, { role, permissions: grants, email: 'e2e@example.test' }));
  }, { auto: true }],
});

export { expect };

/** Opens the app at a hash route and waits until the page has rendered. */
export async function open(page: Page, route = ''): Promise<void> {
  await page.goto(`/#/${route}`);
  await page.locator('#main-content > *:not(router-outlet)').first().waitFor({ timeout: 20_000 });
}

/** The navigation link, opening the drawer first on small screens. */
export async function navLink(page: Page, name: string | RegExp): Promise<Locator> {
  const toggle = page.getByRole('button', { name: 'Open navigation' });
  if (await toggle.isVisible()) await toggle.click();
  return page.getByRole('navigation', { name: 'Workspace sections' }).getByRole('link', { name });
}
