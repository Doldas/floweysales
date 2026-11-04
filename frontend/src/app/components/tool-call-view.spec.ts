// Runs with Node's built-in test runner:
//   node --test src/app/components/tool-call-view.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { argsSummary, displayNotes, durationText, findLines, finishCall, formatBytes, prettyText, startCall } from './tool-call-view.ts';

test('results find their calls by id, so parallel calls to one tool cannot swap', () => {
  let calls = startCall([], { name: 'get_price_history', arguments: '{"symbol":"NVDA"}', call_id: 'a', seq: 1 }, 1000);
  calls = startCall(calls, { name: 'get_price_history', arguments: '{"symbol":"AMD"}', call_id: 'b', seq: 2 }, 1000);
  // The second call finishes first.
  calls = finishCall(calls, { name: 'get_price_history', call_id: 'b', result: '{"symbol":"AMD"}', duration_ms: 40 }, 1100);
  assert.deepEqual(calls.map(c => [c.callId, c.status, c.result]), [['a', 'running', undefined], ['b', 'ok', '{"symbol":"AMD"}']]);
  calls = finishCall(calls, { name: 'get_price_history', call_id: 'a', result: '{"symbol":"NVDA"}' }, 1500);
  assert.equal(calls[0].result, '{"symbol":"NVDA"}');
  assert.equal(calls[0].durationMs, 500, 'without a server duration the client measures it');
});

test('an older server without call ids still matches by tool name', () => {
  let calls = startCall([], { name: 'search_web' }, 0);
  calls = finishCall(calls, { name: 'search_web', result: 'x' }, 10);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].status, 'ok');
});

test('a failed call keeps its error apart from the result', () => {
  let calls = startCall([], { name: 'get_sec_section', call_id: 'c' }, 0, 'Ashgrove');
  calls = finishCall(calls, { call_id: 'c', tool_error: 'filing not found', result: '{"error":"filing not found"}' }, 5);
  assert.equal(calls[0].status, 'error');
  assert.equal(calls[0].error, 'filing not found');
  assert.equal(calls[0].actor, 'Ashgrove');
});

test('a result without a start is still shown, not dropped', () => {
  const calls = finishCall([], { name: 'x', call_id: 'z', result: 'r', duration_ms: 3 }, 0);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].durationMs, 3);
});

test('a cut or masked display says so and how big the whole was', () => {
  const notes = displayNotes({ callId: 'a', name: 'n', status: 'ok', truncated: true, originalBytes: 120_000, redacted: true });
  assert.equal(notes.length, 2);
  assert.match(notes[0], /117\.2 KB/);
  assert.match(notes[1], /masked/);
  assert.deepEqual(displayNotes({ callId: 'a', name: 'n', status: 'ok' }), []);
});

test('formatting helpers', () => {
  assert.equal(prettyText('{"a":1}'), '{\n  "a": 1\n}');
  assert.equal(prettyText('not json'), 'not json');
  assert.equal(prettyText(undefined), '');
  assert.equal(formatBytes(820), '820 B');
  assert.equal(formatBytes(1536), '1.5 KB');
  assert.equal(argsSummary('{"symbol":"NVDA","limit":5}'), 'symbol: NVDA · limit: 5');
  assert.equal(durationText(820), '820 ms');
  assert.equal(durationText(4200), '4.2 s');
});

test('finding in the output keeps matching lines only', () => {
  const text = 'price: 10\nvolume: 5\nprice change: 2';
  assert.deepEqual(findLines(text, 'PRICE'), { text: 'price: 10\nprice change: 2', matches: 2 });
  assert.deepEqual(findLines(text, ' '), { text, matches: 0 });
  assert.deepEqual(findLines(text, 'nothing'), { text: '', matches: 0 });
});
