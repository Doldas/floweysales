import { ChangeDetectionStrategy, Component, DestroyRef, ElementRef, HostListener, computed, inject, input, signal } from '@angular/core';
import { Router } from '@angular/router';
import { Subject, debounceTime, switchMap, tap } from 'rxjs';

import { MarketState } from '../core/market-state';
import type { ProviderId, SymbolInfo } from '../models';

let nextId = 0;

/**
 * Ticker search combobox. Typing searches the selected provider; choosing a
 * result (or pressing Enter on a typed symbol) makes it the active instrument.
 */
@Component({
  selector: 'fs-symbol-search',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="search-box" [class.compact]="compact()">
      <i class="bi bi-search search-icon" aria-hidden="true"></i>
      <input #field type="text" class="search-input" [id]="id + '-input'" autocomplete="off" spellcheck="false"
             role="combobox" aria-autocomplete="list" [attr.aria-expanded]="open()" [attr.aria-controls]="id + '-list'"
             [attr.aria-activedescendant]="active() >= 0 ? id + '-opt-' + active() : null"
             [attr.aria-label]="'Search tickers on ' + market.provider()"
             [placeholder]="placeholder()" [value]="query()"
             (input)="onInput($any($event.target).value)" (keydown)="onKeydown($event)" (focus)="onFocus()">
      @if (searching()) { <span class="spinner-border spinner-border-sm text-secondary me-1" aria-hidden="true"></span> }
      <select class="provider-select" [value]="market.provider()" (change)="setProvider($any($event.target).value)"
              aria-label="Market data provider">
        @for (p of market.providers(); track p.id) { <option [value]="p.id">{{ p.label }}</option> }
        @if (!installedProvider()) {
          <option [value]="market.provider()">{{ market.provider() }} (not installed)</option>
        }
      </select>
    </div>
    @if (open()) {
      <ul class="results" role="listbox" [id]="id + '-list'" aria-label="Ticker results">
        @for (r of results(); track r.symbol + r.exchange; let i = $index) {
          <li role="option" [id]="id + '-opt-' + i" [attr.aria-selected]="i === active()" [class.active]="i === active()"
              (mousedown)="$event.preventDefault()" (click)="choose(r)" (mouseenter)="active.set(i)">
            <span class="r-sym">{{ r.symbol }}</span>
            <span class="r-name">{{ r.name }}</span>
            <span class="r-type">{{ r.type }}@if (r.exchange) { · {{ r.exchange }} }</span>
          </li>
        } @empty {
          <li class="empty" role="presentation">
            @if (query().trim().length < 2) { Type at least two characters } @else if (searching()) { Searching… } @else { No matches on {{ market.provider() }}. Press Enter to try “{{ query().trim().toUpperCase() }}”. }
          </li>
        }
      </ul>
    }
  `,
  styles: [`
    :host { position: relative; display: block; min-width: 0; }
    .search-box {
      display: flex; align-items: center; gap: 0.4rem; height: 2.3rem; padding: 0 0.3rem 0 0.7rem;
      border: 1px solid var(--fs-border-strong); border-radius: var(--bs-border-radius); background: var(--fs-surface);
      transition: border-color .15s, box-shadow .15s;
    }
    .search-box:focus-within { border-color: var(--fs-accent); box-shadow: var(--fs-focus); }
    .search-icon { color: var(--fs-muted); }
    .search-input { flex: 1 1 auto; min-width: 0; border: 0; outline: 0; background: transparent; color: var(--fs-text); font-size: .875rem; }
    .search-input::placeholder { color: var(--fs-muted); }
    .provider-select {
      flex: 0 0 auto; border: 0; border-left: 1px solid var(--fs-border); background: transparent; color: var(--fs-muted);
      font-size: .78rem; padding: .25rem .3rem .25rem .55rem; max-width: 7.5rem; outline: 0; cursor: pointer;
    }
    .provider-select:focus-visible { color: var(--fs-text); box-shadow: var(--fs-focus); border-radius: 4px; }
    .provider-select option { background: var(--fs-surface); color: var(--fs-text); }
    .results {
      position: absolute; z-index: 1060; top: calc(100% + 6px); left: 0; right: 0; max-height: 340px; overflow: auto;
      list-style: none; margin: 0; padding: .35rem; background: var(--fs-surface); border: 1px solid var(--fs-border);
      border-radius: var(--bs-border-radius-lg); box-shadow: var(--fs-shadow-lg); min-width: min(360px, calc(100vw - 2rem));
    }
    .results li { display: grid; grid-template-columns: auto 1fr; column-gap: .6rem; padding: .45rem .6rem; border-radius: var(--bs-border-radius); cursor: pointer; }
    .results li.active { background: var(--fs-accent-soft); }
    .r-sym { font-weight: 650; font-size: .85rem; }
    .r-name { font-size: .85rem; color: var(--fs-text-2); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .r-type { grid-column: 1 / -1; font-size: .72rem; color: var(--fs-muted); }
    .results li.empty { display: block; cursor: default; color: var(--fs-muted); font-size: .82rem; }
  `],
})
export class SymbolSearchComponent {
  readonly market = inject(MarketState);
  private readonly router = inject(Router);
  private readonly host = inject(ElementRef<HTMLElement>);

  readonly placeholder = input('Search ticker, e.g. NVDA or VOLV-B');
  readonly compact = input(false);
  /** Navigate to Markets after choosing, unless already on an instrument page. */
  readonly navigate = input(true);

  readonly id = `fs-symbol-search-${nextId++}`;
  readonly query = signal('');
  readonly results = signal<SymbolInfo[]>([]);
  readonly searching = signal(false);
  readonly open = signal(false);
  readonly active = signal(-1);
  /** Whether the selected provider is installed; until the list loads it is assumed to be. */
  readonly installedProvider = computed(
    () => !this.market.providersLoaded() || this.market.providers().some(p => p.id === this.market.provider()),
  );

  private readonly typed = new Subject<string>();

  constructor() {
    const sub = this.typed
      .pipe(
        debounceTime(250),
        tap(() => this.searching.set(true)),
        switchMap(q => this.market.search(q)),
      )
      .subscribe({
        next: list => {
          this.searching.set(false);
          this.results.set(list.slice(0, 12));
          this.active.set(list.length ? 0 : -1);
        },
        error: () => this.searching.set(false),
      });
    inject(DestroyRef).onDestroy(() => sub.unsubscribe());
  }

  onFocus(): void {
    if (this.query().trim()) this.open.set(true);
  }

  onInput(value: string): void {
    this.query.set(value);
    this.open.set(!!value.trim());
    if (value.trim().length >= 2) this.typed.next(value);
    else this.results.set([]);
  }

  setProvider(value: string): void {
    this.market.setProvider(value as ProviderId);
    if (this.query().trim().length >= 2) this.typed.next(this.query());
  }

  onKeydown(event: KeyboardEvent): void {
    const n = this.results().length;
    switch (event.key) {
      case 'ArrowDown':
        event.preventDefault();
        this.open.set(true);
        if (n) this.active.set((this.active() + 1) % n);
        break;
      case 'ArrowUp':
        event.preventDefault();
        if (n) this.active.set((this.active() - 1 + n) % n);
        break;
      case 'Enter': {
        event.preventDefault();
        const hit = this.open() ? this.results()[this.active()] : undefined;
        if (hit) this.choose(hit);
        else if (this.query().trim()) this.submit(this.query());
        break;
      }
      case 'Escape':
        this.open.set(false);
        break;
    }
  }

  choose(info: SymbolInfo): void {
    this.market.track(info);
    this.done();
  }

  private submit(symbol: string): void {
    this.market.track(symbol);
    this.done();
  }

  private done(): void {
    this.open.set(false);
    this.query.set('');
    this.results.set([]);
    (this.host.nativeElement.querySelector('input') as HTMLInputElement | null)?.blur();
    const url = this.router.url;
    if (this.navigate() && !['/markets', '/financials', '/portfolio'].some(p => url.startsWith(p))) void this.router.navigateByUrl('/markets');
  }

  @HostListener('document:click', ['$event'])
  onDocumentClick(event: MouseEvent): void {
    if (!this.host.nativeElement.contains(event.target as Node)) this.open.set(false);
  }
}
