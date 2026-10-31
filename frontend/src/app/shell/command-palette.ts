import { ChangeDetectionStrategy, Component, ElementRef, Injectable, computed, effect, inject, signal, viewChild } from '@angular/core';
import { INVESTOR_PLAYBOOKS } from '../components/investor-nodes';
import { Router } from '@angular/router';

import { AuthState } from '../auth.service';
import { Access } from '../core/access';
import { ActivityStore } from '../core/activity';
import { AgentSession } from '../core/agent-session';
import { MarketState } from '../core/market-state';
import { NotifyService } from '../core/notify.service';
import { ThemeService } from '../theme.service';

@Injectable({ providedIn: 'root' })
export class CommandPaletteService {
  readonly isOpen = signal(false);
  open(): void { this.isOpen.set(true); }
  close(): void { this.isOpen.set(false); }
  toggle(): void { this.isOpen.update(v => !v); }
}

interface Command {
  id: string;
  group: string;
  label: string;
  hint?: string;
  icon: string;
  keywords?: string;
  run: () => void;
}

export const FLOW_TEMPLATES = [
  ...INVESTOR_PLAYBOOKS.map(p => ({ id: p.id, label: p.label, description: p.description, icon: p.icon })),
  { id: 'research', label: 'Market research report', description: 'An agent researches a company and returns a scored JSON report.', icon: 'bi-file-earmark-bar-graph' },
  { id: 'risk', label: 'Risk-score decision', description: 'Score risk, then branch to “review required” or “low risk”.', icon: 'bi-signpost-split' },
  { id: 'event', label: 'Event-driven analysis', description: 'Run research whenever a custom event such as research.requested fires.', icon: 'bi-lightning-charge' },
];

/** Lower is better; -1 means no match. Substring beats subsequence. */
function score(haystack: string, needle: string): number {
  if (!needle) return 0;
  const h = haystack.toLowerCase();
  const idx = h.indexOf(needle);
  if (idx >= 0) return idx;
  let pos = 0;
  for (const ch of needle) {
    pos = h.indexOf(ch, pos);
    if (pos < 0) return -1;
    pos++;
  }
  return 100 + pos;
}

@Component({
  selector: 'fs-command-palette',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <dialog #dialog class="palette" aria-label="Command palette" (close)="svc.close()" (click)="onBackdrop($event)">
      <div class="palette-search">
        <i class="bi bi-search" aria-hidden="true"></i>
        <input #input type="text" [value]="query()" (input)="query.set($any($event.target).value); active.set(0)" (keydown)="onKeydown($event)"
               placeholder="Ask the agent, jump to a page, run a flow…" aria-label="Command" role="combobox" aria-expanded="true"
               aria-controls="palette-list" [attr.aria-activedescendant]="visible().length ? 'palette-opt-' + active() : null" autocomplete="off">
        <span class="fs-kbd">Esc</span>
      </div>
      <ul id="palette-list" class="palette-list" role="listbox" aria-label="Commands">
        @for (cmd of visible(); track cmd.id; let i = $index) {
          @if (i === 0 || visible()[i - 1].group !== cmd.group) {
            <li class="palette-group" role="presentation">{{ cmd.group }}</li>
          }
          <li role="option" [id]="'palette-opt-' + i" [attr.aria-selected]="i === active()" [class.active]="i === active()"
              (mouseenter)="active.set(i)" (click)="execute(cmd)">
            <i class="bi" [class]="cmd.icon" aria-hidden="true"></i>
            <span class="label">{{ cmd.label }}</span>
            @if (cmd.hint) { <span class="hint">{{ cmd.hint }}</span> }
          </li>
        } @empty {
          <li class="palette-empty" role="presentation">No matching commands.</li>
        }
      </ul>
      <div class="palette-foot"><span><span class="fs-kbd">↑</span> <span class="fs-kbd">↓</span> navigate</span><span><span class="fs-kbd">Enter</span> run</span><span><span class="fs-kbd">Ctrl K</span> toggle</span></div>
    </dialog>
  `,
  styles: [`
    .palette {
      width: min(640px, calc(100vw - 1.5rem)); max-height: min(560px, calc(100dvh - 4rem)); margin: 10vh auto auto; padding: 0;
      border: 1px solid var(--fs-border); border-radius: var(--bs-border-radius-xl); background: var(--fs-surface); color: var(--fs-text);
      box-shadow: var(--fs-shadow-lg); overflow: hidden;
    }
    .palette[open] { display: flex; flex-direction: column; }
    .palette::backdrop { background: rgba(2, 6, 23, .5); backdrop-filter: blur(3px); }
    .palette-search { display: flex; align-items: center; gap: .7rem; padding: .9rem 1rem; border-bottom: 1px solid var(--fs-border); }
    .palette-search .bi { color: var(--fs-muted); font-size: 1.05rem; }
    .palette-search input { flex: 1; border: 0; outline: 0; background: transparent; color: var(--fs-text); font-size: 1rem; min-width: 0; }
    .palette-list { list-style: none; margin: 0; padding: .4rem; overflow-y: auto; flex: 1 1 auto; }
    .palette-group { padding: .6rem .7rem .3rem; font-size: .68rem; font-weight: 600; letter-spacing: .08em; text-transform: uppercase; color: var(--fs-muted); }
    .palette-list li[role=option] { display: flex; align-items: center; gap: .7rem; padding: .55rem .7rem; border-radius: var(--bs-border-radius); cursor: pointer; font-size: .9rem; }
    .palette-list li[role=option] .bi { color: var(--fs-muted); width: 1.1rem; text-align: center; }
    .palette-list li.active { background: var(--fs-accent-soft); }
    .palette-list li.active .bi { color: var(--fs-accent-text); }
    .label { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .hint { font-size: .75rem; color: var(--fs-muted); white-space: nowrap; }
    .palette-empty { padding: 1.5rem; text-align: center; color: var(--fs-muted); }
    .palette-foot { display: flex; gap: 1rem; padding: .55rem 1rem; border-top: 1px solid var(--fs-border); font-size: .72rem; color: var(--fs-muted); flex-wrap: wrap; }
  `],
})
export class CommandPaletteComponent {
  readonly svc = inject(CommandPaletteService);
  private readonly router = inject(Router);
  private readonly access = inject(Access);
  private readonly activity = inject(ActivityStore);
  private readonly agent = inject(AgentSession);
  private readonly market = inject(MarketState);
  private readonly theme = inject(ThemeService);
  private readonly auth = inject(AuthState);
  private readonly notify = inject(NotifyService);
  private readonly dialog = viewChild.required<ElementRef<HTMLDialogElement>>('dialog');
  private readonly input = viewChild.required<ElementRef<HTMLInputElement>>('input');

  readonly query = signal('');
  readonly active = signal(0);

  private readonly commands = computed<Command[]>(() => {
    const go = (path: string, query?: Record<string, string>) => () => void this.router.navigate([path], { queryParams: query });
    const list: Command[] = [];
    const pages: [string, string, string, boolean][] = [
      ['/', 'Command Center', 'bi-grid-1x2', true],
      ['/agent', 'Research Agent', 'bi-stars', true],
      ['/flows', 'Flows', 'bi-diagram-3', this.access.flowsRead()],
      ['/inbox', 'Inbox · approvals', 'bi-inbox', this.access.inbox()],
      ['/markets', 'Markets', 'bi-graph-up-arrow', true],
      ['/financials', 'Financials', 'bi-bank', true],
      ['/portfolio', 'Paper portfolio', 'bi-wallet2', this.access.liveMarkets()],
      ['/workspace', 'Workspace files', 'bi-folder2-open', this.access.workspace()],
      ['/community', 'Community', 'bi-chat-square-text', this.access.liveMarkets()],
      ['/notifications', 'Notifications', 'bi-bell', this.access.notifications()],
      ['/settings', 'Settings', 'bi-gear', this.access.settings()],
    ];
    for (const [path, label, icon, ok] of pages) if (ok) list.push({ id: 'nav' + path, group: 'Go to', label, icon, run: go(path) });

    if (this.access.flowsRead()) {
      for (const f of this.activity.flows()) {
        list.push({ id: 'open-flow-' + f.id, group: 'Flows', label: `Open ${f.name}`, icon: 'bi-diagram-3', hint: f.enabled ? 'enabled' : 'draft', run: go('/flows', { flow: String(f.id) }) });
        if (this.access.flowsRun()) {
          list.push({
            id: 'run-flow-' + f.id, group: 'Flows', label: `Run ${f.name}`, icon: 'bi-play-circle', keywords: 'execute start',
            run: () => this.activity.runFlow(f.id).then(() => this.notify.success(`${f.name} queued.`, 'Flow run started'), e => this.notify.error(String(e), 'Could not start run')),
          });
        }
      }
      if (this.access.flowsWrite()) {
        for (const t of FLOW_TEMPLATES) {
          list.push({ id: 'tpl-' + t.id, group: 'Create', label: `New flow: ${t.label}`, icon: t.icon, keywords: 'template automation', run: go('/flows', { template: t.id }) });
        }
      }
    }
    list.push(
      { id: 'agent-new', group: 'Actions', label: 'New agent conversation', icon: 'bi-chat-left-dots', run: () => { this.agent.clear(); this.agent.openDock(); } },
      { id: 'theme', group: 'Actions', label: this.theme.theme() === 'dark' ? 'Switch to light theme' : 'Switch to dark theme', icon: 'bi-circle-half', run: () => this.theme.toggle() },
    );
    if (this.auth.enabled() && this.auth.user()) list.push({ id: 'logout', group: 'Actions', label: 'Sign out', icon: 'bi-box-arrow-right', run: () => void this.auth.logout() });
    return list;
  });

  readonly visible = computed<Command[]>(() => {
    const raw = this.query().trim();
    const q = raw.toLowerCase();
    const dynamic: Command[] = [];
    if (raw) {
      dynamic.push({ id: 'ask', group: 'Agent', label: `Ask the agent: “${raw}”`, icon: 'bi-stars', run: () => this.agent.ask(raw) });
      if (/^[A-Za-z0-9.\-^=]{1,15}$/.test(raw)) {
        dynamic.push({
          id: 'track', group: 'Agent', label: `Open ${raw.toUpperCase()} in Markets`, hint: this.market.provider(), icon: 'bi-graph-up',
          run: () => { this.market.track(raw); void this.router.navigateByUrl('/markets'); },
        });
      }
    }
    const scored = this.commands()
      .map(c => ({ c, s: score(`${c.label} ${c.keywords ?? ''} ${c.group}`, q) }))
      .filter(x => x.s >= 0);
    if (q) scored.sort((a, b) => a.s - b.s);
    const matched = scored.map(x => x.c).slice(0, 40);
    // A query that names a command directly runs it on Enter; anything else
    // (a research question) goes to the agent first.
    const direct = q && scored.some(x => x.s < 100);
    const all = direct ? [...matched, ...dynamic] : [...dynamic, ...matched];
    // Keep the groups contiguous so headers render once.
    const order: string[] = [];
    for (const c of all) if (!order.includes(c.group)) order.push(c.group);
    return order.flatMap(g => all.filter(c => c.group === g));
  });

  constructor() {
    effect(() => {
      const el = this.dialog().nativeElement;
      if (this.svc.isOpen() && !el.open) {
        this.query.set('');
        this.active.set(0);
        if (this.access.flowsRead() && !this.activity.loaded()) void this.activity.refresh();
        el.showModal();
        queueMicrotask(() => this.input().nativeElement.focus());
      } else if (!this.svc.isOpen() && el.open) {
        el.close();
      }
    });
  }

  onKeydown(event: KeyboardEvent): void {
    const n = this.visible().length;
    if (event.key === 'ArrowDown') {
      event.preventDefault();
      if (n) this.active.set((this.active() + 1) % n);
    } else if (event.key === 'ArrowUp') {
      event.preventDefault();
      if (n) this.active.set((this.active() - 1 + n) % n);
    } else if (event.key === 'Enter') {
      event.preventDefault();
      const cmd = this.visible()[this.active()];
      if (cmd) this.execute(cmd);
    }
  }

  execute(cmd: Command): void {
    this.svc.close();
    cmd.run();
  }

  onBackdrop(event: MouseEvent): void {
    if (event.target === this.dialog().nativeElement) this.svc.close();
  }
}
