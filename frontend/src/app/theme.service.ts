import { Injectable, signal } from '@angular/core';

export type ThemeId = 'theme-cyberpunk' | 'theme-deepspace' | 'theme-matrix' | 'theme-mediterranean';

interface ThemeDef {
  id: ThemeId;
  label: string;
  priceColor: string;
  fastEmaColor: string;
}

export const THEMES: ThemeDef[] = [
  { id: 'theme-cyberpunk', label: '⚡ Neon Cyberpunk', priceColor: '#00f0ff', fastEmaColor: '#ff007f' },
  { id: 'theme-deepspace', label: '🌌 Deep Space', priceColor: '#6366f1', fastEmaColor: '#a855f7' },
  { id: 'theme-matrix', label: '🟢 Matrix Grid', priceColor: '#00ff66', fastEmaColor: '#33ff99' },
  { id: 'theme-mediterranean', label: '🌊 Mediterranean', priceColor: '#00f2fe', fastEmaColor: '#4facfe' },
];

@Injectable({ providedIn: 'root' })
export class ThemeService {
  private static readonly KEY = 'lastTheme';

  readonly theme = signal<ThemeId>(
    (localStorage.getItem(ThemeService.KEY) as ThemeId) ?? 'theme-cyberpunk',
  );

  readonly def = signal<ThemeDef>(THEMES.find((t) => t.id === this.theme()) ?? THEMES[0]);

  constructor() {
    document.body.classList.add(this.theme());
  }

  set(id: ThemeId): void {
    document.body.classList.remove(this.theme());
    document.body.classList.add(id);
    localStorage.setItem(ThemeService.KEY, id);
    this.theme.set(id);
    this.def.set(THEMES.find((t) => t.id === id) ?? THEMES[0]);
  }
}
