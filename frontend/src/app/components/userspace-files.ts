import { Component, computed, inject, signal } from '@angular/core';
import { AgentSession } from '../core/agent-session';
import { FormsModule } from '@angular/forms';
import { CommonModule } from '@angular/common';

interface FileEntry {path:string; content_type:string; size:number}
interface TreeItem { name:string; path:string; depth:number; }

@Component({
 selector:'fse-userspace-files', host:{class:'fs-native-forms'},
 standalone:true,
 imports:[FormsModule,CommonModule],
 template:`
 <section class="explorer" aria-label="My Workspace file explorer">
  <header class="explorer-toolbar">
   <div class="explorer-title"><i class="bi bi-hdd-stack" aria-hidden="true"></i><div><strong>My Workspace</strong><small>Private · 2 MiB per file · 256 files</small></div></div>
   <div class="toolbar-actions">
    <button class="btn btn-sm btn-outline-secondary" (click)="newFolder()" [disabled]="busy()"><i class="bi bi-folder-plus" aria-hidden="true"></i>Folder</button>
    <button class="btn btn-sm btn-primary" (click)="newFile()"><i class="bi bi-file-earmark-plus" aria-hidden="true"></i>New file</button>
    <button class="btn btn-sm btn-ghost btn-icon" (click)="refresh()" [disabled]="busy()" aria-label="Refresh files" title="Refresh"><i class="bi bi-arrow-clockwise" aria-hidden="true"></i></button>
   </div>
  </header>
  @if(creatingFolder()){
   <form class="folder-creator" (submit)="$event.preventDefault();createFolder()">
    <label>New folder name <input name="folderName" [ngModel]="folderName()" (ngModelChange)="folderName.set($event)" maxlength="80" placeholder="e.g. reports" aria-label="New folder name"></label>
    <button class="btn btn-sm btn-primary" type="submit" [disabled]="busy()||!folderName().trim()">Create folder</button>
    <button class="btn btn-sm btn-outline-secondary" type="button" (click)="creatingFolder.set(false)">Cancel</button>
   </form>
  }
  <div class="breadcrumb" aria-label="Current folder">
   <button type="button" (click)="navigate('/')"><i class="bi bi-house" aria-hidden="true"></i> Workspace</button>
   @for(crumb of crumbs();track crumb.path){<i class="bi bi-chevron-right" aria-hidden="true"></i><button type="button" (click)="navigate(crumb.path)">{{crumb.name}}</button>}
  </div>
  @if(folderDeleteTarget();as target){
   <div class="delete-confirm folder-delete-confirm" role="alertdialog" aria-modal="true" aria-label="Confirm folder deletion">
    <strong>Delete folder {{target}}?</strong>
    <p>This permanently deletes all {{folderDeleteFileCount()}} files in this folder and its subfolders. This cannot be undone.</p>
    <button class="btn btn-sm btn-outline-secondary" type="button" (click)="folderDeleteTarget.set('')">Cancel</button>
    <button class="btn btn-sm btn-primary" type="button" [disabled]="busy()" (click)="removeFolder()">Delete folder and contents</button>
   </div>
  }
  <div class="explorer-grid">
   <nav class="folder-tree" aria-label="Folders">
    <strong>Folders</strong>
    <div class="folder-item" (dragover)="allowDrop($event, '/')" (drop)="dropFile($event, '/')">
     <button type="button" class="folder-toggle" (click)="workspaceExpanded.update(v=>!v)" [attr.aria-expanded]="workspaceExpanded()" aria-label="Toggle Workspace folders"><i class="bi" [class.bi-chevron-down]="workspaceExpanded()" [class.bi-chevron-right]="!workspaceExpanded()" aria-hidden="true"></i></button>
     <button type="button" [class.active]="folder()==='/'" (click)="navigate('/')">Workspace</button>
    </div>
    @if(workspaceExpanded()){
     @for(dir of visibleDirectories();track dir.path){
      <div class="folder-item" [style.padding-left.px]="dir.depth*16" (dragover)="allowDrop($event, dir.path)" (drop)="dropFile($event, dir.path)">
       <button type="button" class="folder-toggle" (click)="toggleDirectory(dir.path)" [attr.aria-expanded]="!isCollapsed(dir.path)" [attr.aria-label]="'Toggle '+dir.name"><i class="bi" [class.bi-chevron-right]="isCollapsed(dir.path)" [class.bi-chevron-down]="!isCollapsed(dir.path)" aria-hidden="true"></i></button>
       <button type="button" [class.active]="folder()===dir.path" (click)="navigate(dir.path)">{{dir.name}}</button>
       <button type="button" class="folder-delete" [disabled]="busy()" (click)="requestFolderDelete(dir.path)" [attr.aria-label]="'Delete folder '+dir.name" title="Delete folder and contents"><i class="bi bi-trash3" aria-hidden="true"></i></button>
      </div>
     }
    }
   </nav>
   <div class="directory" role="region" aria-label="Files">
    <div class="directory-header"><strong>{{folder()}}</strong><span>{{visibleFiles().length}} files</span></div>
    <div class="file-table">
     <div class="file-row heading"><span>Name</span><span>Type</span><span>Size</span></div>
     @for(dir of childFolders();track dir.path){
      <div class="folder-row" (dragover)="allowDrop($event, dir.path)" (drop)="dropFile($event, dir.path)">
       <button type="button" class="file-row" (click)="navigate(dir.path)"><span><i class="bi bi-folder-fill text-warning me-2" aria-hidden="true"></i>{{dir.name}}</span><span>Folder</span><span>—</span></button>
       <button type="button" class="folder-delete" [disabled]="busy()" (click)="requestFolderDelete(dir.path)" [attr.aria-label]="'Delete folder '+dir.name" title="Delete folder and contents"><i class="bi bi-trash3" aria-hidden="true"></i></button>
      </div>
     }
     @for(file of visibleFiles();track file.path){
      <button type="button" class="file-row" draggable="true" (dragstart)="startDrag($event,file.path)" [class.active]="selectedPath()===file.path" (click)="open(file.path)">
       <span><i class="bi bi-file-earmark-text text-muted me-2" aria-hidden="true"></i>{{basename(file.path)}}</span><span>{{file.content_type}}</span><span>{{(file.size/1024).toFixed(1)}} KiB</span>
      </button>
     }
     @if(!childFolders().length&&!visibleFiles().length){<p class="empty">This folder is empty. Create a file to get started.</p>}
    </div>
   </div>
   <section class="editor-panel" aria-label="File editor and visualizer">
    <div class="editor-heading"><strong>{{basename(path())}}</strong><span>{{language()}}</span></div>
    <label>Path<input [ngModel]="path()" (ngModelChange)="path.set($event)" maxlength="512" placeholder="/research/report.md"></label>
    <div class="editor-fields"><label>Format<select [ngModel]="mime()" (ngModelChange)="mime.set($event)">
      <option value="text/plain">Text</option><option value="text/markdown">Markdown</option>
      <option value="application/json">JSON</option><option value="application/xml">XML</option>
      <option value="text/csv">CSV</option>
     </select></label>
     <label>View<select [ngModel]="view()" (ngModelChange)="view.set($event)">
      <option value="edit">Code editor</option><option value="highlight">Syntax preview</option>
      <option value="tree">Structure viewer</option>
     </select></label>
    </div>
    @if(view()==='edit'){
     <textarea class="code-editor" spellcheck="false" rows="18" [ngModel]="content()" (ngModelChange)="content.set($event)" maxlength="2097152" aria-label="File content"></textarea>
    } @else if(view()==='highlight'){
     <pre class="syntax" [innerHTML]="highlighted()"></pre>
    } @else if(mime()==='application/json'){
     @if(jsonError()){<p class="error" role="alert">{{jsonError()}}</p>}
     @else {<div class="tree-view">@for(row of jsonTree();track $index){
      <div class="tree-row" [style.padding-left.px]="row.depth*16"><span class="tree-key">{{row.key}}</span><span>{{row.value}}</span></div>
     }</div>}
    } @else if(mime()==='application/xml'){
     @if(xmlError()){<p class="error" role="alert">{{xmlError()}}</p>}
     @else {<div class="tree-view">@for(row of xmlTree();track $index){
      <div class="tree-row" [style.padding-left.px]="row.depth*16"><span class="tree-key">{{row.key}}</span><span>{{row.value}}</span></div>
     }</div>}
    } @else {<pre class="syntax">{{content()}}</pre>}
    <div class="file-actions">
     <button class="btn btn-sm btn-primary" (click)="save()" [disabled]="busy()||!path()">Save</button>
     <button class="btn btn-sm btn-outline-secondary" (click)="download()" [disabled]="!path()">Download</button>
     <button class="btn btn-sm btn-outline-secondary" (click)="startTransfer('copy')" [disabled]="busy()||!selectedPath()">Duplicate</button>
     <button class="btn btn-sm btn-outline-secondary" (click)="startTransfer('move')" [disabled]="busy()||!selectedPath()">Move</button>
     <button class="btn btn-sm btn-outline-danger" (click)="deleteRequested.set(true)" [disabled]="busy()||!selectedPath()">Delete</button>
     <button class="btn btn-sm btn-outline-secondary ms-auto" (click)="askAgent()" [disabled]="!content().trim()"><i class="bi bi-stars" aria-hidden="true"></i>Ask agent about this file</button>
    </div>
    @if(transferMode()){
     <div class="transfer-form" role="group" aria-label="Move or duplicate file">
      <label>Destination folder <select [ngModel]="transferFolder()" (ngModelChange)="transferFolder.set($event)"><option value="/">Workspace</option>@for(dir of directories();track dir.path){<option [value]="dir.path">{{dir.path}}</option>}</select></label>
      <label>File name <input [ngModel]="transferName()" (ngModelChange)="transferName.set($event)" maxlength="255"></label>
      <button class="btn btn-sm btn-primary" (click)="transferFile()" [disabled]="busy()||!transferName().trim()">Confirm {{transferMode()}}</button>
      <button class="btn btn-sm btn-outline-secondary" (click)="transferMode.set('')">Cancel</button>
     </div>
    }
    @if(deleteRequested()){
     <div class="delete-confirm" role="group" aria-label="Confirm file deletion">
      <p>Permanently delete <strong>{{selectedPath()}}</strong>?</p>
      <button class="btn btn-sm btn-outline-secondary" (click)="deleteRequested.set(false)">Cancel</button>
      <button class="btn btn-sm btn-primary" (click)="remove()" [disabled]="busy()">Confirm delete</button>
     </div>
    }
    @if(message()){<p role="status">{{message()}}</p>}
   </section>
  </div>
 </section>`,
 styles:[`
 :host{display:block;color:var(--fs-text)}
 .explorer{background:var(--fs-surface);border:1px solid var(--fs-border);border-radius:var(--bs-border-radius-lg);overflow:hidden;box-shadow:var(--fs-shadow-sm)}
 .explorer-toolbar{display:flex;justify-content:space-between;align-items:center;padding:12px 16px;gap:12px;flex-wrap:wrap;border-bottom:1px solid var(--fs-border)}
 .explorer-title{display:flex;align-items:center;gap:10px}.explorer-title>.bi{font-size:1.2rem;color:var(--fs-accent-text)}
 .explorer-title strong{display:block;font-size:.95rem}.explorer-title small{color:var(--fs-muted);font-size:.75rem}
 .toolbar-actions,.file-actions{display:flex;gap:8px;flex-wrap:wrap;align-items:center}
 .breadcrumb{display:flex;gap:4px;align-items:center;padding:8px 16px;margin:0;background:var(--fs-surface-2);border-bottom:1px solid var(--fs-border);font-size:.8125rem;flex-wrap:wrap}
 .breadcrumb button{background:none;border:0;color:var(--fs-accent-text);padding:2px 4px;border-radius:4px}.breadcrumb button:hover{background:var(--fs-surface-3)}
 .breadcrumb .bi-chevron-right{color:var(--fs-muted);font-size:.7rem}
 .explorer-grid{display:grid;grid-template-columns:minmax(170px,20%) minmax(220px,30%) minmax(0,1fr);min-height:520px}
 .folder-tree,.directory,.editor-panel{padding:12px;min-width:0;overflow:auto}.folder-tree,.directory{border-right:1px solid var(--fs-border)}
 .folder-tree{display:flex;flex-direction:column;gap:2px;background:var(--fs-surface-2)}.folder-tree>strong{font-size:.7rem;text-transform:uppercase;letter-spacing:.08em;color:var(--fs-muted);margin:2px 6px 8px}
 .folder-item{display:flex;align-items:center;min-width:0;gap:0;border-radius:6px}
 .folder-item:hover{background:var(--fs-surface-3)}
 .folder-item button:not(.folder-toggle){flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis}
 .folder-tree button{background:transparent;border:0;text-align:left;white-space:nowrap;color:inherit;padding:6px 8px;border-radius:6px;font-size:.85rem}
 .folder-tree .folder-toggle{flex:0 0 24px;text-align:center;padding:6px 2px;color:var(--fs-muted);font-size:.7rem}
 .folder-tree button.active{background:var(--fs-accent-soft);color:var(--fs-accent-text);font-weight:500}
 .folder-delete{flex:0 0 auto;color:var(--fs-muted);background:transparent;border:0;border-radius:6px;padding:4px 7px;font-size:.8rem;opacity:.7}
 .folder-delete:hover{color:var(--fs-neg);background:var(--fs-neg-soft);opacity:1}
 .folder-delete:disabled{opacity:.35}
 .folder-row{display:flex;align-items:center;gap:4px}.folder-row .file-row{flex:1;min-width:0}
 .folder-delete-confirm{margin:12px 16px}.folder-delete-confirm button{margin-right:8px}
 .folder-creator{display:flex;flex-wrap:wrap;align-items:end;gap:10px;padding:12px 16px;background:var(--fs-surface-2);border-bottom:1px solid var(--fs-border)}
 .folder-creator label{flex:1;min-width:180px}
 .directory-header{display:flex;justify-content:space-between;gap:8px;padding:4px 4px 10px;font-size:.85rem}.directory-header span{color:var(--fs-muted)}
 .file-row{display:grid;grid-template-columns:minmax(120px,1fr) 90px 64px;gap:8px;padding:8px;border:0;border-bottom:1px solid var(--fs-border);width:100%;text-align:left;background:transparent;color:inherit;font:inherit;font-size:.85rem;border-radius:0}
 .file-row span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.file-row span:not(:first-child){color:var(--fs-muted);font-size:.78rem}
 .file-row.heading{color:var(--fs-muted);font-size:.7rem;text-transform:uppercase;letter-spacing:.05em;cursor:default}
 .file-row:not(.heading):hover{background:var(--fs-surface-2)}
 button.file-row.active{background:var(--fs-accent-soft)}
 .editor-panel{display:flex;flex-direction:column;gap:12px}.editor-heading,.editor-fields{display:flex;gap:12px;justify-content:space-between;align-items:center}
 .editor-heading span{font-size:.75rem;color:var(--fs-muted);background:var(--fs-surface-3);padding:2px 8px;border-radius:999px}
 .editor-fields label{flex:1}label{display:flex;flex-direction:column;gap:5px;min-width:0;font-size:13px;font-weight:500}
 .code-editor,.syntax{font:13px/1.6 var(--bs-font-monospace)!important;tab-size:2;overflow:auto;min-height:340px;max-height:560px;white-space:pre;resize:vertical}
 .syntax{border:1px solid var(--fs-border);border-radius:var(--bs-border-radius);padding:12px;background:var(--fs-sunken);margin:0}
 .syntax :is(.token-key){color:var(--fs-accent-text)}.syntax :is(.token-string){color:var(--fs-pos)}.syntax :is(.token-tag){color:var(--fs-agent)}
 .tree-view{min-height:340px;max-height:560px;overflow:auto;background:var(--fs-sunken);border:1px solid var(--fs-border);padding:10px;border-radius:var(--bs-border-radius);font-size:.82rem}
 .tree-row{display:flex;gap:12px;padding:4px;border-bottom:1px solid var(--fs-border);overflow-wrap:anywhere}.tree-key{color:var(--fs-accent-text);font-weight:500}
 .error{color:var(--fs-neg)}.empty{color:var(--fs-muted);padding:16px 4px;font-size:.85rem}
 .delete-confirm{border:1px solid color-mix(in srgb,var(--fs-neg) 45%,transparent);background:var(--fs-neg-soft);padding:12px;border-radius:var(--bs-border-radius)}
 @media(max-width:1050px){.explorer-grid{grid-template-columns:minmax(155px,30%) minmax(0,1fr)}.editor-panel{grid-column:1/-1;border-top:1px solid var(--fs-border)}}
 @media(max-width:650px){.explorer-grid{grid-template-columns:minmax(0,1fr)}.folder-tree,.directory{border-right:0;border-bottom:1px solid var(--fs-border)}.folder-tree{max-height:180px}.file-row{grid-template-columns:minmax(100px,1fr) 70px 58px}}
 `]
})
export class UserspaceFilesComponent {
 private readonly agent=inject(AgentSession);
 /** Sends the open document to the research agent for a summary and critique. */
 askAgent(){
  const text=this.content().slice(0,8000);
  if(!text.trim())return;
  this.agent.ask('Review this workspace document ('+this.path()+'). Summarize its key claims, check any figures you can verify with your tools, and list open questions.\n\n```\n'+text+'\n```');
 }
 readonly files=signal<FileEntry[]>([]);
 readonly folder=signal('/');
 readonly workspaceExpanded=signal(true);
 readonly collapsedDirectories=signal<string[]>([]);
 readonly creatingFolder=signal(false);
 readonly folderName=signal('');
 readonly path=signal('/research/report.md');
 readonly mime=signal('text/markdown');
 readonly content=signal('');
 readonly view=signal<'edit'|'highlight'|'tree'>('edit');
 readonly message=signal('');
 readonly busy=signal(false);
 readonly deleteRequested=signal(false);
 readonly selectedPath=signal('');
 readonly transferMode=signal<''|'move'|'copy'>('');
 readonly transferFolder=signal('/');
 readonly transferName=signal('');
 readonly folderDeleteTarget=signal('');
 readonly folderDeleteFileCount=computed(()=>this.files().filter(f=>f.path.startsWith(this.folderDeleteTarget()+'/')).length);
 requestFolderDelete(path:string){if(this.busy()||path==='/'||!this.directories().some(d=>d.path===path))return;this.folderDeleteTarget.set(path);this.deleteRequested.set(false)}
 async removeFolder():Promise<void>{
  const target=this.folderDeleteTarget();
  if(this.busy()||!target||target==='/'||!this.directories().some(d=>d.path===target))return;
  this.busy.set(true);
  try{
   const result=await this.api('/api/userspace/folder?path='+encodeURIComponent(target),{method:'DELETE'});
   this.folderDeleteTarget.set('');
   if(this.folder()===target||this.folder().startsWith(target+'/'))this.navigate(this.parent(target));
   if(this.path().startsWith(target+'/')){this.path.set('');this.content.set('')}
   this.collapsedDirectories.update(list=>list.filter(dir=>dir!==target&&!dir.startsWith(target+'/')));
   await this.refresh();
   this.message.set('Folder deleted ('+result.files_deleted+' files removed).');
  }catch(e){this.message.set('Could not delete folder: '+String(e))}
  finally{this.busy.set(false)}
 }
 readonly directories=computed<TreeItem[]>(()=>{
  const seen=new Set<string>();
  for(const file of this.files()){
   const parts=file.path.split('/').filter(Boolean);
   for(let i=0;i<parts.length-1;i++)seen.add('/'+parts.slice(0,i+1).join('/'));
  }
  return [...seen].sort().map(path=>({path,name:this.basename(path),depth:path.split('/').filter(Boolean).length-1}));
 });
 readonly visibleDirectories=computed(()=>this.directories().filter(d=>{
  if(!this.workspaceExpanded())return false;
  let parent=this.parent(d.path);
  while(parent!=='/'){if(this.collapsedDirectories().includes(parent))return false;parent=this.parent(parent)}
  return true;
 }));
 readonly childFolders=computed(()=>this.directories().filter(d=>this.parent(d.path)===this.folder()));
 readonly visibleFiles=computed(()=>this.files().filter(f=>!this.isFolderMarker(f.path)&&this.parent(f.path)===this.folder()));
 readonly crumbs=computed(()=>{const parts=this.folder().split('/').filter(Boolean);return parts.map((name,i)=>({name,path:'/'+parts.slice(0,i+1).join('/')}))});
 readonly language=computed(()=>this.mime()==='application/json'?'JSON':this.mime()==='application/xml'?'XML':this.mime()==='text/csv'?'CSV':this.mime()==='text/markdown'?'Markdown':'Text');
 readonly highlighted=computed(()=>{
  const escaped=this.escape(this.content());
  if(this.mime()==='application/json'){
   return escaped.replace(/(&quot;(?:[^&]|&(?!quot;))*?&quot;)\s*:/g,'<span class="token-key">$1</span>:')
    .replace(/:\s*(&quot;.*?&quot;)/g,': <span class="token-string">$1</span>');
  }
  if(this.mime()==='application/xml'){
   return escaped.replace(/(&lt;\/?[\w:.-]+)(.*?)(\/?&gt;)/g,'<span class="token-tag">$1$2$3</span>');
  }
  return escaped;
 });
 readonly jsonError=computed(()=>{try{JSON.parse(this.content());return ''}catch(e){return String(e)}});
 readonly xmlError=computed(()=>{const doc=new DOMParser().parseFromString(this.content(),'application/xml');return doc.querySelector('parsererror')?.textContent||''});
 readonly jsonTree=computed(()=>{
  const rows:{key:string;value:string;depth:number}[]=[];
  if(this.jsonError())return rows;
  const traverse=(data:unknown,key:string,depth:number)=>{
   if(rows.length>=1000||depth>15)return;
   if(data!==null&&typeof data==='object'){
    const entries=Object.entries(data);
    rows.push({key,value:Array.isArray(data)?'Array ('+entries.length+')':'Object ('+entries.length+')',depth});
    for(const [k,v] of entries)traverse(v,k,depth+1);
   } else rows.push({key,value:JSON.stringify(data)??'undefined',depth});
  };
  traverse(JSON.parse(this.content()),'root',0);
  return rows;
 });
 readonly xmlTree=computed(()=>{
  const rows:{key:string;value:string;depth:number}[]=[];
  if(this.xmlError())return rows;
  const doc=new DOMParser().parseFromString(this.content(),'application/xml');
  const walk=(node:Element,depth:number)=>{
   if(rows.length>=1000||depth>15)return;
   rows.push({key:'<'+node.tagName+'>',value:[...node.attributes].map(a=>a.name+'='+JSON.stringify(a.value)).join(' '),depth});
   for(const child of Array.from(node.children))walk(child,depth+1);
   if(!node.children.length&&node.textContent?.trim())rows.push({key:'#text',value:node.textContent.trim().slice(0,200),depth:depth+1});
  };
  if(doc.documentElement)walk(doc.documentElement,0);
  return rows;
 });
 constructor(){void this.refresh()}
 isFolderMarker(path:string):boolean{return path.endsWith('/.folder')}
 isCollapsed(path:string):boolean{return this.collapsedDirectories().includes(path)}
 toggleDirectory(path:string):void{
  this.collapsedDirectories.update(old=>old.includes(path)?old.filter(p=>p!==path):[...old,path]);
 }
 basename(path:string){return path.split('/').filter(Boolean).pop()||'Workspace'}
 parent(path:string){const parts=path.split('/').filter(Boolean);return parts.length<=1?'/':'/'+parts.slice(0,-1).join('/')}
 navigate(path:string){this.folder.set(path)}
 newFolder(){this.folderName.set('');this.creatingFolder.set(true);this.message.set('')}
 async createFolder():Promise<void>{
  if(this.busy())return;
  const name=this.folderName().trim();
  if(!/^[\p{L}\p{N}][\p{L}\p{N} _.-]{0,79}$/u.test(name)||name==='.'||name==='..'){
   this.message.set('Invalid folder name. Use letters, numbers, spaces, _ or - (max 80 characters).');return;
  }
  const target=(this.folder()==='/'?'':this.folder())+'/'+name;
  if(this.directories().some(d=>d.path===target)||this.files().some(f=>f.path===target)){
   this.message.set('A file or folder with that name already exists.');return;
  }
  this.busy.set(true);
  try{
   // Keep an empty folder as a small, private marker file. No new server API is needed.
   await this.api('/api/userspace/file',{method:'PUT',body:JSON.stringify({path:target+'/.folder',content_type:'text/plain',content:''})});
   await this.refresh();
   this.creatingFolder.set(false);
   this.workspaceExpanded.set(true);
   this.collapsedDirectories.update(old=>old.filter(p=>!target.startsWith(p+'/')));
   this.navigate(target);
   this.message.set('Folder created: '+name);
  }catch(e){this.message.set('Could not create folder: '+String(e))}
  finally{this.busy.set(false)}
 }
 newFile(){this.selectedPath.set('');this.transferMode.set('');this.path.set((this.folder()==='/'?'':this.folder())+'/new.md');this.mime.set('text/markdown');this.content.set('');this.view.set('edit');this.deleteRequested.set(false)}
 private escape(s:string){return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;').replace(/'/g,'&#39;')}
 private async api(url:string,options?:RequestInit):Promise<any>{
  const response=await fetch(url,{credentials:'same-origin',...options,headers:{'Content-Type':'application/json',...options?.headers}});
  const data=await response.json();if(!response.ok)throw Error(data.error||'Request failed');return data;
 }
 async refresh(){try{const d=await this.api('/api/userspace/files');this.files.set(d.files||[])}catch(e){this.message.set(String(e))}}
 async open(path:string){
  this.deleteRequested.set(false);this.busy.set(true);
  try{
   const response=await fetch('/api/userspace/file?path='+encodeURIComponent(path),{credentials:'same-origin'});
   if(!response.ok)throw Error('Cannot open file');
   const text=await response.text();this.path.set(path);this.selectedPath.set(path);this.content.set(text);this.view.set('edit');
   this.mime.set(this.files().find(f=>f.path===path)?.content_type||'text/plain');
  }catch(e){this.message.set(String(e))}finally{this.busy.set(false)}
 }
 async save(){this.busy.set(true);try{
  const bytes=new TextEncoder().encode(this.content()).byteLength;
  if(bytes>2*1024*1024)throw Error('File exceeds 2 MiB limit ('+bytes+' bytes)');
  if(this.mime()==='application/json')JSON.parse(this.content());
  if(this.mime()==='application/xml'&&this.xmlError())throw Error(this.xmlError());
  await this.api('/api/userspace/file',{method:'PUT',body:JSON.stringify({path:this.path(),content_type:this.mime(),content:this.content()})});
  const target=this.path();this.selectedPath.set(target);await this.refresh();this.navigate(this.parent(target));this.message.set('File saved');
 }catch(e){this.message.set(String(e))}finally{this.busy.set(false)}}
 async remove(){if(!this.deleteRequested()||!this.selectedPath())return;this.busy.set(true);
  try{await this.api('/api/userspace/file?path='+encodeURIComponent(this.selectedPath()),{method:'DELETE'});
   this.deleteRequested.set(false);this.newFile();await this.refresh();this.message.set('File deleted');
  }catch(e){this.message.set(String(e))}finally{this.busy.set(false)}
 }
 startDrag(event:DragEvent,path:string):void {
  if(this.busy()||!event.dataTransfer)return;
  event.dataTransfer.setData('text/plain',path);
  event.dataTransfer.effectAllowed='move';
 }
 allowDrop(event:DragEvent,target:string):void {
  if(this.busy()||!event.dataTransfer||!this.directories().some(d=>d.path===target)&&target!=='/')return;
  event.preventDefault();event.dataTransfer.dropEffect='move';
 }
 dropFile(event:DragEvent,target:string):void {
  event.preventDefault();
  const source=event.dataTransfer?.getData('text/plain')||'';
  if(!this.files().some(f=>f.path===source&&!this.isFolderMarker(f.path)))return;
  void this.transfer(source,(target==='/'?'':target)+'/'+this.basename(source),false);
 }
 startTransfer(mode:'move'|'copy'):void {
  if(!this.selectedPath())return;
  this.transferMode.set(mode);this.transferFolder.set(this.parent(this.selectedPath()));
  const name=this.basename(this.selectedPath());
  this.transferName.set(mode==='copy'?'copy-'+name:name);
 }
 async transferFile():Promise<void>{
  if(!this.selectedPath()||!this.transferMode())return;
  const destination=(this.transferFolder()==='/'?'':this.transferFolder())+'/'+this.transferName().trim();
  await this.transfer(this.selectedPath(),destination,this.transferMode()==='copy');
 }
 private async transfer(source:string,destination:string,copy:boolean):Promise<void>{
  if(this.busy())return;
  if(destination===source||this.files().some(f=>f.path===destination)){
   this.message.set('Choose a different, unused destination.');return;
  }
  this.busy.set(true);
  try{
   await this.api('/api/userspace/transfer',{method:'POST',body:JSON.stringify({source,destination,copy})});
   this.transferMode.set('');
   await this.refresh();this.navigate(this.parent(destination));
   await this.open(destination);
   this.message.set(copy?'File duplicated.':'File moved.');
  }catch(e){this.message.set('Could not '+(copy?'duplicate':'move')+' file: '+String(e))}
  finally{this.busy.set(false)}
 }
 download(){const blob=new Blob([this.content()],{type:this.mime()});const url=URL.createObjectURL(blob);const anchor=document.createElement('a');anchor.href=url;anchor.download=this.basename(this.path());anchor.click();URL.revokeObjectURL(url)}
}
