import { ChangeDetectionStrategy, Component, computed, input, signal } from '@angular/core';

import { toolLabel } from './agent-tools';
import { parseDocumentEvidence, visionPageLabel } from './document-evidence';
import { argsSummary, displayNotes, durationText, findLines, formatBytes, prettyText, type ToolCall } from './tool-call-view';

/**
 * One tool call of an agent: its name, the arguments it was given, what came
 * back, how long it took and whether anything shown is cut or masked. Used
 * in the research agent chat and in agent teams so both can be inspected the
 * same way.
 */
@Component({
  selector: 'fs-tool-call',
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { '[class.is-error]': "call().status === 'error'", '[class.is-running]': "call().status === 'running'" },
  template: `
<details>
  <summary>
    <span class="step-icon" aria-hidden="true">
      @if (call().status === 'running') { <span class="spinner-border spinner-border-sm"></span> }
      @else if (call().status === 'error') { <i class="bi bi-x-circle-fill"></i> }
      @else { <i class="bi bi-check-circle-fill"></i> }
    </span>
    <span class="step-name">{{ label() }}</span>
    @if (summary()) { <span class="step-args">{{ summary() }}</span> }
    @if (call().actor) { <span class="step-actor">{{ call().actor }}</span> }
    <span class="step-time">{{ call().status === 'running' ? 'running…' : time() }}</span>
  </summary>
  <div class="step-body">
    <div class="step-meta">
      <code>{{ call().name }}</code>
      @if (call().seq) { <span>call #{{ call().seq }}</span> }
      <span>{{ statusText() }}</span>
      <span class="spacer"></span>
      <label class="raw"><input type="checkbox" [checked]="raw()" (change)="raw.set($any($event.target).checked)" /> Raw text</label>
    </div>
    @for (note of notes(); track note) { <p class="step-note"><i class="bi bi-info-circle" aria-hidden="true"></i> {{ note }}</p> }

    @if (evidence(); as ev) {
      <section class="evidence" aria-label="Document evidence">
        <h5>Document evidence</h5>
        <p class="step-note">{{ ev.summary }}</p>
        <p class="step-note">
          <a [href]="ev.url" target="_blank" rel="noopener noreferrer">{{ ev.url }}</a>
          @if (ev.resolvedFrom) { (opened from the viewer page {{ ev.resolvedFrom }}) }
          &middot; {{ ev.pagesExamined }} of {{ ev.pagesTotal }} pages examined
        </p>
        <ul class="ev-pages">
          @for (p of ev.pages; track p.page) {
            <li><strong>Page {{ p.page }}</strong> {{ p.label }}@if (vlabel(p.vision)) { , {{ vlabel(p.vision) }} }@if (p.note) { &mdash; {{ p.note }} }</li>
          }
        </ul>
        @if (ev.observations.length) {
          <div class="ev-table" role="region" aria-label="Figures read by the vision model" tabindex="0">
            <table>
              <thead><tr><th>Page</th><th>Item</th><th>Value as printed</th><th>Period</th><th>Checks</th></tr></thead>
              <tbody>
                @for (o of ev.observations; track $index) {
                  <tr [class.unverified]="!o.verified">
                    <td>{{ o.page }}</td><td>{{ o.label }}</td><td>{{ o.original }} {{ o.unit }}</td><td>{{ o.period || 'not stated' }}</td>
                    <td>{{ o.verified ? 'Passed' : 'Not verified' }}@if (o.flagText.length) { : {{ o.flagText.join('; ') }} }</td>
                  </tr>
                }
              </tbody>
            </table>
          </div>
        }
        @for (l of ev.limitations; track l) { <p class="step-note"><i class="bi bi-exclamation-triangle" aria-hidden="true"></i> {{ l }}</p> }
      </section>
    }

    @if (call().args) {
      <section>
        <h5>Input</h5>
        <pre class="step-pre">{{ raw() ? call().args : pretty(call().args) }}</pre>
      </section>
    }
    @if (call().status !== 'running') {
      <section>
        <div class="out-head">
          <h5>{{ call().status === 'error' ? 'Error' : 'Output' }}</h5>
          <input type="search" class="find" placeholder="Find in output" [value]="query()" (input)="query.set($any($event.target).value)" [attr.aria-label]="'Find in the output of ' + label()" />
          <button type="button" class="btn btn-sm btn-ghost" (click)="copy()">{{ copied() ? 'Copied' : 'Copy' }}</button>
          <button type="button" class="btn btn-sm btn-ghost" (click)="download()">Download</button>
        </div>
        @if (call().error) { <pre class="step-pre err">{{ call().error }}</pre> }
        @if (outputText()) {
          <pre class="step-pre" [class.is-clamped]="clamped()">{{ shown() }}</pre>
          @if (query() && found().matches === 0) { <p class="step-note">Nothing in the output matches &ldquo;{{ query() }}&rdquo;.</p> }
          @else if (query()) { <p class="step-note">{{ found().matches }} matching line{{ found().matches === 1 ? '' : 's' }}.</p> }
          @if (long() && !query()) {
            <button type="button" class="btn btn-sm btn-link" (click)="expanded.set(!expanded())" [attr.aria-expanded]="expanded()">
              {{ expanded() ? 'Show less' : 'Show all ' + length() + ' characters' }}
            </button>
          }
        } @else if (!call().error) { <p class="step-note">No output.</p> }
      </section>
    }
  </div>
</details>
  `,
  styles: [`
:host { display: block; min-width: 0; }
summary { display: flex; align-items: center; gap: 0.5rem; padding: 0.3rem 0.35rem; border-radius: var(--bs-border-radius-sm); cursor: pointer; list-style: none; font-size: 0.82rem; min-width: 0; }
summary::-webkit-details-marker { display: none; }
summary:hover { background: var(--fs-surface-3); }
.step-icon { flex: 0 0 auto; color: var(--fs-pos); display: inline-flex; }
.step-icon .spinner-border { width: 0.85rem; height: 0.85rem; border-width: 2px; color: var(--fs-accent); }
:host(.is-error) .step-icon { color: var(--fs-neg); }
.step-name { font-weight: 600; white-space: nowrap; }
.step-args { color: var(--fs-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; flex: 1 1 auto; font-size: 0.78rem; }
.step-actor { color: var(--fs-text-2); font-size: 0.72rem; white-space: nowrap; border: 1px solid var(--fs-border); border-radius: 999px; padding: 0 0.4rem; }
.step-time { margin-left: auto; color: var(--fs-muted); font-size: 0.72rem; white-space: nowrap; font-variant-numeric: tabular-nums; }
.step-body { padding: 0.25rem 0.35rem 0.5rem 1.7rem; display: grid; grid-template-columns: minmax(0, 1fr); gap: 0.4rem; min-width: 0; }
.step-body section { min-width: 0; max-width: 100%; }
.step-meta { display: flex; flex-wrap: wrap; align-items: center; gap: 0.6rem; font-size: 0.72rem; color: var(--fs-muted); }
.step-meta code { font-size: 0.72rem; }
.spacer { flex: 1 1 auto; }
.raw { display: inline-flex; align-items: center; gap: 0.3rem; cursor: pointer; }
h5 { font-size: 0.7rem; text-transform: uppercase; letter-spacing: 0.05em; color: var(--fs-muted); margin: 0 0 0.2rem; }
.out-head { display: flex; flex-wrap: wrap; align-items: center; gap: 0.4rem; margin-bottom: 0.2rem; h5 { margin: 0; margin-right: auto; } }
.find { font-size: 0.74rem; padding: 0.1rem 0.4rem; border: 1px solid var(--fs-border-strong); border-radius: var(--bs-border-radius-sm); background: var(--fs-surface); color: var(--fs-text); min-width: 0; width: 9rem; }
.step-note { margin: 0; font-size: 0.74rem; color: var(--fs-text-2); }
.step-pre { margin: 0; min-width: 0; max-width: 100%; max-height: 220px; overflow: auto; font-size: 0.74rem; background: var(--fs-sunken); border: 1px solid var(--fs-border); border-radius: var(--bs-border-radius); padding: 0.5rem 0.6rem; white-space: pre-wrap; overflow-wrap: anywhere; }
.step-pre:not(.is-clamped) { max-height: 60vh; }
.step-pre.err { color: var(--fs-neg); }
.ev-pages { margin: 0; padding-left: 1.1rem; font-size: 0.76rem; }
.ev-table { overflow-x: auto; max-width: 100%; }
.ev-table table { border-collapse: collapse; font-size: 0.74rem; min-width: 30rem; }
.ev-table th, .ev-table td { border-bottom: 1px solid var(--fs-border); padding: 0.2rem 0.4rem; text-align: left; vertical-align: top; }
.ev-table tr.unverified td { color: var(--fs-text-2); }
  `],
})
export class ToolCallComponent {
  readonly call = input.required<ToolCall>();

  readonly raw = signal(false);
  readonly query = signal('');
  readonly expanded = signal(false);
  readonly copied = signal(false);

  private static readonly CLAMP = 4000;

  readonly label = computed(() => toolLabel(this.call().name || 'tool'));
  readonly summary = computed(() => argsSummary(this.call().args));
  readonly time = computed(() => durationText(this.call().durationMs));
  readonly notes = computed(() => displayNotes(this.call()));
  readonly statusText = computed(() => ({ running: 'running', ok: 'finished', error: 'failed' }[this.call().status]));
  readonly outputText = computed(() => (this.raw() ? this.call().result ?? '' : prettyText(this.call().result)));
  readonly length = computed(() => this.outputText().length);
  readonly long = computed(() => this.length() > ToolCallComponent.CLAMP);
  readonly found = computed(() => findLines(this.outputText(), this.query()));
  readonly clamped = computed(() => this.long() && !this.expanded() && !this.query());
  readonly shown = computed(() => {
    if (this.query()) return this.found().text;
    const text = this.outputText();
    return this.clamped() ? text.slice(0, ToolCallComponent.CLAMP) + '\n…' : text;
  });

  readonly evidence = computed(() => (this.call().name === 'read_source' ? parseDocumentEvidence(this.call().result) : null));
  vlabel(v: string): string { return visionPageLabel(v); }
  pretty(text: string | undefined): string { return prettyText(text); }
  size(n: number | undefined): string { return formatBytes(n); }

  copy(): void {
    const text = this.outputText() || this.call().error || '';
    const done = () => { this.copied.set(true); setTimeout(() => this.copied.set(false), 1500); };
    if (navigator.clipboard?.writeText) navigator.clipboard.writeText(text).then(done).catch(() => undefined);
  }

  /** Saves exactly what is shown, which may be a cut of a longer result (the notes above say so). */
  download(): void {
    const text = this.outputText() || this.call().error || '';
    const url = URL.createObjectURL(new Blob([text], { type: 'text/plain' }));
    const link = document.createElement('a');
    link.href = url;
    link.download = `${this.call().name}-${this.call().seq ?? this.call().callId}.txt`;
    link.click();
    URL.revokeObjectURL(url);
  }
}
