import { Injectable, computed, inject, signal } from '@angular/core';
import { Access } from './access';

export interface FlowSummary {
  id: number;
  name: string;
  enabled: boolean;
  interval_minutes: number;
  event_key: string;
  updated_at?: string;
  active_revision?: number | null;
  graph?: { nodes?: { type: string }[]; edges?: unknown[] };
}

export interface RunSummary {
  id: number;
  flowId: number;
  flowName: string;
  status: string;
  trigger: string;
  output: string;
  error: string;
  started_at?: string;
  finished_at?: string | null;
  queued_at?: string | null;
  duration_ms?: number | null;
}

export interface PaperApproval {
  client_order_id: string;
  request_payload: string;
  rationale?: string;
  decision_source?: string;
  requested_by: number;
  created_at: string;
  expires_at?: string;
  status: string;
}

/** How many flows the activity feed samples for recent runs. */
const FEED_FLOWS = 8;

async function getJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, { credentials: 'same-origin', ...init, headers: { 'Content-Type': 'application/json', ...init?.headers } });
  const body = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(body.error || `HTTP ${res.status}`);
  return body as T;
}

/** Epoch milliseconds for the most relevant timestamp of a run. */
export function runTime(run: Pick<RunSummary, 'started_at' | 'queued_at' | 'finished_at'>): number {
  const raw = run.started_at || run.queued_at || run.finished_at;
  const t = raw ? Date.parse(raw) : NaN;
  return Number.isNaN(t) ? 0 : t;
}

/**
 * ActivityStore aggregates what the agents are doing across the workspace:
 * saved flows, their latest runs and the decisions waiting for a human.
 */
@Injectable({ providedIn: 'root' })
export class ActivityStore {
  private readonly access = inject(Access);

  readonly flows = signal<FlowSummary[]>([]);
  readonly runs = signal<RunSummary[]>([]);
  readonly pendingEffects = signal(0);
  readonly paperPending = signal<PaperApproval[]>([]);
  readonly loading = signal(false);
  readonly loaded = signal(false);
  readonly error = signal('');

  readonly inboxCount = computed(() => this.pendingEffects() + this.paperPending().length);
  readonly enabledFlows = computed(() => this.flows().filter(f => f.enabled).length);
  readonly automatedFlows = computed(() => this.flows().filter(f => f.interval_minutes > 0 || !!f.event_key).length);
  readonly activeRuns = computed(() => this.runs().filter(r => ['QUEUED', 'RUNNING', 'AWAITING_APPROVAL'].includes(r.status)).length);
  readonly last24h = computed(() => {
    const since = Date.now() - 24 * 3600 * 1000;
    const recent = this.runs().filter(r => runTime(r) >= since);
    const finished = recent.filter(r => r.status === 'SUCCESS' || r.status === 'FAILED' || r.status === 'DEAD');
    const ok = finished.filter(r => r.status === 'SUCCESS').length;
    return { total: recent.length, successRate: finished.length ? Math.round((ok / finished.length) * 100) : null };
  });

  private inflight: Promise<void> | null = null;

  refresh(): Promise<void> {
    if (!this.inflight) {
      this.inflight = this.load().finally(() => (this.inflight = null));
    }
    return this.inflight;
  }

  /** Lightweight refresh of the approval counters only (sidebar badge). */
  async refreshInbox(): Promise<void> {
    await Promise.allSettled([this.loadEffects(), this.loadPaper()]);
  }

  async runFlow(id: number): Promise<void> {
    await getJSON(`/api/admin/flows/${id}/run`, { method: 'POST' });
    setTimeout(() => void this.refresh(), 2500);
  }

  async decidePaper(key: string, approve: boolean, reason = ''): Promise<void> {
    const path = `/api/paper-approvals/${encodeURIComponent(key)}/${approve ? 'approve' : 'reject'}`;
    await getJSON(path, { method: 'POST', body: approve ? undefined : JSON.stringify({ reason }) });
    await this.loadPaper();
  }

  private async load(): Promise<void> {
    this.loading.set(true);
    this.error.set('');
    try {
      await Promise.allSettled([this.loadFlowsAndRuns(), this.loadEffects(), this.loadPaper()]);
    } finally {
      this.loading.set(false);
      this.loaded.set(true);
    }
  }

  private async loadFlowsAndRuns(): Promise<void> {
    if (!this.access.flowsRead()) return;
    try {
      const data = await getJSON<{ flows?: FlowSummary[] }>('/api/admin/flows');
      const flows = data.flows ?? [];
      this.flows.set(flows);
      const sample = [...flows]
        .sort((a, b) => Number(b.enabled) - Number(a.enabled) || Date.parse(b.updated_at ?? '') - Date.parse(a.updated_at ?? '') || b.id - a.id)
        .slice(0, FEED_FLOWS);
      const results = await Promise.allSettled(
        sample.map(f => getJSON<{ runs?: Omit<RunSummary, 'flowId' | 'flowName'>[] }>(`/api/admin/flows/${f.id}/runs`).then(d => ({ flow: f, runs: d.runs ?? [] }))),
      );
      const runs: RunSummary[] = [];
      for (const r of results) {
        if (r.status !== 'fulfilled') continue;
        for (const run of r.value.runs.slice(0, 10)) runs.push({ ...run, flowId: r.value.flow.id, flowName: r.value.flow.name });
      }
      runs.sort((a, b) => runTime(b) - runTime(a));
      this.runs.set(runs.slice(0, 40));
    } catch (e) {
      this.error.set((e as Error).message);
    }
  }

  private async loadEffects(): Promise<void> {
    if (!this.access.flowsRead()) return;
    try {
      const data = await getJSON<{ side_effects?: { status: string }[] }>('/api/admin/flow-side-effects?status=PENDING');
      this.pendingEffects.set((data.side_effects ?? []).filter(e => e.status === 'PENDING').length);
    } catch {
      this.pendingEffects.set(0);
    }
  }

  private async loadPaper(): Promise<void> {
    if (!this.access.paperApprovals()) return;
    try {
      const data = await getJSON<{ approvals?: PaperApproval[] }>('/api/paper-approvals?status=pending&limit=50');
      this.paperPending.set(data.approvals ?? []);
    } catch {
      this.paperPending.set([]);
    }
  }
}
