import { ChangeDetectionStrategy, Component, DestroyRef, computed, inject, signal } from '@angular/core';
import { HelpButtonComponent } from '../ui/help-button';
import { DecimalPipe } from '@angular/common';
import { RouterLink } from '@angular/router';
import type { Subscription } from 'rxjs';

import { Access } from '../core/access';
import { AgentSession } from '../core/agent-session';
import { MarketState } from '../core/market-state';
import { MarketService, describeHttpError } from '../services';
import type { LeaderboardOption, LeaderboardResponse, LeaderboardRow } from '../models';
import { LEADERBOARD_FACTORS, LEADERBOARD_STRATEGIES, LEADERBOARD_UNIVERSES } from './investor-nodes';
import { ProgressStatusComponent } from '../ui/progress-status';
import { SkeletonComponent } from '../ui/skeleton';
import { BacktestPanelComponent } from './backtest-panel';
import { FACTOR_LABELS, STRATEGY_PLAYBOOK, alternativeList, coverageBreakdown, coverageSummary, flagList, leaderboardPrompt, methodFactors, metricValue, num, pct, reasonList, scoreSum, scoreTone, selectionLines, sourceKindLabel, universeCsv } from './leaderboard-view';

const WATCHLIST = 'watchlist';
const STORE_KEY = 'fs.leaderboard';

/**
 * Markets leaderboard: ranks a universe of companies for a strategy from
 * quarterly reports, valuation, trend, entry timing and risk. Scores come
 * from the server; this panel only displays them.
 */
@Component({
  selector: 'fs-leaderboard',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [HelpButtonComponent, DecimalPipe, RouterLink, BacktestPanelComponent, ProgressStatusComponent, SkeletonComponent],
  template: `
<section class="card board" aria-labelledby="board-title">
  <div class="card-header board-head">
    <div class="me-auto">
      <h2 class="card-title" id="board-title"><i class="bi bi-trophy me-2 text-primary" aria-hidden="true"></i>Leaderboard: worth a look now <fs-help-button topic="leaderboard" /></h2>
      <div class="board-sub">{{ strategyInfo()?.description }}</div>
    </div>
    <div class="board-actions">
      @if (data(); as d) { <span class="as-of" title="Latest price date in the data">as of {{ d.as_of || '—' }}</span> }
      <button type="button" class="btn btn-ghost btn-icon btn-sm" (click)="load()" [disabled]="loading()" aria-label="Refresh leaderboard" title="Refresh">
        <i class="bi bi-arrow-clockwise" aria-hidden="true"></i>
      </button>
    </div>
  </div>

  <div class="board-controls">
    <div class="fs-segmented" role="group" aria-label="Universe">
      @for (u of universes(); track u.id) {
        <button type="button" [attr.aria-pressed]="universe() === u.id" (click)="setUniverse(u.id)" [title]="u.description">{{ u.label }}</button>
      }
      @if (watchlistSymbols().length) {
        <button type="button" [attr.aria-pressed]="universe() === watchlist" (click)="setUniverse(watchlist)" title="The symbols on your watchlist or trackers">My watchlist</button>
      }
    </div>
    <div class="strategies" role="group" aria-label="Strategy">
      @for (s of strategies(); track s.id) {
        <button type="button" class="fs-chip" [attr.aria-pressed]="strategy() === s.id" (click)="setStrategy(s.id)" [title]="s.description">{{ s.label }}</button>
      }
    </div>
  </div>

  @if (data(); as d) {
    @if (d.universe_info; as u) {
      <section class="method origin" aria-labelledby="origin-title" data-testid="leaderboard-origin">
        <div class="method-head">
          <h3 id="origin-title">Where do these companies come from?</h3>
          <button type="button" class="btn btn-sm btn-ghost" [attr.aria-expanded]="originOpen()" aria-controls="origin-body" (click)="toggleOrigin()">{{ originOpen() ? 'Hide' : 'Show' }}</button>
        </div>
        <div id="origin-body" [hidden]="!originOpen()">
          <dl class="method-facts">
            <div><dt>List</dt><dd>{{ u.label }} &ndash; {{ u.description }}</dd></div>
            <div><dt>Source</dt><dd><span class="fs-status no-dot is-info kind">{{ kindLabel(u.source?.kind) }}</span> {{ u.source?.name || 'Not recorded' }}
              @if (u.source?.url) { &middot; <a [href]="u.source!.url!" target="_blank" rel="noopener noreferrer">original record</a> } @else { &middot; no link to an original record is recorded }</dd></div>
            <div><dt>As of</dt><dd>{{ u.source?.as_of || 'Not recorded' }}</dd></div>
            <div><dt>How chosen</dt><dd>{{ u.source?.rule || 'Not recorded' }}</dd></div>
            <div><dt>Limits</dt><dd>{{ u.source?.limitations || 'Not recorded' }}</dd></div>
            @if (d.selection; as sel) {
              <div><dt>From list to ranking</dt><dd>{{ selectionText() }}</dd></div>
              <div><dt>Where they trade</dt><dd>{{ coverageBreakdown(sel.exchanges) || 'Not known' }}@if (countryText()) { &middot; countries: {{ countryText() }} }</dd></div>
              @if (sel.excluded.length) {
                <div><dt>Left out</dt><dd>@for (x of sel.excluded; track x.symbol; let last = $last) { <strong>{{ x.symbol }}</strong> ({{ x.reason }}){{ last ? '' : '; ' }} }</dd></div>
              }
            }
          </dl>
          <p class="method-note">Three questions are answered separately: why a company is on the list (here, and under each company), where its numbers come from (the data provider, shown under each company), and why it ranks where it does (the next section).
            <button type="button" class="btn btn-sm btn-link explain-btn" (click)="download('csv')">Download list as CSV</button>
            <button type="button" class="btn btn-sm btn-link explain-btn" (click)="download('json')">as JSON</button></p>
        </div>
      </section>
    }
    @if (d.provenance?.methodology; as m) {
      <section class="method" aria-labelledby="method-title" data-testid="leaderboard-method">
        <div class="method-head">
          <h3 id="method-title">How this ranking works</h3>
          <button type="button" class="btn btn-sm btn-ghost" [attr.aria-expanded]="methodOpen()" aria-controls="method-body" (click)="toggleMethod()">{{ methodOpen() ? 'Hide' : 'Show' }}</button>
        </div>
        <div id="method-body" [hidden]="!methodOpen()">
          <p class="method-lead">{{ strategyInfo()?.description }} Every score is a percentile within <strong>{{ universeLabel() }}</strong>, so it says &ldquo;better than most of these&rdquo;, not &ldquo;will rise&rdquo;.</p>
          <dl class="method-facts">
            <div><dt>List</dt><dd>{{ universeLabel() }}@if (universeInfo()?.description) { &ndash; {{ universeInfo()!.description }} }</dd></div>
            <div><dt>Coverage</dt><dd>{{ coverage(d) }}</dd></div>
            <div><dt>Data</dt><dd>Prices as of {{ d.provenance!.price_dates.newest || 'unknown' }} (oldest {{ d.provenance!.price_dates.oldest || 'unknown' }}), from {{ d.provider || 'the selected provider' }}. Fundamentals are the latest reported quarters from the same provider.</dd></div>
            <div><dt>Method</dt><dd>Version {{ m.version }} &middot; fingerprint {{ m.hash }}</dd></div>
          </dl>
          <h4>What it weighs</h4>
          <ul class="method-factors">
            @for (f of method().used; track f.id) {
              <li>
                <div class="mf-line"><strong>{{ f.label }}</strong><span class="mf-bar" aria-hidden="true"><span [style.width.%]="f.weightPct"></span></span><span class="tabular">{{ f.weightPct }}%</span></div>
                <div class="mf-q">{{ f.question }}</div>
                <details>
                  <summary>Metrics in {{ f.label }}</summary>
                  <ul>
                    @for (x of f.metrics; track x.label) { <li><strong>{{ x.label }}</strong> &ndash; {{ x.direction }}, {{ x.shareOfFactorPct }}% of the factor. {{ x.note }}</li> }
                  </ul>
                </details>
              </li>
            }
          </ul>
          @if (method().unused.length) { <p class="method-note">Not used by this strategy: {{ method().unused.join(', ') }}.</p> }
          <details class="method-formula">
            <summary>Formulas and a worked example</summary>
            <ol>
              <li>Each metric becomes a percentile from 0 to 100 among the companies that have it (ties share their average rank; &ldquo;lower is better&rdquo; metrics are flipped).</li>
              <li>A factor is the weighted average of its metrics&rsquo; percentiles.</li>
              <li>Caps then limit a factor when there is an absolute problem: @for (c of m.caps ?? []; track c) { <span class="cap">{{ c }}.</span> }</li>
              <li>The score is the sum of factor &times; strategy weight. A factor with no data counts as {{ m.missing_factor_score ?? 40 }}, below the median: unknown is not good.</li>
            </ol>
            @if (example(); as ex) {
              <p class="method-note"><strong>{{ ex.symbol }}</strong> (rank {{ ex.rank }}): @for (line of ex.lines; track line; let last = $last) { {{ line }}{{ last ? '' : '; ' }} } &rarr; <strong>{{ ex.score }}</strong></p>
            }
          </details>
          <p class="method-note">{{ d.provenance!.disclaimer }}</p>
        </div>
      </section>
    }
  }

  @if (loading()) {
    <div class="m-3">
      <fs-progress-status [stage]="'Scoring ' + universeLabel()" />
      @if (!data()) {
        <p class="board-note">Quarterly reports and a year of prices. The first load of a universe takes a little while; later switches are instant.</p>
        <fs-skeleton shape="rows" [rows]="8" aria-hidden="true" />
      }
    </div>
  }

  @if (error(); as err) {
    <div class="fs-empty m-3" role="alert">
      <i class="bi bi-exclamation-triangle"></i>
      <h3>Could not build the leaderboard</h3>
      <p class="mb-0">{{ err }}</p>
      <button type="button" class="btn btn-sm btn-outline-secondary mt-1" (click)="load()">Try again</button>
    </div>
  } @else if (data(); as d) {
    @if (!d.rows.length) {
      <div class="fs-empty m-3"><i class="bi bi-inbox"></i><h3>No company had enough data to rank</h3><p class="mb-0">Check that a market-data provider plugin is installed and enabled.</p></div>
    } @else {
      <div class="fs-scroll-x">
        <table class="table table-hover align-middle mb-0 board-table">
          <caption class="visually-hidden">{{ strategyInfo()?.label }} ranking of {{ universeLabel() }}</caption>
          <thead>
            <tr>
              <th scope="col" class="num">#</th>
              <th scope="col">Company</th>
              <th scope="col">Score</th>
              <th scope="col" class="factors-col"><span class="visually-hidden">Factor scores</span><span aria-hidden="true" class="factor-head">@for (f of factors; track f) { <span [title]="factorLabels[f]">{{ factorLabels[f][0] }}</span> }</span></th>
              <th scope="col" class="num">Price</th>
              <th scope="col" class="num" title="Trailing twelve-month price / earnings">P/E</th>
              <th scope="col" class="num" title="Trailing twelve-month revenue growth">Rev y/y</th>
              <th scope="col" class="num" title="12-month return excluding the latest month">12-1M</th>
              <th scope="col" class="num" title="Distance from the 52-week high">From high</th>
              <th scope="col" class="num">RSI</th>
              <th scope="col">Why it ranks</th>
            </tr>
          </thead>
          <tbody>
            @for (r of visibleRows(); track r.symbol) {
              <tr class="board-row" [class.active]="r.symbol === market.trackedSymbol()" tabindex="0" (click)="open(r)" (keydown.enter)="open(r)"
                  [attr.aria-label]="'Open ' + r.symbol + ', rank ' + r.rank + ', score ' + r.score">
                <td class="num rank">{{ r.rank }}</td>
                <td class="company"><span class="sym">{{ r.symbol }}</span> <span class="grade" [attr.data-grade]="r.grade" [title]="'Grade ' + r.grade">{{ r.grade }}</span></td>
                <td class="score-cell">
                  <span class="score tabular">{{ r.score | number: '1.0-0' }}</span>
                  <span class="bar" aria-hidden="true"><span [style.width.%]="r.score" [attr.data-tone]="tone(r.score)"></span></span>
                </td>
                <td class="factors-col">
                  <span class="factors">
                    @for (f of factors; track f) {
                      <span class="factor" [attr.data-tone]="tone(factor(r, f))" [title]="factorLabels[f] + ': ' + (factor(r, f) ?? 'no data')">
                        <span [style.height.%]="factor(r, f) ?? 4"></span>
                      </span>
                    }
                  </span>
                  @if (r.coverage_pct < 100) { <span class="visually-hidden">Some factors have no data.</span> }
                </td>
                <td class="num tabular">{{ r.price !== undefined ? (r.price | number: '1.2-2') : '–' }} <small>{{ r.currency }}</small></td>
                <td class="num tabular">{{ num(r.pe_ttm) }}</td>
                <td class="num tabular" [class.text-pos]="(r.revenue_growth_pct ?? 0) > 0" [class.text-neg]="(r.revenue_growth_pct ?? 0) < 0">{{ pct(r.revenue_growth_pct) }}</td>
                <td class="num tabular" [class.text-pos]="(r.momentum_12_1_pct ?? 0) > 0" [class.text-neg]="(r.momentum_12_1_pct ?? 0) < 0">{{ pct(r.momentum_12_1_pct) }}</td>
                <td class="num tabular">{{ pct(r.from_high_pct) }}</td>
                <td class="num tabular">{{ num(r.rsi14, 0) }}</td>
                <td class="why">
                  @for (reason of reasons(r); track reason) { <span class="reason">{{ reason }}</span> }
                  @for (flag of flags(r); track flag.id) { <span class="fs-status is-warning no-dot flag">{{ flag.label }}</span> }
                  @if (r.confidence) { <span class="reason">Confidence: {{ r.confidence }}. {{ r.confidence_note }}</span> }
                  @for (alt of alternatives(r); track alt) { <span class="reason">Against: {{ alt }}</span> }
                  @if (r.breakdown) {
                    <button type="button" class="btn btn-sm btn-link explain-btn" [attr.aria-expanded]="isExplained(r.symbol)" [attr.aria-controls]="'explain-' + r.symbol"
                            (click)="toggleExplain(r.symbol); $event.stopPropagation()" (keydown.enter)="$event.stopPropagation()">
                      {{ isExplained(r.symbol) ? 'Hide score breakdown' : 'Score breakdown' }}
                    </button>
                  }
                </td>
              </tr>
              @if (r.breakdown && isExplained(r.symbol)) {
                <tr class="explain-row" [id]="'explain-' + r.symbol">
                  <td colspan="11">
                    <div class="three">
                      <section>
                        <h5>Why it is here</h5>
                        <p>{{ r.discovery?.why || 'No inclusion reason is recorded.' }}</p>
                        <p>{{ r.listing?.exchange || 'Listing unknown' }}@if (r.listing?.mic) { &middot; MIC {{ r.listing!.mic }} }@if (r.listing?.country) { &middot; {{ r.listing!.country }} }.
                          @if (r.discovery?.url) { <a [href]="r.discovery!.url!" target="_blank" rel="noopener noreferrer">Original record</a> } @else { No link to an original record is recorded. }</p>
                      </section>
                      <section>
                        <h5>Where its numbers come from</h5>
                        <p>Provider: {{ r.metrics_source?.provider || 'unknown' }}. Prices as of {{ r.metrics_source?.price_as_of || 'unknown' }}; fundamentals through {{ r.metrics_source?.fundamentals_through || 'not available' }}.</p>
                      </section>
                      <section>
                        <h5>Why it ranks here</h5>
                        <p>{{ sumLines(r).join('; ') }} &rarr; <strong>{{ r.score }}</strong></p>
                      </section>
                    </div>
                    <table class="explain-table">
                      <caption class="visually-hidden">Score breakdown of {{ r.symbol }}</caption>
                      <thead><tr><th scope="col">Factor</th><th scope="col">Metric</th><th scope="col" class="num">Value</th><th scope="col" class="num">Percentile</th><th scope="col" class="num">Weight</th></tr></thead>
                      <tbody>
                        @for (f of r.breakdown.factors; track f.factor) {
                          @if (f.weight > 0) {
                            <tr class="factor-line">
                              <th scope="row" colspan="3">{{ factorLabels[f.factor] }}@if (f.missing) { <em> &ndash; no data, counted as {{ r.breakdown.missing_factor_score }}</em> }@if (f.cap_reason) { <em> &ndash; capped from {{ f.capped_from }}: {{ f.cap_reason }}</em> }</th>
                              <td class="num tabular">{{ f.score }}</td><td class="num tabular">&times; {{ f.weight * 100 | number: '1.0-0' }}%</td>
                            </tr>
                            @for (x of f.metrics ?? []; track x.field) {
                              <tr><td></td><td>{{ x.label }} <small>({{ x.higher_is_better ? 'higher' : 'lower' }} is better)</small></td><td class="num tabular">{{ metricValue(x.value) }}</td><td class="num tabular">{{ x.percentile }}</td><td class="num tabular">{{ x.weight }}</td></tr>
                            }
                          }
                        }
                      </tbody>
                    </table>
                  </td>
                </tr>
              }
            }
          </tbody>
        </table>
      </div>
      <p class="board-sub mb-0 px-3 pt-2" data-testid="leaderboard-disclaimer">
        {{ d.provenance?.disclaimer ?? 'Research suggestion, not personalized financial advice. Scores are percentiles within this list, not predictions.' }}
        @if (d.provenance; as p) {
          <span class="fingerprint">Method {{ p.methodology.version }}/{{ p.methodology.hash }} · data {{ p.inputs_hash }} · prices {{ p.price_dates.oldest }} to {{ p.price_dates.newest }}</span>
        }
      </p>
      <div class="board-foot">
        @if (d.rows.length > 10) {
          <button type="button" class="btn btn-sm btn-ghost" (click)="showAll.set(!showAll())" [attr.aria-expanded]="showAll()">
            {{ showAll() ? 'Show top 10' : 'Show all ' + d.rows.length }}
          </button>
        }
        <button type="button" class="btn btn-sm btn-agent" (click)="askAgent()"><i class="bi bi-stars" aria-hidden="true"></i>Research the top 5</button>
        <button type="button" class="btn btn-sm btn-outline-secondary" (click)="showBacktest.set(!showBacktest())" [attr.aria-expanded]="showBacktest()" aria-controls="board-backtest">
          <i class="bi bi-clock-history" aria-hidden="true"></i>Backtest this strategy
        </button>
        @if (access.flowsWrite()) {
          <a class="btn btn-sm btn-outline-secondary" routerLink="/flows" [queryParams]="{ template: playbook() }"><i class="bi bi-diagram-3" aria-hidden="true"></i>Automate as a flow</a>
        }
        <span class="board-note me-auto">
          Scores are 0–100 percentiles within this universe from provider data; a missing factor counts as 40. Research, not investment advice.
          @if (d.unranked.length) { Not ranked (no data): {{ unrankedList() }}. }
        </span>
      </div>
    }
    @if (showBacktest()) {
      <div id="board-backtest">
        <fs-backtest-panel [universe]="universe() === watchlist ? 'custom' : universe()" [strategy]="strategy()" [strategyLabel]="strategyInfo()?.label ?? strategy()"
          [symbols]="universe() === watchlist ? watchlistSymbols() : []" />
      </div>
    }
  }
</section>
  `,
  styles: [`
:host { display: block; min-width: 0; }
.board-head { flex-wrap: wrap; gap: 0.5rem; align-items: flex-start; }
.board-sub { font-size: 0.8rem; color: var(--fs-muted); margin-top: 0.15rem; }
.board-actions { display: flex; align-items: center; gap: 0.4rem; }
.as-of { font-size: 0.75rem; color: var(--fs-muted); white-space: nowrap; }
.board-controls { display: grid; gap: 0.6rem; padding: 0.85rem 1.25rem; border-bottom: 1px solid var(--fs-border); }
.strategies { display: flex; flex-wrap: wrap; gap: 0.35rem; .fs-chip { padding: 0.2rem 0.65rem; font-size: 0.78rem; } }
.board-note { font-size: 0.75rem; color: var(--fs-muted); margin: 0; padding: 0.75rem 1.25rem; }
.board-foot { display: flex; flex-wrap: wrap; align-items: center; gap: 0.5rem; padding: 0.75rem 1.25rem; border-top: 1px solid var(--fs-border);
  .board-note { padding: 0; flex: 1 1 260px; order: 9; } }
.board-table { font-size: 0.82rem; min-width: 900px;
  th { font-size: 0.7rem; text-transform: uppercase; letter-spacing: 0.04em; color: var(--fs-muted); font-weight: 600; white-space: nowrap; }
  td, th { padding: 0.5rem 0.6rem; }
  .num { text-align: right; white-space: nowrap; small { color: var(--fs-muted); font-size: 0.68rem; } } }
.board-row { cursor: pointer; &.active td { background: var(--fs-accent-soft); } &:focus-visible { outline: 2px solid var(--fs-accent); outline-offset: -2px; } }
.rank { color: var(--fs-muted); font-weight: 600; width: 2rem; }
.company { white-space: nowrap; .sym { font-weight: 650; margin-right: 0.3rem; } }
.grade { display: inline-grid; place-items: center; width: 1.3rem; height: 1.3rem; border-radius: 6px; font-size: 0.7rem; font-weight: 700;
  background: var(--fs-surface-3); color: var(--fs-text-2);
  &[data-grade='A'] { background: var(--fs-pos-soft); color: var(--fs-pos); }
  &[data-grade='B'] { background: var(--fs-info-soft); color: var(--fs-info); }
  &[data-grade='E'] { background: var(--fs-neg-soft); color: var(--fs-neg); } }
.score-cell { white-space: nowrap; min-width: 7rem; }
.score { display: inline-block; width: 1.8rem; font-weight: 650; }
.bar { display: inline-block; vertical-align: middle; width: 4.5rem; height: 6px; border-radius: 3px; background: var(--fs-surface-3); overflow: hidden;
  span { display: block; height: 100%; border-radius: 3px; background: var(--fs-muted); } }
[data-tone='pos'] > span, .bar span[data-tone='pos'] { background: var(--fs-pos); }
[data-tone='neg'] > span, .bar span[data-tone='neg'] { background: var(--fs-neg); }
[data-tone='mid'] > span, .bar span[data-tone='mid'] { background: var(--fs-warn); }
.factors-col { width: 7.5rem; }
.factor-head, .factors { display: inline-grid; grid-template-columns: repeat(6, 0.9rem); gap: 0.2rem; text-align: center; }
.factor { display: flex; align-items: flex-end; height: 1.25rem; background: var(--fs-surface-3); border-radius: 3px; overflow: hidden;
  span { display: block; width: 100%; } }
.why { min-width: 18rem; max-width: 30rem; line-height: 1.35; }
.reason { display: block; color: var(--fs-text-2); font-size: 0.76rem; }
.flag { margin: 0.2rem 0.25rem 0 0; font-size: 0.66rem; }
.method { padding: 0.75rem 1.25rem; border-bottom: 1px solid var(--fs-border); font-size: 0.82rem; }
.method-head { display: flex; align-items: center; justify-content: space-between; gap: 0.5rem; h3 { font-size: 0.95rem; margin: 0; } }
.method h4 { font-size: 0.72rem; text-transform: uppercase; letter-spacing: 0.05em; color: var(--fs-muted); margin: 0.9rem 0 0.4rem; }
.method-lead, .method-note { color: var(--fs-text-2); margin: 0.4rem 0; }
.method-facts { display: grid; grid-template-columns: minmax(0, 1fr); gap: 0.25rem; margin: 0.5rem 0 0; div { display: grid; grid-template-columns: 6rem minmax(0, 1fr); gap: 0.5rem; } dt { color: var(--fs-muted); font-weight: 600; } dd { margin: 0; overflow-wrap: anywhere; } }
.method-factors { list-style: none; padding: 0; margin: 0; display: grid; gap: 0.5rem; }
.mf-line { display: grid; grid-template-columns: 7rem 1fr 2.5rem; align-items: center; gap: 0.6rem; }
.mf-bar { height: 6px; border-radius: 3px; background: var(--fs-surface-3); overflow: hidden; span { display: block; height: 100%; background: var(--fs-accent); } }
.mf-q { color: var(--fs-muted); }
.method details { margin-top: 0.25rem; summary { cursor: pointer; color: var(--fs-accent-text); } ul, ol { padding-left: 1.1rem; margin: 0.3rem 0; display: grid; gap: 0.2rem; } }
.cap { display: block; }
.kind { white-space: normal; max-width: 100%; }
.fingerprint { overflow-wrap: anywhere; }
.explain-btn { padding: 0; font-size: 0.74rem; color: var(--fs-accent-text); text-decoration: underline; }
.three { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 15rem), 1fr)); gap: 0.8rem; margin-bottom: 0.6rem; h5 { font-size: 0.72rem; text-transform: uppercase; letter-spacing: 0.05em; color: var(--fs-muted); margin: 0 0 0.25rem; } p { margin: 0 0 0.3rem; font-size: 0.78rem; overflow-wrap: anywhere; } }
.explain-row td { background: var(--fs-surface-2); padding: 0.6rem 1rem; }
.explain-table { width: 100%; font-size: 0.78rem; th, td { padding: 0.15rem 0.4rem; } thead th { color: var(--fs-muted); font-weight: 600; } .factor-line th, .factor-line td { border-top: 1px solid var(--fs-border); font-weight: 600; } .num { text-align: right; } }
@media (max-width: 575.98px) { .board-controls, .board-foot, .board-note { padding-inline: 1rem; } }
  `],
})
export class LeaderboardComponent {
  readonly market = inject(MarketState);
  readonly access = inject(Access);
  private readonly api = inject(MarketService);
  private readonly agent = inject(AgentSession);

  readonly watchlist = WATCHLIST;
  readonly factors = LEADERBOARD_FACTORS;
  readonly factorLabels = FACTOR_LABELS;
  readonly pct = pct;
  readonly num = num;

  readonly universe = signal('us_mega');
  readonly strategy = signal('best');
  readonly data = signal<LeaderboardResponse | null>(null);
  readonly loading = signal(false);
  readonly error = signal('');
  readonly showAll = signal(false);
  readonly showBacktest = signal(false);
  readonly methodOpen = signal(true);
  readonly originOpen = signal(true);
  readonly coverageBreakdown = coverageBreakdown;
  private readonly explained = signal<ReadonlySet<string>>(new Set());
  readonly metricValue = metricValue;
  private request?: Subscription;

  // Labels come with the first response; until then the client copy names them.
  readonly universes = computed<LeaderboardOption[]>(() => this.data()?.universes ?? LEADERBOARD_UNIVERSES.map(u => ({ ...u, description: '' })));
  readonly strategies = computed<LeaderboardOption[]>(() => this.data()?.strategies ?? LEADERBOARD_STRATEGIES.map(s => ({ ...s, description: '' })));
  readonly strategyInfo = computed(() => this.strategies().find(s => s.id === this.strategy()));
  readonly universeLabel = computed(() => this.universe() === WATCHLIST ? 'your watchlist' : this.universes().find(u => u.id === this.universe())?.label ?? this.universe());
  readonly watchlistSymbols = computed(() => [...new Set(this.market.trackedAssets().map(a => a.symbol.toUpperCase()))].slice(0, 25));
  readonly visibleRows = computed(() => (this.showAll() ? this.data()?.rows ?? [] : (this.data()?.rows ?? []).slice(0, 10)));
  readonly unrankedList = computed(() => (this.data()?.unranked ?? []).map(u => u.symbol).join(', '));
  readonly selectionText = computed(() => selectionLines(this.data()?.selection).join(' → '));
  readonly countryText = computed(() => coverageBreakdown(this.data()?.selection?.countries));
  readonly method = computed(() => methodFactors(this.data()?.provenance?.methodology));
  readonly universeInfo = computed(() => this.universes().find(u => u.id === this.universe()));
  readonly example = computed(() => {
    const row = this.data()?.rows.find(r => r.breakdown);
    return row ? { symbol: row.symbol, rank: row.rank, score: row.score, lines: scoreSum(row.breakdown) } : null;
  });
  readonly playbook = computed(() => STRATEGY_PLAYBOOK[this.strategy()] ?? 'top10-buy-now');

  constructor() {
    try {
      const saved = JSON.parse(localStorage.getItem(STORE_KEY) || '{}');
      if (typeof saved.universe === 'string' && saved.universe !== WATCHLIST) this.universe.set(saved.universe);
      if (typeof saved.strategy === 'string') this.strategy.set(saved.strategy);
      if (saved.methodOpen === false) this.methodOpen.set(false);
      if (saved.originOpen === false) this.originOpen.set(false);
    } catch { /* storage may be unavailable */ }
    inject(DestroyRef).onDestroy(() => this.request?.unsubscribe());
    this.load();
  }

  setUniverse(id: string): void {
    if (id === this.universe()) return;
    this.universe.set(id);
    this.showAll.set(false);
    this.save();
    this.load();
  }

  setStrategy(id: string): void {
    if (id === this.strategy()) return;
    this.strategy.set(id);
    this.save();
    this.load();
  }

  load(): void {
    this.request?.unsubscribe();
    this.loading.set(true);
    this.error.set('');
    const symbols = this.universe() === WATCHLIST ? this.watchlistSymbols() : [];
    this.request = this.api.getLeaderboard(this.universe(), this.strategy(), symbols).subscribe({
      next: res => { this.data.set(res); this.loading.set(false); },
      error: err => { this.error.set(describeHttpError(err)); this.loading.set(false); },
    });
  }

  open(row: LeaderboardRow): void {
    this.market.view({ symbol: row.symbol, provider: this.data()?.provider || this.market.provider() });
    // The chart and fundamentals load at the top of the page.
    window.scrollTo({ top: 0, behavior: 'smooth' });
  }

  askAgent(): void {
    const d = this.data();
    if (!d?.rows.length) return;
    this.agent.ask(leaderboardPrompt(d.rows, this.universeLabel(), this.strategyInfo()?.label ?? this.strategy(), d.as_of));
  }

  factor(row: LeaderboardRow, f: string): number | undefined {
    return row[f as keyof LeaderboardRow] as number | undefined;
  }

  tone(score: number | undefined): string { return scoreTone(score); }
  reasons(row: LeaderboardRow): string[] { return reasonList(row); }
  flags(row: LeaderboardRow) { return flagList(row); }
  alternatives(row: LeaderboardRow): string[] { return alternativeList(row); }

  toggleMethod(): void { this.methodOpen.update(v => !v); this.save(); }
  toggleOrigin(): void { this.originOpen.update(v => !v); this.save(); }
  kindLabel(kind: string | undefined): string { return sourceKindLabel(kind); }

  /** Saves the ranked list with each company's listing and the sources of its inclusion and numbers. */
  download(format: 'csv' | 'json'): void {
    const d = this.data();
    if (!d) return;
    const body = format === 'csv' ? universeCsv(d.rows)
      : JSON.stringify({ universe: d.universe_info ? { ...d.universe_info } : d.universe, selection: d.selection, generated_at: d.generated_at, as_of: d.as_of,
        companies: d.rows.map(r => ({ rank: r.rank, symbol: r.symbol, listing: r.listing, discovery: r.discovery, metrics_source: r.metrics_source })) }, null, 2);
    const url = URL.createObjectURL(new Blob([body], { type: format === 'csv' ? 'text/csv' : 'application/json' }));
    const link = document.createElement('a');
    link.href = url;
    link.download = `leaderboard-${d.universe}-${d.as_of || 'undated'}.${format}`;
    link.click();
    URL.revokeObjectURL(url);
  }
  coverage(d: LeaderboardResponse): string { return coverageSummary(d.rows, d.unranked.length); }
  sumLines(row: LeaderboardRow): string[] { return scoreSum(row.breakdown); }
  isExplained(symbol: string): boolean { return this.explained().has(symbol); }
  toggleExplain(symbol: string): void {
    this.explained.update(s => { const next = new Set(s); if (!next.delete(symbol)) next.add(symbol); return next; });
  }

  private save(): void {
    try { localStorage.setItem(STORE_KEY, JSON.stringify({ universe: this.universe(), strategy: this.strategy(), methodOpen: this.methodOpen(), originOpen: this.originOpen() })); } catch { /* ignore */ }
  }
}
