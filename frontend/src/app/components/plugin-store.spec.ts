// Runs with Node's built-in test runner (Node 24+ strips TypeScript types natively):
//   node --test src/app/components/plugin-store.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  capabilityLabels, compareVersions, compatibilityBadge, isDomainProvider, matchesFilter, toolLabels, pluginRows, pluginStatus, uninstallPrompt, updateAllHeadline,
  appRangeLabel, upgradeOutcomeLabel, upgradeReportHeadline, type UpgradeEntry,
  type InstalledPlugin, type PluginManifest, type PluginOverview, type StorePlugin,
} from './plugin-store.ts';

function manifest(id: string, name: string, version = '1.0.0'): PluginManifest {
  return {
    protocol: 1, id, name, version, description: `${name} data`, publisher: 'FloweySales',
    provider: { id: id.toUpperCase(), label: name, official_api: true, experimental: false, capabilities: ['quote', 'history'],
      quote_kind: 'ohlc_bar_close', min_poll_seconds: 5, cache_ttl_seconds: 60, notes: '' },
  };
}
const installed = (m: PluginManifest, extra: Partial<InstalledPlugin> = {}): InstalledPlugin => ({
  manifest: m, installed_at: '2026-10-31T00:00:00Z', source: 'store', sha256: 'ab', running: false, env: [], ready: true,
  update_available: false, log: [], ...extra,
});
const store = (m: PluginManifest, extra: Partial<StorePlugin> = {}): StorePlugin => ({
  manifest: m, versions: [m.version], latest_version: m.version, installable: true, update_available: false, ...extra,
});

test('post-upgrade report headline counts every outcome and says plugins are kept', () => {
  const entry = (id: string, outcome: UpgradeEntry['outcome']): UpgradeEntry => ({ id, name: id, version: '1.0.0', outcome, retained: true, disabled: outcome === 'incompatible', reason: '' });
  const r = { at: '2026-11-06T00:00:00Z', plugins: [entry('a', 'verified'), entry('b', 'needs_attention'), entry('c', 'incompatible'), entry('d', 'removed')] };
  assert.equal(upgradeReportHeadline(r),
    'After the upgrade: 1 verified, 1 need attention, 1 incompatible, 0 updated, 1 removed. 3 plugins kept with settings and data.');
  assert.equal(upgradeOutcomeLabel('incompatible'), 'Incompatible, not run');
});

test('declared app version range reads in words', () => {
  assert.equal(appRangeLabel({ min_app_version: '0.5.0', max_app_version: '0.9.0' }), 'FloweySales 0.5.0 to 0.9.0');
  assert.equal(appRangeLabel({ min_app_version: '0.5.0' }), 'FloweySales 0.5.0 or newer');
  assert.equal(appRangeLabel({ max_app_version: '0.9.0' }), 'FloweySales up to 0.9.0');
  assert.equal(appRangeLabel({}), '');
});

test('versions order like semver, pre-releases first', () => {
  const ordered = ['0.9.9', '1.0.0-alpha', '1.0.0-alpha.1', '1.0.0-beta.2', '1.0.0-beta.11', '1.0.0', '1.2.0', '1.10.0'];
  for (let i = 0; i < ordered.length; i++) {
    for (let j = 0; j < ordered.length; j++) {
      assert.equal(compareVersions(ordered[i], ordered[j]), Math.sign(i - j), `${ordered[i]} vs ${ordered[j]}`);
    }
  }
});

test('rows merge installed and store entries, installed first', () => {
  const yahoo = manifest('yahoo', 'Yahoo Finance');
  const overview: PluginOverview = {
    platform: 'linux-amd64', protocol: 1, store_url: 'https://x', dir: '/plugins',
    installed: [installed(yahoo, { update_available: true, latest_version: '1.1.0' })],
    store: { plugins: [store(manifest('binance', 'Binance')), store(manifest('yahoo', 'Yahoo Finance', '1.1.0'), { installed_version: '1.0.0', update_available: true })] },
    trackers: { yahoo: ['NVDA', 'GIB'] },
  };
  const rows = pluginRows(overview);
  assert.deepEqual(rows.map(r => r.id), ['yahoo', 'binance']);
  assert.equal(rows[0].store?.latest_version, '1.1.0');
  assert.deepEqual(rows[0].trackers, ['NVDA', 'GIB']);
  assert.equal(pluginStatus(rows[0]).tone, 'update');
  assert.equal(pluginStatus(rows[1]).label, 'Not installed');
  assert.match(uninstallPrompt(rows[0]), /2 active trackers \(NVDA, GIB\)/);
  assert.doesNotMatch(uninstallPrompt(rows[1]), /tracker/);
});

test('status explains configuration, failures and unavailable builds', () => {
  const m = manifest('finnhub', 'Finnhub');
  assert.equal(pluginStatus({ id: 'finnhub', manifest: m, installed: installed(m, { ready: false }), trackers: [] }).label, 'Needs configuration');
  assert.equal(pluginStatus({ id: 'finnhub', manifest: m, installed: installed(m, { last_error: 'boom' }), trackers: [] }).label, 'Failed to start');
  assert.equal(pluginStatus({ id: 'finnhub', manifest: m, installed: installed(m, { running: true }), trackers: [] }).label, 'Installed · running');
  const row = { id: 'finnhub', manifest: m, store: store(m, { installable: false, reason: 'No build for linux-riscv64.' }), trackers: [] };
  assert.deepEqual(pluginStatus(row), { label: 'No build for linux-riscv64.', tone: 'muted' });
});

test('capabilities and filtering', () => {
  assert.deepEqual(capabilityLabels(['history', 'forum', 'quote']), ['Quotes', 'Price history', 'Community posts']);
  const row = { id: 'avanza', manifest: manifest('avanza', 'Avanza (unofficial)'), trackers: [] };
  assert.equal(matchesFilter(row, 'AVANZA'), true);
  assert.equal(matchesFilter(row, '  '), true);
  assert.equal(matchesFilter(row, 'crypto'), false);
});

test('a tools-only plugin has no provider and null capabilities', () => {
  // The server sends capabilities: null for a plugin without a provider.
  assert.deepEqual(capabilityLabels(null), []);
  const row = {
    id: 'se-tax', manifest: { id: 'se-tax', name: 'Swedish tax', publisher: 'FloweySales', description: 'K4', version: '1.0.0', protocol: 1,
      provider: { id: '', label: '', capabilities: null }, tools: { tab: { label: 'Tax' } } },
    trackers: [],
  } as unknown as Parameters<typeof uninstallPrompt>[0];
  assert.equal(matchesFilter(row, 'tax'), true);
  assert.match(uninstallPrompt(row), /tools tab stops being available/);
});

test('tool labels stand in for capabilities on a tools-only plugin', () => {
  assert.deepEqual(toolLabels({ tools: { tools: [{ id: 'k4', label: 'K4 report' }, { id: 'holdings', label: 'Holdings & cost basis' }] } }), ['K4 report', 'Holdings & cost basis']);
  assert.deepEqual(toolLabels({}), []);
});

test('domain capabilities have labels and a provider type', () => {
  assert.deepEqual(capabilityLabels(['tax_report', 'tax_estimate']), ['Tax report', 'Tax estimate']);
  assert.equal(isDomainProvider({ type: 'tax' }), true);
  assert.equal(isDomainProvider({ type: 'market' }), false);
  assert.equal(isDomainProvider({}), false);
});

test('compatibility is stated in words and never inferred from being installed or running', () => {
  assert.deepEqual(compatibilityBadge(undefined), { label: 'Not verified', tone: 'muted', icon: 'bi-question-circle' });
  const base = { reason: '', plugin_protocol: 1, app_protocol: 1, evidence: '' };
  assert.equal(compatibilityBadge({ ...base, status: 'compatible' }).label, 'Compatible');
  assert.equal(compatibilityBadge({ ...base, status: 'unknown' }).label, 'Not verified');
  assert.equal(compatibilityBadge({ ...base, status: 'incompatible' }).tone, 'warn');
  assert.equal(compatibilityBadge({ ...base, status: 'needs_update' }).label, 'Needs update');

  const m = manifest('yahoo', 'Yahoo Finance');
  const row = (compatibility: InstalledPlugin['compatibility'], extra: Partial<InstalledPlugin> = {}) =>
    ({ id: 'yahoo', manifest: m, installed: installed(m, { running: true, compatibility, ...extra }), trackers: [] });
  const incompatible = pluginStatus(row({ ...base, status: 'incompatible', plugin_protocol: 7 }));
  assert.equal(incompatible.tone, 'warn');
  assert.match(incompatible.label, /incompatible.*Settings and data are kept/);
  const needsUpdate = pluginStatus(row({ ...base, status: 'needs_update', compatible_versions: ['1.1.0', '1.0.0'] }));
  assert.match(needsUpdate.label, /Update to 1\.1\.0/);
  // A running plugin that is incompatible is not reported as healthy, even with an update on offer.
  assert.equal(pluginStatus(row({ ...base, status: 'incompatible' }, { update_available: true, latest_version: '1.1.0' })).tone, 'warn');
  assert.equal(pluginStatus(row({ ...base, status: 'compatible' })).label, 'Installed · running');
});

test('update all reports every plugin and says the ones not updated keep their version', () => {
  const r = (outcome: 'updated' | 'skipped' | 'failed') => ({ id: outcome, name: outcome, from: '1.0.0', to: '1.1.0', outcome, detail: '' });
  const text = updateAllHeadline([r('updated'), r('failed'), r('skipped'), r('updated')]);
  assert.match(text, /2 updated, 1 skipped, 1 failed/);
  assert.match(text, /keep their installed version and settings/);
});
