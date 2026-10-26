/**
 * Pure helpers for signed event adapters (inbound webhooks). Free of Angular
 * so they run under node --test.
 */

export interface EventAdapter {
  id: number; name: string; slug: string; event_key: string; payload_pointer: string;
  rate_per_minute: number; enabled: boolean; created_at: string; secret_rotated_at: string;
  last_delivery_at?: string; accepted: number; rejected: number;
}

export interface AdapterDelivery {
  id: number; delivery_id?: string; status: 'ACCEPTED' | 'REJECTED'; reason?: string;
  payload_sha256?: string; queued_runs: number; received_at: string;
}

export const deliveryPath = (a: Pick<EventAdapter, 'slug'>) => `/api/hooks/flow-events/${a.slug}`;

/** The problem with an adapter form, or '' when it can be saved. */
export function adapterProblem(name: string, eventKey: string, pointer: string, rate: number): string {
  if (!name.trim()) return 'Give the adapter a name';
  if (name.trim().length > 100) return 'Name is at most 100 characters';
  if (!eventKey) return 'Choose the event this adapter delivers';
  if (pointer && !pointer.startsWith('/')) return 'Payload pointer must start with / (for example /data/value), or be empty for the whole body';
  if (!Number.isInteger(rate) || rate < 1 || rate > 600) return 'Rate must be a whole number from 1 to 600 per minute';
  return '';
}

/** A copy-paste example of a signed delivery, so senders can verify their setup. */
export function curlExample(origin: string, a: Pick<EventAdapter, 'slug' | 'payload_pointer'>, secret = '$SECRET'): string {
  const body = examplePayload(a.payload_pointer);
  return [
    `BODY='${body}'`,
    `TS=$(date +%s); ID=$(uuidgen)`,
    `SIG=sha256=$(printf '%s.%s.%s' "$TS" "$ID" "$BODY" | openssl dgst -sha256 -hmac "${secret}" -hex | sed 's/^.* //')`,
    `curl -X POST '${origin}${deliveryPath(a)}' -H 'Content-Type: application/json' \\`,
    `  -H "X-Flowey-Timestamp: $TS" -H "X-Flowey-Delivery: $ID" -H "X-Flowey-Signature: $SIG" -d "$BODY"`,
  ].join('\n');
}

/** A JSON body that places "example" at the pointer. */
export function examplePayload(pointer: string): string {
  if (!pointer) return '"example"';
  const tokens = pointer.slice(1).split('/').map(t => t.replace(/~1/g, '/').replace(/~0/g, '~'));
  let value: unknown = 'example';
  for (const token of tokens.reverse()) value = /^(0|[1-9]\d*)$/.test(token) ? Object.assign(new Array(Number(token) + 1).fill(null), { [token]: value }) : { [token]: value };
  return JSON.stringify(value);
}

export function deliverySummary(d: AdapterDelivery): string {
  if (d.status === 'REJECTED') return `Rejected: ${d.reason || 'unknown reason'}`;
  return `Accepted ${d.delivery_id || ''} · queued ${d.queued_runs} run${d.queued_runs === 1 ? '' : 's'}`.replace('  ', ' ');
}
