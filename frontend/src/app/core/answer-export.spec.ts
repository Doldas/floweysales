// Runs with Node's built-in test runner:
//   node --test src/app/core/answer-export.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { ANSWER_EXPORT_SCHEMA, answerToJson, answerToMarkdown, buildAnswerExport, policyLabel, redactUrl } from './answer-export.ts';

const now = new Date('2026-11-06T09:30:00.250Z');
const sources = [
  { id: 'S1', tool: 'get_sec_section', class: 'primary_filing', access: 'read', title: '10-K Item 7', url: 'https://sec.example/a?apikey=abc123&x=1', document_id: '0000000001-26-000004', published_at: '2026-02-01', retrieved_at: '2026-11-06T09:00:00Z', excerpt: 'Revenue was 48.2 billion dollars.' },
  { id: 'S2', tool: 'search_web', class: 'web_page', access: 'listed', title: 'News', url: 'https://user:pw@news.example/p#frag', query: 'acme revenue' },
];

test('urls lose credentials, fragments and secret-looking parameters', () => {
  assert.equal(redactUrl('https://sec.example/a?apikey=abc123&x=1'), 'https://sec.example/a?apikey=REDACTED&x=1');
  assert.equal(redactUrl('https://user:pw@news.example/p#frag'), 'https://news.example/p');
  assert.equal(redactUrl('https://h.example/?access_token=t&Signature=s&page=2'), 'https://h.example/?access_token=REDACTED&Signature=REDACTED&page=2');
  assert.equal(redactUrl('javascript:alert(1)'), '');
  assert.equal(redactUrl('not a url'), '');
  assert.equal(redactUrl(undefined), '');
});

test('the policy label names every limit and never softens a strict mode', () => {
  assert.match(policyLabel(undefined), /^Standard/);
  assert.equal(policyLabel({ mode: 'primary_only' }), 'Primary sources only; unsupported statements flagged; counted types: primary_filing');
  assert.equal(
    policyLabel({ mode: 'evidence_only', on_failure: 'block', max_age_days: 90, deny_classes: ['web_page'] }),
    'Evidence only; unsupported statements removed; sources from the last 90 days only; excluded types: web_page');
});

test('the JSON export carries the schema, policy, sources with lineage and the check result', () => {
  const exp = buildAnswerExport({
    answer: 'Revenue was 48.2 billion dollars [S1].', question: 'What was revenue?',
    policy: { mode: 'primary_only' }, sources, findings: [{ kind: 'stale', sentence: 's', source: 'S2', detail: 'old' }], now,
  });
  assert.equal(exp.schema, ANSWER_EXPORT_SCHEMA);
  assert.equal(exp.exported_at, '2026-11-06T09:30:00.250Z');
  assert.equal(exp.check.findings, 1);
  assert.match(exp.check.statement, /1 unresolved problem\./);
  assert.equal(exp.sources[0].url, 'https://sec.example/a?apikey=REDACTED&x=1');
  assert.equal(exp.sources[1].url, 'https://news.example/p');
  assert.equal(exp.sources[1].query, 'acme revenue');
  assert.deepEqual(JSON.parse(answerToJson(exp)), exp);
  assert.ok(!answerToJson(exp).includes('abc123') && !answerToJson(exp).includes('pw@'));
});

test('long excerpts are cut and a clean check never claims the answer is true', () => {
  const exp = buildAnswerExport({ answer: 'x', sources: [{ ...sources[0], excerpt: 'a'.repeat(5000) }], now });
  assert.ok((exp.sources[0].excerpt ?? '').length <= 601);
  assert.match(exp.check.statement, /does not confirm the claims are true/);
});

test('the Markdown export leads with the policy and keeps provenance in UTC', () => {
  const md = answerToMarkdown(buildAnswerExport({
    answer: 'Revenue was 48.2 billion dollars [S1].', question: 'What was revenue?\nIn FY2025?',
    policy: { mode: 'primary_only', on_failure: 'block' }, sources,
    removed: [{ kind: 'snippet_only', sentence: 'Margin was 31%.', detail: 'Not opened.' }], now,
  }));
  assert.ok(md.indexOf('Evidence policy') < md.indexOf('## Answer'));
  assert.match(md, /- \*\*Exported:\*\* 2026-11-06 09:30:00 UTC/);
  assert.match(md, /not authorization to trade/);
  assert.match(md, /> What was revenue\?\n> In FY2025\?/);
  assert.match(md, /## Removed statements\n\n- snippet_only: Not opened\. Removed: “Margin was 31%\.”/);
  assert.match(md, /### \[S1\] 10-K Item 7/);
  assert.match(md, /- Document: 0000000001-26-000004/);
  assert.match(md, /- Retrieved \(UTC\): 2026-11-06T09:00:00Z/);
  assert.match(md, /- Search input: acme revenue/);
  assert.match(md, /search result only \(title and snippet\)/);
  assert.ok(!md.includes('abc123'));
});

test('an answer with no sources says so', () => {
  assert.match(answerToMarkdown(buildAnswerExport({ answer: '', now })), /_No answer text\._[\s\S]*_The agent retrieved no sources\._/);
});
