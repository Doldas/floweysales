// Pure helpers for the web search settings. No imports, so `node --test` can run the spec.

export interface SearchCatalogEntry {
  id: string; name: string; keyless: boolean; needs_endpoint: boolean; needs_model: boolean; needs_engine_id: boolean;
  requires_approval: boolean; legacy: boolean; model_mediated: boolean; default_model: string;
  default_price_per_1000_usd: number; default_free_monthly_searches: number; pricing: string; notes: string; docs_url: string;
}
export interface SearchUsage { searches: number; prompt_tokens: number; output_tokens: number; est_cost_usd: number; failures: number }
export interface SearchProviderState {
  id: string; enabled: boolean; configured: boolean; has_key: boolean; endpoint: string; model: string; engine_id: string;
  paid_approved: boolean; price_per_1000_usd: number; free_monthly_searches: number; input_per_mtok_usd: number; output_per_mtok_usd: number;
  daily_budget_usd: number; monthly_budget_usd: number;
  state: 'ready' | 'disabled' | 'not_configured' | 'needs_approval' | 'paused' | string;
  status: { last_at_utc?: string; last_outcome?: string; last_kind?: string; last_detail?: string; circuit_open_until_utc?: string };
  usage_today: SearchUsage; usage_month: SearchUsage;
}
export interface SearchPolicy { mode: 'auto' | 'manual'; selected?: string; order: string[]; max_attempts: number; max_retries: number; per_run_budget_usd?: number }
export interface SearchAttempt { at_utc: string; provider: string; outcome: string; kind?: string; detail?: string; est_cost_usd?: number }

const STATE_LABELS: Record<string, string> = {
  ready: 'Ready', disabled: 'Disabled', not_configured: 'Not configured',
  needs_approval: 'Needs spending approval', paused: 'Paused after errors',
};
export function stateLabel(state: string): string { return STATE_LABELS[state] ?? state.replace(/_/g, ' '); }

/** Plain-language help for an error kind the router reports. */
const KIND_HELP: Record<string, string> = {
  missing_key: 'No API key is saved for this provider.',
  unauthorized: 'The provider rejected the credentials (HTTP 401 or 403). Check the key and what it is allowed to do.',
  rate_limited: 'The quota or rate limit is reached (HTTP 429). The provider is paused and tried again later.',
  timeout: 'The provider did not answer in time.',
  unavailable: 'The provider could not be reached or answered with a server error.',
  malformed: 'The provider replied with something that is not its documented format.',
  endpoint: 'The configured address cannot be used (invalid, forbidden or redirecting).',
  empty: 'The provider found nothing.',
};
export function kindHelp(kind: string | undefined): string { return kind ? KIND_HELP[kind] ?? '' : ''; }

export function formatUsd(value: number): string {
  if (!isFinite(value)) return '$0.00';
  const digits = value !== 0 && Math.abs(value) < 0.01 ? 4 : 2;
  return '$' + value.toFixed(digits);
}

/** "12 searches · 3,400 prompt tokens · 1,200 output tokens · est. $0.17": searches and model tokens stay apart. */
export function usageLine(u: SearchUsage): string {
  const n = (v: number) => v.toLocaleString('en-US');
  const parts = [`${n(u.searches)} ${u.searches === 1 ? 'search' : 'searches'}`];
  if (u.prompt_tokens || u.output_tokens) parts.push(`${n(u.prompt_tokens)} prompt tokens`, `${n(u.output_tokens)} output tokens`);
  parts.push(`est. ${formatUsd(u.est_cost_usd)}`);
  if (u.failures) parts.push(`${n(u.failures)} failed`);
  return parts.join(' · ');
}

/** Every displayed time is UTC. Takes an ISO timestamp and never converts to the viewer's zone. */
export function utcTime(iso: string | undefined): string {
  if (!iso || iso.startsWith('0001')) return '';
  const m = /^(\d{4}-\d{2}-\d{2})T(\d{2}:\d{2}:\d{2})/.exec(iso);
  return m ? `${m[1]} ${m[2]} UTC` : '';
}

export function attemptLine(a: SearchAttempt, names: Record<string, string>): string {
  const who = names[a.provider] ?? a.provider;
  const when = utcTime(a.at_utc);
  const what = a.outcome === 'ok' ? 'answered' : a.outcome === 'empty' ? 'found nothing' : a.outcome === 'skipped' ? `skipped (${a.detail ?? ''})` : `failed: ${a.detail ?? a.kind ?? ''}`;
  return `${when ? when + ' · ' : ''}${who} ${what}`.trim();
}

/** Moves a provider one place up (-1) or down (+1) in the priority order. */
export function moveProvider(order: readonly string[], id: string, delta: -1 | 1): string[] {
  const out = [...order];
  const i = out.indexOf(id);
  const j = i + delta;
  if (i < 0 || j < 0 || j >= out.length) return out;
  [out[i], out[j]] = [out[j], out[i]];
  return out;
}

export interface ProviderForm {
  enabled: boolean; api_key: string; endpoint: string; model: string; engine_id: string; paid_approved: boolean;
  price_per_1000_usd: number; free_monthly_searches: number; input_per_mtok_usd: number; output_per_mtok_usd: number;
  daily_budget_usd: number; monthly_budget_usd: number;
}

export function formFrom(p: SearchProviderState): ProviderForm {
  return {
    enabled: p.enabled, api_key: '', endpoint: p.endpoint, model: p.model, engine_id: p.engine_id, paid_approved: p.paid_approved,
    price_per_1000_usd: p.price_per_1000_usd, free_monthly_searches: p.free_monthly_searches,
    input_per_mtok_usd: p.input_per_mtok_usd, output_per_mtok_usd: p.output_per_mtok_usd,
    daily_budget_usd: p.daily_budget_usd, monthly_budget_usd: p.monthly_budget_usd,
  };
}

/** Problems with a provider form, in the words shown under it. Empty means it can be saved. */
export function formProblems(form: ProviderForm, info: SearchCatalogEntry, hasKey: boolean): string[] {
  const out: string[] = [];
  const key = form.api_key.trim();
  if (!info.keyless && !key && !hasKey && form.enabled) out.push('Enter an API key before enabling this provider.');
  if (info.needs_endpoint && !form.endpoint.trim() && form.enabled) out.push('Enter the address of your SearXNG instance, for example http://searxng:8080.');
  if (info.needs_endpoint && form.endpoint.trim() && !/^https?:\/\/[^\s/]+/i.test(form.endpoint.trim())) out.push('The address must start with http:// or https://.');
  if (info.needs_engine_id && !form.engine_id.trim() && form.enabled) out.push('Enter the search engine ID (cx).');
  if (info.requires_approval && form.paid_approved && !(form.daily_budget_usd > 0) && !(form.monthly_budget_usd > 0)) {
    out.push('Approving paid use needs a daily or a monthly spending ceiling.');
  }
  for (const v of [form.daily_budget_usd, form.monthly_budget_usd, form.price_per_1000_usd, form.input_per_mtok_usd, form.output_per_mtok_usd]) {
    if (!(v >= 0)) { out.push('Prices and budgets cannot be negative.'); break; }
  }
  return out;
}

/** The request body for PUT /api/admin/web-search/providers/{id}. A blank key keeps the saved one. */
export function providerBody(form: ProviderForm): Record<string, unknown> {
  const body: Record<string, unknown> = {
    enabled: form.enabled, endpoint: form.endpoint.trim(), model: form.model.trim(), engine_id: form.engine_id.trim(),
    paid_approved: form.paid_approved, price_per_1000_usd: +form.price_per_1000_usd || 0, free_monthly_searches: Math.trunc(+form.free_monthly_searches || 0),
    input_per_mtok_usd: +form.input_per_mtok_usd || 0, output_per_mtok_usd: +form.output_per_mtok_usd || 0,
    daily_budget_usd: +form.daily_budget_usd || 0, monthly_budget_usd: +form.monthly_budget_usd || 0,
  };
  if (form.api_key.trim()) body['api_key'] = form.api_key.trim();
  return body;
}

export const SEARCH_NAMES: Record<string, string> = {
  searxng: 'SearXNG', brave: 'Brave Search', brave_search: 'Brave Search', tavily: 'Tavily', google_grounding: 'Google Search (Gemini grounding)', google_cse: 'Google Custom Search',
};
