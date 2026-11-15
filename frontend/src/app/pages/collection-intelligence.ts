import { ChangeDetectionStrategy, Component, DestroyRef, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import type { Observable } from 'rxjs';

import type { Collection } from '../core/collections';
import {
  LINK_LABELS, STATUS_LABELS, TAXONOMY_DIMENSIONS, TAXONOMY_LABELS, canFetchProfiles, compactNumber, draftFrom, factSourceText, factText,
  labelMap, linkMembers, overrideBody, overrideProblem, profileCoverageText, refreshSummary, taxonomyComposition,
  type Fact, type Intelligence, type MemberIntelligence, type OverrideDraft, type ProfileStatus, type ProfileView, type Taxonomy,
  type TaxonomyDimension,
} from '../core/instruments';
import { InstrumentsService } from '../core/instruments.service';
import { NotifyService } from '../core/notify.service';
import { describeHttpError } from '../services';
import { formatUtcDateTime } from '../ui/datetime';

/** Rounds of "Fetch profiles" per click; each round is a bounded server batch. */
const MAX_ROUNDS = 10;

/**
 * Profiles, classification and thematic composition of one collection
 * (#441 phase 2). Reads the server's cache only; providers are asked when
 * the user fetches profiles or opens an instrument's details.
 */
@Component({
  selector: 'fs-collection-intelligence',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
<section class="card" aria-labelledby="intel-title">
  <div class="card-header comp-head">
    <h2 class="card-title me-auto" id="intel-title">Classification</h2>
    <div class="fs-segmented" role="group" aria-label="Classify by">
      @for (d of dimensions; track d) {
        <button type="button" [attr.aria-pressed]="dimension() === d" (click)="dimension.set(d)">{{ dimensionLabels[d] }}</button>
      }
    </div>
    <button type="button" class="btn btn-outline-secondary btn-sm" (click)="fetchProfiles()" [disabled]="fetching() || !canFetch()">
      <i class="bi bi-cloud-download" aria-hidden="true"></i> {{ fetching() ? 'Fetching…' : 'Fetch profiles' }}
    </button>
  </div>
  <div class="card-body">
    @if (error(); as err) {
      <div class="alert alert-warning mb-0" role="alert">{{ err }}</div>
    } @else if (intel(); as i) {
      <p class="comp-note" id="intel-note">
        <i class="bi bi-info-circle" aria-hidden="true"></i>
        @if (comp().mode === 'count') {
          Number of instruments by {{ dimensionLabels[dimension()].toLowerCase() }}.
          @if (dimension() === 'theme') { An instrument can have several themes, so the shares can add up to more than 100%. }
        } @else {
          Hypothetical weights by {{ dimensionLabels[dimension()].toLowerCase() }}, as you entered them. Not a holding and not advice.
        }
        Classes come from your own classification first, then from each instrument's own provider profile.
      </p>
      @if (comp().rows.length) {
        <table class="table comp-table" aria-describedby="intel-note">
          <caption class="visually-hidden">{{ comp().mode === 'count' ? 'Instruments' : 'Weight' }} by {{ dimensionLabels[dimension()] }}</caption>
          <thead><tr><th scope="col">{{ dimensionLabels[dimension()] }}</th><th scope="col" class="bar-col"><span class="visually-hidden">Bar</span></th><th scope="col" class="num">{{ comp().mode === 'count' ? 'Instruments' : 'Weight' }}</th></tr></thead>
          <tbody>
            @for (r of comp().rows; track r.key) {
              <tr [class.unknown]="!r.key">
                <th scope="row">{{ r.label }}</th>
                <td class="bar-col"><span class="bar" [style.width.%]="r.pct" aria-hidden="true"></span></td>
                <td class="num tabular">
                  @if (comp().mode === 'count') { {{ r.count }} <span class="muted">· {{ r.pct }}%</span> }
                  @else { {{ r.weight ?? 0 }}% <span class="muted">· {{ r.count }} instr.</span> }
                </td>
              </tr>
            }
            @if (comp().mode === 'weight' && (comp().unallocated ?? 0) > 0) {
              <tr class="unknown">
                <th scope="row">Unallocated</th>
                <td class="bar-col"><span class="bar hatched" [style.width.%]="comp().unallocated" aria-hidden="true"></span></td>
                <td class="num tabular">{{ comp().unallocated }}%</td>
              </tr>
            }
          </tbody>
        </table>
      } @else if (dimension() === 'theme' && i.members.length) {
        <p class="muted">No themes yet. Open an instrument's details to add your own.</p>
      }
      <p class="muted small mb-0" data-testid="profile-coverage">{{ coverageText() }}</p>
      @if (refreshLine(); as line) { <p class="small mb-0 mt-1" role="status">{{ line }}</p> }
      @if (i.coverage.no_source) {
        <p class="muted small mb-0 mt-1">
          <i class="bi bi-slash-circle" aria-hidden="true"></i>
          @if (i.profile_providers.length) { Profiles come from {{ i.profile_providers.join(', ') }}. } @else { No installed provider offers profiles. }
          Instruments on other providers have none: FloweySales never copies another provider's profile onto them. You can still classify them yourself.
        </p>
      }
    } @else {
      <p class="muted mb-0">Loading…</p>
    }
  </div>
</section>

@if (intel(); as i) {
  @if (i.links.length) {
    <section class="card" aria-labelledby="links-title">
      <div class="card-header"><h2 class="card-title" id="links-title">Identity</h2></div>
      <ul class="list-unstyled links mb-0">
        @for (l of i.links; track $index) {
          <li>
            <span class="fs-status no-dot" [class.is-success]="l.kind === 'same_listing'" [class.is-info]="l.kind === 'same_security'"
              [class.is-warning]="l.kind === 'same_ticker' || l.kind === 'different_instruments'">{{ linkLabels[l.kind] }}</span>
            <span class="link-members">{{ membersOf(l) }}</span>
            @if (l.isin) { <span class="muted small">ISIN {{ l.isin }}</span> }
            <div class="muted small">{{ l.reason }}</div>
          </li>
        }
      </ul>
    </section>
  }

  @if (i.members.length) {
    <section class="card" aria-labelledby="cls-title">
      <div class="card-header"><h2 class="card-title" id="cls-title">Instrument classes</h2></div>
      <div class="table-responsive">
        <table class="table table-hover mb-0 cls">
          <thead><tr><th scope="col">Instrument</th><th scope="col">Profile</th><th scope="col">Asset class</th><th scope="col">Sector</th><th scope="col">Region</th><th scope="col">Themes</th><th scope="col"><span class="visually-hidden">Actions</span></th></tr></thead>
          <tbody>
            @for (m of i.members; track m.member_id) {
              <tr [class.active-row]="detail()?.member_id === m.member_id">
                <td><span class="sym">{{ m.key.symbol }}</span>&ngsp;<span class="muted small">{{ m.key.provider }}</span>@if (m.name) { <div class="muted small">{{ m.name }}</div> }</td>
                <td>
                  <span class="fs-status no-dot" [class.is-success]="m.profile_status === 'ok'" [class.is-warning]="m.profile_status === 'not_found'">{{ statusLabels[m.profile_status] }}</span>
                  @if (m.stale) { <span class="fs-status no-dot is-warning ms-1" title="Past its refresh date">Stale</span> }
                </td>
                <td [class.user]="m.classification.asset_class.source === 'user'" [attr.title]="sourceOf(m.classification.asset_class)">{{ fact(m.classification.asset_class, 'asset_class') }}</td>
                <td [class.user]="m.classification.sector.source === 'user'" [attr.title]="sourceOf(m.classification.sector)">{{ fact(m.classification.sector, 'sector') }}</td>
                <td [class.user]="m.classification.region.source === 'user'" [attr.title]="sourceOf(m.classification.region)">{{ fact(m.classification.region, 'region') }}</td>
                <td>@for (t of m.classification.themes; track t) { <span class="fs-status no-dot is-accent me-1">{{ t }}</span> } @empty { <span class="muted">—</span> }</td>
                <td class="row-actions">
                  <button type="button" class="btn btn-link btn-sm" (click)="openDetail(m)" [attr.aria-label]="'Details of ' + m.key.symbol + ' on ' + m.key.provider">Details</button>
                </td>
              </tr>
            }
          </tbody>
        </table>
      </div>
      <p class="muted small px-3 py-2 mb-0"><span class="user-key">Highlighted</span> values are your own classification. Hover a value to see where it came from.</p>
    </section>
  }
}

@if (detail(); as m) {
  <section class="card" aria-labelledby="detail-title">
    <div class="card-header comp-head">
      <h2 class="card-title me-auto" id="detail-title">{{ m.key.symbol }}&ngsp;<span class="muted small">{{ m.key.provider }}</span></h2>
      <button type="button" class="btn btn-outline-secondary btn-sm" (click)="loadProfile(true)" [disabled]="profileBusy() || view()?.profile?.status === 'no_source'">
        <i class="bi bi-arrow-clockwise" aria-hidden="true"></i> Refresh profile
      </button>
      <button type="button" class="btn-close" aria-label="Close details" (click)="detail.set(null)"></button>
    </div>
    <div class="card-body detail">
      @if (view(); as v) {
        <div>
          <h3 class="h6">Provider profile</h3>
          @switch (v.profile.status) {
            @case ('no_source') { <p class="muted mb-0">{{ m.key.provider }} offers no instrument profiles. FloweySales does not fill this in from another provider.</p> }
            @case ('not_found') { <p class="muted mb-0">{{ m.key.provider }} has no profile for {{ m.key.symbol }}.</p> }
            @case ('unavailable') { <p class="text-warning mb-0">The profile could not be fetched: {{ v.profile.error || 'the provider did not answer' }}. Try again later.</p> }
          }
          @if (v.profile.profile; as p) {
            @if (p.name) { <p class="fw-semibold mb-1">{{ p.name }}</p> }
            @if (p.description) { <p class="desc">{{ p.description }}</p> }
            <dl class="facts">
              @for (f of profileFacts(); track f[0]) { <dt>{{ f[0] }}</dt><dd>{{ f[1] }}</dd> }
            </dl>
            @if (p.fund?.top_holdings?.length) {
              <h4 class="h6 mt-2">Top holdings</h4>
              <ul class="small mb-0">@for (h of p.fund!.top_holdings!; track $index) { <li>{{ h.name || h.symbol }}@if (h.weight !== undefined) { · {{ h.weight }}% }</li> }</ul>
            }
          }
          @if (v.profile.status === 'ok' || v.profile.status === 'not_found') {
            <p class="muted small mt-2 mb-0" data-testid="profile-provenance">
              Source: {{ v.profile.provenance?.provider || m.key.provider }}@if (v.profile.profile?.as_of) { · as of {{ utc(v.profile.profile!.as_of!) }} }@if (v.profile.retrieved_at) { · retrieved {{ utc(v.profile.retrieved_at) }} }
              @if (v.profile.stale) { · <span class="text-warning">past its refresh date</span> }
              @if (sourceUrl(); as url) { · <a [href]="url" target="_blank" rel="noopener noreferrer">provider page</a> }
            </p>
          }
        </div>

        <form class="override" (submit)="$event.preventDefault(); saveOverride()">
          <h3 class="h6">Your classification</h3>
          <p class="muted small">Private to you. Empty fields keep the provider's value. The provider's profile is never changed.</p>
          <div class="form-grid">
            <div>
              <label class="form-label" for="ov-asset">Asset class</label>
              <select id="ov-asset" class="form-select form-select-sm" [value]="draft().asset_class" (change)="patch('asset_class', $any($event.target).value)">
                <option value="">From the profile</option>
                @for (t of taxonomy()?.asset_classes ?? []; track t.id) { <option [value]="t.id" [selected]="draft().asset_class === t.id">{{ t.label }}</option> }
              </select>
            </div>
            <div>
              <label class="form-label" for="ov-sector">Sector</label>
              <select id="ov-sector" class="form-select form-select-sm" [value]="draft().sector" (change)="patch('sector', $any($event.target).value)">
                <option value="">From the profile</option>
                @for (t of taxonomy()?.sectors ?? []; track t.id) { <option [value]="t.id" [selected]="draft().sector === t.id">{{ t.label }}</option> }
              </select>
            </div>
            <div>
              <label class="form-label" for="ov-industry">Industry</label>
              <input id="ov-industry" class="form-control form-control-sm" [value]="draft().industry" (input)="patch('industry', $any($event.target).value)"
                [attr.maxlength]="taxonomy()?.limits?.industry_length" [placeholder]="v.classification.industry.value || v.classification.industry.raw || ''">
            </div>
            <div>
              <label class="form-label" for="ov-country">Country (ISO code)</label>
              <input id="ov-country" class="form-control form-control-sm" [value]="draft().country" (input)="patch('country', $any($event.target).value)" maxlength="2"
                [placeholder]="v.classification.country.value || ''" aria-describedby="ov-country-help">
              <div id="ov-country-help" class="form-text">The region follows the country.</div>
            </div>
            <div class="span-2">
              <label class="form-label" for="ov-themes">Themes</label>
              <input id="ov-themes" class="form-control form-control-sm" [value]="draft().themes" (input)="patch('themes', $any($event.target).value)" placeholder="AI, Energy transition" aria-describedby="ov-themes-help">
              <div id="ov-themes-help" class="form-text">Comma-separated, at most {{ taxonomy()?.limits?.themes ?? 10 }}.</div>
            </div>
          </div>
          @if (problem(); as p) { <p class="text-danger small mt-2 mb-0" role="alert">{{ p }}</p> }
          <div class="form-actions mt-2">
            <button type="submit" class="btn btn-primary btn-sm" [disabled]="profileBusy() || !!problem()">Save classification</button>
            @if (v.override) {
              <button type="button" class="btn btn-outline-secondary btn-sm" (click)="resetOverride()" [disabled]="profileBusy()">Reset to provider</button>
              @if (v.override.updated_at) { <span class="muted small">Saved {{ utc(v.override.updated_at) }}</span> }
            }
          </div>
        </form>
      } @else {
        <p class="muted mb-0">Loading the profile…</p>
      }
    </div>
  </section>
}
  `,
  styles: [`
    :host { display: grid; gap: 1rem; min-width: 0; }
    .comp-head { display: flex; flex-wrap: wrap; align-items: center; gap: 0.5rem; }
    .comp-note { font-size: 0.8rem; color: var(--fs-muted); margin: 0 0 0.6rem; }
    .comp-table { th[scope='row'] { font-weight: 500; white-space: nowrap; } .num { text-align: right; white-space: nowrap; } }
    .bar-col { width: 55%; }
    .bar { display: block; height: 0.6rem; min-width: 2px; border-radius: 0 4px 4px 0; background: var(--fs-accent); }
    .unknown .bar { background: var(--fs-border-strong); }
    .bar.hatched { background: repeating-linear-gradient(45deg, var(--fs-border-strong) 0 3px, transparent 3px 6px); border: 1px solid var(--fs-border-strong); }
    .unknown th { color: var(--fs-muted); }
    .links { display: grid; gap: 0.6rem; padding: 0.8rem 1rem; }
    .links li { display: flex; flex-wrap: wrap; align-items: baseline; gap: 0.2rem 0.6rem; }
    .links li > div { flex-basis: 100%; }
    .link-members { font-weight: 600; overflow-wrap: anywhere; }
    .cls { .sym { font-weight: 650; } td, th { vertical-align: middle; } }
    .user, .user-key { background: var(--fs-accent-soft); }
    .user-key { padding: 0 0.25rem; border-radius: 3px; }
    .active-row { background: var(--fs-accent-soft); }
    .active-row .btn-link { --bs-btn-color: var(--fs-accent-text); --bs-btn-hover-color: var(--fs-accent-text); --bs-btn-active-color: var(--fs-accent-text); color: var(--fs-accent-text); } /* the accent alone is below 4.5:1 on the soft background */
    .row-actions { white-space: nowrap; text-align: right; }
    .detail { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 1.25rem; }
    .desc { font-size: 0.85rem; color: var(--fs-text-2); max-height: 9rem; overflow-y: auto; white-space: pre-line; }
    .facts { display: grid; grid-template-columns: max-content minmax(0, 1fr); gap: 0.2rem 0.8rem; font-size: 0.85rem; margin: 0; }
    .facts dt { font-weight: 500; color: var(--fs-muted); }
    .facts dd { margin: 0; overflow-wrap: anywhere; }
    .form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0.6rem 1rem; }
    .span-2 { grid-column: 1 / -1; }
    .form-actions { display: flex; flex-wrap: wrap; gap: 0.5rem; align-items: center; }
    @media (max-width: 991.98px) { .detail { grid-template-columns: minmax(0, 1fr); } }
    @media (max-width: 575.98px) { .form-grid { grid-template-columns: minmax(0, 1fr); } .bar-col { width: 35%; } }
  `],
})
export class CollectionIntelligence {
  private readonly api = inject(InstrumentsService);
  private readonly notify = inject(NotifyService);
  private readonly destroyRef = inject(DestroyRef);

  readonly collection = input.required<Collection>();
  /** Changes whenever the members change, so the panel reloads. */
  readonly version = input<unknown>();

  readonly dimensions = TAXONOMY_DIMENSIONS;
  readonly dimensionLabels = TAXONOMY_LABELS;
  readonly statusLabels: Record<ProfileStatus, string> = STATUS_LABELS;
  readonly linkLabels = LINK_LABELS;

  readonly taxonomy = signal<Taxonomy | null>(null);
  readonly intel = signal<Intelligence | null>(null);
  readonly error = signal('');
  readonly dimension = signal<TaxonomyDimension>('sector');
  readonly fetching = signal(false);
  readonly refreshLine = signal('');

  readonly detail = signal<MemberIntelligence | null>(null);
  readonly view = signal<ProfileView | null>(null);
  readonly profileBusy = signal(false);
  readonly draft = signal<OverrideDraft>(draftFrom(null));

  private readonly labels = computed(() => labelMap(this.taxonomy()));
  readonly comp = computed(() => taxonomyComposition(this.collection().kind, this.intel(), this.dimension(), this.labels()));
  readonly coverageText = computed(() => { const i = this.intel(); return i ? profileCoverageText(i) : ''; });
  readonly canFetch = computed(() => canFetchProfiles(this.intel()));
  readonly problem = computed(() => { const t = this.taxonomy(); return t ? overrideProblem(this.draft(), t.limits) : ''; });
  readonly sourceUrl = computed(() => {
    const url = this.view()?.profile.profile?.source_url || this.view()?.profile.provenance?.url || '';
    return /^https:\/\//i.test(url) ? url : '';
  });
  readonly profileFacts = computed(() => {
    const p = this.view()?.profile.profile;
    if (!p) return [] as [string, string][];
    const rows: [string, string | number | undefined][] = [
      ['Kind', p.kind], ['ISIN', p.isin], ['Exchange', p.exchange ? (p.mic ? `${p.exchange} (${p.mic})` : p.exchange) : p.mic],
      ['Currency', p.currency], ['Country', p.country], ['Sector', p.sector], ['Industry', p.industry],
      ['Market cap', p.company?.market_cap !== undefined ? compactNumber(p.company.market_cap) : undefined],
      ['Employees', p.company?.employees !== undefined ? compactNumber(p.company.employees) : undefined],
      ['Headquarters', p.company?.headquarters], ['IPO', p.company?.ipo_date],
      ['Issuer', p.fund?.issuer], ['Category', p.fund?.category], ['Benchmark', p.fund?.benchmark],
      ['Expense ratio', p.fund?.expense_ratio !== undefined ? `${p.fund.expense_ratio}%` : undefined],
      ['Total assets', p.fund?.total_assets !== undefined ? compactNumber(p.fund.total_assets) : undefined],
      ['Underlying', p.commodity?.underlying], ['Unit', p.commodity?.unit], ['Expiry', p.commodity?.expiry],
      ['Network', p.crypto?.network], ['Categories', p.crypto?.categories?.join(', ')],
      ['Circulating supply', p.crypto?.circulating_supply !== undefined ? compactNumber(p.crypto.circulating_supply) : undefined],
      ['Max supply', p.crypto?.max_supply !== undefined ? compactNumber(p.crypto.max_supply) : undefined],
      ['Website', p.website],
    ];
    return rows.filter((r): r is [string, string | number] => r[1] !== undefined && r[1] !== '').map(([k, v]) => [k, String(v)] as [string, string]);
  });

  private loadedFor = 0;

  constructor() {
    this.api.taxonomy().pipe(takeUntilDestroyed(this.destroyRef)).subscribe({
      next: t => this.taxonomy.set(t),
      error: err => this.error.set(describeHttpError(err)),
    });
    effect(() => {
      const id = this.collection().id;
      this.version();
      untracked(() => {
        if (id !== this.loadedFor) {
          this.loadedFor = id;
          this.intel.set(null);
          this.detail.set(null);
          this.refreshLine.set('');
        }
        this.load();
      });
    });
  }

  utc(value: string): string { return formatUtcDateTime(value); }
  fact(f: Fact, d: TaxonomyDimension): string { return factText(f, this.labels()[d]); }
  sourceOf(f: Fact): string { return f.raw && !f.value ? `${factSourceText(f)}; the provider's wording is not in the taxonomy` : factSourceText(f); }
  membersOf(l: Intelligence['links'][number]): string { return linkMembers(l, this.intel()?.members ?? []); }

  private load(): void {
    const id = this.collection().id;
    this.api.intelligence(id).pipe(takeUntilDestroyed(this.destroyRef)).subscribe({
      next: i => {
        if (this.collection().id !== id) return;
        this.error.set('');
        this.intel.set(i);
        const d = this.detail();
        if (d) this.detail.set(i.members.find(m => m.member_id === d.member_id) ?? null);
      },
      error: err => this.error.set(describeHttpError(err)),
    });
  }

  /** Fetches due profiles in bounded rounds until none remain or a round makes no progress. */
  fetchProfiles(): void {
    const id = this.collection().id;
    this.fetching.set(true);
    this.refreshLine.set('');
    const round = (n: number) => this.api.refreshProfiles(id).pipe(takeUntilDestroyed(this.destroyRef)).subscribe({
      next: r => {
        this.refreshLine.set(refreshSummary(r));
        const progressed = r.fetched + r.not_found > 0;
        if (r.remaining > 0 && progressed && n + 1 < MAX_ROUNDS && this.collection().id === id) { round(n + 1); return; }
        this.fetching.set(false);
        this.load();
      },
      error: err => { this.fetching.set(false); this.notify.error(describeHttpError(err), 'Fetching profiles failed'); this.load(); },
    });
    round(0);
  }

  openDetail(m: MemberIntelligence): void {
    this.detail.set(m);
    this.view.set(null);
    this.loadProfile(false);
  }

  loadProfile(refresh: boolean): void {
    const m = this.detail();
    if (!m) return;
    this.profileBusy.set(true);
    this.api.profile(m.key, refresh).pipe(takeUntilDestroyed(this.destroyRef)).subscribe({
      next: v => {
        this.profileBusy.set(false);
        if (this.detail()?.member_id !== m.member_id) return;
        this.view.set(v);
        this.draft.set(draftFrom(v.override));
        if (refresh) this.load();
      },
      error: err => { this.profileBusy.set(false); this.notify.error(describeHttpError(err), 'Loading the profile failed'); },
    });
  }

  patch(field: keyof OverrideDraft, value: string): void {
    this.draft.update(d => ({ ...d, [field]: value }));
  }

  saveOverride(): void {
    const m = this.detail();
    if (!m || this.problem()) return;
    this.write(this.api.saveClassification(overrideBody(m.key, this.draft())), 'Classification saved.');
  }

  async resetOverride(): Promise<void> {
    const m = this.detail();
    if (!m) return;
    const ok = await this.notify.confirm({ title: 'Reset classification', message: `Remove your own classification of ${m.key.symbol}? The provider's profile is used again.`, confirmLabel: 'Reset' });
    if (ok) this.write(this.api.deleteClassification(m.key), 'Classification reset.');
  }

  private write(source: Observable<unknown>, done: string): void {
    this.profileBusy.set(true);
    source.pipe(takeUntilDestroyed(this.destroyRef)).subscribe({
      next: () => {
        this.profileBusy.set(false);
        this.notify.success(done);
        this.loadProfile(false);
        this.load();
      },
      error: err => { this.profileBusy.set(false); this.notify.error(describeHttpError(err), 'Saving failed'); },
    });
  }
}
