import { Component, computed, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { CommonModule } from '@angular/common';

interface FileEntry {path:string; content_type:string; size:number}
interface TreeItem { name:string; path:string; depth:number; }

@Component({
 selector:'fse-userspace-files',
 standalone:true,
 imports:[FormsModule,MatButtonModule,CommonModule],
 template:`
 <section class="explorer" aria-label="My Workspace file explorer">
  <header class="explorer-toolbar">
   <div><h3><span aria-hidden="true">▣</span> My Workspace</h3><small>Private documents · 2 MiB per file · 256 files</small></div>
   <div class="toolbar-actions">
    <button mat-stroked-button (click)="newFolder()" [disabled]="busy()">+ Folder</button>
    <button mat-stroked-button (click)="newFile()">+ File</button>
    <button mat-stroked-button (click)="refresh()" [disabled]="busy()">Refresh</button>
   </div>
  </header>
  @if(creatingFolder()){
   <form class="folder-creator" (submit)="$event.preventDefault();void createFolder()">
    <label>New folder name <input name="folderName" [ngModel]="folderName()" (ngModelChange)="folderName.set($event)" maxlength="80" placeholder="e.g. reports" aria-label="New folder name"></label>
    <button mat-flat-button type="submit" [disabled]="busy()||!folderName().trim()">Create folder</button>
    <button mat-stroked-button type="button" (click)="creatingFolder.set(false)">Cancel</button>
   </form>
  }
  <div class="breadcrumb" aria-label="Current folder">
   <button type="button" (click)="navigate('/')">Workspace</button>
   @for(crumb of crumbs();track crumb.path){<span aria-hidden="true">›</span><button type="button" (click)="navigate(crumb.path)">{{crumb.name}}</button>}
  </div>
  <div class="explorer-grid">
   <nav class="folder-tree" aria-label="Folders">
    <strong>Folders</strong>
    <div class="folder-item">
     <button type="button" class="folder-toggle" (click)="workspaceExpanded.update(v=>!v)" [attr.aria-expanded]="workspaceExpanded()" aria-label="Toggle Workspace folders">{{workspaceExpanded()?'▾':'▸'}}</button>
     <button type="button" [class.active]="folder()==='/'" (click)="navigate('/')">Workspace</button>
    </div>
    @if(workspaceExpanded()){
     @for(dir of visibleDirectories();track dir.path){
      <div class="folder-item" [style.padding-left.px]="dir.depth*16">
       <button type="button" class="folder-toggle" (click)="toggleDirectory(dir.path)" [attr.aria-expanded]="!isCollapsed(dir.path)" [attr.aria-label]="'Toggle '+dir.name">{{isCollapsed(dir.path)?'▸':'▾'}}</button>
       <button type="button" [class.active]="folder()===dir.path" (click)="navigate(dir.path)">{{dir.name}}</button>
      </div>
     }
    }
   </nav>
   <div class="directory" role="region" aria-label="Files">
    <div class="directory-header"><strong>{{folder()}}</strong><span>{{visibleFiles().length}} files</span></div>
    <div class="file-table">
     <div class="file-row heading"><span>Name</span><span>Type</span><span>Size</span></div>
     @for(dir of childFolders();track dir.path){
      <button type="button" class="file-row" (click)="navigate(dir.path)"><span>📁 {{dir.name}}</span><span>Folder</span><span>—</span></button>
     }
     @for(file of visibleFiles();track file.path){
      <button type="button" class="file-row" [class.active]="path()===file.path" (click)="open(file.path)">
       <span>📄 {{basename(file.path)}}</span><span>{{file.content_type}}</span><span>{{(file.size/1024).toFixed(1)}} KiB</span>
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
     <button mat-flat-button (click)="save()" [disabled]="busy()||!path()">Save</button>
     <button mat-stroked-button (click)="download()" [disabled]="!path()">Download</button>
     <button mat-stroked-button (click)="deleteRequested.set(true)" [disabled]="busy()||!files().some(f=>f.path===path())">Delete</button>
    </div>
    @if(deleteRequested()){
     <div class="delete-confirm" role="group" aria-label="Confirm file deletion">
      <p>Permanently delete <strong>{{path()}}</strong>?</p>
      <button mat-stroked-button (click)="deleteRequested.set(false)">Cancel</button>
      <button mat-flat-button (click)="remove()" [disabled]="busy()">Confirm delete</button>
     </div>
    }
    @if(message()){<p role="status">{{message()}}</p>}
   </section>
  </div>
 </section>`,
 styles:[`
 :host{display:block;color:#e2effc}.explorer{background:#101d2b;border:1px solid #34566d;border-radius:12px;overflow:hidden}
 .explorer-toolbar{display:flex;justify-content:space-between;align-items:center;padding:16px;background:#1b3045;gap:12px;flex-wrap:wrap}.explorer-toolbar h3{margin:0}.explorer-toolbar small{opacity:.7}
 .toolbar-actions,.file-actions{display:flex;gap:8px;flex-wrap:wrap}.breadcrumb{display:flex;gap:8px;align-items:center;padding:9px 16px;background:#13283b}
 .breadcrumb button{background:none;border:0;color:#b9ddfa;cursor:pointer;padding:4px}
 .explorer-grid{display:grid;grid-template-columns:minmax(155px,20%) minmax(200px,30%) minmax(0,1fr);min-height:500px}
 .folder-tree,.directory,.editor-panel{padding:13px;min-width:0;overflow:auto}.folder-tree,.directory{border-right:1px solid #36556b}
 .folder-tree{display:flex;flex-direction:column;gap:5px}.folder-tree strong{margin-bottom:8px}
 .folder-item{display:flex;align-items:center;min-width:0;gap:0}
 .folder-item button:not(.folder-toggle){flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis}
 .folder-tree .folder-toggle{flex:0 0 28px;text-align:center;padding:7px 4px}
 .folder-creator{display:flex;flex-wrap:wrap;align-items:end;gap:10px;padding:12px 16px;background:#172d42;border-bottom:1px solid #36556b}
 .folder-creator label{flex:1;min-width:180px}
 .folder-tree button{background:transparent;border:0;text-align:left;white-space:nowrap;color:inherit;padding:7px 10px;cursor:pointer;border-radius:5px}
 button.active,.folder-tree button:hover{background:#264865}.directory-header{display:flex;justify-content:space-between;gap:8px;padding:7px 0 14px}
 .file-row{display:grid;grid-template-columns:minmax(120px,1fr) 75px 62px;gap:8px;padding:10px 8px;border:0;border-bottom:1px solid #2d4256;width:100%;text-align:left;background:transparent;color:inherit;cursor:pointer;font:inherit}
 .file-row span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.file-row.heading{color:#a8c4db;font-size:12px;cursor:default}.file-row:not(.heading):hover{background:#1d3952}
 .editor-panel{display:flex;flex-direction:column;gap:12px}.editor-heading,.editor-fields{display:flex;gap:12px;justify-content:space-between;align-items:center}
 .editor-fields label{flex:1}label{display:flex;flex-direction:column;gap:5px;min-width:0;font-size:13px}
 input,select,textarea{box-sizing:border-box;width:100%;background:#0b1b2a;color:#e6f2fe;border:1px solid #567c9b;padding:9px;border-radius:6px;font:inherit}
 .code-editor,.syntax{font:13px/1.6 ui-monospace,SFMono-Regular,Consolas,monospace;tab-size:2;overflow:auto;min-height:310px;max-height:540px;white-space:pre;resize:vertical}
 .syntax{border:1px solid #456078;border-radius:6px;padding:12px;background:#091725;overflow:auto}
 .syntax :is(.token-key){color:#86c8ff}.syntax :is(.token-string){color:#a7e69b}.syntax :is(.token-tag){color:#d9a3ff}
 .tree-view{min-height:310px;max-height:540px;overflow:auto;background:#0c1d2c;padding:10px;border-radius:6px}
 .tree-row{display:flex;gap:12px;padding:5px;border-bottom:1px solid #264158;overflow-wrap:anywhere}.tree-key{color:#8ac7f6}
 .error{color:#ffadb8}.empty{opacity:.7}.delete-confirm{border:1px solid #ad6a72;padding:12px;border-radius:8px}
 @media(max-width:1050px){.explorer-grid{grid-template-columns:minmax(155px,30%) minmax(0,1fr)}.editor-panel{grid-column:1/-1;border-top:1px solid #36556b}}
 @media(max-width:650px){.explorer-grid{grid-template-columns:minmax(0,1fr)}.folder-tree,.directory{border-right:0;border-bottom:1px solid #36556b}.folder-tree{max-height:180px}.file-row{grid-template-columns:minmax(100px,1fr) 65px 58px}}
 `]
})
export class UserspaceFilesComponent {
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
 readonly directories=computed<TreeItem[]>(()=>{
  const seen=new Set<string>();
  for(const file of this.files()){
   const parts=file.path.split('/').filter(Boolean);
   for(let i=0;i<(this.isFolderMarker(file.path)?parts.length-1:parts.length-1);i++)seen.add('/'+parts.slice(0,i+1).join('/'));
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
 newFile(){this.path.set((this.folder()==='/'?'':this.folder())+'/new.md');this.mime.set('text/markdown');this.content.set('');this.view.set('edit');this.deleteRequested.set(false)}
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
   const text=await response.text();this.path.set(path);this.content.set(text);this.view.set('edit');
   this.mime.set(this.files().find(f=>f.path===path)?.content_type||'text/plain');
  }catch(e){this.message.set(String(e))}finally{this.busy.set(false)}
 }
 async save(){this.busy.set(true);try{
  const bytes=new TextEncoder().encode(this.content()).byteLength;
  if(bytes>2*1024*1024)throw Error('File exceeds 2 MiB limit ('+bytes+' bytes)');
  if(this.mime()==='application/json')JSON.parse(this.content());
  if(this.mime()==='application/xml'&&this.xmlError())throw Error(this.xmlError());
  await this.api('/api/userspace/file',{method:'PUT',body:JSON.stringify({path:this.path(),content_type:this.mime(),content:this.content()})});
  const target=this.path();await this.refresh();this.navigate(this.parent(target));this.message.set('File saved');
 }catch(e){this.message.set(String(e))}finally{this.busy.set(false)}}
 async remove(){if(!this.deleteRequested())return;this.busy.set(true);
  try{await this.api('/api/userspace/file?path='+encodeURIComponent(this.path()),{method:'DELETE'});
   this.deleteRequested.set(false);this.newFile();await this.refresh();this.message.set('File deleted');
  }catch(e){this.message.set(String(e))}finally{this.busy.set(false)}
 }
 download(){const blob=new Blob([this.content()],{type:this.mime()});const url=URL.createObjectURL(blob);const anchor=document.createElement('a');anchor.href=url;anchor.download=this.basename(this.path());anchor.click();URL.revokeObjectURL(url)}
}
