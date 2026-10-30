import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { DatePipe } from '@angular/common';
import { RouterLink } from '@angular/router';

import { Access } from '../core/access';
import { ActivityStore, type PaperApproval } from '../core/activity';
import { NotifyService } from '../core/notify.service';
import { FlowSideEffectsComponent } from '../components/flow-side-effects';
import { timeAgo } from '../ui/format';

interface ProposalView {
  approval: PaperApproval;
  symbol: string;
  side: string;
  quantity: string;
  type: string;
  price: string;
  flowRun: number | null;
}

/**
 * Inbox: every decision the agents are waiting on. Flow side effects that
 * require approval, and paper-trade proposals that need a second SYS_ADMIN.
 */
@Component({
  selector: 'fs-inbox-page',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [FlowSideEffectsComponent, RouterLink, DatePipe],
  template: `
    <div class="fs-page">
      <header class="fs-page-header">
        <div>
          <div class="fs-eyebrow">Human in the loop</div>
          <h1>Inbox</h1>
          <p>Agents pause here before anything changes. Review the exact content, then approve or reject.</p>
        </div>
        <button type="button" class="btn btn-outline-secondary" (click)="refresh()" [disabled]="activity.loading()">
          <i class="bi bi-arrow-clockwise" [class.spin]="activity.loading()" aria-hidden="true"></i>Refresh
        </button>
      </header>

      <nav class="fs-segmented mb-3" role="tablist" aria-label="Inbox sections">
        @if (access.flowsRead()) {
          <button type="button" role="tab" [attr.aria-selected]="tab() === 'flows'" (click)="tab.set('flows')">
            Flow approvals @if (activity.pendingEffects()) { <span class="badge rounded-pill text-bg-warning ms-1">{{ activity.pendingEffects() }}</span> }
          </button>
        }
        @if (access.paperApprovals()) {
          <button type="button" role="tab" [attr.aria-selected]="tab() === 'paper'" (click)="tab.set('paper')">
            Paper trade proposals @if (proposals().length) { <span class="badge rounded-pill text-bg-warning ms-1">{{ proposals().length }}</span> }
          </button>
        }
      </nav>

      @if (tab() === 'flows' && access.flowsRead()) {
        <section class="card"><div class="card-body"><fse-flow-side-effects /></div></section>
      }

      @if (tab() === 'paper' && access.paperApprovals()) {
        <p class="text-muted small">Proposals come from analysts or flows and carry a rationale. A different SYS_ADMIN than the requester must approve before the paper engine accepts the order.</p>
        @if (!proposals().length) {
          <div class="fs-empty"><i class="bi bi-check2-circle"></i><h3>No pending proposals</h3><p class="mb-0">Paper trade proposals waiting for approval appear here.</p></div>
        }
        <div class="proposals">
          @for (p of proposals(); track p.approval.client_order_id) {
            <article class="card proposal">
              <div class="card-body">
                <div class="proposal-head">
                  <span class="side" [class.buy]="p.side === 'BUY'" [class.sell]="p.side === 'SELL'">{{ p.side || '—' }}</span>
                  <div class="min-w-0">
                    <h2>{{ p.quantity }} {{ p.symbol }} <span class="text-muted fw-normal">· {{ p.type }}@if (p.price) { @ {{ p.price }} }</span></h2>
                    <div class="small text-muted">
                      <code>{{ p.approval.client_order_id }}</code> · requested {{ timeAgo(p.approval.created_at) }}
                      @if (p.approval.expires_at) { · expires {{ p.approval.expires_at | date: 'MMM d, HH:mm' }} }
                    </div>
                  </div>
                  <span class="fs-status is-warning ms-auto">Pending</span>
                </div>
                @if (p.approval.rationale) {
                  <blockquote class="rationale"><i class="bi bi-quote" aria-hidden="true"></i>{{ p.approval.rationale }}</blockquote>
                }
                <div class="small text-muted mb-3">
                  Source: {{ p.approval.decision_source || 'manual' }}
                  @if (p.flowRun) { · <a routerLink="/flows" [queryParams]="{ tab: 'runs' }">see flow run #{{ p.flowRun }}</a> }
                </div>
                <div class="proposal-actions">
                  <input class="form-control form-control-sm" type="text" maxlength="500" placeholder="Reason (required to reject)" [attr.aria-label]="'Rejection reason for ' + p.approval.client_order_id"
                         [value]="reasons()[p.approval.client_order_id] || ''" (input)="setReason(p.approval.client_order_id, $any($event.target).value)">
                  <button type="button" class="btn btn-sm btn-outline-danger" [disabled]="busy()" (click)="decide(p, false)">Reject</button>
                  <button type="button" class="btn btn-sm btn-primary" [disabled]="busy()" (click)="decide(p, true)"><i class="bi bi-check2" aria-hidden="true"></i>Approve</button>
                </div>
              </div>
            </article>
          }
        </div>
      }
    </div>
  `,
  styles: [`
    .proposals { display: grid; gap: .85rem; max-width: 900px; }
    .proposal-head { display: flex; gap: .85rem; align-items: center; margin-bottom: .75rem; }
    .proposal-head h2 { font-size: 1rem; margin: 0; }
    .side { flex: 0 0 auto; font-size: .72rem; font-weight: 700; padding: .3rem .55rem; border-radius: 6px; background: var(--fs-surface-3); }
    .side.buy { background: var(--fs-pos-soft); color: var(--fs-pos); }
    .side.sell { background: var(--fs-neg-soft); color: var(--fs-neg); }
    .rationale { margin: 0 0 .6rem; padding: .6rem .8rem; border-left: 3px solid var(--fs-accent); background: var(--fs-accent-soft); border-radius: 0 6px 6px 0; font-size: .9rem; white-space: pre-wrap; overflow-wrap: anywhere; }
    .rationale .bi { margin-right: .35rem; color: var(--fs-accent-text); }
    .proposal-actions { display: flex; gap: .5rem; align-items: center; flex-wrap: wrap; }
    .proposal-actions input { flex: 1 1 220px; }
  `],
})
export class InboxPage {
  readonly access = inject(Access);
  readonly activity = inject(ActivityStore);
  private readonly notify = inject(NotifyService);

  readonly tab = signal<'flows' | 'paper'>(this.access.flowsRead() ? 'flows' : 'paper');
  readonly busy = signal(false);
  readonly reasons = signal<Record<string, string>>({});
  readonly timeAgo = timeAgo;

  readonly proposals = computed<ProposalView[]>(() =>
    this.activity.paperPending().map(approval => {
      let payload: Record<string, unknown> = {};
      try {
        payload = JSON.parse(approval.request_payload || '{}');
      } catch {}
      const str = (v: unknown) => (v === undefined || v === null ? '' : String(v));
      const run = /^flow-run:(\d+)$/.exec(approval.decision_source ?? '');
      return {
        approval,
        symbol: str(payload['symbol']).toUpperCase(),
        side: str(payload['side']).toUpperCase(),
        quantity: str(payload['quantity'] ?? payload['qty']),
        type: str(payload['type'] || 'MARKET').toUpperCase(),
        price: str(payload['limit_price'] ?? payload['price'] ?? ''),
        flowRun: run ? Number(run[1]) : null,
      };
    }),
  );

  constructor() {
    void this.activity.refresh();
  }

  refresh(): void {
    void this.activity.refresh();
  }

  setReason(key: string, value: string): void {
    this.reasons.update(r => ({ ...r, [key]: value }));
  }

  async decide(p: ProposalView, approve: boolean): Promise<void> {
    const key = p.approval.client_order_id;
    const reason = (this.reasons()[key] || '').trim();
    if (!approve && !reason) {
      this.notify.error('Add a reason before rejecting a proposal.');
      return;
    }
    if (approve) {
      const ok = await this.notify.confirm({
        title: 'Approve paper order?',
        message: `${p.side} ${p.quantity} ${p.symbol} (${p.type}). The paper engine can then accept this exact order once.`,
        confirmLabel: 'Approve',
      });
      if (!ok) return;
    }
    this.busy.set(true);
    try {
      await this.activity.decidePaper(key, approve, reason);
      this.notify.success(approve ? 'Proposal approved.' : 'Proposal rejected.');
    } catch (e) {
      this.notify.error((e as Error).message);
    } finally {
      this.busy.set(false);
    }
  }
}
