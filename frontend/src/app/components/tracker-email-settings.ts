import {Component,signal} from '@angular/core';
import {FormsModule} from '@angular/forms';
import {MatButtonModule} from '@angular/material/button';
import {MatCardModule} from '@angular/material/card';

interface Subscription {symbol:string;provider:string;golden_cross:boolean;death_cross:boolean;stop_loss:boolean;cooldown_minutes:number}
@Component({
 selector:'fse-tracker-email-settings',standalone:true,
 imports:[FormsModule,MatButtonModule,MatCardModule],
 template:`
 <mat-card class="sub-card">
  <h2>Tracker email notifications</h2>
  <p>Subscribe to the trackers you follow. Choose which events trigger an email. All preferences are personal and notifications are off until you subscribe.</p>
  @if(message()){<p role="status">{{message()}}</p>}
  <div class="new-sub">
   <label>Symbol <input [ngModel]="symbol()" (ngModelChange)="symbol.set($event.toUpperCase())" maxlength="32" placeholder="AAPL"></label>
   <label>Provider <select [ngModel]="provider()" (ngModelChange)="provider.set($event)">
    <option value="YAHOO">Yahoo</option><option value="FINNHUB">Finnhub</option><option value="AVANZA">Avanza</option><option value="BINANCE">Binance</option>
   </select></label>
   <button mat-flat-button [disabled]="busy()||!symbol().trim()" (click)="subscribe()">Subscribe</button>
  </div>
  @for(item of subscriptions();track item.provider+'|'+item.symbol){
   <section class="subscription">
    <div class="subscription-title"><strong>{{item.symbol}} · {{item.provider}}</strong><button mat-stroked-button [disabled]="busy()" (click)="unsubscribe(item)">Unsubscribe</button></div>
    <div class="options">
     <label><input type="checkbox" [ngModel]="item.golden_cross" (ngModelChange)="update(item,'golden_cross',$event)"> Golden Cross</label>
     <label><input type="checkbox" [ngModel]="item.death_cross" (ngModelChange)="update(item,'death_cross',$event)"> Death Cross</label>
     <label><input type="checkbox" [ngModel]="item.stop_loss" (ngModelChange)="update(item,'stop_loss',$event)"> Stop Loss</label>
     <label>Minimum minutes between emails
      <input type="number" min="0" max="1440" [ngModel]="item.cooldown_minutes" (ngModelChange)="update(item,'cooldown_minutes',+$event)">
     </label>
    </div>
   </section>
  } @empty {<p>No tracker email subscriptions yet.</p>}
 </mat-card>
 `,
 styles:[`
 .sub-card{display:flex;flex-direction:column;gap:14px;padding:20px;max-width:900px;margin:16px auto}
 .new-sub,.options,.subscription-title{display:flex;flex-wrap:wrap;gap:14px;align-items:center}
 .subscription-title{justify-content:space-between}
 .subscription{border:1px solid #52728b;border-radius:8px;padding:15px;display:grid;gap:12px}
 label{display:flex;gap:7px;align-items:center}
 input:not([type=checkbox]),select{background:#173047;color:#fff;border:1px solid #68859b;padding:8px;border-radius:6px;max-width:170px}
 `]
})
export class TrackerEmailSettingsComponent {
 readonly subscriptions=signal<Subscription[]>([]);
 readonly symbol=signal('');readonly provider=signal('YAHOO');readonly busy=signal(false);readonly message=signal('');
 constructor(){void this.load()}
 private async api(path:string,init?:RequestInit):Promise<any>{
  const res=await fetch(path,{...init,credentials:'same-origin',headers:{'Content-Type':'application/json',...init?.headers}});
  const data=await res.json().catch(()=>({}));if(!res.ok)throw new Error(data.error||'HTTP '+res.status);return data;
 }
 async load(){try{const data=await this.api('/api/me/tracker-emails');this.subscriptions.set(data.subscriptions||[])}catch(e){this.message.set(String(e))}}
 private async perform(fn:()=>Promise<void>){if(this.busy())return;this.busy.set(true);this.message.set('');try{await fn()}catch(e){this.message.set(String(e))}finally{this.busy.set(false)}}
 async subscribe(){await this.perform(async()=>{
  const sub:Subscription={symbol:this.symbol().trim(),provider:this.provider(),golden_cross:true,death_cross:true,stop_loss:true,cooldown_minutes:10};
  await this.api('/api/me/tracker-emails',{method:'PUT',body:JSON.stringify(sub)});
  this.symbol.set('');await this.load();this.message.set('Subscription saved.');
 })}
 async unsubscribe(item:Subscription){await this.perform(async()=>{
  await this.api('/api/me/tracker-emails/'+encodeURIComponent(item.provider)+'/'+encodeURIComponent(item.symbol),{method:'DELETE'});
  await this.load();this.message.set('Unsubscribed.');
 })}
 async update(item:Subscription,key:'golden_cross'|'death_cross'|'stop_loss'|'cooldown_minutes',value:boolean|number){
  const updated={...item,[key]:key==='cooldown_minutes'?Math.max(0,Math.min(1440,Math.trunc(Number(value)||0))):value};
  await this.perform(async()=>{
   await this.api('/api/me/tracker-emails',{method:'PUT',body:JSON.stringify(updated)});
   this.subscriptions.update(all=>all.map(s=>s.symbol===item.symbol&&s.provider===item.provider?updated:s));
   this.message.set('Notification preferences saved.');
  });
 }
}
