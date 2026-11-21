import { strict as assert } from 'node:assert';
import { test } from 'node:test';

import { alertInstrumentKey, checkAlerts, directionFor, distancePercent, isCrossed, MAX_ALERTS, parseAlerts, proximity, type PriceAlert } from './price-alerts.ts';

const alert = (over: Partial<PriceAlert> = {}): PriceAlert =>
  ({ id: 'a', symbol: 'NVDA', provider: 'YAHOO', direction: 'above', level: 120, created_at: 1, ...over });

test('a new alert watches toward its level from the price now', () => {
  assert.equal(directionFor(120, 118), 'above');
  assert.equal(directionFor(110, 118), 'below');
  assert.equal(directionFor(118, 118), null);
  assert.equal(directionFor(0, 118), null);
  assert.equal(directionFor(120, 0), null);
});

test('an alert is crossed when the price reaches its level from its side', () => {
  assert.equal(isCrossed(alert(), 119.99), false);
  assert.equal(isCrossed(alert(), 120), true);
  assert.equal(isCrossed(alert({ direction: 'below', level: 110 }), 110.5), false);
  assert.equal(isCrossed(alert({ direction: 'below', level: 110 }), 109), true);
  assert.equal(isCrossed(alert({ direction: 'below', level: 110 }), 0), false, 'no price is never a cross');
});

test('distance and proximity tell how far the price has to go', () => {
  assert.equal(distancePercent(110, 100), 10);
  assert.equal(distancePercent(90, 100), -10);
  assert.equal(distancePercent(90, 0), null);
  assert.equal(proximity(alert({ level: 105 }), 100), 0.5);
  assert.equal(proximity(alert({ level: 130 }), 100), 0);
  assert.equal(proximity(alert({ level: 100 }), 101), 1);
});

test('stored alerts are validated and kept newest first', () => {
  const list = parseAlerts([
    alert({ id: 'old', created_at: 1 }),
    alert({ id: 'new', created_at: 5, triggered_at: 9, triggered_price: 121 }),
    { ...alert({ id: 'bad' }), direction: 'sideways' },
    { ...alert({ id: 'neg' }), level: -3 },
    'junk', null,
  ]);
  assert.deepEqual(list.map(a => a.id), ['new', 'old']);
  assert.equal(list[0].triggered_price, 121);
  assert.deepEqual(parseAlerts({}), []);
  assert.equal(parseAlerts(Array.from({ length: 80 }, (_, i) => alert({ id: String(i), created_at: i }))).length, MAX_ALERTS);
});

test('checking marks the alerts that fired and leaves the rest', () => {
  const alerts = [alert({ id: 'up' }), alert({ id: 'down', direction: 'below', level: 100 }), alert({ id: 'done', triggered_at: 3, triggered_price: 125 })];
  const r = checkAlerts(alerts, a => (a.id === 'down' ? 101 : 120.5), 42);
  assert.deepEqual(r.fired.map(a => a.id), ['up']);
  assert.equal(r.alerts[0].triggered_at, 42);
  assert.equal(r.alerts[0].triggered_price, 120.5);
  assert.equal(r.alerts[1].triggered_at, undefined);
  assert.equal(r.alerts[2].triggered_at, 3, 'a fired alert is not fired again');
  assert.equal(checkAlerts(alerts, () => 0, 1).fired.length, 0);
});

test('an alert is keyed like the quote board rows', () => {
  assert.equal(alertInstrumentKey({ symbol: ' nvda', provider: 'yahoo ' }), 'YAHOO|NVDA');
});
