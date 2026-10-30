import { ChangeDetectionStrategy, Component, effect, inject, viewChild } from '@angular/core';
import { Router } from '@angular/router';

import { AgentChatComponent } from '../agent/agent-chat';
import { AgentSession } from '../core/agent-session';

/** Slide-in research agent available on every page. */
@Component({
  selector: 'fs-agent-dock',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [AgentChatComponent],
  template: `
    <aside id="fs-agent-dock" class="dock" [class.open]="session.dockOpen()" aria-label="Research agent" [attr.aria-hidden]="!session.dockOpen()"
           [attr.inert]="session.dockOpen() ? null : ''">
      <header class="dock-head">
        <span class="fs-agent-orb" aria-hidden="true"><i class="bi bi-stars"></i></span>
        <div class="dock-title">
          <h2>Research Agent</h2>
          @if (session.canConfigure() && session.catalog().length) {
            <select class="model-select" aria-label="Model for this conversation" [value]="session.chatModel()" (change)="session.chatModel.set($any($event.target).value)">
              <option [value]="session.model()">{{ session.model() || 'System default' }}</option>
              @for (m of session.catalog(); track m.id) { @if (m.model !== session.model()) { <option [value]="m.model">{{ m.label }}</option> } }
            </select>
          } @else {
            <small>{{ session.model() || 'Model not configured' }}</small>
          }
        </div>
        <div class="dock-actions">
          <button type="button" class="btn btn-ghost btn-icon btn-sm" (click)="session.clear()" [disabled]="!session.entries().length" aria-label="New conversation" title="New conversation"><i class="bi bi-plus-lg"></i></button>
          <button type="button" class="btn btn-ghost btn-icon btn-sm" (click)="expand()" aria-label="Open full-page agent" title="Open full page"><i class="bi bi-arrows-angle-expand"></i></button>
          <button type="button" class="btn btn-ghost btn-icon btn-sm" (click)="session.closeDock()" aria-label="Close agent" title="Close (Esc)"><i class="bi bi-x-lg"></i></button>
        </div>
      </header>
      @if (session.dockOpen()) {
        <fs-agent-chat variant="dock" />
      }
    </aside>
  `,
  styles: [`
    .dock {
      position: fixed; top: 0; right: 0; bottom: 0; z-index: 1045; width: var(--fs-dock-width); max-width: 100vw;
      display: flex; flex-direction: column; background: var(--fs-surface); border-left: 1px solid var(--fs-border);
      box-shadow: var(--fs-shadow-lg); transform: translateX(105%); transition: transform .22s ease; visibility: hidden;
    }
    .dock.open { transform: none; visibility: visible; }
    .dock-head { display: flex; align-items: center; gap: .65rem; height: var(--fs-topbar-height); padding: 0 .75rem 0 1rem; border-bottom: 1px solid var(--fs-border); flex: 0 0 auto; }
    .dock-title { display: grid; min-width: 0; line-height: 1.2; }
    .dock-title h2 { font-size: .95rem; margin: 0; }
    .dock-title small { color: var(--fs-muted); font-size: .72rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .model-select { border: 0; background: transparent; color: var(--fs-muted); font-size: .72rem; padding: 0; max-width: 220px; outline: 0; }
    .model-select:focus-visible { box-shadow: var(--fs-focus); border-radius: 4px; }
    .model-select option { background: var(--fs-surface); color: var(--fs-text); }
    .dock-actions { margin-left: auto; display: flex; gap: .15rem; }
    fs-agent-chat { flex: 1 1 auto; min-height: 0; }
    @media (min-width: 1440px) { .dock { box-shadow: none; } }
    @media (max-width: 575.98px) { .dock { width: 100vw; } }
  `],
  host: { '(document:keydown.escape)': 'onEscape()' },
})
export class AgentDockComponent {
  readonly session = inject(AgentSession);
  private readonly router = inject(Router);
  private readonly chat = viewChild(AgentChatComponent);

  constructor() {
    effect(() => {
      if (this.session.dockOpen()) queueMicrotask(() => this.chat()?.focus());
    });
  }

  expand(): void {
    this.session.closeDock();
    void this.router.navigateByUrl('/agent');
  }

  onEscape(): void {
    if (this.session.dockOpen() && !document.querySelector('dialog[open]')) this.session.closeDock();
  }
}
