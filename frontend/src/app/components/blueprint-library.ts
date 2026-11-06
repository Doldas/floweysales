import { FlowDiagramComponent } from '../ui/flow-canvas/flow-diagram';
import { functionDiagram, triggerDiagram } from '../ui/flow-canvas/diagram-adapters';
import { HostListener, Component, EventEmitter, Output, computed, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { TypedFunctionEditorComponent } from './typed-function-editor';
import { V2Graph, inputPorts, isTypedFunctionGraph, newTypedFunctionGraph, outputPorts } from './function-pins';
import { PublishState, RevisionSummary, canPublish, publishStatusLabel } from './flow-versions';

type Kind='text'|'number'|'boolean';
interface Node {id:string;type:'input'|'output'|'constant'|'operation';value_type?:Kind;value?:string;operation?:string;x:number;y:number}
interface Edge {from:string;to:string;to_port?:string}
interface Definition extends PublishState {id:number;name:string;graph:{nodes:Node[];edges:Edge[]}|V2Graph}
interface Trigger {id:number;event_key:string;payload_type:Kind}
@Component({
 selector:'fse-blueprint-library', host:{class:'fs-native-forms'},standalone:true,
 imports:[FormsModule,TypedFunctionEditorComponent,FlowDiagramComponent],
 template:`
 <div class="card lib">
 <h3>Blueprint Function Library</h3>
 <p>Create reusable functions graphically. All operations are pure and cannot execute trades.</p>
 <div class="toolbar">
  <label>Function <select [ngModel]="currentId()" (ngModelChange)="choose(+$event)"><option [ngValue]="0">New function</option><optgroup label="Custom">@for(f of functions();track f.id){<option [ngValue]="f.id">{{f.name}}</option>}</optgroup></select></label>
  <label>Name <input [ngModel]="name()" (ngModelChange)="name.set($event)"></label>
  <button class="btn btn-sm btn-outline-secondary" (click)="newFunction()">New</button>
  <button class="btn btn-sm btn-outline-secondary" (click)="newTypedFunction()" title="Several named, typed inputs and outputs">New typed function</button>
  <label>Built-in function starters<select [ngModel]="''" (ngModelChange)="loadFunctionTemplate($event)">
   <option value="">Copy a template…</option>
   <option value="double">Double a number</option>
   <option value="threshold">Compare with threshold</option>
   <option value="percent">Convert fraction to percent</option>
  </select></label>
  @if(!typed()){<button class="btn btn-sm btn-outline-secondary" (click)="add('constant')">+ Value</button>
  <button class="btn btn-sm btn-outline-secondary" (click)="add('operation')">+ Operation</button>}
 </div>
 @if(typed();as t){
  <fse-typed-function-editor [graph]="t" (graphChange)="typed.set($event)"></fse-typed-function-editor>
 } @else {
 <div class="blueprint-layout">
 <fse-flow-diagram variant="function" ariaLabel="Function canvas: nodes and directed connections" [nodes]="diagram().nodes" [edges]="diagram().edges"
   [selectedId]="selected()" [fitKey]="currentId()" emptyText="Add a value or operation to start" (nodeSelect)="openQuickNode($event)" (canvasClick)="closeQuickNode()" (canvasContextMenu)="onCanvasContextMenu($event)">
 @if(quickNode();as q){
  <div class="node-quick-tooltip" (click)="$event.stopPropagation()" aria-label="Quick function node settings">
   <div class="quick-tooltip-title"><strong>⚙ {{q.id}}</strong><button type="button" class="btn-close quick-tooltip-close" (click)="closeQuickNode()" aria-label="Close quick settings" title="Close"></button></div>
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
 </fse-flow-diagram>
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
   <button class="btn btn-sm btn-outline-secondary" (click)="connect(from.value,n.id,port.value)">Connect</button>
   <button class="btn btn-sm btn-outline-secondary" (click)="removeNode(n.id)" [disabled]="n.type==='input'||n.type==='output'">Remove node</button>
  } @else {<p>Select a node to edit its pins and properties.</p>}
 </aside>
 </div>
 }
 <div class="toolbar"><button class="btn btn-sm btn-primary" (click)="save()" [disabled]="busy()">Save function</button>
  @if(current();as c){
   <label>Publish note <input [ngModel]="publishNote()" (ngModelChange)="publishNote.set($event)" maxlength="500" placeholder="What changed (optional)"></label>
   <button class="btn btn-sm btn-outline-secondary" (click)="publish()" [disabled]="busy()||!canPublish(c,hasUnsavedChanges())" [title]="hasUnsavedChanges()?'Save the function before publishing':'Freeze the saved draft as the next revision'">Publish revision</button>
  }
 </div>
 @if(current();as c){
  <section class="versions" aria-label="Published function versions">
   <p role="status"><strong>{{publishStatusLabel(c)}}</strong>@if(hasUnsavedChanges()){ · unsaved edits}</p>
   <p class="muted">Flow nodes that follow the active revision use it; pinned nodes keep their revision.</p>
   @if(revisions().length){<ul>
    @for(r of revisions();track r.revision){<li [class.active]="r.active"><span>Revision {{r.revision}}@if(r.active){ · active}@if(r.note){ · {{r.note}}}</span>
     <span><button class="btn btn-sm btn-outline-secondary" (click)="activateRevision(r.revision)" [disabled]="busy()||r.active">Activate</button>
     <button class="btn btn-sm btn-outline-secondary" (click)="restoreRevision(r.revision)" [disabled]="busy()">Restore to draft</button></span></li>}
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
 <p>Define a typed event and subscribe to its key in an enabled flow. Send events with an authenticated admin request, or from another service through a signed inbound webhook below.</p>
 <div class="toolbar">
  <label>Event key <input [ngModel]="eventKey()" (ngModelChange)="eventKey.set($event)" placeholder="research.completed"></label>
  <label>Payload type <select [ngModel]="payloadType()" (ngModelChange)="payloadType.set($event)"><option value="text">Text</option><option value="number">Number</option><option value="boolean">Boolean</option></select></label>
  <button class="btn btn-sm btn-outline-secondary" (click)="createTrigger()" [disabled]="busy()">Create trigger</button>
 </div>
 <div class="triggers">@for(t of triggers();track t.id){<span>{{t.event_key}} · {{t.payload_type}}</span>}</div>
 @if(triggers().length){
  <fse-flow-diagram variant="trigger" ariaLabel="Event triggers: each event feeds a typed payload" [nodes]="triggerModel().nodes" [edges]="triggerModel().edges"
   [selectedId]="selectedTrigger()" [fitKey]="triggers().length" readonlyHint="Read-only: a trigger is its event key and payload type" (nodeSelect)="selectedTrigger.set($event)"/>
  @if(selectedTriggerInfo();as t){<p role="status" class="trigger-detail"><strong>{{t.event_key}}</strong> carries a {{t.payload_type}} payload. Enabled flows subscribed to this key run when it is sent.</p>}
 }
 @if(status()){<p role="status">{{status()}}</p>}
 </div>`,
 styles:[`
 .node-quick-tooltip{position:absolute;right:12px;top:60px;z-index:7;display:flex;flex-direction:column;gap:9px;padding:12px;width:min(260px,85%);background:var(--fs-surface-2);backdrop-filter:blur(10px);border:1px solid var(--fs-accent);border-radius:9px;box-shadow:var(--fs-shadow)}
 .quick-tooltip-title{display:flex;align-items:center;justify-content:space-between;gap:12px}
 .node-quick-tooltip label{display:flex;flex-direction:column;gap:5px}
 .node-quick-tooltip input,.node-quick-tooltip select{width:100%;box-sizing:border-box}
 .node-quick-tooltip button{min-height:32px}
 .canvas-context-menu{position:absolute;top:55px;left:16px;z-index:5;display:flex;flex-direction:column;gap:6px;min-width:180px;padding:12px;background:var(--fs-surface-2);border:1px solid var(--fs-accent);border-radius:10px;box-shadow:var(--fs-shadow)}
 :host{display:block;margin-top:22px}.lib{padding:18px}.toolbar,.panel,.triggers{display:flex;flex-wrap:wrap;align-items:end;gap:12px;margin:12px 0}.panel{padding:16px;border:1px solid var(--fs-border-strong);border-radius:8px;align-content:start;background:var(--fs-surface);min-width:0;max-height:min(75vh,780px);overflow-y:auto;display:flex;flex-direction:column;align-items:stretch}
 .blueprint-layout{display:grid;grid-template-columns:minmax(0,1fr) minmax(270px,315px);gap:12px;align-items:stretch}
 .inspector-heading{display:flex;flex-direction:column;gap:6px;border-bottom:1px solid var(--fs-border-strong);padding-bottom:12px}
 .inspector-heading span{font-size:11px;letter-spacing:.14em;opacity:.75}
 .panel label,.panel input,.panel select{min-width:0;width:100%;box-sizing:border-box}
 @media(max-width:850px){.blueprint-layout{grid-template-columns:minmax(0,1fr)}.panel{max-height:360px}}
.panel strong{width:100%}label{display:flex;flex-direction:column;gap:5px;min-width:130px;flex:1}input,select{background:var(--fs-surface);color:var(--fs-text);border:1px solid var(--fs-border-strong);border-radius:6px;padding:8px}.triggers span{padding:8px;border:1px solid var(--fs-border-strong);border-radius:12px}
 .versions ul{list-style:none;padding:0;display:flex;flex-direction:column;gap:6px}.versions li{display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:8px;padding:8px;border:1px solid var(--fs-border);border-radius:8px}.versions li.active{border-color:var(--fs-accent);background:var(--fs-surface-3)}.muted{opacity:.75}`]
})
export class BlueprintLibraryComponent {
 readonly canvasMenu = signal(false);
 readonly selectedTrigger = signal('');
 readonly triggerModel = computed(() => triggerDiagram(this.triggers()));
 readonly selectedTriggerInfo = computed(() => { const id = this.selectedTrigger().replace(/^(event|payload):/, ''); return this.triggers().find(t => t.event_key === id); });
 readonly diagram = computed(() => functionDiagram(this.nodes(), this.edges(), { inputPorts, outputPorts }));
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
 /** Set while editing a function with declared typed pins (graph version 2). */
 readonly typed=signal<V2Graph|null>(null);
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
 private draftSnapshot():string{return JSON.stringify({name:this.name(),typed:this.typed(),nodes:this.nodes(),edges:this.edges()})}
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
 newTypedFunction(){this.newFunction();this.name.set('My typed function');this.typed.set(newTypedFunctionGraph());this.status.set('Declare typed input and output pins, add nodes and wire them, then save. Flows bind each pin separately.')}
 newFunction(){this.typed.set(null);this.currentId.set(0);this.name.set('My function');this.nodes.set(this.initialNodes());this.edges.set([]);this.selected.set('');this.revisions.set([])}
 choose(id:number){const f=this.functions().find(x=>x.id===id);if(!f){this.newFunction();return}this.currentId.set(id);this.name.set(f.name);if(isTypedFunctionGraph(f.graph)){this.typed.set(structuredClone(f.graph));this.nodes.set([]);this.edges.set([])}else{this.typed.set(null);this.nodes.set(structuredClone(f.graph.nodes));this.edges.set(structuredClone(f.graph.edges))};this.selected.set('');this.savedDraft.set(this.draftSnapshot());this.publishNote.set('');void this.loadRevisions()}
 add(type:'constant'|'operation'){const id=type+'-'+Date.now().toString(36);this.nodes.update(n=>[...n,{id,type,x:240+(n.length%3)*95,y:35+(n.length%3)*90,...(type==='operation'?{operation:'add'}:{value_type:'number' as Kind,value:'1'})}]);this.selected.set(id)}
 edit(field:'value_type'|'value'|'operation',value:string){this.nodes.update(nodes=>nodes.map(n=>n.id===this.selected()?{...n,[field]:value}:n))}
 connect(from:string,to:string,port:string){if(!from||from===to)return;this.edges.update(es=>[...es.filter(e=>!(e.to===to&&(e.to_port||'a')===port)),{from,to,to_port:port}])}
 removeNode(id:string){this.nodes.update(ns=>ns.filter(n=>n.id!==id));this.edges.update(es=>es.filter(e=>e.from!==id&&e.to!==id));this.selected.set('')}
 private async api(url:string,init?:RequestInit):Promise<any>{const res=await fetch(url,{credentials:'same-origin',...init,headers:{'Content-Type':'application/json',...init?.headers}});const data=await res.json();if(!res.ok)throw Error(data.error||'Request failed');return data}
 async refresh(){try{const [f,t]=await Promise.all([this.api('/api/admin/blueprint-functions'),this.api('/api/admin/blueprint-triggers')]);this.functions.set(f.functions||[]);this.triggers.set(t.triggers||[])}catch(e){this.status.set(String(e))}}
 async save(){this.busy.set(true);try{const id=this.currentId();const res:Definition=await this.api(id?'/api/admin/blueprint-functions/'+id:'/api/admin/blueprint-functions',{method:id?'PUT':'POST',body:JSON.stringify({name:this.name(),graph:this.typed()??{nodes:this.nodes(),edges:this.edges()}})});this.currentId.set(res.id);this.savedDraft.set(this.draftSnapshot());this.status.set(res.active_revision?'Draft saved; publish it to change unpinned calls':'Function saved');await this.refresh();await this.loadRevisions();this.functionsChanged.emit()}catch(e){this.status.set(String(e))}finally{this.busy.set(false)}}
 async createTrigger(){this.busy.set(true);try{await this.api('/api/admin/blueprint-triggers',{method:'POST',body:JSON.stringify({event_key:this.eventKey(),payload_type:this.payloadType()})});this.status.set('Trigger created');await this.refresh()}catch(e){this.status.set(String(e))}finally{this.busy.set(false)}}
}
