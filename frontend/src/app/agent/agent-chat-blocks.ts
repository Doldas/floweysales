import type { ChatEntry } from '../core/agent-session';

export type AgentChatBlock =
  | { kind: 'user' | 'assistant'; entry: ChatEntry }
  | { kind: 'trace'; entries: ChatEntry[]; key: number };

/**
 * Collapse only consecutive tool calls and reasoning-only messages.
 * Final assistant answers remain outside the disclosure and always visible.
 * Build fresh arrays so streaming updates cannot mutate previous computed state.
 */
export function groupAgentBlocks(entries: readonly ChatEntry[]): AgentChatBlock[] {
  const blocks: AgentChatBlock[] = [];
  for (const entry of entries) {
    if (entry.role === 'tool' || (entry.role === 'assistant' && !!entry.thinking && !entry.content)) {
      const last = blocks.at(-1);
      if (last?.kind === 'trace') last.entries.push(entry);
      else blocks.push({ kind: 'trace', entries: [entry], key: entry.id });
    } else {
      blocks.push({ kind: entry.role, entry });
    }
  }
  return blocks;
}
