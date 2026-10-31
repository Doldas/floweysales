import { Component, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';

interface User {id:number;email:string;role:string;active:boolean}
interface Role {name:string;permissions:string[]}

@Component({
 selector:'fse-admin-access-settings', host:{class:'fs-native-forms'},
 standalone:true,
 imports:[FormsModule],
 template:`
 @if(message()){<p role="status">{{message()}}</p>}
 @if(tab()==='users'){
  <div class="card">
   <h3>User management</h3>
   <p>Invite people by email. The activation link is valid for 48 hours.</p>
   <label>Invite email <input type="email" [ngModel]="inviteEmail()" (ngModelChange)="inviteEmail.set($event)" maxlength="254" placeholder="person@example.com"></label>
   <button class="btn btn-sm btn-primary" [disabled]="busy()||!inviteEmail().includes('@')" (click)="invite()">Send invitation</button>
   @for(user of users();track user.id){
    <div class="item">
     <div><strong>{{user.email}}</strong><small>{{user.active?'Active':'Pending / disabled'}}</small></div>
     <label>Role<select [ngModel]="user.role" (ngModelChange)="changeUser(user.id,{role:$event})" [disabled]="busy()">
      @for(role of roles();track role.name){<option [value]="role.name">{{role.name}}</option>}
     </select></label>
     <button class="btn btn-sm btn-outline-secondary" [disabled]="busy()" (click)="changeUser(user.id,{active:!user.active})">{{user.active?'Disable':'Enable'}}</button>
     @if(user.role!=='SYS_ADMIN') {<button class="btn btn-sm btn-outline-danger" type="button" [disabled]="busy()" (click)="deleteTarget.set(user)">Delete user</button>}
    </div>
   }
   @if(deleteTarget();as target){<div class="delete-user-confirm" role="alertdialog" aria-label="Confirm account deletion"><strong>Delete {{target.email}}?</strong><p>This permanently removes the account. Existing related records may prevent deletion. This cannot be undone.</p><button class="btn btn-sm btn-outline-secondary" (click)="deleteTarget.set(null)">Cancel</button><button class="btn btn-sm btn-outline-danger" [disabled]="busy()" (click)="deleteUser(target.id)">Permanently delete</button></div>}
  </div>
 }
 @if(tab()==='roles'){
  <div class="card">
   <h3>Role management</h3>
   <p>Custom roles begin with no privileges. SYS_ADMIN cannot be changed in this view.</p>
   <label>New role name<input maxlength="40" [ngModel]="roleName()" (ngModelChange)="roleName.set($event.toUpperCase())" placeholder="ANALYST"></label>
   <button class="btn btn-sm btn-primary" [disabled]="busy()||roleName().length<2" (click)="createRole()">Create role</button>
   @for(role of roles();track role.name){
    <div class="item"><strong>{{role.name}}</strong><small>{{role.permissions.length}} permissions</small></div>
   }
  </div>
 }
 @if(tab()==='permissions'){
  <div class="card">
   <h3>Permission management</h3>
   <p>Changes are applied in the backend. SYS_ADMIN always retains platform permissions.</p>
   @for(role of roles();track role.name){
    <h4>{{role.name}}</h4>
    @for(permission of permissions();track permission){
     <label class="check">
      <input type="checkbox" [checked]="role.name==='SYS_ADMIN'||role.permissions.includes(permission)" [disabled]="busy()||role.name==='SYS_ADMIN'" (change)="grant(role.name,permission,$any($event.target).checked)">
      {{permission}}
     </label>
    }
   }
  </div>
 }
 @if(tab()==='mail'){
  <div class="card">
   <h3>SMTP configuration</h3>
   <p>Account invitations are sent via TLS-verified SMTP STARTTLS on port 587. The activation URL must be HTTPS.</p>
   <label>SMTP host<input [ngModel]="smtpHost()" (ngModelChange)="smtpHost.set($event)" placeholder="smtp.example.com"></label>
   <label>SMTP port<input type="number" [ngModel]="smtpPort()" (ngModelChange)="smtpPort.set(+$event)" min="587" max="587"></label>
   <label>Sender email<input type="email" [ngModel]="smtpSender()" (ngModelChange)="smtpSender.set($event)" placeholder="noreply@example.com"></label>
   <label>SMTP username<input [ngModel]="smtpUsername()" (ngModelChange)="smtpUsername.set($event)"></label>
   <label>Public HTTPS app URL<input type="url" [ngModel]="publicURL()" (ngModelChange)="publicURL.set($event)" placeholder="https://floweysales.example.com"></label>
   <button class="btn btn-sm btn-primary" [disabled]="busy()" (click)="saveSMTP()">Save mail configuration</button>
   <p>SMTP password: {{smtpPasswordConfigured()?'Configured':'Not configured'}}</p>
   <label>SMTP password<input type="password" autocomplete="new-password" [ngModel]="smtpPassword()" (ngModelChange)="smtpPassword.set($event)"></label>
   <button class="btn btn-sm btn-primary" [disabled]="busy()||!smtpPassword()" (click)="saveSMTPPassword()">Save encrypted password</button>
  </div>
 }
 `,
 styles:[`
 .card{display:flex;flex-direction:column;gap:12px;padding:22px;background:var(--fs-surface)}
 label{display:flex;flex-direction:column;gap:5px}
 input,select{box-sizing:border-box;border:1px solid var(--fs-border-strong);border-radius:7px;background:var(--fs-surface);color:var(--fs-text);padding:10px}
 .check{flex-direction:row;align-items:center}
 .item{display:flex;align-items:center;flex-wrap:wrap;justify-content:space-between;gap:10px;border-bottom:1px solid var(--fs-border);padding:10px}
 .item small{display:block;color:var(--fs-muted)}
 .delete-user-confirm{border:1px solid var(--fs-border);border-radius:8px;padding:16px;display:flex;flex-wrap:wrap;gap:10px;align-items:center}.delete-user-confirm p{flex-basis:100%;margin:0}
 `]
})
export class AdminAccessSettingsComponent{
 readonly tab=input<string>('users');
 readonly busy=signal(false);
 readonly message=signal('');
 readonly users=signal<User[]>([]);
 readonly deleteTarget=signal<User|null>(null);
 readonly roles=signal<Role[]>([]);
 readonly permissions=signal<string[]>([]);
 readonly inviteEmail=signal('');
 readonly roleName=signal('');
 readonly smtpHost=signal('');
 readonly smtpPort=signal(587);
 readonly smtpSender=signal('');
 readonly smtpUsername=signal('');
 readonly publicURL=signal('');
 readonly smtpPassword=signal('');
 readonly smtpPasswordConfigured=signal(false);
 constructor(){void this.refresh()}
 private async request(path:string,init?:RequestInit):Promise<any>{
  const response=await fetch(path,{...init,credentials:'same-origin',headers:{'Content-Type':'application/json',...init?.headers}});
  const data=await response.json().catch(()=>({}));
  if(!response.ok)throw new Error(data.error||'HTTP '+response.status);
  return data;
 }
 async refresh(){
  try{
   const [users,roles,smtp]=await Promise.all([
    this.request('/api/admin/users/list'),
    this.request('/api/admin/roles'),
    this.request('/api/admin/settings/smtp')
   ]);
   this.users.set(users.users||[]);
   this.roles.set(roles.roles||[]);
   this.permissions.set(roles.permissions||[]);
   const cfg=smtp.configuration||{};
   this.smtpHost.set(cfg.host||'');this.smtpPort.set(cfg.port||587);
   this.smtpSender.set(cfg.sender||'');this.smtpUsername.set(cfg.username||'');
   this.publicURL.set(cfg.public_url||'');this.smtpPasswordConfigured.set(!!smtp.has_password);
  }catch(e){this.message.set(String(e))}
 }
 private async submit(fn:()=>Promise<void>){
  if(this.busy())return;this.busy.set(true);this.message.set('');
  try{await fn()}catch(e){this.message.set(String(e))}
  finally{this.busy.set(false)}
 }
 async invite(){await this.submit(async()=>{
  await this.request('/api/admin/users/invite',{method:'POST',body:JSON.stringify({email:this.inviteEmail().trim()})});
  this.inviteEmail.set('');this.message.set('Invitation email sent.');
  await this.refresh();
 })}
 async changeUser(id:number,changes:{role?:string;active?:boolean}){await this.submit(async()=>{
  await this.request('/api/admin/users/'+id,{method:'PATCH',body:JSON.stringify(changes)});
  await this.refresh();this.message.set('User updated.');
 })}
 async deleteUser(id:number){await this.submit(async()=>{
  await this.request('/api/admin/users/'+id,{method:'DELETE'});
  this.deleteTarget.set(null);await this.refresh();this.message.set('User permanently deleted.');
 })}
 async createRole(){await this.submit(async()=>{
  await this.request('/api/admin/roles',{method:'POST',body:JSON.stringify({name:this.roleName().trim()})});
  this.roleName.set('');await this.refresh();this.message.set('Role created.');
 })}
 async grant(role:string,permission:string,enabled:boolean){await this.submit(async()=>{
  await this.request('/api/admin/roles/'+encodeURIComponent(role)+'/permissions/'+encodeURIComponent(permission),{method:'PUT',body:JSON.stringify({enabled})});
  await this.refresh();this.message.set('Role permission updated.');
 })}
 async saveSMTP(){await this.submit(async()=>{
  await this.request('/api/admin/settings/smtp',{method:'PUT',body:JSON.stringify({
   host:this.smtpHost().trim(),port:this.smtpPort(),sender:this.smtpSender().trim(),username:this.smtpUsername().trim(),public_url:this.publicURL().trim()
  })});
  this.message.set('SMTP configuration saved.');
 })}
 async saveSMTPPassword(){await this.submit(async()=>{
  await this.request('/api/admin/settings/smtp/password',{method:'PUT',body:JSON.stringify({password:this.smtpPassword()})});
  this.smtpPassword.set('');this.smtpPasswordConfigured.set(true);this.message.set('Encrypted SMTP password saved.');
 })}
}
