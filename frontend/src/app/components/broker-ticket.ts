/**
 * Pure helpers of the broker order ticket (#444). Free of Angular so they run
 * under node --test. The server checks everything again; this only saves the
 * round trip and says what is wrong in plain words.
 */

export type OrderType = 'MARKET' | 'LIMIT' | 'STOP' | 'STOP_LIMIT';

export interface Ticket {
  /** "connection/account" of a tradable account, or empty. */
  route: string;
  symbol: string;
  side: 'BUY' | 'SELL';
  type: OrderType;
  tif: 'DAY' | 'GTC' | 'IOC';
  quantity: string;
  limitPrice: string;
  stopPrice: string;
}

export const ORDER_TYPES: readonly OrderType[] = ['MARKET', 'LIMIT', 'STOP', 'STOP_LIMIT'];

export function needsLimit(type: OrderType): boolean { return type === 'LIMIT' || type === 'STOP_LIMIT'; }
export function needsStop(type: OrderType): boolean { return type === 'STOP' || type === 'STOP_LIMIT'; }

/** A client order id that is unique and safe for the server's pattern. */
export function newClientOrderId(random: () => string = () => crypto.randomUUID()): string {
  return `ui-${random()}`.slice(0, 128);
}

function positive(value: string): number {
  const n = Number(value.trim());
  return Number.isFinite(n) && n > 0 ? n : 0;
}

/** What is missing or wrong in the ticket; empty when it can be sent. */
export function ticketIssues(t: Ticket): string[] {
  const issues: string[] = [];
  if (!t.route) issues.push('Choose the paper account the order is for.');
  if (!/^[A-Za-z0-9][A-Za-z0-9.\-_/:=^]{0,39}$/.test(t.symbol.trim())) issues.push('Enter the instrument symbol as the broker names it.');
  if (!positive(t.quantity)) issues.push('Enter a quantity greater than zero.');
  if (needsLimit(t.type) && !positive(t.limitPrice)) issues.push('This order type needs a limit price.');
  if (needsStop(t.type) && !positive(t.stopPrice)) issues.push('This order type needs a stop price.');
  return issues;
}

export interface OrderBody {
  connection: string;
  broker: string;
  account: string;
  client_order_id: string;
  symbol: string;
  side: string;
  type: string;
  tif: string;
  quantity: number;
  limit_price?: number;
  stop_price?: number;
}

/** The route is "connection/account"; the broker comes with the account. */
export function splitRoute(route: string): { connection: string; account: string } {
  const i = route.indexOf('/');
  return i < 0 ? { connection: '', account: route } : { connection: route.slice(0, i), account: route.slice(i + 1) };
}

/** The request body. Prices the type does not use are left out. */
export function orderBody(t: Ticket, broker: string, clientOrderId: string): OrderBody {
  const { connection, account } = splitRoute(t.route);
  const body: OrderBody = {
    connection, broker, account, client_order_id: clientOrderId, symbol: t.symbol.trim().toUpperCase(),
    side: t.side, type: t.type, tif: t.tif, quantity: positive(t.quantity),
  };
  if (needsLimit(t.type)) body.limit_price = positive(t.limitPrice);
  if (needsStop(t.type)) body.stop_price = positive(t.stopPrice);
  return body;
}

/** Whether a failed submission may have placed the order, so the same id must be sent again. */
export function outcomeUnknown(status: number, body: { status?: string } | null): boolean {
  return status === 0 || status === 504 || (status === 502 && body?.status === 'unknown');
}

const ORDER_STATUS: Record<string, string> = {
  PENDING: 'Being sent',
  SUBMITTED: 'Sent to the broker',
  FAILED: 'Refused by the broker',
  UNKNOWN: 'Outcome unknown — refresh to settle it',
};

/** The routing status of a ledger row, in words. */
export function ledgerStatusLabel(status: string): string { return ORDER_STATUS[status] ?? status; }

/** Amounts are shown per account in its own currency and never added up. */
export function money(value: number, currency: string): string {
  return `${new Intl.NumberFormat('en-US', { maximumFractionDigits: 2, minimumFractionDigits: 2 }).format(value)} ${currency}`;
}

/** When the broker was last asked about an order, in UTC; empty before the first check. */
export function checkedLabel(iso: string | undefined): string {
  if (!iso) return 'Not checked yet';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return 'Not checked yet';
  return `Checked ${d.toISOString().slice(0, 16).replace('T', ' ')} UTC`;
}

/** A moment in UTC, to the minute. */
export function utcStamp(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? '' : `${d.toISOString().slice(0, 16).replace('T', ' ')} UTC`;
}

const EVENT_LABEL: Record<string, string> = {
  routed: 'Routed to the broker',
  acknowledged: 'Broker acknowledged the order',
  refused: 'Broker refused the order',
  no_answer: 'No clear answer from the broker',
  settled: 'Found at the broker',
  given_up: 'Broker has no such order',
  update: 'Broker reported a change',
  cancel: 'Cancellation answered by the broker',
};

/** A history step in words: what happened, the broker's status and the fills so far. */
export function eventLabel(ev: { kind: string; order_status?: string; filled_quantity?: number; detail?: string }): string {
  const parts = [EVENT_LABEL[ev.kind] ?? ev.kind];
  if (ev.order_status) parts.push(ev.order_status + (ev.filled_quantity ? ` (${ev.filled_quantity} filled)` : ''));
  if (ev.detail) parts.push(ev.detail);
  return parts.join(' · ');
}

/** The sentence under the kill switch notice: how many broker orders still work. */
export function workingNotice(halted: boolean, working: number): string {
  if (!working) return '';
  const orders = working === 1 ? '1 order is' : `${working} orders are`;
  return halted
    ? `The kill switch is on, which stops new orders. ${orders} still working at the broker; it does not cancel them.`
    : `${orders} working at the broker.`;
}
