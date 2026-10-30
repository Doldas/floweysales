import { ChangeDetectionStrategy, Component, inject } from '@angular/core';

import { LedgerComponent } from '../components/ledger';
import { MarketState } from '../core/market-state';

/** Paper-trading desk: ticket with pre-trade checks, positions and blotter. */
@Component({
  selector: 'fs-portfolio-page',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [LedgerComponent],
  template: `
    <div class="fs-page">
      <header class="fs-page-header">
        <div>
          <div class="fs-eyebrow">Simulation</div>
          <h1>Paper portfolio</h1>
          <p>Practise decisions against live marks. Simulated fills only; nothing reaches a broker.</p>
        </div>
        <span class="fs-status is-info no-dot"><i class="bi bi-shield-lock" aria-hidden="true"></i> Paper trading</span>
      </header>
      <fse-ledger [trackedSymbol]="market.trackedSymbol()" [livePrice]="market.livePrice()" [currency]="market.currency()" />
    </div>
  `,
})
export class PortfolioPage {
  readonly market = inject(MarketState);
}
