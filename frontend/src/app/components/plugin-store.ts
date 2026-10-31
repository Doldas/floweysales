/**
 * Pure helpers for Settings → Plugins.
 * Kept free of Angular dependencies so they can be tested without a DOM or TestBed.
 */

export interface PluginEnvVar { name: string; secret?: boolean; required?: boolean; description: string; configured?: boolean }
export interface PluginProvider {
  id: string; label: string; official_api: boolean; experimental: boolean; capabilities: string[];
  quote_kind: string; min_poll_seconds: number; cache_ttl_seconds: number; notes: string;
}
export interface PluginManifest {
  protocol: number; id: string; name: string; version: string; description: string; publisher: string;
  homepage?: string; license?: string; terms?: string; provider: PluginProvider; env?: PluginEnvVar[];
}
export interface InstalledPlugin {
  manifest: PluginManifest; installed_at: string; source: string; sha256: string; running: boolean;
  last_error?: string; env: PluginEnvVar[]; ready: boolean; latest_version?: string; update_available: boolean; log: string[];
}
export interface StorePlugin {
  manifest: PluginManifest; released_at?: string; notes?: string; versions: string[]; latest_version?: string;
  installable: boolean; reason?: string; installed_version?: string; update_available: boolean;
}
export interface PluginOverview {
  platform: string; protocol: number; store_url: string; dir: string;
  installed: InstalledPlugin[];
  store: { checked_at?: string; error?: string; warnings?: string[]; plugins: StorePlugin[] };
  trackers: Record<string, string[]>;
}

/** One card in the plugin list: the installed copy and/or the store entry. */
export interface PluginRow {
  id: string;
  manifest: PluginManifest;
  installed?: InstalledPlugin;
  store?: StorePlugin;
  trackers: string[];
}

export type PluginTone = 'ok' | 'update' | 'warn' | 'muted';

const VERSION = /^(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?$/;

/** Orders semantic versions like the backend: -1, 0 or 1; pre-releases sort first. */
export function compareVersions(a: string, b: string): number {
  const ma = VERSION.exec(a), mb = VERSION.exec(b);
  if (!ma || !mb) return ma ? 1 : mb ? -1 : a.localeCompare(b);
  for (let i = 1; i <= 3; i++) {
    const d = Number(ma[i]) - Number(mb[i]);
    if (d) return Math.sign(d);
  }
  if (ma[4] === mb[4]) return 0;
  if (!ma[4]) return 1;
  if (!mb[4]) return -1;
  const pa = ma[4].split('.'), pb = mb[4].split('.');
  for (let i = 0; i < Math.min(pa.length, pb.length); i++) {
    const na = /^\d+$/.test(pa[i]), nb = /^\d+$/.test(pb[i]);
    if (na && nb && Number(pa[i]) !== Number(pb[i])) return Math.sign(Number(pa[i]) - Number(pb[i]));
    if (na !== nb) return na ? -1 : 1;
    if (!na && pa[i] !== pb[i]) return pa[i] < pb[i] ? -1 : 1;
  }
  return Math.sign(pa.length - pb.length);
}

/** Installed plugins first, then the store, each alphabetically by name. */
export function pluginRows(o: PluginOverview): PluginRow[] {
  const rows = new Map<string, PluginRow>();
  for (const p of o.installed) {
    rows.set(p.manifest.id, { id: p.manifest.id, manifest: p.manifest, installed: p, trackers: o.trackers?.[p.manifest.id] ?? [] });
  }
  for (const s of o.store.plugins) {
    const row = rows.get(s.manifest.id);
    if (row) row.store = s;
    else rows.set(s.manifest.id, { id: s.manifest.id, manifest: s.manifest, store: s, trackers: [] });
  }
  return [...rows.values()].sort((a, b) =>
    Number(!a.installed) - Number(!b.installed) || a.manifest.name.localeCompare(b.manifest.name));
}

/** Short status for a row. */
export function pluginStatus(row: PluginRow): { label: string; tone: PluginTone } {
  const i = row.installed;
  if (i) {
    if (i.update_available && i.latest_version) return { label: `Update to ${i.latest_version} available`, tone: 'update' };
    if (!i.ready) return { label: 'Needs configuration', tone: 'warn' };
    if (i.last_error) return { label: 'Failed to start', tone: 'warn' };
    return { label: i.running ? 'Installed · running' : 'Installed', tone: 'ok' };
  }
  if (row.store && !row.store.installable) return { label: row.store.reason || 'Not available for this server', tone: 'muted' };
  return { label: 'Not installed', tone: 'muted' };
}

const CAPABILITIES: Record<string, string> = {
  quote: 'Quotes', history: 'Price history', search: 'Symbol search',
  financials: 'Financial statements', research: 'Research data', forum: 'Community posts',
};

/** Human names of declared capabilities, in a stable order. */
export function capabilityLabels(capabilities: string[]): string[] {
  return Object.keys(CAPABILITIES).filter(c => capabilities.includes(c)).map(c => CAPABILITIES[c]);
}

/** Matches a free-text filter against name, id, provider and description. */
export function matchesFilter(row: PluginRow, filter: string): boolean {
  const q = filter.trim().toLowerCase();
  if (!q) return true;
  const m = row.manifest;
  return [m.name, m.id, m.provider.id, m.provider.label, m.description, m.publisher].some(v => v?.toLowerCase().includes(q));
}

/** Confirmation text before removing a plugin. */
export function uninstallPrompt(row: PluginRow): string {
  const base = `Uninstall ${row.manifest.name}? The ${row.manifest.provider.id} provider stops being available.`;
  if (!row.trackers.length) return base;
  const n = row.trackers.length;
  return `${base} ${n} active tracker${n === 1 ? '' : 's'} (${row.trackers.join(', ')}) will stop receiving prices until it is installed again; their history is kept.`;
}
