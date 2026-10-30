import { Component, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { AuthState } from '../auth.service';

@Component({
  selector: 'fse-login',
  standalone: true,
  imports: [FormsModule],
  template: `
  <main class="auth-page">
    <section class="auth-hero" aria-hidden="true">
      <div class="hero-inner">
        <span class="brand-mark"><i class="bi bi-activity"></i></span>
        <h2>Research that shows its work.</h2>
        <p>Agentic research flows, market analysis and a private workspace. Every figure traced to the tool and filing it came from.</p>
        <ul>
          <li><i class="bi bi-diagram-3"></i> Build multi-step agent flows with approvals and audit trails</li>
          <li><i class="bi bi-graph-up-arrow"></i> Track markets live, with indicators, statements and SEC filings</li>
          <li><i class="bi bi-shield-check"></i> Research only. Nothing trades without a human decision.</li>
        </ul>
      </div>
    </section>
    <section class="auth-panel">
      <div class="auth-card">
        <div class="auth-brand"><span class="brand-mark sm"><i class="bi bi-activity"></i></span><strong>FloweySales</strong></div>
        @if(inviteToken()){
          <h1>Activate your account</h1>
          <p class="auth-caption">Choose a password to activate your FloweySales account.</p>
          <form (ngSubmit)="acceptInvitation()" class="vstack gap-3">
            <div>
              <label class="form-label" for="invitePassword">Password <span class="text-muted">(at least 14 characters)</span></label>
              <input class="form-control form-control-lg" id="invitePassword" name="invitePassword" type="password" autocomplete="new-password" [(ngModel)]="invitePassword" minlength="14" required>
            </div>
            <div>
              <label class="form-label" for="confirmPassword">Confirm password</label>
              <input class="form-control form-control-lg" id="confirmPassword" name="confirmPassword" type="password" autocomplete="new-password" [(ngModel)]="confirmPassword" required>
            </div>
            @if(message()){<div class="alert alert-info py-2 mb-0" role="alert">{{message()}}</div>}
            <button class="btn btn-primary btn-lg w-100" type="submit" [disabled]="busy()||invitePassword.length<14||invitePassword!==confirmPassword">Activate account</button>
          </form>
        } @else {
          <h1>Sign in</h1>
          <p class="auth-caption">Welcome back. Sign in to your workspace.</p>
          <form (ngSubmit)="submit()" class="vstack gap-3">
            <div>
              <label class="form-label" for="email">Email</label>
              <input class="form-control form-control-lg" id="email" name="email" type="email" autocomplete="username" [(ngModel)]="email" required>
            </div>
            <div>
              <label class="form-label" for="password">Password</label>
              <input class="form-control form-control-lg" id="password" name="password" type="password" autocomplete="current-password" [(ngModel)]="password" required>
            </div>
            @if (message()) { <div class="alert alert-danger py-2 mb-0" role="alert">{{ message() }}</div> }
            <button class="btn btn-primary btn-lg w-100" type="submit" [disabled]="busy() || !email || !password">
              @if (busy()) { <span class="spinner-border spinner-border-sm" aria-hidden="true"></span> }
              {{ busy() ? 'Signing in…' : 'Sign in' }}
            </button>
          </form>
        }
        <p class="auth-foot">Research aid, not investment advice.</p>
      </div>
    </section>
  </main>
  `,
  styles: [`
    .auth-page{min-height:100vh;min-height:100dvh;display:grid;grid-template-columns:minmax(0,1.1fr) minmax(0,1fr);background:var(--fs-bg)}
    .auth-hero{position:relative;overflow:hidden;display:flex;align-items:center;padding:clamp(2rem,5vw,4.5rem);background:radial-gradient(900px 500px at 10% 0%,rgba(99,102,241,.35),transparent 60%),radial-gradient(700px 500px at 100% 100%,rgba(6,182,212,.22),transparent 60%),#0b1020;color:#e2e8f0}
    .hero-inner{max-width:520px;display:grid;gap:1.1rem}
    .hero-inner h2{color:#fff;font-size:clamp(1.8rem,3.2vw,2.6rem);letter-spacing:-.02em;margin:.5rem 0 0}
    .hero-inner p{color:#b8c2d6;font-size:1.05rem;margin:0}
    .hero-inner ul{list-style:none;padding:0;margin:.5rem 0 0;display:grid;gap:.8rem}
    .hero-inner li{display:flex;gap:.7rem;align-items:flex-start;color:#cbd5e1}
    .hero-inner li .bi{color:#a5b4fc;font-size:1.1rem}
    .brand-mark{display:grid;place-items:center;width:3rem;height:3rem;border-radius:.9rem;background:var(--fs-agent-gradient);color:#fff;font-size:1.5rem;box-shadow:0 10px 30px rgba(99,102,241,.45)}
    .brand-mark.sm{width:2.1rem;height:2.1rem;border-radius:.6rem;font-size:1.05rem;box-shadow:none}
    .auth-panel{display:grid;place-items:center;padding:max(1.5rem,env(safe-area-inset-top)) max(1rem,env(safe-area-inset-right)) max(1.5rem,env(safe-area-inset-bottom)) max(1rem,env(safe-area-inset-left))}
    .auth-card{width:min(100%,400px);min-width:0;overflow-wrap:anywhere}
    .auth-brand{display:flex;align-items:center;gap:.6rem;margin-bottom:2rem;font-size:1.05rem}
    h1{font-size:1.6rem;margin:0 0 .35rem}
    .auth-caption{color:var(--fs-muted);margin-bottom:1.5rem}
    .auth-foot{margin-top:2rem;font-size:.78rem;color:var(--fs-muted);text-align:center}
    @media(max-width:900px){.auth-page{grid-template-columns:minmax(0,1fr)}.auth-hero{display:none}}
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
