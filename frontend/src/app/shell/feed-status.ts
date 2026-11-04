import { ChangeDetectionStrategy, Component, ElementRef, HostListener, inject, signal } from '@angular/core';

import { MarketState } from '../core/market-state';

let nextId = 0;

/**
 * The price-feed badge. It says what the feed is doing in plain words and
 * opens a popover with the connection, freshness, data type and provider as
 * separate facts. A stop-loss signal is a separate badge, not a feed state.
 */
@Component({
  selector: 'fs-feed-status',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <button type="button" class="fs-status feed-badge" [class]="market.feed().tone" (click)="open.update(v => !v)"
            [attr.aria-expanded]="open()" [attr.aria-controls]="id" aria-haspopup="dialog" title="What does this status mean?">
      @if (market.feed().pulse) { <span class="fs-pulse" aria-hidden="true"></span> }
      {{ market.feed().headline }}
      <i class="bi bi-info-circle" aria-hidden="true"></i>
    </button>
    <span class="visually-hidden" role="status">{{ market.feed().headline }}</span>
    @if (market.stoppedOut()) { <span class="fs-status is-danger">Stopped out</span> }
    @if (open()) {
      <div class="feed-pop" role="dialog" [id]="id" aria-label="Price feed status">
        <dl>
          @for (d of market.feed().details; track d.label) {
            <dt>{{ d.label }}</dt><dd>{{ d.text }}</dd>
          }
        </dl>
        @if (market.feed().canReconnect) {
          <button type="button" class="btn btn-outline-secondary btn-sm" (click)="reconnect()">Reconnect</button>
        }
      </div>
    }
  `,
  styles: [`
    :host { position: relative; display: inline-flex; align-items: center; gap: .4rem; }
    .feed-badge { border: 0; cursor: pointer; }
    .feed-badge:focus-visible { box-shadow: var(--fs-focus); }
    .feed-pop {
      position: absolute; z-index: 1060; top: calc(100% + 8px); right: 0; width: min(22rem, calc(100vw - 2rem));
      padding: .8rem .9rem; background: var(--fs-surface); border: 1px solid var(--fs-border);
      border-radius: var(--bs-border-radius-lg); box-shadow: var(--fs-shadow-lg); font-size: .82rem;
    }
    dl { margin: 0 0 .5rem; }
    dt { font-weight: 650; font-size: .72rem; text-transform: uppercase; letter-spacing: .03em; color: var(--fs-muted); margin-top: .5rem; }
    dt:first-child { margin-top: 0; }
    dd { margin: 0; color: var(--fs-text-2); }
  `],
})
export class FeedStatusComponent {
  readonly market = inject(MarketState);
  private readonly host = inject(ElementRef<HTMLElement>);
  readonly id = `fs-feed-status-${nextId++}`;
  readonly open = signal(false);

  reconnect(): void {
    this.market.reconnectFeed();
    this.open.set(false);
  }

  @HostListener('document:click', ['$event'])
  onDocumentClick(event: MouseEvent): void {
    if (!this.host.nativeElement.contains(event.target as Node)) this.open.set(false);
  }

  @HostListener('keydown.escape')
  onEscape(): void {
    this.open.set(false);
  }
}
