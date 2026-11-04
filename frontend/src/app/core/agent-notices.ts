/**
 * What the agent tells the user about a run besides its answer. A retry in
 * progress is a status line, not an error; a recovery is a quiet note; a
 * warning stays until dismissed. Pure, tested with node --test.
 */
export interface AgentNoticeEvent {
  content?: string;
  severity?: string;
  code?: string;
  attempt?: number;
  max_attempts?: number;
  detail?: string;
}

export interface AgentNotice { id: number; text: string; tone: 'info' | 'warning' }

export interface NoticeState {
  /** One line while a retry is running; empty otherwise. */
  status: string;
  /** Technical reason for the current retry, for troubleshooting. */
  statusDetail: string;
  /** Notes and warnings that stay until dismissed. */
  notices: AgentNotice[];
  /** How many failed model calls were retried and then succeeded. */
  recoveries: number;
  nextId: number;
}

export const EMPTY_NOTICES: NoticeState = { status: '', statusDetail: '', notices: [], recoveries: 0, nextId: 1 };

export function applyNotice(s: NoticeState, ev: AgentNoticeEvent): NoticeState {
  const text = (ev.content ?? '').trim();
  switch (ev.code) {
    case 'retry':
      return { ...s, status: text, statusDetail: ev.detail ?? '' };
    case 'recovered':
      return { ...s, status: '', statusDetail: '', recoveries: s.recoveries + 1 };
    default: {
      if (!text) return s;
      const tone = ev.severity === 'info' ? 'info' : 'warning';
      return { ...s, notices: [...s.notices, { id: s.nextId, text, tone }], nextId: s.nextId + 1 };
    }
  }
}

export function dismissNotice(s: NoticeState, id: number): NoticeState {
  return { ...s, notices: s.notices.filter(n => n.id !== id) };
}

/** A run that ended: a retry still pending is over, whatever the outcome. */
export function finishNotices(s: NoticeState): NoticeState {
  return s.status || s.statusDetail ? { ...s, status: '', statusDetail: '' } : s;
}

/** "Recovered after retrying" line for the finished run, or '' when nothing was retried. */
export function recoveryLine(s: NoticeState): string {
  if (!s.recoveries) return '';
  return s.recoveries === 1 ? 'A model call failed once and was retried successfully.' : `${s.recoveries} model calls failed and were retried successfully.`;
}
