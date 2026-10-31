import { Component, OnInit, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';

interface Channels { email: { configured: boolean }; webhook: { configured: boolean; provider?: string } }

/** Where Notify nodes deliver: the owner's email (SMTP) or one sealed chat webhook. */
@Component({
 selector:'fse-flow-notify-channels', host:{class:'fs-native-forms'},
 standalone:true,
 imports:[FormsModule],
 template:`
 <section class="channels" aria-label="Notification channels">
  <h3>Notifications</h3>
  <p class="muted">Notify nodes send a flow's result to the flow owner by email, or to one Slack, Discord, Microsoft Teams or generic JSON webhook. The webhook URL is a credential: it is encrypted on the server and never shown again.</p>
  <ul class="status">
   <li><strong>Email</strong> · {{channels().email.configured?'SMTP configured; Notify emails the flow owner':'Not configured: set up SMTP under Settings → Email'}}</li>
   <li><strong>Webhook</strong> · {{channels().webhook.configured?providerLabel(channels().webhook.provider)+' webhook configured':'Not configured'}}</li>
  </ul>
  <form class="channel-form" (submit)="$event.preventDefault();save()">
   <label>Chat service <select name="provider" [ngModel]="provider()" (ngModelChange)="provider.set($event)">
    <option value="slack">Slack</option><option value="discord">Discord</option><option value="teams">Microsoft Teams</option><option value="generic">Generic JSON</option>
   </select></label>
   <label>Webhook URL (https) <input name="url" type="password" autocomplete="off" [ngModel]="url()" (ngModelChange)="url.set($event)" maxlength="2048" placeholder="https://hooks.slack.com/services/…"></label>
   <button class="btn btn-sm btn-outline-secondary" type="submit" [disabled]="busy()||!url().startsWith('https://')">{{channels().webhook.configured?'Replace webhook':'Save webhook'}}</button>
  </form>
  @if(channels().webhook.configured){<div class="buttons">
   <button class="btn btn-sm btn-outline-secondary" type="button" [disabled]="busy()" (click)="test()">Send test message</button>
   <button class="btn btn-sm btn-outline-secondary" type="button" [disabled]="busy()" (click)="remove()">Remove webhook</button>
  </div>}
  @if(message()){<p class="channel-message" role="status">{{message()}}</p>}
 </section>`,
 styles:[`
 .channels{display:flex;flex-direction:column;gap:10px;margin-top:18px;color:var(--fs-text)}.channels h3{margin:0}
 .status{margin:0;padding-left:18px;display:grid;gap:4px}
 .channel-form{display:flex;flex-wrap:wrap;gap:12px;align-items:end}
 label{display:flex;flex-direction:column;gap:5px;min-width:150px;flex:1}
 input,select{background:var(--fs-surface);color:var(--fs-text);border:1px solid var(--fs-border-strong);border-radius:6px;padding:8px;font:inherit}
 .buttons{display:flex;gap:8px;flex-wrap:wrap}
 .channel-message{background:var(--fs-surface-3);border:1px solid var(--fs-border-strong);border-radius:8px;padding:10px;margin:0}
 .muted{color:var(--fs-muted);margin:0}`]
})
export class FlowNotifyChannelsComponent implements OnInit {
 readonly channels=signal<Channels>({email:{configured:false},webhook:{configured:false}});
 readonly provider=signal('slack');
 readonly url=signal('');
 readonly busy=signal(false);
 readonly message=signal('');

 ngOnInit(){this.refresh()}
 providerLabel(p?:string){return p==='teams'?'Microsoft Teams':p==='generic'?'Generic JSON':p?p[0].toUpperCase()+p.slice(1):''}

 async refresh(){
  try{this.channels.set(await this.request('/api/admin/flow-notify-channels'))}catch(e){this.message.set((e as Error).message)}
 }
 save(){return this.run(async()=>{
  await this.request('/api/admin/flow-notify-channels/webhook',{method:'PUT',body:JSON.stringify({provider:this.provider(),url:this.url().trim()})});
  this.url.set('');this.message.set('Webhook saved. Send a test message to check it.');
 })}
 test(){return this.run(async()=>{await this.request('/api/admin/flow-notify-channels/test',{method:'POST'});this.message.set('Test message sent.')})}
 remove(){
  if(!confirm('Remove the webhook? Notify nodes that post to it will fail until a new one is saved.'))return;
  return this.run(async()=>{await this.request('/api/admin/flow-notify-channels/webhook',{method:'DELETE'});this.message.set('Webhook removed.')});
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
