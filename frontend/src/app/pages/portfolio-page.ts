import { ChangeDetectionStrategy, Component, inject, signal, viewChild } from '@angular/core';

import { LedgerComponent } from '../components/ledger';
import { PortfolioRiskComponent } from '../components/portfolio-risk-panel';
import type { TicketPrefill } from '../components/portfolio-risk';
import { MarketState } from '../core/market-state';

/** Paper-trading desk: ticket with pre-trade checks, positions and blotter, plus a Risk tab. */
@Component({
  selector: 'fs-portfolio-page',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [LedgerComponent, PortfolioRiskComponent],
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
      <div class="fs-segmented mb-3" role="group" aria-label="Portfolio view">
        <button type="button" [attr.aria-pressed]="view() === 'desk'" (click)="view.set('desk')">Desk</button>
        <button type="button" [attr.aria-pressed]="view() === 'risk'" (click)="view.set('risk')">Risk</button>
      </div>
      <div [hidden]="view() !== 'desk'">
        <fse-ledger [trackedSymbol]="market.trackedSymbol()" [livePrice]="market.livePrice()" [currency]="market.currency()" />
      </div>
      @if (view() === 'risk') { <fs-portfolio-risk (prefill)="prefill($event)" /> }
    </div>
  `,
})
export class PortfolioPage {
  readonly market = inject(MarketState);
  readonly view = signal<'desk' | 'risk'>('desk');
  private readonly ledger = viewChild(LedgerComponent);

  /** Aims the order ticket at a suggestion; the user still reviews and confirms. */
  prefill(t: TicketPrefill): void {
    this.market.view({ symbol: t.symbol, provider: this.market.provider() });
    this.view.set('desk');
    this.ledger()?.prefill(t.symbol, t.side, t.quantity);
  }
}
