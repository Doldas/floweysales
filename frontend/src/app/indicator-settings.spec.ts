import assert from 'node:assert/strict';
import test from 'node:test';

import { DEFAULT_SETTINGS, parseSettings, settingsProblem, studyLabel } from './indicator-settings.ts';

test('the default periods are usable and the labels show them', () => {
  assert.equal(settingsProblem(DEFAULT_SETTINGS), '');
  assert.equal(studyLabel('ema21', DEFAULT_SETTINGS), 'EMA 21');
  assert.equal(studyLabel('macd', DEFAULT_SETTINGS), 'MACD 12,26,9');
  assert.equal(studyLabel('stoch', DEFAULT_SETTINGS), 'Stochastic 14,3,3');
  assert.equal(studyLabel('bb', { ...DEFAULT_SETTINGS, bbMult: 2.5 }), 'Bollinger 20,2.5');
  assert.equal(studyLabel('ema9', { ...DEFAULT_SETTINGS, ema9: 12 }), 'EMA 12');
});

test('periods out of range, fractional or contradicting each other are refused with a reason', () => {
  assert.match(settingsProblem({ ...DEFAULT_SETTINGS, rsiPeriod: 1 }), /RSI period must be between 2 and 100/);
  assert.match(settingsProblem({ ...DEFAULT_SETTINGS, ema50: 20.5 }), /whole number/);
  assert.match(settingsProblem({ ...DEFAULT_SETTINGS, macdFast: 30 }), /MACD fast must be shorter/);
  assert.match(settingsProblem({ ...DEFAULT_SETTINGS, kijun: 9 }), /Ichimoku needs/);
  assert.match(settingsProblem({ ...DEFAULT_SETTINGS, bbMult: NaN }), /between/);
  assert.equal(settingsProblem({ ...DEFAULT_SETTINGS, bbMult: 2.5 }), '');
});

test('stored settings are used when valid and fall back to the defaults when not', () => {
  assert.deepEqual(parseSettings({ ...DEFAULT_SETTINGS, rsiPeriod: 21 }), { ...DEFAULT_SETTINGS, rsiPeriod: 21 });
  assert.deepEqual(parseSettings({ rsiPeriod: 21 }), { ...DEFAULT_SETTINGS, rsiPeriod: 21 });
  assert.deepEqual(parseSettings({ rsiPeriod: 0 }), DEFAULT_SETTINGS);
  assert.deepEqual(parseSettings({ macdFast: 50 }), DEFAULT_SETTINGS);
  assert.deepEqual(parseSettings('x'), DEFAULT_SETTINGS);
  assert.deepEqual(parseSettings(null), DEFAULT_SETTINGS);
  assert.deepEqual(parseSettings({ rsiPeriod: '21' }), DEFAULT_SETTINGS);
});
