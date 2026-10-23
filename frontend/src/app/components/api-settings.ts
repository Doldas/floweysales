import { Component, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';

interface AIConfig {base_url:string;model:string;max_steps:number;max_tool_calls:number;configured:boolean}
interface ModelEntry {id:number;model:string;label:string}
interface IntegrationInfo {encryption_ready:boolean;ai:{has_key:boolean};web_search:{provider:string;has_key:boolean}}

@Component({
 selector:'fse-api-settings',
 standalone:true,
 imports:[FormsModule,MatButtonModule,MatCardModule],
 template:`
 <div class="api-settings">
  <header><h2>System Admin · API Settings</h2>
   <p>Central configuration for AI and web search integrations. Credentials are encrypted on the server and never displayed after saving.</p>
  </header>
  @if(!encryptionReady()){
   <p class="warning" role="alert">Persistent API credentials are unavailable until the server administrator configures FLOWEYSALES_SECRETS_KEY as a base64-encoded, 32-byte encryption key. Store this key securely and back it up.</p>
  }
  @if(message()){<p role="status">{{message()}}</p>}
  <div class="cards">
   <mat-card>
    <h3>AI provider</h3>
    <label>OpenAI-compatible endpoint URL
     <input [ngModel]="aiBaseURL()" (ngModelChange)="aiBaseURL.set($event)" placeholder="https://api.openai.com/v1" maxlength="512">
    </label>
    <label>Default AI model
     <input [ngModel]="aiModel()" (ngModelChange)="aiModel.set($event)" maxlength="128" placeholder="gpt-4o-mini">
    </label>
    <label>Maximum model rounds
     <input type="number" min="1" max="32" [ngModel]="maxSteps()" (ngModelChange)="maxSteps.set(+$event)">
    </label>
    <label>Maximum tool calls
     <input type="number" min="1" max="128" [ngModel]="maxCalls()" (ngModelChange)="maxCalls.set(+$event)">
    </label>
    <button mat-flat-button [disabled]="busy()" (click)="saveAIConfig()">Save AI configuration</button>
    <hr>
    <p>AI API key: <strong>{{aiKeySaved()?'Configured':'Not configured'}}</strong></p>
    <label>New API key (leave blank to keep current)
     <input type="password" autocomplete="new-password" [ngModel]="aiKey()" (ngModelChange)="aiKey.set($event)" placeholder="Enter replacement AI API key">
    </label>
    <div class="actions">
     <button mat-flat-button [disabled]="busy()||!encryptionReady()||!aiKey().trim()" (click)="saveKey('ai')">Save encrypted key</button>
     <button mat-stroked-button [disabled]="busy()||!aiKeySaved()" (click)="deleteKey('ai')">Remove key</button>
    </div>
    <h4>Available AI models</h4>
    @for(model of models();track model.id){
     <div class="item"><span>{{model.label}} <small>{{model.model}}</small></span>
      <button mat-stroked-button [disabled]="busy()" (click)="removeModel(model.id)">Remove</button>
     </div>
    }
    <label>Model name <input [ngModel]="newModel()" (ngModelChange)="newModel.set($event)" maxlength="120" placeholder="gpt-4o-mini"></label>
    <label>Display label <input [ngModel]="newLabel()" (ngModelChange)="newLabel.set($event)" maxlength="120" placeholder="Fast analysis"></label>
    <button mat-stroked-button [disabled]="busy()||!newModel().trim()||!newLabel().trim()" (click)="addModel()">Add AI model</button>
   </mat-card>
   <mat-card>
    <h3>Web search API</h3>
    <label>Search provider
     <select [ngModel]="webProvider()" (ngModelChange)="webProvider.set($event)">
      <option value="brave">Brave Search API</option>
      <option value="tavily">Tavily Search API</option>
     </select>
    </label>
    <p>API key: <strong>{{webKeySaved()?'Configured':'Not configured'}}</strong></p>
    <label>Web search API key
     <input type="password" autocomplete="new-password" [ngModel]="webKey()" (ngModelChange)="webKey.set($event)" placeholder="Enter search provider API key">
    </label>
    <div class="actions">
     <button mat-flat-button [disabled]="busy()||!encryptionReady()||!webKey().trim()" (click)="saveKey('web_search')">Save provider and encrypted key</button>
     <button mat-stroked-button [disabled]="busy()||!webKeySaved()" (click)="deleteKey('web_search')">Remove key</button>
    </div>
    <p class="muted">Provider credentials are configured here for upcoming web-search nodes. Crawling public HTML pages does not require a search API key. Search execution is not yet implemented by this settings change.</p>
   </mat-card>
  </div>
 </div>
 `,
 styles:[`
 .api-settings{display:flex;flex-direction:column;gap:16px;padding:16px;max-width:1150px;margin:0 auto}
 .api-settings h2,.api-settings h3,.api-settings h4{margin:0 0 10px}
 .cards{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,360px),1fr));gap:18px}
 mat-card{display:flex;flex-direction:column;gap:12px;padding:22px;background:var(--mat-sys-surface-container, #182d3f)}
 label{display:flex;flex-direction:column;gap:5px;font-size:14px}
 input,select{width:100%;box-sizing:border-box;border:1px solid #6787a2;background:#11283a;color:#f4f8ff;padding:10px;border-radius:7px;font:inherit}
 .actions{display:flex;flex-wrap:wrap;gap:10px}
 .item{display:flex;align-items:center;justify-content:space-between;gap:10px}
 .item small{display:block;color:#bbcee0}
 .muted{opacity:.8}
 .warning{padding:12px;border-left:4px solid #e3ac54;background:#42321f;color:#fff}
 `]
})
export class ApiSettingsComponent{
 readonly busy=signal(false);
 readonly message=signal('');
 readonly encryptionReady=signal(false);
 readonly aiBaseURL=signal('');
 readonly aiModel=signal('');
 readonly maxSteps=signal(12);
 readonly maxCalls=signal(48);
 readonly aiKey=signal('');
 readonly aiKeySaved=signal(false);
 readonly webProvider=signal('brave');
 readonly webKey=signal('');
 readonly webKeySaved=signal(false);
 readonly models=signal<ModelEntry[]>([]);
 readonly newModel=signal('');
 readonly newLabel=signal('');
 constructor(){void this.refresh()}
 private async api(path:string,init?:RequestInit):Promise<any>{
  const res=await fetch(path,{...init,credentials:'same-origin',headers:{'Content-Type':'application/json',...init?.headers}});
  const body=await res.json().catch(()=>({}));
  if(!res.ok)throw new Error(body.error||'Request failed (HTTP '+res.status+')');
  return body;
 }
 async refresh(){
  try{
   const [config,integrations,entries]=await Promise.all([
    this.api('/api/ai/config'),this.api('/api/admin/integrations'),this.api('/api/admin/ai/models')
   ]) as [AIConfig,IntegrationInfo,{models:ModelEntry[]}];
   this.aiBaseURL.set(config.base_url||'');
   this.aiModel.set(config.model||'');
   this.maxSteps.set(config.max_steps||12);
   this.maxCalls.set(config.max_tool_calls||48);
   this.encryptionReady.set(integrations.encryption_ready);
   this.aiKeySaved.set(integrations.ai.has_key);
   this.webKeySaved.set(integrations.web_search.has_key);
   this.webProvider.set(integrations.web_search.provider||'brave');
   this.models.set(entries.models||[]);
  }catch(e){this.message.set('Unable to load API settings: '+String(e))}
 }
 async saveAIConfig(){
  await this.perform(async()=>{
   await this.api('/api/ai/config',{method:'PUT',body:JSON.stringify({
    base_url:this.aiBaseURL().trim(),model:this.aiModel().trim(),
    max_steps:this.maxSteps(),max_tool_calls:this.maxCalls()
   })});
   this.message.set('AI configuration saved.');
  });
 }
 async saveKey(kind:'ai'|'web_search'){
  const key=kind==='ai'?this.aiKey().trim():this.webKey().trim();
  if(!key)return;
  await this.perform(async()=>{
   await this.api('/api/admin/integrations/'+kind,{method:'PUT',body:JSON.stringify({
    provider:kind==='web_search'?this.webProvider():'openai_compatible',api_key:key
   })});
   if(kind==='ai'){this.aiKey.set('');this.aiKeySaved.set(true)}
   else {this.webKey.set('');this.webKeySaved.set(true)}
   this.message.set('Encrypted '+(kind==='ai'?'AI':'web search')+' credential saved.');
  });
 }
 async deleteKey(kind:'ai'|'web_search'){
  if(!confirm('Remove the saved '+(kind==='ai'?'AI':'web search')+' API key?'))return;
  await this.perform(async()=>{
   await this.api('/api/admin/integrations/'+kind,{method:'DELETE'});
   if(kind==='ai'){this.aiKey.set('');this.aiKeySaved.set(false)}
   else{this.webKey.set('');this.webKeySaved.set(false)}
   this.message.set('Credential removed.');
  });
 }
 async addModel(){
  await this.perform(async()=>{
   await this.api('/api/admin/ai/models',{method:'POST',body:JSON.stringify({model:this.newModel().trim(),label:this.newLabel().trim()})});
   this.newModel.set('');this.newLabel.set('');
   const data=await this.api('/api/admin/ai/models');this.models.set(data.models||[]);
   this.message.set('AI model added.');
  });
 }
 async removeModel(id:number){
  await this.perform(async()=>{
   await this.api('/api/admin/ai/models/'+id,{method:'DELETE'});
   this.models.update(items=>items.filter(m=>m.id!==id));
   this.message.set('AI model removed.');
  });
 }
 private async perform(action:()=>Promise<void>){
  if(this.busy())return;
  this.busy.set(true);this.message.set('');
  try{await action()}catch(e){this.message.set(String(e))}
  finally{this.busy.set(false)}
 }
}
