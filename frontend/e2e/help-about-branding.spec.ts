import { expect, open, test } from './fixtures';

// Branding and theme consistency of Help and About (issue #205). Screenshots
// are attached for desktop and phone in light and dark so a reviewer can see
// them in the CI report; the assertions fail on drift from the app shell.
const ICON = 'icons/icon.svg';
const DOCS = 'https://doldas.github.io/floweysales/';
const REPO = 'https://github.com/Doldas/floweysales';

for (const route of ['help', 'about'] as const) {
  for (const theme of ['light', 'dark'] as const) {
    test(`/${route} uses the app icon, theme and layout (${theme})`, async ({ page, isMobile }, testInfo) => {
      await open(page, route);
      const html = page.locator('html');
      // Set the attribute the app itself uses, so the check does not depend on the storage key.
      await html.evaluate((el, t) => el.setAttribute('data-bs-theme', t), theme);
      await expect(html).toHaveAttribute('data-bs-theme', theme);

      // Same official icon file as the shell, and a visible product name.
      const pageIcon = page.locator(`#main-content img[src="${ICON}"]`).first();
      await expect(pageIcon).toBeVisible();
      await expect(page.locator(`img.brand-mark[src="${ICON}"]`).first()).toBeAttached();
      const loaded = await pageIcon.evaluate((img: HTMLImageElement) => img.complete && img.naturalWidth > 0);
      expect(loaded, 'icon loads').toBe(true);

      // The page sits on the app's typography and surface tokens, not its own.
      const colors = await page.evaluate(() => {
        const cs = (el: Element | null) => (el ? getComputedStyle(el) : null);
        const card = document.querySelector('#main-content .card');
        return {
          body: cs(document.body)!.backgroundColor,
          font: cs(document.body)!.fontFamily,
          cardFont: cs(card)?.fontFamily,
          cardBg: cs(card)?.backgroundColor,
          cardText: cs(card)?.color,
        };
      });
      expect(colors.cardFont).toBe(colors.font);
      expect(colors.cardBg).not.toBe(colors.cardText);
      if (theme === 'dark') expect(colors.body).not.toBe('rgb(255, 255, 255)');

      // No sideways scrolling.
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      expect(overflow).toBeLessThanOrEqual(1);

      await testInfo.attach(`${route}-${theme}-${isMobile ? 'mobile' : 'desktop'}.png`, {
        body: await page.screenshot({ fullPage: true, animations: 'disabled' }),
        contentType: 'image/png',
      });
    });
  }
}

test('Help and About link to the documentation site and the GitHub project', async ({ page }) => {
  await open(page, 'help');
  await expect(page.locator(`#main-content a[href^="${DOCS}"]`).first()).toBeVisible();
  await open(page, 'about');
  await expect(page.locator(`#main-content a[href="${DOCS}"]`)).toBeVisible();
  for (const href of [REPO, `${REPO}/issues`, `${REPO}/releases`]) {
    await expect(page.locator(`#main-content a[href="${href}"]`)).toBeVisible();
  }
});
