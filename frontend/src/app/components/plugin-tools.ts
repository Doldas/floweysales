// Pure helpers for the plugin tools page (forms, results, downloads). The
// types mirror internal/plugins/tools.go; nothing here touches the DOM, so it
// runs under node --test.

export type ToolInputType = 'text' | 'textarea' | 'number' | 'date' | 'select' | 'checkbox' | 'file' | 'files';

export interface ToolInput {
  name: string; label: string; type: ToolInputType; required?: boolean; default?: string;
  placeholder?: string; help?: string; accept?: string; options?: { value: string; label: string }[];
}
export interface Tool { id: string; label: string; description?: string; submit?: string; inputs?: ToolInput[] }
export interface ToolsTab { label: string; icon?: string; description?: string }
export interface ToolPlugin {
  plugin_id: string; name: string; version: string; terms?: string; ready: boolean; missing?: string[];
  tools: { tab: ToolsTab; tools: Tool[] };
}

export interface ToolOutputFile { name: string; mime: string; encoding: 'utf8' | 'base64'; content: string; description?: string }
export interface ToolBlock {
  type: 'text' | 'notice' | 'table' | 'stats' | 'file'; title?: string; text?: string; level?: 'info' | 'success' | 'warning' | 'error';
  columns?: { label: string; align?: string }[]; rows?: string[][]; file?: ToolOutputFile; stats?: { label: string; value: string; hint?: string }[];
}
export interface ToolResult { title?: string; blocks: ToolBlock[] }

/** A file chosen in a form: its text is read in the browser and never stored. */
export interface PickedFile { name: string; content: string; size: number }
export type FieldValue = string | boolean | PickedFile[];
export type FormValues = Record<string, FieldValue>;

export const DEFAULT_ICON = 'bi-tools';

/** The Bootstrap icon class for a plugin's nav entry. */
export function tabIcon(tab: ToolsTab): string {
  return tab.icon && /^bi-[a-z0-9-]+$/.test(tab.icon) ? tab.icon : DEFAULT_ICON;
}

/** Starting values for a tool's form, from the manifest defaults. */
export function initialValues(tool: Tool): FormValues {
  const out: FormValues = {};
  for (const input of tool.inputs ?? []) {
    if (input.type === 'checkbox') out[input.name] = input.default === 'true';
    else if (input.type === 'files') out[input.name] = [];
    else if (input.type === 'file') out[input.name] = [];
    else out[input.name] = input.default ?? (input.type === 'select' ? input.options?.[0]?.value ?? '' : '');
  }
  return out;
}

export interface CollectedInput { input: Record<string, unknown>; errors: Record<string, string> }

/**
 * Turns form values into the request input and validates them the way the
 * backend will: required fields, numbers, select choices. Empty optional
 * fields are left out.
 */
export function collectInput(tool: Tool, values: FormValues): CollectedInput {
  const input: Record<string, unknown> = {};
  const errors: Record<string, string> = {};
  for (const f of tool.inputs ?? []) {
    const v = values[f.name];
    switch (f.type) {
      case 'checkbox':
        if (v === true) input[f.name] = true;
        break;
      case 'file': {
        const picked = Array.isArray(v) ? v : [];
        if (picked.length) input[f.name] = picked[0].content;
        else if (f.required) errors[f.name] = `${f.label} is required.`;
        break;
      }
      case 'files': {
        const picked = Array.isArray(v) ? v : [];
        if (picked.length) input[f.name] = picked.map(p => ({ name: p.name, content: p.content }));
        else if (f.required) errors[f.name] = `${f.label} is required.`;
        break;
      }
      case 'number': {
        const raw = typeof v === 'string' ? v.trim().replace(',', '.') : '';
        if (!raw) { if (f.required) errors[f.name] = `${f.label} is required.`; break; }
        const n = Number(raw);
        if (!Number.isFinite(n)) errors[f.name] = `${f.label} must be a number.`;
        else input[f.name] = n;
        break;
      }
      case 'select': {
        const s = typeof v === 'string' ? v : '';
        if (!s) { if (f.required) errors[f.name] = `${f.label} is required.`; break; }
        if (!(f.options ?? []).some(o => o.value === s)) errors[f.name] = `${f.label} must be one of the choices.`;
        else input[f.name] = s;
        break;
      }
      default: {
        const s = typeof v === 'string' ? v : '';
        if (!s.trim()) { if (f.required) errors[f.name] = `${f.label} is required.`; break; }
        input[f.name] = s;
      }
    }
  }
  return { input, errors };
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(n < 10 * 1024 ? 1 : 0)} kB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

/** The bytes of a file block (utf8 text or base64), as the plugin meant them. */
export function fileBytes(file: Pick<ToolOutputFile, 'encoding' | 'content'>): Uint8Array {
  if (file.encoding === 'base64') {
    const bin = atob(file.content);
    const out = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
    return out;
  }
  return new TextEncoder().encode(file.content);
}

/** The text of a text/* file block for copying; base64 is decoded as Latin-1 (what the Swedish SRU files use). */
export function fileText(file: Pick<ToolOutputFile, 'encoding' | 'content'>): string {
  if (file.encoding !== 'base64') return file.content;
  return new TextDecoder('iso-8859-1').decode(fileBytes(file));
}

export const isTextFile = (file: Pick<ToolOutputFile, 'mime'>): boolean => file.mime.startsWith('text/');

/** Tab-separated text of a table block, for pasting into a spreadsheet. */
export function tableToTsv(block: Pick<ToolBlock, 'columns' | 'rows'>): string {
  const clean = (s: string) => s.replace(/[\t\r\n]+/g, ' ');
  const lines = [(block.columns ?? []).map(c => clean(c.label)).join('\t')];
  for (const row of block.rows ?? []) lines.push(row.map(clean).join('\t'));
  return lines.join('\n');
}

/** Splits a text block into paragraphs; single line breaks inside one are kept by the page. */
export function paragraphs(text: string): string[] {
  return text.split(/\n{2,}/).map(p => p.trim()).filter(Boolean);
}

const LEVELS = new Set(['info', 'success', 'warning', 'error']);
const BLOCKS = new Set(['text', 'notice', 'table', 'stats', 'file']);

/** Defensive copy of a result: unknown blocks dropped, missing arrays filled in. */
export function normalizeResult(raw: Partial<ToolResult> | null | undefined): ToolResult {
  const blocks: ToolBlock[] = [];
  for (const b of raw?.blocks ?? []) {
    if (!b || !BLOCKS.has(b.type)) continue;
    const block: ToolBlock = { ...b };
    if (b.type === 'notice' && !LEVELS.has(b.level ?? '')) block.level = 'info';
    if (b.type === 'table') {
      block.columns = b.columns ?? [];
      block.rows = (b.rows ?? []).map(r => block.columns!.map((_, i) => r[i] ?? ''));
    }
    if (b.type === 'stats' && !(b.stats?.length)) continue;
    if (b.type === 'file' && !b.file) continue;
    blocks.push(block);
  }
  return { title: raw?.title, blocks };
}

export function alignClass(align?: string): string {
  return align === 'right' ? 'text-end' : '';
}
