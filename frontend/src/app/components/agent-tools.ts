/**
 * Pure helpers for the read-only research tools a SYS_ADMIN can grant to an
 * agent node. Free of Angular so they run under node --test.
 */

export interface GrantableTool { name: string; description: string }

export interface ToolCall {
  attempt: number; name: string; arguments?: string; ok: boolean; error?: string; result_bytes: number;
  /** Provenance of a successful result. */
  retrieved_at?: string; sha256?: string; sources?: string[];
}

/** Adds or removes a tool, keeping the grant within the server's limit. */
export function toggleTool(granted: readonly string[] | undefined, name: string, max: number): string[] | null {
  const current = granted ?? [];
  if (current.includes(name)) return current.filter(t => t !== name);
  if (current.length >= max) return null;
  return [...current, name];
}

/** Clamps a tool-call budget to 0 (server default) or 1–max. */
export function clampToolBudget(value: number, max: number): number {
  if (!Number.isFinite(value) || value <= 0) return 0;
  return Math.min(max, Math.trunc(value));
}

/** A readable name for a tool such as get_sec_facts → "SEC facts". */
export function toolLabel(name: string): string {
  const words = name.replace(/^(get|calculate|search)_/, '').split('_');
  return words.map(w => (w === 'sec' || w === 'dcf' || w === 'wacc' || w === 'roic' || w === 'ttm' ? w.toUpperCase() : w)).join(' ')
    .replace(/^./, c => c.toUpperCase()) + (name.startsWith('calculate_') ? ' calculator' : name.startsWith('search_') ? ' search' : '');
}

/** One line per recorded tool call for the run inspector. */
export function describeToolCall(call: ToolCall): string {
  const size = call.result_bytes >= 1024 ? `${(call.result_bytes / 1024).toFixed(1)} KB` : `${call.result_bytes} B`;
  const outcome = call.ok ? `returned ${size}` : `failed: ${call.error || 'unknown error'}`;
  return `${call.attempt > 1 ? `[attempt ${call.attempt}] ` : ''}${call.name}${call.arguments ? ' ' + call.arguments : ''} → ${outcome}`;
}
