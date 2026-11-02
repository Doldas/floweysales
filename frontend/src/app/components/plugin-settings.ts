import { Component, ElementRef, computed, inject, signal, viewChild } from '@angular/core';
import { MarketState } from '../core/market-state';
import { PluginTools } from '../core/plugin-tools';
import {
  capabilityLabels, matchesFilter, pluginRows, pluginStatus, uninstallPrompt,
  type PluginOverview, type PluginRow,
} from './plugin-store';

type View = 'all' | 'installed' | 'available';

@Component({
 selector: 'fse-plugin-settings', host: { class: 'fs-native-forms' },
 standalone: true,
 template: `
 @if (overview(); as o) {
 <div class="card">
  <div class="store-head">
   <div>
    <h3>Plugin store</h3>
    <p class="muted">Market-data providers are plugins: separate programs this server downloads, verifies by SHA-256 and runs on demand. Only install plugins you trust.</p>
   </div>
   <div class="actions">
    @if (updatable().length) {
     <button class="btn btn-sm btn-primary" (click)="updateAll()" [disabled]="busy()">Update all ({{ updatable().length }})</button>
    }
    <button class="btn btn-sm btn-outline-secondary" (click)="check()" [disabled]="busy()">Check for updates</button>
   </div>
  </div>
  <dl>
   <dt>Store</dt><dd><code>{{ o.store_url }}</code></dd>
   <dt>Platform</dt><dd><code>{{ o.platform }}</code> · plugin protocol {{ o.protocol }}</dd>
   <dt>Last checked</dt><dd>{{ o.store.checked_at ? when(o.store.checked_at) : 'Not yet' }}</dd>
  </dl>
  @if (o.store.error) { <p class="warning" role="alert">The store could not be reached: {{ o.store.error }}</p> }
  @for (w of o.store.warnings ?? []; track w) { <p class="muted small">Skipped store entry: {{ w }}</p> }
  @if (!o.installed.length) {
   <p class="result">No provider plugins are installed, so market data, trackers and search are unavailable. Install at least one below.</p>
  }
 </div>

 <div class="toolbar">
  <input type="search" placeholder="Filter plugins" aria-label="Filter plugins" [value]="filter()" (input)="filter.set($any($event.target).value)">
  <div class="btn-group" role="group" aria-label="Show">
   @for (v of views; track v.id) {
    <button type="button" class="btn btn-sm" [class.btn-secondary]="view() === v.id" [class.btn-outline-secondary]="view() !== v.id"
            [attr.aria-pressed]="view() === v.id" (click)="view.set(v.id)">{{ v.label }}</button>
   }
  </div>
 </div>

 <div class="cards">
  @for (row of visible(); track row.id) {
   <article class="card plugin" [attr.aria-labelledby]="'plugin-' + row.id">
    <header>
     <div>
      <h3 [id]="'plugin-' + row.id">{{ row.manifest.name }}</h3>
      <p class="muted small">{{ row.manifest.publisher }} · provider <code>{{ row.manifest.provider.id }}</code>
       · @if (row.installed) { v{{ row.installed.manifest.version }} } @else { v{{ row.store?.latest_version || row.manifest.version }} }</p>
     </div>
     <div class="badges">
      <span class="badge" [class.text-bg-success]="row.manifest.provider.official_api" [class.text-bg-secondary]="!row.manifest.provider.official_api">
       {{ row.manifest.provider.official_api ? 'Official API' : 'Unofficial' }}</span>
      @if (row.manifest.provider.experimental) { <span class="badge text-bg-warning">Experimental</span> }
     </div>
    </header>
    <p>{{ row.manifest.description }}</p>
    <ul class="chips" aria-label="Capabilities">
     @for (c of capabilityLabels(row.manifest.provider.capabilities); track c) { <li>{{ c }}</li> }
    </ul>
    <p class="status" [attr.data-tone]="status(row).tone" role="status">{{ status(row).label }}</p>

    @if (row.installed; as i) {
     @if (i.env.length) {
      <ul class="env">
       @for (e of i.env; track e.name) {
        <li><i class="bi" [class.bi-check-circle]="e.configured" [class.bi-exclamation-circle]="!e.configured" aria-hidden="true"></i>
         <code>{{ e.name }}</code> {{ e.configured ? 'configured' : (e.required ? 'required, not set' : 'optional, not set') }}
         <small>{{ e.description }}</small></li>
       }
      </ul>
      @if (!i.ready) { <p class="muted small">Set the variables in the server environment or config.properties, then restart the backend.</p> }
     }
     @if (row.trackers.length) { <p class="small">Used by trackers: {{ row.trackers.join(', ') }}</p> }
     @if (i.last_error) { <p class="warning small">{{ i.last_error }}</p> }
    }

    <div class="actions">
     @if (row.installed) {
      @if (row.installed.update_available && row.installed.latest_version) {
       <button class="btn btn-sm btn-primary" (click)="install(row, row.installed.latest_version)" [disabled]="busy()">Update to {{ row.installed.latest_version }}</button>
      }
      <button class="btn btn-sm btn-outline-danger" (click)="uninstall(row)" [disabled]="busy()">Uninstall</button>
     } @else if (row.store?.installable) {
      <button class="btn btn-sm btn-primary" (click)="install(row, row.store?.latest_version)" [disabled]="busy()">Install</button>
     }
     @if (working() === row.id) { <span class="spinner-border spinner-border-sm" role="status" aria-label="Working"></span> }
    </div>

    <details>
     <summary>Details</summary>
     @if (row.manifest.provider.notes) { <p class="small">{{ row.manifest.provider.notes }}</p> }
     @if (row.manifest.terms) { <p class="small"><strong>Terms:</strong> {{ row.manifest.terms }}</p> }
     <dl class="small">
      @if (row.manifest.homepage) { <dt>Homepage</dt><dd><a [href]="row.manifest.homepage" target="_blank" rel="noopener">{{ row.manifest.homepage }}</a></dd> }
      @if (row.manifest.license) { <dt>License</dt><dd>{{ row.manifest.license }}</dd> }
      <dt>Polling</dt><dd>at most every {{ row.manifest.provider.min_poll_seconds }} s</dd>
      @if (row.installed; as i) {
       <dt>Installed</dt><dd>{{ when(i.installed_at) }} from {{ i.source === 'file' ? 'a local file' : 'the store' }}</dd>
       <dt>SHA-256</dt><dd><code>{{ i.sha256 }}</code></dd>
      }
      @if (row.store?.released_at) { <dt>Released</dt><dd>{{ when(row.store!.released_at!) }}</dd> }
     </dl>
     @if (row.store?.notes) { <pre>{{ row.store!.notes }}</pre> }
     @if ((row.store?.versions?.length ?? 0) > 1 && row.store?.installable) {
      <div class="actions">
       <label class="small">Version
        <select #ver [attr.aria-label]="'Version of ' + row.manifest.name">
         @for (v of row.store!.versions; track v) { <option [value]="v" [selected]="v === (row.installed?.manifest?.version ?? row.store!.latest_version)">{{ v }}</option> }
        </select>
       </label>
       <button class="btn btn-sm btn-outline-secondary" (click)="install(row, ver.value)" [disabled]="busy() || ver.value === row.installed?.manifest?.version">Install this version</button>
      </div>
     }
     @if (row.installed?.log?.length) {
      <details><summary>Plugin log</summary><pre>{{ row.installed!.log.join('\\n') }}</pre></details>
     }
    </details>
   </article>
  } @empty {
   <p class="muted">{{ o.store.plugins.length || o.installed.length ? 'No plugins match.' : 'The store lists no plugins yet. Use “Check for updates”.' }}</p>
  }
 </div>
 } @else {
  @if (error()) {
   <div class="card">
    <p class="warning" role="alert">{{ error() }}</p>
    <p>Plugins can always be managed on the server:</p>
    <pre>floweysales_backend plugins store
floweysales_backend plugins install yahoo --accept-terms
floweysales_backend plugins update</pre>
   </div>
  } @else { <p class="muted">Loading plugins…</p> }
 }
 @if (message()) { <p role="status" class="result">{{ message() }}</p> }

 <dialog #termsDialog aria-labelledby="plugin-terms-title" (close)="termsClosed()">
  <h3 id="plugin-terms-title">Terms for {{ pending()?.row?.manifest?.name }}</h3>
  <p>{{ pending()?.terms }}</p>
  <label class="accept"><input type="checkbox" [checked]="accepted()" (change)="accepted.set($any($event.target).checked)"> I have read and accept these terms</label>
  <div class="actions">
   <button class="btn btn-sm btn-primary" [disabled]="!accepted()" (click)="acceptTerms()">Accept and install</button>
   <button class="btn btn-sm btn-outline-secondary" (click)="termsDialog.close()">Cancel</button>
  </div>
 </dialog>
 `,
 styles: [`
 :host{display:flex;flex-direction:column;gap:18px}
 .cards{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,340px),1fr));gap:18px}
 .card{display:flex;flex-direction:column;gap:10px;padding:22px;background:var(--fs-surface);min-width:0}
 h3{margin:0;font-size:1.05rem}
 p{margin:0}
 .store-head,.plugin header{display:flex;flex-wrap:wrap;justify-content:space-between;gap:12px;align-items:flex-start}
 dl{display:grid;grid-template-columns:max-content 1fr;gap:6px 14px;margin:0}dt{color:var(--fs-muted)}dd{margin:0;overflow-wrap:anywhere}
 code{font-size:13px;overflow-wrap:anywhere}
 pre{white-space:pre-wrap;overflow-wrap:anywhere;background:var(--fs-surface);border:1px solid var(--fs-border);border-radius:7px;padding:10px;margin:6px 0 0;font-size:12px;max-height:260px;overflow:auto}
 .actions{display:flex;flex-wrap:wrap;align-items:center;gap:10px}
 .toolbar{display:flex;flex-wrap:wrap;gap:12px;align-items:center;justify-content:space-between}
 .toolbar input{flex:1 1 220px;max-width:360px;border:1px solid var(--fs-border-strong);background:var(--fs-surface);color:var(--fs-text);padding:8px 10px;border-radius:7px;font:inherit}
 .badges{display:flex;gap:6px;flex-wrap:wrap}
 .chips{display:flex;flex-wrap:wrap;gap:6px;list-style:none;margin:0;padding:0}
 .chips li{font-size:12px;padding:2px 8px;border-radius:999px;background:var(--fs-surface-3);color:var(--fs-text)}
 .status{padding:8px 10px;border-left:4px solid var(--fs-border-strong);background:var(--fs-surface-3)}
 .status[data-tone=ok]{border-color:color-mix(in srgb, var(--fs-pos) 50%, transparent);background:var(--fs-pos-soft)}
 .status[data-tone=update]{border-color:var(--fs-accent)}
 .status[data-tone=warn]{border-color:color-mix(in srgb, var(--fs-warn) 55%, transparent);background:var(--fs-warn-soft)}
 .env{list-style:none;margin:0;padding:0;display:flex;flex-direction:column;gap:6px;font-size:14px}
 .env i{margin-right:6px}
 .env small{display:block;color:var(--fs-muted);margin-left:22px}
 .result{padding:10px 12px;border-left:4px solid var(--fs-accent);background:var(--fs-surface-3)}
 .warning{padding:12px;border-left:4px solid color-mix(in srgb, var(--fs-warn) 55%, transparent);background:var(--fs-warn-soft);color:var(--fs-text)}
 .muted{color:var(--fs-muted)}
 .small{font-size:13px}
 details summary{cursor:pointer;color:var(--fs-muted)}
 select{border:1px solid var(--fs-border-strong);background:var(--fs-surface);color:var(--fs-text);padding:4px 8px;border-radius:6px;margin-left:6px}
 dialog{max-width:min(560px,calc(100vw - 32px));border:1px solid var(--fs-border-strong);border-radius:10px;background:var(--fs-surface);color:var(--fs-text);padding:22px}
 dialog::backdrop{background:rgb(0 0 0 / .45)}
 dialog p{margin:12px 0;white-space:pre-line}
 .accept{display:flex;gap:8px;align-items:center;margin-bottom:14px}
 a{color:var(--fs-accent-text)}
 `],
})
export class PluginSettingsComponent {
 private readonly market = inject(MarketState);
 private readonly tools = inject(PluginTools);
 private readonly termsDialog = viewChild<ElementRef<HTMLDialogElement>>('termsDialog');

 readonly overview = signal<PluginOverview | null>(null);
 readonly busy = signal(false);
 readonly working = signal('');
 readonly message = signal('');
 readonly error = signal('');
 readonly filter = signal('');
 readonly view = signal<View>('all');
 readonly pending = signal<{ row: PluginRow; version?: string; terms: string } | null>(null);
 readonly accepted = signal(false);
 readonly views: { id: View; label: string }[] = [
  { id: 'all', label: 'All' }, { id: 'installed', label: 'Installed' }, { id: 'available', label: 'Available' },
 ];
 readonly capabilityLabels = capabilityLabels;
 readonly status = pluginStatus;

 readonly rows = computed(() => { const o = this.overview(); return o ? pluginRows(o) : []; });
 readonly visible = computed(() => this.rows().filter(r =>
  matchesFilter(r, this.filter()) && (this.view() === 'all' || (this.view() === 'installed') === !!r.installed)));
 readonly updatable = computed(() => this.rows().filter(r => r.installed?.update_available && r.installed.latest_version));

 constructor() { void this.load(); }

 when(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
 }

 private async api<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, { credentials: 'same-origin', cache: 'no-store', ...init, headers: { 'Content-Type': 'application/json', ...init?.headers } });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw Object.assign(new Error(data.error || 'HTTP ' + res.status), { status: res.status, body: data });
  return data as T;
 }

 async load(): Promise<void> {
  try {
   const o = await this.api<PluginOverview>('/api/admin/plugins');
   this.overview.set(o);
   this.error.set('');
   // First visit: fetch the catalogue so the store is not empty.
   if (!o.store.checked_at) await this.check(true);
  } catch (e) { this.error.set(String((e as Error).message ?? e)); }
 }

 async check(quiet = false): Promise<void> {
  this.busy.set(true);
  if (!quiet) this.message.set('');
  try {
   const o = await this.api<PluginOverview>('/api/admin/plugins/check', { method: 'POST' });
   this.overview.set(o);
   if (!quiet && !o.store.error) {
    const n = o.installed.filter(p => p.update_available).length;
    this.message.set(n ? `${n} update${n === 1 ? '' : 's'} available.` : 'All installed plugins are up to date.');
   }
  } catch (e) { this.message.set(String((e as Error).message ?? e)); }
  finally { this.busy.set(false); }
 }

 async install(row: PluginRow, version?: string, acceptTerms = false): Promise<void> {
  this.busy.set(true); this.working.set(row.id); this.message.set('');
  try {
   const res = await this.api<{ installed: { manifest: { name: string; version: string } }; overview: PluginOverview }>(
    '/api/admin/plugins/install', { method: 'POST', body: JSON.stringify({ id: row.id, version: version ?? '', accept_terms: acceptTerms }) });
   this.overview.set(res.overview);
   this.message.set(`${res.installed.manifest.name} ${res.installed.manifest.version} is installed.`);
   this.market.refreshProviders();
   void this.tools.refresh();
  } catch (e) {
   const err = e as Error & { status?: number; body?: { terms?: string } };
   if (err.status === 409 && err.body?.terms) {
    this.pending.set({ row, version, terms: err.body.terms });
    this.accepted.set(false);
    this.termsDialog()?.nativeElement.showModal();
   } else {
    this.message.set(err.message);
   }
  } finally { this.busy.set(false); this.working.set(''); }
 }

 async acceptTerms(): Promise<void> {
  const p = this.pending();
  this.termsDialog()?.nativeElement.close();
  if (p && this.accepted()) await this.install(p.row, p.version, true);
 }

 termsClosed(): void { this.pending.set(null); }

 async updateAll(): Promise<void> {
  for (const row of this.updatable()) await this.install(row, row.installed?.latest_version);
 }

 async uninstall(row: PluginRow): Promise<void> {
  if (!confirm(uninstallPrompt(row))) return;
  this.busy.set(true); this.working.set(row.id); this.message.set('');
  try {
   const force = row.trackers.length ? '?force=true' : '';
   this.overview.set(await this.api<PluginOverview>('/api/admin/plugins/' + encodeURIComponent(row.id) + force, { method: 'DELETE' }));
   this.message.set(`${row.manifest.name} was uninstalled.`);
   this.market.refreshProviders();
   void this.tools.refresh();
  } catch (e) { this.message.set(String((e as Error).message ?? e)); }
  finally { this.busy.set(false); this.working.set(''); }
 }
}
