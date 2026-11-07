/**
 * Per-user workspace preferences: information density and the state of
 * resizable, collapsible panels. Pure and import-free so node --test can run
 * it. Everything a user can set here is a display choice; no data is hidden
 * by it, and nothing is administered centrally.
 */
export type Density = 'comfortable' | 'compact';

export const DENSITIES: readonly Density[] = ['comfortable', 'compact'];

export interface PanelState {
  /** Folded to its header (summary and critical metadata stay visible). Absent means the pane's own default. */
  collapsed?: boolean;
  /** Body height in CSS pixels chosen by the user; absent means the pane default. */
  height?: number;
}

export interface WorkspacePrefs {
  density: Density;
  panels: Record<string, PanelState>;
}

export const DEFAULT_PREFS: WorkspacePrefs = { density: 'comfortable', panels: {} };

export const STORAGE_KEY = 'fs.workspace';
export const MAX_PANELS = 60;
export const MIN_PANE_HEIGHT = 120;
export const MAX_PANE_HEIGHT = 1400;
export const PANE_STEP = 24;

const ID = /^[a-z0-9][a-z0-9._-]{0,47}$/;

export const isPanelId = (id: unknown): id is string => typeof id === 'string' && ID.test(id);

export function clampHeight(value: number, min = MIN_PANE_HEIGHT, max = MAX_PANE_HEIGHT): number {
  if (!Number.isFinite(value)) return min;
  return Math.min(max, Math.max(min, Math.round(value)));
}

/** Reads stored JSON; anything malformed falls back to defaults. Never throws. */
export function parsePrefs(raw: string | null): WorkspacePrefs {
  const out: WorkspacePrefs = { density: DEFAULT_PREFS.density, panels: {} };
  if (!raw) return out;
  let data: unknown;
  try {
    data = JSON.parse(raw);
  } catch {
    return out;
  }
  if (!data || typeof data !== 'object') return out;
  const { density, panels } = data as Record<string, unknown>;
  if (density === 'compact' || density === 'comfortable') out.density = density;
  if (panels && typeof panels === 'object' && !Array.isArray(panels)) {
    for (const [id, value] of Object.entries(panels as Record<string, unknown>)) {
      if (!isPanelId(id) || !value || typeof value !== 'object') continue;
      if (Object.keys(out.panels).length >= MAX_PANELS) break;
      const { collapsed, height } = value as Record<string, unknown>;
      const state: PanelState = {};
      if (typeof collapsed === 'boolean') state.collapsed = collapsed;
      if (typeof height === 'number' && Number.isFinite(height)) state.height = clampHeight(height);
      if (state.collapsed !== undefined || state.height !== undefined) out.panels[id] = state;
    }
  }
  return out;
}

export const serializePrefs = (prefs: WorkspacePrefs): string => JSON.stringify(parsePrefs(JSON.stringify(prefs)));

/** Returns new prefs with one panel's state merged in; an all-default state is dropped. */
export function withPanel(prefs: WorkspacePrefs, id: string, patch: PanelState): WorkspacePrefs {
  if (!isPanelId(id)) return prefs;
  const next: PanelState = { ...prefs.panels[id], ...patch };
  if (next.collapsed === undefined) delete next.collapsed;
  if (next.height !== undefined) next.height = clampHeight(next.height);
  else delete next.height;
  const panels = { ...prefs.panels };
  if (next.collapsed !== undefined || next.height !== undefined) panels[id] = next;
  else delete panels[id];
  return { ...prefs, panels };
}

export const withDensity = (prefs: WorkspacePrefs, density: Density): WorkspacePrefs => ({ ...prefs, density: DENSITIES.includes(density) ? density : prefs.density });

/** Keyboard resize of a horizontal splitter. Returns the new height, or null for a key it does not handle. */
export function heightForKey(key: string, current: number, min = MIN_PANE_HEIGHT, max = MAX_PANE_HEIGHT, step = PANE_STEP): number | null {
  switch (key) {
    case 'ArrowDown': return clampHeight(current + step, min, max);
    case 'ArrowUp': return clampHeight(current - step, min, max);
    case 'PageDown': return clampHeight(current + step * 5, min, max);
    case 'PageUp': return clampHeight(current - step * 5, min, max);
    case 'End': return max;
    case 'Home': return min;
    default: return null;
  }
}

// ---- Windowing for long tables. Only what is on screen is drawn; every row stays in the data.

export interface RowWindow {
  start: number;
  end: number;
  /** Pixels of spacer above the drawn rows. */
  before: number;
  /** Pixels of spacer below the drawn rows. */
  after: number;
}

/** Rows to draw for a scroll position, assuming `rowHeight` per row, with `overscan` extra rows on each side. */
export function rowWindow(total: number, scrollTop: number, viewport: number, rowHeight: number, overscan = 8): RowWindow {
  const count = Number.isFinite(total) ? Math.max(0, Math.floor(total)) : 0;
  overscan = Number.isFinite(overscan) ? Math.max(0, Math.floor(overscan)) : 0;
  const h = Number.isFinite(rowHeight) && rowHeight > 0 ? rowHeight : 40;
  if (count === 0) return { start: 0, end: 0, before: 0, after: 0 };
  const top = Math.max(0, Number.isFinite(scrollTop) ? scrollTop : 0);
  const view = Math.max(h, Number.isFinite(viewport) ? viewport : h);
  const first = Math.min(count - 1, Math.floor(top / h));
  const start = Math.max(0, first - overscan);
  const end = Math.max(Math.min(count, Math.ceil((top + view) / h) + overscan), start + 1);
  return { start, end, before: start * h, after: Math.max(0, (count - end) * h) };
}

/** Tables with fewer rows than this are drawn in full. */
export const WINDOW_THRESHOLD = 120;
