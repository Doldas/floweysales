import { Injectable, computed, inject, signal } from '@angular/core';

import type { ModelProfile, Persona, SavedTeam, TeamEvent } from '../models';
import { missingProfiles, savedMembers, teamMembersBody } from '../components/profile-view';
import { MarketService } from '../services';
import { MarketState } from './market-state';
import { DEFAULT_TEAM, applyTeamEvent, startTeam, type TeamState } from '../components/team-view';
import { NO_BUDGET, budgetBody, budgetInputOf, parseBudget, type BudgetInput } from '../components/team-budget';

/**
 * Runs a team of persona agents on one question and keeps the streamed
 * result. The page that started a run can be left and reopened; the state
 * lives here.
 */
@Injectable({ providedIn: 'root' })
export class TeamSession {
  private readonly api = inject(MarketService);
  private readonly market = inject(MarketState);

  readonly personas = signal<Persona[]>([]);
  readonly maxTeam = signal(6);
  readonly selected = signal<string[]>([]);
  readonly state = signal<TeamState | null>(null);
  readonly running = signal(false);
  readonly error = signal('');
  readonly question = signal('');
  readonly chair = signal(true);
  /** Model profiles to choose from, the profile of each chosen character (null: the default), and the saved teams. */
  readonly profiles = signal<ModelProfile[]>([]);
  readonly profileOf = signal<Record<string, number | null>>({});
  readonly savedTeams = signal<SavedTeam[]>([]);
  readonly maxSaved = signal(20);
  readonly teamName = signal('');
  readonly savedId = signal<number | null>(null);
  readonly teamNotice = signal('');
  /** Ceilings for a run (empty: none). A saved team keeps them. */
  readonly budget = signal<BudgetInput>(NO_BUDGET);
  private abort: AbortController | null = null;

  readonly chosen = computed(() => this.personas().filter(p => this.selected().includes(p.id)));

  load(): void {
    this.loadChoices();
    if (this.personas().length) return;
    this.api.getPersonas().subscribe({
      next: c => {
        this.personas.set(c.personas);
        this.maxTeam.set(c.max_team);
        if (!this.selected().length) this.selected.set(DEFAULT_TEAM.filter(id => c.personas.some(p => p.id === id)));
      },
      error: () => this.error.set('The persona list could not be loaded.'),
    });
  }

  loadChoices(): void {
    this.api.getProfiles().subscribe({ next: r => this.profiles.set(r.profiles), error: () => this.profiles.set([]) });
    this.api.getSavedTeams().subscribe({ next: r => { this.savedTeams.set(r.teams); this.maxSaved.set(r.max); }, error: () => this.savedTeams.set([]) });
  }

  setMemberProfile(persona: string, id: number | null): void {
    this.profileOf.update(m => ({ ...m, [persona]: id }));
  }

  /** Puts a saved team's characters and profiles on the page. A profile that is gone is dropped and said so. */
  loadTeam(team: SavedTeam): void {
    const ids = team.members.map(m => m.persona).filter(id => this.personas().some(p => p.id === id));
    this.selected.set(ids);
    this.chair.set(team.chair);
    const gone = missingProfiles(team, this.profiles());
    this.profileOf.set(Object.fromEntries(team.members.map(m => [m.persona, m.profile_id !== null && gone.includes(m.profile_id) ? null : m.profile_id])));
    this.budget.set(budgetInputOf(team));
    this.teamName.set(team.name);
    this.savedId.set(team.id);
    this.teamNotice.set(gone.length ? 'A model profile of this team is no longer offered; those members use the default.' : '');
  }

  saveCurrent(): void {
    const name = this.teamName().trim();
    if (!name || !this.selected().length) return;
    const existing = this.savedTeams().find(t => t.name === name);
    const limits = parseBudget(this.budget());
    if (!limits.ok) { this.teamNotice.set(limits.error); return; }
    this.api.saveTeam({ id: existing?.id, name, chair: this.chair(), members: savedMembers(this.selected(), this.profileOf()), ...limits.limits }).subscribe({
      next: r => { this.savedId.set(r.id); this.teamNotice.set(existing ? 'Team updated.' : 'Team saved.'); this.loadChoices(); },
      error: err => this.teamNotice.set(err?.error?.error ?? 'The team could not be saved.'),
    });
  }

  deleteSaved(id: number): void {
    this.api.deleteTeam(id).subscribe({
      next: () => { if (this.savedId() === id) { this.savedId.set(null); } this.teamNotice.set('Team deleted.'); this.loadChoices(); },
      error: err => this.teamNotice.set(err?.error?.error ?? 'The team could not be deleted.'),
    });
  }

  stop(): void { this.abort?.abort(); }

  async run(): Promise<void> {
    const question = this.question().trim();
    const members = this.chosen();
    if (!question || !members.length || this.running()) return;
    this.error.set('');
    const limits = parseBudget(this.budget());
    if (!limits.ok) { this.error.set(limits.error); return; }
    this.running.set(true);
    const withChair = this.chair() && members.length > 1;
    this.state.set(startTeam(members, withChair));
    const controller = new AbortController();
    this.abort = controller;
    const price = this.market.livePrice();
    try {
      const res = await fetch('/api/ai/team', {
        method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, signal: controller.signal,
        body: JSON.stringify({
          question, members: teamMembersBody(members.map(m => m.id), this.profileOf()), chair: withChair, ...budgetBody(limits.limits),
          symbol: this.market.trackedSymbol(), provider: this.market.provider(), range: this.market.historyRange(), price: price > 0 ? price.toFixed(4) : '',
        }),
      });
      if (!res.ok || !res.body) {
        let detail = `HTTP ${res.status}`;
        try { detail = (await res.json())?.error ?? detail; } catch { /* keep the status */ }
        throw new Error(detail);
      }
      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';
      for (;;) {
        const { value, done } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        let sep: number;
        while ((sep = buffer.indexOf('\n\n')) >= 0) {
          this.handle(buffer.slice(0, sep));
          buffer = buffer.slice(sep + 2);
        }
      }
    } catch (err) {
      if ((err as Error)?.name !== 'AbortError') this.error.set((err as Error)?.message || 'Request failed');
    } finally {
      this.state.update(s => (s ? { ...s, finished: true } : s));
      this.running.set(false);
      this.abort = null;
    }
  }

  private handle(chunk: string): void {
    for (const raw of chunk.split('\n')) {
      const line = raw.trim();
      if (!line.startsWith('data:')) continue;
      try {
        const ev: TeamEvent = JSON.parse(line.slice(5).trim());
        this.state.update(s => (s ? applyTeamEvent(s, ev) : s));
      } catch { /* a partial or foreign line */ }
    }
  }
}
