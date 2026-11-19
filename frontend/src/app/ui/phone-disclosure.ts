import { ChangeDetectionStrategy, Component, input, signal } from '@angular/core';

/** Keeps the same content and state on desktop and in a foldable phone panel. */
@Component({
  selector: 'fs-phone-disclosure',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <button type="button" class="phone-toggle" [attr.aria-expanded]="open()" [attr.aria-controls]="panelId()"
            (click)="open.update(value => !value)">
      <span>{{ label() }}</span>
      <i class="bi" [class.bi-chevron-down]="!open()" [class.bi-chevron-up]="open()" aria-hidden="true"></i>
    </button>
    <div [id]="panelId()" class="phone-panel" [class.is-open]="open()"><ng-content /></div>
  `,
  styles: [`
    :host { display: block; min-width: 0; }
    .phone-toggle { display: none; }
    @media (max-width: 700px) {
      .phone-toggle {
        display: flex; align-items: center; justify-content: space-between; gap: .75rem;
        min-height: 44px; width: 100%; padding: .5rem .8rem;
        border: 1px solid var(--fs-border); border-radius: var(--bs-border-radius-lg);
        background: var(--fs-surface); color: var(--fs-text);
        font: inherit; font-weight: 600; text-align: left;
      }
      .phone-toggle .bi { color: var(--fs-muted); }
      .phone-panel { display: none; }
      .phone-panel.is-open { display: block; margin-top: .5rem; }
    }
  `],
})
export class PhoneDisclosureComponent {
  readonly label = input.required<string>();
  readonly panelId = input.required<string>();
  readonly open = signal(false);
}
