import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { RouterLink } from '@angular/router';

import { AgentChatComponent } from '../agent/agent-chat';
import { Access } from '../core/access';
import { AgentSession } from '../core/agent-session';
import { toolLabel } from '../components/agent-tools';

/** Full-page research agent with its tools and current context. */
@Component({
  selector: 'fs-agent-page',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [AgentChatComponent, RouterLink],
  template: `
    <div class="agent-page">
      <section class="agent-main" aria-label="Research agent conversation">
        <header class="agent-head">
          <span class="fs-agent-orb" aria-hidden="true"><i class="bi bi-stars"></i></span>
          <div class="min-w-0">
            <h1>Research Agent</h1>
            <p>{{ session.model() || 'Model not configured' }} · up to {{ session.maxSteps() }} reasoning rounds and {{ session.maxToolCalls() }} tool calls</p>
          </div>
          <div class="ms-auto d-flex gap-2">
            @if (session.canConfigure() && session.catalog().length) {
              <select class="form-select form-select-sm model-pick" aria-label="Model for this conversation" [value]="session.chatModel()" (change)="session.chatModel.set($any($event.target).value)">
                <option [value]="session.model()">{{ session.model() || 'System default' }}</option>
                @for (m of session.catalog(); track m.id) { @if (m.model !== session.model()) { <option [value]="m.model">{{ m.label }}</option> } }
              </select>
            }
            <button type="button" class="btn btn-sm btn-outline-secondary" (click)="session.clear()" [disabled]="!session.entries().length"><i class="bi bi-plus-lg" aria-hidden="true"></i>New chat</button>
          </div>
        </header>
        <fs-agent-chat variant="page" />
      </section>

      <aside class="agent-side" aria-label="Agent context">
        <section class="side-block">
          <h2>Context</h2>
          @if (session.context().symbol) {
            <dl class="ctx">
              <div><dt>Instrument</dt><dd>{{ session.context().symbol }}</dd></div>
              <div><dt>Provider</dt><dd>{{ session.context().provider }}</dd></div>
              <div><dt>Range</dt><dd>{{ session.context().range }}</dd></div>
              @if (session.context().price) { <div><dt>Last price</dt><dd class="tabular">{{ session.context().price }}</dd></div> }
            </dl>
          } @else {
            <p class="text-muted small mb-0">No instrument selected. <a routerLink="/markets">Pick one in Markets</a> to give the agent context.</p>
          }
        </section>
        <section class="side-block">
          <h2>Tools <span class="badge text-bg-secondary ms-1">{{ session.tools().length }}</span></h2>
          @if (session.tools().length) {
            <ul class="tools">
              @for (t of session.tools(); track t) { <li><i class="bi bi-wrench-adjustable" aria-hidden="true"></i>{{ toolLabel(t) }}</li> }
            </ul>
          } @else {
            <p class="text-muted small mb-0">The agent answers from its own knowledge. No tools are enabled for your role.</p>
          }
        </section>
        <section class="side-block">
          <h2>Make it repeatable</h2>
          <p class="text-muted small">Turn a good question into an agent flow that runs on a schedule or an event, with approvals and a full audit trail.</p>
          @if (access.flowsWrite()) {
            <a class="btn btn-sm btn-outline-secondary w-100" routerLink="/flows" [queryParams]="{ template: 'research' }"><i class="bi bi-diagram-3" aria-hidden="true"></i>Create a research flow</a>
          }
        </section>
      </aside>
    </div>
  `,
  styles: [`
    :host { display: block; }
    .agent-page { display: grid; grid-template-columns: minmax(0, 1fr) 300px; height: calc(100dvh - var(--fs-topbar-height)); min-height: 480px; }
    .agent-main { display: flex; flex-direction: column; min-height: 0; min-width: 0; border-right: 1px solid var(--fs-border); }
    .agent-head { display: flex; align-items: center; gap: .8rem; flex-wrap: wrap; padding: 1rem clamp(1rem, 4vw, 3rem); border-bottom: 1px solid var(--fs-border); }
    .agent-head h1 { font-size: 1.15rem; margin: 0; }
    .agent-head p { margin: 0; font-size: .78rem; color: var(--fs-muted); }
    .model-pick { width: auto; max-width: 220px; }
    fs-agent-chat { flex: 1 1 auto; min-height: 0; }
    .agent-side { overflow-y: auto; padding: 1.25rem; display: grid; align-content: start; gap: 1.5rem; background: var(--fs-surface-2); }
    .side-block h2 { font-size: .72rem; text-transform: uppercase; letter-spacing: .08em; color: var(--fs-muted); margin: 0 0 .6rem; display: flex; align-items: center; }
    .ctx { display: grid; gap: .45rem; margin: 0; font-size: .85rem; }
    .ctx div { display: flex; justify-content: space-between; gap: 1rem; }
    .ctx dt { color: var(--fs-muted); font-weight: 400; }
    .ctx dd { margin: 0; font-weight: 600; }
    .tools { list-style: none; padding: 0; margin: 0; display: grid; gap: .35rem; font-size: .82rem; }
    .tools li { display: flex; gap: .5rem; align-items: center; color: var(--fs-text-2); }
    .tools .bi { color: var(--fs-muted); }
    @media (max-width: 1099.98px) {
      .agent-page { grid-template-columns: minmax(0, 1fr); }
      .agent-main { border-right: 0; }
      .agent-side { display: none; }
    }
  `],
})
export class AgentPage {
  readonly session = inject(AgentSession);
  readonly access = inject(Access);
  readonly toolLabel = toolLabel;

  constructor() {
    // The page already shows the conversation; the dock would duplicate it.
    this.session.closeDock();
  }
}
