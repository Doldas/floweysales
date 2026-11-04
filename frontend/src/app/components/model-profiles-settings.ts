import { Component, signal } from '@angular/core';

import type { ModelProfile } from '../models';
import { profileSummary } from './profile-view';

interface Draft {
  id: number; name: string; description: string; model: string; fallback_model: string;
  supports_tools: '' | 'yes' | 'no'; context_window: string; cost_input: string; cost_output: string;
  max_steps: string; max_tool_calls: string; enabled: boolean; is_default: boolean; position: string;
}

const blank = (): Draft => ({ id: 0, name: '', description: '', model: '', fallback_model: '', supports_tools: '', context_window: '', cost_input: '', cost_output: '',
  max_steps: '', max_tool_calls: '', enabled: true, is_default: false, position: '0' });

const num = (s: string): number | null => (s.trim() === '' || !Number.isFinite(Number(s)) ? null : Number(s));

/**
 * SYS_ADMIN prepares the model choices; everyone else picks one by name. A
 * profile names a model on the configured endpoint, an optional fallback, and
 * what is known about it. Users never see the endpoint or the key.
 */
@Component({
  selector: 'fse-model-profiles-settings', host: { class: 'fs-native-forms' },
  standalone: true,
  template: `
  <div class="card">
   <h3>Model profiles</h3>
   <p class="muted">Prepare a few named model choices, such as &ldquo;Fast and cheap&rdquo; or &ldquo;Deep research&rdquo;. Users and agent teams pick a profile by name; they never see the endpoint or the key, and cannot pick a raw model. The model must be the default model or in the model catalog (API integrations).</p>
   @if (error()) { <p class="warning" role="alert">{{ error() }}</p> }
   @if (message()) { <p role="status">{{ message() }}</p> }
   @for (p of profiles(); track p.id) {
    <div class="item">
     <span><strong>{{ p.name }}</strong>
      @if (p.is_default) { <em> default</em> }@if (!p.enabled) { <em> disabled</em> }
      <small>{{ p.description }}</small><small>{{ summary(p) }}@if (p.fallback_model) { · falls back to {{ p.fallback_model }} }</small></span>
     <span class="actions">
      <button type="button" class="btn btn-sm btn-outline-secondary" (click)="edit(p)">Edit</button>
      <button type="button" class="btn btn-sm btn-outline-secondary" [disabled]="busy()" (click)="remove(p)">Delete</button>
     </span>
    </div>
   } @empty { <p class="muted">No profiles yet. Everyone uses the server's default model.</p> }
  </div>
  <div class="card">
   <h3>{{ draft().id ? 'Edit profile' : 'New profile' }}</h3>
   <form (submit)="$event.preventDefault(); save()">
    <div class="grid">
     <label>Name <input class="form-control form-control-sm" maxlength="60" [value]="draft().name" (input)="set('name', $any($event.target).value)" placeholder="Deep research" /></label>
     <label>Model <input class="form-control form-control-sm" maxlength="120" [value]="draft().model" (input)="set('model', $any($event.target).value)" placeholder="gpt-4o" /></label>
     <label>Fallback model (optional) <input class="form-control form-control-sm" maxlength="120" [value]="draft().fallback_model" (input)="set('fallback_model', $any($event.target).value)" />
      <small>Used for the rest of a run if the model is not available.</small></label>
     <label>Description <input class="form-control form-control-sm" maxlength="300" [value]="draft().description" (input)="set('description', $any($event.target).value)" placeholder="Slower and more careful" /></label>
     <label>Can call tools
      <select class="form-select form-select-sm" [value]="draft().supports_tools" (change)="set('supports_tools', $any($event.target).value)">
       <option value="" [selected]="draft().supports_tools===''">Unknown</option><option value="yes" [selected]="draft().supports_tools==='yes'">Yes</option><option value="no" [selected]="draft().supports_tools==='no'">No (runs without tools)</option>
      </select></label>
     <label>Context window (tokens) <input class="form-control form-control-sm" type="number" min="1024" [value]="draft().context_window" (input)="set('context_window', $any($event.target).value)" placeholder="Unknown" /></label>
     <label>Input cost (USD per million tokens) <input class="form-control form-control-sm" type="number" min="0" step="0.01" [value]="draft().cost_input" (input)="set('cost_input', $any($event.target).value)" placeholder="Unknown" /></label>
     <label>Output cost (USD per million tokens) <input class="form-control form-control-sm" type="number" min="0" step="0.01" [value]="draft().cost_output" (input)="set('cost_output', $any($event.target).value)" placeholder="Unknown" /></label>
     <label>Most model rounds (1&ndash;32) <input class="form-control form-control-sm" type="number" min="1" max="32" [value]="draft().max_steps" (input)="set('max_steps', $any($event.target).value)" placeholder="Server limit" /></label>
     <label>Most tool calls (1&ndash;128) <input class="form-control form-control-sm" type="number" min="1" max="128" [value]="draft().max_tool_calls" (input)="set('max_tool_calls', $any($event.target).value)" placeholder="Server limit" /></label>
     <label>Order in lists <input class="form-control form-control-sm" type="number" min="0" max="1000" [value]="draft().position" (input)="set('position', $any($event.target).value)" /></label>
    </div>
    <label class="switch"><input type="checkbox" [checked]="draft().enabled" (change)="set('enabled', $any($event.target).checked)" /><span><strong>Enabled</strong><small>Users can pick it.</small></span></label>
    <label class="switch"><input type="checkbox" [checked]="draft().is_default" (change)="set('is_default', $any($event.target).checked)" /><span><strong>Default</strong><small>Used when a user picks none. Only one profile is the default.</small></span></label>
    <div class="actions">
     <button type="submit" class="btn btn-sm btn-primary" [disabled]="busy() || !draft().name.trim() || !draft().model.trim()">{{ draft().id ? 'Save changes' : 'Create profile' }}</button>
     @if (draft().id) { <button type="button" class="btn btn-sm btn-ghost" (click)="draft.set(blank())">Cancel</button> }
    </div>
   </form>
  </div>`,
  styles: [`
   :host{display:flex;flex-direction:column;gap:18px}
   .card{display:flex;flex-direction:column;gap:14px;padding:22px;background:var(--fs-surface);min-width:0}
   h3{margin:0} .muted,small{color:var(--fs-muted)} small{display:block;font-size:12px;font-weight:400;overflow-wrap:anywhere}
   form{display:flex;flex-direction:column;gap:14px}
   .item{display:flex;gap:12px;justify-content:space-between;align-items:flex-start;border-top:1px solid var(--fs-border);padding-top:10px}
   .item em{font-style:normal;font-size:12px;color:var(--fs-muted)}
   .switch{display:flex;gap:10px;align-items:flex-start}
   .grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,260px),1fr));gap:14px}
   .grid label{display:flex;flex-direction:column;gap:4px;font-weight:500}
   .actions{display:flex;gap:8px;flex-wrap:wrap}
  `],
})
export class ModelProfilesSettingsComponent {
  readonly profiles = signal<ModelProfile[]>([]);
  readonly draft = signal<Draft>(blank());
  readonly busy = signal(false);
  readonly message = signal('');
  readonly error = signal('');
  readonly blank = blank;

  constructor() { void this.load(); }

  summary(p: ModelProfile): string { return profileSummary(p); }

  set<K extends keyof Draft>(key: K, value: Draft[K]): void { this.draft.update(d => ({ ...d, [key]: value })); }

  edit(p: ModelProfile): void {
    this.message.set(''); this.error.set('');
    this.draft.set({
      id: p.id, name: p.name, description: p.description, model: p.model, fallback_model: p.fallback_model ?? '',
      supports_tools: p.supports_tools === null ? '' : p.supports_tools ? 'yes' : 'no',
      context_window: p.context_window?.toString() ?? '', cost_input: p.cost_input_per_mtok?.toString() ?? '', cost_output: p.cost_output_per_mtok?.toString() ?? '',
      max_steps: p.max_steps?.toString() ?? '', max_tool_calls: p.max_tool_calls?.toString() ?? '', enabled: p.enabled !== false, is_default: p.is_default, position: String(p.position ?? 0),
    });
  }

  private async call(path: string, init?: RequestInit): Promise<any> {
    const res = await fetch(path, { credentials: 'same-origin', cache: 'no-store', ...init, headers: { 'Content-Type': 'application/json', ...init?.headers } });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.error || 'HTTP ' + res.status);
    return data;
  }

  async load(): Promise<void> {
    try { this.profiles.set((await this.call('/api/admin/ai/profiles')).profiles ?? []); }
    catch (e) { this.error.set((e as Error).message); }
  }

  async save(): Promise<void> {
    const d = this.draft();
    const body = {
      name: d.name.trim(), description: d.description.trim(), model: d.model.trim(), fallback_model: d.fallback_model.trim(),
      supports_tools: d.supports_tools === '' ? null : d.supports_tools === 'yes',
      context_window: num(d.context_window), cost_input_per_mtok: num(d.cost_input), cost_output_per_mtok: num(d.cost_output),
      max_steps: num(d.max_steps), max_tool_calls: num(d.max_tool_calls), enabled: d.enabled, is_default: d.is_default, position: num(d.position) ?? 0,
    };
    this.busy.set(true); this.error.set(''); this.message.set('');
    try {
      await this.call(d.id ? `/api/admin/ai/profiles/${d.id}` : '/api/admin/ai/profiles', { method: d.id ? 'PUT' : 'POST', body: JSON.stringify(body) });
      this.message.set(d.id ? 'Profile saved.' : 'Profile created.');
      this.draft.set(blank());
      await this.load();
    } catch (e) { this.error.set((e as Error).message); }
    finally { this.busy.set(false); }
  }

  async remove(p: ModelProfile): Promise<void> {
    this.busy.set(true); this.error.set(''); this.message.set('');
    try {
      await this.call(`/api/admin/ai/profiles/${p.id}`, { method: 'DELETE' });
      this.message.set(`${p.name} deleted. Teams that used it now use the default.`);
      await this.load();
    } catch (e) { this.error.set((e as Error).message); }
    finally { this.busy.set(false); }
  }
}
