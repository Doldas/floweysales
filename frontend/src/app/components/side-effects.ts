/**
 * Pure helpers for flow side effects: the capability a node uses, approval
 * settings and the audit list. Free of Angular so they run under node --test.
 */

export type SideEffectStatus = 'PENDING' | 'APPROVED' | 'APPLIED' | 'REJECTED' | 'EXPIRED' | 'CANCELLED';

export interface SideEffect {
  id: number; run_id: number; flow_id: number; flow_name: string; node_id: string; node_label: string;
  capability: string; target: string; approval_required: boolean; status: SideEffectStatus;
  payload_sha256: string; payload_bytes: number; payload_preview: string; content_type?: string;
  requested_at: string; decided_by_email?: string; decided_at?: string; decision_note?: string; applied_at?: string;
}

export interface CapabilityNode { id: string; type: string; label?: string; value?: string; event?: string; approval?: string }
export interface CapabilityUse { node: string; label: string; capability: string; target: string; approvalRequired: boolean }

/** The capability and fixed scope of a side-effect node, or null for nodes without side effects. */
export function nodeCapability(n: CapabilityNode): { capability: string; target: string } | null {
  if (n.type === 'workspace_write') return { capability: 'workspace.write', target: n.value || '' };
  if (n.type === 'emit_event') return { capability: 'events.emit', target: n.event || '' };
  return null;
}

/** Every side effect a flow can perform, in node order. */
export function capabilityManifest(nodes: readonly CapabilityNode[]): CapabilityUse[] {
  const uses: CapabilityUse[] = [];
  for (const n of nodes) {
    const c = nodeCapability(n);
    if (c) uses.push({ node: n.id, label: n.label || n.id, ...c, approvalRequired: n.approval === 'required' });
  }
  return uses;
}

/** Plain-language description of what a capability lets the node do. */
export function describeCapability(capability: string, target: string): string {
  if (capability === 'workspace.write') return `Write the workspace file ${target || '(no path yet)'}`;
  if (capability === 'events.emit') return `Emit event ${target || '(no event yet)'} to subscribed flows`;
  return `${capability} ${target}`;
}

export function sideEffectStatusLabel(status: string): string {
  switch (status) {
    case 'PENDING': return 'Awaiting approval';
    case 'APPROVED': return 'Approved, applying';
    case 'APPLIED': return 'Applied';
    case 'REJECTED': return 'Rejected';
    case 'EXPIRED': return 'Expired';
    case 'CANCELLED': return 'Cancelled';
    default: return status;
  }
}

/** Only a SYS_ADMIN decides, and only while the request is pending. */
export function canDecide(effect: Pick<SideEffect, 'status'>, isAdmin: boolean): boolean {
  return isAdmin && effect.status === 'PENDING';
}

/** A rejection must say why; approval notes are optional. */
export function decisionProblem(approve: boolean, note: string): string {
  return !approve && note.trim().length < 3 ? 'Give a reason when rejecting.' : '';
}

/** Pending requests first, oldest first, so the longest-waiting request is on top; the rest newest first. */
export function sortForReview(effects: readonly SideEffect[]): SideEffect[] {
  const pending = effects.filter(e => e.status === 'PENDING').sort((a, b) => a.id - b.id);
  const rest = effects.filter(e => e.status !== 'PENDING').sort((a, b) => b.id - a.id);
  return [...pending, ...rest];
}

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MiB`;
}
