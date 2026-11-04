// Pure helpers for saved research tasks (checklist, status, resume). No imports, so `node --test` can run the spec.

export type DeliverableStatus = 'pending' | 'done' | 'missing';
export interface TaskDeliverable { title: string; status: DeliverableStatus | string; note?: string }

/** A task as GET /api/ai/tasks returns it. */
export interface SavedTask {
  id: number; title: string; status: string; steps: number; tool_calls: number;
  /** RFC 3339, UTC. */
  created_at: string; updated_at: string;
  deliverables: TaskDeliverable[]; resumable: boolean;
}

/** The server accepts this many required parts, of at most 200 characters each. */
export const MAX_DELIVERABLES = 12;
export const MAX_DELIVERABLE_LENGTH = 200;

/** Required parts typed one per line; blank lines, list dashes and repeats are dropped. */
export function parseDeliverables(text: string): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const raw of text.split('\n')) {
    const title = raw.replace(/^\s*[-*•]\s*/, '').trim().slice(0, MAX_DELIVERABLE_LENGTH);
    const key = title.toLowerCase();
    if (!title || seen.has(key)) continue;
    seen.add(key);
    out.push(title);
    if (out.length >= MAX_DELIVERABLES) break;
  }
  return out;
}

const STATUS_LABEL: Record<string, string> = {
  running: 'Running', complete: 'Complete', incomplete: 'Incomplete: parts are missing', failed: 'Failed', cancelled: 'Stopped',
};

export function taskStatusLabel(status: string): string {
  return STATUS_LABEL[status] ?? status;
}

/** Reads the checklist a `checkpoint` event carries; anything unreadable is no checklist. */
export function parseChecklist(raw: string | undefined): TaskDeliverable[] {
  if (!raw) return [];
  try {
    const list = JSON.parse(raw);
    return Array.isArray(list) ? list.filter(d => d && typeof d.title === 'string') : [];
  } catch {
    return [];
  }
}

/** One line on how far the checklist is. A part the answer never reported counts as missing, never as done. */
export function checklistLine(list: readonly TaskDeliverable[]): string {
  if (!list.length) return '';
  const done = list.filter(d => d.status === 'done').length;
  const missing = list.filter(d => d.status === 'missing').length;
  const open = list.length - done - missing;
  const parts = [`${done} of ${list.length} done`];
  if (missing) parts.push(`${missing} reported missing`);
  if (open) parts.push(`${open} not yet reported`);
  return parts.join(' · ');
}

/** Whether a task can be continued from its last checkpoint: anything but a complete one. A task still marked running
 *  belongs to a run that died with the server, so it can be resumed too; the page disables Resume while its own run streams. */
export function canResume(task: Pick<SavedTask, 'status' | 'resumable'>): boolean {
  return task.resumable && task.status !== 'complete';
}
