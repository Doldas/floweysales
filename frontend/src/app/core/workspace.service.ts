import { Injectable, signal } from '@angular/core';

import { STORAGE_KEY, parsePrefs, serializePrefs, withDensity, withPanel, type Density, type PanelState, type WorkspacePrefs } from './workspace-prefs';

/**
 * The user's workspace preferences: density and the state of panes. Saved in
 * this browser and applied as `data-density` on the document root, so every
 * component follows it through design tokens. Display choices only.
 */
@Injectable({ providedIn: 'root' })
export class Workspace {
  private readonly state = signal<WorkspacePrefs>(this.load());
  readonly density = signal<Density>(this.state().density);

  constructor() {
    this.apply(this.density());
  }

  setDensity(density: Density): void {
    this.state.set(withDensity(this.state(), density));
    this.density.set(this.state().density);
    this.apply(this.density());
    this.save();
  }

  toggleDensity(): void {
    this.setDensity(this.density() === 'compact' ? 'comfortable' : 'compact');
  }

  panel(id: string): PanelState {
    return this.state().panels[id] ?? {};
  }

  setPanel(id: string, patch: PanelState): void {
    this.state.set(withPanel(this.state(), id, patch));
    this.save();
  }

  private apply(density: Density): void {
    if (typeof document !== 'undefined') document.documentElement.setAttribute('data-density', density);
  }

  private load(): WorkspacePrefs {
    try {
      return parsePrefs(localStorage.getItem(STORAGE_KEY));
    } catch {
      return parsePrefs(null);
    }
  }

  private save(): void {
    try {
      localStorage.setItem(STORAGE_KEY, serializePrefs(this.state()));
    } catch {}
  }
}
