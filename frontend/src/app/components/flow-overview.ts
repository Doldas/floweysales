/**
 * Pure helpers for the Flow Studio "My Flows" overview: search, status filter and sort.
 * Kept free of Angular dependencies so they can be tested without a DOM or TestBed.
 */

export interface OverviewFlow {
  id: number;
  name: string;
  enabled: boolean;
  interval_minutes: number;
  event_key: string;
}

/**
 * Returns the flows that match the search, status filter and sort order.
 *
 * - Search is trimmed and case-insensitive. It matches the flow name or its event key.
 *   A blank search matches every flow.
 * - Filter values: `all`, `enabled`, `disabled`, `scheduled` (interval > 0), `manual` (interval 0).
 *   An unrecognised filter value matches nothing.
 * - Sort values: `name` (A–Z), `name-desc` (Z–A), `newest` (highest id first), `oldest` (lowest id first).
 *   Any other value falls back to name A–Z.
 *
 * The input array is not mutated.
 */
export function listOverviewFlows<T extends OverviewFlow>(flows: readonly T[], search: string, filter: string, sort: string): T[] {
  const query = search.trim().toLowerCase();
  return flows
    .filter(flow => (!query || flow.name.toLowerCase().includes(query) || (flow.event_key || '').toLowerCase().includes(query)) && matchesFilter(flow, filter))
    .sort((a, b) => compareFlows(a, b, sort));
}

function matchesFilter(flow: OverviewFlow, filter: string): boolean {
  switch (filter) {
    case 'all': return true;
    case 'enabled': return flow.enabled;
    case 'disabled': return !flow.enabled;
    case 'scheduled': return flow.interval_minutes > 0;
    case 'manual': return flow.interval_minutes === 0;
    default: return false;
  }
}

function compareFlows(a: OverviewFlow, b: OverviewFlow, sort: string): number {
  switch (sort) {
    case 'newest': return b.id - a.id;
    case 'oldest': return a.id - b.id;
    case 'name-desc': return b.name.localeCompare(a.name);
    default: return a.name.localeCompare(b.name);
  }
}
