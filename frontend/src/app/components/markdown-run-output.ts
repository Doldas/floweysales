import { Component, Input } from '@angular/core';
import { CommonModule } from '@angular/common';

interface RunStep { node?: string; label?: string; output?: string; variable?: string }

// Strictly local formatter: never trusts model output as markup and never loads remote content.
function escapeHtml(text: string): string {
  return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}
export function renderRunMarkdown(text: string): string {
  const escaped = escapeHtml(text.slice(0, 12000));
  const inline = (value: string) => value
    .replace(/\*\*([^\n*]+)\*\*/g, '<strong>$1</strong>')
    .replace(/\*([^\n*]+)\*/g, '<em>$1</em>')
    .replace(/`([^`\n]+)`/g, '<code>$1</code>');
  const lines = escaped.split(/\r?\n/);
  let inCode = false;
  let inList = false;
  const rendered: string[] = [];
  for (const line of lines) {
    if (line.trim().startsWith('```')) {
      if (inList) { rendered.push('</ul>'); inList = false; }
      rendered.push(inCode ? '</code></pre>' : '<pre><code>');
      inCode = !inCode;
    } else if (inCode) {
      rendered.push(line + '\n');
    } else if (/^\s*[-*] /.test(line)) {
      if (!inList) { rendered.push('<ul>'); inList = true; }
      rendered.push('<li>' + inline(line.replace(/^\s*[-*] /, '')) + '</li>');
    } else {
      if (inList) { rendered.push('</ul>'); inList = false; }
      const heading = /^(#{1,3})\s+(.+)$/.exec(line);
      if (heading) {
        const level = heading[1].length;
        rendered.push('<h' + level + '>' + inline(heading[2]) + '</h' + level + '>');
      } else if (line.trim()) {
        rendered.push('<p>' + inline(line) + '</p>');
      }
    }
  }
  if (inList) rendered.push('</ul>');
  if (inCode) rendered.push('</code></pre>');
  return rendered.join('');
}
@Component({
  selector: 'fse-markdown-run-output',
  standalone: true,
  imports: [CommonModule],
  template: `
    @if(steps.length) {
      <div class="run-steps">
        @for(step of steps; track $index) {
          <article class="run-step">
            <div class="step-meta"><span class="step-index">{{ $index + 1 }}</span><strong>{{ step.label || step.node || 'Step' }}</strong><small>{{ step.node }}</small></div>
            @if(step.variable){<p class="variable">Saved variable: {{ step.variable }}</p>}
            <div class="markdown-output" [innerHTML]="render(step.output || '')"></div>
          </article>
        }
      </div>
    } @else { <div class="markdown-output" [innerHTML]="render(raw)"></div> }
  `,
  styles: [`
    .run-steps{display:grid;gap:12px;margin:12px 0}
    .run-step{border:1px solid #365772;border-radius:9px;padding:14px;min-width:0}
    .step-meta{display:flex;align-items:center;flex-wrap:wrap;gap:9px;margin-bottom:10px}
    .step-meta small{opacity:.65}
    .step-index{border-radius:100%;background:#335b78;padding:4px 8px}
    .variable{font-size:12px;opacity:.7}
    .markdown-output{overflow-wrap:anywhere;line-height:1.65}
    .markdown-output :is(pre){overflow:auto;max-height:360px;white-space:pre-wrap;background:#0b1c2e;padding:12px;border-radius:6px}
    .markdown-output :is(code){font-family:monospace}
  `]
})
export class MarkdownRunOutputComponent {
  @Input() raw = '';
  get steps(): RunStep[] {
    try {
      const parsed: unknown = JSON.parse(this.raw);
      if (!Array.isArray(parsed)) return [];
      return parsed.slice(0, 32).filter((step): step is RunStep =>
        step !== null && typeof step === 'object' && typeof step.output === 'string');
    } catch { return []; }
  }
  render(text: string): string { return renderRunMarkdown(text); }
}
