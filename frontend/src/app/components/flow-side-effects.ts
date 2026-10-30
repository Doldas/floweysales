import { Component, OnInit, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { AuthState } from '../auth.service';
import { canDecide, decisionProblem, describeAudit, describeCapability, formatBytes, sideEffectStatusLabel, sortForReview, type AuditEntry, type SideEffect } from './side-effects';

/** Side-effect approvals and the audit trail of every workspace write and event emission. */
@Component({
 selector:'fse-flow-side-effects', host:{class:'fs-native-forms'},
 standalone:true,
 imports:[FormsModule],
 template:`
 <section class="effects" aria-label="Side effect approvals and audit trail">
  <header class="effects-top">
   <div><h3>Approvals &amp; audit</h3>
    <p class="muted">Every workspace write and event emission a flow performs is recorded here. Nodes set to require approval park their run until a SYS_ADMIN approves the exact content.</p></div>
   <div class="effects-actions">
    <label>Status <select [ngModel]="status()" (ngModelChange)="status.set($event);refresh()">
     <option value="">All</option><option value="PENDING">Awaiting approval</option><option value="APPLIED">Applied</option>
     <option value="REJECTED">Rejected</option><option value="EXPIRED">Expired</option><option value="CANCELLED">Cancelled</option>
    </select></label>
    <button class="btn btn-sm btn-outline-secondary" type="button" (click)="refresh()" [disabled]="busy()">Refresh</button>
   </div>
  </header>
  @if(isAdmin()){
   <div class="effects-views" role="tablist" aria-label="Audit views">
    <button type="button" role="tab" [attr.aria-selected]="view()==='effects'" [class.selected]="view()==='effects'" (click)="view.set('effects')">Side effects</button>
    <button type="button" role="tab" [attr.aria-selected]="view()==='changes'" [class.selected]="view()==='changes'" (click)="view.set('changes');loadChanges()">Change log</button>
   </div>
  }
  @if(view()==='changes'){
   <p class="muted">Every successful change to flows, functions, triggers, events, runs and approvals, with who made it. Request bodies are kept only as a SHA-256 digest.</p>
   @if(!changes().length){<p class="muted">No changes recorded yet.</p>}
   <ul class="effect-list">
    @for(entry of changes();track entry.id){
     <li class="effect"><div class="effect-head"><strong>{{describeAudit(entry)}}</strong></div>
      <p class="effect-meta">{{entry.actor_email||'unknown user'}} · {{time(entry.created_at)}} · HTTP {{entry.status_code}}@if(entry.summary){ · {{entry.summary}}}</p></li>
    }
   </ul>
   @if(changes().length && changes().length%200===0){<button class="btn btn-sm btn-outline-secondary" type="button" (click)="loadChanges(true)" [disabled]="busy()">Load older changes</button>}
  } @else {
  @if(pendingCount()){<p class="effects-pending" role="status">{{pendingCount()}} {{pendingCount()===1?'request is':'requests are'}} waiting for approval.</p>}
  @if(message()){<p class="effects-message" role="status">{{message()}}</p>}
  @if(!effects().length){<p class="muted">No side effects recorded{{status()?' with this status':''}} yet.</p>}
  <ul class="effect-list">
   @for(effect of effects();track effect.id){
    <li class="effect" [class.effect-pending]="effect.status==='PENDING'">
     <div class="effect-head">
      <span class="effect-status" [attr.data-status]="effect.status">{{statusLabel(effect.status)}}</span>
      <strong>{{describe(effect.capability,effect.target)}}</strong>
     </div>
     <p class="effect-meta">{{effect.flow_name||('Flow #'+effect.flow_id)}} · {{effect.node_label||effect.node_id}} · run #{{effect.run_id}} · {{bytes(effect.payload_bytes)}}{{effect.approval_required?' · approval required':''}}</p>
     <p class="effect-meta">Requested {{time(effect.requested_at)}}@if(effect.decided_by_email){ · {{effect.status==='REJECTED'?'rejected':'approved'}} by {{effect.decided_by_email}}}@if(effect.applied_at){ · applied {{time(effect.applied_at)}}}@if(effect.decision_note){ · “{{effect.decision_note}}”}</p>
     <details (toggle)="onToggle($event,effect)">
      <summary>{{canDecide(effect)?'Review content':'Content preview'}}</summary>
      <pre class="effect-payload">{{reviewed()[effect.id] ?? effect.payload_preview}}</pre>
      <p class="muted effect-digest">SHA-256 {{effect.payload_sha256}}</p>
      @if(canDecide(effect)){
       <label>Note <textarea [ngModel]="notes()[effect.id]||''" (ngModelChange)="setNote(effect.id,$event)" maxlength="500" rows="2" placeholder="Required when rejecting"></textarea></label>
       <div class="effect-buttons">
        <button class="btn btn-sm btn-primary" type="button" [disabled]="busy()" (click)="decide(effect,true)">Approve and resume run</button>
        <button class="btn btn-sm btn-outline-secondary" type="button" [disabled]="busy()" (click)="decide(effect,false)">Reject</button>
       </div>
      }
     </details>
    </li>
   }
  </ul>
  }
 </section>`,
 styles:[`
 .effects{display:flex;flex-direction:column;gap:12px;color:var(--fs-text)}
 .effects-top{display:flex;justify-content:space-between;gap:12px;flex-wrap:wrap}.effects-top h3{margin:0 0 4px;font-size:1rem}.effects-top p{margin:0;max-width:70ch;font-size:.875rem}
 .effects-actions{display:flex;gap:8px;align-items:flex-end;flex-wrap:wrap}.effects-actions label{display:flex;flex-direction:column;gap:4px;font-size:13px;min-width:180px}
 .effects-pending{background:var(--fs-warn-soft);color:var(--fs-warn);border-radius:var(--bs-border-radius);padding:10px 12px;margin:0;font-weight:500}
 .effects-message{background:var(--fs-accent-soft);border-radius:var(--bs-border-radius);padding:10px 12px;margin:0}
 .effect-list{list-style:none;margin:0;padding:0;display:flex;flex-direction:column;gap:10px}
 .effect{border:1px solid var(--fs-border);border-radius:var(--bs-border-radius-lg);padding:12px 14px;background:var(--fs-surface);min-width:0}
 .effect-pending{border-left:3px solid var(--fs-warn)}
 .effect-head{display:flex;gap:10px;align-items:center;flex-wrap:wrap;overflow-wrap:anywhere}
 .effect-status{font-size:11px;font-weight:700;border-radius:999px;padding:3px 9px;background:var(--fs-surface-3);color:var(--fs-text-2)}
 .effect-status[data-status=PENDING]{background:var(--fs-warn-soft);color:var(--fs-warn)}.effect-status[data-status=APPLIED]{background:var(--fs-pos-soft);color:var(--fs-pos)}
 .effect-status[data-status=REJECTED],.effect-status[data-status=EXPIRED]{background:var(--fs-neg-soft);color:var(--fs-neg)}
 .effect-meta{margin:6px 0 0;font-size:13px;color:var(--fs-muted);overflow-wrap:anywhere}
 details{margin-top:8px}summary{cursor:pointer;color:var(--fs-accent-text);font-size:.875rem;font-weight:500}
 .effect-payload{white-space:pre-wrap;word-break:break-word;max-height:260px;overflow:auto;margin:8px 0}
 .effect-digest{font-size:11px;overflow-wrap:anywhere}
 .effects-views{display:inline-flex;gap:2px;padding:3px;background:var(--fs-surface-3);border-radius:var(--bs-border-radius);align-self:flex-start}
 .effects-views button{background:transparent;color:var(--fs-muted);border:0;border-radius:calc(var(--bs-border-radius) - 2px);padding:5px 12px;font:inherit;font-size:.8125rem;font-weight:500}
 .effects-views button.selected{background:var(--fs-surface);color:var(--fs-text);box-shadow:var(--fs-shadow-sm)}
 .effect-buttons{display:flex;gap:8px;flex-wrap:wrap;margin-top:8px}
 .muted{color:var(--fs-muted)}`]
})
export class FlowSideEffectsComponent implements OnInit {
 private readonly auth=inject(AuthState);
 readonly effects=signal<SideEffect[]>([]);
 readonly status=signal('');
 readonly busy=signal(false);
 readonly message=signal('');
 readonly notes=signal<Record<number,string>>({});
 /** Full payloads fetched for review, keyed by side effect ID. */
 readonly reviewed=signal<Record<number,string>>({});
 readonly isAdmin=computed(()=>this.auth.user()?.role==='SYS_ADMIN');
 readonly pendingCount=computed(()=>this.effects().filter(e=>e.status==='PENDING').length);
 readonly view=signal<'effects'|'changes'>('effects');
 readonly changes=signal<AuditEntry[]>([]);
 readonly describeAudit=describeAudit;
 readonly statusLabel=sideEffectStatusLabel;
 readonly describe=describeCapability;
 readonly bytes=formatBytes;

 ngOnInit(){this.refresh()}
 canDecide(effect:SideEffect){return canDecide(effect,this.isAdmin())}
 time(value?:string){return value?new Date(value).toLocaleString():''}
 setNote(id:number,note:string){this.notes.update(n=>({...n,[id]:note}))}

 async refresh(){
  this.busy.set(true);
  try{
   const query=this.status()?'?status='+encodeURIComponent(this.status()):'';
   const body=await this.request('/api/admin/flow-side-effects'+query);
   this.effects.set(sortForReview(body.side_effects||[]));
  }catch(e){this.message.set((e as Error).message)}
  finally{this.busy.set(false)}
 }

 async loadChanges(older=false){
  this.busy.set(true);
  try{
   const last=this.changes().at(-1);
   const body=await this.request('/api/admin/flow-audit'+(older&&last?'?before='+last.id:''));
   this.changes.set(older?[...this.changes(),...(body.entries||[])]:body.entries||[]);
  }catch(e){this.message.set((e as Error).message)}
  finally{this.busy.set(false)}
 }

 async onToggle(event:Event,effect:SideEffect){
  if(!(event.target as HTMLDetailsElement).open||!this.canDecide(effect)||effect.id in this.reviewed())return;
  try{
   const body=await this.request('/api/admin/flow-side-effects/'+effect.id);
   this.reviewed.update(r=>({...r,[effect.id]:body.payload??''}));
  }catch(e){this.message.set((e as Error).message)}
 }

 async decide(effect:SideEffect,approve:boolean){
  const note=this.notes()[effect.id]||'';
  const problem=decisionProblem(approve,note);
  if(problem){this.message.set(problem);return}
  this.busy.set(true);
  try{
   await this.request('/api/admin/flow-side-effects/'+effect.id+'/decision',{method:'POST',body:JSON.stringify({approve,note})});
   this.message.set(approve?'Approved; run #'+effect.run_id+' resumes at '+(effect.node_label||effect.node_id)+'.':'Rejected; run #'+effect.run_id+' failed without applying the change.');
  }catch(e){this.message.set((e as Error).message)}
  finally{this.busy.set(false)}
  await this.refresh();
 }

 private async request(url:string,options?:RequestInit):Promise<any>{
  const r=await fetch(url,{credentials:'same-origin',...options,headers:{'Content-Type':'application/json',...options?.headers}});
  const body=await r.json().catch(()=>({}));
  if(!r.ok)throw new Error(body.error||'Request failed ('+r.status+')');
  return body;
 }
}
