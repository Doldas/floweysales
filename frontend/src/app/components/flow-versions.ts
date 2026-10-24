/**
 * Pure helpers for published flow and function revisions.
 * Kept free of Angular dependencies so they can be tested without a DOM or TestBed.
 */

/** Publish state the API reports with every flow and function draft. */
export interface PublishState {
  active_revision?: number | null;
  latest_revision?: number;
  has_unpublished_changes?: boolean;
}

/** One entry of a `/revisions` listing. */
export interface RevisionSummary {
  revision: number;
  name: string;
  note?: string;
  published_by: number;
  published_at: string;
  active: boolean;
}

/**
 * Describes which graph runs and unpinned calls use.
 *
 * - Never published: the draft runs.
 * - Published: names the active revision, whether the draft differs, and
 *   the latest revision when an older one is active after a rollback.
 */
export function publishStatusLabel(state: PublishState): string {
  const active = state.active_revision ?? null;
  if (active === null) return 'Not published · runs use the draft';
  const latest = state.latest_revision ?? active;
  const rolledBack = latest > active ? ` (latest is ${latest})` : '';
  const draft = state.has_unpublished_changes ? 'draft has unpublished changes' : 'draft matches';
  return `Active: revision ${active}${rolledBack} · ${draft}`;
}

/**
 * Publishing needs saved edits and something new to freeze: either the
 * definition was never published, or the draft differs from the active revision.
 */
export function canPublish(state: PublishState, hasUnsavedChanges: boolean): boolean {
  if (hasUnsavedChanges) return false;
  return (state.active_revision ?? null) === null || !!state.has_unpublished_changes;
}

/** Short label for the revision a run executed; `null` means the draft ran. */
export function runRevisionLabel(revision: number | null | undefined): string {
  return revision ? `rev ${revision}` : 'draft';
}

/** Option label for a revision in a call node's pin selector. */
export function revisionOptionLabel(revision: RevisionSummary): string {
  const active = revision.active ? ' (active)' : '';
  const note = revision.note ? ` — ${revision.note}` : '';
  return `Revision ${revision.revision}${active}${note}`;
}

/** Describes what an unpinned (0) or pinned call node executes. */
export function pinDescription(pinned: number | null | undefined): string {
  return pinned ? `Pinned to revision ${pinned}; later publishes do not change this call.` :
    'Follows the target\'s active revision (its draft until first published).';
}
