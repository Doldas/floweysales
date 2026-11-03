/** Wording for the World view. Pure, tested with node --test. */
import type { CompanyWorld, WorldHeadline, WorldRole, WorldTheme, WorldView } from '../models';

export const ROLE_LABEL: Record<WorldRole, string> = { benefits: 'Tends to gain', hurt: 'Tends to lose', mixed: 'Cuts both ways' };
export const ROLE_ICON: Record<WorldRole, string> = { benefits: '▲', hurt: '▼', mixed: '◆' };

/** "Gain" and "lose" are relative to the driver rising, so say which driver. */
export function roleLine(role: WorldRole, driver: string): string {
  return role === 'mixed' ? `${ROLE_LABEL[role]} when ${driver} change` : `${ROLE_LABEL[role]} when ${driver} rise`;
}

const UTC_DATE_TIME = new Intl.DateTimeFormat('en-US', { timeZone: 'UTC', year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' });

/** "Nov 3, 2026, 14:32 UTC". Local to this file because node --test cannot resolve extensionless sibling imports. */
function formatUtcDateTime(unixMs: number): string {
  return `${UTC_DATE_TIME.format(unixMs)} UTC`;
}

/** Hours since a headline came out, or undefined when it has no timestamp. */
export function ageHours(h: WorldHeadline, nowMs: number): number | undefined {
  return h.published ? Math.max(0, (nowMs - h.published * 1000) / 3_600_000) : undefined;
}

export function ageLabel(h: WorldHeadline, nowMs: number): string {
  const hours = ageHours(h, nowMs);
  if (hours === undefined) return h.age ?? '';
  if (hours < 1) return 'under an hour ago';
  if (hours < 48) return `${Math.floor(hours)} h ago`;
  return `${Math.floor(hours / 24)} days ago`;
}

/**
 * The date line of a headline: the publisher's date in UTC with the age beside
 * it, or an honest note when no date is known. A relative age from a search
 * result is shown as approximate and never turned into a date.
 */
export function publishedLabel(h: WorldHeadline, nowMs: number): string {
  if (h.published) {
    const age = ageLabel(h, nowMs);
    return `Published ${formatUtcDateTime(h.published * 1000)}${age ? ` (${age})` : ''}`;
  }
  return h.age ? `About ${h.age} (from search; exact date unavailable)` : 'Date unavailable';
}

/** A headline as the agent is given it: title, publisher and date. */
export function headlineText(h: WorldHeadline): string {
  const date = h.published ? formatUtcDateTime(h.published * 1000) : h.age ? `about ${h.age}` : 'date unavailable';
  return `${h.title} (${h.source ? h.source + ', ' : ''}${date})`;
}

export function themeById(w: WorldView, id: string): WorldTheme | undefined {
  return w.themes.find(t => t.id === id);
}

/** Theme labels for the ids in a sector row; ids the view does not know are skipped. */
export function themeLabels(w: WorldView, ids: string[] | undefined): string[] {
  return (ids ?? []).map(id => themeById(w, id)?.label).filter((l): l is string => !!l);
}

/**
 * A prompt that hands the agent exactly these headlines and exposures and
 * asks it to stay inside them.
 */
export function worldPrompt(w: WorldView): string {
  const lines = w.themes.slice(0, 6).map(t =>
    `- ${t.label} (${t.count} headlines): ${t.headlines.slice(0, 3).map(headlineText).join(' | ')}. Exposed: ${t.exposures.filter(x => x.kind !== 'macro').map(x => `${x.name} ${ROLE_LABEL[x.role].toLowerCase()}`).join('; ')}.`);
  return `Summarize what is happening in the world for investors in under 200 words. Use only the headlines (each with its publisher and date) and exposures below, name the themes in order of importance, and say which sectors and commodities each touches. Do not add facts or prices of your own. This is research, not advice.\n\nAs of ${w.generated_at}:\n` + lines.join('\n');
}

export function companyPrompt(c: CompanyWorld): string {
  const lines = c.themes.slice(0, 6).map(t => {
    const own = (t.own_headlines ?? []).map(headlineText).slice(0, 2);
    const world = t.headlines.slice(0, 2).map(headlineText);
    return `- ${t.label}${t.role ? ` (${ROLE_LABEL[t.role].toLowerCase()} when ${t.driver} rise: ${t.why})` : ''}. ${own.length ? `Company headlines: ${own.join(' | ')}. ` : ''}${world.length ? `World headlines: ${world.join(' | ')}.` : ''}`;
  });
  return `Explain how current world events may affect ${c.symbol}${c.sector ? ` (${c.sector})` : ''} in under 200 words. Use only the items below, separate what the headlines say from what is inference, and do not add numbers of your own. This is research, not advice.\n\nAs of ${c.generated_at}:\n` + lines.join('\n');
}
