import {
  Component,
  ElementRef,
  HostListener,
  OnDestroy,
  OnInit,
  ViewChild,
  computed,
  inject,
  signal,
  input,
} from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { DomSanitizer, SafeHtml } from '@angular/platform-browser';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';
import { MatDividerModule } from '@angular/material/divider';
import { MatProgressSpinnerModule } from '@angular/material/progress-spinner';

import { Message } from '../models';
import { formatToolOutput, renderMarkdown } from '../markdown';

interface AiPreset {
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
}

/**
 * AiChatComponent is a floating assistant that talks to the backend's
 * OpenAI-compatible agent endpoint (POST /api/ai/chat) and renders the SSE
 * stream (deltas, tool calls, errors) as a chat conversation.
 */
@Component({
  selector: 'fse-ai-chat',
  templateUrl: './ai-chat.html',
  styleUrl: './ai-chat.scss',
  standalone: true,
  imports: [
    CommonModule,
    FormsModule,
    MatButtonModule,
    MatIconModule,
    MatDividerModule,
    MatProgressSpinnerModule,
  ],
})
export class AiChatComponent implements OnInit, OnDestroy {
  @ViewChild('messagesContainer') messagesContainer?: ElementRef<HTMLElement>;
  @ViewChild('composer') composer?: ElementRef<HTMLTextAreaElement>;

  private readonly abort = signal<AbortController | null>(null);
  private readonly sanitizer = inject(DomSanitizer);

  /** Cached rendered HTML so streaming deltas are only re-rendered when needed. */
  private readonly mdCache = new Map<string, SafeHtml>();

  readonly messages = signal<Message[]>([]);
  readonly isStreaming = signal(false);
  /** Starts collapsed so the floating panel never covers page controls. */
  readonly isMinimized = signal(true);
  readonly showSettings = signal(false);
  readonly systemPrompt = signal('');
  readonly connectionError = signal('');

  readonly baseUrl = signal('');
  readonly apiKey = signal('');
  readonly model = signal('');
  readonly presets = signal<AiPreset[]>([]);
  readonly serverHasKey = signal(false);

  inputText = '';

  readonly quickPrompts = [
    'Show me the current price action',
    'Technical analysis for GIB',
    'Quarterly financials for MSFT',
    'What are the market stats?',
    'Show the most recent ticks',
    'Compare EMA 9 vs EMA 21',
  ];

  readonly canSend = computed(() => !this.isStreaming() && !!this.model().trim() && !!this.baseUrl().trim());

  readonly showThinking = computed(() => {
    if (!this.isStreaming()) return false;
    const msgs = this.messages();
    const last = msgs[msgs.length - 1];
    return !last || last.role !== 'assistant';
  });

  /** UI context from the parent (tracked symbol, provider, range, price). */
  readonly contextSymbol = input<string>('');
  readonly contextProvider = input<string>('');
  readonly contextRange = input<string>('');
  readonly contextPrice = input<string | null>(null);

  private assistantIndex = -1;
  /** Full wire transcript; tool calls and results must survive follow-up prompts. */
  private wireMessages: Message[] = [];

  ngOnInit(): void {
    try {
      const stored = localStorage.getItem('ai_chat_config');
      if (stored) {
        const cfg = JSON.parse(stored);
        this.baseUrl.set(cfg.baseUrl || '');
        this.apiKey.set(cfg.apiKey || '');
        this.model.set(cfg.model || '');
      }
      this.systemPrompt.set(localStorage.getItem('ai_system_prompt') || '');
      this.isMinimized.set(localStorage.getItem('ai_panel_open') !== '1');
    } catch {}

    void this.loadConfig();
  }

  ngOnDestroy(): void {
    this.abort()?.abort();
    this.persistConfig();
  }

  private async loadConfig(): Promise<void> {
    try {
      const res = await fetch('/api/ai/config');
      if (!res.ok) return;
      const cfg: AiConfig = await res.json();
      this.presets.set(cfg.presets || []);
      this.serverHasKey.set(!!cfg.has_server_key);
      if (!this.model().trim() && cfg.model) this.model.set(cfg.model);
      if (!this.baseUrl().trim() && cfg.base_url) this.baseUrl.set(cfg.base_url);
    } catch {}
  }

  private persistConfig(): void {
    try {
      localStorage.setItem('ai_chat_config', JSON.stringify({
        baseUrl: this.baseUrl().trim(),
        apiKey: this.apiKey().trim(),
        model: this.model().trim(),
      }));
      localStorage.setItem('ai_system_prompt', this.systemPrompt());
    } catch {}
  }

  // ---- UI actions ----

  toggleMinimize(): void {
    const next = !this.isMinimized();
    this.isMinimized.set(next);
    try {
      localStorage.setItem('ai_panel_open', next ? '0' : '1');
    } catch {}
  }

  openSettings(): void {
    this.showSettings.set(true);
  }

  closeSettings(): void {
    this.showSettings.set(false);
  }

  saveSettings(): void {
    if (!this.baseUrl().trim()) {
      this.flashError('Base URL is required');
      return;
    }
    if (!this.model().trim()) {
      this.flashError('Model is required');
      return;
    }
    this.persistConfig();
    this.showSettings.set(false);
  }

  selectPreset(p: AiPreset): void {
    this.baseUrl.set(p.base_url);
    this.model.set(p.model);
  }

  clearChat(): void {
    this.wireMessages = [];
    this.messages.set([]);
    this.assistantIndex = -1;
  }

  onSuggestionClick(prompt: string): void {
    void this.sendMessage(prompt);
  }

  onInputKeydown(event: KeyboardEvent): void {
    if (event.key === 'Tab') {
      this.onComposerKeydown(event);
      return;
    }
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault();
      void this.sendMessage(this.inputText);
    }
  }

  // ---- Chat ----

  async sendMessage(text: string): Promise<void> {
    const content = (text || '').trim();
    if (!content || this.isStreaming() || !this.canSend()) return;

    this.inputText = '';
    setTimeout(() => this.onInput(this.composer?.nativeElement));
    this.connectionError.set('');
    this.appendMessage({ role: 'user', content });
    this.wireMessages.push({ role: 'user', content });
    this.isStreaming.set(true);
    this.assistantIndex = -1;

    const history = this.wireMessages.map((m) => ({ ...m }));

    const controller = new AbortController();
    this.abort.set(controller);

    try {
      const res = await fetch('/api/ai/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        signal: controller.signal,
        body: JSON.stringify({
          messages: history,
          base_url: this.baseUrl().trim(),
          api_key: this.apiKey().trim(),
          model: this.model().trim(),
          system_prompt: this.systemPrompt().trim(),
          // Pass UI context so the agent knows what the user is looking at.
          symbol: this.contextSymbol(),
          provider: this.contextProvider(),
          range: this.contextRange(),
          price: this.contextPrice() ?? '',
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
          const chunk = buffer.slice(0, sep);
          buffer = buffer.slice(sep + 2);
          this.handleChunk(chunk);
        }
      }
    } catch (err: any) {
      if (err?.name !== 'AbortError') {
        const msg = err?.message || 'Request failed';
        this.connectionError.set(msg);
        this.upsertAssistant(`⚠️ ${msg}`);
      }
    } finally {
      this.isStreaming.set(false);
      this.abort.set(null);
    }
  }

  private handleChunk(chunk: string): void {
    for (const rawLine of chunk.split('\n')) {
      const line = rawLine.trim();
      if (!line.startsWith('data:')) continue;
      const payload = line.slice(5).trim();
      if (!payload) continue;
      let ev: any;
      try {
        ev = JSON.parse(payload);
      } catch {
        continue;
      }
      switch (ev.type) {
        case 'transcript_message':
          if (ev.message && ['assistant', 'tool'].includes(ev.message.role)) {
            this.wireMessages.push(ev.message as Message);
          }
          break;
        case 'delta':
          this.upsertAssistant(ev.content || '');
          break;
        case 'tool_start':
          this.appendMessage({ role: 'tool', name: ev.name, content: `Running ${ev.name}…` });
          break;
        case 'tool_result':
          this.appendMessage({
            role: 'tool',
            name: ev.name,
            content: ev.tool_error ? `Error: ${ev.tool_error}` : (ev.result || 'No result'),
          });
          this.assistantIndex = -1;
          break;
        case 'error':
          this.connectionError.set(ev.error || 'AI error');
          this.upsertAssistant(`⚠️ ${ev.error || 'AI error'}`);
          break;
        case 'done':
          break;
      }
    }
  }

  private appendMessage(msg: Message): void {
    this.messages.set([...this.messages(), msg]);
    this.scrollToBottom();
  }

  /** Appends streamed text to the current assistant bubble, creating it lazily. */
  private upsertAssistant(delta: string): void {
    const msgs = this.messages();
    if (this.assistantIndex < 0 || this.assistantIndex >= msgs.length) {
      this.assistantIndex = msgs.length;
      this.messages.set([...msgs, { role: 'assistant', content: delta }]);
    } else {
      const copy = msgs.slice();
      const current = copy[this.assistantIndex];
      copy[this.assistantIndex] = { ...current, content: (current.content ?? '') + delta };
      this.messages.set(copy);
    }
    this.scrollToBottom();
  }

  private flashError(msg: string): void {
    this.connectionError.set(msg);
    setTimeout(() => {
      if (this.connectionError() === msg) this.connectionError.set('');
    }, 3000);
  }

  trackByIndex(index: number): number {
    return index;
  }

  /** Renders a chat message as markdown (cached per content). */
  md(content?: string): SafeHtml {
    const key = content ?? '';
    if (!key) return '';
    const cached = this.mdCache.get(key);
    if (cached) return cached;
    const rendered = this.sanitizer.bypassSecurityTrustHtml(renderMarkdown(key));
    if (this.mdCache.size > 400) this.mdCache.clear();
    this.mdCache.set(key, rendered);
    return rendered;
  }

  /** Pretty-prints JSON tool payloads for readability. */
  toolOutput(content?: string): string {
    return formatToolOutput(content);
  }

  /** Copy button inside rendered markdown code blocks. */
  @HostListener('click', ['$event'])
  onCopyClick(event: Event): void {
    const target = (event.target as HTMLElement | null)?.closest<HTMLButtonElement>('[data-md-copy]');
    if (!target) return;
    const code = target.closest('.md-code')?.querySelector('code')?.textContent ?? '';
    const flash = (label: string) => {
      target.textContent = label;
      setTimeout(() => (target.textContent = 'copy'), 1500);
    };
    this.copyText(code)
      .then(() => flash('copied'))
      .catch(() => flash('press ⌘C'));
  }

  /** Clipboard API with a textarea/execCommand fallback for non-secure origins. */
  private copyText(text: string): Promise<void> {
    if (navigator.clipboard?.writeText) {
      return navigator.clipboard.writeText(text);
    }
    return new Promise((resolve, reject) => {
      const ta = document.createElement('textarea');
      ta.value = text;
      ta.style.position = 'fixed';
      ta.style.opacity = '0';
      document.body.appendChild(ta);
      ta.select();
      const ok = document.execCommand('copy');
      ta.remove();
      ok ? resolve() : reject(new Error('copy failed'));
    });
  }

  /** Keeps the message textarea sized to its content (up to ~5 lines). */
  onInput(input?: HTMLTextAreaElement): void {
    if (!input) return;
    input.style.height = 'auto';
    input.style.height = Math.min(input.scrollHeight, 140) + 'px';
  }

  /** Tab inserts two spaces instead of moving focus away from the composer. */
  onComposerKeydown(event: KeyboardEvent): void {
    if (event.key !== 'Tab') return;
    event.preventDefault();
    const el = event.target as HTMLTextAreaElement | null;
    const start = el?.selectionStart ?? this.inputText.length;
    const end = el?.selectionEnd ?? start;
    this.inputText = this.inputText.slice(0, start) + '  ' + this.inputText.slice(end);
    queueMicrotask(() => {
      if (el) {
        el.selectionStart = el.selectionEnd = start + 2;
      }
    });
  }

  scrollToBottom(): void {
    queueMicrotask(() => {
      const el = this.messagesContainer?.nativeElement;
      if (el) el.scrollTop = el.scrollHeight;
    });
  }
}
