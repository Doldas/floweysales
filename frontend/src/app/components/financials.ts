import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  inject,
  input,
  signal,
} from '@angular/core';
import { MatCardModule } from '@angular/material/card';
import { MatTableModule } from '@angular/material/table';
import { MatIconModule } from '@angular/material/icon';
import { MatButtonModule } from '@angular/material/button';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { MatTooltipModule } from '@angular/material/tooltip';

import { MarketService, describeHttpError } from '../services';
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
  imports: [
    MatCardModule,
    MatTableModule,
    MatIconModule,
    MatButtonModule,
    MatProgressBarModule,
    MatTooltipModule,
  ],
  templateUrl: './financials.html',
  styleUrl: './financials.scss',
})
export class FinancialsComponent {
  private readonly api = inject(MarketService);

  readonly symbol = input('');
  readonly provider = input<ProviderId>('YAHOO');

  readonly loading = signal(false);
  readonly error = signal('');
  readonly data = signal<FinancialsResponse | null>(null);

  readonly columns = ['period', 'revenue', 'netIncome', 'grossMargin', 'netMargin', 'eps', 'fcf'];

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

  reload(): void {
    const symbol = this.symbol();
    if (!symbol) return;
    this.load(symbol, this.provider());
  }

  private load(symbol: string, provider: ProviderId): void {
    const id = ++this.requestId;
    this.loading.set(true);
    this.error.set('');
    this.api.getQuarterlyFinancials(symbol, provider).subscribe({
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
