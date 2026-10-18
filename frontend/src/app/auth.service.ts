import { Injectable, signal } from '@angular/core';

export interface AuthUser { id: number; email: string; role: string; }

@Injectable({ providedIn: 'root' })
export class AuthState {
  readonly ready = signal(false);
  readonly enabled = signal(false);
  readonly user = signal<AuthUser | null>(null);
  readonly error = signal('');

  async check(): Promise<void> {
    try {
      const response = await fetch('/api/auth/me', { credentials: 'same-origin', cache: 'no-store' });
      if (response.ok) {
        const data = await response.json();
        this.enabled.set(data.enabled === true);
        this.user.set(data.user ?? null);
      } else if (response.status === 401) {
        this.enabled.set(true);
        this.user.set(null);
      } else {
        throw new Error('Authentication status unavailable: HTTP ' + response.status);
      }
    } catch (err) {
      this.enabled.set(true);
      this.user.set(null);
      this.error.set(err instanceof Error ? err.message : 'Authentication unavailable');
    } finally {
      this.ready.set(true);
    }
  }

  async login(email: string, password: string): Promise<void> {
    const response = await fetch('/api/auth/login', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email, password }),
    });
    if (!response.ok) {
      const data = await response.json().catch(() => null);
      throw new Error(data?.error || 'Login failed');
    }
    const data = await response.json();
    this.user.set(data.user);
    this.error.set('');
    window.location.reload();
  }

  async logout(): Promise<void> {
    await fetch('/api/auth/logout', {
      method: 'POST', credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
    });
    this.user.set(null);
    window.location.reload();
  }
}
