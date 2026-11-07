// Runs with Node's built-in test runner:
//   node --test src/app/components/document-evidence.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { evidenceSummary, flagText, parseDocumentEvidence } from './document-evidence.ts';

const result = JSON.stringify({
  url: 'https://ir.example.com/viewer', document_url: 'https://ir.example.com/q3.pdf', resolved_from: 'https://ir.example.com/viewer',
  text: '[page 1] ...',
  document: {
    type: 'pdf', pages_total: 3, pages_examined: 3, vision_status: 'used', limitations: ['page 3 has neither text nor picture'],
    pages: [
      { page: 1, class: 'text', method: 'text', text_chars: 900 },
      { page: 2, class: 'scanned', method: 'vision', vision: 'ok' },
      { page: 3, class: 'scanned', method: 'none', vision: 'failed', note: 'the budget is used up' },
    ],
    evidence: { retrieved: true, text_parsed: true, vision_interpreted: true, verified_values: 1, unverified_values: 1 },
  },
  visual_evidence: [{ page: 2, observations: [
    { page: 2, label: 'Revenue', original_value: '4 300', unit: 'MSEK', period: 'Q3 2024', verified: true, flags: [] },
    { page: 2, label: 'Profit', original_value: '(12,5)', currency: 'SEK', verified: false, flags: ['period_missing', 'total_consistent'] },
  ] }],
});

test('page methods, vision state and citations are kept per page', () => {
  const d = parseDocumentEvidence(result)!;
  assert.equal(d.url, 'https://ir.example.com/q3.pdf');
  assert.equal(d.resolvedFrom, 'https://ir.example.com/viewer');
  assert.deepEqual(d.pages.map(p => [p.page, p.label, p.vision]), [[1, 'Text layer', ''], [2, 'Vision model', 'ok'], [3, 'Not read', 'failed']]);
  assert.equal(d.pages[2].note, 'the budget is used up');
});

test('observations carry page, original value and plain-language flags', () => {
  const d = parseDocumentEvidence(result)!;
  assert.equal(d.observations.length, 2);
  assert.equal(d.observations[0].original, '4 300');
  assert.equal(d.observations[0].verified, true);
  assert.deepEqual(d.observations[1].flagText, ['No period is stated']);
  assert.equal(d.observations[1].unit, 'SEK');
});

test('the summary separates retrieval, text, vision and verification', () => {
  const d = parseDocumentEvidence(result)!;
  assert.match(evidenceSummary(d), /PDF retrieved · text parsed · pages interpreted by the vision model · 1 of 2 vision-read values passed the automatic checks \(none confirmed independently\)/);
});

test('anything that is not a PDF read gives no panel', () => {
  assert.equal(parseDocumentEvidence(undefined), null);
  assert.equal(parseDocumentEvidence('not json'), null);
  assert.equal(parseDocumentEvidence(JSON.stringify({ url: 'x', text: 'y' })), null);
  assert.equal(parseDocumentEvidence('{"truncated":true,"preview_text":"{\\"doc'), null);
});

test('unknown flags are shown readably', () => {
  assert.equal(flagText('something_new'), 'something new');
});
