// Runs with Node's built-in test runner:
//   node --test src/app/components/saved-transactions.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { DEFAULT_LIMITS, uploadProblem, usedPercent, type SavedTransactionFile } from './saved-transactions.ts';

const file = (name: string, size: number): SavedTransactionFile => ({ id: 1, name, size_bytes: size, created_at: '2026-11-03T00:00:00Z' });

test('upload problems name the limit that is hit', () => {
  assert.equal(uploadProblem('a.csv', 1000, []), '');
  assert.match(uploadProblem('a.csv', 0, []), /empty/);
  assert.match(uploadProblem('a.csv', DEFAULT_LIMITS.max_file_bytes + 1, []), /larger than 2 MB/);
  const full = Array.from({ length: 10 }, (_, i) => file(`f${i}.csv`, 10));
  assert.match(uploadProblem('new.csv', 10, full), /at most 10 files/);
  assert.equal(uploadProblem('f3.csv', 10, full), '', 'a file with a saved name replaces it');
  assert.match(uploadProblem('b.csv', 2 << 20, [file('a.csv', 2 << 20)]), /total 3 MB/);
});

test('used percent is capped', () => {
  assert.equal(usedPercent([]), 0);
  assert.equal(usedPercent([file('a', 3 << 19)]), 50);
  assert.equal(usedPercent([file('a', 9 << 20)]), 100);
});
