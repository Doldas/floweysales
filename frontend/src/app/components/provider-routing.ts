import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';

import { MarketService } from '../services';
import type { RoutingDomain, RoutingSource, RoutingView } from '../models';
import { type PolicyForm, formFrom, groupDomains, moveFallback, policyBody, policyLine, policyProblems, sourceState, sourcesFor, stateLabel, toggleFallback } from './provider-routing-view';

/**
 * Settings → Providers. Lists every source FloweySales can get data from,
 * grouped by what it provides, and lets an administrator choose a primary and an
 * ordered fallback per domain. Installing a plugin (Settings → Plugins) is a
 * separate step from deciding which installed source serves a domain.
 */
@Component({
  selector: 'fse-provider-routing',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [FormsModule],
  host: { class: 'fs-native-forms' },
  template: `
    <section class="pr" aria-labelledby="pr-title">
      <h3 id="pr-title">Providers</h3>
      @if (view(); as v) {
        <p class="muted">{{ v.notice }}</p>
        @if (!v.can_edit) { <p class="muted" role="note">You can see which sources exist. Only an administrator can change the routing.</p> }
        @if (message()) { <p class="alert alert-info py-2" role="status">{{ message() }}</p> }
        @for (g of groups(); track g.group) {
          <section class="pr-group" [attr.aria-label]="g.group + ' providers'">
            <h4>{{ g.group }}</h4>
            @for (d of g.domains; track d.id) {
              <article class="pr-domain" [attr.aria-label]="d.label" [attr.data-domain]="d.id">
                <header>
                  <strong>{{ d.label }}</strong>
                  <span class="muted">{{ d.sources }} {{ d.sources === 1 ? 'source' : 'sources' }}</span>
                </header>
                <p class="muted pr-line">{{ line(d) }}</p>
                @if (sources(d).routable.length === 0 && sources(d).managed.length === 0) {
                  <p class="muted">No installed source provides this. Install a plugin under Settings → Plugins.</p>
                }
                <ul class="pr-sources">
                  @for (s of sources(d).routable; track s.id) {
                    <li><span class="pr-name">{{ s.label }}</span> <span class="pr-state" [attr.data-state]="state(s)">{{ stateText(s) }}</span>
                      @if (s.experimental) { <span class="muted">experimental</span> }
                      @if (s.restrictions) { <small class="muted"> {{ s.restrictions }}</small> }</li>
                  }
                  @for (s of sources(d).managed; track s.id) {
                    <li><span class="pr-name">{{ s.label }}</span> <span class="pr-state" [attr.data-state]="state(s)">{{ stateText(s) }}</span>
                      <small class="muted"> Managed in {{ s.managed_by }}.</small></li>
                  }
                </ul>
                @if (v.can_edit && sources(d).routable.length) {
                  <form class="pr-form" (submit)="$event.preventDefault(); save(d)">
                    <label>Primary source
                      <select [ngModel]="form(d).primary" (ngModelChange)="setPrimary(d, $event)" [name]="'primary-' + d.id">
                        <option value="">Choose…</option>
                        @for (s of sources(d).routable; track s.id) { <option [value]="s.id">{{ s.label }}</option> }
                      </select>
                    </label>
                    @if (d.no_fallback) {
                      <p class="muted" role="note">Fallback is off for this domain: {{ d.why }}</p>
                    } @else {
                      <label class="pr-check"><input type="checkbox" [ngModel]="form(d).noFallback" (ngModelChange)="setNoFallback(d, $event)" [name]="'nofb-' + d.id" /> No fallback: fail instead of asking another source</label>
                      @if (!form(d).noFallback) {
                        <fieldset class="pr-fallback">
                          <legend>Fallback, tried in this order</legend>
                          @for (s of sources(d).routable; track s.id) {
                            @if (s.id !== form(d).primary) {
                              <div class="pr-row">
                                <label><input type="checkbox" [checked]="form(d).fallback.includes(s.id)" (change)="toggle(d, s.id)" /> {{ s.label }}</label>
                                @if (form(d).fallback.includes(s.id)) {
                                  <button type="button" class="btn btn-sm btn-outline-secondary" (click)="move(d, s.id, -1)" [attr.aria-label]="'Move ' + s.label + ' earlier'">↑</button>
                                  <button type="button" class="btn btn-sm btn-outline-secondary" (click)="move(d, s.id, 1)" [attr.aria-label]="'Move ' + s.label + ' later'">↓</button>
                                }
                              </div>
                            }
                          }
                        </fieldset>
                      }
                    }
                    @if (problems(d).length && touched()[d.id]) { <ul class="pr-errors" role="alert">@for (p of problems(d); track p) { <li>{{ p }}</li> }</ul> }
                    <div class="pr-actions">
                      <button type="submit" class="btn btn-sm btn-primary" [disabled]="busy()">Save routing</button>
                      @if (v.policies[d.id]) { <button type="button" class="btn btn-sm btn-outline-secondary" [disabled]="busy()" (click)="clear(d)">Remove policy</button> }
                    </div>
                  </form>
                }
              </article>
            }
          </section>
        }
      } @else if (error()) {
        <p class="alert alert-warning" role="alert">{{ error() }}</p>
      } @else {
        <p class="muted" role="status">Loading providers…</p>
      }
    </section>
  `,
  styles: [`
    .pr { display: grid; gap: 0.75rem; }
    .pr-group { display: grid; gap: 0.6rem; }
    .pr-group h4 { margin: 0.5rem 0 0; font-size: 1rem; }
    .pr-domain { border: 1px solid var(--fs-border); border-radius: 0.5rem; padding: 0.7rem 0.9rem; display: grid; gap: 0.4rem; }
    .pr-domain header { display: flex; gap: 0.75rem; align-items: baseline; justify-content: space-between; }
    .pr-line { margin: 0; font-size: 0.8rem; }
    .pr-sources { margin: 0; padding-left: 1.1rem; font-size: 0.85rem; }
    .pr-state { font-size: 0.72rem; font-weight: 600; padding: 0 0.4rem; border: 1px solid currentColor; border-radius: 0.6rem; }
    .pr-state[data-state='ready'] { color: var(--fs-pos); }
    .pr-state[data-state='needs_key'], .pr-state[data-state='disabled'] { color: var(--fs-warn); }
    .pr-form { display: grid; gap: 0.5rem; max-width: 34rem; margin-top: 0.3rem; }
    .pr-form label { display: grid; gap: 0.2rem; font-size: 0.8rem; }
    .pr-check { display: flex !important; gap: 0.5rem; align-items: center; }
    .pr-fallback { border: 1px solid var(--fs-border); border-radius: 0.4rem; padding: 0.4rem 0.7rem; display: grid; gap: 0.3rem; }
    .pr-fallback legend { font-size: 0.78rem; padding: 0 0.3rem; width: auto; margin: 0; }
    .pr-row { display: flex; gap: 0.5rem; align-items: center; }
    .pr-row label { display: flex; gap: 0.4rem; align-items: center; flex: 1; }
    .pr-errors { margin: 0; padding-left: 1rem; font-size: 0.8rem; color: var(--fs-neg); }
    .pr-actions { display: flex; gap: 0.5rem; flex-wrap: wrap; }
  `],
})
export class ProviderRoutingComponent {
  private readonly api = inject(MarketService);

  readonly view = signal<RoutingView | null>(null);
  readonly error = signal('');
  readonly message = signal('');
  readonly busy = signal(false);
  readonly touched = signal<Record<string, boolean>>({});
  private readonly forms = signal<Record<string, PolicyForm>>({});

  readonly groups = computed(() => groupDomains(this.view()?.domains ?? []));

  constructor() { this.load(); }

  load(): void {
    this.api.getProviderRouting().subscribe({
      next: v => {
        this.view.set(v);
        this.forms.set(Object.fromEntries(v.domains.map(d => [d.id, formFrom(v.policies[d.id], d)])));
        this.error.set('');
      },
      error: () => this.error.set('The provider list could not be loaded.'),
    });
  }

  sources(d: RoutingDomain): { routable: RoutingSource[]; managed: RoutingSource[] } { return sourcesFor(d.id, this.view()?.sources ?? []); }
  form(d: RoutingDomain): PolicyForm { return this.forms()[d.id] ?? formFrom(undefined, d); }
  problems(d: RoutingDomain): string[] { return policyProblems(this.form(d), this.sources(d).routable); }
  line(d: RoutingDomain): string { return policyLine(this.view()?.policies[d.id], d, this.view()?.sources ?? []); }
  state(s: RoutingSource): string { return sourceState(s); }
  stateText(s: RoutingSource): string { return stateLabel(s); }

  private patch(d: RoutingDomain, f: PolicyForm): void { this.forms.update(all => ({ ...all, [d.id]: f })); }
  setPrimary(d: RoutingDomain, id: string): void {
    const f = this.form(d);
    this.patch(d, { ...f, primary: id, fallback: f.fallback.filter(x => x !== id) });
  }
  setNoFallback(d: RoutingDomain, on: boolean): void { this.patch(d, { ...this.form(d), noFallback: on }); }
  toggle(d: RoutingDomain, id: string): void { this.patch(d, toggleFallback(this.form(d), id)); }
  move(d: RoutingDomain, id: string, delta: -1 | 1): void { this.patch(d, moveFallback(this.form(d), id, delta)); }

  save(d: RoutingDomain): void {
    this.touched.update(t => ({ ...t, [d.id]: true }));
    if (this.problems(d).length) return;
    this.busy.set(true);
    this.api.saveProviderPolicy(d.id, policyBody(this.form(d))).subscribe({
      next: () => { this.busy.set(false); this.message.set(`Routing for ${d.label.toLowerCase()} saved.`); this.load(); },
      error: e => { this.busy.set(false); this.message.set(e?.error?.error ?? 'The routing could not be saved.'); },
    });
  }

  clear(d: RoutingDomain): void {
    this.busy.set(true);
    this.api.clearProviderPolicy(d.id).subscribe({
      next: () => { this.busy.set(false); this.message.set(`Routing for ${d.label.toLowerCase()} removed.`); this.load(); },
      error: () => { this.busy.set(false); this.message.set('The routing could not be removed.'); },
    });
  }
}
