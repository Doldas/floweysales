/**
 * Pure helpers for Settings → Updates.
 * Kept free of Angular dependencies so they can be tested without a DOM or TestBed.
 */

/** Host updater states, in the order scripts/auto-update.sh moves through them. */
export const UPDATE_STEPS = [
  { state: 'pulling', label: 'Download images' },
  { state: 'backing_up', label: 'Back up database' },
  { state: 'migrating', label: 'Run database migrations' },
  { state: 'restarting', label: 'Restart application' },
  { state: 'verifying', label: 'Health check' },
] as const;

const RUNNING = new Set(['queued', ...UPDATE_STEPS.map(s => s.state)]);

/** True while an update is queued or being applied. */
export function isUpdateRunning(state: string | undefined): boolean {
  return !!state && RUNNING.has(state);
}

/** First 12 characters of a commit, or a placeholder for unknown builds. */
export function shortCommit(commit: string | undefined | null): string {
  return commit ? commit.slice(0, 12) : 'unknown';
}

/** Headline for the outcome of an update check. */
export function comparisonLabel(comparison: string | undefined): string {
  switch (comparison) {
    case 'up_to_date': return 'You are running the latest version.';
    case 'update_available': return 'An update is available.';
    case 'newer_than_latest': return 'This build is newer than the published image; nothing to install.';
    case 'unknown_current': return 'This build has no version information (built before update tracking). Installing the latest image enables version checks.';
    case 'unknown_latest': return 'The published image has no version information, so it cannot be compared. You can still install it.';
    case 'check_failed': return 'The update check failed.';
    default: return 'Not checked yet.';
  }
}

/** Human description of a host updater state. */
export function stateLabel(state: string | undefined): string {
  switch (state) {
    case 'queued': return 'Queued: waiting for the host updater';
    case 'succeeded': return 'Update installed';
    case 'up_to_date': return 'Already up to date';
    case 'failed': return 'Update failed';
    case 'rolled_back': return 'Update rolled back';
    case 'idle': case undefined: case '': return 'No update has run yet';
  }
  return UPDATE_STEPS.find(s => s.state === state)?.label ?? state;
}

export type StepProgress = 'done' | 'active' | 'pending' | 'failed';

/**
 * Progress of each step for a state. Final success marks every step done;
 * a failure marks the steps after the last known one as pending.
 */
export function stepProgress(state: string | undefined): { label: string; progress: StepProgress }[] {
  const index = UPDATE_STEPS.findIndex(s => s.state === state);
  return UPDATE_STEPS.map((step, i) => {
    let progress: StepProgress = 'pending';
    if (state === 'succeeded') progress = 'done';
    else if (index >= 0) progress = i < index ? 'done' : i === index ? 'active' : 'pending';
    return { label: step.label, progress };
  });
}
