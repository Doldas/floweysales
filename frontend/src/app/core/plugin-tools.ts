import { Injectable, signal } from '@angular/core';

import { normalizeResult, type ToolPlugin, type ToolResult } from '../components/plugin-tools';

/** Plugins that contribute a tab of tools, and the call that runs one tool. */
@Injectable({ providedIn: 'root' })
export class PluginTools {
  readonly plugins = signal<ToolPlugin[]>([]);
  readonly loaded = signal(false);

  /** Reloads the list; a failure leaves the navigation as it was. */
  async refresh(): Promise<void> {
    try {
      const res = await fetch('/api/plugin-tools', { credentials: 'same-origin', cache: 'no-store' });
      if (!res.ok) return;
      const data = (await res.json()) as { plugins?: ToolPlugin[] };
      this.plugins.set(data.plugins ?? []);
    } catch {
      /* keep the previous list */
    } finally {
      this.loaded.set(true);
    }
  }

  /** Runs a tool. The input is sent once and kept nowhere. */
  async run(plugin: string, tool: string, input: Record<string, unknown>): Promise<ToolResult> {
    const res = await fetch(`/api/plugin-tools/${encodeURIComponent(plugin)}/${encodeURIComponent(tool)}`, {
      method: 'POST', credentials: 'same-origin', cache: 'no-store',
      headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ input }),
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error((data as { error?: string }).error || `Request failed (${res.status})`);
    return normalizeResult(data as ToolResult);
  }
}
