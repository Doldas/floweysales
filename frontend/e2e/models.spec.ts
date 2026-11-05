import { expect, open, test } from './fixtures';

test.describe('model profiles', () => {
  test('a user picks a profile by name for the research agent', async ({ page, isMobile }) => {
    await open(page, 'agent');
    // On phones the agent settings start collapsed; the open state is remembered across the reload below.
    if (isMobile) await page.locator('.settings-toggle').click();
    const select = page.getByLabel('Model', { exact: true });
    await expect(select).toBeVisible();
    await expect(select.locator('option').first()).toHaveText('Default profile (Fast and cheap)');
    await expect(page.getByText('small-model · can call tools · 32k context')).toBeVisible();
    await select.selectOption({ label: 'Deep research' });
    await expect(page.getByText('big-model · can call tools · context size unknown · cost unknown')).toBeVisible();

    const request = page.waitForRequest(r => r.url().endsWith('/api/ai/chat') && r.method() === 'POST');
    await page.getByRole('textbox', { name: 'Message the research agent' }).fill('How is NVDA doing?');
    await page.keyboard.press('Enter');
    expect((await request).postDataJSON().profile_id).toBe(2);
    // The profile is remembered.
    await page.reload();
    await expect(page.getByLabel('Model', { exact: true })).toHaveValue('2');
  });

  test('each team member can use its own profile, and the choice is sent and shown', async ({ page }) => {
    await open(page, 'team');
    await page.getByLabel('Ashgrove', { exact: true }).selectOption({ label: 'Deep research' });
    const request = page.waitForRequest(r => r.url().endsWith('/api/ai/team') && r.method() === 'POST');
    await page.getByRole('button', { name: 'Ask the team' }).click();
    const body = (await request).postDataJSON();
    expect(body.members).toEqual([{ persona: 'owner', profile_id: 2 }, { persona: 'teacher' }]);
    await expect(page.getByRole('article', { name: 'Ashgrove' })).toContainText('big-model');
    await expect(page.getByRole('article', { name: 'Holm' })).toContainText('small-model');
  });

  test('a team can be saved and a saved team loaded', async ({ page }) => {
    await open(page, 'team');
    await page.getByLabel('Name for this team').fill('My panel');
    const save = page.waitForRequest(r => r.url().endsWith('/api/ai/teams') && r.method() === 'POST');
    await page.getByRole('button', { name: 'Save team' }).click();
    expect((await save).postDataJSON()).toMatchObject({ name: 'My panel', chair: true, members: [{ persona: 'owner', profile_id: null }, { persona: 'teacher', profile_id: null }] });
    await expect(page.getByText('Team saved.')).toBeVisible();

    await page.getByLabel('Saved team', { exact: true }).selectOption({ label: 'Value panel (2)' });
    await page.getByRole('button', { name: 'Load' }).click();
    await expect(page.getByLabel('Ashgrove', { exact: true })).toHaveValue('2');
    await expect(page.getByLabel('Name for this team')).toHaveValue('Value panel');
  });
});

test.describe('administrator', () => {
  test('prepares profiles in Settings', async ({ page }) => {
    await open(page, 'settings');
    await page.getByRole('tab', { name: 'Model profiles' }).click();
    await expect(page.getByRole('heading', { name: 'Model profiles' })).toBeVisible();
    await expect(page.getByRole('strong').filter({ hasText: 'Deep research' })).toBeVisible();
    await expect(page.getByText('falls back to')).toHaveCount(0);
    await expect(page.getByText('context size unknown')).toBeVisible(); // unknown stays unknown
    await page.getByRole('button', { name: 'Edit' }).first().click();
    await expect(page.getByRole('heading', { name: 'Edit profile' })).toBeVisible();
  });
});
