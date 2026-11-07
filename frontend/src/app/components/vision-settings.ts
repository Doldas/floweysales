import { Component, computed, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { probeAdvice, statusText, utcTime, visionRequestBody, type VisionSettings } from './vision-settings-view';

/**
 * SYS_ADMIN card for the vision (multimodal) model. It is separate from the
 * chat model: its own endpoint, model, key and limits. The key is write-only.
 */
@Component({
  selector: 'fse-vision-settings',
  standalone: true,
  imports: [FormsModule],
  template: `
<div class="card vision">
  <h3>Vision model</h3>
  <p>Status: <strong data-testid="vision-status">{{ status() }}</strong></p>
  <p class="muted">Reads scanned PDF pages and charts or tables that are pictures. It can run on a different endpoint than the chat model. Page images are sent only to the endpoint configured here.</p>
  <label class="check"><input type="checkbox" [ngModel]="enabled()" (ngModelChange)="enabled.set($event)"> Use the vision model for documents</label>
  <label>OpenAI-compatible endpoint URL
    <input [ngModel]="baseURL()" (ngModelChange)="baseURL.set($event)" placeholder="https://api.openai.com/v1" maxlength="512" autocomplete="off">
  </label>
  <label>Vision model
    <input [ngModel]="model()" (ngModelChange)="model.set($event)" placeholder="gpt-4o-mini" maxlength="120" autocomplete="off">
  </label>
  <label>Fallback model (optional, same endpoint)
    <input [ngModel]="fallback()" (ngModelChange)="fallback.set($event)" maxlength="120" autocomplete="off">
  </label>
  <label class="check"><input type="checkbox" [ngModel]="supportsImages()" (ngModelChange)="supportsImages.set($event)"> This model accepts image input</label>
  <p>API key: <strong>{{ hasKey() ? 'Configured' : 'Not configured' }}</strong></p>
  <label>New API key (leave blank to keep the current key)
    <input type="password" autocomplete="new-password" [ngModel]="apiKey()" (ngModelChange)="apiKey.set($event)" placeholder="Enter vision API key">
  </label>
  <fieldset class="limits">
    <legend>Limits</legend>
    <label>Pages per document <input type="number" min="1" max="20" [ngModel]="limits().max_pages_per_document" (ngModelChange)="setLimit('max_pages_per_document', $event)"></label>
    <label>Images per run <input type="number" min="1" max="100" [ngModel]="limits().max_images_per_run" (ngModelChange)="setLimit('max_images_per_run', $event)"></label>
    <label>Tokens per run <input type="number" min="1000" max="2000000" [ngModel]="limits().max_tokens_per_run" (ngModelChange)="setLimit('max_tokens_per_run', $event)"></label>
    <label>Timeout per call (seconds) <input type="number" min="5" max="300" [ngModel]="limits().timeout_seconds" (ngModelChange)="setLimit('timeout_seconds', $event)"></label>
    <label>Parallel calls <input type="number" min="1" max="4" [ngModel]="limits().concurrency" (ngModelChange)="setLimit('concurrency', $event)"></label>
  </fieldset>
  <div class="actions">
    <button class="btn btn-sm btn-primary" [disabled]="busy() || !encryptionReady()" (click)="save()">Save vision settings</button>
    <button class="btn btn-sm btn-outline-secondary" [disabled]="busy() || !canTest()" (click)="test()">Test with an image</button>
    <button class="btn btn-sm btn-outline-secondary" [disabled]="busy() || !configured()" (click)="remove()">Remove</button>
  </div>
  @if (message()) { <p class="alert alert-info py-2 mb-0" role="status">{{ message() }}</p> }
  @if (verifiedAt()) { <p class="muted">Last passed the image test: {{ verifiedAt() }}.</p> }
  <p class="muted">Save first, then test: the test sends a generated picture and checks that the model describes it, so a text-only model is never used for images. Changing the endpoint or model requires a new test. Calls: {{ telemetry().calls }}, failures: {{ telemetry().failures }}, cache hits: {{ telemetry().cache_hits }}, images: {{ telemetry().images }}, tokens: {{ telemetry().tokens }}@if (telemetry().last_error_at) {, last error {{ telemetry().last_error_code }} at {{ telemetry().last_error_at }} }.</p>
</div>
  `,
  styles: [`
.card{display:flex;flex-direction:column;gap:12px;padding:22px;background:var(--fs-surface)}
label{display:flex;flex-direction:column;gap:5px;font-size:14px}
label.check{flex-direction:row;align-items:center;gap:8px}
label.check input{width:auto}
input{width:100%;box-sizing:border-box;border:1px solid var(--fs-border-strong);background:var(--fs-surface);color:var(--fs-text);padding:10px;border-radius:7px;font:inherit}
.limits{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,150px),1fr));gap:10px;border:1px solid var(--fs-border);border-radius:7px;padding:10px}
.actions{display:flex;flex-wrap:wrap;gap:10px}
.muted{color:var(--fs-muted)}
h3{margin:0 0 10px}
  `],
})
export class VisionSettingsComponent {
  readonly busy = signal(false);
  readonly message = signal('');
  readonly encryptionReady = signal(true);
  readonly enabled = signal(false);
  readonly baseURL = signal('');
  readonly model = signal('');
  readonly fallback = signal('');
  readonly supportsImages = signal(false);
  readonly apiKey = signal('');
  readonly hasKey = signal(false);
  readonly verifiedAt = signal('');
  readonly state = signal('not_configured');
  readonly limits = signal<VisionSettings['limits']>({ max_pages_per_document: 6, max_images_per_run: 20, max_tokens_per_run: 200000, timeout_seconds: 90, concurrency: 2 });
  readonly telemetry = signal<VisionSettings['telemetry']>({ calls: 0, failures: 0, cache_hits: 0, images: 0, tokens: 0, last_error_code: '', last_error_at: '' });
  readonly status = computed(() => statusText(this.state()));
  readonly configured = computed(() => this.state() !== 'not_configured');
  readonly canTest = computed(() => this.configured() && this.state() !== 'no_image_support_declared');

  constructor() { void this.refresh(); }

  private async api(path: string, init?: RequestInit): Promise<any> {
    const res = await fetch(path, { ...init, credentials: 'same-origin', headers: { 'Content-Type': 'application/json', ...init?.headers } });
    const body = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(body.error || 'Request failed (HTTP ' + res.status + ')');
    return body;
  }

  private apply(v: VisionSettings): void {
    this.encryptionReady.set(v.encryption_ready !== false);
    this.state.set(v.status);
    this.enabled.set(v.enabled);
    this.baseURL.set(v.base_url || '');
    this.model.set(v.model || '');
    this.fallback.set(v.fallback_model || '');
    this.supportsImages.set(v.supports_images);
    this.hasKey.set(v.has_key);
    this.verifiedAt.set(v.verified && v.verified_at ? utcTime(v.verified_at) : '');
    this.limits.set(v.limits);
    this.telemetry.set(v.telemetry);
    this.apiKey.set('');
  }

  async refresh(): Promise<void> {
    try { this.apply(await this.api('/api/admin/vision')); }
    catch (e) { this.message.set('Unable to load the vision settings: ' + String(e)); }
  }

  setLimit(key: keyof VisionSettings['limits'], value: number | string): void {
    this.limits.update(l => ({ ...l, [key]: Number(value) || 0 }));
  }

  private async perform(action: () => Promise<void>): Promise<void> {
    if (this.busy()) return;
    this.busy.set(true); this.message.set('');
    try { await action(); } catch (e) { this.message.set(String(e)); } finally { this.busy.set(false); }
  }

  save(): Promise<void> {
    return this.perform(async () => {
      this.apply(await this.api('/api/admin/vision', { method: 'PUT', body: JSON.stringify(visionRequestBody({
        enabled: this.enabled(), baseURL: this.baseURL(), model: this.model(), fallback: this.fallback(),
        supportsImages: this.supportsImages(), apiKey: this.apiKey(), limits: this.limits(),
      })) }));
      this.message.set('Vision settings saved. Run the image test before the model is used.');
    });
  }

  test(): Promise<void> {
    return this.perform(async () => {
      const out = await this.api('/api/admin/vision/test', { method: 'POST', body: '{}' });
      this.apply(out.settings);
      this.message.set(out.result.ok ? 'The model accepted an image and described it correctly. Vision is ready.' : probeAdvice(out.result.code, out.result.message));
    });
  }

  remove(): Promise<void> {
    if (!confirm('Remove the vision model settings and its key?')) return Promise.resolve();
    return this.perform(async () => {
      await this.api('/api/admin/vision', { method: 'DELETE' });
      await this.refresh();
      this.message.set('Vision model settings removed. Scanned pages are reported as unread.');
    });
  }
}
