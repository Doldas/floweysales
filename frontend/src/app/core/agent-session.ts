import { Injectable, computed, inject, signal } from '@angular/core';

import type { Message, ModelProfile, Persona } from '../models';
import { MarketState } from './market-state';
import { defaultTypes, delegationProgress, parseEvidenceMode, policyFromSettings, settingsFromPolicy, settingsProblem, type AnswerClaim, type AnswerFinding, type AnswerSource, type EvidenceMode, type EvidencePolicy, type EvidenceSettings, type ClaimStats } from './answer-sources';
import { MAX_DELIVERABLES, parseChecklist, parseDeliverables, type SavedTask, type TaskDeliverable } from './task-view';
import { EMPTY_NOTICES, applyNotice, dismissNotice, finishNotices, recoveryLine, type NoticeState } from './agent-notices';
import { takeSseFrames, sseSequence } from './agent-stream';

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

const PROFILE_KEY = 'fs.profile';
const EVIDENCE_KEY = 'fs.evidenceMode';

/** The settings kept in this browser: a policy object, or the plain mode an older version stored. */
function readStoredEvidence(): EvidenceSettings {
  try {
    const raw = localStorage.getItem(EVIDENCE_KEY);
    if (raw?.startsWith('{')) return settingsFromPolicy(JSON.parse(raw));
    return settingsFromPolicy({ mode: parseEvidenceMode(raw) });
  } catch { return settingsFromPolicy(null); }
}

function readStoredProfile(): number | null {
  try {
    const n = Number(localStorage.getItem(PROFILE_KEY));
    return Number.isInteger(n) && n > 0 ? n : null;
  } catch { return null; }
}

/** One entry in the visible conversation. Tool entries are agent steps. */
export interface ChatEntry {
  id: number;
  role: 'user' | 'assistant' | 'tool';
  content: string;
  /** Tool name for agent steps. */
  name?: string;
  /** Ties a step to its result when several calls overlap. */
  callId?: string;
  seq?: number;
  truncated?: boolean;
  originalBytes?: number;
  redacted?: boolean;
  /** The tool's error text, kept apart from its output. */
  toolError?: string;
  /** JSON arguments the agent passed to the tool. */
  args?: string;
  status?: 'running' | 'ok' | 'error';
  startedAt?: number;
  durationMs?: number;
  /** What the model reasoned before and while answering, when it sends that apart from the answer. Display only. */
  thinking?: string;
  /** The character that wrote an assistant message, when one was chosen. */
  persona?: string;
  /** Latest progress line of work the step delegated, such as a team member answering. */
  progress?: string;
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
  /** A terminal failure of the last run; the user can dismiss it. */
  readonly error = signal('');
  /** Retry status, recoveries and warnings of the current run. */
  readonly noticeState = signal<NoticeState>(EMPTY_NOTICES);
  /** What the latest run retrieved, and what the citation check found in its answer. */
  readonly answerSources = signal<AnswerSource[]>([]);
  readonly answerAudit = signal<AnswerFinding[]>([]);
  /** Statements the block option took out of the latest answer. */
  readonly answerRemoved = signal<AnswerFinding[]>([]);
  /** The evidence policy the latest run was sent with, and its question, for exports. */
  readonly answerPolicy = signal<EvidencePolicy>({ mode: 'standard' });
  readonly answerQuestion = signal('');
  /** Claim-level evidence records of the latest answer, with the cost of checking them. */
  readonly answerClaims = signal<AnswerClaim[]>([]);
  readonly claimStats = signal<ClaimStats | null>(null);
  readonly runStatus = computed(() => this.noticeState().status);
  readonly runStatusDetail = computed(() => this.noticeState().statusDetail);
  readonly notices = computed(() => this.noticeState().notices);
  readonly recovery = computed(() => recoveryLine(this.noticeState()));
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
  /** The investor character answering, or '' for the plain analyst. */
  readonly persona = signal('');
  /**
   * How strictly answers must be backed by retrieved sources, and the limits under
   * the mode. Kept in this browser; the user's saved default (on the server) fills
   * it in when the chat opens.
   */
  readonly evidenceSettings = signal<EvidenceSettings>(readStoredEvidence());
  readonly evidenceMode = computed<EvidenceMode>(() => this.evidenceSettings().mode);
  readonly evidenceProblem = computed(() => settingsProblem(this.evidenceSettings()));
  /** Whether the user has a saved default, and the outcome of the last save or reset. */
  readonly evidenceHasDefault = signal(false);
  readonly evidenceNote = signal('');
  setEvidenceMode(mode: EvidenceMode): void {
    // The source types a mode counts differ, so a change of mode starts from its own.
    this.updateEvidence({ mode, ...defaultTypes(mode), ...(mode === 'standard' ? { block: false } : {}) });
  }
  updateEvidence(patch: Partial<EvidenceSettings>): void {
    this.evidenceSettings.update(s => ({ ...s, ...patch }));
    this.evidenceNote.set('');
    try { localStorage.setItem(EVIDENCE_KEY, JSON.stringify(policyFromSettings(this.evidenceSettings()))); } catch { /* ignore */ }
  }

  private evidenceLoaded = false;
  /** Fills the settings from the saved default once per page load, so a later change in the chat is not overwritten. */
  async loadEvidenceDefault(): Promise<void> {
    if (this.evidenceLoaded) return;
    this.evidenceLoaded = true;
    try {
      const res = await fetch('/api/ai/evidence-default', { credentials: 'same-origin' });
      if (!res.ok) return;
      const saved: EvidencePolicy | null = (await res.json()).policy;
      this.evidenceHasDefault.set(!!saved);
      if (saved) {
        this.evidenceSettings.set(settingsFromPolicy(saved));
        try { localStorage.setItem(EVIDENCE_KEY, JSON.stringify(saved)); } catch { /* ignore */ }
      }
    } catch { /* The browser's own choice stays. */ }
  }

  /** Saves the current settings as the user's default, or removes the default. */
  async saveEvidenceDefault(save: boolean): Promise<void> {
    if (save && this.evidenceProblem()) { this.evidenceNote.set(this.evidenceProblem()); return; }
    try {
      const res = await fetch('/api/ai/evidence-default', {
        method: save ? 'PUT' : 'DELETE', credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: save ? JSON.stringify(policyFromSettings(this.evidenceSettings())) : undefined,
      });
      if (!res.ok) {
        let detail = `HTTP ${res.status}`;
        try { detail = (await res.json())?.error || detail; } catch { /* keep the status */ }
        this.evidenceNote.set(`Could not ${save ? 'save' : 'reset'} the default: ${detail}`);
        return;
      }
      this.evidenceHasDefault.set(save);
      this.evidenceNote.set(save ? 'Saved as your default for chat and team runs.' : 'Your default was removed; new chats start from Standard.');
    } catch {
      this.evidenceNote.set(`Could not ${save ? 'save' : 'reset'} the default.`);
    }
  }
  readonly personas = signal<Persona[]>([]);
  /** Model profiles the administrator prepared, and the one chosen (null: the default). */
  readonly profiles = signal<ModelProfile[]>([]);
  readonly profileId = signal<number | null>(readStoredProfile());
  /** The model the current run uses, from the server (it changes if a fallback takes over). */
  readonly activeModel = signal('');
  readonly personaName = computed(() => this.personas().find(p => p.id === this.persona())?.name ?? '');
  /** Required parts typed for the next question, one per line. Filled in, the question becomes a saved task. */
  readonly deliverablesText = signal('');
  readonly deliverableCount = computed(() => parseDeliverables(this.deliverablesText()).length);
  readonly maxDeliverables = MAX_DELIVERABLES;
  /** The task the current or latest run belongs to, with its checklist as the server last reported it. */
  readonly activeTask = signal<{ id: number; status: string; checklist: TaskDeliverable[] } | null>(null);
  /** The user's saved tasks, newest first. */
  readonly tasks = signal<SavedTask[]>([]);
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
  private activeRunId: string | null = null;
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

  async loadProfiles(): Promise<void> {
    try {
      const res = await fetch('/api/ai/profiles', { credentials: 'same-origin' });
      if (!res.ok) return;
      const list: ModelProfile[] = (await res.json()).profiles || [];
      this.profiles.set(list);
      // A stored choice that was deleted or disabled since is forgotten, not silently replaced.
      if (this.profileId() !== null && !list.some(p => p.id === this.profileId())) this.setProfile(null);
    } catch {}
  }

  setProfile(id: number | null): void {
    this.profileId.set(id);
    try { id === null ? localStorage.removeItem(PROFILE_KEY) : localStorage.setItem(PROFILE_KEY, String(id)); } catch {}
  }

  async loadPersonas(): Promise<void> {
    if (this.personas().length) return;
    try {
      const res = await fetch('/api/ai/personas', { credentials: 'same-origin' });
      if (res.ok) this.personas.set((await res.json()).personas || []);
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
    this.noticeState.set(EMPTY_NOTICES);
    this.answerSources.set([]);
    this.answerAudit.set([]);
    this.answerRemoved.set([]);
    this.answerClaims.set([]);
    this.claimStats.set(null);
    this.assistantId = -1;
  }

  dismissError(): void { this.error.set(''); }
  dismissNotice(id: number): void { this.noticeState.update(s => dismissNotice(s, id)); }

  stop(): void {
    // Stopping is different from losing a connection: explicitly cancel the
    // server-owned run, rather than silently leaving it using model resources.
    const id = this.activeRunId;
    this.abort?.abort();
    if (id) void fetch(`/api/ai/runs/${encodeURIComponent(id)}`, {
      method: 'DELETE', credentials: 'same-origin',
    }).catch(() => { /* the visible request has already been stopped */ });
    this.activeRunId = null;
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

    if (this.evidenceProblem()) {
      this.error.set(`Evidence settings: ${this.evidenceProblem()}`);
      return;
    }
    this.error.set('');
    this.noticeState.set(EMPTY_NOTICES);
    this.answerSources.set([]);
    this.answerAudit.set([]);
    this.answerRemoved.set([]);
    this.answerPolicy.set(policyFromSettings(this.evidenceSettings()));
    this.answerQuestion.set(content);
    this.answerClaims.set([]);
    this.claimStats.set(null);
    this.push({ role: 'user', content });
    this.wire.push({ role: 'user', content });
    this.streaming.set(true);
    this.assistantId = -1;

    const ctx = this.context();
    const required = parseDeliverables(this.deliverablesText());
    this.deliverablesText.set('');
    this.activeTask.set(null);
    await this.stream('/api/ai/chat', {
      messages: this.wire.map(m => ({ ...m })),
      model: this.canConfigure() ? this.chatModel() : undefined,
      persona: this.persona() || undefined,
      profile_id: this.profileId() || undefined,
      // Always sent, so a choice of Standard is not replaced by the saved default.
      evidence: policyFromSettings(this.evidenceSettings()),
      deliverables: required.length ? required : undefined,
      symbol: ctx.symbol,
      provider: ctx.provider,
      range: ctx.range,
      price: ctx.price,
    });
    if (required.length) void this.loadTasks();
  }

  async loadTasks(): Promise<void> {
    try {
      const res = await fetch('/api/ai/tasks', { credentials: 'same-origin' });
      if (res.ok) this.tasks.set((await res.json()).tasks || []);
    } catch { /* the list stays as it was */ }
  }

  /** Continues a saved task from its last checkpoint: the server restores the conversation, the evidence gathered and the budgets already spent. */
  async resumeTask(task: SavedTask): Promise<void> {
    if (this.streaming()) return;
    await this.loadConfig();
    if (!this.canSend()) return;
    this.error.set('');
    this.noticeState.set(EMPTY_NOTICES);
    this.answerSources.set([]);
    this.answerAudit.set([]);
    this.answerRemoved.set([]);
    this.answerClaims.set([]);
    this.claimStats.set(null);
    // The saved conversation lives on the server; this view shows the continuation.
    this.wire = [];
    this.entries.set([]);
    this.push({ role: 'user', content: `Resume the task: ${task.title}` });
    this.streaming.set(true);
    this.assistantId = -1;
    this.activeTask.set({ id: task.id, status: task.status, checklist: task.deliverables });
    await this.stream(`/api/ai/tasks/${task.id}/resume`, {});
    void this.loadTasks();
  }

  /**
   * Fetches a saved task with its last answer, sources, findings and policy, for an export. Returns null when the
   * task has no answer yet or cannot be read.
   */
  async taskForExport(id: number): Promise<{ title: string; answer: string; policy: EvidencePolicy | null; sources: AnswerSource[]; audit: AnswerFinding[]; removed: AnswerFinding[] } | null> {
    try {
      const res = await fetch(`/api/ai/tasks/${id}`, { credentials: 'same-origin' });
      if (!res.ok) { this.error.set('The task could not be read.'); return null; }
      const t = await res.json();
      if (!t.answer) { this.error.set('This task has no answer to export yet.'); return null; }
      return { title: t.title ?? '', answer: t.answer, policy: t.evidence ?? null, sources: t.sources ?? [], audit: t.audit ?? [], removed: t.removed ?? [] };
    } catch { this.error.set('The task could not be read.'); return null; }
  }

  async deleteTask(id: number): Promise<void> {
    try {
      const res = await fetch(`/api/ai/tasks/${id}`, { method: 'DELETE', credentials: 'same-origin' });
      if (res.ok) this.tasks.update(list => list.filter(t => t.id !== id));
      else this.error.set((await res.json().catch(() => null))?.error || 'The task could not be deleted.');
    } catch { this.error.set('The task could not be deleted.'); }
  }

  /** A streaming POST creates one server-owned run. A reconnect only GETs
   * events from the last acknowledged cursor: it never restarts tool calls. */
  private async stream(url: string, body: unknown): Promise<void> {
    const controller = new AbortController();
    this.abort = controller;
    let runId: string | null = null;
    let lastSequence = 0;
    let completed = false;
    let attempts = 0;
    let requestUrl = url;
    let requestMethod = 'POST';
    try {
      for (;;) {
        try {
          const res = await fetch(requestUrl, {
            method: requestMethod,
            credentials: 'same-origin',
            headers: requestMethod === 'POST' ? { 'Content-Type': 'application/json' } : {},
            signal: controller.signal,
            body: requestMethod === 'POST' ? JSON.stringify(body) : undefined,
          });
          if (!res.ok || !res.body) {
            let detail = `HTTP ${res.status}`;
            try {
              const payload = await res.json();
              if (payload?.error) detail = payload.error;
            } catch { /* preserve HTTP status */ }
            // A reconnect GET is idempotent. Retry transient gateway failures
            // without sending another POST and executing the agent again.
            if (requestMethod === 'GET' && [429, 502, 503, 504].includes(res.status)) {
              throw new TypeError(detail);
            }
            throw new Error(detail);
          }
          if (requestMethod === 'GET') {
            this.noticeState.update(s => ({ ...s, status: '', statusDetail: '' }));
          }
          const headerId = res.headers.get('X-Agent-Run-ID');
          if (headerId) {
            if (runId && runId !== headerId) throw new Error('The server returned a different research run.');
            runId = headerId;
            this.activeRunId = headerId;
          }
          const reader = res.body.getReader();
          const decoder = new TextDecoder();
          let buffer = '';
          for (;;) {
            const { value, done } = await reader.read();
            if (done) break;
            const parsed = takeSseFrames(buffer, decoder.decode(value, { stream: true }));
            buffer = parsed.remainder;
            for (const frame of parsed.frames) {
              const seq = sseSequence(frame);
              if (seq !== null) {
                if (seq <= lastSequence) continue;
                if (seq !== lastSequence + 1) throw new Error('A portion of the research stream could not be replayed; the answer may be incomplete.');
                lastSequence = seq;
              }
              if (this.handleChunk(frame)) completed = true;
            }
          }
          if (completed || !runId) return;
          // An HTTP 200 stream that ends without a terminal event may have
          // been truncated by a proxy; replay from the last delivered id.
          throw new TypeError('The response ended before the research run completed');
        } catch (err) {
          if (controller.signal.aborted || !runId || !(err instanceof TypeError) || attempts >= 450) throw err;
          attempts++;
          this.noticeState.update(s => ({
            ...s,
            status: 'Connection interrupted. Reconnecting to the same research run…',
            statusDetail: `Attempt ${attempts}; tools and model work continue on the server.`,
          }));
          // Keep retrying across extended network outages. Stop is instant:
          // the abort signal cancels this delay and the server run.
          const delay = Math.min(1000 * 2 ** Math.min(attempts - 1, 4), 12000);
          await new Promise<void>(resolve => {
            const onAbort = () => { clearTimeout(timer); resolve(); };
            const timer = setTimeout(() => { controller.signal.removeEventListener('abort', onAbort); resolve(); }, delay);
            controller.signal.addEventListener('abort', onAbort, { once: true });
          });
          if (controller.signal.aborted) return;
          requestUrl = `/api/ai/runs/${encodeURIComponent(runId)}/events?after=${lastSequence}`;
          requestMethod = 'GET';
        }
      }
    } catch (err) {
      if ((err as Error)?.name !== 'AbortError' && !controller.signal.aborted) {
        const msg = (err as Error)?.message || 'Request failed';
        this.error.set(err instanceof TypeError
          ? `The research run lost its connection after repeated reconnection attempts (${msg}). The answer may be incomplete.`
          : msg);
      }
    } finally {
      // Only mark genuinely unfinished tool steps as interrupted.
      this.entries.update(list => list.map(e => (e.status === 'running' ? { ...e, status: 'error', content: e.content || 'Interrupted' } : e)));
      this.noticeState.update(finishNotices);
      this.streaming.set(false);
      this.activeRunId = null;
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

  private handleChunk(chunk: string): boolean {
    let finished = false;
    for (const rawLine of chunk.split('\n')) {
      const line = rawLine.trim();
      if (!line.startsWith('data:')) continue;
      const payload = line.slice(5).trim();
      if (!payload) continue;
      let ev: { model?: string; call_id?: string; seq?: number; duration_ms?: number; truncated?: boolean; original_bytes?: number; redacted?: boolean; type?: string; severity?: string; code?: string; attempt?: number; max_attempts?: number; detail?: string; content?: string; name?: string; arguments?: string; result?: string; tool_error?: string; error?: string; message?: Message; sources?: AnswerSource[]; audit?: AnswerFinding[]; removed?: AnswerFinding[]; claims?: AnswerClaim[]; claim_stats?: ClaimStats; parent_call_id?: string; agent?: string; stance?: string };
      try {
        ev = JSON.parse(payload);
      } catch {
        continue;
      }
      switch (ev.type) {
        case 'stream_end':
          finished = true;
          break;
        case 'run_start':
          this.activeModel.set(ev.model || '');
          break;
        case 'task':
          // The run was saved as a task; its checklist follows in checkpoint events.
          this.activeTask.update(t => ({ id: Number(ev.name), status: ev.content || 'running', checklist: t?.checklist ?? [] }));
          break;
        case 'checkpoint':
          this.activeTask.update(t => ({ id: Number(ev.name) || t?.id || 0, status: ev.content || t?.status || 'running', checklist: ev.result ? parseChecklist(ev.result) : t?.checklist ?? [] }));
          break;
        case 'transcript_message':
          if (ev.message && (ev.message.role === 'assistant' || ev.message.role === 'tool')) this.wire.push(ev.message);
          break;
        case 'delta':
          this.appendAssistant(ev.content || '');
          break;
        case 'thinking':
          this.appendThinking(ev.content || '');
          break;
        case 'tool_start':
          this.push({ role: 'tool', name: ev.name, args: ev.arguments, content: '', status: 'running', startedAt: Date.now(),
            callId: ev.call_id, seq: ev.seq, truncated: ev.truncated, redacted: ev.redacted });
          this.assistantId = -1;
          break;
        case 'tool_result':
          this.finishStep(ev);
          this.assistantId = -1;
          break;
        case 'member_start':
        case 'member_done':
          // Work of a team the agent delegated to; shown on the delegating step.
          if (ev.parent_call_id) this.setProgress(ev.parent_call_id, delegationProgress(ev));
          break;
        case 'sources':
          this.answerSources.set(ev.sources ?? []);
          this.answerAudit.set(ev.audit ?? []);
          this.answerRemoved.set(ev.removed ?? []);
          this.answerClaims.set(ev.claims ?? []);
          this.claimStats.set(ev.claim_stats ?? null);
          break;
        case 'error':
          this.error.set(ev.error || 'AI error');
          break;
        case 'notice':
          // The backend explains retries, recoveries and cut-off replies (a full
          // context window, say). Only real problems stay on screen.
          if (ev.code === 'retry') this.discardPartialReply();
          this.noticeState.update(s => applyNotice(s, ev));
          break;
      }
    }
    return finished;
  }

  private setProgress(callId: string, progress: string): void {
    this.entries.update(list => list.map(e => (e.role === 'tool' && e.callId === callId ? { ...e, progress } : e)));
  }

  /** A failed model call may have streamed part of a reply; the retry starts it over. */
  private discardPartialReply(): void {
    const id = this.assistantId;
    if (id < 0) return;
    this.entries.update(list => list.filter(e => e.id !== id));
    this.assistantId = -1;
  }

  private push(entry: Omit<ChatEntry, 'id'>): number {
    const id = this.nextId++;
    this.entries.update(list => [...list, { id, ...entry }]);
    return id;
  }

  /** Completes the step of the call the event belongs to: by call id, else the latest running call of the same tool. */
  private finishStep(ev: { name?: string; result?: string; tool_error?: string; call_id?: string; seq?: number; duration_ms?: number; truncated?: boolean; original_bytes?: number; redacted?: boolean }): void {
    const ok = !ev.tool_error;
    const done = {
      content: ev.result || (ok ? 'No result' : ''), toolError: ev.tool_error, status: ok ? 'ok' as const : 'error' as const,
      truncated: ev.truncated, originalBytes: ev.original_bytes, redacted: ev.redacted,
    };
    this.entries.update(list => {
      const copy = list.slice();
      let at = ev.call_id ? copy.findIndex(e => e.role === 'tool' && e.callId === ev.call_id) : -1;
      if (at < 0) for (let i = copy.length - 1; i >= 0; i--) {
        const e = copy[i];
        if (e.role === 'tool' && e.status === 'running' && (!ev.name || e.name === ev.name)) { at = i; break; }
      }
      if (at >= 0) {
        const e = copy[at];
        copy[at] = { ...e, ...done, truncated: e.truncated || ev.truncated, redacted: e.redacted || ev.redacted,
          durationMs: ev.duration_ms ?? (e.startedAt ? Date.now() - e.startedAt : undefined) };
        return copy;
      }
      return [...copy, { id: this.nextId++, role: 'tool', name: ev.name, callId: ev.call_id, seq: ev.seq, durationMs: ev.duration_ms, ...done }];
    });
  }

  /** Reasoning arrives before the answer of the same round, so it opens that round's message. */
  private appendThinking(delta: string): void {
    if (!delta) return;
    if (this.assistantId < 0) {
      this.assistantId = this.push({ role: 'assistant', content: '', thinking: delta, persona: this.personaName() || undefined });
      return;
    }
    const id = this.assistantId;
    this.entries.update(list => list.map(e => (e.id === id ? { ...e, thinking: (e.thinking ?? '') + delta } : e)));
  }

  private appendAssistant(delta: string): void {
    if (this.assistantId < 0) {
      this.assistantId = this.push({ role: 'assistant', content: delta, persona: this.personaName() || undefined });
      return;
    }
    const id = this.assistantId;
    this.entries.update(list => list.map(e => (e.id === id ? { ...e, content: e.content + delta } : e)));
  }
}
