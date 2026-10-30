import { ChangeDetectionStrategy, Component, computed, input, signal } from '@angular/core';

import { StockHolding, type MarketData } from '../models';

@Component({
  selector: 'fse-asset-info',
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './asset-info.html',
  styleUrl: './asset-info.scss',
})
export class AssetInfoComponent {
  readonly data = input.required<MarketData | null>();
  readonly currentCurrency = input('USD');
  readonly tab = signal<'overview' | 'ratios' | 'holdings'>('overview');

  readonly isStock = computed(() => {
    const d = this.data();
    if (!d) return false;
    const type = (d.type ?? '').toUpperCase();
    if (type === 'STOCK' || type === 'EQUITY') return true;
    // Yahoo/finnhub assets only expose quote/keyIndicators data.
    return !!(d.quote || d.keyIndicators?.peRatio);
  });

  readonly isFund = computed(() => !this.isStock());

  readonly holdings = computed<Array<{ name: string; weight: string }>>(() => {
    const raw = this.data()?.holdings ?? [];
    return raw.map((h) => {
      if (Array.isArray(h)) {
        const [name, weight] = h;
        return { name: String(name ?? 'Unknown'), weight: typeof weight === 'number' ? weight.toFixed(2) : '-' };
      }
      const obj = h as StockHolding;
      const w = obj.weight ?? obj.percent;
      return {
        name: obj.name ?? obj.symbol ?? 'Unknown',
        weight: typeof w === 'number' ? w.toFixed(2) : '-',
      };
    });
  });

  readonly fmtPerf = (val: number | undefined | null): string =>
    val === undefined || val === null || Number.isNaN(val)
      ? '---'
      : `${val >= 0 ? '+' : ''}${val.toFixed(2)}%`;

  readonly fmtMarketCap = (val: number | undefined, currency: string): string => {
    if (!val || Number.isNaN(val)) return '---';
    if (val >= 1e9) return `${(val / 1e9).toFixed(2)}B ${currency}`;
    if (val >= 1e6) return `${(val / 1e6).toFixed(2)}M ${currency}`;
    return `${val.toLocaleString()} ${currency}`;
  };

  readonly stars = (n: number | undefined): string => (n ? `${n} / 5` : 'N/A');
  readonly seedlings = (n: number | undefined): string => (n ? `${n} / 5` : 'N/A');
}
