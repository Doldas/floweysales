import { ChangeDetectionStrategy, Component, inject } from '@angular/core';

import { ForumComponent } from '../components/forum';
import { MarketState } from '../core/market-state';

@Component({
  selector: 'fs-community-page',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ForumComponent],
  template: `
    <div class="fs-page">
      <header class="fs-page-header">
        <div>
          <div class="fs-eyebrow">Workspace</div>
          <h1>Community</h1>
          <p>Share theses and evidence with your team@if (market.trackedSymbol()) { · showing posts for {{ market.trackedSymbol() }}}.</p>
        </div>
      </header>
      <fse-forum [trackedSymbol]="market.trackedSymbol()" />
    </div>
  `,
})
export class CommunityPage {
  readonly market = inject(MarketState);
}
