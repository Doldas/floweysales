/**
 * The budget of a team run: ceilings the user may set on tokens, cost and
 * tool calls, and the line that says what the run spent. Pure and free of
 * imports so node --test can run it. What the endpoint did not report is
 * said to be unknown, never shown as zero.
 */
export interface BudgetSummary {
  requests: number; prompt_tokens: number; completion_tokens: number;
  /** null when it cannot be told: an endpoint without usage figures, or a model without a price. */
  cost: number | null; unknown_requests?: number; tool_calls: number;
  max_tokens?: number; max_cost?: number; max_tool_calls?: number; stopped_by?: string;
}

/** What the user typed; empty means no ceiling. */
export interface BudgetInput { tokens: string; cost: string; calls: string }
export interface BudgetLimits { max_tokens: number | null; max_cost: number | null; max_tool_calls: number | null }

export const NO_BUDGET: BudgetInput = { tokens: '', cost: '', calls: '' };

export const MIN_TOKENS = 1000;
export const MAX_TOKENS = 10_000_000;
export const MAX_COST = 100_000;
export const MAX_CALLS = 500;

export type ParsedBudget = { ok: true; limits: BudgetLimits } | { ok: false; error: string };

/** Turns the three fields into ceilings, or says which one is wrong. */
export function parseBudget(i: BudgetInput): ParsedBudget {
  const tokens = whole(i.tokens);
  const calls = whole(i.calls);
  const cost = i.cost.trim() === '' ? null : Number(i.cost.trim().replace(',', '.'));
  if (tokens === 'bad' || (tokens !== null && (tokens < MIN_TOKENS || tokens > MAX_TOKENS))) {
    return { ok: false, error: `Token ceiling: a whole number from ${MIN_TOKENS} to ${MAX_TOKENS}, or empty for none.` };
  }
  if (cost !== null && (!Number.isFinite(cost) || cost <= 0 || cost > MAX_COST)) {
    return { ok: false, error: `Cost ceiling: an amount in US dollars above 0 and up to ${MAX_COST}, or empty for none.` };
  }
  if (calls === 'bad' || (calls !== null && (calls < 1 || calls > MAX_CALLS))) {
    return { ok: false, error: `Tool-call ceiling: a whole number from 1 to ${MAX_CALLS}, or empty for none.` };
  }
  return { ok: true, limits: { max_tokens: tokens, max_cost: cost, max_tool_calls: calls } };
}

function whole(text: string): number | null | 'bad' {
  const t = text.trim();
  if (t === '') return null;
  return /^\d+$/.test(t) ? Number(t) : 'bad';
}

/** The ceilings as request fields; a ceiling that is not set is left out. */
export function budgetBody(l: BudgetLimits): { max_tokens?: number; max_cost?: number; max_tool_calls?: number } {
  const out: { max_tokens?: number; max_cost?: number; max_tool_calls?: number } = {};
  if (l.max_tokens !== null) out.max_tokens = l.max_tokens;
  if (l.max_cost !== null) out.max_cost = l.max_cost;
  if (l.max_tool_calls !== null) out.max_tool_calls = l.max_tool_calls;
  return out;
}

/** The fields of a saved team's ceilings. */
export function budgetInputOf(t: { max_tokens?: number | null; max_cost?: number | null; max_tool_calls?: number | null }): BudgetInput {
  return {
    tokens: t.max_tokens ? String(t.max_tokens) : '',
    cost: t.max_cost ? String(t.max_cost) : '',
    calls: t.max_tool_calls ? String(t.max_tool_calls) : '',
  };
}

const num = (n: number): string => n.toLocaleString('en-US');

/** What a finished run spent, and why it stopped when a ceiling ended it. */
export function budgetLine(s: BudgetSummary): string {
  const known = s.requests - (s.unknown_requests ?? 0);
  const parts: string[] = [];
  if (known === 0 && s.requests > 0) parts.push('tokens unknown: the model endpoint reports no usage');
  else {
    const total = s.prompt_tokens + s.completion_tokens;
    parts.push(`${num(total)} tokens${s.unknown_requests ? ` (${s.unknown_requests} of ${s.requests} requests reported none)` : ''}`);
  }
  parts.push(s.cost === null ? 'cost unknown' : `cost $${+s.cost.toFixed(4)}`);
  parts.push(`${s.tool_calls} tool ${s.tool_calls === 1 ? 'call' : 'calls'}`);
  let line = parts.join(' · ') + '.';
  if (s.stopped_by) line += ` Stopped at ${s.stopped_by}; the answers may be incomplete.`;
  return line;
}
