import assert from 'node:assert/strict';
import test from 'node:test';

import { chatPage } from './chat-page.ts';

test('the page is the first route segment, home for the root', () => {
  assert.equal(chatPage('/'), 'home');
  assert.equal(chatPage(''), 'home');
  assert.equal(chatPage('/collections'), 'collections');
  assert.equal(chatPage('/collections/12?tab=news#top'), 'collections');
  assert.equal(chatPage('/?symbol=AAPL'), 'home');
  assert.equal(chatPage('/tools/sec-plugin'), 'tools');
});
