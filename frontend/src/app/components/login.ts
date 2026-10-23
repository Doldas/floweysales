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
        @if(inviteToken()){
          <p class="auth-caption">Set your password to activate your FloweySales account.</p>
          <form (ngSubmit)="acceptInvitation()">
           <mat-form-field appearance="outline"><mat-label>Password (at least 14 characters)</mat-label>
            <input matInput name="invitePassword" type="password" autocomplete="new-password" [(ngModel)]="invitePassword" minlength="14" required>
           </mat-form-field>
           <mat-form-field appearance="outline"><mat-label>Confirm password</mat-label>
            <input matInput name="confirmPassword" type="password" autocomplete="new-password" [(ngModel)]="confirmPassword" required>
           </mat-form-field>
           @if(message()){<p class="auth-error" role="alert">{{message()}}</p>}
           <button mat-flat-button type="submit" [disabled]="busy()||invitePassword.length<14||invitePassword!==confirmPassword">Activate account</button>
          </form>
        } @else {
        <p class="auth-caption">Sign in to your FloweySales account.</p>
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
        }
      </mat-card-content>
    </mat-card>
  </main>
  `,
  styles: [`
    .auth-page{min-height:100vh;min-height:100dvh;display:grid;place-items:center;padding:max(16px,env(safe-area-inset-top)) max(12px,env(safe-area-inset-right)) max(16px,env(safe-area-inset-bottom)) max(12px,env(safe-area-inset-left));background:#0b1424}
    .auth-card{width:min(100%,430px);min-width:0;padding:clamp(12px,4vw,24px);overflow-wrap:anywhere}
    mat-card-content{padding-top:1.5rem}
    .auth-caption{font-size:14px;opacity:.75;margin-bottom:1.5rem}
    form,mat-form-field{display:block;width:100%}
    .auth-error{color:#e65454;font-size:14px}
    button{width:100%;min-height:48px;margin-top:.5rem}
    input{font-size:16px}
    @media(max-width:390px){mat-card-content{padding-inline:0}.auth-card{padding:14px}}
  `]
})
export class LoginComponent {
  private readonly auth = inject(AuthState);
  readonly busy = signal(false);
  readonly message = signal('');
  readonly inviteToken=signal(new URLSearchParams(typeof window==='undefined'?'':window.location.search).get('invite')||'');
  invitePassword='';
  confirmPassword='';
  async acceptInvitation():Promise<void>{
   if(this.busy()||!this.inviteToken()||this.invitePassword.length<14||this.invitePassword!==this.confirmPassword)return;
   this.busy.set(true);this.message.set('');
   try{
    const response=await fetch('/api/auth/accept-invite',{method:'POST',headers:{'Content-Type':'application/json'},credentials:'same-origin',
      body:JSON.stringify({token:this.inviteToken(),password:this.invitePassword})});
    const result=await response.json().catch(()=>({}));
    if(!response.ok)throw new Error(result.error||'Invitation could not be accepted');
    this.inviteToken.set('');this.invitePassword='';this.confirmPassword='';
    if(typeof window!=='undefined')window.history.replaceState({},'',window.location.pathname);
    this.message.set('Account activated. Sign in with your email and password.');
   }catch(e){this.message.set(e instanceof Error?e.message:'Invitation failed')}
   finally{this.busy.set(false)}
  }

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
