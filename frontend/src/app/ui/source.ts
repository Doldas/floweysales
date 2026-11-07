import { ChangeDetectionStrategy, Component, ElementRef, HostListener, computed, inject, input, signal } from '@angular/core';

import { provenanceBadge, provenanceLines, safeSourceUrl, type Provenance } from '../core/provenance';
import { formatUtcDateTime } from './datetime';

let nextId = 0;

/** Where a value came from, in a popover that opens on click or keyboard, never on hover alone. */
@Component({
  selector: 'fs-source',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <button type="button" class="src-btn" [class.is-unknown]="badge() === 'Source unknown'" [attr.aria-expanded]="open()"
            [attr.aria-controls]="panelId" (click)="open.set(!open())">
      <i class="bi bi-info-circle" aria-hidden="true"></i> {{ badge() }}
    </button>
    @if (open()) {
      <div class="src-panel" [id]="panelId" role="group" aria-label="Source details">
        <dl>
          @for (line of lines(); track line.label) {
            <dt>{{ line.label }}</dt><dd>{{ line.value }}</dd>
          }
        </dl>
        @if (link()) { <a [href]="link()" target="_blank" rel="noopener noreferrer">View source</a> }
      </div>
    }
  `,
  styles: [`
    :host { position: relative; display: inline-block; }
    .src-btn { border: 0; background: transparent; color: var(--fs-muted); font-size: .75rem; padding: .1rem .25rem; min-height: 24px; min-width: 24px; display: inline-flex; align-items: center; justify-content: center; border-radius: 4px; }
    .src-btn:hover { color: var(--fs-accent); }
    .src-btn:focus-visible { box-shadow: var(--fs-focus); outline: 0; }
    .src-btn.is-unknown { color: var(--fs-warn-text, var(--fs-muted)); }
    .src-panel { position: absolute; z-index: 20; top: 100%; left: 0; min-width: 16rem; max-width: min(22rem, 85vw); margin-top: .25rem;
      padding: .6rem .75rem; background: var(--fs-surface); color: var(--fs-text); border: 1px solid var(--fs-border-strong);
      border-radius: var(--bs-border-radius); box-shadow: var(--fs-shadow, 0 4px 16px rgba(0,0,0,.15)); font-size: .8125rem; }
    dl { display: grid; grid-template-columns: auto 1fr; gap: .2rem .6rem; margin: 0 0 .4rem; }
    dt { color: var(--fs-muted); font-weight: 400; }
    dd { margin: 0; overflow-wrap: anywhere; }
  `],
})
export class SourceComponent {
  readonly fact = input<Provenance | null | undefined>();
  readonly open = signal(false);
  readonly panelId = `fs-source-${nextId++}`;
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);

  readonly badge = computed(() => provenanceBadge(this.fact()));
  readonly lines = computed(() => provenanceLines(this.fact(), formatUtcDateTime));
  readonly link = computed(() => safeSourceUrl(this.fact()));

  @HostListener('document:keydown.escape') close(): void { this.open.set(false); }
  @HostListener('document:click', ['$event']) outside(event: Event): void {
    if (this.open() && !this.host.nativeElement.contains(event.target as Node)) this.open.set(false);
  }
}
