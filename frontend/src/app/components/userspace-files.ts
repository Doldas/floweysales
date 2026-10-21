import { Component, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';

interface FileEntry { path:string; content_type:string; size:number }
@Component({
 selector:'fse-userspace-files',standalone:true,imports:[FormsModule,MatButtonModule],
 template:`
 <section class="files">
  <div class="files-header">
   <div><h3>My Workspace</h3><p>Private files organized by folder path. Maximum 256 files, 64 KiB each.</p></div>
   <button mat-stroked-button (click)="refresh()" [disabled]="busy()">Refresh</button>
  </div>
  <div class="file-layout">
   <nav class="file-list" aria-label="Workspace files">
    <button mat-stroked-button (click)="newFile()">+ New file</button>
    @for(file of files();track file.path){
      <button type="button" [class.active]="path()===file.path" (click)="open(file.path)">
        <span>{{file.path}}</span><small>{{file.size}} bytes</small>
      </button>
    }
   </nav>
   <div class="file-details">
    <label>Path<input [ngModel]="path()" (ngModelChange)="path.set($event)" maxlength="512" placeholder="/research/report.md"></label>
    <label>Content type<select [ngModel]="mime()" (ngModelChange)="mime.set($event)">
     <option value="text/markdown">Markdown</option><option value="text/plain">Text</option>
     <option value="application/json">JSON</option><option value="text/csv">CSV</option>
    </select></label>
    <label>Content<textarea rows="16" [ngModel]="content()" (ngModelChange)="content.set($event)" maxlength="65536"></textarea></label>
    <div class="file-actions">
      <button mat-flat-button (click)="save()" [disabled]="busy()||!path()">Save file</button>
      <button mat-stroked-button (click)="download()" [disabled]="!path()">Download</button>
      <button mat-stroked-button (click)="deleteRequested.set(true)" [disabled]="busy()||!files().some(f=>f.path===path())">Delete file</button>
    </div>
    @if(deleteRequested()){
     <div class="delete-confirm" role="group" aria-label="Confirm file deletion">
      <p>Delete <strong>{{path()}}</strong>? This cannot be undone.</p>
      <button mat-stroked-button (click)="deleteRequested.set(false)">Cancel</button>
      <button mat-flat-button (click)="remove()" [disabled]="busy()">Confirm delete</button>
     </div>
    }
    @if(message()){<p role="status">{{message()}}</p>}
   </div>
  </div>
 </section>`,
 styles:[`
 :host{display:block}.files{border:1px solid #34566d;border-radius:12px;padding:16px;background:#15263a}
 .files-header{display:flex;justify-content:space-between;gap:12px;align-items:start}.files-header h3{margin:0}
 .file-layout{display:grid;grid-template-columns:minmax(210px,30%) minmax(0,1fr);gap:14px;margin-top:18px}
 .file-list{min-width:0;display:flex;flex-direction:column;gap:6px;max-height:550px;overflow:auto}
 .file-list>button{display:flex;text-align:left;justify-content:space-between;gap:8px;background:#1c3347;border:1px solid #466a83;color:#e4f4ff;padding:10px;border-radius:6px;cursor:pointer;overflow-wrap:anywhere}
 .file-list>button.active{border-color:#80caff;background:#224967}.file-list small{opacity:.7;white-space:nowrap}
 .file-details{display:flex;flex-direction:column;gap:12px;min-width:0}
 label{display:flex;flex-direction:column;gap:6px}
 input,select,textarea{background:#0f2132;color:#e3f2ff;border:1px solid #6587a0;border-radius:6px;padding:10px;font:inherit;max-width:100%;box-sizing:border-box}
 textarea{width:100%;resize:vertical}.file-actions{display:flex;gap:10px;flex-wrap:wrap}
 .delete-confirm{border:1px solid #ae6471;border-radius:8px;padding:12px}
 @media(max-width:750px){.file-layout{grid-template-columns:minmax(0,1fr)}.file-list{max-height:200px}}
 `]
})
export class UserspaceFilesComponent {
 readonly files=signal<FileEntry[]>([]);
 readonly path=signal('/research/report.md');
 readonly mime=signal('text/markdown');
 readonly content=signal('');
 readonly message=signal('');
 readonly busy=signal(false);
 readonly deleteRequested=signal(false);
 constructor(){void this.refresh()}
 private async api(url:string,options?:RequestInit):Promise<any>{
  const response=await fetch(url,{credentials:'same-origin',...options,headers:{'Content-Type':'application/json',...options?.headers}});
  const data=await response.json();if(!response.ok)throw Error(data.error||'Request failed');return data;
 }
 async refresh(){try{const d=await this.api('/api/userspace/files');this.files.set(d.files||[])}catch(e){this.message.set(String(e))}}
 newFile(){this.path.set('/research/new.md');this.mime.set('text/markdown');this.content.set('');this.deleteRequested.set(false)}
 async open(path:string){
  this.deleteRequested.set(false);this.busy.set(true);
  try{
   const response=await fetch('/api/userspace/file?path='+encodeURIComponent(path),{credentials:'same-origin'});
   if(!response.ok)throw Error('Cannot open file');
   const text=await response.text();
   this.path.set(path);this.content.set(text);
   this.mime.set(this.files().find(f=>f.path===path)?.content_type||'text/plain');
  }catch(e){this.message.set(String(e))}finally{this.busy.set(false)}
 }
 async save(){this.busy.set(true);try{
  await this.api('/api/userspace/file',{method:'PUT',body:JSON.stringify({path:this.path(),content_type:this.mime(),content:this.content()})});
  await this.refresh();this.message.set('File saved')
 }catch(e){this.message.set(String(e))}finally{this.busy.set(false)}}
 async remove(){if(!this.deleteRequested())return;this.busy.set(true);
  try{await this.api('/api/userspace/file?path='+encodeURIComponent(this.path()),{method:'DELETE'});
   this.deleteRequested.set(false);this.newFile();await this.refresh();this.message.set('File deleted')
  }catch(e){this.message.set(String(e))}finally{this.busy.set(false)}
 }
 download(){
  const blob=new Blob([this.content()],{type:this.mime()});
  const url=URL.createObjectURL(blob);
  const anchor=document.createElement('a');anchor.href=url;anchor.download=this.path().split('/').pop()||'file';
  anchor.click();URL.revokeObjectURL(url);
 }
}
