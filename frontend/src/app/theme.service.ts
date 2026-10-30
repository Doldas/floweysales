import { Injectable, computed, signal } from '@angular/core';

export type ThemeId = 'dark' | 'light';

interface ChartPalette {
  priceColor: string;
  fastEmaColor: string;
  slowEmaColor: string;
  gridColor: string;
  textColor: string;
}

const STORAGE_KEY = 'fs.theme';

/** Reads a design token from the document root (falls back during SSR/tests). */
function token(name: string, fallback: string): string {
  if (typeof document === 'undefined') return fallback;
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || fallback;
}

/**
 * ThemeService switches Bootstrap's colour mode (`data-bs-theme`) and exposes
 * the chart palette resolved from the current design tokens.
 */
@Injectable({ providedIn: 'root' })
export class ThemeService {
  readonly theme = signal<ThemeId>(this.initial());

  /** Chart colours for the active theme; recomputed whenever the theme changes. */
  readonly def = computed<ChartPalette>(() => {
    this.theme();
    return {
      priceColor: token('--fs-chart-price', '#818cf8'),
      fastEmaColor: token('--fs-chart-fast', '#f472b6'),
      slowEmaColor: token('--fs-chart-slow', '#fbbf24'),
      gridColor: token('--fs-chart-grid', 'rgba(226,232,240,.07)'),
      textColor: token('--fs-chart-text', '#8b98ad'),
    };
  });

  constructor() {
    this.apply(this.theme());
  }

  toggle(): void {
    this.set(this.theme() === 'dark' ? 'light' : 'dark');
  }

  set(id: ThemeId): void {
    try {
      localStorage.setItem(STORAGE_KEY, id);
    } catch {}
    this.apply(id);
    this.theme.set(id);
  }

  private apply(id: ThemeId): void {
    if (typeof document !== 'undefined') document.documentElement.setAttribute('data-bs-theme', id);
  }

  private initial(): ThemeId {
    try {
      const saved = localStorage.getItem(STORAGE_KEY);
      if (saved === 'dark' || saved === 'light') return saved;
      if (typeof window !== 'undefined' && window.matchMedia?.('(prefers-color-scheme: light)').matches) return 'light';
    } catch {}
    return 'dark';
  }
}
