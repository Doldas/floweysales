/** The periods the chart's studies are worked out with. Kept free of imports so it can be tested on its own. */
export interface IndicatorSettings {
  ema9: number;
  ema21: number;
  ema50: number;
  ema100: number;
  ema200: number;
  bbPeriod: number;
  bbMult: number;
  rsiPeriod: number;
  macdFast: number;
  macdSlow: number;
  macdSignal: number;
  stochK: number;
  stochD: number;
  stochSmooth: number;
  atrPeriod: number;
  tenkan: number;
  kijun: number;
  senkouB: number;
}

export type SettingKey = keyof IndicatorSettings;

export const DEFAULT_SETTINGS: IndicatorSettings = {
  ema9: 9, ema21: 21, ema50: 50, ema100: 100, ema200: 200,
  bbPeriod: 20, bbMult: 2,
  rsiPeriod: 14,
  macdFast: 12, macdSlow: 26, macdSignal: 9,
  stochK: 14, stochD: 3, stochSmooth: 3,
  atrPeriod: 14,
  tenkan: 9, kijun: 26, senkouB: 52,
};

export interface SettingField { key: SettingKey; label: string; group: string; min: number; max: number; step: number }

export const SETTING_FIELDS: SettingField[] = [
  { key: 'ema9', label: 'EMA 1', group: 'Moving averages', min: 2, max: 500, step: 1 },
  { key: 'ema21', label: 'EMA 2', group: 'Moving averages', min: 2, max: 500, step: 1 },
  { key: 'ema50', label: 'EMA 3', group: 'Moving averages', min: 2, max: 500, step: 1 },
  { key: 'ema100', label: 'EMA 4', group: 'Moving averages', min: 2, max: 500, step: 1 },
  { key: 'ema200', label: 'EMA 5', group: 'Moving averages', min: 2, max: 500, step: 1 },
  { key: 'bbPeriod', label: 'Period', group: 'Bollinger', min: 2, max: 200, step: 1 },
  { key: 'bbMult', label: 'Deviations', group: 'Bollinger', min: 0.5, max: 5, step: 0.1 },
  { key: 'rsiPeriod', label: 'Period', group: 'RSI', min: 2, max: 100, step: 1 },
  { key: 'macdFast', label: 'Fast', group: 'MACD', min: 2, max: 100, step: 1 },
  { key: 'macdSlow', label: 'Slow', group: 'MACD', min: 3, max: 200, step: 1 },
  { key: 'macdSignal', label: 'Signal', group: 'MACD', min: 2, max: 100, step: 1 },
  { key: 'stochK', label: '%K period', group: 'Stochastic', min: 2, max: 100, step: 1 },
  { key: 'stochSmooth', label: '%K smoothing', group: 'Stochastic', min: 1, max: 20, step: 1 },
  { key: 'stochD', label: '%D period', group: 'Stochastic', min: 1, max: 20, step: 1 },
  { key: 'atrPeriod', label: 'Period', group: 'ATR', min: 2, max: 100, step: 1 },
  { key: 'tenkan', label: 'Conversion', group: 'Ichimoku', min: 2, max: 100, step: 1 },
  { key: 'kijun', label: 'Base', group: 'Ichimoku', min: 3, max: 200, step: 1 },
  { key: 'senkouB', label: 'Span B', group: 'Ichimoku', min: 4, max: 300, step: 1 },
];

/** Why a set of periods cannot be used, or '' when it can. */
export function settingsProblem(s: IndicatorSettings): string {
  for (const f of SETTING_FIELDS) {
    const v = s[f.key];
    if (!Number.isFinite(v) || v < f.min || v > f.max) return `${f.group} ${f.label.toLowerCase()} must be between ${f.min} and ${f.max}.`;
    if (f.step === 1 && !Number.isInteger(v)) return `${f.group} ${f.label.toLowerCase()} must be a whole number.`;
  }
  if (s.macdFast >= s.macdSlow) return 'MACD fast must be shorter than slow.';
  if (!(s.tenkan < s.kijun && s.kijun < s.senkouB)) return 'Ichimoku needs conversion < base < span B.';
  return '';
}

/** Settings read back from browser storage: any value that is not usable is replaced by its default. */
export function parseSettings(raw: unknown): IndicatorSettings {
  const out = { ...DEFAULT_SETTINGS };
  if (raw && typeof raw === 'object') {
    for (const f of SETTING_FIELDS) {
      const v = (raw as Record<string, unknown>)[f.key];
      if (typeof v === 'number' && Number.isFinite(v)) out[f.key] = v;
    }
  }
  return settingsProblem(out) ? { ...DEFAULT_SETTINGS } : out;
}

/** The chip text of a study with its periods, such as "EMA 12" or "MACD 12,26,9". */
export function studyLabel(key: string, s: IndicatorSettings): string {
  switch (key) {
    case 'ema9': return `EMA ${s.ema9}`;
    case 'ema21': return `EMA ${s.ema21}`;
    case 'ema50': return `EMA ${s.ema50}`;
    case 'ema100': return `EMA ${s.ema100}`;
    case 'ema200': return `EMA ${s.ema200}`;
    case 'bb': return `Bollinger ${s.bbPeriod},${s.bbMult}`;
    case 'vwap': return 'VWAP';
    case 'volume': return 'Volume';
    case 'rsi': return `RSI ${s.rsiPeriod}`;
    case 'macd': return `MACD ${s.macdFast},${s.macdSlow},${s.macdSignal}`;
    case 'stoch': return `Stochastic ${s.stochK},${s.stochSmooth},${s.stochD}`;
    case 'atr': return `ATR ${s.atrPeriod}`;
    case 'ichimoku': return `Ichimoku ${s.tenkan},${s.kijun},${s.senkouB}`;
    default: return key;
  }
}
