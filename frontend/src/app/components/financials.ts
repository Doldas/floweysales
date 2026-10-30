import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  inject,
  input,
  signal,
} from '@angular/core';

import { MarketService, describeHttpError } from '../services';
import { AgentSession } from '../core/agent-session';
import type { FinancialsResponse, ProviderId, QuarterlyFinancial } from '../models';

interface FinRow {
  period: string;
  periodEnd: string;
  form: string;
  revenue: string;
  qoq: string;
  qoqTone: 'pos' | 'neg' | 'none';
  netIncome: string;
  grossMargin: string;
  netMargin: string;
  eps: string;
  fcf: string;
}

const moneyFmt = new Intl.NumberFormat(undefined, {
  notation: 'compact',
  maximumFractionDigits: 2,
});

const DASH = '—';

@Component({
  selector: 'fse-financials',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [],
  templateUrl: './financials.html',
  styleUrl: './financials.scss',
})
export class FinancialsComponent {
  private readonly api = inject(MarketService);
  private readonly agent = inject(AgentSession);

  readonly symbol = input('');
  /** Shows the "Analyze with agent" action in the header. */
  readonly agentAction = input(true);
  readonly provider = input<ProviderId>('YAHOO');

  readonly yearOptions = Array.from({length: new Date().getFullYear() - 1999}, (_,i) => new Date().getFullYear()-i);
  readonly yearMode = signal<'recent'|'custom'>('recent');
  readonly fromYear = signal(Math.max(2000,new Date().getFullYear()-4));
  readonly toYear = signal(new Date().getFullYear());
  readonly loading = signal(false);
  readonly error = signal('');
  readonly data = signal<FinancialsResponse | null>(null);


  readonly rows = computed<FinRow[]>(() => {
    const reports = this.data()?.reports ?? [];
    return reports.map((r, i) => {
      const older = reports[i + 1];
      const qoq = this.qoq(r, older);
      return {
        period: r.period,
        periodEnd: r.period_end,
        form: r.form ?? '',
        revenue: this.money(r.revenue),
        qoq: qoq === null ? '' : this.pct(qoq),
        qoqTone: qoq === null ? 'none' : qoq >= 0 ? 'pos' : 'neg',
        netIncome: this.money(r.net_income),
        grossMargin: this.margin(r.gross_profit, r.revenue),
        netMargin: this.margin(r.net_income, r.revenue),
        eps: r.eps === undefined ? DASH : r.eps.toFixed(2),
        fcf: this.money(r.free_cash_flow),
      };
    });
  });

  /** Revenue bars, oldest quarter first, scaled to the largest absolute value. */
  readonly trend = computed(() => {
    const reports = [...(this.data()?.reports ?? [])].reverse().slice(-12);
    const max = Math.max(...reports.map(r => Math.abs(r.revenue ?? 0)), 1);
    return reports.map(r => ({
      period: r.period,
      short: r.period.replace(/^CY/, '').replace(/^(\d{4}) (Q\d)$/, "$2 '$1").replace(/'(\d\d)(\d\d)$/, "'$2"),
      pct: (Math.abs(r.revenue ?? 0) / max) * 100,
      negative: (r.revenue ?? 0) < 0,
      label: this.money(r.revenue),
    }));
  });

  readonly hasReports = computed(() => (this.data()?.reports?.length ?? 0) > 0);
  readonly note = computed(() => this.data()?.note ?? '');
  readonly sourceLabel = computed(() => {
    const src = this.data()?.source ?? '';
    if (src === 'yahoo') return 'Yahoo Finance';
    if (src === 'finnhub') return 'Finnhub (SEC filings)';
    return '';
  });
  readonly currency = computed(() => this.data()?.reports?.find((r) => r.currency)?.currency ?? '');

  /** Hide the whole card until a symbol is tracked. */
  readonly visible = computed(() => !!this.symbol());
  readonly showBody = computed(
    () => this.hasReports() || !!this.note() || !!this.error(),
  );

  private requestId = 0;

  constructor() {
    effect(() => {
      const symbol = this.symbol();
      const provider = this.provider();
      if (!symbol) return;
      this.load(symbol, provider);
    });
  }

  setYearMode(mode: 'recent'|'custom'):void {
    if (mode===this.yearMode()) return;
    this.yearMode.set(mode);
    this.reload();
  }

  setFromYear(year: number):void {
    this.fromYear.set(year);
    if (this.yearMode()==='custom') this.reload();
  }

  setToYear(year: number):void {
    this.toYear.set(year);
    if (this.yearMode()==='custom') this.reload();
  }

  /** Hands the loaded statements to the research agent for interpretation. */
  askAgent(): void {
    const periods = this.rows().slice(0, 8).map(r => r.period).join(', ');
    this.agent.ask(`Analyze ${this.symbol()}'s quarterly financials (${periods}): revenue growth, margin trend, earnings quality and free cash flow. Flag anything unusual and cite the periods.`);
  }

  reload(): void {
    const symbol = this.symbol();
    if (!symbol) return;
    this.load(symbol, this.provider());
  }

  private load(symbol: string, provider: ProviderId): void {
    const id = ++this.requestId;
    this.loading.set(true);
    this.error.set('');
    const custom=this.yearMode()==='custom';
    const from=custom?this.fromYear():undefined;
    const to=custom?this.toYear():undefined;
    if (custom && (from!>to! || to!-from!+1>20)) {
      this.loading.set(false);
      this.data.set(null);
      this.error.set('Select a valid range of up to 20 years (start must be at or before end).');
      return;
    }
    this.api.getQuarterlyFinancials(symbol, provider, from, to).subscribe({
      next: (res) => {
        if (id !== this.requestId) return;
        this.loading.set(false);
        this.data.set(res);
      },
      error: (err) => {
        if (id !== this.requestId) return;
        this.loading.set(false);
        this.data.set(null);
        this.error.set(describeHttpError(err));
      },
    });
  }

  private qoq(cur: QuarterlyFinancial, older?: QuarterlyFinancial): number | null {
    if (!cur.revenue || !older?.revenue) return null;
    return ((cur.revenue - older.revenue) / older.revenue) * 100;
  }

  private money(v: number | undefined): string {
    if (v === undefined || v === null || Number.isNaN(v)) return DASH;
    const sign = v < 0 ? '-' : '';
    return sign + moneyFmt.format(Math.abs(v));
  }

  private pct(v: number): string {
    return `${v >= 0 ? '+' : ''}${v.toFixed(1)}%`;
  }

  private margin(part: number | undefined, whole: number | undefined): string {
    if (!part || !whole) return DASH;
    return `${((part / whole) * 100).toFixed(1)}%`;
  }
}
