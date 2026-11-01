import { ChangeDetectionStrategy, Component, inject } from '@angular/core';

import { FinancialsComponent } from '../components/financials';
import { UpcomingEventsComponent } from '../components/upcoming-events';
import { MarketState } from '../core/market-state';
import { SymbolSearchComponent } from '../shell/symbol-search';

/** Quarterly statements for the active instrument. */
@Component({
  selector: 'fs-financials-page',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [FinancialsComponent, SymbolSearchComponent, UpcomingEventsComponent],
  template: `
    <div class="fs-page research-container">
      <header class="fs-page-header">
        <div>
          <div class="fs-eyebrow">Fundamentals</div>
          <h1>Financials @if (market.trackedSymbol()) { <span class="text-muted fw-normal">· {{ market.trackedSymbol() }}</span> }</h1>
          <p>Quarterly income statement and cash-flow figures with provider, period and filing form.</p>
        </div>
        <fs-symbol-search class="page-search" [navigate]="false" placeholder="Switch company…" />
      </header>
      @if (market.trackedSymbol()) {
        <fs-upcoming-events [symbol]="market.trackedSymbol()" [price]="market.livePrice()" />
        <fse-financials [symbol]="market.trackedSymbol()" [provider]="market.provider()" />
      } @else {
        <div class="fs-empty"><i class="bi bi-bank"></i><h3>Choose a company</h3><p class="mb-0">Search a ticker to load its quarterly statements.</p></div>
      }
    </div>
  `,
  styles: [`.page-search { flex: 0 1 380px; min-width: min(100%, 260px); }`],
})
export class FinancialsPage {
  readonly market = inject(MarketState);
}
