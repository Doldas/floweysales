import { NgTemplateOutlet } from '@angular/common';
import { RouterLink } from '@angular/router';
import { ChangeDetectionStrategy, Component, computed, effect, inject, untracked } from '@angular/core';
import { DomSanitizer, type SafeHtml } from '@angular/platform-browser';

import { toolLabel } from '../components/agent-tools';
import { STANCE_ICON, STANCE_LABEL, defaultQuestion, shownText, stanceCounts, togglePersona, type MemberState } from '../components/team-view';
import { MarketState } from '../core/market-state';
import { TeamSession } from '../core/team-session';
import { renderMarkdown } from '../markdown';
import { SymbolSearchComponent } from '../shell/symbol-search';

/**
 * Agent team: several investor characters answer one question side by side,
 * each in its own style, and a neutral chair compares them. The characters
 * are fictional; they do research, give no advice and place no orders.
 */
@Component({
  selector: 'fs-team-page',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [SymbolSearchComponent, NgTemplateOutlet, RouterLink],
  template: `
<div class="fs-page research-container">
  <header class="fs-page-header">
    <div>
      <div class="fs-eyebrow">Research agent</div>
      <h1>Agent team @if (market.trackedSymbol()) { <span class="text-muted fw-normal">· {{ market.trackedSymbol() }}</span> }</h1>
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
            @if (m.stance) { <span [class]="'badge-stance ' + m.stance"><i [class]="'bi ' + icon(m.stance)"></i> {{ stanceLabel(m.stance) }}</span> }
          </h2>
          <ng-container *ngTemplateOutlet="body; context: { m }" />
        </article>
      }
    </div>
    <p class="muted">Each character only sees your question and its own tool results, never another's answer. Fictional characters; not investment advice.</p>
  }
</div>

<ng-template #body let-m="m">
  @if (m.steps.length) {
    <details class="steps"><summary>{{ m.steps.length }} tool {{ m.steps.length === 1 ? 'call' : 'calls' }}</summary>
      <ul>@for (st of m.steps; track $index) { <li [class.run]="st.status === 'running'" [class.bad]="st.status === 'error'">{{ label(st.name) }}</li> }</ul></details>
  }
  @switch (m.status) {
    @case ('waiting') { <p class="muted">Waiting…</p> }
    @case ('error') { <p class="err">{{ m.error || 'No answer.' }}</p> }
    @default {
      @if (text(m); as t) { <div class="answer chat-markdown" [innerHTML]="html(t)"></div> }
      @else { <p class="muted"><span class="spinner-border spinner-border-sm" aria-hidden="true"></span> Working…</p> }
    }
  }
</ng-template>
  `,
  styles: [`
.page-search { flex: 0 1 380px; min-width: min(100%, 260px); }
.panel { padding: 1rem 1.25rem; display: grid; gap: 0.8rem; min-width: 0; margin-bottom: 1rem; }
h2 { font-size: 1rem; margin: 0; display: flex; flex-wrap: wrap; gap: 0.5rem; align-items: baseline; small { font-size: 0.75rem; font-weight: 400; color: var(--fs-muted); } }
.cast { display: grid; grid-template-columns: repeat(auto-fill, minmax(15rem, 1fr)); gap: 0.6rem; }
.member { text-align: left; border: 1px solid var(--fs-border); background: var(--fs-surface); color: var(--fs-text); border-radius: 8px; padding: 0.6rem 0.75rem; display: grid; gap: 0.15rem; }
.member small, .arch { color: var(--fs-muted); font-size: 0.78rem; }
.member:hover:not(:disabled) { background: var(--fs-surface-3); }
.member[aria-pressed='true'] { border-color: var(--fs-accent); background: var(--fs-accent-soft); }
.strat { font-size: 0.85rem; summary { cursor: pointer; color: var(--fs-muted); } ul { margin: 0.4rem 0; padding-left: 1.1rem; display: grid; gap: 0.25rem; } }
.q { display: grid; gap: 0.25rem; font-size: 0.8rem; color: var(--fs-muted); }
.run-bar { display: flex; flex-wrap: wrap; gap: 0.75rem; align-items: center; }
.check { display: flex; gap: 0.4rem; align-items: center; font-size: 0.85rem; }
.err { color: var(--fs-neg); font-size: 0.85rem; margin: 0; }
.muted { color: var(--fs-muted); font-size: 0.8rem; margin: 0; }
.tally { display: flex; flex-wrap: wrap; gap: 0.5rem; margin: 0 0 1rem; }
.badge-stance { font-size: 0.72rem; font-weight: 600; padding: 0.1rem 0.5rem; border-radius: 999px; white-space: nowrap; }
.badge-stance.fits { background: var(--fs-pos-soft); color: var(--fs-pos); }
.badge-stance.partly, .badge-stance.insufficient_data { background: var(--fs-warn-soft); color: var(--fs-warn); }
.badge-stance.does_not_fit { background: var(--fs-neg-soft); color: var(--fs-neg); }
.grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 24rem), 1fr)); gap: 1rem; align-items: start; }
.grid .panel { margin-bottom: 0; }
.chair { border-color: var(--fs-accent); }
.answer { font-size: 0.9rem; }
.steps { font-size: 0.78rem; color: var(--fs-muted); ul { margin: 0.3rem 0 0; padding-left: 1.1rem; } .run { opacity: 0.6; } .bad { color: var(--fs-neg); } }
  `],
})
export class TeamPage {
  readonly team = inject(TeamSession);
  readonly market = inject(MarketState);
  private readonly sanitizer = inject(DomSanitizer);

  readonly counts = computed(() => stanceCounts(this.team.state()?.members ?? []));
  readonly icon = (s: string) => STANCE_ICON[s as keyof typeof STANCE_ICON];
  readonly stanceLabel = (s: string) => STANCE_LABEL[s as keyof typeof STANCE_LABEL];
  readonly label = toolLabel;
  readonly text = shownText;
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
