import { Component, OnInit, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { adapterProblem, curlExample, deliveryPath, deliverySummary, type AdapterDelivery, type EventAdapter } from './event-adapters';

interface Trigger { id: number; event_key: string; payload_type: string }

/** Signed inbound webhooks that deliver third-party events to one typed trigger. */
@Component({
 selector:'fse-flow-event-adapters', host:{class:'fs-native-forms'},
 standalone:true,
 imports:[FormsModule],
 template:`
 <section class="adapters" aria-label="Inbound webhooks">
  <h3>Inbound webhooks</h3>
  <p class="muted">An adapter gives a third-party service its own signed URL for one event. Each delivery is checked against an HMAC-SHA256 signature, a five-minute timestamp window and a unique delivery ID, then queues every enabled flow subscribed to the event exactly once. Unattended flows with side effects still need to pass the readiness gate.</p>
  <form class="adapter-form" (submit)="$event.preventDefault();create()">
   <label>Name <input name="name" [ngModel]="name()" (ngModelChange)="name.set($event)" maxlength="100" placeholder="Price alerts"></label>
   <label>Event <select name="event" [ngModel]="eventKey()" (ngModelChange)="eventKey.set($event)">
    <option value="">Choose a trigger…</option>
    @for(t of triggers();track t.id){<option [value]="t.event_key">{{t.event_key}} · {{t.payload_type}}</option>}
   </select></label>
   <label>Payload pointer <input name="pointer" [ngModel]="pointer()" (ngModelChange)="pointer.set($event)" maxlength="200" placeholder="/data/value (empty: whole body)"></label>
   <label>Max deliveries / minute <input name="rate" type="number" min="1" max="600" [ngModel]="rate()" (ngModelChange)="rate.set(+$event)"></label>
   <button class="btn btn-sm btn-outline-secondary" type="submit" [disabled]="busy()">Create adapter</button>
  </form>
  @if(!triggers().length){<p class="muted">Create a custom event trigger above first.</p>}
  @if(message()){<p class="adapter-message" role="status">{{message()}}</p>}
  @if(revealed(); as r){
   <div class="secret" role="alert">
    <strong>Signing secret for {{r.name}} — shown only now</strong>
    <code>{{r.secret}}</code>
    <p class="muted">Store it in the sending service. Rotating the secret invalidates this one immediately.</p>
    <details><summary>Example signed request</summary><pre>{{curl(r.adapter,r.secret)}}</pre></details>
    <button class="btn btn-sm btn-outline-secondary" type="button" (click)="revealed.set(null)">I have stored it</button>
   </div>
  }
  @if(!adapters().length){<p class="muted">No adapters yet.</p>}
  <ul class="adapter-list">
   @for(a of adapters();track a.id){
    <li class="adapter" [class.disabled]="!a.enabled">
     <div class="adapter-head"><strong>{{a.name}}</strong><span class="pill">{{a.enabled?'Enabled':'Disabled'}}</span><span class="muted">→ {{a.event_key}}</span></div>
     <p class="meta"><code>{{path(a)}}</code></p>
     <p class="meta">Pointer {{a.payload_pointer||'(whole body)'}} · {{a.rate_per_minute}}/min · {{a.accepted}} accepted · {{a.rejected}} rejected@if(a.last_delivery_at){ · last {{time(a.last_delivery_at)}}} · secret from {{time(a.secret_rotated_at)}}</p>
     <div class="buttons">
      <button class="btn btn-sm btn-outline-secondary" type="button" [disabled]="busy()" (click)="toggle(a)">{{a.enabled?'Disable':'Enable'}}</button>
      <button class="btn btn-sm btn-outline-secondary" type="button" [disabled]="busy()" (click)="rotate(a)">Rotate secret</button>
      <button class="btn btn-sm btn-outline-secondary" type="button" [disabled]="busy()" (click)="loadDeliveries(a)">Deliveries</button>
      <button class="btn btn-sm btn-outline-secondary" type="button" [disabled]="busy()" (click)="remove(a)">Delete</button>
     </div>
     @if(deliveries()[a.id]; as list){
      @if(!list.length){<p class="muted">No deliveries received yet.</p>}
      <ul class="deliveries">
       @for(d of list;track d.id){<li [attr.data-status]="d.status">{{time(d.received_at)}} · {{summary(d)}}</li>}
      </ul>
     }
    </li>
   }
  </ul>
 </section>`,
 styles:[`
 .adapters{display:flex;flex-direction:column;gap:10px;margin-top:18px;color:var(--fs-text)}.adapters h3{margin:0}
 .adapter-form{display:flex;flex-wrap:wrap;gap:12px;align-items:end}
 label{display:flex;flex-direction:column;gap:5px;min-width:150px;flex:1}
 input,select{background:var(--fs-surface);color:var(--fs-text);border:1px solid var(--fs-border-strong);border-radius:6px;padding:8px;font:inherit}
 .adapter-message{background:var(--fs-surface-3);border:1px solid var(--fs-border-strong);border-radius:8px;padding:10px;margin:0}
 .secret{display:flex;flex-direction:column;gap:8px;background:var(--fs-surface-3);border:1px solid color-mix(in srgb, var(--fs-warn) 55%, transparent);border-radius:8px;padding:12px}
 .secret code{overflow-wrap:anywhere;font-size:14px;color:var(--fs-text)}
 pre{white-space:pre-wrap;word-break:break-all;background:var(--fs-surface);border-radius:6px;padding:10px;margin:6px 0 0}
 .adapter-list,.deliveries{list-style:none;margin:0;padding:0;display:flex;flex-direction:column;gap:8px}
 .adapter{border:1px solid var(--fs-border);border-radius:10px;padding:12px;background:var(--fs-surface-2);min-width:0}.adapter.disabled{opacity:.75}
 .adapter-head{display:flex;gap:10px;align-items:center;flex-wrap:wrap;overflow-wrap:anywhere}
 .pill{font-size:12px;font-weight:700;border-radius:999px;padding:3px 9px;background:var(--fs-surface-3)}
 .meta{margin:6px 0 0;font-size:13px;color:var(--fs-muted);overflow-wrap:anywhere}
 .buttons{display:flex;gap:8px;flex-wrap:wrap;margin-top:8px}
 .deliveries{margin-top:8px;font-size:13px}.deliveries li{border-left:3px solid color-mix(in srgb, var(--fs-pos) 50%, transparent);padding-left:8px;overflow-wrap:anywhere}
 .deliveries li[data-status=REJECTED]{border-color:color-mix(in srgb, var(--fs-neg) 50%, transparent)}
 .muted{color:var(--fs-muted);margin:0}`]
})
export class FlowEventAdaptersComponent implements OnInit {
 readonly adapters=signal<EventAdapter[]>([]);
 readonly triggers=signal<Trigger[]>([]);
 readonly deliveries=signal<Record<number,AdapterDelivery[]>>({});
 readonly revealed=signal<{name:string;secret:string;adapter:EventAdapter}|null>(null);
 readonly name=signal('');
 readonly eventKey=signal('');
 readonly pointer=signal('');
 readonly rate=signal(30);
 readonly busy=signal(false);
 readonly message=signal('');
 readonly path=deliveryPath;
 readonly summary=deliverySummary;

 ngOnInit(){this.refresh()}
 time(value?:string){return value?new Date(value).toLocaleString():''}
 curl(a:EventAdapter,secret:string){return curlExample(location.origin,a,secret)}

 async refresh(){
  try{
   const [a,t]=await Promise.all([this.request('/api/admin/flow-event-adapters'),this.request('/api/admin/blueprint-triggers')]);
   this.adapters.set(a.adapters||[]);this.triggers.set(t.triggers||[]);
  }catch(e){this.message.set((e as Error).message)}
 }

 async create(){
  const problem=adapterProblem(this.name(),this.eventKey(),this.pointer(),this.rate());
  if(problem){this.message.set(problem);return}
  await this.run(async()=>{
   const body=await this.request('/api/admin/flow-event-adapters',{method:'POST',body:JSON.stringify({name:this.name(),event_key:this.eventKey(),payload_pointer:this.pointer(),rate_per_minute:this.rate()})});
   this.revealed.set({name:body.name,secret:body.secret,adapter:body.adapter});
   this.name.set('');this.pointer.set('');
   this.message.set('Adapter created.');
  });
 }

 async toggle(a:EventAdapter){
  await this.run(async()=>{
   await this.request('/api/admin/flow-event-adapters/'+a.id,{method:'PUT',body:JSON.stringify({name:a.name,payload_pointer:a.payload_pointer,rate_per_minute:a.rate_per_minute,enabled:!a.enabled})});
   this.message.set(a.enabled?'Adapter disabled; its URL now answers 404.':'Adapter enabled.');
  });
 }

 async rotate(a:EventAdapter){
  if(!confirm('Rotate the secret for '+a.name+'? Deliveries signed with the old secret are refused from now on.'))return;
  await this.run(async()=>{
   const body=await this.request('/api/admin/flow-event-adapters/'+a.id+'/rotate-secret',{method:'POST'});
   this.revealed.set({name:a.name,secret:body.secret,adapter:a});
   this.message.set('Secret rotated.');
  });
 }

 async remove(a:EventAdapter){
  if(!confirm('Delete '+a.name+' and its delivery log? Its URL stops working.'))return;
  await this.run(async()=>{
   await this.request('/api/admin/flow-event-adapters/'+a.id,{method:'DELETE'});
   this.message.set('Adapter deleted.');
  });
 }

 async loadDeliveries(a:EventAdapter){
  try{
   const body=await this.request('/api/admin/flow-event-adapters/'+a.id+'/deliveries');
   this.deliveries.update(d=>({...d,[a.id]:body.deliveries||[]}));
  }catch(e){this.message.set((e as Error).message)}
 }

 private async run(action:()=>Promise<void>){
  this.busy.set(true);
  try{await action()}catch(e){this.message.set((e as Error).message)}
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
