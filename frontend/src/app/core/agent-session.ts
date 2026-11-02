import { Injectable, computed, inject, signal } from '@angular/core';

import type { Message } from '../models';
import { MarketState } from './market-state';

export interface AiPreset {
  label: string;
  base_url: string;
  model: string;
  needs_key: boolean;
  local?: boolean;
}

interface AiConfig {
  base_url: string;
  model: string;
  configured: boolean;
  has_server_key: boolean;
  presets: AiPreset[];
  tools: string[];
  can_configure: boolean;
  max_steps: number;
  max_tool_calls: number;
}

/** One entry in the visible conversation. Tool entries are agent steps. */
export interface ChatEntry {
  id: number;
  role: 'user' | 'assistant' | 'tool';
  content: string;
  /** Tool name for agent steps. */
  name?: string;
  /** JSON arguments the agent passed to the tool. */
  args?: string;
  status?: 'running' | 'ok' | 'error';
  startedAt?: number;
  durationMs?: number;
}

/** Conversation context the agent receives alongside the transcript. */
export interface AgentContext {
  symbol: string;
  provider: string;
  range: string;
  price: string;
}

/**
 * AgentSession owns the research agent conversation. The dock and the full
 * Agent page render the same session, so a question asked anywhere in the app
 * continues wherever the user goes next.
 */
@Injectable({ providedIn: 'root' })
export class AgentSession {
  private readonly market = inject(MarketState);

  readonly entries = signal<ChatEntry[]>([]);
  readonly streaming = signal(false);
  readonly error = signal('');
  readonly configLoaded = signal(false);
  readonly configured = signal(false);
  readonly canConfigure = signal(false);
  readonly model = signal('');
  readonly chatModel = signal('');
  readonly baseUrl = signal('');
  readonly maxSteps = signal(12);
  readonly maxToolCalls = signal(48);
  readonly presets = signal<AiPreset[]>([]);
  readonly tools = signal<string[]>([]);
  readonly catalog = signal<{ id: number; model: string; label: string }[]>([]);
  /** Whether the floating dock is open. */
  readonly dockOpen = signal(false);

  readonly canSend = computed(() => !this.streaming() && this.configured() && !!this.model().trim());
  readonly stepCount = computed(() => this.entries().filter(e => e.role === 'tool').length);
  readonly thinking = computed(() => {
    if (!this.streaming()) return false;
    const last = this.entries().at(-1);
    return !last || last.role === 'user' || (last.role === 'tool' && last.status !== 'running');
  });

  readonly context = computed<AgentContext>(() => {
    const price = this.market.livePrice();
    return {
      symbol: this.market.trackedSymbol(),
      provider: this.market.provider(),
      range: this.market.historyRange(),
      price: price > 0 ? price.toFixed(4) : '',
    };
  });

  private abort: AbortController | null = null;
  private assistantId = -1;
  private nextId = 1;
  /** Full wire transcript; tool calls and results must survive follow-up prompts. */
  private wire: Message[] = [];

  constructor() {
    try {
      // Purge legacy browser-stored credentials; they belong on the server.
      localStorage.removeItem('ai_chat_config');
      localStorage.removeItem('ai_system_prompt');
    } catch {}
  }

  async loadConfig(force = false): Promise<void> {
    if (this.configLoaded() && !force) return;
    try {
      const res = await fetch('/api/ai/config', { credentials: 'same-origin' });
      if (!res.ok) return;
      const cfg: AiConfig = await res.json();
      this.canConfigure.set(!!cfg.can_configure);
      this.configured.set(!!cfg.configured);
      this.presets.set(cfg.presets || []);
      this.tools.set(cfg.tools || []);
      this.model.set(cfg.model || '');
      if (!this.chatModel()) this.chatModel.set(cfg.model || '');
      this.baseUrl.set(cfg.base_url || '');
      this.maxSteps.set(cfg.max_steps || 12);
      this.maxToolCalls.set(cfg.max_tool_calls || 48);
      this.configLoaded.set(true);
      if (cfg.can_configure) void this.loadCatalog();
    } catch {}
  }

  async loadCatalog(): Promise<void> {
    try {
      const res = await fetch('/api/admin/ai/models', { credentials: 'same-origin' });
      if (!res.ok) return;
      const data = await res.json();
      this.catalog.set(data.models || []);
    } catch {}
  }

  openDock(): void { this.dockOpen.set(true); }
  closeDock(): void { this.dockOpen.set(false); }
  toggleDock(): void { this.dockOpen.update(v => !v); }

  /** Opens the dock and asks a question in one step (used by quick actions). */
  ask(prompt: string): void {
    this.openDock();
    void this.send(prompt);
  }

  clear(): void {
    this.stop();
    this.wire = [];
    this.entries.set([]);
    this.error.set('');
    this.assistantId = -1;
  }

  stop(): void {
    this.abort?.abort();
    this.abort = null;
  }

  async send(text: string): Promise<void> {
    const content = (text || '').trim();
    if (!content) return;
    await this.loadConfig();
    if (!this.canSend()) {
      if (!this.configured()) this.error.set('The AI model is not configured yet. A SYS_ADMIN can set it up under Settings.');
      return;
    }

    this.error.set('');
    this.push({ role: 'user', content });
    this.wire.push({ role: 'user', content });
    this.streaming.set(true);
    this.assistantId = -1;

    const controller = new AbortController();
    this.abort = controller;
    const ctx = this.context();

    try {
      const res = await fetch('/api/ai/chat', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        signal: controller.signal,
        body: JSON.stringify({
          messages: this.wire.map(m => ({ ...m })),
          model: this.canConfigure() ? this.chatModel() : undefined,
          symbol: ctx.symbol,
          provider: ctx.provider,
          range: ctx.range,
          price: ctx.price,
        }),
      });
      if (!res.ok || !res.body) {
        let detail = `HTTP ${res.status}`;
        try {
          const body = await res.json();
          if (body?.error) detail = body.error;
        } catch {}
        throw new Error(detail);
      }
      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';
      for (;;) {
        const { value, done } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        let sep: number;
        while ((sep = buffer.indexOf('\n\n')) >= 0) {
          this.handleChunk(buffer.slice(0, sep));
          buffer = buffer.slice(sep + 2);
        }
      }
    } catch (err) {
      if ((err as Error)?.name !== 'AbortError') {
        const msg = (err as Error)?.message || 'Request failed';
        this.error.set(msg);
      }
    } finally {
      // Any step still marked running was interrupted.
      this.entries.update(list => list.map(e => (e.status === 'running' ? { ...e, status: 'error', content: e.content || 'Interrupted' } : e)));
      this.streaming.set(false);
      this.abort = null;
    }
  }

  async saveSettings(): Promise<void> {
    if (!this.canConfigure()) return;
    if (!this.baseUrl().trim() || !this.model().trim()) throw new Error('Base URL and model are required');
    const result = await fetch('/api/ai/config', {
      method: 'PUT',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ base_url: this.baseUrl().trim(), model: this.model().trim(), max_steps: this.maxSteps(), max_tool_calls: this.maxToolCalls() }),
    });
    if (!result.ok) {
      const error = await result.json().catch(() => null);
      throw new Error(error?.error || 'Configuration failed (HTTP ' + result.status + ')');
    }
    this.configured.set(true);
  }

  private handleChunk(chunk: string): void {
    for (const rawLine of chunk.split('\n')) {
      const line = rawLine.trim();
      if (!line.startsWith('data:')) continue;
      const payload = line.slice(5).trim();
      if (!payload) continue;
      let ev: { type?: string; content?: string; name?: string; arguments?: string; result?: string; tool_error?: string; error?: string; message?: Message };
      try {
        ev = JSON.parse(payload);
      } catch {
        continue;
      }
      switch (ev.type) {
        case 'transcript_message':
          if (ev.message && (ev.message.role === 'assistant' || ev.message.role === 'tool')) this.wire.push(ev.message);
          break;
        case 'delta':
          this.appendAssistant(ev.content || '');
          break;
        case 'tool_start':
          this.push({ role: 'tool', name: ev.name, args: ev.arguments, content: '', status: 'running', startedAt: Date.now() });
          this.assistantId = -1;
          break;
        case 'tool_result':
          this.finishStep(ev.name || '', ev.tool_error ? `Error: ${ev.tool_error}` : ev.result || 'No result', !ev.tool_error);
          this.assistantId = -1;
          break;
        case 'error':
          this.error.set(ev.error || 'AI error');
          break;
        case 'notice':
          // The backend explains why a reply was cut off (a full context window, say).
          if (ev.content) this.error.set(ev.content);
          break;
      }
    }
  }

  private push(entry: Omit<ChatEntry, 'id'>): number {
    const id = this.nextId++;
    this.entries.update(list => [...list, { id, ...entry }]);
    return id;
  }

  private finishStep(name: string, result: string, ok: boolean): void {
    this.entries.update(list => {
      const copy = list.slice();
      for (let i = copy.length - 1; i >= 0; i--) {
        const e = copy[i];
        if (e.role === 'tool' && e.status === 'running' && (!name || e.name === name)) {
          copy[i] = { ...e, content: result, status: ok ? 'ok' : 'error', durationMs: e.startedAt ? Date.now() - e.startedAt : undefined };
          return copy;
        }
      }
      return [...copy, { id: this.nextId++, role: 'tool', name, content: result, status: ok ? 'ok' : 'error' }];
    });
  }

  private appendAssistant(delta: string): void {
    if (this.assistantId < 0) {
      this.assistantId = this.push({ role: 'assistant', content: delta });
      return;
    }
    const id = this.assistantId;
    this.entries.update(list => list.map(e => (e.id === id ? { ...e, content: e.content + delta } : e)));
  }
}
