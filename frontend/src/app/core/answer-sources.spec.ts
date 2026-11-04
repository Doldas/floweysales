// Runs with Node's built-in test runner:
//   node --test src/app/core/answer-sources.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { EVIDENCE_MODES, auditSummary, parseEvidenceMode, findingLabel, safeUrl, splitSources } from './answer-sources.ts';

test('sources split by whether the agent opened them', () => {
  const { read, listed } = splitSources([
    { id: 'S1', tool: 'search_web', class: 'web_page', access: 'listed' },
    { id: 'S2', tool: 'get_sec_section', class: 'primary_filing', access: 'read' },
  ]);
  assert.deepEqual(read.map(s => s.id), ['S2']);
  assert.deepEqual(listed.map(s => s.id), ['S1']);
});

test('a clean audit never claims the answer is true', () => {
  const text = auditSummary([]);
  assert.match(text, /does not confirm/);
  assert.match(auditSummary([{ kind: 'fabricated', sentence: 'x', detail: 'y' }]), /1 problem\./);
  assert.match(auditSummary([{ kind: 'a', sentence: '', detail: '' }, { kind: 'b', sentence: '', detail: '' }]), /2 problems/);
});

test('finding labels and links', () => {
  assert.equal(findingLabel('snippet_only'), 'Search result not opened');
  assert.equal(findingLabel('new_kind'), 'new kind');
  assert.equal(safeUrl('https://sec.gov/x'), 'https://sec.gov/x');
  assert.equal(safeUrl('javascript:alert(1)'), '');
  assert.equal(safeUrl(undefined), '');
});

test('an unknown stored evidence mode falls back to standard', () => {
  assert.equal(parseEvidenceMode('primary_only'), 'primary_only');
  assert.equal(parseEvidenceMode('strict'), 'standard');
  assert.equal(parseEvidenceMode(null), 'standard');
  assert.deepEqual(EVIDENCE_MODES.map(m => m.id), ['standard', 'evidence_only', 'primary_only']);
});
