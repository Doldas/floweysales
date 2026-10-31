// Runs with Node's built-in test runner (Node 24+ strips TypeScript types natively):
//   node --test src/app/components/plugin-store.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  capabilityLabels, compareVersions, matchesFilter, pluginRows, pluginStatus, uninstallPrompt,
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
