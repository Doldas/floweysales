import type { RoutingDomain, RoutingPolicy, RoutingRegion, RoutingSource } from '../models';

export type SourceState = 'ready' | 'needs_key' | 'disabled';

export function sourceState(s: RoutingSource): SourceState {
  if (!s.enabled) return 'disabled';
  if (s.needs_key && !s.configured) return 'needs_key';
  return 'ready';
}

export function stateLabel(s: RoutingSource): string {
  return { ready: 'Ready', needs_key: 'Needs credentials', disabled: 'Disabled' }[sourceState(s)];
}

/** Sources that serve a domain, split into those a policy can name and those managed elsewhere. */
export function sourcesFor(domain: string, sources: RoutingSource[]): { routable: RoutingSource[]; managed: RoutingSource[] } {
  const serving = sources.filter(s => s.domains.includes(domain));
  return { routable: serving.filter(s => !s.managed_by && s.scope === 'system'), managed: serving.filter(s => !!s.managed_by) };
}

/** Domains grouped for display, in the order the server lists them. */
export function groupDomains(domains: RoutingDomain[]): { group: string; domains: RoutingDomain[] }[] {
  const out: { group: string; domains: RoutingDomain[] }[] = [];
  for (const d of domains) {
    let g = out.find(x => x.group === d.group);
    if (!g) out.push((g = { group: d.group, domains: [] }));
    g.domains.push(d);
  }
  return out;
}

/** byRegion maps a market code to the source tried first for that market's instruments. */
export interface PolicyForm { primary: string; fallback: string[]; noFallback: boolean; byRegion?: Record<string, string> }

export function formFrom(policy: RoutingPolicy | undefined, domain: RoutingDomain): PolicyForm {
  return { primary: policy?.primary ?? '', fallback: [...(policy?.fallback ?? [])], noFallback: domain.no_fallback || !!policy?.no_fallback,
    byRegion: { ...(policy?.by_region ?? {}) } };
}

/** Sets, or with an empty id removes, the source tried first for a market. */
export function setRegion(f: PolicyForm, code: string, id: string): PolicyForm {
  const byRegion = { ...(f.byRegion ?? {}) };
  if (id) byRegion[code] = id; else delete byRegion[code];
  return { ...f, byRegion };
}

/** What must be fixed before saving; the server checks everything again. */
export function policyProblems(f: PolicyForm, routable: RoutingSource[]): string[] {
  const ids = new Set(routable.map(s => s.id));
  const out: string[] = [];
  if (!f.primary) out.push('Choose a primary source.');
  else if (!ids.has(f.primary)) out.push('The primary source is not available.');
  if (!f.noFallback) {
    if (f.fallback.includes(f.primary)) out.push('The primary source cannot also be a fallback.');
    if (f.fallback.some(id => !ids.has(id))) out.push('A fallback source is not available.');
    if (f.fallback.length > 4) out.push('Use at most four fallback sources.');
  }
  if (Object.values(f.byRegion ?? {}).some(id => !ids.has(id))) out.push('A source chosen for a market is not available.');
  return out;
}

export function policyBody(f: PolicyForm): { primary: string; fallback: string[]; no_fallback: boolean; by_region?: Record<string, string> } {
  const body: { primary: string; fallback: string[]; no_fallback: boolean; by_region?: Record<string, string> } =
    { primary: f.primary, fallback: f.noFallback ? [] : f.fallback.filter(id => id !== f.primary), no_fallback: f.noFallback };
  if (Object.keys(f.byRegion ?? {}).length) body.by_region = { ...f.byRegion };
  return body;
}

/** Toggles a fallback source, keeping the order in which they were added. */
export function toggleFallback(f: PolicyForm, id: string): PolicyForm {
  return { ...f, fallback: f.fallback.includes(id) ? f.fallback.filter(x => x !== id) : [...f.fallback, id] };
}

/** Moves a fallback source one step earlier or later in the chain. */
export function moveFallback(f: PolicyForm, id: string, delta: -1 | 1): PolicyForm {
  const i = f.fallback.indexOf(id);
  const j = i + delta;
  if (i < 0 || j < 0 || j >= f.fallback.length) return f;
  const next = [...f.fallback];
  [next[i], next[j]] = [next[j], next[i]];
  return { ...f, fallback: next };
}

/** One line on what the policy of a domain does today. */
export function policyLine(policy: RoutingPolicy | undefined, domain: RoutingDomain, sources: RoutingSource[], regions: RoutingRegion[] = []): string {
  if (!policy) return domain.routed ? 'No policy: features choose their own source.' : 'No policy. Features choose their own source; none reads a policy for this domain yet.';
  const name = (id: string) => sources.find(s => s.id === id)?.label ?? id;
  const chain = policy.no_fallback || !policy.fallback?.length ? 'no fallback' : `then ${policy.fallback.map(name).join(', then ')}`;
  const markets = Object.entries(policy.by_region ?? {}).map(([code, id]) => `${regions.find(r => r.code === code)?.label ?? code}, ${name(id)}`);
  const perMarket = markets.length ? ` Tried first per market: ${markets.join('; ')}.` : '';
  return `${name(policy.primary)}, ${chain}.${perMarket}${domain.routed ? '' : ' Saved, but no feature reads it for this domain yet.'}`;
}
