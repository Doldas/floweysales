import { NgTemplateOutlet } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { ActivatedRoute, RouterLink } from '@angular/router';
import { map } from 'rxjs';

import {
  alignClass, collectInput, fileBytes, fileText, formatBytes, initialValues, isTextFile, paragraphs, tableToTsv, tabIcon,
  type FormValues, type PickedFile, type Tool, type ToolBlock, type ToolInput, type ToolResult,
} from '../components/plugin-tools';
import { PluginTools } from '../core/plugin-tools';
import { NotifyService } from '../core/notify.service';

/**
 * A tools plugin's tab: its tools as a list, the selected tool's form
 * rendered from the manifest and the blocks the plugin returns. Inputs live in
 * memory only; nothing is written to browser storage (tax data).
 */
@Component({
  selector: 'fs-plugin-tools-page',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink, NgTemplateOutlet],
  template: `
@if (plugin(); as p) {
<div class="fs-page">
  <header class="fs-page-header">
    <div>
      <div class="fs-eyebrow">{{ p.name }}</div>
      <h1><i class="bi" [class]="icon()" aria-hidden="true"></i> {{ p.tools.tab.label }}</h1>
      @if (p.tools.tab.description) { <p>{{ p.tools.tab.description }}</p> }
    </div>
  </header>

  <div class="tools-layout">
    <nav class="tool-list" aria-label="Tools">
      @for (t of p.tools.tools; track t.id) {
        <button type="button" class="tool-item" [class.active]="t.id === tool()?.id" [attr.aria-current]="t.id === tool()?.id ? 'true' : null" (click)="select(t.id)">
          <strong>{{ t.label }}</strong>
          @if (t.description) { <small>{{ t.description }}</small> }
        </button>
      }
    </nav>

    <div class="tool-main">
      @if (!p.ready) {
        <div class="alert alert-warning" role="alert">
          This plugin needs configuration on the server before it can run:
          @for (m of p.missing ?? []; track m) { <code class="ms-1">{{ m }}</code> }
        </div>
      } @else if (tool(); as t) {
        <section class="card" [attr.aria-labelledby]="'tool-' + t.id">
          <div class="card-header"><h2 class="card-title" [id]="'tool-' + t.id">{{ t.label }}</h2></div>
          <form class="card-body form-grid" (submit)="$event.preventDefault(); submit(p.plugin_id, t)" novalidate>
            @if (t.description) { <p class="form-note mb-0">{{ t.description }}</p> }
            @for (f of t.inputs ?? []; track f.name) {
              <div [class.has-error]="!!errors()[f.name]">
                @switch (f.type) {
                  @case ('checkbox') {
                    <div class="form-check">
                      <input class="form-check-input" type="checkbox" [id]="fid(f)" [checked]="values()[f.name] === true" [attr.aria-describedby]="hint(f)" (change)="set(f, $any($event.target).checked)">
                      <label class="form-check-label" [for]="fid(f)">{{ f.label }}</label>
                    </div>
                  }
                  @case ('file') { <ng-container [ngTemplateOutlet]="filePicker" [ngTemplateOutletContext]="{ f: f, multiple: false }" /> }
                  @case ('files') { <ng-container [ngTemplateOutlet]="filePicker" [ngTemplateOutletContext]="{ f: f, multiple: true }" /> }
                  @default {
                    <label class="form-label" [for]="fid(f)">{{ f.label }}@if (f.required) { <span aria-hidden="true"> *</span> }</label>
                    @switch (f.type) {
                      @case ('textarea') {
                        <textarea class="form-control" rows="4" [id]="fid(f)" [attr.placeholder]="f.placeholder || null" [attr.aria-describedby]="hint(f)" [attr.aria-invalid]="errors()[f.name] ? 'true' : null"
                                  [value]="text(f)" (input)="set(f, $any($event.target).value)"></textarea>
                      }
                      @case ('select') {
                        <select class="form-select" [id]="fid(f)" [attr.aria-describedby]="hint(f)" (change)="set(f, $any($event.target).value)">
                          @for (o of f.options ?? []; track o.value) { <option [value]="o.value" [selected]="o.value === text(f)">{{ o.label }}</option> }
                        </select>
                      }
                      @default {
                        <input class="form-control" [id]="fid(f)" [attr.type]="f.type === 'number' ? 'text' : f.type" [attr.inputmode]="f.type === 'number' ? 'decimal' : null"
                               [attr.placeholder]="f.placeholder || null" [attr.aria-describedby]="hint(f)" [attr.aria-invalid]="errors()[f.name] ? 'true' : null"
                               [value]="text(f)" (input)="set(f, $any($event.target).value)">
                      }
                    }
                  }
                }
                @if (f.help) { <div class="form-text" [id]="fid(f) + '-help'">{{ f.help }}</div> }
                @if (errors()[f.name]; as e) { <div class="field-error" [id]="fid(f) + '-err'" role="alert">{{ e }}</div> }
              </div>
            }
            <div class="form-actions">
              <button type="submit" class="btn btn-primary" [disabled]="busy()">
                @if (busy()) { <span class="spinner-border spinner-border-sm me-1" aria-hidden="true"></span> Working… } @else { {{ t.submit || 'Run' }} }
              </button>
            </div>
          </form>
        </section>

        <div aria-live="polite" class="results">
          @if (error(); as e) { <div class="alert alert-danger" role="alert">{{ e }}</div> }
          @if (result(); as r) {
            @if (r.title) { <h2 class="result-title">{{ r.title }}</h2> }
            @for (b of r.blocks; track $index) {
              @switch (b.type) {
                @case ('text') { <div class="block-text">@for (para of paras(b.text); track $index) { <p>{{ para }}</p> }</div> }
                @case ('notice') {
                  <div class="alert" [class]="'alert alert-' + noticeClass(b)" [attr.role]="b.level === 'error' ? 'alert' : 'note'">
                    @if (b.title) { <strong>{{ b.title }}</strong> }
                    @for (para of paras(b.text); track $index) { <p class="mb-1">{{ para }}</p> }
                  </div>
                }
                @case ('stats') {
                  @if (b.title) { <h3 class="block-title">{{ b.title }}</h3> }
                  <div class="tiles">
                    @for (s of b.stats ?? []; track $index) {
                      <div class="tile"><div class="tile-label">{{ s.label }}</div><div class="tile-value tabular">{{ s.value }}</div>@if (s.hint) { <small>{{ s.hint }}</small> }</div>
                    }
                  </div>
                }
                @case ('table') {
                  <section class="card">
                    <div class="card-header table-head">
                      <h3 class="card-title">{{ b.title || 'Table' }}</h3>
                      <button type="button" class="btn btn-sm btn-outline-secondary" (click)="copy(tsv(b), 'Table copied')"><i class="bi bi-clipboard" aria-hidden="true"></i> Copy as TSV</button>
                    </div>
                    <div class="table-wrap" tabindex="0" role="region" [attr.aria-label]="b.title || 'Table'">
                      <table class="table table-sm">
                        <thead><tr>@for (c of b.columns ?? []; track $index) { <th scope="col" [class]="align(c.align)">{{ c.label }}</th> }</tr></thead>
                        <tbody>@for (row of b.rows ?? []; track $index) { <tr>@for (cell of row; track $index) { <td [class]="align((b.columns ?? [])[$index]?.align)">{{ cell }}</td> }</tr> }</tbody>
                      </table>
                    </div>
                  </section>
                }
                @case ('file') {
                  @if (b.file; as f) {
                    <section class="card file-card">
                      <div class="card-body">
                        <i class="bi bi-file-earmark-arrow-down file-icon" aria-hidden="true"></i>
                        <div class="file-meta">
                          <strong>{{ f.name }}</strong>
                          <small>{{ size(b) }} · {{ f.mime }}</small>
                          @if (f.description) { <span>{{ f.description }}</span> }
                        </div>
                        <div class="file-actions">
                          <button type="button" class="btn btn-primary btn-sm" (click)="download(b)"><i class="bi bi-download" aria-hidden="true"></i> Download</button>
                          @if (textFile(b)) { <button type="button" class="btn btn-outline-secondary btn-sm" (click)="copyFile(b)"><i class="bi bi-clipboard" aria-hidden="true"></i> Copy</button> }
                        </div>
                      </div>
                    </section>
                  }
                }
              }
            }
          }
        </div>
      }
    </div>
  </div>
</div>

<ng-template #filePicker let-f="f" let-multiple="multiple">
  <label class="form-label" [for]="fid(f)">{{ f.label }}@if (f.required) { <span aria-hidden="true"> *</span> }</label>
  <input class="form-control" type="file" [id]="fid(f)" [attr.accept]="f.accept || null" [attr.multiple]="multiple ? '' : null" [attr.aria-describedby]="hint(f)" (change)="pick(f, $any($event.target))">
  @if (picked(f).length) {
    <ul class="picked">
      @for (file of picked(f); track $index) {
        <li><i class="bi bi-file-earmark-text" aria-hidden="true"></i> <span class="picked-name">{{ file.name }}</span> <small>{{ fmt(file.size) }}</small>
          <button type="button" class="btn btn-sm btn-link" (click)="remove(f, $index)" [attr.aria-label]="'Remove ' + file.name"><i class="bi bi-x-lg" aria-hidden="true"></i></button></li>
      }
    </ul>
  }
</ng-template>
} @else if (store.loaded()) {
  <div class="fs-page"><div class="fs-empty"><i class="bi bi-tools"></i><h3>Tool plugin not found</h3>
    <p class="mb-0">It may have been uninstalled. <a routerLink="/">Back to Command Center</a></p></div></div>
}
  `,
  styles: [`
:host { display: block; }
.tools-layout { display: grid; grid-template-columns: minmax(12rem, 16rem) minmax(0, 1fr); gap: 1rem; align-items: start; }
@media (max-width: 991.98px) { .tools-layout { grid-template-columns: minmax(0, 1fr); } .tool-list { flex-direction: row; overflow-x: auto; } .tool-item { min-width: 11rem; } }
.tool-list { display: flex; flex-direction: column; gap: 0.35rem; }
.tool-item { text-align: left; border: 1px solid var(--fs-border); background: var(--fs-surface, transparent); color: var(--fs-text); border-radius: 0.6rem; padding: 0.6rem 0.75rem; display: grid; gap: 0.15rem; cursor: pointer; }
.tool-item small { color: var(--fs-muted); font-size: 0.75rem; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
.tool-item.active { border-color: var(--fs-accent); box-shadow: inset 3px 0 0 var(--fs-accent); }
.tool-item:focus-visible { outline: 2px solid var(--fs-accent); outline-offset: 2px; }
.tool-main { display: grid; gap: 1rem; min-width: 0; }
.form-grid { display: grid; gap: 0.9rem; }
.form-note { color: var(--fs-text-2); font-size: 0.85rem; }
.field-error { color: var(--fs-neg, #c0392b); font-size: 0.8rem; margin-top: 0.25rem; }
.has-error .form-control, .has-error .form-select { border-color: var(--fs-neg, #c0392b); }
.picked { list-style: none; margin: 0.4rem 0 0; padding: 0; display: grid; gap: 0.2rem; }
.picked li { display: flex; align-items: center; gap: 0.4rem; font-size: 0.85rem; }
.picked-name { overflow-wrap: anywhere; }
.results { display: grid; gap: 1rem; min-width: 0; }
.result-title { font-size: 1.25rem; margin: 0.25rem 0 0; }
.block-title { font-size: 1rem; margin: 0; }
.block-text p { margin: 0 0 0.6rem; white-space: pre-line; max-width: 75ch; }
.tiles { display: grid; grid-template-columns: repeat(auto-fit, minmax(11rem, 1fr)); gap: 0.75rem; }
.tile { border: 1px solid var(--fs-border); border-radius: 0.6rem; padding: 0.7rem 0.85rem; display: grid; gap: 0.15rem; }
.tile-label { font-size: 0.75rem; color: var(--fs-muted); text-transform: uppercase; letter-spacing: 0.04em; }
.tile-value { font-size: 1.3rem; font-weight: 650; }
.tile small { color: var(--fs-muted); }
.table-head { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 0.5rem; }
.table-wrap { overflow-x: auto; max-width: 100%; }
.table-wrap:focus-visible { outline: 2px solid var(--fs-accent); outline-offset: -2px; }
.table-wrap th, .table-wrap td { white-space: nowrap; }
.file-card .card-body { display: flex; flex-wrap: wrap; align-items: center; gap: 0.9rem; }
.file-icon { font-size: 1.8rem; color: var(--fs-accent); }
.file-meta { display: grid; gap: 0.1rem; flex: 1 1 14rem; min-width: 0; overflow-wrap: anywhere; }
.file-meta small { color: var(--fs-muted); }
.file-actions { display: flex; gap: 0.5rem; }
  `],
})
export class PluginToolsPage {
  readonly store = inject(PluginTools);
  private readonly notify = inject(NotifyService);
  private readonly pluginId = toSignal(inject(ActivatedRoute).paramMap.pipe(map(p => p.get('plugin') ?? '')), { initialValue: '' });

  readonly plugin = computed(() => this.store.plugins().find(p => p.plugin_id === this.pluginId()) ?? null);
  readonly icon = computed(() => { const p = this.plugin(); return p ? tabIcon(p.tools.tab) : ''; });
  private readonly selected = signal<Record<string, string>>({});
  readonly tool = computed<Tool | null>(() => {
    const p = this.plugin();
    if (!p) return null;
    return p.tools.tools.find(t => t.id === this.selected()[p.plugin_id]) ?? p.tools.tools[0] ?? null;
  });

  /** Form values per plugin+tool, kept while switching tools; in memory only. */
  private readonly all = signal<Record<string, FormValues>>({});
  private readonly results = signal<Record<string, ToolResult | undefined>>({});
  private readonly errorsByTool = signal<Record<string, Record<string, string>>>({});
  private readonly failure = signal<Record<string, string>>({});
  readonly busy = signal(false);

  private readonly key = computed(() => `${this.pluginId()}/${this.tool()?.id ?? ''}`);
  readonly values = computed<FormValues>(() => {
    const t = this.tool();
    return (t && this.all()[this.key()]) || (t ? initialValues(t) : {});
  });
  readonly errors = computed(() => this.errorsByTool()[this.key()] ?? {});
  readonly result = computed(() => this.results()[this.key()] ?? null);
  readonly error = computed(() => this.failure()[this.key()] ?? '');

  select(id: string): void {
    this.selected.update(s => ({ ...s, [this.pluginId()]: id }));
  }

  fid = (f: ToolInput) => `pt-${this.pluginId()}-${this.tool()?.id}-${f.name}`;
  hint = (f: ToolInput): string | null => [f.help ? this.fid(f) + '-help' : '', this.errors()[f.name] ? this.fid(f) + '-err' : ''].filter(Boolean).join(' ') || null;
  text = (f: ToolInput): string => { const v = this.values()[f.name]; return typeof v === 'string' ? v : ''; };
  picked = (f: ToolInput): PickedFile[] => { const v = this.values()[f.name]; return Array.isArray(v) ? v : []; };
  fmt = formatBytes;
  paras = (t?: string) => paragraphs(t ?? '');
  align = alignClass;
  tsv = tableToTsv;
  noticeClass = (b: ToolBlock) => ({ info: 'info', success: 'success', warning: 'warning', error: 'danger' })[b.level ?? 'info'];

  private patch(values: FormValues): void {
    this.all.update(a => ({ ...a, [this.key()]: values }));
  }

  set(f: ToolInput, value: string | boolean): void {
    this.patch({ ...this.values(), [f.name]: value });
    this.errorsByTool.update(e => ({ ...e, [this.key()]: { ...(e[this.key()] ?? {}), [f.name]: '' } }));
  }

  async pick(f: ToolInput, el: HTMLInputElement): Promise<void> {
    const chosen = Array.from(el.files ?? []);
    el.value = '';
    try {
      const read = await Promise.all(chosen.map(async file => ({ name: file.name, content: await file.text(), size: file.size })));
      const current = f.type === 'files' ? this.picked(f) : [];
      this.patch({ ...this.values(), [f.name]: [...current, ...read] });
      this.errorsByTool.update(e => ({ ...e, [this.key()]: { ...(e[this.key()] ?? {}), [f.name]: '' } }));
    } catch {
      this.errorsByTool.update(e => ({ ...e, [this.key()]: { ...(e[this.key()] ?? {}), [f.name]: 'The file could not be read.' } }));
    }
  }

  remove(f: ToolInput, index: number): void {
    this.patch({ ...this.values(), [f.name]: this.picked(f).filter((_, i) => i !== index) });
  }

  async submit(plugin: string, tool: Tool): Promise<void> {
    const key = this.key();
    const { input, errors } = collectInput(tool, this.values());
    this.errorsByTool.update(e => ({ ...e, [key]: errors }));
    this.failure.update(f => ({ ...f, [key]: '' }));
    if (Object.keys(errors).length) return;
    this.busy.set(true);
    try {
      const result = await this.store.run(plugin, tool.id, input);
      this.results.update(r => ({ ...r, [key]: result }));
    } catch (e) {
      this.results.update(r => ({ ...r, [key]: undefined }));
      this.failure.update(f => ({ ...f, [key]: e instanceof Error ? e.message : 'The tool failed.' }));
    } finally {
      this.busy.set(false);
    }
  }

  size = (b: ToolBlock) => formatBytes(b.file ? fileBytes(b.file).length : 0);
  textFile = (b: ToolBlock) => !!b.file && isTextFile(b.file);

  download(b: ToolBlock): void {
    const file = b.file;
    if (!file) return;
    const url = URL.createObjectURL(new Blob([fileBytes(file) as BlobPart], { type: file.mime }));
    const a = document.createElement('a');
    a.href = url;
    a.download = file.name;
    document.body.appendChild(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(url), 0);
  }

  copyFile(b: ToolBlock): void {
    if (b.file) void this.copy(fileText(b.file), `${b.file.name} copied`);
  }

  async copy(text: string, message: string): Promise<void> {
    try {
      await navigator.clipboard.writeText(text);
      this.notify.show(message, 'success');
    } catch {
      this.notify.show('Copying is not available here.', 'error');
    }
  }
}
