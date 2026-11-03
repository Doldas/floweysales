import { Injectable, signal } from '@angular/core';

import { DEFAULT_LIMITS, type SavedTransactionFile, type SavedTransactionLimits } from '../components/saved-transactions';

/** The signed-in user's saved transaction exports. Content is never read back. */
@Injectable({ providedIn: 'root' })
export class SavedTransactions {
  readonly files = signal<SavedTransactionFile[]>([]);
  readonly limits = signal<SavedTransactionLimits>(DEFAULT_LIMITS);
  readonly loaded = signal(false);

  async refresh(): Promise<void> {
    try {
      const res = await fetch('/api/me/transactions', { credentials: 'same-origin', cache: 'no-store' });
      if (!res.ok) return;
      const data = (await res.json()) as { files?: SavedTransactionFile[]; limits?: SavedTransactionLimits };
      this.files.set(data.files ?? []);
      if (data.limits) this.limits.set(data.limits);
    } catch {
      /* keep the previous list */
    } finally {
      this.loaded.set(true);
    }
  }

  async save(name: string, content: string): Promise<void> {
    const res = await fetch('/api/me/transactions', {
      method: 'POST', credentials: 'same-origin', cache: 'no-store',
      headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name, content }),
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error((data as { error?: string }).error || `Request failed (${res.status})`);
    await this.refresh();
  }

  async remove(id: number): Promise<void> {
    const res = await fetch(`/api/me/transactions/${id}`, { method: 'DELETE', credentials: 'same-origin', cache: 'no-store' });
    if (!res.ok) {
      const data = await res.json().catch(() => ({}));
      throw new Error((data as { error?: string }).error || `Request failed (${res.status})`);
    }
    await this.refresh();
  }
}
