import { ChangeDetectionStrategy, Component, ElementRef, HostListener, computed, effect, inject, input, signal, viewChild } from '@angular/core';
import { DomSanitizer, SafeHtml } from '@angular/platform-browser';

import { EVIDENCE_MODES, auditSummary, findingLabel, safeUrl, sourceOrigin, splitSources } from '../core/answer-sources';
import { AgentSession, type ChatEntry } from '../core/agent-session';
import { renderMarkdown } from '../markdown';
import { toolLabel } from '../components/agent-tools';
import { defaultOptionLabel, profileOption, profileSummary } from '../components/profile-view';
import { ToolCallComponent } from '../components/tool-call';
import { canResume, checklistLine, taskStatusLabel } from '../core/task-view';
import { formatUtcDateTime } from '../ui/datetime';
import type { ToolCall } from '../components/tool-call-view';

/** Consecutive tool calls are rendered together as one block of agent steps. */
type Block = { kind: 'user' | 'assistant'; entry: ChatEntry } | { kind: 'steps'; steps: ChatEntry[]; key: number };

/**
 * The research agent conversation: messages, the tool calls the agent made
 * (as an expandable step timeline) and the composer. Used by the dock and
 * by the full-page Agent view; both render the shared AgentSession.
 */
@Component({
  selector: 'fs-agent-chat',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ToolCallComponent],
  templateUrl: './agent-chat.html',
  styleUrl: './agent-chat.scss',
})
export class AgentChatComponent {
  readonly session = inject(AgentSession);
  readonly findingLabel = findingLabel;
  readonly evidenceModes = EVIDENCE_MODES;
  readonly evidenceHint = computed(() => this.session.evidenceMode() === 'standard' ? '' : EVIDENCE_MODES.find(m => m.id === this.session.evidenceMode())?.hint ?? '');
  readonly origin = sourceOrigin;
  readonly statusLabel = taskStatusLabel;
  readonly checklistLine = checklistLine;
  readonly canResume = canResume;
  readonly when = formatUtcDateTime;
  readonly mark = (status: string): string => (status === 'done' ? 'bi-check-circle-fill' : status === 'missing' ? 'bi-x-circle-fill' : 'bi-circle');
  readonly groups = computed(() => splitSources(this.session.answerSources()));
  readonly auditText = computed(() => auditSummary(this.session.answerAudit()));
  link(url: string | undefined): string { return safeUrl(url); }
  private readonly sanitizer = inject(DomSanitizer);
  private readonly scroller = viewChild<ElementRef<HTMLElement>>('scroller');
  private readonly composer = viewChild<ElementRef<HTMLTextAreaElement>>('composer');

  /** `page` uses a roomier layout with larger suggestion cards. */
  readonly variant = input<'dock' | 'page'>('dock');

  readonly draft = signal('');
  private readonly mdCache = new Map<string, SafeHtml>();

  readonly blocks = computed<Block[]>(() => {
    const out: Block[] = [];
    for (const e of this.session.entries()) {
      if (e.role === 'tool') {
        const last = out.at(-1);
        if (last?.kind === 'steps') last.steps.push(e);
        else out.push({ kind: 'steps', steps: [e], key: e.id });
      } else {
        out.push({ kind: e.role, entry: e });
      }
    }
    return out;
  });

  readonly currentStep = computed(() => [...this.session.entries()].reverse().find(e => e.role === 'tool' && e.status === 'running'));

  readonly suggestions = computed(() => {
    const ctx = this.session.context();
    const sym = ctx.symbol;
    if (sym) {
      return [
        { icon: 'bi-graph-up', title: 'Technical read', prompt: `Give me a technical read on ${sym} over the ${ctx.range} range: trend, momentum and key levels.` },
        { icon: 'bi-bank', title: 'Quarterly results', prompt: `Summarize ${sym}'s last four quarters: revenue growth, margins and free cash flow. Cite the periods.` },
        { icon: 'bi-file-earmark-text', title: 'Filing risks', prompt: `What do ${sym}'s most recent SEC filings say about key risks? Quote the filing and section.` },
        { icon: 'bi-calculator', title: 'Valuation range', prompt: `Estimate a DCF value range for ${sym}. List every assumption and how sensitive the result is to each.` },
      ];
    }
    return [
      { icon: 'bi-search', title: 'Screen an idea', prompt: 'Find the ticker for Nvidia and give me a short research brief with current price and recent performance.' },
      { icon: 'bi-bank', title: 'Read the statements', prompt: 'Summarize Microsoft’s last four quarters of revenue, margins and free cash flow.' },
      { icon: 'bi-file-earmark-text', title: 'SEC filings', prompt: 'What are the most recent SEC filings for Apple and what do they cover?' },
      { icon: 'bi-calculator', title: 'Valuation', prompt: 'Walk me through a DCF valuation for Alphabet with explicit assumptions.' },
    ];
  });

  readonly toolLabel = toolLabel;
  readonly profileOption = profileOption;
  readonly defaultOptionLabel = defaultOptionLabel;
  readonly profileSummary = profileSummary;

  /** The profile chosen now, for the line under the select. */
  readonly chosenProfile = computed(() => this.session.profiles().find(p => p.id === this.session.profileId()) ?? this.session.profiles().find(p => p.is_default));

  constructor() {
    void this.session.loadConfig();
    void this.session.loadPersonas();
    void this.session.loadProfiles();
    void this.session.loadTasks();
    // Keep the newest message in view while the agent streams.
    effect(() => {
      this.session.entries();
      this.session.thinking();
      queueMicrotask(() => {
        const el = this.scroller()?.nativeElement;
        if (el) el.scrollTop = el.scrollHeight;
      });
    });
  }

  send(text = this.draft()): void {
    const content = text.trim();
    if (!content || this.session.streaming()) return;
    this.draft.set('');
    queueMicrotask(() => this.resize());
    void this.session.send(content);
  }

  onKeydown(event: KeyboardEvent): void {
    if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
      event.preventDefault();
      this.send();
    }
  }

  onInput(value: string): void {
    this.draft.set(value);
    this.resize();
  }

  focus(): void {
    this.composer()?.nativeElement.focus();
  }

  private resize(): void {
    const el = this.composer()?.nativeElement;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = Math.min(el.scrollHeight, 180) + 'px';
  }

  md(content: string): SafeHtml {
    if (!content) return '';
    const cached = this.mdCache.get(content);
    if (cached) return cached;
    // renderMarkdown escapes every source character before emitting its own tags.
    const html = this.sanitizer.bypassSecurityTrustHtml(renderMarkdown(content));
    if (this.mdCache.size > 400) this.mdCache.clear();
    this.mdCache.set(content, html);
    return html;
  }

  /** A chat step as the shared tool call view takes it. */
  callOf(step: ChatEntry): ToolCall {
    return {
      callId: step.callId ?? `entry-${step.id}`, seq: step.seq, name: step.name ?? 'tool', args: step.args, result: step.content || undefined,
      error: step.toolError, status: step.status ?? 'ok', startedAt: step.startedAt, durationMs: step.durationMs,
      truncated: step.truncated, originalBytes: step.originalBytes, redacted: step.redacted,
    };
  }

  args(raw?: string): string {
    if (!raw) return '';
    try {
      const parsed = JSON.parse(raw);
      const entries = Object.entries(parsed as Record<string, unknown>);
      return entries.map(([k, v]) => `${k}: ${typeof v === 'string' ? v : JSON.stringify(v)}`).join(' · ');
    } catch {
      return raw;
    }
  }

  duration(ms?: number): string {
    if (ms === undefined) return '';
    return ms < 1000 ? `${ms} ms` : `${(ms / 1000).toFixed(1)} s`;
  }

  /** Copy buttons inside rendered markdown code blocks. */
  @HostListener('click', ['$event'])
  onCopyClick(event: Event): void {
    const target = (event.target as HTMLElement | null)?.closest<HTMLButtonElement>('[data-md-copy]');
    if (!target) return;
    const code = target.closest('.md-code')?.querySelector('code')?.textContent ?? '';
    const flash = (label: string) => {
      target.textContent = label;
      setTimeout(() => (target.textContent = 'copy'), 1500);
    };
    const copy = navigator.clipboard?.writeText ? navigator.clipboard.writeText(code) : Promise.reject(new Error('no clipboard'));
    copy.then(() => flash('copied')).catch(() => flash('press ⌘C'));
  }
}
