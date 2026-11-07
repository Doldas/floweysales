// Runs with Node's built-in test runner:
//   node --test src/app/core/answer-sources.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { EVIDENCE_MODES, auditSummary, claimFacts, claimsSummary, sortClaims, spanOrigin, spanParts, verdictLabel, whyLabel, type AnswerClaim, delegationProgress, parseEvidenceMode, findingLabel, patchNodeEvidence, policyFromSettings, removedSummary, safeUrl, sourceOrigin, settingsFromPolicy, settingsProblem, settingsSummary, splitSources, parseRankingProfile, sourceCard, sourceDates, sortSources, evidenceStateLabel, type AnswerSource, evidenceForOutputMode, jsonCitationLine, type EvidencePolicy } from './answer-sources.ts';

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
  assert.equal(findingLabel('quote_mismatch'), 'Quote not in the cited text');
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

test('settings become the request object, leaving defaults out', () => {
  const base = { mode: 'evidence_only' as const, block: false, maxAgeDays: 0, filings: true, webPages: true, rankingProfile: 'facts_first' as const, searchWindowDays: 0 };
  assert.deepEqual(policyFromSettings(base), { mode: 'evidence_only' });
  assert.deepEqual(policyFromSettings({ ...base, block: true, maxAgeDays: 90 }), { mode: 'evidence_only', on_failure: 'block', max_age_days: 90 });
  // Primary sources only counts filings alone unless web pages are switched on.
  assert.deepEqual(policyFromSettings({ ...base, mode: 'primary_only', webPages: false }), { mode: 'primary_only' });
  assert.deepEqual(policyFromSettings({ ...base, mode: 'primary_only' }), { mode: 'primary_only', allow_classes: ['primary_filing', 'web_page'] });
  assert.deepEqual(policyFromSettings({ ...base, filings: false }), { mode: 'evidence_only', allow_classes: ['web_page'] });
  // Standard mode carries no limits: they only apply when the answer is held.
  assert.deepEqual(policyFromSettings({ ...base, mode: 'standard', block: true, maxAgeDays: 30 }), { mode: 'standard' });
});

test('a saved policy fills the form and survives the round trip', () => {
  const policy = { mode: 'primary_only' as const, on_failure: 'block' as const, max_age_days: 365, allow_classes: ['primary_filing' as const, 'web_page' as const] };
  const s = settingsFromPolicy(policy);
  assert.deepEqual(s, { mode: 'primary_only', block: true, maxAgeDays: 365, filings: true, webPages: true, rankingProfile: 'facts_first', searchWindowDays: 0 });
  assert.deepEqual(policyFromSettings(s), policy);
  assert.deepEqual(settingsFromPolicy(null), { mode: 'standard', block: false, maxAgeDays: 0, filings: true, webPages: true, rankingProfile: 'facts_first', searchWindowDays: 0 });
  // Block means nothing in standard mode; unknown modes and bad ages fall back.
  assert.equal(settingsFromPolicy({ mode: 'standard', on_failure: 'block' }).block, false);
  assert.equal(settingsFromPolicy({ mode: 'strict' as never, max_age_days: -4 }).maxAgeDays, 0);
  assert.equal(settingsFromPolicy({ mode: 'evidence_only', deny_classes: ['web_page'] }).webPages, false);
});

test('settings that cannot run are described, and a summary says what they do', () => {
  const ok = { mode: 'evidence_only' as const, block: true, maxAgeDays: 30, filings: true, webPages: true, rankingProfile: 'facts_first' as const, searchWindowDays: 0 };
  assert.equal(settingsProblem(ok), '');
  assert.match(settingsProblem({ ...ok, filings: false, webPages: false }), /at least one source type/);
  assert.match(settingsProblem({ ...ok, maxAgeDays: 99999 }), /0 to 3650/);
  assert.match(settingsProblem({ ...ok, maxAgeDays: 1.5 }), /whole number/);
  assert.equal(settingsProblem({ ...ok, mode: 'standard', maxAgeDays: -1 }), '');
  assert.match(settingsSummary(ok), /removed/);
  assert.match(settingsSummary(ok), /last 30 days count; a source with no date does not/);
  assert.equal(settingsSummary({ ...ok, mode: 'standard' }), '');
  assert.match(settingsSummary({ ...ok, block: false, mode: 'primary_only', webPages: true }), /flagged.*filings and web pages/);
});

test('a flow node keeps the settings the form does not show', () => {
  assert.equal(patchNodeEvidence(undefined, {}), undefined);
  assert.equal(patchNodeEvidence(undefined, { mode: 'standard' }), undefined);
  assert.deepEqual(patchNodeEvidence(undefined, { mode: 'evidence_only' }), { mode: 'evidence_only' });
  const set = { mode: 'evidence_only' as const, on_failure: 'block' as const, max_age_days: 30, deny_classes: ['web_page' as const] };
  assert.deepEqual(patchNodeEvidence(set, { maxAgeDays: 90 }), { ...set, max_age_days: 90 });
  assert.deepEqual(patchNodeEvidence(set, { block: false }), { mode: 'evidence_only', max_age_days: 30, deny_classes: ['web_page'] });
  assert.deepEqual(patchNodeEvidence(set, { maxAgeDays: 0 }), { mode: 'evidence_only', on_failure: 'block', deny_classes: ['web_page'] });
  assert.equal(patchNodeEvidence(set, { maxAgeDays: 99999 })?.max_age_days, undefined);
  // A new mode drops the allow list of the old one; turning the mode off drops everything.
  assert.equal(patchNodeEvidence({ ...set, allow_classes: ['web_page'] }, { mode: 'primary_only' })?.allow_classes, undefined);
  assert.equal(patchNodeEvidence(set, { mode: '' }), undefined);
});

test('removed statements are reported plainly', () => {
  assert.equal(removedSummary([]), '');
  assert.match(removedSummary([{ kind: 'stale', sentence: 'x', detail: 'y' }]), /^1 statement was removed/);
  assert.match(removedSummary([{ kind: 'a', sentence: '', detail: '' }, { kind: 'b', sentence: '', detail: '' }]), /^2 statements were removed/);
  assert.equal(findingLabel('stale'), 'Source too old or undated');
  assert.equal(findingLabel('unreachable'), 'Link no longer resolves');
});

test('delegationProgress names the member and its stance', () => {
  assert.equal(delegationProgress({ type: 'member_start', name: 'Walter Ashgrove' }), 'Walter Ashgrove is working');
  assert.equal(delegationProgress({ type: 'member_done', name: 'Walter Ashgrove', stance: 'does_not_fit' }), 'Walter Ashgrove answered (does not fit)');
  assert.equal(delegationProgress({ type: 'member_done', agent: 'quant', error: 'timeout' }), 'quant failed: timeout');
  assert.equal(delegationProgress({ type: 'delta' }), '');
});

test('a source a delegated team member retrieved names that member', () => {
  assert.equal(sourceOrigin({ member: 'Owner' }), 'retrieved by team member Owner');
  assert.equal(sourceOrigin({}), '');
});

test('a listed source names the search provider and says it was only a snippet', () => {
  assert.equal(sourceOrigin({ search_provider: 'searxng', access: 'listed' }), 'found by SearXNG; only a snippet');
  assert.equal(sourceOrigin({ search_provider: 'brave_search', member: 'Owner' }), 'retrieved by team member Owner · found by Brave Search; only a snippet');
  assert.match(sourceOrigin({ search_provider: 'google_grounding', result_type: 'grounded_source' }), /cited by Gemini.*not opened/);
  assert.equal(sourceOrigin({ search_provider: 'searxng', access: 'read' }), '', 'an opened page is not a snippet');
});

const claim = (over: Partial<AnswerClaim>): AnswerClaim => ({
  id: 'C1', sentence: 'Acme revenue was $4.5 billion in fiscal 2024 [S1].', kind: 'fact', verdict: 'supported', code: 'match',
  explanation: 'The cited text states the figure.', limitations: 'Not proof the source is right.', verifier_version: 'claims-v1', verified_at: '2026-11-06T10:00:00Z', ...over,
});

test('verdict labels never present uncertainty as proof', () => {
  assert.equal(verdictLabel('supported'), 'Supported by the cited text');
  assert.match(verdictLabel('insufficient'), /Not proven/);
  assert.equal(verdictLabel('not_checked'), 'Not checked');
  assert.equal(verdictLabel('contradicted'), 'Contradicted by the cited text');
  assert.equal(whyLabel('supported'), 'Why is this claim supported?');
  assert.equal(whyLabel('contradicted'), 'Why is this claim not supported?');
  assert.equal(findingLabel('claim_contradicted'), 'Claim contradicted by the cited text');
});

test('claims are listed with the worst first and keep answer order within a verdict', () => {
  const order = sortClaims([claim({ id: 'C1' }), claim({ id: 'C2', verdict: 'not_checked' }), claim({ id: 'C3', verdict: 'contradicted' }), claim({ id: 'C4', verdict: 'insufficient' }), claim({ id: 'C5' })]);
  assert.deepEqual(order.map(c => c.id), ['C3', 'C4', 'C2', 'C1', 'C5']);
});

test('the claims summary counts verdicts and states the cost of the check', () => {
  const text = claimsSummary([claim({}), claim({ verdict: 'contradicted' })], { claims: 2, supported: 1, contradicted: 1, insufficient: 0, not_checked: 0, skipped: 3, elapsed_ms: 4, model_calls: 0, verifier_version: 'claims-v1' });
  assert.match(text, /2 claims: 1 supported, 1 contradicted, 0 not proven, 0 not checked/);
  assert.match(text, /4 ms with no model call/);
  assert.match(text, /3 more sentences were not checked/);
  assert.equal(claimsSummary([]), '');
});

test('claim facts list only what the verifier found', () => {
  const rows = claimFacts(claim({ entity: 'Acme', metric: 'revenue', period: 'FY2024', causal: true }));
  assert.deepEqual(rows.map(r => r.label), ['Entity', 'Metric', 'Period', 'Kind']);
  assert.equal(rows[3].value, 'fact (states a cause)');
});

test('a span keeps the matched token apart and names its source with UTC times', () => {
  const span = { source: 'S1', start: 10, end: 15, before: 'Total revenue', match: '4,500', after: '3,900', title: 'Acme 10-K', retrieved_at: '2026-11-05T12:00:00Z', published_at: '2025-02-01' };
  assert.deepEqual(spanParts(span), { before: 'Total revenue', match: '4,500', after: '3,900' });
  assert.equal(spanOrigin(span), '[S1] · Acme 10-K · published 2025-02-01 · retrieved 2026-11-05 12:00 UTC');
  assert.equal(spanOrigin({ source: 'S2', start: 0, end: 1, match: 'x' }), '[S2]');
});

const src = (over: Partial<AnswerSource>): AnswerSource => ({ id: 'S1', tool: 'search_web', class: 'web_page', access: 'listed', ...over });

test('ranking profile and search window travel with the policy, even in standard mode', () => {
  const base = { mode: 'standard' as const, block: false, maxAgeDays: 0, filings: true, webPages: true, rankingProfile: 'balanced' as const, searchWindowDays: 14 };
  assert.deepEqual(policyFromSettings(base), { mode: 'standard', ranking_profile: 'balanced', search_window_days: 14 });
  assert.deepEqual(policyFromSettings({ ...base, rankingProfile: 'facts_first', searchWindowDays: 0 }), { mode: 'standard' });
  const back = settingsFromPolicy({ mode: 'standard', ranking_profile: 'sentiment_first', search_window_days: 30 });
  assert.equal(back.rankingProfile, 'sentiment_first');
  assert.equal(back.searchWindowDays, 30);
  assert.equal(parseRankingProfile('bogus'), 'facts_first');
  assert.match(settingsProblem({ ...base, searchWindowDays: 99999 }), /0 to 3650/);
});

test('a source card labels type, role, state and keeps file dates apart', () => {
  const card = sourceCard(src({ trust: {
    source_type: 'company_ir', evidence_role: 'self_reported', evidence_state: 'pdf_text', published_at: 'October 30, 2024', date_source: 'pdf_cover', date_confidence: 'medium',
    file_created_at: '2026-10-01T00:00:00Z', http_last_modified: 'Tue, 03 Nov 2026 10:00:00 GMT', retrieved_at: '2026-11-07T12:01:00Z', period: 'Q3 2024', why_ranked: ['covers the requested period Q3 2024'],
  } }));
  assert.equal(card.type, 'Company investor relations');
  assert.match(card.role, /Self-reported/);
  assert.equal(card.state, 'PDF text read');
  const labels = card.dates.map(d => d.label);
  assert.deepEqual(labels, ['Published', 'Report period', 'File created', 'HTTP Last-Modified', 'Retrieved']);
  assert.match(card.dates[0].value, /report cover, medium confidence/);
  assert.match(card.dates[4].value, /2026-11-07 12:01 UTC/);
  assert.equal(card.why.length, 1);
});

test('unknown dates are shown as unknown and file dates are not called the publication date', () => {
  const card = sourceCard(src({ trust: { source_type: 'unknown', evidence_role: 'unknown', file_created_at: '2026-10-01T00:00:00Z', date_source: 'unknown', date_confidence: 'none' } }));
  assert.deepEqual(card.dates[0], { label: 'Published', value: 'unknown' });
  assert.ok(card.notes.some(n => /not when the report was published/.test(n)));
  assert.deepEqual(sourceDates(undefined), [{ label: 'Published', value: 'unknown' }]);
});

test('access denied, snippet only and vision-derived are different states', () => {
  assert.equal(evidenceStateLabel('access_denied'), 'Access denied, not read');
  assert.equal(evidenceStateLabel('snippet_only'), 'Search snippet only');
  assert.equal(evidenceStateLabel('vision_derived'), 'Includes vision-derived content');
  assert.equal(sourceCard(src({ trust: { source_type: 'official_filing', evidence_role: 'primary', evidence_state: 'access_denied', access_note: 'The site refused automated access.' } })).denied, true);
  assert.equal(evidenceStateLabel(undefined, 'read'), 'Page read');
});

test('sources sort by rank or by date with undated last', () => {
  const list = [
    src({ id: 'S1', trust: { source_type: 'u', evidence_role: 'u', rank: 2, published_at: '2026-01-01' } }),
    src({ id: 'S2', trust: { source_type: 'u', evidence_role: 'u', rank: 1 } }),
    src({ id: 'S3', trust: { source_type: 'u', evidence_role: 'u', rank: 3, published_at: '2026-06-01' } }),
  ];
  assert.deepEqual(sortSources(list, 'relevance').map(s => s.id), ['S2', 'S1', 'S3']);
  assert.deepEqual(sortSources(list, 'date').map(s => s.id), ['S3', 'S1', 'S2']);
});

test('the sentiment-as-fact finding has a label', () => {
  assert.equal(findingLabel('sentiment_as_fact'), 'Financial figure rests on discussion or opinion');
});

test('switching a node to JSON output drops the removal option and nothing else', () => {
  const policy = { mode: 'evidence_only', on_failure: 'block', max_age_days: 30 } as EvidencePolicy;
  assert.deepEqual(evidenceForOutputMode(policy, 'json'), { mode: 'evidence_only', max_age_days: 30 });
  assert.equal(evidenceForOutputMode(policy, 'markdown'), policy);
  assert.equal(evidenceForOutputMode(undefined, 'json'), undefined);
});

test('a JSON citation names the path and the sources it cites', () => {
  assert.equal(jsonCitationLine({ path: '/summary', sources: ['S1', 'S2'], text: 'Revenue grew' }), '/summary cites S1, S2: Revenue grew');
  assert.equal(jsonCitationLine({ path: '', sources: ['S3'] }), '/ cites S3');
});
