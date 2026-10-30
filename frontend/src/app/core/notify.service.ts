import { Injectable, signal } from '@angular/core';

export type ToastKind = 'success' | 'error' | 'info';

export interface Toast {
  id: number;
  message: string;
  kind: ToastKind;
  title?: string;
}

interface ConfirmRequest {
  title: string;
  message: string;
  confirmLabel: string;
  danger: boolean;
  resolve: (ok: boolean) => void;
}

/**
 * Notifications (toasts) and confirmation prompts rendered by the app shell.
 * Replaces Material's snack bar and dialog with lightweight signals.
 */
@Injectable({ providedIn: 'root' })
export class NotifyService {
  readonly toasts = signal<Toast[]>([]);
  readonly confirmRequest = signal<ConfirmRequest | null>(null);
  private nextId = 1;

  show(message: string, kind: ToastKind = 'info', title?: string): void {
    const id = this.nextId++;
    this.toasts.update(list => [...list.slice(-3), { id, message, kind, title }]);
    setTimeout(() => this.dismiss(id), kind === 'error' ? 7000 : 4000);
  }

  success(message: string, title?: string): void { this.show(message, 'success', title); }
  error(message: string, title?: string): void { this.show(message, 'error', title); }
  info(message: string, title?: string): void { this.show(message, 'info', title); }

  dismiss(id: number): void {
    this.toasts.update(list => list.filter(t => t.id !== id));
  }

  /** Asks the user to confirm an action; resolves false when cancelled. */
  confirm(options: { title: string; message: string; confirmLabel?: string; danger?: boolean }): Promise<boolean> {
    this.confirmRequest()?.resolve(false);
    return new Promise(resolve => {
      this.confirmRequest.set({
        title: options.title,
        message: options.message,
        confirmLabel: options.confirmLabel ?? 'Confirm',
        danger: options.danger ?? false,
        resolve,
      });
    });
  }

  settleConfirm(ok: boolean): void {
    const request = this.confirmRequest();
    this.confirmRequest.set(null);
    request?.resolve(ok);
  }
}
