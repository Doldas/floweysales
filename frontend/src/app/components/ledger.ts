import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  inject,
  input,
  output,
  signal,
} from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { MatCardModule } from '@angular/material/card';
import { MatButtonModule } from '@angular/material/button';
import { MatButtonToggleModule } from '@angular/material/button-toggle';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatProgressSpinnerModule } from '@angular/material/progress-spinner';
import { MatSelectModule } from '@angular/material/select';
import { MatSnackBar, MatSnackBarModule } from '@angular/material/snack-bar';

import { MarketService, describeHttpError } from '../services';
import { OrdersState } from '../orders-state';
import { Order, OrderSide, OrderTif, OrderType, Position } from '../models';

type BlotterTab = 'positions' | 'open' | 'history';

interface SummaryTile {
  label: string;
  value: string;
  cls: string;
}

const EPSILON = 1e-9;

/**
 * Trading desk: an order ticket with pre-trade checks and a confirmation
 * step, plus the blotter (positions, working orders, execution history).
 */
@Component({
  selector: 'fse-ledger',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    CommonModule,
    FormsModule,
    MatCardModule,
    MatButtonModule,
    MatButtonToggleModule,
    MatFormFieldModule,
    MatIconModule,
    MatInputModule,
    MatProgressSpinnerModule,
    MatSelectModule,
    MatSnackBarModule,
  ],
  templateUrl: './ledger.html',
  styleUrl: './ledger.scss',
})
export class LedgerComponent {
  private readonly api = inject(MarketService);
  private readonly state = inject(OrdersState);
  private readonly snackBar = inject(MatSnackBar);

  /** Currently tracked symbol; changing it reloads the book. */
  readonly trackedSymbol = input.required<string>();
  /** Latest live price for the tracked symbol. */
  readonly livePrice = input<number>(0);
  readonly currency = input<string>('USD');

  readonly ordersChanged = output<void>();

  readonly orders = this.state.orders;
  readonly positions = this.state.positions;
  readonly summary = this.state.summary;

  readonly openOrders = computed(() => this.orders().filter((o) => o.status === 'OPEN'));
  readonly tab = signal<BlotterTab>('positions');
  readonly busy = signal(false);

  // --- order ticket state (plain fields so ngModel stays simple) ---
  side: OrderSide = 'BUY';
  orderType: OrderType = 'MARKET';
  tif: OrderTif = 'GTC';
  qty = '';
  limitPrice = '';
  stopPrice = '';

  /** compose = fill the ticket in, review = confirm the exact order. */
  step: 'compose' | 'review' = 'compose';
  serverError: string | null = null;

  readonly presets = [25, 50, 75, 100];

  /** Account summary tiles across the top of the card. */
  readonly tiles = computed<SummaryTile[]>(() => {
    const s = this.summary();
    const cur = this.currency();
    if (!s) {
      return [
        { label: 'Equity', value: '—', cls: '' },
        { label: 'Buying power', value: '—', cls: '' },
        { label: 'Market value', value: '—', cls: '' },
        { label: 'Unrealised P&L', value: '—', cls: '' },
        { label: 'Realised P&L', value: '—', cls: '' },
        { label: 'Open orders', value: '—', cls: '' },
      ];
    }
    return [
      { label: 'Equity', value: this.money(s.equity), cls: '' },
      { label: 'Buying power', value: this.money(s.cash), cls: '' },
      { label: 'Market value', value: this.money(s.market_value), cls: '' },
      {
        label: 'Unrealised P&L',
        value: `${this.signed(s.unrealized_pnl)} ${cur}`,
        cls: this.pnlClass(s.unrealized_pnl),
      },
      {
        label: 'Realised P&L',
        value: `${this.signed(s.realized_pnl)} ${cur}`,
        cls: this.pnlClass(s.realized_pnl),
      },
      { label: 'Open orders', value: String(s.open_order_count), cls: '' },
    ];
  });

  constructor() {
    effect(() => {
      const symbol = this.trackedSymbol();
      this.resetTicket();
      if (symbol) this.state.loadFor(symbol);
    });
  }

  // ---------------------------------------------------------------- views

  get symbolLabel(): string {
    return this.trackedSymbol() || '—';
  }

  /** Last traded mark for the tracked symbol. */
  mark(): number {
    const live = this.livePrice();
    if (live > 0) return live;
    return this.state.markOf(this.trackedSymbol());
  }

  held(): number {
    return this.state.heldQuantity(this.trackedSymbol());
  }

  tabCount(tab: BlotterTab): number {
    if (tab === 'positions') return this.positions().length;
    if (tab === 'open') return this.openOrders().length;
    return this.orders().length;
  }

  // --------------------------------------------------------------- ticket

  needsLimit(): boolean {
    return this.orderType === 'LIMIT' || this.orderType === 'STOP_LIMIT';
  }

  needsStop(): boolean {
    return this.orderType === 'STOP' || this.orderType === 'STOP_LIMIT';
  }

  private quantity(): number {
    const v = Number(this.qty);
    return Number.isFinite(v) ? v : 0;
  }

  private limit(): number {
    const v = Number(this.limitPrice);
    return Number.isFinite(v) && v > 0 ? v : 0;
  }

  private stop(): number {
    const v = Number(this.stopPrice);
    return Number.isFinite(v) && v > 0 ? v : 0;
  }

  private stopBreached(mark: number): boolean {
    const stop = this.stop();
    if (stop <= 0 || mark <= 0) return false;
    return this.side === 'BUY' ? mark >= stop : mark <= stop;
  }

  /** Price the order is expected to execute at (price improvement included). */
  executionPrice(): number {
    const mark = this.mark();
    const type = this.orderType;
    if (type === 'MARKET') return mark;

    if (type === 'STOP' || type === 'STOP_LIMIT') {
      const stop = this.stop();
      if (stop > 0 && !this.stopBreached(mark)) return stop;
      if (type === 'STOP') return mark > 0 ? mark : stop;
    }

    const limit = this.limit();
    if (limit <= 0) return mark > 0 ? mark : this.stop();
    if (mark <= 0) return limit;
    const marketable = this.side === 'BUY' ? mark <= limit : mark >= limit;
    return marketable ? mark : limit;
  }

  /** Worst case price used by the pre-trade risk check. */
  worstPrice(): number {
    const mark = this.mark();
    switch (this.orderType) {
      case 'LIMIT':
        return this.limit() > 0 ? this.limit() : mark;
      case 'STOP':
      case 'STOP_LIMIT':
        return this.stop() > 0 ? this.stop() : mark;
      default:
        return mark;
    }
  }

  private commission(): { pct: number; fixed: number } {
    const s = this.summary();
    return { pct: s?.commission_pct ?? 0, fixed: s?.commission_fixed ?? 0 };
  }

  feeFor(notional: number): number {
    const { pct, fixed } = this.commission();
    return Math.round((fixed + Math.abs(notional) * (pct / 100)) * 100) / 100;
  }

  /** Rows of the estimate block in the ticket. */
  estimate(): { label: string; value: string; strong?: boolean }[] {
    const qty = this.quantity();
    const fill = this.executionPrice();
    const cur = this.currency();
    const notional = qty * fill;
    const fee = this.feeFor(notional);
    const total = this.side === 'BUY' ? notional + fee : notional - fee;
    const summary = this.summary();
    const after = summary
      ? this.side === 'BUY'
        ? summary.buying_power - total
        : summary.buying_power + total
      : null;

    return [
      { label: 'Est. fill price', value: fill > 0 ? `${fill.toFixed(4)} ${cur}` : '—' },
      { label: 'Notional', value: qty > 0 && fill > 0 ? `${notional.toFixed(2)} ${cur}` : '—' },
      { label: 'Commission', value: `${fee.toFixed(2)} ${cur}` },
      {
        label: this.side === 'BUY' ? 'Estimated cost' : 'Estimated proceeds',
        value: qty > 0 && fill > 0 ? `${total.toFixed(2)} ${cur}` : '—',
        strong: true,
      },
      {
        label: this.side === 'BUY' ? 'Buying power after' : 'Buying power after',
        value: after !== null ? `${after.toFixed(2)} ${cur}` : '—',
      },
    ];
  }

  /** Pre-trade checks mirrored from the engine; null means the ticket is valid. */
  validationError(): string | null {
    const symbol = this.trackedSymbol();
    if (!symbol) return 'Track an asset before placing an order.';

    const qty = this.quantity();
    if (!(qty > 0)) return 'Enter a quantity greater than zero.';

    if (this.needsLimit() && this.limit() <= 0) return 'Enter a limit price.';
    if (this.needsStop() && this.stop() <= 0) return 'Enter a stop price.';

    if (this.orderType === 'MARKET' && this.mark() <= 0) {
      return `No market price for ${symbol} yet — wait for a tick before using a market order.`;
    }

    const worst = this.worstPrice();
    if (!(worst > 0)) return 'Enter a price greater than zero.';

    const notional = qty * worst;
    const cost = notional + this.feeFor(notional);
    const summary = this.summary();

    if (this.side === 'SELL') {
      const held = this.held();
      if (!summary?.allow_short && qty > held + EPSILON) {
        return `Insufficient position: holding ${this.qtyText(held)}, cannot sell ${this.qtyText(qty)}.`;
      }
    } else if (summary && cost > summary.buying_power + EPSILON) {
      return `Insufficient buying power: need ${cost.toFixed(2)} ${this.currency()}, available ${summary.buying_power.toFixed(2)} ${this.currency()}.`;
    }

    return null;
  }

  /** Largest quantity the account can take on the current side. */
  maxQuantity(): number {
    const price = this.worstPrice() > 0 ? this.worstPrice() : this.mark();
    if (price <= 0) return 0;
    if (this.side === 'SELL') return Math.max(0, this.held());
    const buyingPower = this.summary()?.buying_power ?? 0;
    const { pct, fixed } = this.commission();
    const perUnit = price * (1 + pct / 100);
    if (perUnit <= 0) return 0;
    return Math.max(0, Math.floor(((buyingPower - fixed) / perUnit) * 1e6) / 1e6);
  }

  applyPreset(pct: number): void {
    const target = Math.floor(this.maxQuantity() * (pct / 100) * 1e6) / 1e6;
    this.qty = target > 0 ? String(target) : '';
  }

  applyMax(): void {
    const max = this.maxQuantity();
    this.qty = max > 0 ? String(max) : '';
  }

  /** Order attributes shown on the confirmation step. */
  reviewRows(): { label: string; value: string }[] {
    const cur = this.currency();
    const limit = Number(this.limitPrice || 0).toFixed(4);
    const stop = Number(this.stopPrice || 0).toFixed(4);
    let pricing = 'Executes at the next available market price';
    if (this.orderType === 'LIMIT') pricing = `Limit ${limit} ${cur}`;
    else if (this.orderType === 'STOP') pricing = `Stop ${stop} ${cur}`;
    else if (this.orderType === 'STOP_LIMIT') pricing = `Stop ${stop} / limit ${limit} ${cur}`;

    return [
      { label: 'Symbol', value: this.symbolLabel },
      { label: 'Side', value: this.side },
      { label: 'Order type', value: this.orderType },
      { label: 'Time in force', value: this.tif },
      { label: 'Quantity', value: `${this.qty} units` },
      { label: 'Pricing', value: pricing },
      { label: 'Risk price', value: `${this.worstPrice().toFixed(4)} ${cur}` },
    ];
  }

  review(): void {
    const problem = this.validationError();
    if (problem) {
      this.serverError = problem;
      return;
    }
    this.serverError = null;
    this.step = 'review';
  }

  backToCompose(): void {
    this.step = 'compose';
    this.serverError = null;
  }

  submit(): void {
    const symbol = this.trackedSymbol();
    if (!symbol || this.busy()) return;

    const problem = this.validationError();
    if (problem) {
      this.serverError = problem;
      this.step = 'compose';
      return;
    }

    this.busy.set(true);
    this.serverError = null;

    this.api
      .submitOrder({
        symbol,
        side: this.side,
        type: this.orderType,
        tif: this.tif,
        quantity: this.quantity(),
        limit_price: this.needsLimit() ? this.limit() : undefined,
        stop_price: this.needsStop() ? this.stop() : undefined,
      })
      .subscribe({
        next: (res) => {
          this.busy.set(false);
          const order = res.order;
          const filled = order.status === 'FILLED';
          this.notify(
            filled
              ? `${order.side} ${this.qtyText(order.quantity)} ${symbol} filled at ${order.fill_price.toFixed(4)} (fee ${order.fee.toFixed(2)}).`
              : `${order.side} ${this.qtyText(order.quantity)} ${symbol} working as a ${order.type} order (#${order.id}).`,
            false,
          );
          this.resetTicket();
          this.state.loadFor(symbol);
          this.ordersChanged.emit();
        },
        error: (err) => {
          this.busy.set(false);
          this.serverError = describeHttpError(err);
          this.step = 'review';
        },
      });
  }

  // ------------------------------------------------------------ blotter

  cancel(order: Order): void {
    this.busy.set(true);
    this.api.cancelOrder(order.id, 'cancelled by user').subscribe({
      next: () => {
        this.busy.set(false);
        this.notify(`Order #${order.id} cancelled.`, false);
        this.refresh();
      },
      error: (err) => {
        this.busy.set(false);
        this.notify(`Cancel failed: ${describeHttpError(err)}`, true);
      },
    });
  }

  cancelAll(): void {
    const working = this.openOrders();
    if (!working.length || this.busy()) return;
    this.busy.set(true);

    let done = 0;
    let failed = 0;
    const finish = () => {
      this.busy.set(false);
      this.notify(
        failed ? `${done} cancelled, ${failed} failed.` : `${done} working orders cancelled.`,
        failed > 0,
      );
      this.refresh();
    };

    working.forEach((order) => {
      this.api.cancelOrder(order.id, 'cancel all').subscribe({
        next: () => {
          done++;
          if (done + failed === working.length) finish();
        },
        error: () => {
          failed++;
          if (done + failed === working.length) finish();
        },
      });
    });
  }

  placeFromPosition(position: Position, side: OrderSide): void {
    this.side = side;
    this.orderType = 'MARKET';
    this.step = 'compose';
    this.serverError = null;
    if (side === 'SELL') {
      this.qty = String(Math.max(0, position.quantity));
    }
  }

  // ------------------------------------------------------------- helpers

  trackById(index: number, item: { id: number }): number {
    return item.id;
  }

  trackBySymbol(index: number, item: { symbol: string }): string {
    return item.symbol;
  }

  money(value: number): string {
    return `${value.toFixed(2)} ${this.currency()}`;
  }

  signed(value: number): string {
    return `${value >= 0 ? '+' : ''}${value.toFixed(2)}`;
  }

  pnlClass(value: number): string {
    if (value > 0) return 'txt-green';
    if (value < 0) return 'txt-red';
    return 'txt-muted';
  }

  qtyText(value: number): string {
    return String(Number(value.toFixed(6)));
  }

  /** Mark used to value one position (tick driven, engine value as fallback). */
  lastOf(position: Position): number {
    return this.state.markOf(position.symbol, position.last_price);
  }

  positionPnl(position: Position): number {
    const mark = this.state.markOf(position.symbol, position.last_price);
    return mark * position.quantity - position.cost_basis;
  }

  positionPnlPct(position: Position): number {
    const cost = Math.abs(position.cost_basis);
    if (cost <= EPSILON) return 0;
    return (this.positionPnl(position) / cost) * 100;
  }

  priceOf(order: Order): number {
    return order.fill_price > 0 ? order.fill_price : order.price;
  }

  statusClass(status: string): string {
    switch (status) {
      case 'FILLED':
        return 'status-filled';
      case 'OPEN':
        return 'status-open';
      case 'CANCELLED':
        return 'status-cancelled';
      default:
        return 'status-rejected';
    }
  }

  orderPriceLabel(order: Order): string {
    if (order.type === 'MARKET') return 'Market';
    if (order.type === 'STOP') return `Stop ${order.stop_price.toFixed(4)}`;
    if (order.type === 'STOP_LIMIT')
      return `Stop ${order.stop_price.toFixed(4)} / Limit ${order.limit_price.toFixed(4)}`;
    return `Limit ${order.limit_price.toFixed(4)}`;
  }

  timeOf(order: Order): number {
    return (order.filled_at || order.timestamp) * 1000;
  }

  private resetTicket(): void {
    this.qty = '';
    this.limitPrice = '';
    this.stopPrice = '';
    this.step = 'compose';
    this.serverError = null;
    this.side = 'BUY';
    this.orderType = 'MARKET';
    this.tif = 'GTC';
  }

  private refresh(): void {
    const symbol = this.trackedSymbol();
    if (symbol) this.state.loadFor(symbol);
    else this.state.loadPortfolio();
    this.ordersChanged.emit();
  }

  private notify(message: string, error: boolean): void {
    this.snackBar.open(message, 'OK', {
      duration: 5000,
      panelClass: error ? ['snack-error'] : ['snack-ok'],
    });
  }

}
