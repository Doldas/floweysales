import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { checkCombination, compareVersions, validateMatrix, readProtocolConst } from './check-sdk-compat.mjs';

const matrix = JSON.parse(readFileSync(new URL('../pluginsdk/compat.json', import.meta.url), 'utf8'));
const protocolSrc = readFileSync(new URL('../pluginsdk/protocol.go', import.meta.url), 'utf8');

test('committed matrix matches protocol.go', () => {
  assert.deepEqual(validateMatrix(matrix, readProtocolConst(protocolSrc)), []);
});

test('version ordering treats rc before stable', () => {
  assert.ok(compareVersions('v0.2.0-rc.1', 'v0.2.0') < 0);
  assert.ok(compareVersions('v0.10.0', 'v0.9.9') > 0);
  assert.throws(() => compareVersions('v1.0', 'v1.0.0'));
});

test('supported and rejected combinations', () => {
  assert.equal(checkCombination(matrix, 'v0.1.0', 1).ok, true);
  assert.equal(checkCombination(matrix, 'v1.4.2', 1).ok, true);
  assert.equal(checkCombination(matrix, 'v0.0.9', 1).ok, false);
  assert.equal(checkCombination(matrix, 'v2.0.0', 1).ok, false);
  assert.equal(checkCombination(matrix, 'v0.1.0', 2).ok, false);
});

test('validation catches drift and bad rows', () => {
  assert.ok(validateMatrix(matrix, 2).some((e) => e.includes('missing from the matrix')));
  const bad = { ...matrix, protocols: [{ protocol: 1, status: 'removed', sdkMin: '1.0.0', sdkMaxExclusive: '0.5.0' }] };
  const errs = validateMatrix(bad, 1);
  assert.ok(errs.some((e) => e.includes('sdkMin must be below')));
  assert.ok(errs.some((e) => e.includes('must be "supported"')));
});
