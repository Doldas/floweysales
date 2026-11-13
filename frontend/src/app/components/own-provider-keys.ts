import { ChangeDetectionStrategy, Component, inject, signal } from '@angular/core';

import { MarketService } from '../services';
import type { OwnKeyPlugin, OwnKeysView } from '../models';

/**
 * Settings → Providers → Your own keys. A user saves keys for the providers an
 * administrator opened to own keys; their requests use them first, in a
 * process of their own. A saved key is never shown again, only that it is
 * saved. An administrator decides per provider whether users may add one.
 */
@Component({
  selector: 'fse-own-provider-keys',
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'fs-native-forms' },
  template: `
    <section class="ok" aria-labelledby="ok-title">
      <h3 id="ok-title">Your own keys</h3>
      @if (view(); as v) {
        <p class="muted">{{ v.notice }}</p>
        @if (!v.signed_in) { <p class="muted" role="note">Own keys need a signed-in account. Without accounts, set the provider's key under Settings → Plugins.</p> }
        @if (message()) { <p class="alert alert-info py-2" role="status">{{ message() }}</p> }
        @if (!v.plugins.length) { <p class="muted">No provider takes your own key yet.</p> }
        @for (p of v.plugins; track p.plugin_id) {
          <article class="ok-plugin" [attr.aria-label]="p.label">
            <header><strong>{{ p.label }}</strong> <span class="muted">{{ p.provider_id }}</span></header>
            @if (v.can_edit) {
              <label class="ok-check"><input type="checkbox" [checked]="p.allowed" [disabled]="busy()" (change)="allow(p, $any($event.target).checked)" /> Let users add their own key</label>
            }
            @if (p.allowed && v.signed_in) {
              <form class="ok-form" (submit)="$event.preventDefault(); save(p)">
                @for (x of p.variables; track x.name) {
                  <label>{{ x.name }}
                    <input type="password" autocomplete="off" spellcheck="false" [name]="p.plugin_id + '-' + x.name"
                      [value]="draft(p, x.name)" (input)="type(p, x.name, $any($event.target).value)"
                      [attr.placeholder]="x.saved ? 'Saved (type to replace)' : 'Not set'" [attr.aria-describedby]="p.plugin_id + '-' + x.name + '-help'" />
                    <small class="muted" [id]="p.plugin_id + '-' + x.name + '-help'">{{ x.description }}@if (x.saved) { · <span class="ok-saved">Saved</span>}</small>
                  </label>
                }
                @if (problem()[p.plugin_id]) { <p class="ok-error" role="alert">{{ problem()[p.plugin_id] }}</p> }
                <div class="ok-actions">
                  <button type="submit" class="btn btn-sm btn-primary" [disabled]="busy()">Save my key</button>
                  @if (anySaved(p)) { <button type="button" class="btn btn-sm btn-outline-secondary" [disabled]="busy()" (click)="remove(p)">Remove my key</button> }
                </div>
              </form>
            }
          </article>
        }
      } @else if (error()) {
        <p class="alert alert-warning" role="alert">{{ error() }}</p>
      } @else {
        <p class="muted" role="status">Loading your keys…</p>
      }
    </section>
  `,
  styles: [`
    .ok { display: grid; gap: 0.75rem; margin-top: 1.25rem; }
    .ok-plugin { border: 1px solid var(--fs-border); border-radius: 0.5rem; padding: 0.7rem 0.9rem; display: grid; gap: 0.5rem; }
    .ok-plugin header { display: flex; gap: 0.75rem; align-items: baseline; flex-wrap: wrap; }
    .ok-form { display: grid; gap: 0.5rem; max-width: 34rem; }
    .ok-form label { display: grid; gap: 0.2rem; font-size: 0.8rem; min-width: 0; }
    .ok-form input { min-width: 0; width: 100%; }
    .ok-check { display: flex; gap: 0.5rem; align-items: center; font-size: 0.85rem; }
    .ok-saved { font-weight: 600; color: var(--fs-pos); }
    .ok-error { margin: 0; color: var(--fs-neg); font-size: 0.85rem; }
    .ok-actions { display: flex; gap: 0.5rem; flex-wrap: wrap; }
  `],
})
export class OwnProviderKeysComponent {
  private readonly api = inject(MarketService);

  readonly view = signal<OwnKeysView | null>(null);
  readonly error = signal('');
  readonly message = signal('');
  readonly busy = signal(false);
  readonly problem = signal<Record<string, string>>({});
  // Typed values live only here until they are sent, and are cleared after.
  private readonly drafts = signal<Record<string, Record<string, string>>>({});

  constructor() { this.load(); }

  load(): void {
    this.api.getOwnKeys().subscribe({
      next: v => { this.view.set(v); this.error.set(''); },
      error: () => this.error.set('Your own keys could not be loaded.'),
    });
  }

  draft(p: OwnKeyPlugin, name: string): string { return this.drafts()[p.plugin_id]?.[name] ?? ''; }
  type(p: OwnKeyPlugin, name: string, value: string): void {
    this.drafts.update(all => ({ ...all, [p.plugin_id]: { ...all[p.plugin_id], [name]: value } }));
  }
  anySaved(p: OwnKeyPlugin): boolean { return p.variables.some(x => x.saved); }

  private setProblem(p: OwnKeyPlugin, text: string): void { this.problem.update(all => ({ ...all, [p.plugin_id]: text })); }

  save(p: OwnKeyPlugin): void {
    const values = Object.fromEntries(Object.entries(this.drafts()[p.plugin_id] ?? {}).map(([k, v]) => [k, v.trim()]).filter(([, v]) => v));
    if (!Object.keys(values).length) { this.setProblem(p, 'Enter a key first.'); return; }
    this.setProblem(p, '');
    this.busy.set(true);
    this.api.saveOwnKeys(p.plugin_id, values).subscribe({
      next: () => {
        this.drafts.update(all => ({ ...all, [p.plugin_id]: {} }));
        this.busy.set(false); this.message.set(`Your key for ${p.label} is saved.`); this.load();
      },
      error: e => { this.busy.set(false); this.setProblem(p, e?.error?.error ?? 'The key could not be saved.'); },
    });
  }

  remove(p: OwnKeyPlugin): void {
    this.busy.set(true);
    this.api.deleteOwnKeys(p.plugin_id).subscribe({
      next: () => { this.busy.set(false); this.message.set(`Your key for ${p.label} is removed.`); this.load(); },
      error: () => { this.busy.set(false); this.setProblem(p, 'The key could not be removed.'); },
    });
  }

  allow(p: OwnKeyPlugin, allowed: boolean): void {
    this.busy.set(true);
    this.api.allowOwnKeys(p.plugin_id, allowed).subscribe({
      next: () => {
        this.busy.set(false);
        this.message.set(allowed ? `Users may now add their own key for ${p.label}.` : `Own keys for ${p.label} are off; saved keys are no longer used.`);
        this.load();
      },
      error: () => { this.busy.set(false); this.message.set('The setting could not be saved.'); },
    });
  }
}
