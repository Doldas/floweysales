import { ChangeDetectionStrategy, Component, DestroyRef, computed, inject, signal } from '@angular/core';
import { DecimalPipe } from '@angular/common';
import { Router, RouterLink } from '@angular/router';

import { AuthState } from '../auth.service';
import { Access } from '../core/access';
import { ActivityStore, runTime, type FlowSummary } from '../core/activity';
import { AgentSession } from '../core/agent-session';
import { MarketState } from '../core/market-state';
import { NotifyService } from '../core/notify.service';
import { FLOW_TEMPLATES, CommandPaletteService } from '../shell/command-palette';
import { SparklineComponent } from '../ui/sparkline';
import { duration, everyLabel, greeting, runLabel, runTone, timeAgo } from '../ui/format';
import type { TrackedAsset } from '../services';

interface TrackerStatus { symbol: string; provider: string; has_tick: boolean; age_seconds: number | null }
interface WorkspaceFile { path: string; content_type: string; size: number }

/**
 * Command Center: the agentic home. Ask the agent, see what flows are doing,
 * act on decisions waiting for a human and jump into markets.
 */
@Component({
  selector: 'fs-command-center',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink, DecimalPipe, SparklineComponent],
  templateUrl: './command-center.html',
  styleUrl: './command-center.scss',
})
export class CommandCenterPage {
  readonly auth = inject(AuthState);
  readonly access = inject(Access);
  readonly activity = inject(ActivityStore);
  readonly agent = inject(AgentSession);
  readonly market = inject(MarketState);
  readonly palette = inject(CommandPaletteService);
  private readonly router = inject(Router);
  private readonly notify = inject(NotifyService);

  readonly templates = FLOW_TEMPLATES;
  readonly greeting = greeting();
  readonly today = new Date().toLocaleDateString(undefined, { weekday: 'long', month: 'long', day: 'numeric' });
  readonly name = computed(() => {
    const local = this.auth.user()?.email?.split('@')[0] ?? '';
    return local ? local.charAt(0).toUpperCase() + local.slice(1).split(/[._-]/)[0] : '';
  });

  readonly prompt = signal('');
  readonly trackerStatus = signal<Record<string, TrackerStatus>>({});
  readonly files = signal<WorkspaceFile[]>([]);
  readonly runningFlow = signal(0);

  readonly timeAgo = timeAgo;
  readonly duration = duration;
  readonly runTone = runTone;
  readonly runLabel = runLabel;

  readonly priceChange = computed(() => this.market.dayChange() ?? this.market.rangeChange());
  readonly recentRuns = computed(() => this.activity.runs().slice(0, 8));
  readonly automations = computed(() =>
    [...this.activity.flows()].sort((a, b) => Number(b.enabled) - Number(a.enabled) || a.name.localeCompare(b.name)).slice(0, 6),
  );
  readonly lastRunByFlow = computed(() => {
    const map: Record<number, { status: string; at: number }> = {};
    for (const r of this.activity.runs()) if (!map[r.flowId]) map[r.flowId] = { status: r.status, at: runTime(r) };
    return map;
  });

  readonly chips = computed(() => {
    const sym = this.market.trackedSymbol();
    if (sym) {
      return [
        `What moved ${sym} over the last ${this.market.historyRange()}?`,
        `Summarize ${sym}'s latest quarter`,
        `Key risks in ${sym}'s recent SEC filings`,
        `Estimate a DCF range for ${sym}`,
      ];
    }
    return ['Research brief on Nvidia', 'Compare Microsoft and Alphabet margins', 'Latest SEC filings for Apple', 'Explain RSI and MACD on Bitcoin'];
  });

  constructor() {
    void this.activity.refresh();
    if (this.access.liveMarkets()) void this.loadTrackerStatus();
    if (this.access.workspace()) void this.loadFiles();
    const timer = setInterval(() => {
      if (document.visibilityState !== 'visible') return;
      void this.activity.refresh();
      if (this.access.liveMarkets()) void this.loadTrackerStatus();
    }, 30_000);
    inject(DestroyRef).onDestroy(() => clearInterval(timer));
  }

  ask(text = this.prompt()): void {
    const q = text.trim();
    if (!q) {
      this.agent.openDock();
      return;
    }
    this.prompt.set('');
    this.agent.ask(q);
  }

  openTracker(t: TrackedAsset): void {
    this.market.view(t);
    void this.router.navigateByUrl('/markets');
  }

  isActive(t: TrackedAsset): boolean {
    return t.symbol === this.market.trackedSymbol() && t.provider === this.market.provider();
  }

  freshness(t: TrackedAsset): { label: string; tone: string } {
    const s = this.trackerStatus()[`${t.provider}|${t.symbol}`];
    if (!s) return { label: '', tone: '' };
    if (!s.has_tick || s.age_seconds === null) return { label: 'No ticks yet', tone: 'is-warning' };
    if (s.age_seconds < 120) return { label: 'Live', tone: 'is-success' };
    if (s.age_seconds < 3600) return { label: `${Math.round(s.age_seconds / 60)} min old`, tone: 'is-info' };
    return { label: timeAgo(Date.now() - s.age_seconds * 1000), tone: 'is-warning' };
  }

  trigger(f: FlowSummary): string {
    if (f.interval_minutes > 0) return everyLabel(f.interval_minutes);
    if (f.event_key) return `On ${f.event_key}`;
    return 'Manual';
  }

  async run(f: FlowSummary): Promise<void> {
    if (this.runningFlow()) return;
    this.runningFlow.set(f.id);
    try {
      await this.activity.runFlow(f.id);
      this.notify.success(`${f.name} is queued. Results appear in agent activity.`, 'Flow started');
    } catch (e) {
      this.notify.error((e as Error).message, 'Could not start flow');
    } finally {
      this.runningFlow.set(0);
    }
  }

  fileIcon(f: WorkspaceFile): string {
    if (f.content_type.includes('json')) return 'bi-filetype-json';
    if (f.content_type.includes('csv')) return 'bi-filetype-csv';
    if (f.content_type.includes('markdown')) return 'bi-filetype-md';
    if (f.content_type.includes('xml')) return 'bi-filetype-xml';
    return 'bi-file-earmark-text';
  }

  private async loadTrackerStatus(): Promise<void> {
    try {
      const res = await fetch('/api/trackers/status', { credentials: 'same-origin' });
      if (!res.ok) return;
      const data = (await res.json()) as { trackers?: TrackerStatus[] };
      const map: Record<string, TrackerStatus> = {};
      for (const t of data.trackers ?? []) map[`${t.provider}|${t.symbol}`] = t;
      this.trackerStatus.set(map);
    } catch {}
  }

  private async loadFiles(): Promise<void> {
    try {
      const res = await fetch('/api/userspace/files', { credentials: 'same-origin' });
      if (!res.ok) return;
      const data = (await res.json()) as { files?: WorkspaceFile[] };
      this.files.set((data.files ?? []).filter(f => !f.path.endsWith('/.folder')).slice(0, 6));
    } catch {}
  }
}
