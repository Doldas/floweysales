import { ChangeDetectionStrategy, Component, ElementRef, computed, input, signal, viewChild } from '@angular/core';
import { RouterLink } from '@angular/router';

import { guideUrl, topicById } from '../help/help-topics';
import { renderMarkdown } from '../markdown';

/**
 * A small "?" that opens the Help article for a screen's topic in a side panel,
 * so the reader keeps their place. The panel links to the full Help page and the
 * documentation site. This is written documentation, never AI-generated text.
 */
@Component({
  selector: 'fs-help-button',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink],
  template: `
    <button type="button" class="help-btn" #opener (click)="open()"
       [attr.aria-label]="'Help: ' + title()" [title]="'Help: ' + title()" aria-haspopup="dialog" [attr.aria-expanded]="shown()">
      <i class="bi bi-question-circle" aria-hidden="true"></i>
    </button>
    @if (shown()) {
      <aside class="help-panel" role="dialog" aria-modal="false" [attr.aria-label]="'Help: ' + title()" (keydown.escape)="close()">
        <header>
          <strong>{{ title() }}</strong>
          <button type="button" class="btn btn-icon btn-sm" #closer (click)="close()" aria-label="Close help">
            <i class="bi bi-x-lg" aria-hidden="true"></i>
          </button>
        </header>
        <div class="help-body chat-markdown" [innerHTML]="html()"></div>
        <footer>
          <a routerLink="/help" [queryParams]="{ topic: topic() }" (click)="close()">Open in Help</a>
          <a [href]="guide()" target="_blank" rel="noopener noreferrer">Read more in the user guide</a>
          <small>Written documentation, not AI-generated.</small>
        </footer>
      </aside>
    }
  `,
  styles: [`
    :host { display: inline-flex; vertical-align: middle; }
    .help-btn { color: var(--fs-muted); font-size: .95rem; line-height: 1; padding: .15rem; min-width: 1.5rem; min-height: 1.5rem; display: inline-grid; place-items: center; border-radius: 50%; border: 0; background: none; }
    .help-btn:hover { color: var(--fs-accent); }
    .help-btn:focus-visible { box-shadow: var(--fs-focus); outline: 0; }
    .help-panel { position: fixed; top: 0; right: 0; bottom: 0; width: min(26rem, 100vw); z-index: 1060; display: flex; flex-direction: column;
      background: var(--fs-surface); color: var(--fs-text); border-left: 1px solid var(--fs-border); box-shadow: -8px 0 24px rgba(0, 0, 0, .18); font-size: .95rem; font-weight: normal; }
    .help-panel header { display: flex; align-items: center; justify-content: space-between; padding: .75rem 1rem; border-bottom: 1px solid var(--fs-border); }
    .help-body { flex: 1; overflow-y: auto; padding: 1rem; text-align: left; }
    .help-panel footer { display: flex; flex-wrap: wrap; gap: .5rem 1rem; align-items: center; padding: .75rem 1rem; border-top: 1px solid var(--fs-border); }
    .help-panel footer small { color: var(--fs-muted); flex-basis: 100%; }
  `],
})
export class HelpButtonComponent {
  readonly topic = input.required<string>();
  readonly shown = signal(false);
  private readonly opener = viewChild<ElementRef<HTMLButtonElement>>('opener');
  private readonly closer = viewChild<ElementRef<HTMLButtonElement>>('closer');

  readonly guide = computed(() => guideUrl(topicById(this.topic())));
  readonly title = computed(() => topicById(this.topic())?.title ?? this.topic());
  readonly html = computed(() => renderMarkdown(topicById(this.topic())?.body ?? ''));

  open(): void {
    this.shown.set(true);
    setTimeout(() => this.closer()?.nativeElement.focus());
  }

  close(): void {
    this.shown.set(false);
    this.opener()?.nativeElement.focus();
  }
}
