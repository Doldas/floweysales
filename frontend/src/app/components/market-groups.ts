import { ChangeDetectionStrategy, Component, DestroyRef, computed, effect, inject, signal, untracked } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { RouterLink } from '@angular/router';
import { Subject, catchError, of, switchMap } from 'rxjs';

import { COLLECTION_LIMITS, KIND_LABELS, instrumentFrom, type Collection, type CollectionMember } from '../core/collections';
import { CollectionsService } from '../core/collections.service';
import { instrumentKey } from '../core/instrument-ref';
import { MarketState } from '../core/market-state';
import { NotifyService } from '../core/notify.service';
import { describeHttpError } from '../services';
import { GroupDashboard } from './group-dashboard';

const GROUP_KEY = 'fs.markets.group';

function readGroup(): number {
  try { return Number(localStorage.getItem(GROUP_KEY)) || 0; } catch { return 0; }
}

/**
 * The user's collections as groups on the Markets page: pick a group to see
 * its instruments and its dashboard, open an instrument, and add the
 * instrument on screen to a group, or to a new one.
 */
@Component({
  selector: 'fs-market-groups',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink, GroupDashboard],
  template: `
    <section class="card groups-card" aria-labelledby="groups-title">
      <div class="card-body">
        <div class="groups-head">
          <h2 id="groups-title" class="groups-title"><i class="bi bi-collection" aria-hidden="true"></i>Groups</h2>
          <label class="visually-hidden" for="group-select">Group</label>
          <select id="group-select" class="form-select form-select-sm group-select" [value]="activeId()" (change)="choose(+$any($event.target).value)" [disabled]="!groups().length">
            <option value="0">{{ groups().length ? 'Choose a group' : 'No groups yet' }}</option>
            @for (g of groups(); track g.id) {
              <option [value]="g.id" [selected]="g.id === activeId()">{{ g.name }} · {{ g.member_count }}</option>
            }
          </select>
          @if (market.trackedSymbol()) {
            <button type="button" class="btn btn-sm btn-outline-primary" [attr.aria-expanded]="addOpen()" aria-controls="group-add" (click)="toggleAdd()">
              <i class="bi bi-plus-lg" aria-hidden="true"></i>Add {{ market.trackedSymbol() }} to a group
            </button>
          }
          <a class="btn btn-sm btn-ghost" routerLink="/collections">Manage groups</a>
        </div>

        @if (addOpen() && market.trackedSymbol()) {
          <div id="group-add" class="group-add" role="group" [attr.aria-label]="'Add ' + market.trackedSymbol() + ' to a group'">
            <p class="small text-muted mb-2">{{ market.trackedSymbol() }} on {{ market.trackedProvider() }}. A group is one of your collections; adding never changes trackers or the watchlist.</p>
            <div class="group-targets">
              @for (g of groups(); track g.id) {
                @if (containing().has(g.id)) {
                  <span class="fs-chip in-group"><i class="bi bi-check2" aria-hidden="true"></i>{{ g.name }}<span class="visually-hidden"> (already in this group)</span></span>
                } @else {
                  <button type="button" class="fs-chip" (click)="addTo(g)" [disabled]="busy()">{{ g.name }} <small>{{ kindLabels[g.kind] }}</small></button>
                }
              }
            </div>
            <form class="group-new" (submit)="$event.preventDefault(); addToNew()">
              <label class="small" for="group-new-name">New group</label>
              <input id="group-new-name" class="form-control form-control-sm" [attr.maxlength]="limits.name" [value]="newName()" (input)="newName.set($any($event.target).value)" placeholder="e.g. Nordic industrials">
              <button type="submit" class="btn btn-sm btn-primary" [disabled]="busy() || !newName().trim()">Create and add</button>
            </form>
          </div>
        }

        @if (active(); as g) {
          <div class="group-members" role="list" [attr.aria-label]="g.name + ' instruments'">
            @for (m of members(); track m.id) {
              <span role="listitem">
                <button type="button" class="fs-chip member-chip" [class.selected]="isOnScreen(m)" [attr.aria-pressed]="isOnScreen(m)" (click)="open(m)" [title]="'View ' + m.instrument.symbol + ' on ' + m.instrument.provider">
                  {{ m.instrument.symbol }} <small>{{ m.instrument.provider }}</small>
                </button>
              </span>
            } @empty {
              <span class="small text-muted">{{ loading() ? 'Loading instruments…' : 'This group has no instruments yet. Open an instrument and add it.' }}</span>
            }
          </div>
          @if (truncated()) { <p class="small text-muted mb-0">Showing the first {{ members().length }} of {{ g.member_count }} instruments.</p> }
        } @else if (!groups().length && !loading()) {
          <p class="small text-muted mb-0">Groups are your collections. Create one here by adding the instrument on screen, or in Collections.</p>
        }
      </div>
    </section>

    @if (active(); as g) {
      @if (members().length) { <fs-group-dashboard [collection]="g" [members]="members()" /> }
    }
  `,
  styles: [`
    :host { display: flex; flex-direction: column; gap: var(--fs-gap, 1rem); }
    .groups-head { display: flex; flex-wrap: wrap; align-items: center; gap: .5rem; }
    .groups-title { font-size: 1rem; margin: 0 .25rem 0 0; display: inline-flex; gap: .4rem; align-items: center; }
    .group-select { width: auto; min-width: min(12rem, 100%); max-width: 100%; flex: 0 1 auto; }
    .group-add { margin-top: .75rem; padding: .75rem; border: 1px solid var(--fs-border); border-radius: var(--fs-radius-sm, 8px); background: var(--fs-surface-2); }
    .group-targets, .group-members { display: flex; flex-wrap: wrap; gap: .4rem; }
    .group-members { margin-top: .75rem; }
    .group-new { display: flex; flex-wrap: wrap; align-items: center; gap: .5rem; margin-top: .75rem; }
    .group-new input { flex: 1 1 12rem; max-width: 22rem; }
    .in-group { opacity: .8; }
    .member-chip { min-height: 28px; }
    .member-chip.selected { border-color: var(--fs-accent); background: var(--fs-accent-soft); }
    .fs-chip small { opacity: .7; margin-left: .2rem; }
    @media (max-width: 30rem) { .group-select { flex: 1 1 100%; } }
  `],
})
export class MarketGroups {
  readonly market = inject(MarketState);
  private readonly api = inject(CollectionsService);
  private readonly notify = inject(NotifyService);
  private readonly destroyRef = inject(DestroyRef);

  readonly kindLabels = KIND_LABELS;
  readonly limits = COLLECTION_LIMITS;
  readonly groups = signal<Collection[]>([]);
  readonly activeId = signal(readGroup());
  readonly active = computed(() => this.groups().find(g => g.id === this.activeId()) ?? null);
  readonly members = signal<CollectionMember[]>([]);
  readonly loading = signal(true);
  readonly busy = signal(false);
  readonly addOpen = signal(false);
  readonly newName = signal('');
  /** Ids of the groups that already hold the instrument on screen. */
  readonly containing = signal<Set<number>>(new Set());
  readonly truncated = computed(() => (this.active()?.member_count ?? 0) > this.members().length);

  private readonly loadMembers$ = new Subject<number>();

  constructor() {
    this.loadMembers$.pipe(
      switchMap(id => (id ? this.api.members(id, 500).pipe(catchError(() => of(null))) : of(null))),
      takeUntilDestroyed(this.destroyRef),
    ).subscribe(page => {
      this.loading.set(false);
      this.members.set(page?.members ?? []);
    });
    this.reloadGroups();
    // A new instrument on screen: find the groups that already hold it.
    effect(() => {
      const symbol = this.market.trackedSymbol();
      const provider = this.market.trackedProvider();
      untracked(() => {
        this.containing.set(new Set());
        if (symbol && this.addOpen()) this.loadContaining(provider, symbol);
      });
    });
  }

  private reloadGroups(after?: () => void): void {
    this.api.list({ archived: 'exclude', sort: 'name' }).pipe(takeUntilDestroyed(this.destroyRef)).subscribe({
      next: list => {
        this.groups.set(list);
        if (this.activeId() && !list.some(g => g.id === this.activeId())) this.choose(0);
        else if (this.activeId()) this.loadMembers$.next(this.activeId());
        else this.loading.set(false);
        after?.();
      },
      error: () => this.loading.set(false),
    });
  }

  private loadContaining(provider: string, symbol: string): void {
    this.api.containing(provider, symbol).subscribe({
      next: list => this.containing.set(new Set(list.map(c => c.id))),
      error: () => this.containing.set(new Set()),
    });
  }

  choose(id: number): void {
    this.activeId.set(id);
    try { id ? localStorage.setItem(GROUP_KEY, String(id)) : localStorage.removeItem(GROUP_KEY); } catch { /* the choice lasts for this visit */ }
    this.members.set([]);
    this.loading.set(!!id);
    this.loadMembers$.next(id);
  }

  toggleAdd(): void {
    const open = !this.addOpen();
    this.addOpen.set(open);
    if (open) this.loadContaining(this.market.trackedProvider(), this.market.trackedSymbol());
  }

  isOnScreen(m: CollectionMember): boolean {
    return instrumentKey(m.instrument) === instrumentKey({ symbol: this.market.trackedSymbol(), provider: this.market.trackedProvider() });
  }

  open(m: CollectionMember): void {
    this.market.view({ symbol: m.instrument.symbol, provider: m.instrument.provider });
  }

  private selectedInstrument() {
    return instrumentFrom(this.market.trackedProvider(), {
      symbol: this.market.trackedSymbol(), name: this.market.trackedName(), exchange: this.market.trackedExchange(),
    });
  }

  addTo(g: Collection): void {
    const instrument = this.selectedInstrument();
    this.busy.set(true);
    this.api.addMember(g.id, { instrument }).subscribe({
      next: () => {
        this.busy.set(false);
        this.notify.success(`${instrument.symbol} added to ${g.name}.`);
        this.containing.update(s => new Set([...s, g.id]));
        this.reloadGroups();
      },
      error: err => {
        this.busy.set(false);
        this.notify.error(`Could not add ${instrument.symbol} to ${g.name}: ${describeHttpError(err)}`);
      },
    });
  }

  addToNew(): void {
    const name = this.newName().trim();
    if (!name) return;
    this.busy.set(true);
    this.api.create({ name, kind: 'watch' }).subscribe({
      next: g => {
        this.newName.set('');
        this.busy.set(false);
        this.activeId.set(g.id);
        try { localStorage.setItem(GROUP_KEY, String(g.id)); } catch { /* the choice lasts for this visit */ }
        this.groups.update(list => [...list, g]);
        this.addTo(g);
      },
      error: err => {
        this.busy.set(false);
        this.notify.error(`Could not create ${name}: ${describeHttpError(err)}`);
      },
    });
  }
}
