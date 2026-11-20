import assert from 'node:assert/strict';
import test from 'node:test';

import { boardInstruments, boardKey, dayPercent, flashClass, heatLevel, nextEntry, rangePosition, sameBoard, signedPercent, topMovers } from './quote-board.ts';

const quote = (price: number, day?: object) => ({ symbol: 'X', price, currency: 'USD', ...(day ? { day } : {}) }) as never;

test('the board lists tracked instruments first, then starred ones, each once', () => {
  const list = boardInstruments(
    [{ symbol: 'nvda', provider: 'yahoo' }, { symbol: 'BTCUSDT', provider: 'BINANCE' }],
    [{ symbol: 'NVDA', provider: 'YAHOO', name: 'NVIDIA' }, { symbol: 'VOLV-B', provider: 'YAHOO' }],
  );
  assert.deepEqual(list.map(boardKey), ['YAHOO|NVDA', 'BINANCE|BTCUSDT', 'YAHOO|VOLV-B']);
  assert.deepEqual(list[0], { symbol: 'NVDA', provider: 'YAHOO', name: 'NVIDIA', tracked: true, favorite: true });
  assert.equal(list[2].tracked, false);
  assert.equal(list[2].favorite, true);
});

test('the board stops at its limit and skips blank entries', () => {
  const many = Array.from({ length: 30 }, (_, i) => ({ symbol: `S${i}`, provider: 'YAHOO' }));
  assert.equal(boardInstruments(many, [], 20).length, 20);
  assert.deepEqual(boardInstruments([{ symbol: ' ', provider: 'YAHOO' }], [{ symbol: 'A', provider: '' }]), []);
});

test('two boards are the same only with the same instruments, order and flags', () => {
  const a = boardInstruments([{ symbol: 'A', provider: 'P' }], [{ symbol: 'B', provider: 'P' }]);
  assert.ok(sameBoard(a, boardInstruments([{ symbol: 'a', provider: 'p' }], [{ symbol: 'b', provider: 'p' }])));
  assert.ok(!sameBoard(a, boardInstruments([], [{ symbol: 'A', provider: 'P' }, { symbol: 'B', provider: 'P' }])));
});

test('a new quote records which way the price moved and counts each move', () => {
  const first = nextEntry(undefined, quote(100));
  assert.deepEqual({ move: first.move, seq: first.moveSeq }, { move: '', seq: 0 });
  const up = nextEntry(first, quote(101));
  assert.deepEqual({ move: up.move, seq: up.moveSeq }, { move: 'up', seq: 1 });
  const same = nextEntry(up, quote(101));
  assert.deepEqual({ move: same.move, seq: same.moveSeq }, { move: 'up', seq: 1 });
  const down = nextEntry(same, quote(99));
  assert.deepEqual({ move: down.move, seq: down.moveSeq }, { move: 'down', seq: 2 });
  assert.equal(flashClass(up), 'fs-flash-up-a');
  assert.equal(flashClass(down), 'fs-flash-down-b');
  assert.equal(flashClass(first), '');
});

test('the day change comes from the quote, or from its previous close for a newer price', () => {
  const q = quote(99, { previous_close: 100, change_percent: -1 });
  assert.equal(dayPercent(q), -1);
  assert.equal(dayPercent(q, 99), -1);
  assert.equal(Number(dayPercent(q, 102)!.toFixed(6)), 2);
  assert.equal(dayPercent(quote(99)), null);
  assert.equal(dayPercent(undefined, 5), null);
});

test('heat levels step at 0.1, 1 and 2.5 percent either way', () => {
  assert.deepEqual([0, 0.05, 0.5, -0.5, 1.5, -2, 3, -7].map(heatLevel), [0, 0, 1, -1, 2, -2, 3, -3]);
  assert.equal(heatLevel(null), null);
  assert.equal(heatLevel(Number.NaN), null);
});

test('top movers rank gainers and losers apart and ignore unknown changes', () => {
  const items = [{ s: 'A', p: 2 }, { s: 'B', p: -3 }, { s: 'C', p: 0.5 }, { s: 'D', p: null }, { s: 'E', p: -0.1 }, { s: 'F', p: 0 }];
  const m = topMovers(items, i => i.p, 2);
  assert.deepEqual(m.gainers.map(g => g.item.s), ['A', 'C']);
  assert.deepEqual(m.losers.map(l => l.item.s), ['B', 'E']);
});

test('the range position is clamped and absent without a range', () => {
  assert.equal(rangePosition(105, 100, 110), 0.5);
  assert.equal(rangePosition(120, 100, 110), 1);
  assert.equal(rangePosition(90, 100, 110), 0);
  assert.equal(rangePosition(100, 100, 100), null);
});

test('signed percentages use a real minus sign and never show a signed zero', () => {
  assert.equal(signedPercent(1.844), '+1.84%');
  assert.equal(signedPercent(-0.92), '−0.92%');
  assert.equal(signedPercent(-0.001), '0.00%');
  assert.equal(signedPercent(null), '—');
});
