import { ChangeDetectionStrategy, Component, DestroyRef, computed, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Router } from '@angular/router';
import { Observable, Subject, catchError, debounceTime, distinctUntilChanged, forkJoin, of, switchMap } from 'rxjs';

import {
  COLLECTION_LIMITS, CREATABLE_KINDS, DIMENSION_LABELS, KIND_LABELS, composition, coverageText, exportFileName, facets, importSummary,
  instrumentFrom, parseTags, weightProblem, weightSummary,
  type ArchiveFilter, type Collection, type CollectionKind, type CollectionMember, type CollectionSort, type Dimension,
} from '../core/collections';
import { CollectionsService } from '../core/collections.service';
import { instrumentKey } from '../core/instrument-ref';
import { MarketState } from '../core/market-state';
import { NotifyService } from '../core/notify.service';
import type { ProviderId, SymbolInfo } from '../models';
import { describeHttpError } from '../services';
import { formatUtcDateTime } from '../ui/datetime';

const PAGE = 50;
const DIMENSIONS: Dimension[] = ['type', 'exchange', 'currency', 'country', 'provider'];

/**
 * Market Collections (#441): the user's own, private sets of instruments.
 * Watch collections are counted, never shown as an allocation; model
 * portfolios carry hypothetical weights. Everything is stored by the server.
 */
@Component({
  selector: 'fs-collections-page',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
<div class="fs-page">
  <header class="fs-page-header">
    <div>
      <div class="fs-eyebrow">Markets</div>
      <h1>Collections</h1>
      <p>Your own groups of instruments, for following a theme or sketching a model portfolio. Only you can see them.</p>
    </div>
    <div class="head-actions">
      <button type="button" class="btn btn-outline-secondary btn-sm" (click)="exportAll()" [disabled]="!collections().length">
        <i class="bi bi-download" aria-hidden="true"></i> Export
      </button>
      <button type="button" class="btn btn-outline-secondary btn-sm" (click)="importInput.click()">
        <i class="bi bi-upload" aria-hidden="true"></i> Import
      </button>
      <input #importInput type="file" hidden accept="application/json,.json" aria-label="Collections file to import" (change)="importFile($event)">
      <button type="button" class="btn btn-primary btn-sm" (click)="creating.set(!creating())" [attr.aria-expanded]="creating()" aria-controls="col-create">
        <i class="bi bi-plus-lg" aria-hidden="true"></i> New collection
      </button>
    </div>
  </header>

  @if (loadError(); as err) { <div class="alert alert-danger" role="alert">{{ err }}</div> }
  @if (importLines().length) {
    <div class="alert alert-info" role="status">
      <strong>Import finished.</strong>
      <ul class="mb-0">@for (l of importLines(); track $index) { <li>{{ l }}</li> }</ul>
    </div>
  }

  @if (creating()) {
    <form class="card create" id="col-create" aria-labelledby="col-create-title" (submit)="$event.preventDefault(); create()">
      <div class="card-header"><h2 class="card-title" id="col-create-title">New collection</h2></div>
      <div class="card-body form-grid">
        <div>
          <label class="form-label" for="nc-name">Name</label>
          <input class="form-control" id="nc-name" required [maxLength]="limits.name" [value]="draft().name" (input)="setDraft('name', $any($event.target).value)">
        </div>
        <div>
          <label class="form-label" for="nc-kind">Kind</label>
          <select class="form-select" id="nc-kind" [value]="draft().kind" (change)="setDraft('kind', $any($event.target).value)" aria-describedby="nc-kind-hint">
            @for (k of creatableKinds; track k) { <option [value]="k">{{ kindLabels[k] }}</option> }
          </select>
          <div class="form-text" id="nc-kind-hint">
            {{ draft().kind === 'model' ? 'Hypothetical weights, never more than 100% in total. Not a holding.' : 'Instruments to follow. No weights.' }}
            The kind cannot be changed later.
          </div>
        </div>
        <div>
          <label class="form-label" for="nc-category">Category <span class="muted">(optional)</span></label>
          <input class="form-control" id="nc-category" placeholder="Sector" [maxLength]="limits.category" [value]="draft().category" (input)="setDraft('category', $any($event.target).value)">
        </div>
        <div>
          <label class="form-label" for="nc-tags">Tags <span class="muted">(comma-separated)</span></label>
          <input class="form-control" id="nc-tags" placeholder="AI, dividends" [value]="draft().tags" (input)="setDraft('tags', $any($event.target).value)">
        </div>
        <div class="span-2">
          <label class="form-label" for="nc-desc">Description <span class="muted">(optional)</span></label>
          <textarea class="form-control" id="nc-desc" rows="2" [maxLength]="limits.description" [value]="draft().description" (input)="setDraft('description', $any($event.target).value)"></textarea>
        </div>
        <div class="span-2 form-actions">
          <button type="submit" class="btn btn-primary" [disabled]="busy() || !draft().name.trim()">Create</button>
          <button type="button" class="btn btn-ghost" (click)="creating.set(false)">Cancel</button>
        </div>
      </div>
    </form>
  }

  <div class="layout" [class.has-selection]="!!selected()">
    <section class="card list-pane" aria-labelledby="col-list-title">
      <div class="card-header"><h2 class="card-title" id="col-list-title">Your collections <span class="muted tabular">{{ collections().length }}</span></h2></div>
      <div class="card-body filters">
        <div>
          <label class="visually-hidden" for="cf-q">Search collections</label>
          <input class="form-control form-control-sm" id="cf-q" type="search" placeholder="Search name or description" [value]="query()" (input)="setQuery($any($event.target).value)">
        </div>
        <div class="fs-segmented" role="group" aria-label="Kind">
          <button type="button" [attr.aria-pressed]="kindFilter() === ''" (click)="setFilter('kind', '')">All</button>
          <button type="button" [attr.aria-pressed]="kindFilter() === 'watch'" (click)="setFilter('kind', 'watch')">Watch</button>
          <button type="button" [attr.aria-pressed]="kindFilter() === 'model'" (click)="setFilter('kind', 'model')">Model</button>
        </div>
        <div class="filter-row">
          <div>
            <label class="form-label small" for="cf-category">Category</label>
            <select class="form-select form-select-sm" id="cf-category" [value]="category()" (change)="setFilter('category', $any($event.target).value)">
              <option value="">Any</option>
              @for (c of facetList().categories; track c) { <option [value]="c">{{ c }}</option> }
            </select>
          </div>
          <div>
            <label class="form-label small" for="cf-tag">Tag</label>
            <select class="form-select form-select-sm" id="cf-tag" [value]="tag()" (change)="setFilter('tag', $any($event.target).value)">
              <option value="">Any</option>
              @for (t of facetList().tags; track t) { <option [value]="t">{{ t }}</option> }
            </select>
          </div>
          <div>
            <label class="form-label small" for="cf-archived">Show</label>
            <select class="form-select form-select-sm" id="cf-archived" [value]="archived()" (change)="setFilter('archived', $any($event.target).value)">
              <option value="exclude">Active</option>
              <option value="include">Active and archived</option>
              <option value="only">Archived</option>
            </select>
          </div>
          <div>
            <label class="form-label small" for="cf-sort">Sort</label>
            <select class="form-select form-select-sm" id="cf-sort" [value]="sort()" (change)="setFilter('sort', $any($event.target).value)">
              <option value="name">Name</option>
              <option value="updated">Recently changed</option>
              <option value="created">Newest</option>
              <option value="members">Most instruments</option>
            </select>
          </div>
        </div>
      </div>
      @if (loading() && !collections().length) {
        <p class="muted px-3">Loading…</p>
      } @else if (!collections().length) {
        <div class="fs-empty m-3">
          <i class="bi bi-collection" aria-hidden="true"></i>
          <h3>{{ filtered() ? 'No collection matches' : 'No collections yet' }}</h3>
          <p class="mb-0">{{ filtered() ? 'Change the filters to see more.' : 'Create one to group instruments by theme, sector or idea.' }}</p>
        </div>
      } @else {
        <ul class="col-list" aria-label="Collections">
          @for (c of collections(); track c.id) {
            <li>
              <button type="button" class="col-row" [class.selected]="selected()?.id === c.id" [attr.aria-current]="selected()?.id === c.id ? 'true' : null" (click)="open(c.id)">
                <span class="col-name">{{ c.name }}@if (c.archived) { <span class="fs-status no-dot ms-1">Archived</span> }</span>
                <span class="col-meta">
                  <span>{{ c.kind === 'model' ? 'Model' : 'Watch' }}</span>
                  <span class="tabular">{{ c.member_count }} instrument{{ c.member_count === 1 ? '' : 's' }}</span>
                  @if (c.category) { <span>{{ c.category }}</span> }
                </span>
              </button>
            </li>
          }
        </ul>
      }
    </section>

    <div class="detail-pane">
      @if (selected(); as c) {
        <button type="button" class="btn btn-ghost btn-sm back d-lg-none" (click)="close()"><i class="bi bi-arrow-left" aria-hidden="true"></i> All collections</button>

        <section class="card" aria-labelledby="col-title">
          <div class="card-body">
            @if (!editing()) {
              <div class="detail-head">
                <div class="min-w-0">
                  <div class="fs-eyebrow">{{ kindLabels[c.kind] }}@if (c.category) { · {{ c.category }} }</div>
                  <h2 id="col-title" class="detail-title">{{ c.name }}</h2>
                  @if (c.description) { <p class="detail-desc">{{ c.description }}</p> }
                  <div class="tags">
                    @if (c.archived) { <span class="fs-status no-dot">Archived</span> }
                    @for (t of c.tags ?? []; track t) { <span class="fs-status no-dot is-accent">{{ t }}</span> }
                  </div>
                  <p class="muted small mb-0">Changed {{ utc(c.updated_at) }}</p>
                </div>
                <div class="detail-actions">
                  <button type="button" class="btn btn-outline-secondary btn-sm" (click)="startEdit(c)">Edit</button>
                  <button type="button" class="btn btn-outline-secondary btn-sm" (click)="setArchived(c, !c.archived)">{{ c.archived ? 'Restore' : 'Archive' }}</button>
                  <button type="button" class="btn btn-outline-danger btn-sm" (click)="remove(c)">Delete</button>
                </div>
              </div>
            } @else {
              <form class="form-grid" aria-label="Edit collection" (submit)="$event.preventDefault(); saveEdit(c)">
                <div>
                  <label class="form-label" for="ec-name">Name</label>
                  <input class="form-control" id="ec-name" required [maxLength]="limits.name" [value]="edit().name" (input)="setEdit('name', $any($event.target).value)">
                </div>
                <div>
                  <label class="form-label" for="ec-category">Category</label>
                  <input class="form-control" id="ec-category" [maxLength]="limits.category" [value]="edit().category" (input)="setEdit('category', $any($event.target).value)">
                </div>
                <div class="span-2">
                  <label class="form-label" for="ec-tags">Tags <span class="muted">(comma-separated)</span></label>
                  <input class="form-control" id="ec-tags" [value]="edit().tags" (input)="setEdit('tags', $any($event.target).value)">
                </div>
                <div class="span-2">
                  <label class="form-label" for="ec-desc">Description</label>
                  <textarea class="form-control" id="ec-desc" rows="2" [maxLength]="limits.description" [value]="edit().description" (input)="setEdit('description', $any($event.target).value)"></textarea>
                </div>
                <div class="span-2 form-actions">
                  <button type="submit" class="btn btn-primary btn-sm" [disabled]="busy() || !edit().name.trim()">Save</button>
                  <button type="button" class="btn btn-ghost btn-sm" (click)="editing.set(false)">Cancel</button>
                </div>
              </form>
            }
          </div>
        </section>

        <section class="tiles" aria-label="Collection summary">
          <div class="card fs-kpi">
            <span class="fs-kpi-label">Instruments</span>
            <span class="fs-kpi-value tabular">{{ total() }}</span>
            <span class="fs-kpi-sub">of at most 1,000</span>
          </div>
          @if (c.kind === 'model') {
            <div class="card fs-kpi">
              <span class="fs-kpi-label">Weighted</span>
              <span class="fs-kpi-value tabular">{{ weights().total }}%</span>
              <span class="fs-kpi-sub">{{ weights().weighted }} of {{ total() }} instruments · hypothetical</span>
            </div>
            <div class="card fs-kpi">
              <span class="fs-kpi-label">Unallocated</span>
              <span class="fs-kpi-value tabular">{{ weights().remaining }}%</span>
              <span class="fs-kpi-sub">not assigned to any instrument</span>
            </div>
          } @else {
            <div class="card fs-kpi">
              <span class="fs-kpi-label">Data providers</span>
              <span class="fs-kpi-value tabular">{{ providerCount() }}</span>
              <span class="fs-kpi-sub">the same ticker on two providers counts twice</span>
            </div>
          }
        </section>

        <section class="card" aria-labelledby="comp-title">
          <div class="card-header comp-head">
            <h2 class="card-title me-auto" id="comp-title">Composition</h2>
            <div class="fs-segmented" role="group" aria-label="Group by">
              @for (d of dimensions; track d) {
                <button type="button" [attr.aria-pressed]="dimension() === d" (click)="dimension.set(d)">{{ dimensionLabels[d] }}</button>
              }
            </div>
          </div>
          <div class="card-body">
            <p class="comp-note" id="comp-note">
              @if (comp().mode === 'count') {
                <i class="bi bi-info-circle" aria-hidden="true"></i> Number of instruments by {{ dimensionLabels[dimension()].toLowerCase() }}. A watch collection has no weights, so this is a count, not an allocation.
              } @else {
                <i class="bi bi-info-circle" aria-hidden="true"></i> Hypothetical weights by {{ dimensionLabels[dimension()].toLowerCase() }}, as you entered them. Not a holding and not advice.
              }
            </p>
            @if (comp().total) {
              <table class="table comp-table" aria-describedby="comp-note">
                <caption class="visually-hidden">{{ comp().mode === 'count' ? 'Instruments' : 'Weight' }} by {{ dimensionLabels[dimension()] }}</caption>
                <thead><tr><th scope="col">{{ dimensionLabels[dimension()] }}</th><th scope="col" class="bar-col"><span class="visually-hidden">Bar</span></th><th scope="col" class="num">{{ comp().mode === 'count' ? 'Instruments' : 'Weight' }}</th></tr></thead>
                <tbody>
                  @for (r of comp().rows; track r.key) {
                    <tr [class.unknown]="!r.key">
                      <th scope="row">{{ r.label }}</th>
                      <td class="bar-col"><span class="bar" [style.width.%]="r.pct" aria-hidden="true"></span></td>
                      <td class="num tabular">
                        @if (comp().mode === 'count') { {{ r.count }} <span class="muted">· {{ r.pct }}%</span> }
                        @else { {{ r.weight }}% <span class="muted">· {{ r.count }} instr.</span> }
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
              <p class="muted small mb-0">{{ coverage() }}@if (comp().unweighted) { {{ comp().unweighted }} instrument{{ comp().unweighted === 1 ? ' has' : 's have' }} no weight. }</p>
            } @else {
              <p class="muted mb-0">Add instruments to see the composition.</p>
            }
          </div>
        </section>

        <section class="card" aria-labelledby="add-title">
          <div class="card-header"><h2 class="card-title" id="add-title">Add instruments</h2></div>
          <div class="card-body add">
            @if (market.trackedSymbol()) {
              <div class="add-selected">
                <span>Selected: <strong>{{ market.trackedSymbol() }}</strong> <span class="muted">{{ market.trackedName() }} · {{ market.trackedProvider() }}</span></span>
                @if (has(market.trackedProvider(), market.trackedSymbol())) {
                  <span class="muted small">Already in this collection</span>
                } @else {
                  <button type="button" class="btn btn-outline-primary btn-sm" (click)="addSelected()" [disabled]="busy()">Add selected instrument</button>
                }
              </div>
            }
            <div class="search-row">
              <div>
                <label class="form-label small" for="as-provider">Provider</label>
                <select class="form-select form-select-sm" id="as-provider" [value]="searchProvider()" (change)="setSearchProvider($any($event.target).value)">
                  @for (p of searchProviders(); track p.id) { <option [value]="p.id">{{ p.label }}</option> }
                  @if (!searchProviders().length) { <option [value]="searchProvider()">{{ searchProvider() }}</option> }
                </select>
              </div>
              <div class="grow">
                <label class="form-label small" for="as-query">Search</label>
                <input class="form-control form-control-sm" id="as-query" type="search" autocomplete="off" placeholder="Ticker or name" [value]="searchText()" (input)="setSearch($any($event.target).value)">
              </div>
            </div>
            @if (searching()) { <p class="muted small mb-0" role="status">Searching…</p> }
            @else if (searchError(); as e) { <p class="text-neg small mb-0" role="alert">{{ e }}</p> }
            @else if (searchText().trim().length >= 2 && !hits().length) { <p class="muted small mb-0" role="status">Nothing found on {{ searchProvider() }}. You can add the symbol by hand below.</p> }
            @if (hits().length) {
              <ul class="hits" aria-label="Search results">
                @for (h of hits(); track h.symbol + h.exchange) {
                  <li>
                    <span class="min-w-0"><strong>{{ h.symbol }}</strong> <span class="muted">{{ h.name }}@if (h.exchange) { · {{ h.exchange }} }@if (h.type) { · {{ h.type }} }</span></span>
                    @if (has(searchProvider(), h.symbol)) {
                      <span class="muted small">Added</span>
                    } @else {
                      <button type="button" class="btn btn-outline-primary btn-sm" (click)="addHit(h)" [disabled]="busy()" [attr.aria-label]="'Add ' + h.symbol">Add</button>
                    }
                  </li>
                }
              </ul>
            }
            <details class="manual">
              <summary>Add a symbol by hand</summary>
              <form class="search-row" (submit)="$event.preventDefault(); addManual()">
                <div>
                  <label class="form-label small" for="am-provider">Provider</label>
                  <input class="form-control form-control-sm" id="am-provider" autocomplete="off" [value]="manualProvider()" (input)="manualProvider.set($any($event.target).value)">
                </div>
                <div class="grow">
                  <label class="form-label small" for="am-symbol">Symbol</label>
                  <input class="form-control form-control-sm" id="am-symbol" autocomplete="off" placeholder="GC=F" [value]="manualSymbol()" (input)="manualSymbol.set($any($event.target).value)">
                </div>
                <button type="submit" class="btn btn-outline-primary btn-sm align-self-end" [disabled]="busy() || !manualSymbol().trim() || !manualProvider().trim()">Add</button>
              </form>
              <p class="form-text mb-0">Only provider and symbol are stored; name, exchange and type stay empty rather than guessed.</p>
            </details>
          </div>
        </section>

        <section class="card" aria-labelledby="members-title">
          <div class="card-header"><h2 class="card-title" id="members-title">Instruments <span class="muted tabular">{{ total() }}</span></h2></div>
          @if (!page().length) {
            <div class="card-body"><p class="muted mb-0">No instruments yet. Search above to add some.</p></div>
          } @else {
            <div class="fs-scroll-x">
              <table class="table members">
                <caption class="visually-hidden">Instruments in {{ c.name }}</caption>
                <thead>
                  <tr>
                    <th scope="col">Instrument</th><th scope="col">Provider</th><th scope="col">Type</th><th scope="col">Exchange</th><th scope="col">Currency</th>
                    <th scope="col">Note</th>@if (c.kind === 'model') { <th scope="col" class="num">Weight</th> }<th scope="col"><span class="visually-hidden">Actions</span></th>
                  </tr>
                </thead>
                <tbody>
                  @for (m of page(); track m.id) {
                    <tr [class.active-row]="memberEdit()?.id === m.id">
                      <th scope="row"><span class="sym">{{ m.instrument.symbol }}</span>@if (m.instrument.name) { <span class="muted d-block small">{{ m.instrument.name }}</span> }</th>
                      <td>{{ m.instrument.provider }}</td>
                      <td>{{ m.instrument.type || '—' }}</td>
                      <td>{{ m.instrument.exchange || '—' }}</td>
                      <td>{{ m.instrument.currency || '—' }}</td>
                      <td class="note">{{ m.note || '' }}@for (t of m.tags ?? []; track t) { <span class="fs-status no-dot ms-1">{{ t }}</span> }</td>
                      @if (c.kind === 'model') { <td class="num tabular">{{ m.weight !== undefined && m.weight !== null ? m.weight + '%' : '—' }}</td> }
                      <td class="row-actions">
                        <button type="button" class="btn btn-ghost btn-sm" (click)="openInMarkets(m)" [attr.aria-label]="'Open ' + m.instrument.symbol + ' in Markets'"><i class="bi bi-graph-up" aria-hidden="true"></i></button>
                        <button type="button" class="btn btn-ghost btn-sm" (click)="startMemberEdit(m)" [attr.aria-label]="'Edit ' + m.instrument.symbol"><i class="bi bi-pencil" aria-hidden="true"></i></button>
                        <button type="button" class="btn btn-ghost btn-sm" (click)="removeMember(m)" [attr.aria-label]="'Remove ' + m.instrument.symbol"><i class="bi bi-x-lg" aria-hidden="true"></i></button>
                      </td>
                    </tr>
                  }
                </tbody>
              </table>
            </div>
            @if (total() > pageSize) {
              <nav class="pager" aria-label="Instrument pages">
                <button type="button" class="btn btn-outline-secondary btn-sm" [disabled]="offset() === 0" (click)="goto(offset() - pageSize)">Previous</button>
                <span class="muted small tabular">{{ offset() + 1 }}–{{ Math.min(offset() + pageSize, total()) }} of {{ total() }}</span>
                <button type="button" class="btn btn-outline-secondary btn-sm" [disabled]="offset() + pageSize >= total()" (click)="goto(offset() + pageSize)">Next</button>
              </nav>
            }
          }
        </section>

        @if (memberEdit(); as m) {
          <section class="card" aria-labelledby="me-title">
            <div class="card-header"><h2 class="card-title" id="me-title">{{ m.instrument.symbol }} <span class="muted">· {{ m.instrument.provider }}</span></h2></div>
            <div class="card-body">
              <form class="form-grid" (submit)="$event.preventDefault(); saveMember(c, m)">
                <div class="span-2">
                  <label class="form-label" for="me-note">Note</label>
                  <textarea class="form-control" id="me-note" rows="2" [maxLength]="limits.note" [value]="memberDraft().note" (input)="setMemberDraft('note', $any($event.target).value)"></textarea>
                </div>
                <div>
                  <label class="form-label" for="me-tags">Tags <span class="muted">(comma-separated)</span></label>
                  <input class="form-control" id="me-tags" [value]="memberDraft().tags" (input)="setMemberDraft('tags', $any($event.target).value)">
                </div>
                @if (c.kind === 'model') {
                  <div>
                    <label class="form-label" for="me-weight">Hypothetical weight, % <span class="muted">(empty for none)</span></label>
                    <input class="form-control" id="me-weight" type="number" min="0" max="100" step="any" inputmode="decimal" [value]="memberDraft().weight"
                           (input)="setMemberDraft('weight', $any($event.target).value)" [attr.aria-invalid]="!!memberWeightProblem()" aria-describedby="me-weight-hint">
                    <div class="form-text" [class.text-neg]="!!memberWeightProblem()" id="me-weight-hint">{{ memberWeightProblem() || 'The other instruments have ' + othersWeight() + '%.' }}</div>
                  </div>
                }
                <div class="span-2 form-actions">
                  <button type="submit" class="btn btn-primary btn-sm" [disabled]="busy() || !!memberWeightProblem()">Save</button>
                  <button type="button" class="btn btn-ghost btn-sm" (click)="memberEdit.set(null)">Close</button>
                </div>
              </form>
              @if (targets().length) {
                <form class="transfer" aria-label="Copy or move to another collection" (submit)="$event.preventDefault()">
                  <label class="form-label small" for="me-target">Copy or move to</label>
                  <div class="search-row">
                    <select class="form-select form-select-sm grow" id="me-target" [value]="transferTo()" (change)="transferTo.set(+$any($event.target).value)">
                      @for (t of targets(); track t.id) { <option [value]="t.id">{{ t.name }}</option> }
                    </select>
                    <button type="button" class="btn btn-outline-secondary btn-sm" (click)="transfer(c, m, false)" [disabled]="busy() || !transferTo()">Copy</button>
                    <button type="button" class="btn btn-outline-secondary btn-sm" (click)="transfer(c, m, true)" [disabled]="busy() || !transferTo()">Move</button>
                  </div>
                  <p class="form-text mb-0">Note and tags go along; a weight does not.</p>
                </form>
              }
            </div>
          </section>
        }
      } @else if (collections().length) {
        <div class="fs-empty d-none d-lg-grid">
          <i class="bi bi-collection" aria-hidden="true"></i>
          <h3>Choose a collection</h3>
          <p class="mb-0">Pick one from the list to see its instruments and composition.</p>
        </div>
      }
    </div>
  </div>
</div>
  `,
  styles: [`
    .head-actions { display: flex; flex-wrap: wrap; gap: 0.5rem; }
    .create { margin-bottom: 1rem; }
    .form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0.75rem 1rem; }
    .span-2 { grid-column: 1 / -1; }
    .form-actions { display: flex; gap: 0.5rem; align-items: center; }
    .layout { display: grid; grid-template-columns: minmax(260px, 340px) minmax(0, 1fr); gap: 1rem; align-items: start; }
    .list-pane .filters { display: grid; gap: 0.6rem; border-bottom: 1px solid var(--fs-border); }
    .filter-row { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0.5rem; }
    .col-list { list-style: none; margin: 0; padding: 0.4rem; display: grid; gap: 0.2rem; max-height: 70vh; overflow-y: auto; }
    .col-row { width: 100%; display: grid; gap: 0.15rem; text-align: left; padding: 0.55rem 0.7rem; border: 1px solid transparent; border-radius: var(--bs-border-radius); background: transparent; color: var(--fs-text); }
    .col-row:hover { background: var(--fs-surface-2); }
    .col-row.selected { border-color: var(--fs-accent); background: var(--fs-accent-soft); }
    .col-name { font-weight: 600; font-size: 0.9rem; overflow-wrap: anywhere; }
    .col-meta { display: flex; flex-wrap: wrap; gap: 0 0.6rem; font-size: 0.75rem; color: var(--fs-muted); }
    .detail-pane { display: grid; gap: 1rem; min-width: 0; }
    .detail-head { display: flex; flex-wrap: wrap; justify-content: space-between; gap: 0.75rem; }
    .detail-title { font-size: 1.25rem; margin: 0.1rem 0 0.3rem; overflow-wrap: anywhere; }
    .detail-desc { margin: 0 0 0.4rem; color: var(--fs-text-2); white-space: pre-line; }
    .detail-actions { display: flex; flex-wrap: wrap; gap: 0.4rem; align-items: flex-start; }
    .tags { display: flex; flex-wrap: wrap; gap: 0.3rem; margin-bottom: 0.4rem; }
    .min-w-0 { min-width: 0; }
    .tiles { display: grid; grid-template-columns: repeat(auto-fit, minmax(170px, 1fr)); gap: 1rem; }
    .comp-head { display: flex; flex-wrap: wrap; align-items: center; gap: 0.5rem; }
    .comp-note { font-size: 0.8rem; color: var(--fs-muted); margin: 0 0 0.6rem; }
    .comp-table { th[scope='row'] { font-weight: 500; white-space: nowrap; } .num { text-align: right; white-space: nowrap; } }
    .bar-col { width: 55%; }
    .bar { display: block; height: 0.6rem; min-width: 2px; border-radius: 0 4px 4px 0; background: var(--fs-accent); }
    .unknown .bar { background: var(--fs-border-strong); }
    .bar.hatched { background: repeating-linear-gradient(45deg, var(--fs-border-strong) 0 3px, transparent 3px 6px); border: 1px solid var(--fs-border-strong); }
    .unknown th { color: var(--fs-muted); }
    .add { display: grid; gap: 0.6rem; }
    .add-selected { display: flex; flex-wrap: wrap; justify-content: space-between; align-items: center; gap: 0.5rem; padding: 0.5rem 0.7rem; border: 1px solid var(--fs-border); border-radius: var(--bs-border-radius); background: var(--fs-surface-2); }
    .search-row { display: flex; flex-wrap: wrap; gap: 0.5rem; align-items: flex-end; }
    .search-row > div { min-width: 8rem; }
    .grow { flex: 1 1 12rem; }
    .hits { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.25rem; max-height: 18rem; overflow-y: auto; }
    .hits li { display: flex; justify-content: space-between; align-items: center; gap: 0.5rem; padding: 0.35rem 0.5rem; border-radius: var(--bs-border-radius); }
    .hits li:hover { background: var(--fs-surface-2); }
    .manual summary { cursor: pointer; font-size: 0.85rem; color: var(--fs-text-2); }
    .manual form { margin: 0.5rem 0 0.3rem; }
    .members { .sym { font-weight: 650; } .num { text-align: right; } .note { max-width: 18rem; overflow-wrap: anywhere; } td, th { vertical-align: middle; } }
    .active-row { background: var(--fs-accent-soft); }
    .row-actions { white-space: nowrap; text-align: right; }
    .pager { display: flex; justify-content: space-between; align-items: center; gap: 0.5rem; padding: 0.6rem 1rem; border-top: 1px solid var(--fs-border); }
    .transfer { margin-top: 1rem; padding-top: 0.8rem; border-top: 1px solid var(--fs-border); }
    .back { justify-self: start; }
    @media (max-width: 991.98px) {
      .layout { grid-template-columns: minmax(0, 1fr); }
      .layout.has-selection .list-pane { display: none; }
    }
    @media (max-width: 575.98px) {
      .form-grid, .filter-row { grid-template-columns: minmax(0, 1fr); }
      .bar-col { width: 35%; }
    }
  `],
})
export class CollectionsPage {
  private readonly api = inject(CollectionsService);
  private readonly notify = inject(NotifyService);
  private readonly router = inject(Router);
  private readonly destroyRef = inject(DestroyRef);
  readonly market = inject(MarketState);

  readonly limits = COLLECTION_LIMITS;
  readonly kindLabels = KIND_LABELS;
  readonly creatableKinds = CREATABLE_KINDS;
  readonly dimensions = DIMENSIONS;
  readonly dimensionLabels = DIMENSION_LABELS;
  readonly pageSize = PAGE;
  readonly Math = Math;

  readonly collections = signal<Collection[]>([]);
  readonly loading = signal(true);
  readonly loadError = signal('');
  readonly busy = signal(false);
  readonly importLines = signal<string[]>([]);

  readonly query = signal('');
  readonly kindFilter = signal<CollectionKind | ''>('');
  readonly category = signal('');
  readonly tag = signal('');
  readonly archived = signal<ArchiveFilter>('exclude');
  readonly sort = signal<CollectionSort>('name');
  /** Facets come from the unfiltered list, so a filter never hides its own options. */
  private readonly everything = signal<Collection[]>([]);
  readonly facetList = computed(() => facets(this.everything()));
  readonly filtered = computed(() => !!(this.query().trim() || this.kindFilter() || this.category() || this.tag() || this.archived() !== 'exclude'));

  readonly creating = signal(false);
  readonly draft = signal({ name: '', kind: 'watch' as CollectionKind, category: '', tags: '', description: '' });

  readonly selected = signal<Collection | null>(null);
  readonly page = signal<CollectionMember[]>([]);
  readonly total = signal(0);
  readonly offset = signal(0);
  /** Every member (up to 1,000), for the composition and duplicate checks. */
  readonly allMembers = signal<CollectionMember[]>([]);
  readonly editing = signal(false);
  readonly edit = signal({ name: '', category: '', tags: '', description: '' });

  readonly dimension = signal<Dimension>('type');
  readonly comp = computed(() => composition(this.selected()?.kind ?? 'watch', this.allMembers(), this.dimension()));
  readonly coverage = computed(() => coverageText(this.comp(), this.dimension()));
  readonly weights = computed(() => weightSummary(this.allMembers()));
  readonly providerCount = computed(() => new Set(this.allMembers().map(m => m.instrument.provider)).size);
  private readonly memberKeys = computed(() => new Set(this.allMembers().map(m => instrumentKey(m.instrument))));

  readonly searchProvider = signal<ProviderId>('YAHOO');
  readonly searchProviders = computed(() => this.market.providers().filter(p => p.search));
  readonly searchText = signal('');
  readonly hits = signal<SymbolInfo[]>([]);
  readonly searching = signal(false);
  readonly searchError = signal('');
  private readonly search$ = new Subject<{ q: string; provider: ProviderId }>();
  readonly manualProvider = signal('YAHOO');
  readonly manualSymbol = signal('');

  readonly memberEdit = signal<CollectionMember | null>(null);
  readonly memberDraft = signal({ note: '', tags: '', weight: '' });
  readonly othersWeight = computed(() => {
    const id = this.memberEdit()?.id;
    return weightSummary(this.allMembers().filter(m => m.id !== id)).total;
  });
  readonly memberWeightProblem = computed(() => {
    const raw = this.memberDraft().weight.trim();
    return weightProblem(raw === '' ? null : Number(raw), this.othersWeight());
  });
  readonly targets = computed(() => this.everything().filter(c => c.id !== this.selected()?.id && !c.archived && c.kind !== 'linked'));
  readonly transferTo = signal(0);

  private readonly reload$ = new Subject<void>();
  /** The collection asked for last; a slower answer for an earlier one is dropped. */
  private openId = 0;

  constructor() {
    this.searchProvider.set(this.market.provider());
    this.manualProvider.set(this.market.provider());

    this.reload$.pipe(
      switchMap(() => {
        this.loading.set(true);
        return forkJoin({
          list: this.api.list({ q: this.query(), kind: this.kindFilter(), category: this.category(), tag: this.tag(), archived: this.archived(), sort: this.sort() }),
          all: this.api.list({ archived: 'include' }),
        }).pipe(catchError(err => { this.loadError.set(describeHttpError(err)); return of(null); }));
      }),
      takeUntilDestroyed(this.destroyRef),
    ).subscribe(r => {
      this.loading.set(false);
      if (!r) return;
      this.loadError.set('');
      this.collections.set(r.list);
      this.everything.set(r.all);
      const sel = this.selected();
      if (sel) {
        const fresh = r.all.find(c => c.id === sel.id);
        if (fresh) this.selected.set(fresh); else this.close();
      }
      if (!this.targets().some(t => t.id === this.transferTo())) this.transferTo.set(this.targets()[0]?.id ?? 0);
    });

    this.search$.pipe(
      debounceTime(300),
      distinctUntilChanged((a, b) => a.q === b.q && a.provider === b.provider),
      switchMap(({ q, provider }) => {
        this.searchError.set('');
        if (q.length < 2) { this.searching.set(false); return of([] as SymbolInfo[]); }
        this.searching.set(true);
        return this.market.search(q, provider).pipe(catchError(err => { this.searchError.set('Search failed: ' + describeHttpError(err)); return of([] as SymbolInfo[]); }));
      }),
      takeUntilDestroyed(this.destroyRef),
    ).subscribe(list => { this.searching.set(false); this.hits.set(list.slice(0, 20)); });

    this.reload$.next();
  }

  utc(value: string): string { return formatUtcDateTime(value); }

  setQuery(value: string): void {
    this.query.set(value);
    this.reload$.next();
  }

  setFilter(key: 'kind' | 'category' | 'tag' | 'archived' | 'sort', value: string): void {
    switch (key) {
      case 'kind': this.kindFilter.set(value as CollectionKind | ''); break;
      case 'category': this.category.set(value); break;
      case 'tag': this.tag.set(value); break;
      case 'archived': this.archived.set(value as ArchiveFilter); break;
      case 'sort': this.sort.set(value as CollectionSort); break;
    }
    this.reload$.next();
  }

  setDraft(key: 'name' | 'kind' | 'category' | 'tags' | 'description', value: string): void {
    this.draft.update(d => ({ ...d, [key]: value }));
  }

  create(): void {
    const d = this.draft();
    this.run(this.api.create({ name: d.name.trim(), kind: d.kind, category: d.category.trim(), description: d.description.trim(), tags: parseTags(d.tags) }), c => {
      this.creating.set(false);
      this.draft.set({ name: '', kind: 'watch', category: '', tags: '', description: '' });
      this.notify.success(`${c.name} created.`);
      this.open(c.id, c);
      this.reload$.next();
    });
  }

  open(id: number, known?: Collection): void {
    this.editing.set(false);
    this.memberEdit.set(null);
    this.offset.set(0);
    this.page.set([]);
    this.allMembers.set([]);
    this.total.set(0);
    this.openId = id;
    this.selected.set(known ?? this.everything().find(c => c.id === id) ?? this.collections().find(c => c.id === id) ?? null);
    this.loadMembers(id);
  }

  close(): void {
    this.openId = 0;
    this.selected.set(null);
    this.page.set([]);
    this.allMembers.set([]);
    this.memberEdit.set(null);
  }

  goto(offset: number): void {
    const c = this.selected();
    if (!c) return;
    this.offset.set(Math.max(0, offset));
    this.api.get(c.id, PAGE, this.offset()).subscribe({
      next: d => { this.page.set(d.members.members); this.total.set(d.members.total); },
      error: err => this.notify.error(describeHttpError(err)),
    });
  }

  /** The visible page plus every member for the composition (two pages of 500 at most). */
  private loadMembers(id: number): void {
    forkJoin({ detail: this.api.get(id, PAGE, this.offset()), first: this.api.members(id, 500, 0) }).pipe(
      switchMap(({ detail, first }) => first.total > 500
        ? this.api.members(id, 500, 500).pipe(switchMap(second => of({ detail, all: [...first.members, ...second.members] })))
        : of({ detail, all: first.members })),
    ).subscribe({
      next: ({ detail, all }) => {
        // The user picked another collection while this one loaded.
        if (this.openId !== id) return;
        this.selected.set(detail.collection);
        this.page.set(detail.members.members);
        this.total.set(detail.members.total);
        if (this.offset() >= detail.members.total && this.offset() > 0) this.goto(this.offset() - PAGE);
        this.allMembers.set(all);
        const m = this.memberEdit();
        if (m) this.memberEdit.set(all.find(x => x.id === m.id) ?? null);
      },
      error: err => {
        this.notify.error(describeHttpError(err));
        this.close();
      },
    });
  }

  startEdit(c: Collection): void {
    this.edit.set({ name: c.name, category: c.category ?? '', tags: (c.tags ?? []).join(', '), description: c.description ?? '' });
    this.editing.set(true);
  }

  setEdit(key: 'name' | 'category' | 'tags' | 'description', value: string): void {
    this.edit.update(e => ({ ...e, [key]: value }));
  }

  saveEdit(c: Collection): void {
    const e = this.edit();
    this.run(this.api.update(c.id, { name: e.name.trim(), category: e.category.trim(), description: e.description.trim(), tags: parseTags(e.tags) }), fresh => {
      this.editing.set(false);
      this.selected.set(fresh);
      this.reload$.next();
    });
  }

  setArchived(c: Collection, archived: boolean): void {
    this.run(this.api.update(c.id, { archived }), fresh => {
      this.selected.set(fresh);
      this.notify.info(archived ? `${c.name} archived. Choose "Archived" under Show to find it again.` : `${c.name} restored.`);
      this.reload$.next();
    });
  }

  async remove(c: Collection): Promise<void> {
    const ok = await this.notify.confirm({
      title: 'Delete collection',
      message: `Delete ${c.name} and its ${c.member_count} instrument${c.member_count === 1 ? '' : 's'}? The instruments stay in your other collections. This cannot be undone.`,
      confirmLabel: 'Delete', danger: true,
    });
    if (!ok) return;
    this.run(this.api.delete(c.id), () => {
      this.close();
      this.notify.success(`${c.name} deleted.`);
      this.reload$.next();
    });
  }

  has(provider: string, symbol: string): boolean {
    return this.memberKeys().has(instrumentKey({ provider, symbol }));
  }

  setSearchProvider(provider: ProviderId): void {
    this.searchProvider.set(provider);
    this.search$.next({ q: this.searchText().trim(), provider });
  }

  setSearch(text: string): void {
    this.searchText.set(text);
    this.search$.next({ q: text.trim(), provider: this.searchProvider() });
  }

  addHit(h: SymbolInfo): void {
    this.addInstrument(instrumentFrom(this.searchProvider(), h));
  }

  addSelected(): void {
    this.addInstrument(instrumentFrom(this.market.trackedProvider(), {
      symbol: this.market.trackedSymbol(), name: this.market.trackedName(), exchange: this.market.trackedExchange(),
    }));
  }

  addManual(): void {
    this.addInstrument(instrumentFrom(this.manualProvider(), { symbol: this.manualSymbol() }), () => this.manualSymbol.set(''));
  }

  private addInstrument(instrument: ReturnType<typeof instrumentFrom>, done?: () => void): void {
    const c = this.selected();
    if (!c) return;
    this.run(this.api.addMember(c.id, { instrument }), m => {
      done?.();
      this.notify.success(`${m.instrument.symbol} added to ${c.name}.`);
      this.refreshSelected();
    });
  }

  openInMarkets(m: CollectionMember): void {
    this.market.view({ symbol: m.instrument.symbol, provider: m.instrument.provider });
    void this.router.navigate(['/markets']);
  }

  startMemberEdit(m: CollectionMember): void {
    this.memberEdit.set(m);
    this.memberDraft.set({ note: m.note ?? '', tags: (m.tags ?? []).join(', '), weight: m.weight !== undefined && m.weight !== null ? String(m.weight) : '' });
    if (!this.targets().some(t => t.id === this.transferTo())) this.transferTo.set(this.targets()[0]?.id ?? 0);
  }

  setMemberDraft(key: 'note' | 'tags' | 'weight', value: string): void {
    this.memberDraft.update(d => ({ ...d, [key]: value }));
  }

  saveMember(c: Collection, m: CollectionMember): void {
    const d = this.memberDraft();
    const patch: { note: string; tags: string[]; weight?: number; clear_weight?: boolean } = { note: d.note.trim(), tags: parseTags(d.tags) };
    if (c.kind === 'model') {
      if (d.weight.trim() === '') { if (m.weight !== undefined && m.weight !== null) patch.clear_weight = true; }
      else patch.weight = Number(d.weight);
    }
    this.run(this.api.updateMember(c.id, m.id, patch), () => {
      this.notify.success(`${m.instrument.symbol} saved.`);
      this.refreshSelected();
    });
  }

  transfer(c: Collection, m: CollectionMember, move: boolean): void {
    const target = this.targets().find(t => t.id === this.transferTo());
    if (!target) return;
    this.run(this.api.transfer(c.id, m.id, target.id, move), () => {
      this.notify.success(`${m.instrument.symbol} ${move ? 'moved' : 'copied'} to ${target.name}.`);
      if (move) this.memberEdit.set(null);
      this.refreshSelected();
    });
  }

  async removeMember(m: CollectionMember): Promise<void> {
    const c = this.selected();
    if (!c) return;
    const ok = await this.notify.confirm({ title: 'Remove instrument', message: `Remove ${m.instrument.symbol} (${m.instrument.provider}) from ${c.name}?`, confirmLabel: 'Remove', danger: true });
    if (!ok) return;
    this.run(this.api.removeMember(c.id, m.id), () => {
      if (this.memberEdit()?.id === m.id) this.memberEdit.set(null);
      this.refreshSelected();
    });
  }

  exportAll(): void {
    this.api.exportAll().subscribe({
      next: text => {
        const url = URL.createObjectURL(new Blob([text], { type: 'application/json' }));
        const a = document.createElement('a');
        a.href = url;
        a.download = exportFileName(new Date());
        a.click();
        setTimeout(() => URL.revokeObjectURL(url), 0);
      },
      error: err => this.notify.error(describeHttpError(err), 'Export failed'),
    });
  }

  async importFile(event: Event): Promise<void> {
    const input = event.target as HTMLInputElement;
    const file = input.files?.[0];
    input.value = '';
    if (!file) return;
    let doc: unknown;
    try {
      doc = JSON.parse(await file.text());
    } catch {
      this.notify.error(`${file.name} is not a JSON file.`, 'Import failed');
      return;
    }
    this.run(this.api.import(doc, 'rename'), results => {
      this.importLines.set(importSummary(results));
      this.reload$.next();
    }, 'Import failed');
  }

  private refreshSelected(): void {
    const c = this.selected();
    if (c) this.loadMembers(c.id);
    this.reload$.next();
  }

  private run<T>(source: Observable<T>, next: (value: T) => void, title?: string): void {
    this.busy.set(true);
    source.subscribe({
      next: v => { this.busy.set(false); next(v); },
      error: err => { this.busy.set(false); this.notify.error(describeHttpError(err), title); },
    });
  }
}
