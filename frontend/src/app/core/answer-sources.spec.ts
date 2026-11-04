// Runs with Node's built-in test runner:
//   node --test src/app/core/answer-sources.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { EVIDENCE_MODES, auditSummary, delegationProgress, parseEvidenceMode, findingLabel, patchNodeEvidence, policyFromSettings, removedSummary, safeUrl, sourceOrigin, settingsFromPolicy, settingsProblem, settingsSummary, splitSources } from './answer-sources.ts';

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
  const base = { mode: 'evidence_only' as const, block: false, maxAgeDays: 0, filings: true, webPages: true };
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
  assert.deepEqual(s, { mode: 'primary_only', block: true, maxAgeDays: 365, filings: true, webPages: true });
  assert.deepEqual(policyFromSettings(s), policy);
  assert.deepEqual(settingsFromPolicy(null), { mode: 'standard', block: false, maxAgeDays: 0, filings: true, webPages: true });
  // Block means nothing in standard mode; unknown modes and bad ages fall back.
  assert.equal(settingsFromPolicy({ mode: 'standard', on_failure: 'block' }).block, false);
  assert.equal(settingsFromPolicy({ mode: 'strict' as never, max_age_days: -4 }).maxAgeDays, 0);
  assert.equal(settingsFromPolicy({ mode: 'evidence_only', deny_classes: ['web_page'] }).webPages, false);
});

test('settings that cannot run are described, and a summary says what they do', () => {
  const ok = { mode: 'evidence_only' as const, block: true, maxAgeDays: 30, filings: true, webPages: true };
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
