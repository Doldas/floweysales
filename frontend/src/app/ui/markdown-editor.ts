import { ChangeDetectionStrategy, Component, DestroyRef, computed, effect, inject, input, model, output, signal, viewChild, type ElementRef } from '@angular/core';
import { renderMarkdown, resolveWorkspacePath } from '../markdown';
import { htmlToMarkdown, richTextBlockers } from '../markdown-rich';
import { MarkdownViewComponent } from './markdown-view';

export type MarkdownMode = 'source' | 'preview' | 'rich' | 'split';

/** Rich text keeps the whole document in the DOM; beyond this size the editor stays in source mode. */
export const RICH_TEXT_LIMIT = 400_000;

/**
 * The shared Markdown editor: Edit source, Preview, Rich text and (on wide screens) a Split view.
 *
 * The Markdown text in `value` is the only state. Every mode edits or shows that same text, so switching modes
 * never loses a draft. Rich text writes Markdown back only when the user edits; documents with constructs it
 * cannot represent (raw HTML, front matter, footnotes and so on) stay in source mode with an explanation.
 */
@Component({
  selector: 'fse-markdown-editor',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [MarkdownViewComponent],
  template: `
    <div class="md-editor">
      <div class="md-tabs" role="tablist" aria-label="Markdown view" (keydown)="onTabKey($event)">
        @for (tab of tabs(); track tab.id) {
          <button type="button" role="tab" class="md-tab" [id]="'md-tab-' + tab.id" [attr.aria-selected]="mode() === tab.id"
                  [attr.aria-controls]="'md-panel-' + tab.id" [attr.tabindex]="mode() === tab.id ? 0 : -1"
                  [disabled]="tab.disabled" [attr.title]="tab.disabled ? blockedText() : null" (click)="setMode(tab.id)">
            <i class="bi" [class]="tab.icon" aria-hidden="true"></i>{{ tab.label }}
          </button>
        }
      </div>
      @if (blockers().length) {
        <p class="md-note" role="note"><i class="bi bi-info-circle" aria-hidden="true"></i> {{ blockedText() }}</p>
      } @else if (mode() === 'rich') {
        <p class="md-note" role="note"><i class="bi bi-info-circle" aria-hidden="true"></i> Rich text saves Markdown. Your formatting is normalized only when you change something here.</p>
      }

      <div class="md-panes" [class.split]="mode() === 'split'">
        <div class="md-pane" id="md-panel-source" role="tabpanel" aria-labelledby="md-tab-source" [hidden]="mode() === 'preview' || mode() === 'rich'">
          <div class="md-toolbar" role="toolbar" aria-label="Source editing">
            <button type="button" class="md-tool" (click)="undoSource()" aria-label="Undo" title="Undo (Ctrl+Z)"><i class="bi bi-arrow-counterclockwise" aria-hidden="true"></i></button>
            <button type="button" class="md-tool" (click)="redoSource()" aria-label="Redo" title="Redo (Ctrl+Shift+Z)"><i class="bi bi-arrow-clockwise" aria-hidden="true"></i></button>
          </div>
          <textarea #source class="md-source" spellcheck="false" aria-label="Markdown source" [value]="value()" [disabled]="disabled()"
                    maxlength="2097152" (input)="onSource($event)" (keydown)="onShortcut($event)"></textarea>
        </div>

        @if (mode() === 'preview' || mode() === 'split') {
          <div class="md-pane md-preview" id="md-panel-preview" role="tabpanel" aria-label="Rendered preview" tabindex="0">
            <fse-markdown-view [source]="value()" [baseDir]="baseDir()" (openPath)="openPath.emit($event)" />
            @if (!value().trim()) { <p class="md-empty">Nothing to preview yet.</p> }
          </div>
        }

        <div class="md-pane" id="md-panel-rich" role="tabpanel" aria-labelledby="md-tab-rich" [hidden]="mode() !== 'rich'">
          <div class="md-toolbar" role="toolbar" aria-label="Rich text formatting">
            <button type="button" class="md-tool" (click)="command('undo')" aria-label="Undo" title="Undo (Ctrl+Z)"><i class="bi bi-arrow-counterclockwise" aria-hidden="true"></i></button>
            <button type="button" class="md-tool" (click)="command('redo')" aria-label="Redo" title="Redo (Ctrl+Shift+Z)"><i class="bi bi-arrow-clockwise" aria-hidden="true"></i></button>
            <span class="md-sep" aria-hidden="true"></span>
            <button type="button" class="md-tool" (click)="block('h1')" aria-label="Heading 1" title="Heading 1"><b>H1</b></button>
            <button type="button" class="md-tool" (click)="block('h2')" aria-label="Heading 2" title="Heading 2"><b>H2</b></button>
            <button type="button" class="md-tool" (click)="block('h3')" aria-label="Heading 3" title="Heading 3"><b>H3</b></button>
            <button type="button" class="md-tool" (click)="block('p')" aria-label="Paragraph" title="Paragraph"><i class="bi bi-paragraph" aria-hidden="true"></i></button>
            <span class="md-sep" aria-hidden="true"></span>
            <button type="button" class="md-tool" (click)="command('bold')" aria-label="Bold" title="Bold (Ctrl+B)"><i class="bi bi-type-bold" aria-hidden="true"></i></button>
            <button type="button" class="md-tool" (click)="command('italic')" aria-label="Italic" title="Italic (Ctrl+I)"><i class="bi bi-type-italic" aria-hidden="true"></i></button>
            <button type="button" class="md-tool" (click)="command('strikeThrough')" aria-label="Strikethrough" title="Strikethrough"><i class="bi bi-type-strikethrough" aria-hidden="true"></i></button>
            <button type="button" class="md-tool" (click)="inlineCode()" aria-label="Inline code" title="Inline code"><i class="bi bi-code" aria-hidden="true"></i></button>
            <button type="button" class="md-tool" (click)="openLink()" aria-label="Link" title="Link (Ctrl+K)"><i class="bi bi-link-45deg" aria-hidden="true"></i></button>
            <span class="md-sep" aria-hidden="true"></span>
            <button type="button" class="md-tool" (click)="command('insertUnorderedList')" aria-label="Bulleted list" title="Bulleted list"><i class="bi bi-list-ul" aria-hidden="true"></i></button>
            <button type="button" class="md-tool" (click)="command('insertOrderedList')" aria-label="Numbered list" title="Numbered list"><i class="bi bi-list-ol" aria-hidden="true"></i></button>
            <button type="button" class="md-tool" (click)="block('blockquote')" aria-label="Quote" title="Quote"><i class="bi bi-quote" aria-hidden="true"></i></button>
            <button type="button" class="md-tool" (click)="command('insertHorizontalRule')" aria-label="Horizontal rule" title="Horizontal rule"><i class="bi bi-hr" aria-hidden="true"></i></button>
          </div>
          @if (linkOpen()) {
            <form class="md-link-form" (submit)="$event.preventDefault(); applyLink(url.value)">
              <label>Link address <input #url name="linkUrl" type="text" inputmode="url" placeholder="https://example.com or notes/other.md" autocomplete="off" aria-describedby="md-link-help"></label>
              <button type="submit" class="btn btn-sm btn-primary">Apply link</button>
              <button type="button" class="btn btn-sm btn-outline-secondary" (click)="closeLink()">Cancel</button>
              <small id="md-link-help" [class.md-link-error]="linkError()" role="status">{{ linkError() || 'Web addresses (https) or paths inside the Workspace.' }}</small>
            </form>
          }
          <div #rich class="md-rich chat-markdown" contenteditable="true" role="textbox" aria-multiline="true" aria-label="Rich text editor"
               spellcheck="true" (input)="onRichInput()" (keydown)="onShortcut($event)" (paste)="onPaste($event)"
               (drop)="$event.preventDefault()" (click)="onRichClick($event)" (focus)="onRichFocus()"></div>
        </div>
      </div>
    </div>
  `,
  styles: [`
    :host { display: block; min-width: 0; }
    .md-editor { display: flex; flex-direction: column; gap: 8px; min-width: 0; }
    .md-tabs { display: flex; flex-wrap: wrap; gap: 4px; border-bottom: 1px solid var(--fs-border); }
    .md-tab { display: inline-flex; align-items: center; gap: 6px; min-height: 40px; padding: 6px 12px; border: 0; border-bottom: 2px solid transparent; background: transparent; color: var(--fs-text-2); font-size: .85rem; border-radius: 6px 6px 0 0; }
    .md-tab[aria-selected='true'] { color: var(--fs-accent-text); border-bottom-color: var(--fs-accent); font-weight: 600; background: var(--fs-accent-soft); }
    .md-tab:disabled { opacity: .5; }
    .md-note { margin: 0; font-size: .78rem; color: var(--fs-muted); }
    .md-panes { display: grid; gap: 12px; min-width: 0; }
    .md-panes.split { grid-template-columns: repeat(2, minmax(0, 1fr)); }
    .md-pane { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
    .md-pane[hidden] { display: none; }
    .md-toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: 4px; padding: 4px; background: var(--fs-surface-2); border: 1px solid var(--fs-border); border-radius: var(--bs-border-radius); }
    .md-tool { min-width: 40px; min-height: 40px; display: inline-flex; align-items: center; justify-content: center; padding: 0 8px; background: transparent; color: var(--fs-text); border: 0; border-radius: 6px; font-size: .9rem; }
    .md-tool:hover { background: var(--fs-surface-3); }
    .md-sep { width: 1px; align-self: stretch; margin: 4px 2px; background: var(--fs-border); }
    .md-source, .md-rich, .md-preview { height: clamp(220px, 55dvh, 560px); overflow: auto; border: 1px solid var(--fs-border); border-radius: var(--bs-border-radius); padding: 12px; margin: 0; min-width: 0; }
    .md-source { font: 13px/1.6 var(--bs-font-monospace) !important; tab-size: 2; resize: vertical; white-space: pre-wrap; overflow-wrap: anywhere; background: var(--fs-surface); color: var(--fs-text); }
    .md-rich, .md-preview { background: var(--fs-surface); }
    .md-rich:focus-visible, .md-preview:focus-visible, .md-source:focus-visible { outline: 2px solid var(--fs-accent); outline-offset: 1px; }
    .md-empty { color: var(--fs-muted); font-size: .85rem; margin: 0; }
    .md-link-form { display: flex; flex-wrap: wrap; gap: 8px; align-items: end; padding: 8px; background: var(--fs-surface-2); border: 1px solid var(--fs-border); border-radius: var(--bs-border-radius); }
    .md-link-form label { flex: 1; min-width: 200px; }
    .md-link-form small { flex-basis: 100%; color: var(--fs-muted); }
    .md-link-form small.md-link-error { color: var(--fs-neg); }
    @media (max-width: 650px) { .md-tab { padding: 6px 8px; font-size: .8rem; } }
  `],
})
export class MarkdownEditorComponent {
  /** The Markdown text (two-way). */
  readonly value = model('');
  /** Workspace folder of the file, for relative links. */
  readonly baseDir = input('/');
  readonly disabled = input(false);
  /** Ctrl/Cmd+S inside the editor. */
  readonly save = output<void>();
  /** A Workspace path followed from the preview. */
  readonly openPath = output<string>();

  readonly mode = signal<MarkdownMode>('source');
  protected readonly linkOpen = signal(false);
  protected readonly linkError = signal('');
  private readonly wide = signal(false);

  private readonly sourceEl = viewChild<ElementRef<HTMLTextAreaElement>>('source');
  private readonly richEl = viewChild<ElementRef<HTMLElement>>('rich');
  /** The Markdown the rich pane last rendered or wrote; the pane is rebuilt only when `value` differs from it. */
  private lastRich: string | null = null;
  private savedRange: Range | null = null;
  private sourceState = { start: 0, end: 0, top: 0 };

  protected readonly blockers = computed(() => {
    const v = this.value();
    const found = v.length > RICH_TEXT_LIMIT ? ['a very large file (over ' + RICH_TEXT_LIMIT.toLocaleString('en-US') + ' characters)'] : richTextBlockers(v);
    return found;
  });
  protected readonly blockedText = computed(() => {
    const found = this.blockers();
    return found.length
      ? 'Rich text is unavailable because this file contains ' + found.join(', ') + '. Use Edit source to keep it exactly as written.'
      : '';
  });
  protected readonly tabs = computed(() => {
    const list: { id: MarkdownMode; label: string; icon: string; disabled: boolean }[] = [
      { id: 'source', label: 'Edit source', icon: 'bi-code-slash', disabled: false },
      { id: 'preview', label: 'Preview', icon: 'bi-eye', disabled: false },
      { id: 'rich', label: 'Rich text', icon: 'bi-pencil-square', disabled: this.blockers().length > 0 },
    ];
    if (this.wide()) list.push({ id: 'split', label: 'Split', icon: 'bi-layout-split', disabled: false });
    return list;
  });

  constructor() {
    const destroyRef = inject(DestroyRef);
    if (typeof window !== 'undefined' && window.matchMedia) {
      const query = window.matchMedia('(min-width: 1600px)');
      const update = () => {
        this.wide.set(query.matches);
        if (!query.matches && this.mode() === 'split') this.setMode('source');
      };
      update();
      query.addEventListener('change', update);
      destroyRef.onDestroy(() => query.removeEventListener('change', update));
    }
    // Fill the rich pane whenever it becomes visible with text it has not rendered yet.
    effect(() => {
      const el = this.richEl()?.nativeElement;
      const text = this.value();
      const base = this.baseDir();
      if (!el || this.mode() !== 'rich' || text === this.lastRich) return;
      // renderMarkdown escapes every source character before emitting its own tags.
      el.innerHTML = renderMarkdown(text, { rich: true, workspaceBase: base });
      this.lastRich = text;
    });
  }

  setMode(next: MarkdownMode): void {
    if (next === 'rich' && this.blockers().length) return;
    const previous = this.mode();
    if (previous === next) return;
    const area = this.sourceEl()?.nativeElement;
    if (area && (previous === 'source' || previous === 'split')) {
      this.sourceState = { start: area.selectionStart, end: area.selectionEnd, top: area.scrollTop };
    }
    this.linkOpen.set(false);
    this.mode.set(next);
    if (next === 'source' || next === 'split') {
      queueMicrotask(() => {
        const el = this.sourceEl()?.nativeElement;
        if (!el) return;
        const max = el.value.length;
        el.setSelectionRange(Math.min(this.sourceState.start, max), Math.min(this.sourceState.end, max));
        el.scrollTop = this.sourceState.top;
      });
    }
  }

  protected onTabKey(event: KeyboardEvent): void {
    const keys = ['ArrowLeft', 'ArrowRight', 'Home', 'End'];
    if (!keys.includes(event.key)) return;
    const enabled = this.tabs().filter((t) => !t.disabled);
    let index = enabled.findIndex((t) => t.id === this.mode());
    if (event.key === 'ArrowRight') index = (index + 1) % enabled.length;
    else if (event.key === 'ArrowLeft') index = (index - 1 + enabled.length) % enabled.length;
    else index = event.key === 'Home' ? 0 : enabled.length - 1;
    event.preventDefault();
    this.setMode(enabled[index].id);
    queueMicrotask(() => document.getElementById('md-tab-' + enabled[index].id)?.focus());
  }

  protected onSource(event: Event): void {
    this.value.set((event.target as HTMLTextAreaElement).value);
  }

  protected undoSource(): void {
    this.sourceEl()?.nativeElement.focus();
    document.execCommand('undo');
  }

  protected redoSource(): void {
    this.sourceEl()?.nativeElement.focus();
    document.execCommand('redo');
  }

  protected onShortcut(event: KeyboardEvent): void {
    const mod = event.ctrlKey || event.metaKey;
    if (mod && event.key.toLowerCase() === 's') {
      event.preventDefault();
      this.save.emit();
    } else if (mod && event.key.toLowerCase() === 'k' && this.mode() === 'rich') {
      event.preventDefault();
      this.openLink();
    }
  }

  // ---- Rich text ----

  protected onRichFocus(): void {
    try {
      document.execCommand('defaultParagraphSeparator', false, 'p');
    } catch {
      /* the browser keeps its default separator */
    }
  }

  protected onRichInput(): void {
    const el = this.richEl()?.nativeElement;
    if (!el) return;
    const markdown = htmlToMarkdown(el);
    this.lastRich = markdown;
    if (markdown !== this.value()) this.value.set(markdown);
  }

  protected onPaste(event: ClipboardEvent): void {
    // Only plain text is accepted, so pasted HTML can never enter the document.
    event.preventDefault();
    const text = event.clipboardData?.getData('text/plain') ?? '';
    if (text) document.execCommand('insertText', false, text);
  }

  protected onRichClick(event: Event): void {
    if ((event.target as HTMLElement | null)?.closest('a')) event.preventDefault();
  }

  protected command(name: string): void {
    this.richEl()?.nativeElement.focus();
    document.execCommand(name);
    this.onRichInput();
  }

  protected block(tag: string): void {
    this.richEl()?.nativeElement.focus();
    document.execCommand('formatBlock', false, tag);
    this.onRichInput();
  }

  protected inlineCode(): void {
    const el = this.richEl()?.nativeElement;
    const selection = window.getSelection();
    if (!el || !selection || !selection.rangeCount) return;
    const range = selection.getRangeAt(0);
    if (range.collapsed || !el.contains(range.commonAncestorContainer)) return;
    const code = document.createElement('code');
    code.textContent = range.toString();
    range.deleteContents();
    range.insertNode(code);
    selection.removeAllRanges();
    this.onRichInput();
  }

  protected openLink(): void {
    const selection = window.getSelection();
    const el = this.richEl()?.nativeElement;
    this.savedRange = selection && selection.rangeCount && el?.contains(selection.anchorNode) ? selection.getRangeAt(0).cloneRange() : null;
    this.linkError.set(this.savedRange && !this.savedRange.collapsed ? '' : 'Select the text to link first.');
    this.linkOpen.set(true);
  }

  protected closeLink(): void {
    this.linkOpen.set(false);
    this.linkError.set('');
    this.richEl()?.nativeElement.focus();
  }

  protected applyLink(raw: string): void {
    const url = raw.trim();
    const range = this.savedRange;
    if (!range || range.collapsed) {
      this.linkError.set('Select the text to link first.');
      return;
    }
    const web = /^https?:\/\/[^\s]+$/i.test(url);
    if (!web && resolveWorkspacePath(this.baseDir(), url) === null) {
      this.linkError.set('Use an https:// address or a path inside the Workspace.');
      return;
    }
    this.linkOpen.set(false);
    this.linkError.set('');
    const el = this.richEl()?.nativeElement;
    el?.focus();
    const selection = window.getSelection();
    selection?.removeAllRanges();
    selection?.addRange(range);
    document.execCommand('createLink', false, url);
    this.onRichInput();
  }
}
