import { ChangeDetectionStrategy, Component, computed, inject, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';

import { MarketService } from '../services';
import type { IndexInfo } from '../models';
import { LICENSE_STATEMENT, fetchProblems, historyLine, importProblems, refusalLines, sourceChoices } from './index-sources-view';

/**
 * Lets a user supply their own constituent list for an index: a CSV file or,
 * for indices Financial Modeling Prep serves, their own FMP key. FloweySales
 * ships no list, so an index is unavailable until the user does this. The key
 * is write-only here: it is sent once and never shown again.
 */
@Component({
  selector: 'fs-index-sources',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [FormsModule],
  template: `
    <section class="ix-src" [attr.aria-labelledby]="'ixs-h-' + index().id">
      <h4 class="h6" [id]="'ixs-h-' + index().id">Your data for {{ index().name }}</h4>
      <p class="text-muted">FloweySales ships no list of index members, because index lists are licensed by their providers. You supply your own: {{ choices() }}. The list is stored for your account only. Check the provider's terms and prices yourself before you use their data.</p>

      @if (index().source; as s) {
        <p class="ix-have" role="status">
          <strong>In use:</strong> {{ s.provider === 'fmp' ? 'Financial Modeling Prep' : 'CSV import' }}, {{ s.count }} securities as of {{ s.as_of }} (UTC), imported {{ s.imported_at }}.
          {{ history() }} You confirmed: &ldquo;{{ s.license_terms }}&rdquo;
          @if (s.unverified) { <span class="ix-warn"> This fetch has not been checked against a live response.</span> }
        </p>
        @if (index().verification; as v) {
          <div class="ix-verify" role="status">
            <strong [attr.data-level]="v.level">{{ v.level === 'verified' ? 'Verified' : 'Approximate' }}.</strong> {{ v.summary }}
            <ul>@for (c of v.checks; track c.id) { <li [class.ix-fail]="!c.ok">{{ c.ok ? 'Passed' : 'Not passed' }}: {{ c.detail }}</li> }</ul>
          </div>
        }
        <button type="button" class="btn btn-sm btn-outline-secondary" [disabled]="busy()" (click)="remove()">Remove my list</button>
      }

      <fieldset class="ix-form" [disabled]="busy()">
        <legend class="h6">Import a CSV file</legend>
        <p class="text-muted ix-hint">Columns: <code>symbol</code> (required), <code>name</code>, <code>weight</code> (percent), <code>exchange</code> (for example <code>ST</code>) and <code>isin</code> (recommended: it keeps share classes and look-alike tickers apart when you compare or combine indices). Leave a weight blank when you do not know it; it is never treated as 0.@if (index().country !== 'US') { Write Swedish tickers as <code>VOLV-B.ST</code> or fill the exchange column. }</p>
        <label>CSV file
          <input type="file" accept=".csv,text/csv" (change)="pick($event)" />
        </label>
        @if (fileName()) { <span class="ix-hint">{{ fileName() }}</span> }
        <label>History file (optional): <code>date</code>, <code>added</code>, <code>removed</code> rows
          <input type="file" accept=".csv,text/csv" (change)="pickHistory($event)" />
        </label>
        @if (historyName()) { <span class="ix-hint">{{ historyName() }}</span> }
        <label>As-of date of the list (UTC)
          <input type="date" [(ngModel)]="asOf" name="asOf" [max]="today" />
        </label>
        <label>Source of the file
          <input type="text" [(ngModel)]="sourceName" name="sourceName" maxlength="200" placeholder="For example: my broker's index export" />
        </label>
        <label>Licence or terms that let you use it
          <input type="text" [(ngModel)]="terms" name="terms" maxlength="1000" placeholder="For example: included in my data subscription" />
        </label>
        <label class="ix-check"><input type="checkbox" [(ngModel)]="confirmed" name="confirmed" /> {{ statement }}</label>
        <button type="button" class="btn btn-sm btn-primary" (click)="importCsv()">Import list</button>
      </fieldset>

      @if (hasFmp()) {
        <fieldset class="ix-form" [disabled]="busy()">
          <legend class="h6">Or fetch it with your Financial Modeling Prep key</legend>
          <p class="text-muted ix-hint">Needs your own FMP account; whether your plan includes constituent data, and what it costs, is set by FMP, so check their terms and pricing. Your key is sent only to FMP, stored encrypted for your account and never shown again.
            @if (index().id === 'index_nasdaq100') { The Nasdaq-100 fetch has not been checked against a live response and is marked unverified. }
            Past membership comes from FMP's add/remove records when your plan returns them.</p>
          <label>FMP API key
            <input type="password" autocomplete="off" spellcheck="false" [(ngModel)]="apiKey" name="apiKey" [placeholder]="fmpKeySaved() ? 'A key is saved; type a new one to replace it' : 'Paste your key'" />
          </label>
          <div class="ix-row">
            <button type="button" class="btn btn-sm btn-outline-primary" (click)="saveKey()">Save key</button>
            @if (fmpKeySaved()) { <button type="button" class="btn btn-sm btn-outline-secondary" (click)="removeKey()">Remove saved key</button> }
          </div>
          <label>Terms that let you use the data
            <input type="text" [(ngModel)]="fmpTerms" name="fmpTerms" maxlength="1000" placeholder="For example: my FMP plan's terms" />
          </label>
          <label class="ix-check"><input type="checkbox" [(ngModel)]="fmpConfirmed" name="fmpConfirmed" /> {{ statement }}</label>
          <button type="button" class="btn btn-sm btn-primary" (click)="fetchFmp()">Fetch list</button>
        </fieldset>
      } @else {
        <p class="text-muted ix-hint">No data service is supported for this index, so a CSV file is the only way to supply it.</p>
      }

      @if (lines().length) {
        <div class="ix-errors" role="alert">
          <ul>@for (l of lines(); track $index) { <li>{{ l }}</li> }</ul>
        </div>
      }
      @if (done()) { <p class="ix-done" role="status">{{ done() }}</p> }
    </section>
  `,
  styles: [`
    .ix-src { border-top: 1px solid var(--fs-border); margin-top: 0.75rem; padding-top: 0.75rem; display: grid; gap: 0.6rem; }
    .ix-form { display: grid; gap: 0.5rem; border: 1px solid var(--fs-border); border-radius: 0.4rem; padding: 0.6rem 0.8rem; max-width: 40rem; }
    .ix-form label { display: grid; gap: 0.2rem; font-size: 0.78rem; color: var(--fs-text-2); }
    .ix-form input[type=text], .ix-form input[type=password], .ix-form input[type=date] { padding: 0.3rem 0.5rem; }
    .ix-check { display: flex !important; gap: 0.5rem; align-items: flex-start; }
    .ix-hint { font-size: 0.75rem; color: var(--fs-muted); margin: 0; }
    .ix-row { display: flex; flex-wrap: wrap; gap: 0.5rem; }
    .ix-errors { border-left: 3px solid var(--fs-neg); padding-left: 0.6rem; font-size: 0.8rem; ul { margin: 0; padding-left: 1rem; } }
    .ix-done { color: var(--fs-pos); font-size: 0.8rem; margin: 0; }
    .ix-have { font-size: 0.8rem; margin: 0; }
    .ix-verify { font-size: 0.8rem; ul { margin: 0.25rem 0 0; padding-left: 1rem; } }
    .ix-verify [data-level='verified'] { color: var(--fs-pos); }
    .ix-verify [data-level='approximate'], .ix-fail { color: var(--fs-warn); }
    .ix-warn { color: var(--fs-warn); }
  `],
})
export class IndexSourcesComponent {
  private readonly api = inject(MarketService);

  readonly index = input.required<IndexInfo>();
  readonly fmpKeySaved = input(false);
  /** Emits the index id after a list was imported, fetched or removed. */
  readonly changed = output<string>();

  readonly statement = LICENSE_STATEMENT;
  readonly today = new Date().toISOString().slice(0, 10);
  readonly choices = computed(() => sourceChoices(this.index()));
  readonly history = computed(() => historyLine(this.index()));
  readonly hasFmp = computed(() => (this.index().sources ?? []).includes('fmp'));

  readonly busy = signal(false);
  readonly lines = signal<string[]>([]);
  readonly done = signal('');
  readonly fileName = signal('');
  readonly historyName = signal('');
  private csv = '';
  private historyCsv = '';

  asOf = '';
  sourceName = '';
  terms = '';
  confirmed = false;
  apiKey = '';
  fmpTerms = '';
  fmpConfirmed = false;

  pick(ev: Event): void {
    const file = (ev.target as HTMLInputElement).files?.[0];
    this.csv = '';
    this.fileName.set('');
    if (!file) return;
    if (file.size > 512 * 1024) { this.lines.set(['The file is larger than 512 KiB.']); return; }
    const reader = new FileReader();
    reader.onload = () => { this.csv = String(reader.result ?? ''); this.fileName.set(`${file.name} (${file.size} bytes)`); };
    reader.onerror = () => this.lines.set(['The file could not be read.']);
    reader.readAsText(file);
  }

  pickHistory(ev: Event): void {
    const file = (ev.target as HTMLInputElement).files?.[0];
    this.historyCsv = '';
    this.historyName.set('');
    if (!file) return;
    if (file.size > 512 * 1024) { this.lines.set(['The history file is larger than 512 KiB.']); return; }
    const reader = new FileReader();
    reader.onload = () => { this.historyCsv = String(reader.result ?? ''); this.historyName.set(`${file.name} (${file.size} bytes)`); };
    reader.onerror = () => this.lines.set(['The history file could not be read.']);
    reader.readAsText(file);
  }

  importCsv(): void {
    const problems = importProblems({ csv: this.csv, asOf: this.asOf, sourceName: this.sourceName, terms: this.terms, confirmed: this.confirmed }, this.today);
    if (problems.length) { this.lines.set(problems); this.done.set(''); return; }
    this.run(this.api.importIndexCsv(this.index().id, { csv: this.csv, as_of: this.asOf, source_name: this.sourceName.trim(), license_terms: this.terms.trim(), license_confirmed: this.confirmed, ...(this.historyCsv.trim() ? { history_csv: this.historyCsv } : {}) }), 'List imported.');
  }

  fetchFmp(): void {
    const problems = fetchProblems({ terms: this.fmpTerms, confirmed: this.fmpConfirmed, keySaved: this.fmpKeySaved() });
    if (problems.length) { this.lines.set(problems); this.done.set(''); return; }
    this.run(this.api.fetchIndexFromFmp(this.index().id, { license_terms: this.fmpTerms.trim(), license_confirmed: this.fmpConfirmed }), 'List fetched.');
  }

  saveKey(): void {
    const key = this.apiKey.trim();
    if (!key) { this.lines.set(['Paste your Financial Modeling Prep key first.']); return; }
    this.busy.set(true);
    this.api.saveFmpKey(key).subscribe({
      next: () => { this.apiKey = ''; this.finish('Key saved. It is stored encrypted and will not be shown again.'); },
      error: e => this.fail(e),
    });
  }

  removeKey(): void {
    this.busy.set(true);
    this.api.removeFmpKey().subscribe({ next: () => this.finish('Key removed.'), error: e => this.fail(e) });
  }

  remove(): void {
    this.busy.set(true);
    this.api.removeIndexSource(this.index().id).subscribe({ next: () => this.finish('Your list was removed; the index is unavailable again.'), error: e => this.fail(e) });
  }

  private run(call: ReturnType<MarketService['importIndexCsv']>, message: string): void {
    this.busy.set(true);
    call.subscribe({ next: () => this.finish(message), error: e => this.fail(e) });
  }

  private finish(message: string): void {
    this.busy.set(false);
    this.lines.set([]);
    this.done.set(message);
    this.changed.emit(this.index().id);
  }

  private fail(err: unknown): void {
    this.busy.set(false);
    this.done.set('');
    this.lines.set(refusalLines(err));
  }
}
