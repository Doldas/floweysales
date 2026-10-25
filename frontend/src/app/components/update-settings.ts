import { Component, DestroyRef, inject, signal } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { comparisonLabel, isUpdateRunning, shortCommit, stateLabel, stepProgress } from './update-status';

interface ImageInfo { image: string; tag: string; digest: string; commit?: string; created?: string }
interface Commit { sha: string; title: string; url?: string }
interface Changes { status: string; commits: Commit[]; total_commits: number; migrations: string[]; compare_url?: string }
interface CheckResult { comparison: string; latest?: ImageInfo; changes?: Changes; changes_error?: string; error?: string; checked_at: string }
interface Agent { engine: string; installed_at?: string; last_run_at?: string; timer: boolean; backups: boolean }
interface Status { state: string; message?: string; request_id?: string; trigger?: string; started_at?: string; finished_at?: string; from_commit?: string; to_commit?: string; backup?: string; log_tail?: string[] }
interface Overview { current: { commit?: string; build_time?: string; image: string; tag: string }; check?: CheckResult; can_apply: boolean; agent?: Agent; agent_error?: string; status: Status; status_error?: string }

/** Polling interval while an update runs; the API is briefly unreachable during the restart. */
const POLL_MS = 3000;

@Component({
 selector: 'fse-update-settings',
 standalone: true,
 imports: [MatButtonModule, MatCardModule],
 template: `
 @if(overview();as o){
 <div class="cards">
  <mat-card>
   <h3>Installed version</h3>
   <dl>
    <dt>Build</dt><dd><code>{{shortCommit(o.current.commit)}}</code>@if(o.current.build_time){ · built {{when(o.current.build_time)}}}</dd>
    <dt>Update channel</dt><dd><code>{{o.current.image}}:{{o.current.tag}}</code></dd>
   </dl>
   <div class="actions">
    <button mat-flat-button (click)="check()" [disabled]="busy()||running()">Check for updates</button>
   </div>
   @if(o.check;as c){
    <p role="status" class="result" [class.available]="c.comparison==='update_available'" [class.problem]="c.comparison==='check_failed'">{{comparisonLabel(c.comparison)}}</p>
    @if(c.error){<p class="warning">{{c.error}}</p>}
    <p class="muted">Last checked {{when(c.checked_at)}}</p>
   } @else {<p class="muted">{{comparisonLabel(undefined)}}</p>}
  </mat-card>

  <mat-card>
   <h3>Host updater</h3>
   @if(o.agent;as a){
    <p>Installed for <strong>{{a.engine}}</strong> · {{a.timer?'also updates four times daily':'updates only when requested here'}} · database backups {{a.backups?'on':'off'}}</p>
    @if(a.last_run_at){<p class="muted">Last run {{when(a.last_run_at)}}</p>}
   } @else {
    <p class="warning">The host updater is not installed, so updates can be checked but not installed from here.</p>
    <p>On the server, in the FloweySales directory, run:</p>
    <pre>bash scripts/install-auto-update.sh docker   # or podman; add --no-timer for on-request only
docker compose -f compose.deploy.yml up -d app</pre>
   }
   @if(o.status_error){<p class="warning">{{o.status_error}}</p>}
  </mat-card>
 </div>

 @if(o.check?.latest;as latest){
 <mat-card class="wide">
  <h3>Available version</h3>
  <p><code>{{shortCommit(latest.commit)}}</code>@if(latest.created){ · published {{when(latest.created)}}}</p>
  @if(o.check?.changes;as ch){
   @if(ch.commits.length){
    <h4>{{ch.total_commits}} change{{ch.total_commits===1?'':'s'}}@if(ch.compare_url){ · <a [href]="ch.compare_url" target="_blank" rel="noopener">compare on GitHub</a>}</h4>
    <ul class="commits">@for(c of ch.commits;track c.sha){<li><code>{{c.sha.slice(0,7)}}</code> {{c.title}}</li>}</ul>
    @if(ch.total_commits>ch.commits.length){<p class="muted">Showing the newest {{ch.commits.length}}.</p>}
   }
   <h4>Database migrations</h4>
   @if(ch.migrations.length){
    <p>This update adds {{ch.migrations.length}} Liquibase changeset file{{ch.migrations.length===1?'':'s'}}. They are applied after a backup and before the restart:</p>
    <ul class="commits">@for(m of ch.migrations;track m){<li><code>{{m}}</code></li>}</ul>
   } @else {<p>No new database migrations.</p>}
  }
  @if(o.check?.changes_error){<p class="muted">The change list is unavailable ({{o.check?.changes_error}}); the image can still be installed.</p>}
  @if(o.check?.comparison!=='up_to_date' && o.check?.comparison!=='newer_than_latest'){
   <div class="actions">
    <button mat-flat-button color="primary" (click)="apply()" [disabled]="busy()||!o.can_apply">Download and install update</button>
    @if(!o.agent){<span class="muted">Install the host updater first.</span>}
   </div>
   <p class="muted">Installing backs up MySQL to <code>backups/</code>, downloads the images, runs the migrations, restarts the application (a short interruption) and checks its health. If the new version is unhealthy the previous image is restored; migrations are additive and stay applied.</p>
  }
 </mat-card>
 }

 @if(o.status.state!=='idle'){
 <mat-card class="wide" aria-live="polite">
  <h3>{{running()?'Updating…':'Last update'}}</h3>
  <p role="status" class="result" [class.problem]="o.status.state==='failed'||o.status.state==='rolled_back'" [class.available]="o.status.state==='succeeded'">
   {{stateLabel(o.status.state)}}@if(o.status.message){ · {{o.status.message}}}</p>
  @if(running()||o.status.state==='succeeded'){
   <ol class="steps">@for(s of stepProgress(o.status.state);track s.label){<li [class]="s.progress">{{s.label}}</li>}</ol>
  }
  @if(restarting()){<p class="muted">The application is restarting; this page reconnects automatically.</p>}
  <p class="muted">
   @if(o.status.trigger){Started by {{o.status.trigger==='ui'?'request from Settings':o.status.trigger}}}
   @if(o.status.started_at){ · {{when(o.status.started_at)}}}
   @if(o.status.finished_at){ · finished {{when(o.status.finished_at)}}}
   @if(o.status.from_commit||o.status.to_commit){ · <code>{{shortCommit(o.status.from_commit)}}</code> → <code>{{shortCommit(o.status.to_commit)}}</code>}
  </p>
  @if(o.status.backup){<p>Backup: <code>{{o.status.backup}}</code></p>}
  @if(o.status.state==='succeeded' && o.status.to_commit && o.current.commit && !o.status.to_commit.startsWith(o.current.commit)){
   <p class="warning">This page was loaded from the previous version. <button mat-stroked-button (click)="reload()">Reload</button></p>
  }
  @if(o.status.log_tail?.length){<details [open]="o.status.state==='failed'||o.status.state==='rolled_back'"><summary>Updater log</summary><pre>{{o.status.log_tail!.join('\\n')}}</pre></details>}
 </mat-card>
 }
 } @else {<p class="muted">{{error()||'Loading update information…'}}</p>}
 @if(message()){<p role="status">{{message()}}</p>}
 `,
 styles: [`
 :host{display:flex;flex-direction:column;gap:18px}
 .cards{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,360px),1fr));gap:18px}
 mat-card{display:flex;flex-direction:column;gap:10px;padding:22px;background:var(--mat-sys-surface-container, #182d3f);min-width:0}
 h3,h4{margin:0}
 dl{display:grid;grid-template-columns:max-content 1fr;gap:6px 14px;margin:0}dt{opacity:.8}dd{margin:0;overflow-wrap:anywhere}
 code{font-size:13px;overflow-wrap:anywhere}
 pre{white-space:pre-wrap;overflow-wrap:anywhere;background:#0f2232;border:1px solid #3d5d77;border-radius:7px;padding:10px;margin:0;font-size:12px;max-height:320px;overflow:auto}
 .actions{display:flex;flex-wrap:wrap;align-items:center;gap:10px}
 .result{margin:0;padding:10px 12px;border-left:4px solid #73baff;background:#1b3854}
 .result.available{border-color:#5fd39a;background:#183f33}
 .result.problem{border-color:#e36a6a;background:#45222a}
 .warning{padding:12px;border-left:4px solid #e3ac54;background:#42321f;color:#fff;margin:0}
 .muted{opacity:.8;margin:0}
 .commits{margin:0;padding-left:20px;display:flex;flex-direction:column;gap:4px;overflow-wrap:anywhere}
 .steps{margin:0;padding-left:22px;display:flex;flex-direction:column;gap:4px}
 .steps li.done{color:#8fe0b5}.steps li.done::marker{content:'✓  '}
 .steps li.active{font-weight:600;color:#bfe2ff}.steps li.active::marker{content:'●  '}
 .steps li.pending{opacity:.6}
 a{color:#9fd3ff}
 `],
})
export class UpdateSettingsComponent {
 readonly overview = signal<Overview | null>(null);
 readonly busy = signal(false);
 readonly message = signal('');
 readonly error = signal('');
 /** True while the API is unreachable during an update's restart. */
 readonly restarting = signal(false);
 readonly comparisonLabel = comparisonLabel;
 readonly stateLabel = stateLabel;
 readonly shortCommit = shortCommit;
 readonly stepProgress = stepProgress;
 private timer: ReturnType<typeof setTimeout> | undefined;

 constructor() {
  inject(DestroyRef).onDestroy(() => clearTimeout(this.timer));
  void this.load();
 }

 running(): boolean { return isUpdateRunning(this.overview()?.status.state) || this.restarting(); }

 when(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
 }

 private async api(url: string, init?: RequestInit): Promise<Overview & { request?: unknown; overview?: Overview }> {
  const res = await fetch(url, { credentials: 'same-origin', cache: 'no-store', ...init, headers: { 'Content-Type': 'application/json', ...init?.headers } });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || 'HTTP ' + res.status);
  return data;
 }

 private schedulePoll(): void {
  clearTimeout(this.timer);
  if (this.running()) this.timer = setTimeout(() => void this.load(true), POLL_MS);
 }

 async load(polling = false): Promise<void> {
  try {
   this.overview.set(await this.api('/api/admin/updates'));
   this.restarting.set(false);
   this.error.set('');
  } catch (e) {
   // During an update the app restarts; keep polling instead of failing.
   if (polling) this.restarting.set(true);
   else this.error.set(String(e));
  }
  this.schedulePoll();
 }

 async check(): Promise<void> {
  this.busy.set(true); this.message.set('');
  try { this.overview.set(await this.api('/api/admin/updates/check', { method: 'POST' })); }
  catch (e) { this.message.set(String(e)); }
  finally { this.busy.set(false); }
 }

 async apply(): Promise<void> {
  const o = this.overview();
  if (!o?.check) return;
  const migrations = o.check.changes?.migrations.length ?? 0;
  const note = migrations ? `${migrations} database migration file(s) will be applied after a backup.` : 'No new database migrations are listed.';
  if (!confirm(`Install ${shortCommit(o.check.latest?.commit)} now? ${note} The application restarts briefly.`)) return;
  this.busy.set(true); this.message.set('');
  try {
   const data = await this.api('/api/admin/updates/apply', { method: 'POST', body: JSON.stringify({ commit: o.check.latest?.commit ?? '' }) });
   if (data.overview) this.overview.set(data.overview);
   this.message.set('Update requested; the host updater starts it within seconds.');
  } catch (e) { this.message.set(String(e)); }
  finally { this.busy.set(false); this.schedulePoll(); }
 }

 reload(): void { location.reload(); }
}
