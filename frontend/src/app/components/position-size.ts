/** Wording for the order ticket's "Size by risk" result. Pure, tested with node --test. */
import type { SizeResponse } from '../models';

const CAP_TEXT: Record<string, string> = {
  max_weight: 'capped by the maximum position size (20% of equity)',
  buying_power: 'capped by buying power',
};

function qty(v: number): string {
  return Number.isInteger(v) ? String(v) : v.toFixed(8).replace(/0+$/, '').replace(/\.$/, '');
}

/** "Risk 1.0% = 1,000 · stop 182.40 (2× ATR) · 12% of equity", plus any cap. */
export function sizeSummary(r: SizeResponse, money: (v: number) => string): string {
  const stop = r.stop_source === 'atr' ? `stop ${r.result.stop.toFixed(2)} (${r.atr_mult}× ATR)` : `stop ${r.result.stop.toFixed(2)}`;
  const parts = [`Risk ${r.risk_pct.toFixed(1)}% = ${money(r.result.risk_amount)}`, stop, `${r.result.position_pct.toFixed(0)}% of equity`];
  if (r.result.quantity < 1e-8) return `${parts.join(' · ')} · too small to size at this stop`;
  const cap = r.result.capped_by ? ` · ${CAP_TEXT[r.result.capped_by]}` : '';
  return `${parts.join(' · ')} · quantity ${qty(r.result.quantity)}${cap}`;
}
