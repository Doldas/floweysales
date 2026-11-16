import assert from 'node:assert/strict';
import test from 'node:test';
import {
  ledgerStatusLabel, money, needsLimit, needsStop, newClientOrderId, orderBody, outcomeUnknown, splitRoute, ticketIssues, type Ticket,
} from './broker-ticket.ts';

const ticket = (over: Partial<Ticket> = {}): Ticket => ({
  route: 'main/PA-1', symbol: 'nvda', side: 'BUY', type: 'LIMIT', tif: 'DAY', quantity: '2', limitPrice: '100', stopPrice: '', ...over,
});

test('each order type asks for the prices it uses', () => {
  assert.deepEqual(['MARKET', 'LIMIT', 'STOP', 'STOP_LIMIT'].map(t => [needsLimit(t as never), needsStop(t as never)]),
    [[false, false], [true, false], [false, true], [true, true]]);
});

test('a complete ticket has no issues', () => {
  assert.deepEqual(ticketIssues(ticket()), []);
  assert.deepEqual(ticketIssues(ticket({ type: 'MARKET', limitPrice: '' })), []);
  assert.deepEqual(ticketIssues(ticket({ type: 'STOP_LIMIT', stopPrice: '95' })), []);
});

test('the ticket says what is missing before anything is sent', () => {
  assert.equal(ticketIssues(ticket({ route: '' })).length, 1);
  assert.equal(ticketIssues(ticket({ symbol: ' ' })).length, 1);
  assert.equal(ticketIssues(ticket({ symbol: 'NV DA' })).length, 1);
  assert.equal(ticketIssues(ticket({ quantity: '0' })).length, 1);
  assert.equal(ticketIssues(ticket({ quantity: '-3' })).length, 1);
  assert.equal(ticketIssues(ticket({ quantity: 'abc' })).length, 1);
  assert.equal(ticketIssues(ticket({ limitPrice: '' })).length, 1);
  assert.equal(ticketIssues(ticket({ type: 'STOP', limitPrice: '', stopPrice: '' })).length, 1);
  assert.equal(ticketIssues(ticket({ route: '', quantity: '', limitPrice: '' })).length, 3);
});

test('the route names the connection and the account', () => {
  assert.deepEqual(splitRoute('main/PA-1'), { connection: 'main', account: 'PA-1' });
  assert.deepEqual(splitRoute('/PA-1'), { connection: '', account: 'PA-1' });
  assert.deepEqual(splitRoute('main/PA/1'), { connection: 'main', account: 'PA/1' });
});

test('the body names its route and sends only the prices the type uses', () => {
  assert.deepEqual(orderBody(ticket(), 'PAPER_X', 'ui-1'), {
    connection: 'main', broker: 'PAPER_X', account: 'PA-1', client_order_id: 'ui-1', symbol: 'NVDA', side: 'BUY', type: 'LIMIT', tif: 'DAY', quantity: 2, limit_price: 100,
  });
  const market = orderBody(ticket({ type: 'MARKET', limitPrice: '100', stopPrice: '90' }), 'PAPER_X', 'ui-2');
  assert.equal('limit_price' in market, false);
  assert.equal('stop_price' in market, false);
  const stopLimit = orderBody(ticket({ type: 'STOP_LIMIT', stopPrice: '95' }), 'PAPER_X', 'ui-3');
  assert.equal(stopLimit.limit_price, 100);
  assert.equal(stopLimit.stop_price, 95);
});

test('client order ids are unique, bounded and safe', () => {
  const a = newClientOrderId();
  assert.notEqual(a, newClientOrderId());
  assert.match(a, /^[A-Za-z0-9._:-]{1,128}$/);
  assert.equal(newClientOrderId(() => 'x'.repeat(500)).length, 128);
});

test('only a missing answer keeps the same client order id for a retry', () => {
  assert.equal(outcomeUnknown(0, null), true);
  assert.equal(outcomeUnknown(504, null), true);
  assert.equal(outcomeUnknown(502, { status: 'unknown' }), true);
  assert.equal(outcomeUnknown(502, { status: 'error' }), false);
  assert.equal(outcomeUnknown(403, { status: 'error' }), false);
  assert.equal(outcomeUnknown(422, null), false);
});

test('ledger statuses are plain words', () => {
  assert.equal(ledgerStatusLabel('SUBMITTED'), 'Sent to the broker');
  assert.match(ledgerStatusLabel('UNKNOWN'), /refresh/i);
  assert.equal(ledgerStatusLabel('SOMETHING'), 'SOMETHING');
});

test('money keeps its own currency', () => {
  assert.equal(money(1234.5, 'SEK'), '1,234.50 SEK');
  assert.equal(money(0, 'USD'), '0.00 USD');
});
