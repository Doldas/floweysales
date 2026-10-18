import { Injectable, computed, signal, inject } from '@angular/core';
import { MarketService } from './services';
import { Order, PortfolioSummary, Position } from './models';

/**
 * OrdersState is the single source of truth for the trading book: the order
 * history of the tracked symbol, the net positions, the account summary and
 * the last traded mark per symbol. The KPI cards and the ledger both read it.
 */
@Injectable({ providedIn: 'root' })
export class OrdersState {
  private readonly api = inject(MarketService);

  private readonly _orders = signal<Order[]>([]);
  private readonly _positions = signal<Position[]>([]);
  private readonly _summary = signal<PortfolioSummary | null>(null);
  private readonly _marks = signal<Record<string, number>>({});

  readonly orders = this._orders.asReadonly();
  readonly positions = this._positions.asReadonly();
  readonly summary = this._summary.asReadonly();

  /** Raw live price map kept for components that read it directly. */
  livePrices: Record<string, number> = {};

  /**
   * Total unrealised P&L. Marks from the tick stream win so the KPI moves
   * with the market; otherwise the last value the engine reported is used.
   */
  readonly totalPnL = computed(() => {
    const positions = this._positions();
    const marks = this._marks();
    if (!positions.length) return this._summary()?.unrealized_pnl ?? 0;

    let total = 0;
    for (const p of positions) {
      const mark = marks[p.symbol] ?? p.last_price;
      total += mark * p.quantity - p.cost_basis;
    }
    return Math.round(total * 100) / 100;
  });

  /** Loads the tracked symbol's orders and refreshes the portfolio. */
  loadFor(symbol: string): void {
    this.loadPortfolio();
    if (!symbol) {
      this._orders.set([]);
      return;
    }
    this.api.getOrders(symbol).subscribe({
      next: (orders) => this._orders.set(orders ?? []),
      error: () => this._orders.set([]),
    });
  }

  /** Reloads net positions and the account summary. */
  loadPortfolio(): void {
    this.api.getPortfolio().subscribe({
      next: (res) => {
        this._positions.set(res?.positions ?? []);
        this._summary.set(res?.summary ?? null);
      },
      error: () => {
        /* engine unreachable: keep the previous book */
      },
    });
  }

  /** Called for every tick so P&L stays live without extra requests. */
  updateMark(symbol: string, price: number): void {
    if (!symbol || !Number.isFinite(price) || price <= 0) return;
    this.livePrices[symbol] = price;
    const marks = this._marks();
    if (marks[symbol] === price) return;
    this._marks.set({ ...marks, [symbol]: price });
  }

  /** Latest mark for a symbol, falling back to the engine's own value. */
  markOf(symbol: string, fallback = 0): number {
    return this._marks()[symbol] ?? this.livePrices[symbol] ?? fallback;
  }

  /** Net quantity held in one symbol across every execution. */
  heldQuantity(symbol: string): number {
    return this._positions().find((p) => p.symbol === symbol)?.quantity ?? 0;
  }
}
