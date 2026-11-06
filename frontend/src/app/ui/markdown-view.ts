import { ChangeDetectionStrategy, Component, computed, inject, input, output } from '@angular/core';
import { DomSanitizer } from '@angular/platform-browser';
import { renderMarkdown } from '../markdown';

/**
 * Read-only rendered Markdown, shared by the Workspace preview, the folder README overview and anywhere else a
 * Markdown file is shown. It uses the one shared renderer, which escapes every source character before adding
 * its own tags, never loads images or remote content and only turns links into Workspace paths when they stay
 * inside the Workspace. Clicking such a link emits `openPath` instead of navigating.
 */
@Component({
  selector: 'fse-markdown-view',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `<div class="chat-markdown md-view" [innerHTML]="html()" (click)="onClick($event)"></div>`,
  styles: [`:host { display: block; min-width: 0; } .md-view { overflow-wrap: anywhere; }`],
})
export class MarkdownViewComponent {
  private readonly sanitizer = inject(DomSanitizer);

  /** Markdown source. */
  readonly source = input('');
  /** Workspace folder of the document; relative links resolve against it. */
  readonly baseDir = input('/');
  /** A Workspace path the reader followed. */
  readonly openPath = output<string>();

  protected readonly html = computed(() =>
    // renderMarkdown escapes every source character before emitting its own tags.
    this.sanitizer.bypassSecurityTrustHtml(renderMarkdown(this.source(), { workspaceBase: this.baseDir() })),
  );

  protected onClick(event: Event): void {
    const target = event.target as HTMLElement | null;
    const link = target?.closest<HTMLAnchorElement>('a[data-ws-path]');
    if (link) {
      event.preventDefault();
      const path = link.getAttribute('data-ws-path');
      if (path) this.openPath.emit(path);
      return;
    }
    const copy = target?.closest<HTMLButtonElement>('[data-md-copy]');
    if (!copy) return;
    const code = copy.closest('.md-code')?.querySelector('code')?.textContent ?? '';
    const flash = (label: string) => {
      copy.textContent = label;
      setTimeout(() => (copy.textContent = 'copy'), 1500);
    };
    const written = navigator.clipboard?.writeText ? navigator.clipboard.writeText(code) : Promise.reject(new Error('no clipboard'));
    written.then(() => flash('copied')).catch(() => flash('press Ctrl+C'));
  }
}
