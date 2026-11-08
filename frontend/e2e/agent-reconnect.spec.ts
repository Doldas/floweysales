import { expect, open, test } from './fixtures';

test('Research Agent reconnects to the same run after a cut stream without repeating tools or tokens', async ({ page }) => {
  const id = 'synthetic-reconnect-test';
  let initialPosts = 0;
  let replayGets = 0;
  await page.route('**/api/ai/chat', async route => {
    initialPosts++;
    await route.fulfill({
      status: 200,
      headers: { 'content-type': 'text/event-stream', 'x-agent-run-id': id },
      // A proxy can close an HTTP 200 SSE response without a terminal event.
      body: 'id: 1\ndata: {"type":"delta","content":"First half " }\n\n',
    });
  });
  await page.route(/\/api\/ai\/runs\/[^/]+\/events(?:\?|$)/, async route => {
    replayGets++;
    const url = new URL(route.request().url());
    expect(url.pathname).toBe(`/api/ai/runs/${id}/events`);
    expect(url.searchParams.get('after')).toBe('1');
    expect(route.request().method()).toBe('GET');
    await route.fulfill({
      status: 200,
      headers: { 'content-type': 'text/event-stream', 'x-agent-run-id': id },
      body: 'id: 2\ndata: {"type":"delta","content":"second half."}\n\nid: 3\ndata: {"type":"stream_end"}\n\n',
    });
  });

  await open(page, 'agent');
  const input = page.getByRole('textbox', { name: 'Message the research agent' });
  await input.fill('Analyse an imaginary market without restarting the run.');
  await input.press('Enter');

  const answer = page.locator('.msg-agent .bubble.chat-markdown');
  await expect(answer).toContainText('First half second half.');
  await expect(page.getByRole('button', { name: 'Send' })).toBeVisible();
  expect(initialPosts, 'the original model/tools must execute once').toBe(1);
  expect(replayGets, 'only the missing event suffix must be downloaded').toBe(1);
  await expect(answer).toHaveCount(1);
  expect((await answer.innerText()).match(/First half/g)?.length).toBe(1);
});
