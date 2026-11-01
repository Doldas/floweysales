import { ChangeDetectionStrategy, Component, computed, effect, inject, signal, untracked, viewChild } from '@angular/core';
import { DecimalPipe } from '@angular/common';
import { RouterLink } from '@angular/router';

import { Access } from '../core/access';
import { AgentSession } from '../core/agent-session';
import { MarketState } from '../core/market-state';
import { NotifyService } from '../core/notify.service';
import { AssetInfoComponent } from '../components/asset-info';
import { LeaderboardComponent } from '../components/leaderboard';
import { TradingChartComponent } from '../components/chart';
import { SymbolSearchComponent } from '../shell/symbol-search';
import { INDICATOR_KEYS, INDICATOR_LABELS } from '../indicators';
import type { TrackedAsset } from '../services';

interface Kpi { label: string; value: string; sub?: string; tone?: 'pos' | 'neg' | ''; icon: string }

/** Markets: active instrument, trackers, chart with studies, insights, fundamentals and the leaderboard. */
@Component({
  selector: 'fs-markets-page',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DecimalPipe, RouterLink, AssetInfoComponent, LeaderboardComponent, TradingChartComponent, SymbolSearchComponent],
  templateUrl: './markets-page.html',
  styleUrl: './markets-page.scss',
})
export class MarketsPage {
  readonly market = inject(MarketState);
  readonly access = inject(Access);
  private readonly agent = inject(AgentSession);
  private readonly notify = inject(NotifyService);
  private readonly chart = viewChild(TradingChartComponent);

  readonly indicatorKeys = INDICATOR_KEYS;
  readonly indicatorLabels = INDICATOR_LABELS;
  readonly confirmClear = signal(false);

  readonly change = computed(() => this.market.dayChange() ?? this.market.rangeChange());
  readonly changeLabel = computed(() => (this.market.dayChange() !== null ? 'today' : this.market.historyRange()));

  readonly kpis = computed<Kpi[]>(() => {
    const cur = this.market.currency();
    const fmt = (n: number | null | undefined) => (n !== null && n !== undefined && Number.isFinite(n) && n !== 0 ? n.toFixed(2) : '—');
    const tick = this.market.latestTick();
    const seed = this.market.historySeed();
    const points = this.market.marketSeries()?.points ?? [];
    const high = points.length ? Math.max(...points.map(p => p.h || p.c)) : null;
    const low = points.length ? Math.min(...points.map(p => p.l || p.c)) : null;
    const price = this.market.livePrice();
    if (this.market.mode() === 'research') {
      const chg = this.market.rangeChange();
      return [
        { label: 'Last close', value: price > 0 ? `${price.toFixed(2)} ${cur}` : '—', icon: 'bi-currency-exchange' },
        { label: `Change · ${this.market.historyRange()}`, value: chg === null ? '—' : `${chg >= 0 ? '+' : ''}${chg.toFixed(2)}%`, tone: chg === null ? '' : chg >= 0 ? 'pos' : 'neg', icon: 'bi-arrow-left-right' },
        { label: 'Range high', value: fmt(high), sub: cur, icon: 'bi-arrow-up' },
        { label: 'Range low', value: fmt(low), sub: cur, icon: 'bi-arrow-down' },
      ];
    }
    const pnl = this.market.totalPnL();
    return [
      { label: 'Latest price', value: price > 0 ? `${price.toFixed(2)} ${cur}` : '—', sub: tick ? 'live tick' : seed ? 'from history' : '', icon: 'bi-currency-exchange' },
      { label: 'Stop loss', value: (tick?.stop_loss ?? 0) > 0 ? `${tick!.stop_loss.toFixed(2)} ${cur}` : 'Standby', tone: 'neg', icon: 'bi-shield-exclamation' },
      { label: 'Fast EMA', value: fmt(tick?.fast_ema ?? seed?.fastEMA), icon: 'bi-lightning' },
      { label: 'Slow EMA', value: fmt(tick?.slow_ema ?? seed?.slowEMA), icon: 'bi-hourglass-split' },
      { label: 'Unrealised P&L', value: `${pnl >= 0 ? '+' : ''}${pnl.toFixed(2)} ${cur}`, tone: pnl > 0 ? 'pos' : pnl < 0 ? 'neg' : '', icon: 'bi-wallet2' },
    ];
  });

  constructor() {
    // Seed the chart whenever history for the active instrument arrives.
    effect(() => {
      const series = this.market.marketSeries();
      const chart = this.chart();
      if (!chart || !series) return;
      untracked(() => this.market.historySeed.set(chart.seedSeries(series)));
    });
    // Stream live ticks into the chart.
    effect(() => {
      const tick = this.market.latestTick();
      const chart = this.chart();
      if (chart && tick && tick.symbol === untracked(this.market.trackedSymbol)) untracked(() => chart.addTick(tick));
    });
  }

  isActive(t: TrackedAsset): boolean {
    return t.symbol === this.market.trackedSymbol() && t.provider === this.market.provider();
  }

  askAgent(): void {
    const sym = this.market.trackedSymbol();
    if (!sym) return;
    const studies = this.market.enabledIndicatorKeys().map(k => INDICATOR_LABELS[k]).join(', ');
    this.agent.ask(`Analyze ${sym} on ${this.market.provider()} over the ${this.market.historyRange()} range${studies ? ` using ${studies}` : ''}: trend, momentum, key support and resistance, and what would change the picture.`);
  }

  clearHistory(): void {
    if (!this.confirmClear()) {
      this.confirmClear.set(true);
      setTimeout(() => this.confirmClear.set(false), 4000);
      return;
    }
    this.confirmClear.set(false);
    this.market.clearHistory();
  }

  resetZoom(): void {
    this.chart()?.resetZoom();
  }

  stop(t: TrackedAsset, event: Event): void {
    event.stopPropagation();
    this.market.stop(t);
    this.notify.info(`${t.symbol} removed from ${this.market.mode() === 'live' ? 'trackers' : 'your watchlist'}.`);
  }
}
