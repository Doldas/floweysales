import { expect, open, test } from './fixtures';

test.describe('vision model settings', () => {
  test('a SYS_ADMIN sees the card, cannot test before configuring, and a blank key is not sent', async ({ page }) => {
    await open(page, 'settings');
    const status = page.getByTestId('vision-status');
    await expect(status).toContainText('Not configured');
    await expect(page.getByRole('button', { name: 'Test with an image' })).toBeDisabled();

    await page.getByLabel('Vision model', { exact: true }).fill('vision-1');
    await page.getByLabel('OpenAI-compatible endpoint URL').nth(1).fill('https://vision.example/v1');
    await page.getByLabel('This model accepts image input').check();
    const save = page.waitForRequest(r => r.url().endsWith('/api/admin/vision') && r.method() === 'PUT');
    await page.getByRole('button', { name: 'Save vision settings' }).click();
    const body = (await save).postDataJSON();
    expect(body).toMatchObject({ model: 'vision-1', base_url: 'https://vision.example/v1', supports_images: true });
    expect(body).not.toHaveProperty('api_key');
  });
});
