import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import type { HttpErrorResponse } from '@angular/common/http';

import { MarketService } from '../services';
import type { BrokerAccount, BrokerAccountsView, BrokerEntry, BrokerLedgerRow, BrokersView } from '../models';
import {
  ORDER_TYPES, ledgerStatusLabel, money, needsLimit, needsStop, newClientOrderId, orderBody, outcomeUnknown, ticketIssues, type Ticket,
} from './broker-ticket';

/**
 * Settings → Trading → Brokers (#444). Brokers are plugins; this panel lists
 * what is installed, keeps the user's own connections, reads accounts and
 * sends paper orders to the broker and account the user picks. Live accounts
 * are listed and never traded, and no order goes anywhere the user did not
 * name. A saved value is never shown again, only that it is saved.
 */
@Component({
  selector: 'fse-broker-center',
  imports: [FormsModule],
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'fs-native-forms' },
  template: `
    @if (view(); as v) {
      <section class="bc" aria-labelledby="bc-title">
        <h3 id="bc-title">Brokers</h3>
        <p class="bc-safety" role="note"><strong>Paper trading only.</strong> {{ v.notice }}</p>
        @if (message()) { <p class="alert alert-info py-2" role="status">{{ message() }}</p> }

        <article class="bc-card" aria-label="Built-in paper account">
          <header><strong>Built-in paper account</strong> <span class="bc-chip">paper</span></header>
          <p class="muted">The simulated account of this app. Its orders, approvals and audit trail stay in the trade ticket and Approvals.</p>
        </article>

        @if (!v.brokers.length) { <p class="muted">No broker extension is installed. An administrator installs them under Settings → Plugins.</p> }
        @for (b of v.brokers; track b.provider_id) {
          <article class="bc-card" [attr.aria-label]="b.label">
            <header>
              <strong>{{ b.label }}</strong> <span class="muted">{{ b.provider_id }} · v{{ b.version }} · contract {{ b.contract }}</span>
              @for (env of b.broker?.environments ?? []; track env) { <span class="bc-chip" [class.bc-chip-live]="env === 'live'">{{ env }}</span> }
            </header>
            @if (!b.usable) {
              <ul class="bc-problems" role="alert">@for (p of b.problems ?? []; track p) { <li>{{ p }}</li> }</ul>
            } @else {
              <ul class="bc-connections" [attr.aria-label]="b.label + ' connections'">
                @for (c of connectionsOf(b); track c.name) {
                  <li>
                    <span><strong>{{ c.name }}</strong> @if (c.saved.length) { <span class="bc-saved">· {{ c.saved.length }} value(s) saved</span> }</span>
                    <button type="button" class="btn btn-sm btn-outline-secondary" [disabled]="busy()" (click)="remove(c.name)">Remove {{ c.name }}</button>
                  </li>
                }
                @if (!connectionsOf(b).length) { <li class="muted">No connection yet.</li> }
              </ul>
              <form class="bc-form" (submit)="$event.preventDefault(); save(b)">
                <label>Connection name
                  <input name="conn-name" [ngModel]="draftName(b)" (ngModelChange)="setName(b, $event)" maxlength="40" autocomplete="off" spellcheck="false" placeholder="main" />
                </label>
                @for (x of b.connection_vars ?? []; track x.name) {
                  <label>{{ x.name }}
                    <input type="password" autocomplete="off" spellcheck="false" [name]="b.provider_id + '-' + x.name"
                      [value]="draftValue(b, x.name)" (input)="setValue(b, x.name, $any($event.target).value)" />
                    <small class="muted">{{ x.description }}</small>
                  </label>
                }
                @if (problem()[b.provider_id]) { <p class="bc-error" role="alert">{{ problem()[b.provider_id] }}</p> }
                <div><button type="submit" class="btn btn-sm btn-primary" [disabled]="busy()">Save connection</button></div>
              </form>
            }
          </article>
        }
        @if (v.trading_hosts.length) {
          <p class="muted">Trading hosts: {{ hostNames(v) }}. A host proposes orders; it never places one.</p>
        }

        <section aria-labelledby="bc-acc-title">
          <h4 id="bc-acc-title">Accounts</h4>
          <button type="button" class="btn btn-sm btn-outline-secondary" [disabled]="busy()" (click)="loadAccounts()">Load accounts</button>
          @if (accounts(); as a) {
            @for (s of a.sources; track s.label) { @if (s.error) { <p class="bc-error" role="alert">{{ s.label }}: {{ s.error }}</p> } }
            @if (!a.accounts.length) { <p class="muted">No accounts were returned.</p> }
            <p class="muted">{{ a.mixed_currencies }}</p>
            <ul class="bc-accounts" aria-label="Broker accounts">
              @for (acc of a.accounts; track acc.connection + '/' + acc.id) {
                <li [class.bc-live]="!acc.tradable">
                  <strong>{{ acc.label || acc.id }}</strong> <span class="muted">{{ acc.broker }} · {{ acc.connection || 'system' }} · {{ acc.id }}</span>
                  <span class="bc-chip" [class.bc-chip-live]="!acc.tradable">{{ acc.environment }}</span>
                  <span>cash {{ money(acc.cash, acc.currency) }} · equity {{ money(acc.equity, acc.currency) }}</span>
                  @if (!acc.tradable) { <em>Live account — listed only, never traded.</em> }
                </li>
              }
            </ul>
          }
        </section>

        <section aria-labelledby="bc-ticket-title">
          <h4 id="bc-ticket-title">Paper order ticket</h4>
          <form class="bc-form bc-ticket" (submit)="$event.preventDefault(); send()">
            <label>Account
              <select name="route" [ngModel]="ticket().route" (ngModelChange)="set('route', $event)">
                <option value="">Choose an account</option>
                @for (a of tradable(); track a.connection + '/' + a.id) { <option [value]="routeOf(a)">{{ a.broker }} · {{ a.connection || 'system' }} · {{ a.id }} ({{ a.currency }})</option> }
              </select>
            </label>
            <label>Symbol <input name="symbol" [ngModel]="ticket().symbol" (ngModelChange)="set('symbol', $event)" autocomplete="off" spellcheck="false" /></label>
            <label>Side
              <select name="side" [ngModel]="ticket().side" (ngModelChange)="set('side', $event)"><option value="BUY">Buy</option><option value="SELL">Sell</option></select>
            </label>
            <label>Type
              <select name="type" [ngModel]="ticket().type" (ngModelChange)="set('type', $event)">@for (t of orderTypes; track t) { <option [value]="t">{{ t }}</option> }</select>
            </label>
            <label>Time in force
              <select name="tif" [ngModel]="ticket().tif" (ngModelChange)="set('tif', $event)"><option value="DAY">Day</option><option value="GTC">Good till cancelled</option><option value="IOC">Immediate or cancel</option></select>
            </label>
            <label>Quantity <input name="quantity" inputmode="decimal" [ngModel]="ticket().quantity" (ngModelChange)="set('quantity', $event)" /></label>
            @if (showLimit()) { <label>Limit price <input name="limit" inputmode="decimal" [ngModel]="ticket().limitPrice" (ngModelChange)="set('limitPrice', $event)" /></label> }
            @if (showStop()) { <label>Stop price <input name="stop" inputmode="decimal" [ngModel]="ticket().stopPrice" (ngModelChange)="set('stopPrice', $event)" /></label> }
            @if (issues().length && touched()) { <ul class="bc-problems" role="alert">@for (i of issues(); track i) { <li>{{ i }}</li> }</ul> }
            @if (ticketError()) { <p class="bc-error" role="alert">{{ ticketError() }}</p> }
            <div><button type="submit" class="btn btn-sm btn-primary" [disabled]="busy()">Send paper order</button></div>
          </form>
        </section>

        <section aria-labelledby="bc-orders-title">
          <h4 id="bc-orders-title">Routed orders</h4>
          <button type="button" class="btn btn-sm btn-outline-secondary" [disabled]="busy()" (click)="loadOrders()">Reload</button>
          @if (!orders().length) { <p class="muted">No order has been routed to a broker yet.</p> }
          <ul class="bc-orders" aria-label="Routed orders">
            @for (o of orders(); track o.id) {
              <li>
                <span><strong>#{{ o.id }}</strong> {{ o.request.side }} {{ o.request.quantity }} {{ o.request.symbol }} <span class="muted">{{ o.broker }} · {{ o.connection || 'system' }} · {{ o.account }}</span></span>
                <span>{{ ledgerStatusLabel(o.status) }}@if (o.order) { · {{ o.order.status }} ({{ o.order.filled_quantity }}/{{ o.order.quantity }}) }@if (o.error) { · {{ o.error }} }</span>
                <span class="bc-actions">
                  <button type="button" class="btn btn-sm btn-outline-secondary" [disabled]="busy()" (click)="refresh(o)">Refresh #{{ o.id }}</button>
                  @if (canCancel(o)) { <button type="button" class="btn btn-sm btn-outline-danger" [disabled]="busy()" (click)="cancel(o)">Cancel #{{ o.id }}</button> }
                </span>
              </li>
            }
          </ul>
        </section>
      </section>
    }
  `,
  styles: [`
    .bc { display: grid; gap: 0.75rem; margin-top: 1.25rem; }
    .bc-safety { margin: 0; padding: 0.5rem 0.75rem; border: 1px solid var(--fs-border); border-radius: 0.5rem; font-size: 0.85rem; }
    .bc-card { border: 1px solid var(--fs-border); border-radius: 0.5rem; padding: 0.7rem 0.9rem; display: grid; gap: 0.5rem; }
    .bc-card header { display: flex; gap: 0.6rem; align-items: baseline; flex-wrap: wrap; }
    .bc-chip { font-size: 0.7rem; border: 1px solid var(--fs-border); border-radius: 999px; padding: 0 0.5rem; }
    .bc-chip-live { color: var(--fs-neg); border-color: var(--fs-neg); }
    .bc-form { display: grid; gap: 0.5rem; max-width: 34rem; }
    .bc-form label { display: grid; gap: 0.2rem; font-size: 0.8rem; min-width: 0; }
    .bc-form input, .bc-form select { min-width: 0; width: 100%; }
    .bc-connections, .bc-accounts, .bc-orders { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.4rem; }
    .bc-connections li, .bc-accounts li, .bc-orders li { display: flex; gap: 0.6rem; flex-wrap: wrap; align-items: baseline; justify-content: space-between; font-size: 0.85rem; }
    .bc-live { border-left: 3px solid var(--fs-neg); padding-left: 0.5rem; }
    .bc-saved { color: var(--fs-pos); }
    .bc-problems, .bc-error { margin: 0; color: var(--fs-neg); font-size: 0.85rem; }
    .bc-problems { padding-left: 1.1rem; }
    .bc-actions { display: flex; gap: 0.4rem; flex-wrap: wrap; }
  `],
})
export class BrokerCenterComponent {
  private readonly api = inject(MarketService);

  readonly orderTypes = ORDER_TYPES;
  readonly money = money;
  readonly ledgerStatusLabel = ledgerStatusLabel;

  readonly view = signal<BrokersView | null>(null);
  readonly accounts = signal<BrokerAccountsView | null>(null);
  readonly orders = signal<BrokerLedgerRow[]>([]);
  readonly message = signal('');
  readonly busy = signal(false);
  readonly problem = signal<Record<string, string>>({});
  readonly ticketError = signal('');
  readonly touched = signal(false);
  readonly ticket = signal<Ticket>({ route: '', symbol: '', side: 'BUY', type: 'LIMIT', tif: 'DAY', quantity: '', limitPrice: '', stopPrice: '' });
  readonly issues = computed(() => ticketIssues(this.ticket()));
  readonly showLimit = computed(() => needsLimit(this.ticket().type));
  readonly showStop = computed(() => needsStop(this.ticket().type));
  readonly tradable = computed(() => (this.accounts()?.accounts ?? []).filter(a => a.tradable));

  // Typed values live only here until they are sent, and are cleared after.
  private readonly names = signal<Record<string, string>>({});
  private readonly values = signal<Record<string, Record<string, string>>>({});
  // One client order id per ticket: it is kept while the outcome of a send is unknown, so sending again is safe.
  private clientOrderId = newClientOrderId();

  constructor() {
    this.api.getBrokers().subscribe({
      // 403 and 404 mean the feature is not available to this account: show nothing.
      next: v => { this.view.set(v); this.loadOrders(); },
      error: (e: HttpErrorResponse) => { if (e.status >= 500 || e.status === 0) this.message.set('Brokers could not be loaded.'); },
    });
  }

  connectionsOf(b: BrokerEntry) { return (this.view()?.connections ?? []).filter(c => c.broker === b.provider_id); }
  hostNames(v: BrokersView): string { return v.trading_hosts.map(h => h.label).join(', '); }
  routeOf(a: BrokerAccount): string { return `${a.connection}/${a.id}`; }
  canCancel(o: BrokerLedgerRow): boolean { return !!o.order && ['ACCEPTED', 'OPEN', 'PARTIALLY_FILLED'].includes(o.order.status); }

  draftName(b: BrokerEntry): string { return this.names()[b.provider_id] ?? ''; }
  setName(b: BrokerEntry, v: string): void { this.names.update(all => ({ ...all, [b.provider_id]: v })); }
  draftValue(b: BrokerEntry, name: string): string { return this.values()[b.provider_id]?.[name] ?? ''; }
  setValue(b: BrokerEntry, name: string, v: string): void { this.values.update(all => ({ ...all, [b.provider_id]: { ...all[b.provider_id], [name]: v } })); }
  private setProblem(b: BrokerEntry, text: string): void { this.problem.update(all => ({ ...all, [b.provider_id]: text })); }

  set<K extends keyof Ticket>(key: K, value: Ticket[K]): void {
    this.ticket.update(t => ({ ...t, [key]: value }));
    // A changed ticket is a new order; only an unchanged one retries under the same id.
    this.clientOrderId = newClientOrderId();
  }

  save(b: BrokerEntry): void {
    const name = this.draftName(b).trim();
    const values = Object.fromEntries(Object.entries(this.values()[b.provider_id] ?? {}).map(([k, v]) => [k, v.trim()]).filter(([, v]) => v));
    if (!name) { this.setProblem(b, 'Name the connection.'); return; }
    this.setProblem(b, '');
    this.busy.set(true);
    this.api.saveBrokerConnection(name, { broker: b.provider_id, label: name, values }).subscribe({
      next: () => {
        this.values.update(all => ({ ...all, [b.provider_id]: {} }));
        this.names.update(all => ({ ...all, [b.provider_id]: '' }));
        this.busy.set(false); this.message.set(`Connection ${name} is saved.`); this.reloadView();
      },
      error: (e: HttpErrorResponse) => { this.busy.set(false); this.setProblem(b, e?.error?.error ?? 'The connection could not be saved.'); },
    });
  }

  remove(name: string): void {
    this.busy.set(true);
    this.api.deleteBrokerConnection(name).subscribe({
      next: () => { this.busy.set(false); this.message.set(`Connection ${name} is removed.`); this.accounts.set(null); this.reloadView(); },
      error: () => { this.busy.set(false); this.message.set('The connection could not be removed.'); },
    });
  }

  private reloadView(): void { this.api.getBrokers().subscribe({ next: v => this.view.set(v) }); }

  loadAccounts(): void {
    this.busy.set(true);
    this.api.getBrokerAccounts().subscribe({
      next: a => { this.accounts.set(a); this.busy.set(false); },
      error: () => { this.busy.set(false); this.message.set('Accounts could not be loaded.'); },
    });
  }

  loadOrders(): void {
    this.api.getBrokerOrders().subscribe({ next: r => this.orders.set(r.orders), error: () => undefined });
  }

  send(): void {
    this.touched.set(true);
    this.ticketError.set('');
    if (this.issues().length) return;
    const account = this.tradable().find(a => this.routeOf(a) === this.ticket().route);
    if (!account) { this.ticketError.set('Choose one of the paper accounts above.'); return; }
    this.busy.set(true);
    this.api.submitBrokerOrder(orderBody(this.ticket(), account.broker, this.clientOrderId)).subscribe({
      next: r => {
        this.busy.set(false);
        this.message.set(r.replayed ? `Order #${r.order.id} was already routed; nothing was sent again.` : `Order #${r.order.id} was sent to ${account.broker}.`);
        this.clientOrderId = newClientOrderId();
        this.ticket.update(t => ({ ...t, symbol: '', quantity: '', limitPrice: '', stopPrice: '' }));
        this.touched.set(false);
        this.loadOrders();
      },
      error: (e: HttpErrorResponse) => {
        this.busy.set(false);
        const unknown = outcomeUnknown(e.status, e.error);
        // An unknown outcome keeps the id, so pressing send again cannot place a second order.
        this.ticketError.set(e?.error?.error ?? 'The order could not be sent.');
        if (!unknown) this.clientOrderId = newClientOrderId();
        else this.loadOrders();
      },
    });
  }

  refresh(o: BrokerLedgerRow): void {
    this.busy.set(true);
    this.api.refreshBrokerOrder(o.id).subscribe({
      next: () => { this.busy.set(false); this.loadOrders(); },
      error: (e: HttpErrorResponse) => { this.busy.set(false); this.message.set(e?.error?.error ?? 'The order could not be refreshed.'); },
    });
  }

  cancel(o: BrokerLedgerRow): void {
    this.busy.set(true);
    this.api.cancelBrokerOrder(o.id).subscribe({
      next: () => { this.busy.set(false); this.message.set(`Order #${o.id} is cancelled.`); this.loadOrders(); },
      error: (e: HttpErrorResponse) => { this.busy.set(false); this.message.set(e?.error?.error ?? 'The order could not be cancelled.'); },
    });
  }
}
