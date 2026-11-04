import { DatePipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, inject, signal } from '@angular/core';

import { SavedTransactions } from '../core/saved-transactions';
import { NotifyService } from '../core/notify.service';
import { formatBytes } from './plugin-tools';
import { uploadProblem, usedPercent } from './saved-transactions';

/**
 * The user's saved transaction exports. Saved files are encrypted on the
 * server and let the AI agent calculate tax without pasting the data into the
 * chat. Nothing is kept in browser storage.
 */
@Component({
  selector: 'fs-saved-transactions',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DatePipe],
  template: `
<section class="card mb-3" aria-labelledby="saved-tx-title">
  <div class="card-header"><h2 class="card-title" id="saved-tx-title"><i class="bi bi-safe2 me-2" aria-hidden="true"></i>Saved transactions</h2></div>
  <div class="card-body">
    <p class="form-note">Save your broker exports (CSV) here, encrypted on the server, and the AI agent can calculate your tax from them when you ask, without you pasting anything. Only you and an agent acting for you can use them. Delete them any time.</p>
    @if (store.files().length) {
      <ul class="list-unstyled mb-2">
        @for (f of store.files(); track f.id) {
          <li class="d-flex align-items-center gap-2 py-1">
            <i class="bi bi-file-earmark-text" aria-hidden="true"></i>
            <span class="me-auto">{{ f.name }} <small class="text-muted">{{ size(f.size_bytes) }} · saved {{ f.created_at | date: 'yyyy-MM-dd' }} UTC</small></span>
            <button type="button" class="btn btn-sm btn-outline-danger" [disabled]="busy()" (click)="remove(f.id, f.name)" [attr.aria-label]="'Delete ' + f.name">Delete</button>
          </li>
        }
      </ul>
      <div class="progress mb-2" role="progressbar" aria-label="Storage used" [attr.aria-valuenow]="percent()" aria-valuemin="0" aria-valuemax="100" style="height:6px">
        <div class="progress-bar" [style.width.%]="percent()"></div>
      </div>
    } @else if (store.loaded()) {
      <p class="muted small">Nothing saved yet.</p>
    }
    <label class="btn btn-sm btn-primary mb-0" [class.disabled]="busy()">
      <i class="bi bi-upload me-1" aria-hidden="true"></i> Save a file
      <input type="file" accept=".csv,.txt,.tsv" multiple class="visually-hidden" [disabled]="busy()" (change)="pick($any($event.target))">
    </label>
    @if (problem()) { <p class="text-danger small mt-2 mb-0" role="alert">{{ problem() }}</p> }
  </div>
</section>`,
})
export class SavedTransactionsPanel {
  protected readonly store = inject(SavedTransactions);
  private readonly notify = inject(NotifyService);
  protected readonly busy = signal(false);
  protected readonly problem = signal('');

  constructor() { void this.store.refresh(); }

  protected size = formatBytes;
  protected percent = () => usedPercent(this.store.files(), this.store.limits());

  protected async pick(el: HTMLInputElement): Promise<void> {
    const chosen = Array.from(el.files ?? []);
    el.value = '';
    this.problem.set('');
    this.busy.set(true);
    try {
      for (const file of chosen) {
        const why = uploadProblem(file.name, file.size, this.store.files(), this.store.limits());
        if (why) { this.problem.set(why); continue; }
        try {
          await this.store.save(file.name, await file.text());
          this.notify.show(`${file.name} saved`, 'success');
        } catch (e) {
          this.problem.set(e instanceof Error ? e.message : 'The file could not be saved.');
        }
      }
    } finally {
      this.busy.set(false);
    }
  }

  protected async remove(id: number, name: string): Promise<void> {
    this.busy.set(true);
    this.problem.set('');
    try {
      await this.store.remove(id);
      this.notify.show(`${name} deleted`, 'success');
    } catch (e) {
      this.problem.set(e instanceof Error ? e.message : 'The file could not be deleted.');
    } finally {
      this.busy.set(false);
    }
  }
}
