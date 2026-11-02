import { Injectable, computed, inject, signal } from '@angular/core';

import type { Persona, TeamEvent } from '../models';
import { MarketService } from '../services';
import { MarketState } from './market-state';
import { DEFAULT_TEAM, applyTeamEvent, startTeam, type TeamState } from '../components/team-view';

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
  private abort: AbortController | null = null;

  readonly chosen = computed(() => this.personas().filter(p => this.selected().includes(p.id)));

  load(): void {
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

  stop(): void { this.abort?.abort(); }

  async run(): Promise<void> {
    const question = this.question().trim();
    const members = this.chosen();
    if (!question || !members.length || this.running()) return;
    this.error.set('');
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
          question, personas: members.map(m => m.id), chair: withChair,
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
