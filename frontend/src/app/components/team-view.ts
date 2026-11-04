/**
 * State and wording for agent teams. A team run streams events tagged with
 * the member they belong to; applyTeamEvent folds them into one immutable
 * state the page renders. Pure, tested with node --test.
 */
import type { Persona, Stance, TeamEvent } from '../models';
import { finishCall, startCall, type ToolCall } from '#app/components/tool-call-view';
import { auditSummary, removedSummary, type AnswerFinding, type AnswerSource } from '#app/core/answer-sources';

export const STANCE_LABEL: Record<Stance, string> = {
  fits: 'Fits', partly: 'Partly fits', does_not_fit: 'Does not fit', insufficient_data: 'Not enough data',
};
export const STANCE_ICON: Record<Stance, string> = { fits: 'bi-check-circle-fill', partly: 'bi-dash-circle-fill', does_not_fit: 'bi-x-circle-fill', insufficient_data: 'bi-question-circle-fill' };

/** A tool call of a member; the shared tool call view shows it with its input, output and timing. */
export type MemberStep = ToolCall;
export interface MemberState {
  id: string; name: string; status: 'waiting' | 'running' | 'done' | 'error';
  streamed: string; final: string; stance?: Stance; steps: MemberStep[]; error: string;
  /** The model this member answers with, from the server; it changes if a fallback takes over. */
  model?: string;
  /** Why the member has no tools, when that is so. */
  note?: string;
  /** What the member retrieved, what the citation check found in its answer, and what the block option removed. */
  sources?: AnswerSource[]; audit?: AnswerFinding[]; removed?: AnswerFinding[];
}
export interface TeamState { members: MemberState[]; chair: MemberState | null; finished: boolean; error: string }

const blank = (id: string, name: string): MemberState => ({ id, name, status: 'waiting', streamed: '', final: '', steps: [], error: '' });

export function startTeam(members: { id: string; name: string }[], withChair: boolean): TeamState {
  return { members: members.map(m => blank(m.id, m.name)), chair: withChair ? blank('chair', 'Chair') : null, finished: false, error: '' };
}

const isStance = (v?: string): v is Stance => v === 'fits' || v === 'partly' || v === 'does_not_fit' || v === 'insufficient_data';

function patch(s: TeamState, agent: string, f: (m: MemberState) => MemberState): TeamState {
  if (agent === 'chair' && s.chair) return { ...s, chair: f(s.chair) };
  return { ...s, members: s.members.map(m => (m.id === agent ? f(m) : m)) };
}

export function applyTeamEvent(s: TeamState, ev: TeamEvent): TeamState {
  const agent = ev.agent ?? '';
  switch (ev.type) {
    case 'member_start':
      return patch(s, agent, m => ({ ...m, status: 'running', model: ev.model || m.model, note: ev.detail || m.note }));
    case 'run_start':
      return ev.model ? patch(s, agent, m => ({ ...m, model: ev.model })) : s;
    case 'delta':
      return patch(s, agent, m => ({ ...m, streamed: m.streamed + (ev.content ?? '') }));
    case 'tool_start':
      return patch(s, agent, m => ({ ...m, steps: startCall(m.steps, ev, Date.now(), m.name) }));
    case 'tool_result':
      return patch(s, agent, m => ({ ...m, steps: finishCall(m.steps, ev, Date.now(), m.name) }));
    case 'sources':
      return agent ? patch(s, agent, m => ({ ...m, sources: ev.sources ?? [], audit: ev.audit ?? [], removed: ev.removed ?? [] })) : s;
    case 'error':
      return agent ? patch(s, agent, m => ({ ...m, error: ev.error ?? m.error })) : { ...s, error: ev.error ?? 'Team run failed' };
    case 'member_done':
      return patch(s, agent, m => ({
        ...m, final: ev.content ?? '', stance: isStance(ev.stance) ? ev.stance : undefined, error: ev.error ?? m.error,
        status: ev.content ? 'done' : 'error',
      }));
    case 'done': {
      // A member that never reported by the end of the run has no answer to wait for.
      const settle = (m: MemberState): MemberState => (m.status === 'waiting' || m.status === 'running'
        ? { ...m, status: m.final || m.streamed ? 'done' : 'error', error: m.final || m.streamed ? m.error : m.error || 'No answer before the run ended.' } : m);
      return { ...s, members: s.members.map(settle), chair: s.chair ? settle(s.chair) : null, finished: true };
    }
    default:
      return s;
  }
}

/** What a member's card shows: the finished answer, else what has streamed so far. */
export function shownText(m: MemberState): string {
  return stripStance(m.final || m.streamed);
}

/** The citation check result under a member's answer, or '' when the member retrieved no sources. */
export function evidenceLine(m: MemberState): string {
  if (!m.sources?.length && !m.audit?.length && !m.removed?.length) return '';
  return [removedSummary(m.removed ?? []), auditSummary(m.audit ?? [])].filter(Boolean).join(' ');
}

/** The closing STANCE line is a machine-readable summary, shown as a badge instead. */
export function stripStance(text: string): string {
  return text.replace(/\n?\s*\**\s*STANCE\s*:[^\n]*\s*$/i, '').trimEnd();
}

/** How many members landed on each stance, in a fixed order. */
export function stanceCounts(members: MemberState[]): { stance: Stance; label: string; count: number }[] {
  return (Object.keys(STANCE_LABEL) as Stance[])
    .map(stance => ({ stance, label: STANCE_LABEL[stance], count: members.filter(m => m.stance === stance).length }))
    .filter(x => x.count > 0);
}

export const teamReady = (state: TeamState | null): boolean => !!state && state.finished;

/** Personas to preselect: a mix of philosophies that tends to disagree usefully. */
export const DEFAULT_TEAM = ['owner', 'teacher', 'quant', 'innovator'];

export function defaultQuestion(symbol: string): string {
  return symbol
    ? `Is ${symbol} a good fit for your approach right now? Use real data and show the numbers behind your view.`
    : 'Which kind of company fits your approach best today? Use real data for any example you give.';
}

export function togglePersona(selected: string[], id: string, max: number): string[] {
  if (selected.includes(id)) return selected.filter(x => x !== id);
  return selected.length >= max ? selected : [...selected, id];
}

export function personaName(list: Persona[], id: string): string {
  return list.find(p => p.id === id)?.name ?? id;
}
