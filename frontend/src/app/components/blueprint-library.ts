import { CanvasGestures } from './canvas-gestures';
import { Component, EventEmitter, Output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';

type Kind='text'|'number'|'boolean';
interface Node {id:string;type:'input'|'output'|'constant'|'operation';value_type?:Kind;value?:string;operation?:string;x:number;y:number}
interface Edge {from:string;to:string;to_port?:string}
interface Definition {id:number;name:string;graph:{nodes:Node[];edges:Edge[]}}
interface Trigger {id:number;event_key:string;payload_type:Kind}
@Component({
 selector:'fse-blueprint-library',standalone:true,
 imports:[FormsModule,MatButtonModule,MatCardModule],
 template:`
 <mat-card class="lib">
 <h3>Blueprint Function Library</h3>
 <p>Create reusable functions graphically. All operations are pure and cannot execute trades.</p>
 <div class="toolbar">
  <label>Function <select [ngModel]="currentId()" (ngModelChange)="choose(+$event)"><option [ngValue]="0">New function</option>@for(f of functions();track f.id){<option [ngValue]="f.id">{{f.name}}</option>}</select></label>
  <label>Name <input [ngModel]="name()" (ngModelChange)="name.set($event)"></label>
  <button mat-stroked-button (click)="newFunction()">New</button>
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
  <svg [attr.viewBox]="canvasViewBox()" (wheel)="onCanvasWheel($event)" (pointerdown)="onCanvasPointerDown($event)" (pointermove)="onCanvasPointerMove($event)" (pointerup)="onCanvasPointerEnd($event)" (pointercancel)="onCanvasPointerEnd($event)" (contextmenu)="onCanvasContextMenu($event)">
   <defs><marker id="fn-arrow" markerWidth="9" markerHeight="9" refX="8" refY="4" orient="auto"><path d="M0,0 L8,4 L0,8 Z" fill="#7ebdf2"/></marker></defs>
   @for(e of edges();track e.from+e.to+e.to_port){
    <line [attr.x1]="node(e.from)?.x!+120" [attr.y1]="node(e.from)?.y!+25"
      [attr.x2]="node(e.to)?.x" [attr.y2]="node(e.to)?.y!+25" stroke="#7ebdf2" stroke-width="2" marker-end="url(#fn-arrow)"/>
   }
   @for(n of nodes();track n.id){
    <g data-canvas-node (click)="selected.set(n.id)" tabindex="0" (keydown.enter)="selected.set(n.id)">
     <rect [attr.x]="n.x" [attr.y]="n.y" width="120" height="50" rx="9" [attr.fill]="selected()===n.id?'#345579':'#1b3449'" stroke="#8db5d1"/>
     <text [attr.x]="n.x+8" [attr.y]="n.y+21" fill="white" font-size="12">{{n.type.toUpperCase()}}</text>
     <text [attr.x]="n.x+8" [attr.y]="n.y+40" fill="#bbd7eb" font-size="11">{{n.operation||n.value_type||n.id}}</text>
    </g>
   }
  </svg>
 </div>
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
 <div class="toolbar"><button mat-flat-button (click)="save()" [disabled]="busy()">Save function</button></div>
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
.panel strong{width:100%}label{display:flex;flex-direction:column;gap:5px;min-width:130px;flex:1}input,select{background:#14263a;color:#ecf6ff;border:1px solid #68859b;border-radius:6px;padding:8px}.canvas{overflow:auto;border:1px solid #45627b;background:#102032;border-radius:8px}svg{display:block;min-width:650px;width:100%;height:auto;touch-action:none;cursor:grab}.triggers span{padding:8px;border:1px solid #50738e;border-radius:12px}`]
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
 constructor(){void this.refresh()}
 private initialNodes():Node[]{return [{id:'input',type:'input',value_type:'number',x:30,y:150},{id:'output',type:'output',value_type:'number',x:690,y:150}]}
 node(id:string){return this.nodes().find(n=>n.id===id)}
 newFunction(){this.currentId.set(0);this.name.set('My function');this.nodes.set(this.initialNodes());this.edges.set([]);this.selected.set('')}
 choose(id:number){const f=this.functions().find(x=>x.id===id);if(!f){this.newFunction();return}this.currentId.set(id);this.name.set(f.name);this.nodes.set(structuredClone(f.graph.nodes));this.edges.set(structuredClone(f.graph.edges));this.selected.set('')}
 add(type:'constant'|'operation'){const id=type+'-'+Date.now().toString(36);this.nodes.update(n=>[...n,{id,type,x:240+(n.length%3)*95,y:35+(n.length%3)*90,...(type==='operation'?{operation:'add'}:{value_type:'number' as Kind,value:'1'})}]);this.selected.set(id)}
 edit(field:'value_type'|'value'|'operation',value:string){this.nodes.update(nodes=>nodes.map(n=>n.id===this.selected()?{...n,[field]:value}:n))}
 connect(from:string,to:string,port:string){if(!from||from===to)return;this.edges.update(es=>[...es.filter(e=>!(e.to===to&&(e.to_port||'a')===port)),{from,to,to_port:port}])}
 removeNode(id:string){this.nodes.update(ns=>ns.filter(n=>n.id!==id));this.edges.update(es=>es.filter(e=>e.from!==id&&e.to!==id));this.selected.set('')}
 private async api(url:string,init?:RequestInit):Promise<any>{const res=await fetch(url,{credentials:'same-origin',...init,headers:{'Content-Type':'application/json',...init?.headers}});const data=await res.json();if(!res.ok)throw Error(data.error||'Request failed');return data}
 async refresh(){try{const [f,t]=await Promise.all([this.api('/api/admin/blueprint-functions'),this.api('/api/admin/blueprint-triggers')]);this.functions.set(f.functions||[]);this.triggers.set(t.triggers||[])}catch(e){this.status.set(String(e))}}
 async save(){this.busy.set(true);try{const id=this.currentId();const res:Definition=await this.api(id?'/api/admin/blueprint-functions/'+id:'/api/admin/blueprint-functions',{method:id?'PUT':'POST',body:JSON.stringify({name:this.name(),graph:{nodes:this.nodes(),edges:this.edges()}})});this.currentId.set(res.id);this.status.set('Function saved');await this.refresh();this.functionsChanged.emit()}catch(e){this.status.set(String(e))}finally{this.busy.set(false)}}
 async createTrigger(){this.busy.set(true);try{await this.api('/api/admin/blueprint-triggers',{method:'POST',body:JSON.stringify({event_key:this.eventKey(),payload_type:this.payloadType()})});this.status.set('Trigger created');await this.refresh()}catch(e){this.status.set(String(e))}finally{this.busy.set(false)}}
}
