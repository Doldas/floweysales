import assert from 'node:assert/strict';
import test from 'node:test';

import { EMPTY_NOTICES, applyNotice, dismissNotice, finishNotices, recoveryLine } from './agent-notices.ts';

test('a retry is a status line, not a persistent notice', () => {
  const s = applyNotice(EMPTY_NOTICES, { code: 'retry', severity: 'info', content: 'Retrying, attempt 2 of 3.', detail: 'HTTP 400' });
  assert.equal(s.status, 'Retrying, attempt 2 of 3.');
  assert.equal(s.statusDetail, 'HTTP 400');
  assert.equal(s.notices.length, 0);
});

test('a successful retry clears the status and is counted, not shown as an error', () => {
  let s = applyNotice(EMPTY_NOTICES, { code: 'retry', content: 'Retrying, attempt 2 of 3.' });
  s = applyNotice(s, { code: 'recovered', content: 'Recovered after 2 attempts.' });
  assert.equal(s.status, '');
  assert.equal(s.notices.length, 0);
  assert.match(recoveryLine(s), /retried successfully/);
});

test('warnings stay until dismissed, one at a time', () => {
  let s = applyNotice(EMPTY_NOTICES, { code: 'task_incomplete', severity: 'warning', content: 'The task is incomplete.' });
  s = applyNotice(s, { code: 'notice', severity: 'warning', content: 'The context window is full.' });
  assert.equal(s.notices.length, 2);
  s = dismissNotice(s, s.notices[0].id);
  assert.deepEqual(s.notices.map(n => n.text), ['The context window is full.']);
});

test('a run that ends mid-retry does not leave the status behind', () => {
  const s = finishNotices(applyNotice(EMPTY_NOTICES, { code: 'retry', content: 'Retrying, attempt 3 of 3.' }));
  assert.equal(s.status, '');
});

test('an empty notice adds nothing and nothing retried gives no recovery line', () => {
  assert.equal(applyNotice(EMPTY_NOTICES, { code: 'notice', content: '  ' }).notices.length, 0);
  assert.equal(recoveryLine(EMPTY_NOTICES), '');
});
