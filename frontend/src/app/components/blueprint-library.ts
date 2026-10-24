import { CanvasGestures } from './canvas-gestures';
import { HostListener, Component, EventEmitter, Output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { PublishState, RevisionSummary, canPublish, publishStatusLabel } from './flow-versions';

type Kind='text'|'number'|'boolean';
interface Node {id:string;type:'input'|'output'|'constant'|'operation';value_type?:Kind;value?:string;operation?:string;x:number;y:number}
interface Edge {from:string;to:string;to_port?:string}
interface Definition extends PublishState {id:number;name:string;graph:{nodes:Node[];edges:Edge[]}}
interface Trigger {id:number;event_key:string;payload_type:Kind}
@Component({
 selector:'fse-blueprint-library',standalone:true,
 imports:[FormsModule,MatButtonModule,MatCardModule],
 template:`
 <mat-card class="lib">
 <h3>Blueprint Function Library</h3>
 <p>Create reusable functions graphically. All operations are pure and cannot execute trades.</p>
 <div class="toolbar">
  <label>Function <select [ngModel]="currentId()" (ngModelChange)="choose(+$event)"><option [ngValue]="0">New function</option><optgroup label="Custom">@for(f of functions();track f.id){<option [ngValue]="f.id">{{f.name}}</option>}</optgroup></select></label>
  <label>Name <input [ngModel]="name()" (ngModelChange)="name.set($event)"></label>
  <button mat-stroked-button (click)="newFunction()">New</button>
  <label>Built-in function starters<select [ngModel]="''" (ngModelChange)="loadFunctionTemplate($event)">
   <option value="">Copy a template…</option>
   <option value="double">Double a number</option>
   <option value="threshold">Compare with threshold</option>
   <option value="percent">Convert fraction to percent</option>
  </select></label>
  <button mat-stroked-button (click)="add('constant')">+ Value</button>
  <button mat-stroked-button (click)="add('operation')">+ Operation</button>
 </div>
 <div class="blueprint-layout">
 <div class="canvas-shell">
    <div class="canvas-navigation" aria-label="Canvas navigation">
      <button type="button" (click)="zoomCanvas(0.8)" aria-label="Zoom in">＋</button>
      <span>{{ canvasZoomPercent() }}%</span>
      <button type="button" (click)="zoomCanvas(1.25)" aria-label="Zoom out">－</button>
      <button type="button" (click)="resetCanvas()" aria-label="Reset canvas view">Reset view</button>
    </div>
    <div class="canvas">
  <svg [attr.viewBox]="canvasViewBox()" (wheel)="onCanvasWheel($event)" (pointerdown)="onCanvasPointerDown($event)" (click)="closeQuickNodeOnCanvas($event)" (pointermove)="onCanvasPointerMove($event)" (pointerup)="onCanvasPointerEnd($event)" (pointercancel)="onCanvasPointerEnd($event)" (contextmenu)="onCanvasContextMenu($event)">
   <defs><marker id="fn-arrow" markerWidth="9" markerHeight="9" refX="8" refY="4" orient="auto"><path d="M0,0 L8,4 L0,8 Z" fill="#7ebdf2"/></marker></defs>
   @for(e of edges();track e.from+e.to+e.to_port){
    <line [attr.x1]="node(e.from)?.x!+120" [attr.y1]="node(e.from)?.y!+25"
      [attr.x2]="node(e.to)?.x" [attr.y2]="node(e.to)?.y!+25" stroke="#7ebdf2" stroke-width="2" marker-end="url(#fn-arrow)"/>
   }
   @for(n of nodes();track n.id){
    <g data-canvas-node (click)="$event.stopPropagation();openQuickNode(n.id)" tabindex="0" (keydown.enter)="openQuickNode(n.id)">
     <rect [attr.x]="n.x" [attr.y]="n.y" width="120" height="50" rx="9" [attr.fill]="selected()===n.id?'#345579':'#1b3449'" stroke="#8db5d1"/>
     <text [attr.x]="n.x+8" [attr.y]="n.y+21" fill="white" font-size="12">{{n.type.toUpperCase()}}</text>
     <text [attr.x]="n.x+8" [attr.y]="n.y+40" fill="#bbd7eb" font-size="11">{{n.operation||n.value_type||n.id}}</text>
    </g>
   }
  </svg>
 </div>
 @if(quickNode();as q){
  <div class="node-quick-tooltip" (click)="$event.stopPropagation()" aria-label="Quick function node settings">
   <div class="quick-tooltip-title"><strong>⚙ {{q.id}}</strong><button type="button" class="quick-tooltip-close" (click)="closeQuickNode()" aria-label="Close quick settings" title="Close">×</button></div>
   @if(q.type==='constant'||q.type==='input'||q.type==='output'){
     <label>Type <select [ngModel]="q.value_type||'text'" (ngModelChange)="editNode(q.id,'value_type',$event)"><option value="text">Text</option><option value="number">Number</option><option value="boolean">Boolean</option></select></label>
   }
   @if(q.type==='constant'){<label>Value <input [ngModel]="q.value||''" (ngModelChange)="editNode(q.id,'value',$event)"></label>}
   @if(q.type==='operation'){<label>Operation <select [ngModel]="q.operation||'add'" (ngModelChange)="editNode(q.id,'operation',$event)">@for(op of operations;track op){<option [value]="op">{{op}}</option>}</select></label>}
   <button type="button" (click)="selected.set(q.id);hoveredNode.set('')">Full details</button>
   <button type="button" (click)="closeQuickNode()">Close</button>
  </div>
 }
 @if(canvasMenu()){<div class="canvas-context-menu" role="menu" aria-label="Create node">
 <strong>Add node</strong>
 <button type="button" role="menuitem" (click)="add('constant');closeCanvasMenu()">Constant</button>
 <button type="button" role="menuitem" (click)="add('operation');closeCanvasMenu()">Operation</button>
 <button type="button" (click)="closeCanvasMenu()">Cancel</button>
 </div>}
 </div>
 <aside class="panel" aria-label="Function node details">
  <header class="inspector-heading"><span>DETAILS</span><strong>{{ selectedNode()?.id || 'No node selected' }}</strong></header>
  @if(selectedNode();as n){
   @if(n.type==='input'||n.type==='output'||n.type==='constant'){
    <label>Type <select [ngModel]="n.value_type||'text'" (ngModelChange)="edit('value_type',$event)"><option value="text">Text</option><option value="number">Number</option><option value="boolean">Boolean</option></select></label>
   }
   @if(n.type==='constant'){<label>Value <input [ngModel]="n.value||''" (ngModelChange)="edit('value',$event)"></label>}
   @if(n.type==='operation'){
    <label>Operation <select [ngModel]="n.operation||'add'" (ngModelChange)="edit('operation',$event)">
     @for(op of operations;track op){<option [value]="op">{{op}}</option>}
    </select></label>
   }
   <label>Connect from <select #from><option value="">Source node</option>@for(src of nodes();track src.id){@if(src.id!==n.id){<option [value]="src.id">{{src.id}}</option>}}</select></label>
   <label>Target pin <select #port><option value="a">a / output</option><option value="b">b</option><option value="condition">condition</option><option value="true">true</option><option value="false">false</option></select></label>
   <button mat-stroked-button (click)="connect(from.value,n.id,port.value)">Connect</button>
   <button mat-stroked-button (click)="removeNode(n.id)" [disabled]="n.type==='input'||n.type==='output'">Remove node</button>
  } @else {<p>Select a node to edit its pins and properties.</p>}
 </aside>
 </div>
 <div class="toolbar"><button mat-flat-button (click)="save()" [disabled]="busy()">Save function</button>
  @if(current();as c){
   <label>Publish note <input [ngModel]="publishNote()" (ngModelChange)="publishNote.set($event)" maxlength="500" placeholder="What changed (optional)"></label>
   <button mat-stroked-button (click)="publish()" [disabled]="busy()||!canPublish(c,hasUnsavedChanges())" [title]="hasUnsavedChanges()?'Save the function before publishing':'Freeze the saved draft as the next revision'">Publish revision</button>
  }
 </div>
 @if(current();as c){
  <section class="versions" aria-label="Published function versions">
   <p role="status"><strong>{{publishStatusLabel(c)}}</strong>@if(hasUnsavedChanges()){ · unsaved edits}</p>
   <p class="muted">Flow nodes that follow the active revision use it; pinned nodes keep their revision.</p>
   @if(revisions().length){<ul>
    @for(r of revisions();track r.revision){<li [class.active]="r.active"><span>Revision {{r.revision}}@if(r.active){ · active}@if(r.note){ · {{r.note}}}</span>
     <span><button mat-stroked-button (click)="activateRevision(r.revision)" [disabled]="busy()||r.active">Activate</button>
     <button mat-stroked-button (click)="restoreRevision(r.revision)" [disabled]="busy()">Restore to draft</button></span></li>}
   </ul>}
  </section>
 }
 <h3>Event trigger starters</h3>
 <div class="toolbar">
  <label>Trigger template<select [ngModel]="''" (ngModelChange)="useTriggerTemplate($event)">
   <option value="">Copy an event key…</option>
   <option value="research.requested">Research requested</option>
   <option value="market.close">Market close</option>
   <option value="risk.alert">Risk alert</option>
  </select></label>
 </div>
 <h3>Custom Event Triggers</h3>
 <p>Define a typed event and subscribe to its key in an enabled flow. Events currently require an authenticated admin request.</p>
 <div class="toolbar">
  <label>Event key <input [ngModel]="eventKey()" (ngModelChange)="eventKey.set($event)" placeholder="research.completed"></label>
  <label>Payload type <select [ngModel]="payloadType()" (ngModelChange)="payloadType.set($event)"><option value="text">Text</option><option value="number">Number</option><option value="boolean">Boolean</option></select></label>
  <button mat-stroked-button (click)="createTrigger()" [disabled]="busy()">Create trigger</button>
 </div>
 <div class="triggers">@for(t of triggers();track t.id){<span>{{t.event_key}} · {{t.payload_type}}</span>}</div>
 @if(status()){<p role="status">{{status()}}</p>}
 </mat-card>`,
 styles:[`
 .canvas-shell{position:relative;min-width:0;min-height:0}
 .node-quick-tooltip{position:absolute;right:12px;top:60px;z-index:7;display:flex;flex-direction:column;gap:9px;padding:12px;width:min(260px,85%);background:rgba(26,51,74,.89);backdrop-filter:blur(10px);border:1px solid #82bbec;border-radius:9px;box-shadow:0 12px 28px #000a}
 .quick-tooltip-title{display:flex;align-items:center;justify-content:space-between;gap:12px}
 .quick-tooltip-close{width:26px;height:26px;min-height:26px!important;padding:0!important;flex:0 0 auto;color:#fff!important;background:#b52b38!important;border:1px solid #e15d66!important;border-radius:5px!important;font-size:20px!important;line-height:1!important;cursor:pointer}
 .quick-tooltip-close:hover{background:#db3545!important}
 .node-quick-tooltip label{display:flex;flex-direction:column;gap:5px}
 .node-quick-tooltip input,.node-quick-tooltip select{width:100%;box-sizing:border-box}
 .node-quick-tooltip button{min-height:32px}
 .canvas-navigation{display:flex;align-items:center;gap:5px;flex-wrap:wrap;padding:8px;background:#14263a;border:1px solid #45627b;border-radius:8px 8px 0 0}
 .canvas-navigation button,.canvas-context-menu button{background:#203a53;color:#e5f3ff;border:1px solid #527b99;border-radius:5px;min-height:34px;padding:6px 10px;cursor:pointer}
 .canvas-navigation button:focus-visible,.canvas-context-menu button:focus-visible{outline:2px solid #80caff}
 .canvas-navigation span{min-width:48px;text-align:center;font-variant-numeric:tabular-nums}
 .canvas-context-menu{position:absolute;top:55px;left:16px;z-index:5;display:flex;flex-direction:column;gap:6px;min-width:180px;padding:12px;background:#15283d;border:1px solid #77b9e9;border-radius:10px;box-shadow:0 10px 35px #0008}
 :host{display:block;margin-top:22px}.lib{padding:18px}.toolbar,.panel,.triggers{display:flex;flex-wrap:wrap;align-items:end;gap:12px;margin:12px 0}.panel{padding:16px;border:1px solid #50738e;border-radius:8px;align-content:start;background:#142438;min-width:0;max-height:min(75vh,780px);overflow-y:auto;display:flex;flex-direction:column;align-items:stretch}
 .blueprint-layout{display:grid;grid-template-columns:minmax(0,1fr) minmax(270px,315px);gap:12px;align-items:stretch}
 .blueprint-layout .canvas{min-width:0;min-height:440px}
 .inspector-heading{display:flex;flex-direction:column;gap:6px;border-bottom:1px solid #50738e;padding-bottom:12px}
 .inspector-heading span{font-size:11px;letter-spacing:.14em;opacity:.75}
 .panel label,.panel input,.panel select{min-width:0;width:100%;box-sizing:border-box}
 @media(max-width:850px){.blueprint-layout{grid-template-columns:minmax(0,1fr)}.blueprint-layout .canvas{min-height:280px}.panel{max-height:360px}}
.panel strong{width:100%}label{display:flex;flex-direction:column;gap:5px;min-width:130px;flex:1}input,select{background:#14263a;color:#ecf6ff;border:1px solid #68859b;border-radius:6px;padding:8px}.canvas{overflow:auto;border:1px solid #45627b;background:#102032;border-radius:8px}svg{display:block;min-width:650px;width:100%;height:auto;touch-action:none;cursor:grab}.triggers span{padding:8px;border:1px solid #50738e;border-radius:12px}
 .versions ul{list-style:none;padding:0;display:flex;flex-direction:column;gap:6px}.versions li{display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:8px;padding:8px;border:1px solid #3d5d77;border-radius:8px}.versions li.active{border-color:#73baff;background:#1b3854}.muted{opacity:.75}`]
})
export class BlueprintLibraryComponent {
 readonly canvasView = signal({ x: 0, y: 0, width: 850, height: 340 });
 readonly canvasMenu = signal(false);
 readonly canvasViewBox = () => {
   const v = this.canvasView();
   return `${v.x} ${v.y} ${v.width} ${v.height}`;
 };
 readonly canvasZoomPercent = () => Math.round(850 / this.canvasView().width * 100);
 zoomCanvas(factor: number): void {
   const v = this.canvasView();
   const width = Math.max(850 / 3, Math.min(850 * 4, v.width * factor));
   const height = width * 340 / 850;
   this.canvasView.set({ x: v.x + (v.width - width) / 2, y: v.y + (v.height - height) / 2, width, height });
 }
 panCanvas(dx: number, dy: number): void {
   this.canvasView.update(v => ({ ...v, x: v.x + dx * v.width / 850, y: v.y + dy * v.height / 340 }));
 }
 resetCanvas(): void { this.gestures.reset(); this.canvasView.set({ x: 0, y: 0, width: 850, height: 340 }); this.closeCanvasMenu(); }
 readonly gestures = new CanvasGestures(() => this.canvasView(), view => this.canvasView.set(view), 850, 340);
 onCanvasPointerDown(event: PointerEvent): void { this.gestures.pointerDown(event); }
 onCanvasPointerMove(event: PointerEvent): void { this.gestures.pointerMove(event); }
 onCanvasPointerEnd(event: PointerEvent): void { this.gestures.pointerEnd(event); }
 onCanvasWheel(event: WheelEvent): void { this.gestures.wheel(event); }
 onCanvasContextMenu(event: MouseEvent): void {
   event.preventDefault();
   this.canvasMenu.set(true);
 }
 closeCanvasMenu(): void { this.canvasMenu.set(false); }

 readonly hoveredNode=signal('');
 openQuickNode(id:string):void { this.selected.set(id); this.hoveredNode.set(id); }
 closeQuickNode():void { this.hoveredNode.set(''); }
 @HostListener('document:click',['$event'])
 onDocumentClick(event:MouseEvent):void {
  const el=event.target;
  if(el instanceof Element && !el.closest('[data-canvas-node], .node-quick-tooltip'))this.closeQuickNode();
 }
 closeQuickNodeOnCanvas(event:MouseEvent):void {
  if(!(event.target as Element).closest('[data-canvas-node]'))this.closeQuickNode();
 }

 readonly quickNode=()=>this.node(this.hoveredNode());
 editNode(id:string,field:'value_type'|'value'|'operation',value:string):void { this.nodes.update(items=>items.map(n=>n.id===id?{...n,[field]:value}:n)); }
 @Output() functionsChanged=new EventEmitter<void>();
 readonly operations=['add','subtract','multiply','divide','greater','less','equal','and','or','not','if','to_text'];
 readonly functions=signal<Definition[]>([]);
 readonly triggers=signal<Trigger[]>([]);
 readonly currentId=signal(0);
 readonly name=signal('My function');
 readonly nodes=signal<Node[]>(this.initialNodes());
 readonly edges=signal<Edge[]>([]);
 readonly selected=signal('');
 readonly selectedNode=()=>this.nodes().find(n=>n.id===this.selected());
 readonly eventKey=signal('');
 readonly payloadType=signal<Kind>('text');
 readonly busy=signal(false);
 readonly status=signal('');
 readonly publishStatusLabel=publishStatusLabel;
 readonly canPublish=canPublish;
 readonly revisions=signal<RevisionSummary[]>([]);
 readonly publishNote=signal('');
 readonly current=()=>this.functions().find(f=>f.id===this.currentId());
 private draftSnapshot():string{return JSON.stringify({name:this.name(),nodes:this.nodes(),edges:this.edges()})}
 private savedDraft=signal('');
 readonly hasUnsavedChanges=()=>this.savedDraft()!==this.draftSnapshot();
 async loadRevisions(){const id=this.currentId();if(!id){this.revisions.set([]);return}
  try{const data=await this.api('/api/admin/blueprint-functions/'+id+'/revisions');if(this.currentId()===id)this.revisions.set(data.revisions||[])}catch(e){this.revisions.set([]);this.status.set(String(e))}}
 async publish(){const id=this.currentId();if(!id||this.hasUnsavedChanges()){this.status.set('Save the function before publishing');return}
  this.busy.set(true);try{const data=await this.api('/api/admin/blueprint-functions/'+id+'/publish',{method:'POST',body:JSON.stringify({note:this.publishNote().trim()})});this.publishNote.set('');this.status.set('Published revision '+data.active_revision);await this.refresh();await this.loadRevisions();this.functionsChanged.emit()}catch(e){this.status.set(String(e))}finally{this.busy.set(false)}}
 async activateRevision(revision:number){const id=this.currentId();if(!id||!confirm('Make revision '+revision+' active for unpinned calls?'))return;
  this.busy.set(true);try{await this.api('/api/admin/blueprint-functions/'+id+'/revisions/'+revision+'/activate',{method:'POST'});this.status.set('Revision '+revision+' is now active');await this.refresh();await this.loadRevisions();this.functionsChanged.emit()}catch(e){this.status.set(String(e))}finally{this.busy.set(false)}}
 async restoreRevision(revision:number){const id=this.currentId();if(!id||!confirm('Replace the draft with revision '+revision+'?'+(this.hasUnsavedChanges()?' Unsaved edits will be lost.':'')))return;
  this.busy.set(true);try{const restored:Definition=await this.api('/api/admin/blueprint-functions/'+id+'/revisions/'+revision+'/restore',{method:'POST'});this.functions.update(list=>list.map(f=>f.id===id?restored:f));this.choose(id);this.status.set('Draft restored from revision '+revision+'; publish it to make it the next revision');await this.loadRevisions()}catch(e){this.status.set(String(e))}finally{this.busy.set(false)}}
 constructor(){void this.refresh()}
 private initialNodes():Node[]{return [{id:'input',type:'input',value_type:'number',x:30,y:150},{id:'output',type:'output',value_type:'number',x:690,y:150}]}
 node(id:string){return this.nodes().find(n=>n.id===id)}
 loadFunctionTemplate(key:string):void {
  if(!key)return;
  this.newFunction();
  const input:Node={id:'input',type:'input',value_type:'number',x:45,y:140};
  const constant:Node={id:'value',type:'constant',value_type:'number',value:key==='percent'?'100':key==='double'?'2':'70',x:235,y:260};
  const operation:Node={id:'operation',type:'operation',operation:key==='threshold'?'greater':'multiply',x:410,y:140};
  const output:Node={id:'output',type:'output',value_type:key==='threshold'?'boolean':'number',x:680,y:140};
  this.currentId.set(0);
  this.name.set(key==='double'?'Double a number':key==='percent'?'Convert to percent':'Threshold comparison');
  this.nodes.set([input,constant,operation,output]);
  this.edges.set([{from:'input',to:'operation',to_port:'a'},{from:'value',to:'operation',to_port:'b'},{from:'operation',to:'output'}]);
  this.status.set('Built-in function copied. Save to Custom to keep your editable version.');
 }
 useTriggerTemplate(key:string):void{
  if(!['research.requested','market.close','risk.alert'].includes(key))return;
  this.eventKey.set(key);this.payloadType.set('text');
  this.status.set('Trigger template selected. Create trigger to activate this event key.');
 }
 newFunction(){this.currentId.set(0);this.name.set('My function');this.nodes.set(this.initialNodes());this.edges.set([]);this.selected.set('');this.revisions.set([])}
 choose(id:number){const f=this.functions().find(x=>x.id===id);if(!f){this.newFunction();return}this.currentId.set(id);this.name.set(f.name);this.nodes.set(structuredClone(f.graph.nodes));this.edges.set(structuredClone(f.graph.edges));this.selected.set('');this.savedDraft.set(this.draftSnapshot());this.publishNote.set('');void this.loadRevisions()}
 add(type:'constant'|'operation'){const id=type+'-'+Date.now().toString(36);this.nodes.update(n=>[...n,{id,type,x:240+(n.length%3)*95,y:35+(n.length%3)*90,...(type==='operation'?{operation:'add'}:{value_type:'number' as Kind,value:'1'})}]);this.selected.set(id)}
 edit(field:'value_type'|'value'|'operation',value:string){this.nodes.update(nodes=>nodes.map(n=>n.id===this.selected()?{...n,[field]:value}:n))}
 connect(from:string,to:string,port:string){if(!from||from===to)return;this.edges.update(es=>[...es.filter(e=>!(e.to===to&&(e.to_port||'a')===port)),{from,to,to_port:port}])}
 removeNode(id:string){this.nodes.update(ns=>ns.filter(n=>n.id!==id));this.edges.update(es=>es.filter(e=>e.from!==id&&e.to!==id));this.selected.set('')}
 private async api(url:string,init?:RequestInit):Promise<any>{const res=await fetch(url,{credentials:'same-origin',...init,headers:{'Content-Type':'application/json',...init?.headers}});const data=await res.json();if(!res.ok)throw Error(data.error||'Request failed');return data}
 async refresh(){try{const [f,t]=await Promise.all([this.api('/api/admin/blueprint-functions'),this.api('/api/admin/blueprint-triggers')]);this.functions.set(f.functions||[]);this.triggers.set(t.triggers||[])}catch(e){this.status.set(String(e))}}
 async save(){this.busy.set(true);try{const id=this.currentId();const res:Definition=await this.api(id?'/api/admin/blueprint-functions/'+id:'/api/admin/blueprint-functions',{method:id?'PUT':'POST',body:JSON.stringify({name:this.name(),graph:{nodes:this.nodes(),edges:this.edges()}})});this.currentId.set(res.id);this.savedDraft.set(this.draftSnapshot());this.status.set(res.active_revision?'Draft saved; publish it to change unpinned calls':'Function saved');await this.refresh();await this.loadRevisions();this.functionsChanged.emit()}catch(e){this.status.set(String(e))}finally{this.busy.set(false)}}
 async createTrigger(){this.busy.set(true);try{await this.api('/api/admin/blueprint-triggers',{method:'POST',body:JSON.stringify({event_key:this.eventKey(),payload_type:this.payloadType()})});this.status.set('Trigger created');await this.refresh()}catch(e){this.status.set(String(e))}finally{this.busy.set(false)}}
}
