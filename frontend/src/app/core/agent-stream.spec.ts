import test from 'node:test';
import assert from 'node:assert/strict';
import { takeSseFrames, sseSequence } from './agent-stream.ts';

test('partial SSE chunks and CRLF boundaries preserve event order', () => {
  const a = takeSseFrames('', 'id: 1\r');
  assert.deepEqual(a.frames, []);
  const b = takeSseFrames(a.remainder, '\ndata: {"type":"delta","content":"hello"}\r\n\r');
  assert.deepEqual(b.frames, []);
  const c = takeSseFrames(b.remainder, '\nid: 2\ndata: {"type":"stream_end"}\n\n');
  assert.equal(c.frames.length, 2);
  assert.equal(sseSequence(c.frames[0]), 1);
  assert.equal(sseSequence(c.frames[1]), 2);
  assert.equal(JSON.parse(c.frames[0].split('\n').find(s => s.startsWith('data:'))!.slice(5)).content, 'hello');
});

test('heartbeats and malicious cursors do not change the replay position', () => {
  const input = takeSseFrames('', ': keep-alive\n\nid: 3\ndata: {"type":"delta","content":"x"}\n\n');
  assert.equal(input.frames.length, 2);
  assert.equal(sseSequence(input.frames[0]), null);
  assert.equal(sseSequence(input.frames[1]), 3);
  assert.equal(sseSequence('id: -1'), null);
  assert.equal(sseSequence('id: 99999999999999999999'), null);
  assert.equal(sseSequence('data: {"id":5}'), null);
});
