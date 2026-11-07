import { NgTemplateOutlet } from '@angular/common';
import { HelpButtonComponent } from '../ui/help-button';
import { RouterLink } from '@angular/router';
import { ChangeDetectionStrategy, Component, computed, effect, inject, signal, untracked } from '@angular/core';
import { DomSanitizer, type SafeHtml } from '@angular/platform-browser';

import { toolLabel } from '../components/agent-tools';
import { KIND_LABEL, changeLabel, stalenessNote, summaryLine, usdCompact } from '../components/portfolio-view';
import { EVIDENCE_MODES, findingLabel } from '../core/answer-sources';
import { buildTeamExport, teamToJson, teamToMarkdown } from '../core/answer-export';
import { STANCE_ICON, STANCE_LABEL, defaultQuestion, evidenceLine, shownText, stanceCounts, togglePersona, type MemberState } from '../components/team-view';
import { defaultOptionLabel, profileOption, profileSummary } from '../components/profile-view';
import { budgetLine } from '../components/team-budget';
import { MarketState } from '../core/market-state';
import { TeamSession } from '../core/team-session';
import { renderMarkdown } from '../markdown';
import { ToolCallComponent } from '../components/tool-call';
import { ProgressStatusComponent } from '../ui/progress-status';
import { SymbolSearchComponent } from '../shell/symbol-search';

/**
 * Agent team: several investor characters answer one question side by side,
 * each in its own style, and a neutral chair compares them. The characters
 * are fictional; they do research, give no advice and place no orders.
 */
@Component({
  selector: 'fs-team-page',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [HelpButtonComponent, SymbolSearchComponent, NgTemplateOutlet, RouterLink, ToolCallComponent, ProgressStatusComponent],
  template: `
<div class="fs-page research-container">
  <header class="fs-page-header">
    <div>
      <div class="fs-eyebrow">Research agent</div>
      <h1>Agent team <fs-help-button topic="agent-team" /> @if (market.trackedSymbol()) { <span class="text-muted fw-normal">· {{ market.trackedSymbol() }}</span> }</h1>
      <p>Pick investor characters, ask one question, and read how each of them would look at it. They are fictional and work from real data; they give research, not advice.</p>
    </div>
    <fs-symbol-search class="page-search" [navigate]="false" placeholder="Ask about a company…" />
  </header>

  <section class="card panel" aria-label="Choose the team">
    <h2>Team <small>{{ team.selected().length }} of {{ team.maxTeam() }}</small></h2>
    <div class="cast">
      @for (p of team.personas(); track p.id) {
        <button type="button" class="member" [attr.aria-pressed]="team.selected().includes(p.id)" [disabled]="team.running()" (click)="toggle(p.id)">
          <strong>{{ p.name }}</strong>
          <span class="arch">{{ p.archetype }}</span>
          <small>{{ p.tagline }} · {{ p.horizon }}</small>
        </button>
      }
    </div>
    <div class="profile-links" role="group" aria-label="Character profiles">
      @for (p of team.personas(); track p.id) {
        <button type="button" class="btn btn-sm btn-ghost" [attr.aria-expanded]="viewing() === p.id" (click)="view(p.id)"><i class="bi bi-person-lines-fill"></i> {{ p.name }}: profile and portfolio</button>
      }
    </div>
    @if (profile(); as p) {
      <article class="profile" [attr.aria-label]="p.name + ' profile'">
        <h3>{{ p.name }} <small>a fictional character · {{ p.archetype }}</small></h3>
        <div class="cols">
          <section>
            <h4>How it decides</h4>
            <ol>@for (x of p.process ?? []; track $index) { <li>{{ x }}</li> }</ol>
            <h4>How it builds a portfolio</h4>
            <ul>@for (x of p.sizing ?? []; track $index) { <li>{{ x }}</li> }</ul>
          </section>
          <section>
            @if (p.portfolio; as ref) {
              <h4>Reference portfolio <small>{{ kindLabel(ref.kind) }}</small></h4>
              <p class="muted">{{ summary(ref) }}</p>
              @if (ref.kind !== 'rules') {
                <table>
                  <caption class="visually-hidden">Largest disclosed positions</caption>
                  <thead><tr><th scope="col">Position</th><th scope="col" class="num">Weight</th><th scope="col" class="num">Change in shares</th></tr></thead>
                  <tbody>
                    @for (x of ref.positions.slice(0, 10); track $index) {
                      <tr><td>{{ x.symbol || x.name }} <small>{{ x.symbol ? x.name : '' }}</small></td><td class="num">{{ x.weight_pct.toFixed(1) }}%</td><td class="num">{{ change(x, ref.kind) }}</td></tr>
                    }
                  </tbody>
                </table>
                <p class="muted">{{ staleness(ref) }} Source: <a [href]="ref.source_url" target="_blank" rel="noopener noreferrer">{{ ref.form || 'published holdings' }}</a>@if (ref.total_value_usd) { , {{ usd(ref.total_value_usd) }} in disclosed holdings}.</p>
                @if (ref.exited_since_previous?.length) { <p class="muted">Sold out of since the previous quarter: {{ ref.exited_since_previous!.join(', ') }}.</p> }
              } @else {
                <ul>@for (x of ref.rules ?? []; track $index) { <li>{{ x }}</li> }</ul>
              }
              @for (n of ref.notes; track $index) { <p class="caveat">{{ n }}</p> }
            }
            <h4>Documented in</h4>
            <ul class="src">
              @for (x of p.sources ?? []; track x.title) { <li>@if (x.url) { <a [href]="x.url" target="_blank" rel="noopener noreferrer">{{ x.title }}</a> } @else { {{ x.title }} }</li> }
            </ul>
          </section>
        </div>
        <p class="muted">A disclosure of past holdings is not a current position, a view or a recommendation. The character is invented; it follows the approach, not the person.</p>
      </article>
    }
    @if (team.chosen().length) {
      <details class="strat">
        <summary>Strategies the team will run</summary>
        <ul>
          @for (p of team.chosen(); track p.id) {
            @for (st of p.strategies ?? []; track st.id) {
              <li><strong>{{ p.name }}</strong> · {{ st.kind === 'rules' ? 'rules on one instrument' : 'ranking' }}:
                @if (st.kind === 'rules') { <a routerLink="/strategies" [queryParams]="{ strategy: st.id }">{{ st.id }}</a> } @else { <code>{{ st.id }}</code> }
                <span class="muted"> {{ st.why }}</span></li>
            }
          }
        </ul>
        <p class="muted">Each character runs its strategies with real data before it answers, and cites the trade count and warnings.</p>
      </details>
    }
    <section class="models" aria-label="Models and saved teams">
      @if (team.profiles().length && team.chosen().length) {
        <h3>Model per character</h3>
        <ul class="model-rows">
          @for (p of team.chosen(); track p.id) {
            <li>
              <label [for]="'mp-' + p.id">{{ p.name }}</label>
              <select [id]="'mp-' + p.id" class="form-select form-select-sm" [disabled]="team.running()"
                      (change)="team.setMemberProfile(p.id, $any($event.target).value ? +$any($event.target).value : null)">
                <option value="" [selected]="!team.profileOf()[p.id]">{{ defaultLabel() }}</option>
                @for (pr of team.profiles(); track pr.id) { <option [value]="pr.id" [selected]="team.profileOf()[p.id] === pr.id">{{ option(pr) }}</option> }
              </select>
              <small class="muted">{{ summaryFor(p.id) }}</small>
            </li>
          }
        </ul>
      }
      <h3>Budget for a run</h3>
      <div class="saved-row" role="group" aria-label="Budget for a run">
        <label class="budget-field">Tokens <input class="form-control form-control-sm" inputmode="numeric" placeholder="no ceiling" [value]="team.budget().tokens" [disabled]="team.running()" (input)="setBudget('tokens', $event)" /></label>
        <label class="budget-field">Cost (US$) <input class="form-control form-control-sm" inputmode="decimal" placeholder="no ceiling" [value]="team.budget().cost" [disabled]="team.running()" (input)="setBudget('cost', $event)" /></label>
        <label class="budget-field">Tool calls <input class="form-control form-control-sm" inputmode="numeric" placeholder="no ceiling" [value]="team.budget().calls" [disabled]="team.running()" (input)="setBudget('calls', $event)" /></label>
      </div>
      <p class="muted">The run stops with a message when a ceiling is reached; answers so far are kept. Tokens and cost are counted only where the model endpoint reports usage, and cost needs the prices in the model profile; otherwise they show as unknown and only the tool-call ceiling holds. A saved team keeps its ceilings.</p>
      <h3>Saved teams</h3>
      <div class="saved-row">
        @if (team.savedTeams().length) {
          <label class="visually-hidden" for="saved-pick">Saved team</label>
          <select id="saved-pick" class="form-select form-select-sm" (change)="picked.set($any($event.target).value ? +$any($event.target).value : null)">
            <option value="" [selected]="picked() === null">Choose a saved team…</option>
            @for (t of team.savedTeams(); track t.id) { <option [value]="t.id" [selected]="t.id === picked()">{{ t.name }} ({{ t.members.length }})</option> }
          </select>
          <button type="button" class="btn btn-sm btn-outline-secondary" [disabled]="picked() === null || team.running()" (click)="loadPicked()">Load</button>
          <button type="button" class="btn btn-sm btn-outline-secondary" [disabled]="picked() === null" (click)="deletePicked()">Delete</button>
        } @else { <span class="muted">You have no saved teams yet.</span> }
      </div>
      <div class="saved-row">
        <label class="visually-hidden" for="team-name">Name for this team</label>
        <input id="team-name" class="form-control form-control-sm" maxlength="80" placeholder="Name this team to save it" [value]="team.teamName()" (input)="team.teamName.set($any($event.target).value)" />
        <button type="button" class="btn btn-sm btn-outline-secondary" [disabled]="!team.teamName().trim() || !team.selected().length || team.running()" (click)="team.saveCurrent()">Save team</button>
      </div>
      @if (team.teamNotice()) { <p class="muted" role="status">{{ team.teamNotice() }}</p> }
    </section>
    <label class="q">Question
      <textarea class="form-control" rows="2" maxlength="4000" [value]="team.question()" (input)="team.question.set($any($event.target).value)" [attr.aria-label]="'Question for the team'"></textarea>
    </label>
    <div class="run-bar">
      <label class="check"><input type="checkbox" class="form-check-input" [checked]="team.chair()" (change)="team.chair.set($any($event.target).checked)" [disabled]="team.running() || team.selected().length < 2" /> Neutral chair compares the answers</label>
      @if (team.running()) {
        <button type="button" class="btn btn-outline-secondary" (click)="team.stop()"><i class="bi bi-stop-fill"></i> Stop</button>
      } @else {
        <button type="button" class="btn btn-primary" [disabled]="!team.question().trim() || !team.selected().length" (click)="team.run()"><i class="bi bi-people"></i> Ask the team</button>
      }
      <button type="button" class="btn btn-sm btn-ghost" (click)="suggest()">Suggest a question</button>
    </div>
    <div class="run-bar">
      <label for="team-evidence">Evidence</label>
      <select id="team-evidence" class="form-select form-select-sm" [disabled]="team.running()" (change)="team.evidence.set($any($event.target).value)">
        <option value="default" [selected]="team.evidence() === 'default'">My default (set in the chat)</option>
        @for (m of evidenceModes; track m.id) { <option [value]="m.id" [selected]="team.evidence() === m.id">{{ m.label }}</option> }
      </select>
    </div>
    @if (team.evidence() !== 'default' && team.evidence() !== 'standard') {
      <p class="muted">Every character's answer is held until its citations are checked. A mode picked here brings none of the limits of your saved default. The neutral chair has no sources of its own, so its summary is not citation checked.</p>
    }
    @if (team.error()) { <p class="err" role="alert">{{ team.error() }}</p> }
  </section>

  @if (team.state(); as s) {
    @if (counts().length) {
      <p class="tally" aria-label="How the team landed">
        @for (c of counts(); track c.stance) { <span [class]="'badge-stance ' + c.stance"><i [class]="'bi ' + icon(c.stance)"></i> {{ c.count }} {{ c.label }}</span> }
      </p>
    }
    @if (s.chair; as chair) {
      <article class="card panel chair" aria-label="Chair summary">
        <h2><i class="bi bi-clipboard-check"></i> Chair <small>compares the answers, adds no facts of its own</small></h2>
        <ng-container *ngTemplateOutlet="body; context: { m: chair }" />
      </article>
    }
    <div class="grid">
      @for (m of s.members; track m.id) {
        <article class="card panel" [attr.aria-label]="m.name">
          <h2>{{ m.name }}
            @if (m.model) { <small class="model-tag" title="The model this character answers with">{{ m.model }}</small> }
            @if (m.stance) { <span [class]="'badge-stance ' + m.stance"><i [class]="'bi ' + icon(m.stance)"></i> {{ stanceLabel(m.stance) }}</span> }
          </h2>
          <ng-container *ngTemplateOutlet="body; context: { m }" />
        </article>
      }
    </div>
    @if (s.budget; as b) { <p class="muted" role="status" aria-label="What the run used">{{ budgetText(b) }}</p> }
    @if (s.finished && s.members.length) {
      <p class="export-row">
        <button type="button" class="btn btn-sm btn-outline-secondary" (click)="exportRun('md')">Export Markdown</button>
        <button type="button" class="btn btn-sm btn-outline-secondary" (click)="exportRun('json')">Export JSON</button>
        <span class="muted">Every member's answer with its sources and citation check, the chair summary and the question. Times are UTC; links lose credentials.</span>
      </p>
    }
    <p class="muted">Each character only sees your question and its own tool results, never another's answer. Fictional characters; not investment advice.</p>
  }
</div>

<ng-template #body let-m="m">
  @if (m.steps.length) {
    <details class="steps"><summary>{{ m.steps.length }} tool {{ m.steps.length === 1 ? 'call' : 'calls' }}@if (failedCalls(m); as n) { <span class="bad"> · {{ n }} failed</span> }</summary>
      <ul>@for (st of m.steps; track st.callId) { <li><fs-tool-call [call]="st" /></li> }</ul></details>
  }
  @if (m.note) { <p class="muted">{{ m.note }}</p> }
  @if (m.thinking) {
    <details class="think" [open]="m.status === 'running' && !text(m)">
      <summary><i class="bi bi-lightbulb" aria-hidden="true"></i> {{ m.status === 'running' && !text(m) ? 'Thinking…' : 'Thought process' }}</summary>
      <div class="think-text">{{ m.thinking }}</div>
    </details>
  }
  @if (evidence(m); as line) {
    <p class="muted evidence-line" [class.bad]="m.audit?.length || m.removed?.length" role="status">{{ line }}</p>
    @for (f of m.audit ?? []; track $index) { <p class="muted evidence-line"><strong>{{ findingLabel(f.kind) }}.</strong> {{ f.detail }}</p> }
    @for (f of m.removed ?? []; track $index) { <p class="muted evidence-line"><strong>Removed:</strong> {{ findingLabel(f.kind) }}. {{ f.detail }}</p> }
  }
  @switch (m.status) {
    @case ('waiting') { <fs-progress-status stage="Waiting for this member" /> }
    @case ('error') { <p class="err">{{ m.error || 'No answer.' }}</p> }
    @default {
      @if (text(m); as t) { <div class="answer chat-markdown" [innerHTML]="html(t)"></div> }
      @else { <fs-progress-status stage="Working on the answer" /> }
    }
  }
</ng-template>
  `,
  styles: [`
.think { border-left: 2px solid var(--fs-border); padding-left: 0.65rem; color: var(--fs-muted); font-size: 0.8rem; summary { cursor: pointer; user-select: none; } .think-text { margin-top: 0.3rem; white-space: pre-wrap; overflow-wrap: anywhere; max-height: 12rem; overflow-y: auto; } }
.page-search { flex: 0 1 380px; min-width: min(100%, 260px); }
.panel { padding: 1rem 1.25rem; display: grid; gap: 0.8rem; min-width: 0; margin-bottom: 1rem; }
h2 { font-size: 1rem; margin: 0; display: flex; flex-wrap: wrap; gap: 0.5rem; align-items: baseline; small { font-size: 0.75rem; font-weight: 400; color: var(--fs-muted); } }
.cast { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(100%, 15rem), 1fr)); gap: 0.6rem; }
.member { text-align: left; border: 1px solid var(--fs-border); background: var(--fs-surface); color: var(--fs-text); border-radius: 8px; padding: 0.6rem 0.75rem; display: grid; gap: 0.15rem; }
.member small, .arch { color: var(--fs-muted); font-size: 0.78rem; }
.member:hover:not(:disabled) { background: var(--fs-surface-3); }
.member[aria-pressed='true'] { border-color: var(--fs-accent); background: var(--fs-accent-soft); }
.profile-links { display: flex; flex-wrap: wrap; gap: 0.25rem 0.5rem; }
.profile { border: 1px solid var(--fs-border); border-radius: 8px; padding: 0.9rem 1.1rem; display: grid; gap: 0.6rem; }
.profile h3 { font-size: 0.95rem; margin: 0; small { font-weight: 400; color: var(--fs-muted); margin-left: 0.4rem; } }
.profile h4 { font-size: 0.78rem; text-transform: uppercase; letter-spacing: 0.05em; color: var(--fs-muted); margin: 0.6rem 0 0.3rem; small { text-transform: none; letter-spacing: 0; margin-left: 0.4rem; } }
.profile .cols { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 22rem), 1fr)); gap: 1.2rem; }
.profile ol, .profile ul { padding-left: 1.2rem; margin: 0; display: grid; gap: 0.3rem; font-size: 0.85rem; }
.profile table { width: 100%; border-collapse: collapse; font-size: 0.82rem; th, td { padding: 0.25rem 0.4rem; border-top: 1px solid var(--fs-border); text-align: left; } thead th { border-top: 0; color: var(--fs-muted); } }
.profile .num { text-align: right; white-space: nowrap; }
.caveat { font-size: 0.8rem; color: var(--fs-warn); margin: 0.3rem 0 0; }
.strat { font-size: 0.85rem; summary { cursor: pointer; color: var(--fs-muted); } ul { margin: 0.4rem 0; padding-left: 1.1rem; display: grid; gap: 0.25rem; } }
.models { display: grid; gap: 0.5rem; h3 { font-size: 0.78rem; text-transform: uppercase; letter-spacing: 0.05em; color: var(--fs-muted); margin: 0.2rem 0 0; } }
.model-rows { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.4rem; li { display: grid; grid-template-columns: 8rem minmax(0, 14rem) minmax(0, 1fr); gap: 0.5rem; align-items: center; font-size: 0.82rem; } small { overflow-wrap: anywhere; } }
@media (max-width: 700px) { .model-rows li { grid-template-columns: minmax(0, 1fr); } }
.budget-field { display: grid; gap: 0.15rem; font-size: 0.78rem; color: var(--fs-muted); input { width: 9rem; } }
.saved-row { display: flex; flex-wrap: wrap; gap: 0.4rem; align-items: center; select, input { max-width: 18rem; min-width: 0; } }
.model-tag { font-family: var(--bs-font-monospace); }
.q { display: grid; gap: 0.25rem; font-size: 0.8rem; color: var(--fs-muted); }
.run-bar { display: flex; flex-wrap: wrap; gap: 0.75rem; align-items: center; }
.check { display: flex; gap: 0.4rem; align-items: center; font-size: 0.85rem; }
.err { color: var(--fs-neg); font-size: 0.85rem; margin: 0; }
.muted { color: var(--fs-muted); font-size: 0.8rem; margin: 0; }
.export-row { display: flex; flex-wrap: wrap; align-items: center; gap: 0.5rem; margin: 0 0 1rem; }
.tally { display: flex; flex-wrap: wrap; gap: 0.5rem; margin: 0 0 1rem; }
.badge-stance { font-size: 0.72rem; font-weight: 600; padding: 0.1rem 0.5rem; border-radius: 999px; white-space: nowrap; }
.badge-stance.fits { background: var(--fs-pos-soft); color: var(--fs-pos); }
.badge-stance.partly, .badge-stance.insufficient_data { background: var(--fs-warn-soft); color: var(--fs-warn); }
.badge-stance.does_not_fit { background: var(--fs-neg-soft); color: var(--fs-neg); }
.grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 24rem), 1fr)); gap: 1rem; align-items: start; min-width: 0; }
.grid .panel { margin-bottom: 0; min-width: 0; }
.chair { border-color: var(--fs-accent); }
.answer { font-size: 0.9rem; }
.steps { font-size: 0.78rem; color: var(--fs-muted); min-width: 0; max-width: 100%; summary { cursor: pointer; } ul { margin: 0.3rem 0 0; padding: 0; list-style: none; display: grid; grid-template-columns: minmax(0, 1fr); gap: 2px; min-width: 0; } li { min-width: 0; max-width: 100%; } fs-tool-call { display: block; min-width: 0; max-width: 100%; } .bad { color: var(--fs-neg); } }
  `],
})
export class TeamPage {
  readonly picked = signal<number | null>(null);
  readonly option = profileOption;
  defaultLabel = (): string => defaultOptionLabel(this.team.profiles());
  summaryFor = (persona: string): string => {
    const id = this.team.profileOf()[persona];
    const p = this.team.profiles().find(x => x.id === id) ?? (id ? undefined : this.team.profiles().find(x => x.is_default));
    return p ? (p.description ? p.description + ' ' : '') + profileSummary(p) : '';
  };
  loadPicked(): void { const t = this.team.savedTeams().find(x => x.id === this.picked()); if (t) this.team.loadTeam(t); }
  deletePicked(): void { const id = this.picked(); if (id !== null) { this.team.deleteSaved(id); this.picked.set(null); } }
  readonly budgetText = budgetLine;
  setBudget(field: 'tokens' | 'cost' | 'calls', ev: Event): void { this.team.budget.update(b => ({ ...b, [field]: (ev.target as HTMLInputElement).value })); }
  failedCalls = (m: { steps: { status: string }[] }): number => m.steps.filter(x => x.status === 'error').length;
  readonly team = inject(TeamSession);
  readonly market = inject(MarketState);
  private readonly sanitizer = inject(DomSanitizer);

  readonly viewing = signal('');
  readonly profile = computed(() => this.team.personas().find(p => p.id === this.viewing()));
  readonly kindLabel = (k: keyof typeof KIND_LABEL) => KIND_LABEL[k];
  readonly summary = summaryLine;
  readonly change = changeLabel;
  readonly usd = usdCompact;
  readonly staleness = (ref: Parameters<typeof stalenessNote>[0]) => stalenessNote(ref, new Date().toISOString().slice(0, 10));
  view(id: string): void { this.viewing.update(v => (v === id ? '' : id)); }

  readonly counts = computed(() => stanceCounts(this.team.state()?.members ?? []));
  readonly icon = (s: string) => STANCE_ICON[s as keyof typeof STANCE_ICON];
  readonly stanceLabel = (s: string) => STANCE_LABEL[s as keyof typeof STANCE_LABEL];
  readonly label = toolLabel;
  readonly text = shownText;
  readonly evidence = evidenceLine;
  readonly findingLabel = findingLabel;
  readonly evidenceModes = EVIDENCE_MODES;
  private readonly cache = new Map<string, SafeHtml>();

  constructor() {
    this.team.load();
    if (!this.team.question()) this.team.question.set(defaultQuestion(this.market.trackedSymbol()));
    effect(() => {
      const symbol = this.market.trackedSymbol();
      untracked(() => { if (!this.team.running() && !this.team.state()) this.team.question.set(defaultQuestion(symbol)); });
    });
  }

  toggle(id: string): void { this.team.selected.update(list => togglePersona(list, id, this.team.maxTeam())); }
  suggest(): void { this.team.question.set(defaultQuestion(this.market.trackedSymbol())); }

  /** Downloads the finished run: question, each member's answer with its sources and findings, and the chair. */
  exportRun(format: 'md' | 'json'): void {
    const s = this.team.state();
    if (!s) return;
    const mode = this.team.evidence();
    const exp = buildTeamExport({
      question: this.team.question(), members: s.members, chair: s.chair,
      policy: mode === 'default' ? null : { mode },
    });
    const blob = new Blob([format === 'json' ? teamToJson(exp) : teamToMarkdown(exp)], { type: format === 'json' ? 'application/json' : 'text/markdown' });
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = `team-answer-${exp.exported_at.slice(0, 19).replace(/[:T]/g, '-')}Z.${format}`;
    a.click();
    URL.revokeObjectURL(a.href);
  }

  html(source: string): SafeHtml {
    let out = this.cache.get(source);
    if (!out) {
      out = this.sanitizer.bypassSecurityTrustHtml(renderMarkdown(source));
      if (this.cache.size > 200) this.cache.clear();
      this.cache.set(source, out);
    }
    return out;
  }
}
