import { Component, computed, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import {
  SEARCH_NAMES, attemptLine, formFrom, formProblems, formatUsd, kindHelp, moveProvider, providerBody, stateLabel, usageLine, utcTime,
  type ProviderForm, type SearchAttempt, type SearchCatalogEntry, type SearchPolicy, type SearchProviderState,
} from './web-search-view';

interface AdminView {
  encryption_ready: boolean; available: boolean; catalog: SearchCatalogEntry[]; providers: SearchProviderState[];
  policy: SearchPolicy; recent: SearchAttempt[];
}

/** SYS_ADMIN settings for web search: several providers, their order, budgets and the fallback policy. */
@Component({
  selector: 'fse-web-search-settings',
  standalone: true,
  imports: [FormsModule],
  host: { class: 'fs-native-forms' },
  template: `
  <section class="ws" aria-labelledby="ws-title">
   <h3 id="ws-title">Web search</h3>
   <p class="muted">Used by the research agent, agent teams and the Flow Studio web search node. They all search by the order and limits below. A search result is a snippet: it is not a page that was read and not a verified fact. Users see whether search works, never keys or addresses.</p>
   @if(message()){<p class="alert alert-info py-2 mb-0" role="status">{{message()}}</p>}
   @if(loaded() && !view()!.available){
    <div class="help" role="note">
     <strong>No search provider is ready.</strong>
     Without one, the agent cannot search the web and says so. The quickest free choice is your own SearXNG instance; Tavily and Brave need an API key; Google Search through Gemini costs money and needs your explicit approval.
    </div>
   }
   @for(p of providers(); track p.id){
    <article class="provider" [attr.aria-label]="info(p.id).name">
     <header>
      <h4>{{info(p.id).name}}</h4>
      <span class="badge" [class.ok]="p.state==='ready'" [class.warn]="p.state==='needs_approval'||p.state==='paused'">{{label(p.state)}}</span>
      <span class="order">
       <button type="button" class="btn btn-sm btn-outline-secondary" [attr.aria-label]="'Move '+info(p.id).name+' up'" [disabled]="busy()||$first" (click)="move(p.id,-1)">&uarr;</button>
       <button type="button" class="btn btn-sm btn-outline-secondary" [attr.aria-label]="'Move '+info(p.id).name+' down'" [disabled]="busy()||$last" (click)="move(p.id,1)">&darr;</button>
      </span>
     </header>
     <p class="muted">{{info(p.id).pricing}}</p>
     @if(info(p.id).legacy){<p class="warning">Legacy: Google closed this API to new customers. Existing customers must migrate by January 1, 2027. Do not set it up for new use.</p>}
     @if(info(p.id).notes){<p class="muted">{{info(p.id).notes}}</p>}
     @if(info(p.id).docs_url){<p class="muted"><a [href]="info(p.id).docs_url" target="_blank" rel="noopener noreferrer">Provider documentation</a></p>}
     @if(forms()[p.id]; as f){
      <label class="check"><input type="checkbox" [ngModel]="f.enabled" (ngModelChange)="patch(p.id,{enabled:$event})"> Enabled</label>
      @if(info(p.id).needs_endpoint){
       <label>Instance address
        <input [ngModel]="f.endpoint" (ngModelChange)="patch(p.id,{endpoint:$event})" placeholder="http://searxng:8080" maxlength="512" autocomplete="off">
       </label>
      }
      @if(!info(p.id).keyless){
       <p>API key: <strong>{{p.has_key?'Saved (hidden)':'Not saved'}}</strong></p>
       <label>{{p.has_key?'New API key (leave blank to keep the saved one)':'API key'}}
        <input type="password" autocomplete="new-password" [ngModel]="f.api_key" (ngModelChange)="patch(p.id,{api_key:$event})" placeholder="Enter API key">
       </label>
      }
      @if(info(p.id).needs_model){
       <label>Gemini model
        <input [ngModel]="f.model" (ngModelChange)="patch(p.id,{model:$event})" maxlength="128" [placeholder]="info(p.id).default_model">
       </label>
      }
      @if(info(p.id).needs_engine_id){
       <label>Search engine ID (cx)
        <input [ngModel]="f.engine_id" (ngModelChange)="patch(p.id,{engine_id:$event})" maxlength="128" autocomplete="off">
       </label>
      }
      @if(info(p.id).requires_approval){
       <fieldset class="cost">
        <legend>Cost control</legend>
        <p class="warning">This provider costs money. It is never used until you approve it here, and then only within the ceilings below. A free provider that fails is not silently replaced by this one without that approval.</p>
        <div class="grid">
         <label>Price per 1,000 searches (USD, estimate)
          <input type="number" min="0" step="0.5" [ngModel]="f.price_per_1000_usd" (ngModelChange)="patch(p.id,{price_per_1000_usd:+$event})">
         </label>
         <label>Free searches per month
          <input type="number" min="0" step="100" [ngModel]="f.free_monthly_searches" (ngModelChange)="patch(p.id,{free_monthly_searches:+$event})">
         </label>
         @if(info(p.id).model_mediated){
          <label>Model input price (USD per million tokens)
           <input type="number" min="0" step="0.05" [ngModel]="f.input_per_mtok_usd" (ngModelChange)="patch(p.id,{input_per_mtok_usd:+$event})">
          </label>
          <label>Model output price (USD per million tokens)
           <input type="number" min="0" step="0.05" [ngModel]="f.output_per_mtok_usd" (ngModelChange)="patch(p.id,{output_per_mtok_usd:+$event})">
          </label>
         }
         <label>Daily ceiling (USD)
          <input type="number" min="0" step="1" [ngModel]="f.daily_budget_usd" (ngModelChange)="patch(p.id,{daily_budget_usd:+$event})">
         </label>
         <label>Monthly ceiling (USD)
          <input type="number" min="0" step="1" [ngModel]="f.monthly_budget_usd" (ngModelChange)="patch(p.id,{monthly_budget_usd:+$event})">
         </label>
        </div>
        @if(info(p.id).model_mediated){<p class="muted">Gemini can run several Google searches for one prompt. Searches and model tokens are counted separately; model token cost is only estimated when you enter token prices. Before each call the limits assume up to 3 searches.</p>}
        <label class="check"><input type="checkbox" [ngModel]="f.paid_approved" (ngModelChange)="patch(p.id,{paid_approved:$event})"> I approve paid use of {{info(p.id).name}} within these ceilings</label>
       </fieldset>
      }
      @for(problem of problems(p.id); track problem){<p class="alert alert-warning py-2 mb-0" role="alert">{{problem}}</p>}
      <div class="actions">
       <button class="btn btn-sm btn-primary" [disabled]="busy()||!view()!.encryption_ready||problems(p.id).length>0" (click)="save(p.id)">Save</button>
       <button class="btn btn-sm btn-outline-secondary" [disabled]="busy()||!p.configured" (click)="test(p.id)">Test connection</button>
       <button class="btn btn-sm btn-outline-secondary" [disabled]="busy()||(!p.configured&&!p.enabled)" (click)="remove(p.id)">Remove settings</button>
      </div>
     }
     @if(tests()[p.id]; as t){
      <p [class.ok-text]="t.ok" role="status">{{t.message}} @if(t.ok){<span>({{t.results}} results, {{t.duration_ms}} ms@if(t.est_cost_usd){, est. {{usd(t.est_cost_usd)}}})</span>} @else if(help(t.kind)){<span class="muted">{{help(t.kind)}}</span>}</p>
     }
     @if(p.status.last_outcome){
      <p class="muted">Last result: {{p.status.last_outcome}}@if(p.status.last_at_utc){ at {{time(p.status.last_at_utc)}}}@if(p.status.last_kind){ &mdash; {{help(p.status.last_kind)}}}@if(p.status.circuit_open_until_utc){ Paused until {{time(p.status.circuit_open_until_utc)}}.}</p>
     }
     @if(p.usage_month.searches>0||p.usage_today.searches>0){
      <p class="muted">Today: {{usage(p.usage_today)}}<br>This month: {{usage(p.usage_month)}}</p>
     }
    </article>
   }
   <article class="provider" aria-label="Search policy">
    <h4>Policy</h4>
    @if(policy(); as pol){
     <label>Provider choice
      <select [ngModel]="pol.mode" (ngModelChange)="patchPolicy({mode:$event})">
       <option value="auto">Automatic: use the providers above in order, with fallback</option>
       <option value="manual">Manual: always use one provider, no fallback</option>
      </select>
     </label>
     @if(pol.mode==='manual'){
      <label>Provider
       <select [ngModel]="pol.selected||''" (ngModelChange)="patchPolicy({selected:$event})">
        <option value="">Choose a provider</option>
        @for(p of providers(); track p.id){<option [value]="p.id">{{info(p.id).name}}</option>}
       </select>
      </label>
     } @else {
      <label>Providers tried per search (1 to 5)
       <input type="number" min="1" max="5" [ngModel]="pol.max_attempts" (ngModelChange)="patchPolicy({max_attempts:+$event})">
      </label>
     }
     <label>Retries after a timeout or server error (0 to 2)
      <input type="number" min="0" max="2" [ngModel]="pol.max_retries" (ngModelChange)="patchPolicy({max_retries:+$event})">
     </label>
     <label>Spending ceiling per agent or flow run (USD, 0 = none)
      <input type="number" min="0" step="0.5" [ngModel]="pol.per_run_budget_usd||0" (ngModelChange)="patchPolicy({per_run_budget_usd:+$event})">
     </label>
     <p class="muted">A provider that is rate limited or rejects its key is paused for a while instead of being asked again. Move providers up or down above to change the order.</p>
     <div class="actions"><button class="btn btn-sm btn-primary" [disabled]="busy()||!view()!.encryption_ready" (click)="savePolicy()">Save policy</button></div>
    }
   </article>
   @if(recent().length){
    <article class="provider" aria-label="Recent search decisions">
     <h4>Recent decisions</h4>
     <p class="muted">Which provider answered, what was skipped and why. Queries are never recorded here.</p>
     <ul>@for(a of recent(); track $index){<li>{{attempt(a)}}</li>}</ul>
    </article>
   }
  </section>
  `,
  styles: [`
   .ws{display:flex;flex-direction:column;gap:14px}
   .ws h3,.ws h4{margin:0}
   .provider{display:flex;flex-direction:column;gap:10px;padding:18px;border:1px solid var(--fs-border);border-radius:10px;background:var(--fs-surface)}
   .provider header{display:flex;align-items:center;gap:10px;flex-wrap:wrap}
   .provider header .order{margin-left:auto;display:flex;gap:6px}
   .badge{padding:2px 9px;border-radius:999px;border:1px solid var(--fs-border-strong);font-size:12px;color:var(--fs-muted)}
   .badge.ok{color:var(--fs-text);border-color:var(--fs-accent)}
   .badge.warn{background:var(--fs-warn-soft);color:var(--fs-text)}
   label{display:flex;flex-direction:column;gap:5px;font-size:14px}
   label.check{flex-direction:row;align-items:center;gap:8px}
   label.check input{width:auto}
   input,select{width:100%;box-sizing:border-box;border:1px solid var(--fs-border-strong);background:var(--fs-surface);color:var(--fs-text);padding:10px;border-radius:7px;font:inherit}
   .grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,220px),1fr));gap:12px}
   fieldset.cost{display:flex;flex-direction:column;gap:10px;border:1px solid var(--fs-border);border-radius:8px;padding:12px}
   .actions{display:flex;flex-wrap:wrap;gap:10px}
   .muted{color:var(--fs-muted);margin:0}
   .warning,.help{padding:12px;margin:0;border-left:4px solid color-mix(in srgb, var(--fs-warn) 55%, transparent);background:var(--fs-warn-soft);color:var(--fs-text)}
   .ok-text{color:var(--fs-text)}
   ul{margin:0;padding-left:20px;font-size:14px}
  `],
})
export class WebSearchSettingsComponent {
  readonly busy = signal(false);
  readonly message = signal('');
  readonly loaded = signal(false);
  readonly view = signal<AdminView | null>(null);
  readonly forms = signal<Record<string, ProviderForm>>({});
  readonly tests = signal<Record<string, { ok: boolean; kind?: string; message: string; results: number; duration_ms: number; est_cost_usd?: number }>>({});
  readonly policy = signal<SearchPolicy | null>(null);
  readonly providers = computed(() => this.view()?.providers ?? []);
  readonly recent = computed(() => (this.view()?.recent ?? []).slice(-12).reverse());
  private readonly names = computed(() => Object.fromEntries((this.view()?.catalog ?? []).map(c => [c.id, c.name])));

  constructor() { void this.refresh(); }

  readonly label = stateLabel;
  readonly help = kindHelp;
  readonly usage = usageLine;
  readonly usd = formatUsd;
  readonly time = utcTime;
  info(id: string): SearchCatalogEntry {
    return this.view()?.catalog.find(c => c.id === id) ?? { id, name: SEARCH_NAMES[id] ?? id } as SearchCatalogEntry;
  }
  attempt(a: SearchAttempt): string { return attemptLine(a, { ...SEARCH_NAMES, ...this.names() }); }
  problems(id: string): string[] {
    const f = this.forms()[id];
    const p = this.providers().find(x => x.id === id);
    return f && p ? formProblems(f, this.info(id), p.has_key) : [];
  }
  patch(id: string, change: Partial<ProviderForm>): void {
    this.forms.update(all => ({ ...all, [id]: { ...all[id], ...change } }));
  }
  patchPolicy(change: Partial<SearchPolicy>): void {
    this.policy.update(p => (p ? { ...p, ...change } : p));
  }

  private async api(path: string, init?: RequestInit): Promise<any> {
    const res = await fetch(path, { ...init, credentials: 'same-origin', headers: { 'Content-Type': 'application/json', ...init?.headers } });
    const body = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(body.error || 'Request failed (HTTP ' + res.status + ')');
    return body;
  }

  async refresh(): Promise<void> {
    try {
      const view = await this.api('/api/admin/web-search') as AdminView;
      this.view.set(view);
      this.policy.set(view.policy);
      this.forms.set(Object.fromEntries(view.providers.map(p => [p.id, formFrom(p)])));
      this.loaded.set(true);
    } catch (e) { this.message.set('Unable to load web search settings: ' + String(e)); }
  }

  async save(id: string): Promise<void> {
    const form = this.forms()[id];
    await this.perform(async () => {
      await this.api('/api/admin/web-search/providers/' + id, { method: 'PUT', body: JSON.stringify(providerBody(form)) });
      await this.refresh();
      this.message.set(this.info(id).name + ' saved. The key is encrypted and is not shown again.');
    });
  }

  async remove(id: string): Promise<void> {
    if (!confirm('Remove the saved settings and key of ' + this.info(id).name + '?')) return;
    await this.perform(async () => {
      await this.api('/api/admin/web-search/providers/' + id, { method: 'DELETE' });
      await this.refresh();
      this.message.set(this.info(id).name + ' removed.');
    });
  }

  async test(id: string): Promise<void> {
    const info = this.info(id);
    const body: Record<string, boolean> = {};
    if (info.requires_approval) {
      if (!confirm('This runs one real search with ' + info.name + ' and may be billed. Continue?')) return;
      body['confirm_paid'] = true;
    }
    await this.perform(async () => {
      const result = await this.api('/api/admin/web-search/providers/' + id + '/test', { method: 'POST', body: JSON.stringify(body) });
      this.tests.update(all => ({ ...all, [id]: result }));
      await this.refreshKeepForms();
    });
  }

  private async refreshKeepForms(): Promise<void> {
    const view = await this.api('/api/admin/web-search') as AdminView;
    this.view.set(view);
  }

  async savePolicy(): Promise<void> {
    const pol = this.policy();
    if (!pol) return;
    await this.perform(async () => {
      const order = this.view()?.policy.order ?? pol.order;
      await this.api('/api/admin/web-search/policy', { method: 'PUT', body: JSON.stringify({ ...pol, order }) });
      await this.refresh();
      this.message.set('Search policy saved.');
    });
  }

  async move(id: string, delta: -1 | 1): Promise<void> {
    const pol = this.view()?.policy;
    if (!pol) return;
    const order = moveProvider(pol.order, id, delta);
    await this.perform(async () => {
      await this.api('/api/admin/web-search/policy', { method: 'PUT', body: JSON.stringify({ ...(this.policy() ?? pol), order }) });
      await this.refresh();
      this.message.set('Order saved.');
    });
  }

  private async perform(action: () => Promise<void>): Promise<void> {
    if (this.busy()) return;
    this.busy.set(true); this.message.set('');
    try { await action(); } catch (e) { this.message.set(String(e instanceof Error ? e.message : e)); }
    finally { this.busy.set(false); }
  }
}
