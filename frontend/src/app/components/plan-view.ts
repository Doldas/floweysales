/**
 * Display helpers for the wealth plan page. Pure functions so they run under
 * node --test; the classification and projection live in the server
 * (internal/planner).
 */
import type { PlanAllocation, PlanBand, PlanMilestone, PlanProfile } from '../models';

export const PHASES: readonly { id: string; label: string }[] = [
  { id: 'foundation', label: 'Foundation' },
  { id: 'starter', label: 'Starter' },
  { id: 'accumulation', label: 'Accumulation' },
  { id: 'acceleration', label: 'Acceleration' },
  { id: 'preservation', label: 'Preservation' },
  { id: 'income', label: 'Income' },
];

export interface PhaseStep { id: string; label: string; state: 'done' | 'current' | 'todo' }

/** The six-step track with everything before the current phase marked done. */
export function phaseSteps(currentId: string | undefined): PhaseStep[] {
  const at = PHASES.findIndex(p => p.id === currentId);
  return PHASES.map((p, i) => ({ ...p, state: at < 0 ? 'todo' : i < at ? 'done' : i === at ? 'current' : 'todo' }));
}

export interface FanPaths {
  /** Closed outline of the p10–p90 band. */
  band: string;
  /** The median line. */
  median: string;
  /** Vertical position of the goal line, or null when it is off the chart. */
  goalY: number | null;
  /** Highest value on the y axis. */
  max: number;
}

/**
 * SVG paths for the projection fan. Real figures are in today's money. The
 * y axis runs from 0 to the larger of the p90 end value and the goal.
 */
export function fanPaths(bands: PlanBand[], width: number, height: number, real: boolean, goal = 0, pad = 6): FanPaths {
  const empty: FanPaths = { band: '', median: '', goalY: null, max: 0 };
  if (bands.length < 2) return empty;
  const pick = (b: PlanBand) => (real ? [b.real_p10, b.real_p50, b.real_p90] : [b.p10, b.p50, b.p90]);
  const max = Math.max(...bands.map(b => pick(b)[2]), goal) * 1.05 || 1;
  const x = (i: number) => pad + (i / (bands.length - 1)) * (width - 2 * pad);
  const y = (v: number) => pad + (1 - v / max) * (height - 2 * pad);
  const pt = (i: number, v: number) => `${x(i).toFixed(1)} ${y(v).toFixed(1)}`;
  const upper = bands.map((b, i) => `${i ? 'L' : 'M'}${pt(i, pick(b)[2])}`).join('');
  const lower = bands.map((b, i) => `L${pt(bands.length - 1 - i, pick(bands[bands.length - 1 - i])[0])}`).join('');
  return {
    band: `${upper}${lower}Z`,
    median: bands.map((b, i) => `${i ? 'L' : 'M'}${pt(i, pick(b)[1])}`).join(''),
    goalY: goal > 0 && goal <= max ? Number(y(goal).toFixed(1)) : null,
    max,
  };
}

export interface MilestoneDot { label: string; x: number; y: number; year: number }

/**
 * Where the milestones sit on the real-money fan: the year the median first
 * reaches each amount. Milestones the median never reaches are left out.
 */
export function milestoneDots(milestones: PlanMilestone[], bands: PlanBand[], width: number, height: number, max: number, pad = 6): MilestoneDot[] {
  if (bands.length < 2 || max <= 0) return [];
  return milestones.filter(m => m.year !== undefined && m.amount <= max).map(m => ({
    label: m.label, year: m.year!,
    x: Number((pad + (m.year! / (bands.length - 1)) * (width - 2 * pad)).toFixed(1)),
    y: Number((pad + (1 - m.amount / max) * (height - 2 * pad)).toFixed(1)),
  }));
}

export interface AllocationRow { id: keyof PlanAllocation; label: string; pct: number; note: string }

const ALLOCATION_LABELS: Record<keyof PlanAllocation, string> = {
  core_equity: 'Core equity', bonds: 'Bonds', cash: 'Cash', satellite: 'Satellite',
};

/** Rows for the stacked bar and its table; the satellite carries its cap. */
export function allocationRows(a: PlanAllocation): AllocationRow[] {
  return (Object.keys(ALLOCATION_LABELS) as (keyof PlanAllocation)[]).map(id => ({
    id, label: ALLOCATION_LABELS[id], pct: a[id],
    note: id === 'satellite' ? `High-risk sleeve, capped at ${a.satellite} %` : '',
  }));
}

/** Fields the form edits, as strings so an empty box stays empty. */
export interface PlanForm {
  monthly_expenses: string; emergency_fund: string; high_interest_debt: string; invested: string;
  monthly_contribution: string; age: string; retirement_age: string; goal_amount: string;
  risk_profile: NonNullable<PlanProfile['risk_profile']>; retired: boolean; use_paper_portfolio: boolean; currency: string;
}

export const EMPTY_FORM: PlanForm = {
  monthly_expenses: '', emergency_fund: '', high_interest_debt: '', invested: '', monthly_contribution: '',
  age: '', retirement_age: '', goal_amount: '', risk_profile: 'balanced', retired: false, use_paper_portfolio: false, currency: '',
};

const NUMBER_FIELDS = ['monthly_expenses', 'emergency_fund', 'high_interest_debt', 'invested', 'monthly_contribution', 'age', 'retirement_age', 'goal_amount'] as const;

export function formFromProfile(p: PlanProfile | null | undefined): PlanForm {
  if (!p) return { ...EMPTY_FORM };
  const form = { ...EMPTY_FORM, risk_profile: p.risk_profile ?? 'balanced', retired: !!p.retired, use_paper_portfolio: !!p.use_paper_portfolio, currency: p.currency ?? '' };
  for (const k of NUMBER_FIELDS) form[k] = p[k] ? String(p[k]) : '';
  return form;
}

/**
 * The profile to send, or null while the form cannot make a plan yet (no
 * monthly expenses, or a number that is not a number). Empty boxes are left
 * out rather than sent as zero.
 */
export function profileFromForm(f: PlanForm): PlanProfile | null {
  const out: Record<string, unknown> = { risk_profile: f.risk_profile };
  for (const k of NUMBER_FIELDS) {
    const raw = f[k].trim();
    if (raw === '') continue;
    const v = Number(raw);
    if (!Number.isFinite(v) || v < 0) return null;
    out[k] = v;
  }
  if (!(Number(out['monthly_expenses']) > 0)) return null;
  if (f.retired) out['retired'] = true;
  if (f.use_paper_portfolio) out['use_paper_portfolio'] = true;
  if (/^[A-Za-z]{3}$/.test(f.currency.trim())) out['currency'] = f.currency.trim().toUpperCase();
  return out as unknown as PlanProfile;
}

/** One sentence for the fan chart's accessible name. */
export function fanSummary(bands: PlanBand[], real: boolean, goal: number, currency: string, money: (v: number, c: string) => string): string {
  if (bands.length < 2) return 'No projection to show.';
  const last = bands[bands.length - 1];
  const [lo, mid, hi] = real ? [last.real_p10, last.real_p50, last.real_p90] : [last.p10, last.p50, last.p90];
  return `In ${last.year} years the middle outcome is ${money(mid, currency)}, with a range from ${money(lo, currency)} to ${money(hi, currency)}` +
    `${real ? ' in today\'s money' : ''}; the goal is ${money(goal, currency)}.`;
}
