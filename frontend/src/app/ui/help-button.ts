import { ChangeDetectionStrategy, Component, input } from '@angular/core';
import { RouterLink } from '@angular/router';

import { topicById } from '../help/help-topics';

/** A small "?" that opens the Help article for a screen's topic. */
@Component({
  selector: 'fs-help-button',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink],
  template: `
    <a class="help-btn" routerLink="/help" [queryParams]="{ topic: topic() }" target="_blank"
       [attr.aria-label]="'Help: ' + (title() || topic()) + ' (opens in a new tab)'" [title]="'Help: ' + title()">
      <i class="bi bi-question-circle" aria-hidden="true"></i>
    </a>
  `,
  styles: [`
    :host { display: inline-flex; vertical-align: middle; }
    .help-btn { color: var(--fs-muted); font-size: .95rem; line-height: 1; padding: .15rem; border-radius: 50%; text-decoration: none; }
    .help-btn:hover { color: var(--fs-accent); }
    .help-btn:focus-visible { box-shadow: var(--fs-focus); outline: 0; }
  `],
})
export class HelpButtonComponent {
  readonly topic = input.required<string>();
  title(): string { return topicById(this.topic())?.title ?? ''; }
}
