// Pure helpers for the saved transaction exports (Tax tab). The limits mirror
// internal/storage/transaction_files.go; the server enforces them again.

export interface SavedTransactionFile { id: number; name: string; size_bytes: number; created_at: string }
export interface SavedTransactionLimits { max_files: number; max_file_bytes: number; max_total_bytes: number }

export const DEFAULT_LIMITS: SavedTransactionLimits = { max_files: 10, max_file_bytes: 2 << 20, max_total_bytes: 3 << 20 };

/** Why a file cannot be saved, or '' when it can. A file with a saved name replaces that one. */
export function uploadProblem(name: string, size: number, saved: SavedTransactionFile[], limits: SavedTransactionLimits = DEFAULT_LIMITS): string {
  if (!size) return `${name} is empty.`;
  if (size > limits.max_file_bytes) return `${name} is larger than ${(limits.max_file_bytes / (1 << 20)).toFixed(0)} MB.`;
  const others = saved.filter(f => f.name !== name);
  if (others.length >= limits.max_files) return `You can save at most ${limits.max_files} files. Delete one first.`;
  if (others.reduce((sum, f) => sum + f.size_bytes, 0) + size > limits.max_total_bytes) {
    return `Saved files may total ${(limits.max_total_bytes / (1 << 20)).toFixed(0)} MB. Delete one first.`;
  }
  return '';
}

/** How much of the total allowance the saved files use, as a percentage. */
export function usedPercent(saved: SavedTransactionFile[], limits: SavedTransactionLimits = DEFAULT_LIMITS): number {
  const used = saved.reduce((sum, f) => sum + f.size_bytes, 0);
  return Math.min(100, Math.round((used / limits.max_total_bytes) * 100));
}
