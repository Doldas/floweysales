import { Injectable, computed, inject } from '@angular/core';
import { AuthState } from '../auth.service';

/**
 * What the current session may see. Mirrors the server's authorisation:
 * SYS_ADMIN (or auth disabled) gets the live terminal; USER accounts get
 * public-market research plus whatever flow permissions their role grants.
 */
@Injectable({ providedIn: 'root' })
export class Access {
  private readonly auth = inject(AuthState);

  readonly isAdmin = computed(() => !this.auth.enabled() || this.auth.user()?.role === 'SYS_ADMIN');
  private has(permission: string): boolean {
    return this.isAdmin() || this.auth.permissions().includes(permission);
  }
  readonly liveMarkets = computed(() => this.isAdmin());
  readonly flowsRead = computed(() => this.has('flows.read'));
  readonly flowsWrite = computed(() => this.has('flows.write'));
  readonly flowsRun = computed(() => this.has('flows.run'));
  /** Private userspace files require a signed-in account. */
  readonly workspace = computed(() => this.auth.enabled() && !!this.auth.user());
  readonly paperApprovals = computed(() => this.auth.enabled() && this.auth.user()?.role === 'SYS_ADMIN');
  readonly settings = computed(() => this.auth.user()?.role === 'SYS_ADMIN');
  readonly notifications = computed(() => this.auth.enabled() && !!this.auth.user());
  readonly inbox = computed(() => this.flowsRead() || this.paperApprovals());
}
