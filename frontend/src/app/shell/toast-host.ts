import { ChangeDetectionStrategy, Component, ElementRef, effect, inject, viewChild } from '@angular/core';
import { NotifyService } from '../core/notify.service';

/** Renders toasts and the shared confirmation dialog. */
@Component({
  selector: 'fs-toast-host',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="toast-stack" aria-live="polite" aria-atomic="false">
      @for (t of notify.toasts(); track t.id) {
        <div class="toast show fs-toast" [class.is-error]="t.kind === 'error'" [class.is-success]="t.kind === 'success'"
             [attr.role]="t.kind === 'error' ? 'alert' : 'status'">
          <i class="bi toast-icon" aria-hidden="true"
             [class.bi-check-circle-fill]="t.kind === 'success'" [class.bi-exclamation-octagon-fill]="t.kind === 'error'"
             [class.bi-info-circle-fill]="t.kind === 'info'"></i>
          <div class="toast-text">
            @if (t.title) { <strong>{{ t.title }}</strong> }
            <span>{{ t.message }}</span>
          </div>
          <button type="button" class="btn-close" aria-label="Dismiss notification" (click)="notify.dismiss(t.id)"></button>
        </div>
      }
    </div>

    <dialog #dialog class="fs-dialog" (close)="notify.settleConfirm(false)" (cancel)="$event.preventDefault(); notify.settleConfirm(false)"
            aria-labelledby="fs-confirm-title">
      @if (notify.confirmRequest(); as req) {
        <form method="dialog" (submit)="$event.preventDefault(); notify.settleConfirm(true)">
          <div class="dialog-body">
            <span class="dialog-icon" [class.danger]="req.danger" aria-hidden="true">
              <i class="bi" [class.bi-exclamation-triangle]="req.danger" [class.bi-question-circle]="!req.danger"></i>
            </span>
            <div>
              <h2 id="fs-confirm-title">{{ req.title }}</h2>
              <p>{{ req.message }}</p>
            </div>
          </div>
          <div class="dialog-actions">
            <button type="button" class="btn btn-outline-secondary" (click)="notify.settleConfirm(false)">Cancel</button>
            <button type="submit" class="btn" [class.btn-danger]="req.danger" [class.btn-primary]="!req.danger" autofocus>{{ req.confirmLabel }}</button>
          </div>
        </form>
      }
    </dialog>
  `,
  styles: [`
    .toast-stack { position: fixed; right: 1rem; bottom: 1rem; z-index: 2100; display: grid; gap: .6rem; width: min(400px, calc(100vw - 2rem)); }
    .fs-toast { display: flex; align-items: flex-start; gap: .7rem; padding: .8rem .9rem; width: 100%; border-left: 3px solid var(--fs-info); }
    .fs-toast.is-error { border-left-color: var(--fs-neg); }
    .fs-toast.is-success { border-left-color: var(--fs-pos); }
    .toast-icon { font-size: 1.1rem; color: var(--fs-info); margin-top: 1px; }
    .is-error .toast-icon { color: var(--fs-neg); }
    .is-success .toast-icon { color: var(--fs-pos); }
    .toast-text { display: grid; gap: .1rem; flex: 1; min-width: 0; font-size: .875rem; overflow-wrap: anywhere; }
    .toast-text strong { font-size: .875rem; }
    .btn-close { flex: 0 0 auto; font-size: .7rem; margin-top: 4px; }
    .fs-dialog {
      width: min(460px, calc(100vw - 2rem)); padding: 0; border: 1px solid var(--fs-border); border-radius: var(--bs-border-radius-xl);
      background: var(--fs-surface); color: var(--fs-text); box-shadow: var(--fs-shadow-lg);
    }
    .fs-dialog::backdrop { background: rgba(2, 6, 23, .55); backdrop-filter: blur(2px); }
    .dialog-body { display: flex; gap: 1rem; padding: 1.4rem 1.4rem 1rem; }
    .dialog-body h2 { font-size: 1.05rem; margin: .15rem 0 .35rem; }
    .dialog-body p { margin: 0; color: var(--fs-text-2); font-size: .9rem; overflow-wrap: anywhere; }
    .dialog-icon { display: grid; place-items: center; flex: 0 0 auto; width: 2.4rem; height: 2.4rem; border-radius: 50%; background: var(--fs-accent-soft); color: var(--fs-accent-text); font-size: 1.15rem; }
    .dialog-icon.danger { background: var(--fs-neg-soft); color: var(--fs-neg); }
    .dialog-actions { display: flex; justify-content: flex-end; gap: .5rem; padding: .9rem 1.4rem 1.2rem; }
  `],
})
export class ToastHostComponent {
  readonly notify = inject(NotifyService);
  private readonly dialog = viewChild.required<ElementRef<HTMLDialogElement>>('dialog');

  constructor() {
    effect(() => {
      const el = this.dialog().nativeElement;
      const request = this.notify.confirmRequest();
      if (request && !el.open) el.showModal();
      if (!request && el.open) el.close();
    });
  }
}
