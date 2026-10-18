import { Component, inject, signal } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatCardModule } from '@angular/material/card';
import { AuthState } from '../auth.service';

@Component({
  selector: 'fse-login',
  standalone: true,
  imports: [CommonModule, FormsModule, MatButtonModule, MatFormFieldModule, MatInputModule, MatCardModule],
  template: `
  <main class="auth-page">
    <mat-card class="auth-card">
      <mat-card-header><mat-card-title>FloweySales · Sign in</mat-card-title></mat-card-header>
      <mat-card-content>
        <p class="auth-caption">Owner access to your local financial research terminal.</p>
        <form (ngSubmit)="submit()">
          <mat-form-field appearance="outline">
            <mat-label>Email</mat-label>
            <input matInput name="email" type="email" autocomplete="username" [(ngModel)]="email" required>
          </mat-form-field>
          <mat-form-field appearance="outline">
            <mat-label>Password</mat-label>
            <input matInput name="password" type="password" autocomplete="current-password" [(ngModel)]="password" required>
          </mat-form-field>
          @if (message()) { <p class="auth-error" role="alert">{{ message() }}</p> }
          <button mat-flat-button color="primary" type="submit" [disabled]="busy() || !email || !password">
            {{ busy() ? 'Signing in…' : 'Sign in' }}
          </button>
        </form>
      </mat-card-content>
    </mat-card>
  </main>
  `,
  styles: [`
    .auth-page{min-height:100vh;display:grid;place-items:center;padding:1.5rem;background:#0b1424}
    .auth-card{width:min(100%,430px);padding:1.5rem}
    mat-card-content{padding-top:1.5rem}
    .auth-caption{font-size:14px;opacity:.75;margin-bottom:1.5rem}
    form,mat-form-field{display:block;width:100%}
    .auth-error{color:#e65454;font-size:14px}
    button{width:100%;margin-top:.5rem}
  `]
})
export class LoginComponent {
  private readonly auth = inject(AuthState);
  readonly busy = signal(false);
  readonly message = signal('');
  email = '';
  password = '';
  async submit(): Promise<void> {
    if (this.busy() || !this.email || !this.password) return;
    this.busy.set(true);
    this.message.set('');
    try {
      await this.auth.login(this.email.trim(), this.password);
    } catch (err) {
      this.message.set(err instanceof Error ? err.message : 'Sign-in failed');
    } finally {
      this.password = '';
      this.busy.set(false);
    }
  }
}
