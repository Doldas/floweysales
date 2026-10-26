// Runs with Node's built-in test runner (Node 24+ strips TypeScript types natively):
//   node --test src/app/components/event-adapters.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { adapterProblem, curlExample, deliveryPath, deliverySummary, examplePayload } from './event-adapters.ts';

test('the form needs a name, an event, a JSON pointer and a bounded rate', () => {
  assert.equal(adapterProblem('Prices', 'market.close', '/data/price', 30), '');
  assert.equal(adapterProblem('Prices', 'market.close', '', 1), '');
  assert.match(adapterProblem(' ', 'market.close', '', 30), /name/);
  assert.match(adapterProblem('Prices', '', '', 30), /event/);
  assert.match(adapterProblem('Prices', 'market.close', 'data', 30), /start with \//);
  assert.match(adapterProblem('Prices', 'market.close', '', 601), /1 to 600/);
  assert.match(adapterProblem('Prices', 'market.close', '', 1.5), /whole number/);
});

test('example payloads put the value at the pointer', () => {
  assert.equal(examplePayload(''), '"example"');
  assert.equal(examplePayload('/data/price'), '{"data":{"price":"example"}}');
  assert.equal(examplePayload('/items/1'), '{"items":[null,"example"]}');
  assert.equal(examplePayload('/a~1b'), '{"a/b":"example"}');
});

test('the curl example signs timestamp, delivery ID and body', () => {
  const a = { slug: 'abcdefghijklmnopqrstuvwxyz', payload_pointer: '/value' };
  assert.equal(deliveryPath(a), '/api/hooks/flow-events/abcdefghijklmnopqrstuvwxyz');
  const curl = curlExample('https://flowey.example', a);
  assert.match(curl, /https:\/\/flowey\.example\/api\/hooks\/flow-events\/abcdefghijklmnopqrstuvwxyz/);
  assert.match(curl, /'%s\.%s\.%s' "\$TS" "\$ID" "\$BODY"/);
  assert.match(curl, /X-Flowey-Signature/);
  assert.match(curl, /\{"value":"example"\}/);
});

test('deliveries summarize their outcome', () => {
  assert.equal(deliverySummary({ id: 1, status: 'REJECTED', reason: 'signature mismatch', queued_runs: 0, received_at: '' }), 'Rejected: signature mismatch');
  assert.equal(deliverySummary({ id: 2, status: 'ACCEPTED', delivery_id: 'evt-1', queued_runs: 1, received_at: '' }), 'Accepted evt-1 · queued 1 run');
  assert.equal(deliverySummary({ id: 3, status: 'ACCEPTED', queued_runs: 2, received_at: '' }), 'Accepted · queued 2 runs');
});
