// Runs with Node's built-in test runner: node --test src/app/core/provenance.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { provenanceBadge, provenanceLines, safeSourceUrl } from './provenance.ts';

const fmt = (iso: string) => iso.slice(0, 16).replace('T', ' ') + ' UTC';

test('known observed facts list their origin and UTC times', () => {
  const f = { kind: 'observed', status: 'known', provider: 'finnhub', url: 'https://a.com/x', published_at: '2026-11-03T14:32:00Z', retrieved_at: '2026-11-03T15:00:00Z' } as const;
  assert.equal(provenanceBadge(f), 'Source');
  const lines = provenanceLines(f, fmt);
  assert.deepEqual(lines.map(l => l.label), ['What this is', 'Provider', 'Published', 'Retrieved']);
  assert.equal(lines[2].value, '2026-11-03 14:32 UTC');
  assert.equal(safeSourceUrl(f), 'https://a.com/x');
});

test('partial facts say so and invent nothing', () => {
  const f = { kind: 'observed', status: 'partial', provider: 'Reuters' } as const;
  assert.equal(provenanceBadge(f), 'Source (partial)');
  assert.deepEqual(provenanceLines(f, fmt).map(l => l.label), ['What this is', 'Provider']);
});

test('unknown is stated plainly, also when there is no fact at all', () => {
  assert.equal(provenanceBadge({ kind: 'observed', status: 'unknown' }), 'Source unknown');
  assert.equal(provenanceBadge(undefined), 'Source unknown');
  assert.deepEqual(provenanceLines(undefined, fmt), [{ label: 'Source', value: 'No source was recorded for this value.' }]);
  assert.equal(provenanceLines({ kind: 'observed', status: 'unknown', note: 'Imported by hand' }, fmt)[0].value, 'Imported by hand');
});

test('calculated and AI-written values are not presented as sources', () => {
  const calc = { kind: 'calculated', status: 'known', method: { id: 'sma', version: '1', formula: 'mean(close, 20)' }, inputs: ['quote:AAPL'] } as const;
  assert.equal(provenanceBadge(calc), 'Calculated');
  assert.deepEqual(provenanceLines(calc, fmt).map(l => l.label), ['What this is', 'How calculated', 'Built from']);
  assert.equal(provenanceBadge({ kind: 'ai_generated', status: 'known', note: 'x' }), 'Written by an AI model');
});

test('only http(s) links are offered', () => {
  assert.equal(safeSourceUrl({ kind: 'observed', status: 'known', url: 'javascript:alert(1)' }), '');
  assert.equal(safeSourceUrl(undefined), '');
});

test('source links refuse credentials, control characters and unsafe protocols', () => {
  for (const url of ['javascript:alert(1)', 'data:text/html,x', 'https://user:secret@example.org/', 'https://', 'https://example.org/\\nnext']) {
    assert.equal(safeSourceUrl({ url } as Provenance), '', url);
  }
  assert.equal(safeSourceUrl({ url: 'https://example.org/report' } as Provenance), 'https://example.org/report');
});
