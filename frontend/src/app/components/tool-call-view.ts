/**
 * State and wording for tool calls shown in the research agent chat and in
 * agent teams. Pure, free of imports so node --test can run it. A call is
 * tied to its result by the call id the server sends, so two calls to the
 * same tool at once cannot swap results.
 */
export type ToolCallStatus = 'running' | 'ok' | 'error';

export interface ToolCall {
  callId: string;
  seq?: number;
  name: string;
  /** The arguments as shown (redacted and bounded by the server). */
  args?: string;
  /** The result as shown. */
  result?: string;
  error?: string;
  status: ToolCallStatus;
  startedAt?: number;
  durationMs?: number;
  /** The shown text is a cut of something longer. */
  truncated?: boolean;
  originalBytes?: number;
  /** Secrets were masked in what is shown. */
  redacted?: boolean;
  /** The agent that made the call, in a team. */
  actor?: string;
}

/** The fields of a tool_start or tool_result stream event the UI needs. */
export interface ToolEvent {
  name?: string; arguments?: string; result?: string; tool_error?: string;
  call_id?: string; seq?: number; duration_ms?: number; truncated?: boolean; original_bytes?: number; redacted?: boolean;
}

export function startCall(calls: ToolCall[], ev: ToolEvent, now: number, actor?: string): ToolCall[] {
  const call: ToolCall = {
    callId: ev.call_id || `local-${calls.length + 1}`, seq: ev.seq, name: ev.name || 'tool', args: ev.arguments,
    status: 'running', startedAt: now, redacted: ev.redacted, truncated: ev.truncated, actor,
  };
  return [...calls, call];
}

/** Completes the call the event belongs to: by call id, else the oldest running call of the same tool. */
export function finishCall(calls: ToolCall[], ev: ToolEvent, now: number, actor?: string): ToolCall[] {
  let i = ev.call_id ? calls.findIndex(c => c.callId === ev.call_id) : -1;
  if (i < 0) i = calls.findIndex(c => c.status === 'running' && (!ev.name || c.name === ev.name));
  const status: ToolCallStatus = ev.tool_error ? 'error' : 'ok';
  const done = {
    status, result: ev.result, error: ev.tool_error, truncated: ev.truncated, originalBytes: ev.original_bytes, redacted: ev.redacted,
  };
  if (i < 0) {
    return [...calls, { callId: ev.call_id || `local-${calls.length + 1}`, seq: ev.seq, name: ev.name || 'tool', durationMs: ev.duration_ms, actor, ...done }];
  }
  const was = calls[i];
  const durationMs = ev.duration_ms ?? (was.startedAt !== undefined ? now - was.startedAt : undefined);
  const next = calls.slice();
  next[i] = { ...was, ...done, durationMs, redacted: was.redacted || ev.redacted, truncated: was.truncated || ev.truncated };
  return next;
}

/** Pretty-printed JSON, or the text as it is when it is not JSON. */
export function prettyText(text: string | undefined): string {
  if (!text) return '';
  try { return JSON.stringify(JSON.parse(text), null, 2); } catch { return text; }
}

/** Binary size in plain words: 820 B, 1.5 KB, 3.2 MB. */
export function formatBytes(n: number | undefined): string {
  if (n === undefined || !Number.isFinite(n)) return '';
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

/** What is cut, masked or missing in a call's display, so a preview is never mistaken for the whole. */
export function displayNotes(call: ToolCall): string[] {
  const notes: string[] = [];
  if (call.truncated) {
    notes.push(call.originalBytes
      ? `The result was ${formatBytes(call.originalBytes)}; the agent was given its beginning and you see the same cut here.`
      : 'This text is shortened: it was longer than what is kept for display.');
  }
  if (call.redacted) notes.push('Credentials were masked in this text.');
  return notes;
}

/** Lines of text that contain the query, case-insensitive; the whole text when the query is empty. */
export function findLines(text: string, query: string): { text: string; matches: number } {
  const q = query.trim().toLowerCase();
  if (!q) return { text, matches: 0 };
  const hits = text.split('\n').filter(l => l.toLowerCase().includes(q));
  return { text: hits.join('\n'), matches: hits.length };
}

/** A short one-line view of JSON arguments: `symbol: NVDA · provider: YAHOO`. */
export function argsSummary(raw: string | undefined): string {
  if (!raw) return '';
  try {
    const parsed = JSON.parse(raw) as Record<string, unknown>;
    return Object.entries(parsed).map(([k, v]) => `${k}: ${typeof v === 'string' ? v : JSON.stringify(v)}`).join(' · ');
  } catch { return raw; }
}

export function durationText(ms: number | undefined): string {
  if (ms === undefined) return '';
  return ms < 1000 ? `${ms} ms` : `${(ms / 1000).toFixed(1)} s`;
}
