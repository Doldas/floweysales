import { Component, signal } from '@angular/core';

interface TradingSettings {
  base_cash: number;
  allow_short: boolean; halt_new_orders: boolean; require_paper_approval: boolean; approval_ttl_minutes: number;
  max_order_notional: number; max_symbol_allocation_pct: number; max_sector_allocation_pct: number;
  max_daily_volume_pct: number; max_drawdown_pct: number; commission_pct: number; commission_fixed: number;
}
interface TradingView { settings: TradingSettings; defaults: TradingSettings; saved: boolean }

type NumberKey = { [K in keyof TradingSettings]: TradingSettings[K] extends number ? K : never }[keyof TradingSettings];
type BoolKey = { [K in keyof TradingSettings]: TradingSettings[K] extends boolean ? K : never }[keyof TradingSettings];

const LIMITS: { key: NumberKey; label: string; hint: string; step: string }[] = [
  { key: 'base_cash', label: 'Starting cash', hint: 'The account’s equity before any trade. Changing it moves the equity of the whole history; use Reset account below to start over.', step: '1' },
  { key: 'max_order_notional', label: 'Largest single order', hint: 'Orders worth more are rejected. 0 means no limit.', step: '1' },
  { key: 'max_symbol_allocation_pct', label: 'Largest share of equity in one symbol (%)', hint: 'New buys that would exceed it are rejected. 0 means off.', step: '0.1' },
  { key: 'max_sector_allocation_pct', label: 'Largest share of equity in one sector (%)', hint: 'Needs PAPER_SYMBOL_SECTORS in the server environment. 0 means off.', step: '0.1' },
  { key: 'max_daily_volume_pct', label: 'Largest share of a symbol’s daily volume (%)', hint: 'Needs PAPER_DAILY_TRADED_NOTIONAL in the server environment. 0 means off.', step: '0.1' },
  { key: 'max_drawdown_pct', label: 'Stop new buys at drawdown (%)', hint: 'Peak-to-current paper equity. 0 means off.', step: '0.1' },
  { key: 'commission_pct', label: 'Commission (% of the order value)', hint: 'Charged on every execution.', step: '0.001' },
  { key: 'commission_fixed', label: 'Commission (fixed per execution)', hint: 'Added to the percentage.', step: '0.01' },
  { key: 'approval_ttl_minutes', label: 'Approval lifetime (minutes)', hint: 'How long a proposed order waits for approval. 1 to 10080.', step: '1' },
];
const SWITCHES: { key: BoolKey; label: string; hint: string }[] = [
  { key: 'halt_new_orders', label: 'Halt all new orders', hint: 'Kill switch: no new simulated orders and no fills of resting ones.' },
  { key: 'require_paper_approval', label: 'Require approval before an order executes', hint: 'Orders wait for a SYS_ADMIN to approve them.' },
  { key: 'allow_short', label: 'Allow short selling', hint: 'Sells beyond the held quantity open a short position.' },
];

@Component({
  selector: 'fse-trading-settings', host: { class: 'fs-native-forms' },
  standalone: true,
  template: `
  <div class="card">
   <h3>Paper trading limits and costs</h3>
   <p class="muted">Applies to simulated orders only, and takes effect at once. Values saved here replace <code>PAPER_*</code> and <code>TRADE_COMMISSION_*</code> from the server environment; Reset goes back to them. The sector and volume tables stay in the environment.</p>
   @if (error()) { <p class="warning" role="alert">{{ error() }}</p> }
   @if (message()) { <p role="status">{{ message() }}</p> }
   @if (form(); as f) {
    <form (submit)="$event.preventDefault(); save()">
     @for (s of switches; track s.key) {
      <label class="switch"><input type="checkbox" [checked]="f[s.key]" (change)="setBool(s.key, $any($event.target).checked)" />
       <span><strong>{{ s.label }}</strong><small>{{ s.hint }}</small></span></label>
     }
     <div class="grid">
      @for (l of limits; track l.key) {
       <label>{{ l.label }}
        <input class="form-control form-control-sm" type="number" min="0" [step]="l.step" [value]="f[l.key]" (input)="setNumber(l.key, $any($event.target).value)" />
        <small>{{ l.hint }}</small></label>
      }
     </div>
     <div class="actions">
      <button type="submit" class="btn btn-sm btn-primary" [disabled]="busy()">Save</button>
      @if (saved()) { <button type="button" class="btn btn-sm btn-ghost" [disabled]="busy()" (click)="reset()">Reset to environment values</button> }
     </div>
    </form>
   }
  </div>
  <div class="card danger">
   <h3>Reset the paper account</h3>
   <p class="muted">Deletes every simulated order with its audit trail, approval proposals and the drawdown high-water mark, so the account starts again from the starting cash. This cannot be undone. Settings above are kept.</p>
   <form class="reset" (submit)="$event.preventDefault(); resetAccount(confirm.value); confirm.value = ''">
    <label class="visually-hidden" for="reset-confirm">Type RESET to confirm</label>
    <input #confirm id="reset-confirm" class="form-control form-control-sm" autocomplete="off" placeholder="Type RESET to confirm" />
    <button type="submit" class="btn btn-sm btn-danger" [disabled]="busy()">Reset account</button>
   </form>
  </div>`,
  styles: [`
   :host{display:flex;flex-direction:column;gap:18px}
   .card{display:flex;flex-direction:column;gap:14px;padding:22px;background:var(--fs-surface);min-width:0}
   h3{margin:0} .muted,small{color:var(--fs-muted)} small{display:block;font-size:12px;font-weight:400}
   form{display:flex;flex-direction:column;gap:16px}
   .switch{display:flex;gap:10px;align-items:flex-start}
   .grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,280px),1fr));gap:14px}
   .grid label{display:flex;flex-direction:column;gap:4px;font-weight:500}
   .actions{display:flex;gap:8px}
   .reset{display:flex;gap:8px;max-width:420px}
   .danger{border:1px solid var(--fs-danger, #c0392b)}
  `],
})
export class TradingSettingsComponent {
  readonly limits = LIMITS;
  readonly switches = SWITCHES;
  readonly form = signal<TradingSettings | null>(null);
  readonly saved = signal(false);
  readonly busy = signal(false);
  readonly message = signal('');
  readonly error = signal('');

  constructor() { void this.load(); }

  private async api(init?: RequestInit): Promise<TradingView> {
    const res = await fetch('/api/admin/trading-settings', { credentials: 'same-origin', cache: 'no-store', ...init, headers: { 'Content-Type': 'application/json', ...init?.headers } });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.error || 'HTTP ' + res.status);
    return data;
  }

  private show(v: TradingView): void { this.form.set({ ...v.settings }); this.saved.set(v.saved); }

  async load(): Promise<void> {
    try { this.show(await this.api()); this.error.set(''); } catch (e) { this.error.set(String((e as Error).message ?? e)); }
  }

  setBool(key: BoolKey, value: boolean): void { this.form.update(f => f && { ...f, [key]: value }); }
  setNumber(key: NumberKey, raw: string): void { this.form.update(f => f && { ...f, [key]: raw === '' ? 0 : Number(raw) }); }

  async save(): Promise<void> {
    const f = this.form();
    if (!f) return;
    await this.run(() => this.api({ method: 'PUT', body: JSON.stringify(f) }), 'Saved. The new limits apply to the next order.');
  }

  async reset(): Promise<void> {
    if (!confirm('Go back to the values from the server environment?')) return;
    await this.run(() => this.api({ method: 'DELETE' }), 'Reset to the environment values.');
  }

  async resetAccount(typed: string): Promise<void> {
    if (typed.trim() !== 'RESET') { this.error.set('Type RESET in the box to confirm.'); return; }
    this.busy.set(true); this.message.set(''); this.error.set('');
    try {
      const res = await fetch('/api/admin/trading-settings/reset-account', { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ confirm: 'RESET' }) });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error || 'HTTP ' + res.status);
      this.message.set(`The paper account was reset (${data.deleted_orders} orders deleted).`);
    } catch (e) { this.error.set(String((e as Error).message ?? e)); }
    finally { this.busy.set(false); }
  }

  private async run(call: () => Promise<TradingView>, done: string): Promise<void> {
    this.busy.set(true); this.message.set(''); this.error.set('');
    try { this.show(await call()); this.message.set(done); } catch (e) { this.error.set(String((e as Error).message ?? e)); }
    finally { this.busy.set(false); }
  }
}
