import { Component, ChangeDetectionStrategy, signal } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatIconModule } from '@angular/material/icon';
import { BlueprintLibraryComponent } from './blueprint-library';

interface FlowNode { id:string; type:'trigger'|'agent'|'flow'|'constant'|'function'; label:string; prompt?:string; flow_id?:number; function_id?:number; value_type?:'text'|'number'|'boolean'; value?:string; x:number; y:number }
interface FlowEdge { from:string; to:string; from_port?:string; to_port?:string }
interface FlowGraph { nodes:FlowNode[]; edges:FlowEdge[] }
interface SavedFlow { id:number; name:string; graph:FlowGraph; enabled:boolean; interval_minutes:number; event_key:string }
interface FlowRun { id:number; trigger:string; status:string; output:string; error:string }
interface TriggerDefinition {event_key:string;payload_type:'text'|'number'|'boolean'}

@Component({
 selector:'fse-flow-studio', standalone:true,
 imports:[CommonModule,FormsModule,MatButtonModule,MatCardModule,MatIconModule,BlueprintLibraryComponent],
 changeDetection:ChangeDetectionStrategy.Default,
 template:`
 <mat-card class="flow-studio glass">
  <mat-card-header><mat-card-title><mat-icon>account_tree</mat-icon> Agent Flow Studio</mat-card-title></mat-card-header>
  <mat-card-content>
   <p class="muted">Design read-only research automations. No live trades.</p>
   <nav class="studio-tabs" role="tablist" aria-label="Flow Studio sections">
     <button type="button" role="tab" [attr.aria-selected]="studioTab()==='editor'" [class.selected]="studioTab()==='editor'" (click)="studioTab.set('editor')">Flow editor</button>
     <button type="button" role="tab" [attr.aria-selected]="studioTab()==='library'" [class.selected]="studioTab()==='library'" (click)="studioTab.set('library')">Function library &amp; triggers</button>
     <button type="button" role="tab" [attr.aria-selected]="studioTab()==='runs'" [class.selected]="studioTab()==='runs'" (click)="studioTab.set('runs')">Run history</button>
   </nav>
   @if (studioTab()==='editor') {
   <div class="flow-toolbar">
     <label>Saved flows <select [ngModel]="selectedId()" (ngModelChange)="loadFlow(+$event)"><option [ngValue]="0">New flow</option>@for(f of flows();track f.id){<option [ngValue]="f.id">{{f.name}}</option>}</select></label>
     <button mat-stroked-button (click)="newFlow()">New</button>
     <button mat-stroked-button (click)="addAgent()">+ Agent</button>
     <button mat-stroked-button (click)="addSubflow()">+ Call Flow</button>
     <button mat-stroked-button (click)="addConstant()">+ Value</button>
     <button mat-stroked-button (click)="addFunctionCall()">+ Function</button>
     <button mat-stroked-button (click)="linkSelected()" [disabled]="!selectedNode()||graph().nodes.length<3">Connect to…</button>
     <button mat-stroked-button (click)="removeSelected()" [disabled]="!selectedNode()||selectedNode()?.type==='trigger'">Remove node</button>
   </div>
   <div class="flow-meta">
    <label>Flow name <input [ngModel]="name()" (ngModelChange)="name.set($event)" maxlength="120"></label>
    <label>Repeat every (minutes, 0 = off) <input type="number" min="0" max="10080" [ngModel]="interval()" (ngModelChange)="interval.set(+$event)"></label>
    <label>Event key <input [ngModel]="eventKey()" (ngModelChange)="eventKey.set($event)" placeholder="e.g. market.close"></label>
    <label class="flow-check"><input type="checkbox" [ngModel]="enabled()" (ngModelChange)="enabled.set($event)"> Enabled</label>
   </div>
   <div class="flow-workspace" aria-label="Agent flow graph editor">
    <svg viewBox="0 0 820 380" role="img" aria-label="Agent nodes and directed connections">
     <defs><marker id="flow-arrow" markerWidth="9" markerHeight="9" refX="8" refY="4" orient="auto"><path d="M0,0 L8,4 L0,8 Z" fill="#72b7ef"/></marker></defs>
     @for(edge of graph().edges;track edge.from+edge.to){
       <line [attr.x1]="node(edge.from)?.x!+125" [attr.y1]="node(edge.from)?.y!+35"
             [attr.x2]="node(edge.to)?.x" [attr.y2]="node(edge.to)?.y!+35"
             stroke="#72b7ef" stroke-width="2.5" marker-end="url(#flow-arrow)"/>
     }
     @for(n of graph().nodes;track n.id){
       <g (click)="select(n.id)" (pointerdown)="dragStart($event,n.id)" style="cursor:move" tabindex="0"
          (keydown.enter)="select(n.id)" [attr.aria-label]="'Select '+n.label">
         <rect [attr.x]="n.x" [attr.y]="n.y" width="125" height="70" rx="12"
           [attr.fill]="n.id===selectedNode()?.id?'#244970':'#1c2a3b'"
           [attr.stroke]="n.id===selectedNode()?.id?'#94caff':'#607993'" stroke-width="2"/>
         <text [attr.x]="n.x+12" [attr.y]="n.y+24" fill="#dceaf8" font-size="13">{{n.type==='trigger'?'⏱ TRIGGER':n.type==='flow'?'↪ SUBFLOW':n.type==='constant'?'◆ VALUE':n.type==='function'?'ƒ FUNCTION':'✦ AGENT'}}</text>
         <text [attr.x]="n.x+12" [attr.y]="n.y+48" fill="#dceaf8" font-size="12">{{n.label.slice(0,16)}}</text>
       </g>
     }
    </svg>
   </div>
   @if(selectedNode();as n){
     <div class="flow-node-editor"><strong>Selected node</strong>
       <label>Label <input [ngModel]="n.label" (ngModelChange)="updateNode('label',$event)" maxlength="100"></label>
       @if(n.type==='agent') {
        <label>Research instructions<textarea rows="3" [ngModel]="n.prompt" (ngModelChange)="updateNode('prompt',$event)" maxlength="2000"></textarea></label>
        <label>Connect from <select #source><option value="">Select parent</option>@for(other of graph().nodes;track other.id){@if(other.id!==n.id){<option [value]="other.id">{{other.label}}</option>}}</select></label>
        <button mat-stroked-button (click)="connect(source.value,n.id)">Create connection</button>
       }
       @if(n.type==='constant'){
         <label>Value type <select [ngModel]="n.value_type||'text'" (ngModelChange)="updateConstant('value_type',$event)"><option value="text">Text</option><option value="number">Number</option><option value="boolean">Boolean</option></select></label>
         <label>Value <input [ngModel]="n.value||''" (ngModelChange)="updateConstant('value',$event)" maxlength="2000" placeholder="Literal value"></label>
         <p class="muted">Use a text value as input to an agent. Number and Boolean values have separate types and cannot connect directly to text inputs.</p>
         <label>Connect value to <select #valueTarget><option value="">Select target</option>@for(other of graph().nodes;track other.id){@if(other.type==='agent'||other.type==='flow'){<option [value]="other.id">{{other.label}}</option>}}</select></label>
         <button mat-stroked-button (click)="connectValue(n.id,valueTarget.value)">Connect typed value</button>
       }
       @if(n.type==='function'){
         <label>Reusable function <select [ngModel]="n.function_id||0" (ngModelChange)="updateFunctionReference(+$event)"><option [ngValue]="0">Choose saved function</option>@for(f of functions();track f.id){<option [ngValue]="f.id">{{f.name}}</option>}</select></label>
         <p class="muted">Applies the function to the preceding node's typed value; invalid inputs fail the run safely.</p>
       }
       @if(n.type==='flow'){
         <label>Reusable flow to call <select [ngModel]="n.flow_id||0" (ngModelChange)="updateFlowReference(+$event)"><option [ngValue]="0">Choose target flow</option>@for(f of flows();track f.id){@if(f.id!==selectedId()){<option [ngValue]="f.id">{{f.name}} (#{{f.id}})</option>}}</select></label>
         <p class="muted">Runs the selected graph as a function. Recursion is blocked and nesting is limited to 4 levels.</p>
       }
     </div>
   }
   <div class="flow-actions">
     <button mat-flat-button color="primary" (click)="save()" [disabled]="busy()">Save flow</button>
     <button mat-stroked-button (click)="run()" [disabled]="busy()||!selectedId()">Run now</button>
     <label>Event value <input [ngModel]="triggerValue()" (ngModelChange)="triggerValue.set($event)" placeholder="Value matching trigger type"></label>
     <button mat-stroked-button (click)="emitEvent()" [disabled]="busy()||!eventKey()">Trigger event</button>
     <button mat-stroked-button (click)="refresh()">Refresh</button>
   </div>
   @if(message()) { <p class="flow-message" role="status">{{message()}}</p> }
   }
   @if (studioTab()==='library') {
     <fse-blueprint-library (functionsChanged)="loadFunctions()"></fse-blueprint-library>
   }
   @if (studioTab()==='runs') {
   <h3>Recent runs</h3>
   @if(!runs().length){<p class="muted">No executions yet. Save and run a flow to see a trace.</p>}
   @for(run of runs();track run.id){
    <details class="flow-run"><summary>#{{run.id}} · {{run.trigger}} · {{run.status}}</summary>
      @if(run.error){<p>{{run.error}}</p>}
      @if(run.output){<pre>{{run.output}}</pre>}
    </details>
   }
  </mat-card-content>
 </mat-card>`,
 styles:[`
:host{display:block;margin:0 0 24px}
 .studio-tabs{display:flex;gap:6px;overflow-x:auto;margin:16px 0 20px;border-bottom:1px solid #50738e;padding:0 0 7px}
 .studio-tabs button{flex:0 0 auto;padding:12px 16px;border:1px solid transparent;border-radius:8px;background:transparent;color:#c4d7eb;cursor:pointer;font:inherit;font-weight:600}
 .studio-tabs button.selected{background:#1e435f;border-color:#6aa5c9;color:white}
 .studio-tabs button:focus-visible{outline:2px solid #78c8ef}
.flow-studio{padding:18px}.flow-studio mat-card-title{display:flex;align-items:center;gap:12px}
 .muted{opacity:.7}.flow-toolbar,.flow-meta,.flow-actions{display:flex;flex-wrap:wrap;gap:12px;align-items:end;margin:16px 0}
 label{display:flex;flex-direction:column;gap:4px;font-size:13px;flex:1;min-width:160px}
 input,select,textarea{background:#14263a;color:#e5f3ff;border:1px solid #5e7b93;border-radius:6px;padding:9px;font:inherit}
 textarea{width:100%}.flow-check{flex-direction:row;align-items:center}.flow-check input{width:18px;height:18px}
 .flow-workspace{background:radial-gradient(#294057 1px,transparent 1px) 0 0/20px 20px,#0d1b2a;border:1px solid #45627b;overflow:auto;border-radius:12px}
 svg{min-width:660px;width:100%;height:auto;display:block;touch-action:none}
 .flow-node-editor{margin:12px 0;padding:12px;border:1px solid #355a75;border-radius:8px;display:flex;flex-wrap:wrap;gap:12px}
 .flow-node-editor strong{width:100%}.flow-message{border-left:3px solid #73baff;padding-left:12px}
 .flow-run{padding:10px;border-top:1px solid #3d5268}.flow-run pre{white-space:pre-wrap;word-break:break-word;max-height:250px;overflow:auto}
 `]
})
export class FlowStudioComponent {
 readonly studioTab=signal<'editor'|'library'|'runs'>('editor');
 readonly flows=signal<SavedFlow[]>([]);
 readonly functions=signal<{id:number;name:string}[]>([]);
 readonly selectedId=signal(0);
 readonly name=signal('Daily research');
 readonly enabled=signal(false);
 readonly interval=signal(0);
 readonly eventKey=signal('');
 readonly triggerValue=signal('');
 readonly triggers=signal<TriggerDefinition[]>([]);
 readonly graph=signal<FlowGraph>(this.defaultGraph());
 readonly chosen=signal('');
 readonly selectedNode=()=>this.graph().nodes.find(n=>n.id===this.chosen());
 readonly runs=signal<FlowRun[]>([]);
 readonly busy=signal(false);
 readonly message=signal('');
 private dragging:{id:string;startX:number;startY:number;x:number;y:number}|null=null;
 constructor(){void this.refresh();void this.loadFunctions()}
 private defaultGraph():FlowGraph{return {nodes:[
  {id:'start',type:'trigger',label:'Start',x:65,y:150},
  {id:'agent-1',type:'agent',label:'Research',prompt:'Analyze the current market and summarize significant opportunities, risks and evidence.',x:310,y:150}
 ],edges:[{from:'start',to:'agent-1'}]}}
 node(id:string):FlowNode|undefined{return this.graph().nodes.find(n=>n.id===id)}
 select(id:string){this.chosen.set(id)}
 newFlow(){this.selectedId.set(0);this.name.set('New research flow');this.interval.set(0);this.eventKey.set('');this.enabled.set(false);this.graph.set(this.defaultGraph());this.runs.set([]);this.message.set('')}
 loadFlow(id:number){const f=this.flows().find(x=>x.id===id);if(!f){this.newFlow();return}
  this.selectedId.set(f.id);this.name.set(f.name);this.interval.set(f.interval_minutes);this.eventKey.set(f.event_key||'');this.enabled.set(f.enabled);this.graph.set(structuredClone(f.graph));this.chosen.set('');void this.refreshRuns()}
 async loadFunctions(){try{const data=await this.request("/api/admin/blueprint-functions");this.functions.set(data.functions||[])}catch(e){this.message.set(String(e))}}
 addFunctionCall(){const n=this.graph().nodes;const id="fn-"+Date.now().toString(36);this.graph.update(g=>({...g,nodes:[...g.nodes,{id,type:"function",label:"Call function",function_id:0,x:260+(n.length%4)*140,y:40+Math.floor(n.length/4)*100}]}));this.chosen.set(id)}
 updateFunctionReference(target:number){this.graph.update(g=>({...g,nodes:g.nodes.map(n=>n.id===this.chosen()?{...n,function_id:target}:n)}))}
 addConstant(){const n=this.graph().nodes;const id='value-'+Date.now().toString(36);this.graph.update(g=>({...g,nodes:[...g.nodes,{id,type:'constant',label:'Value',value_type:'text',value:'Research this signal',x:260+(n.length%4)*140,y:40+Math.floor(n.length/4)*100}]}));this.chosen.set(id)}
 updateConstant(field:'value_type'|'value',value:string){this.graph.update(g=>({...g,nodes:g.nodes.map(n=>n.id===this.chosen()?{...n,[field]:value}:n)}))}
 connectValue(from:string,to:string){if(!to||this.node(from)?.value_type!=='text'){this.message.set('Typed link requires a text value and an agent or flow input');return}if(this.graph().edges.some(e=>e.from===from&&e.to===to))return;this.graph.update(g=>({...g,edges:[...g.edges,{from,to,from_port:'value',to_port:'input'}]}))}
 addSubflow(){const n=this.graph().nodes;const id='flow-'+Date.now().toString(36);this.graph.update(g=>({...g,nodes:[...g.nodes,{id,type:'flow',label:'Call flow',flow_id:0,x:260+(n.length%4)*140,y:40+Math.floor(n.length/4)*100}]}));this.chosen.set(id)}
 updateFlowReference(target:number){this.graph.update(g=>({...g,nodes:g.nodes.map(n=>n.id===this.chosen()?{...n,flow_id:target}:n)}))}
 addAgent(){const n=this.graph().nodes;const id='agent-'+Date.now().toString(36);const x=260+(n.length%4)*140,y=40+Math.floor(n.length/4)*100;this.graph.update(g=>({...g,nodes:[...g.nodes,{id,type:'agent',label:'Research agent',prompt:'Analyze the evidence and summarize findings.',x,y}]}));this.chosen.set(id)}
 updateNode(field:'label'|'prompt',value:string){this.graph.update(g=>({...g,nodes:g.nodes.map(n=>n.id===this.chosen()?{...n,[field]:value}:n)}))}
 connect(from:string,to:string){if(!from||from===to||this.graph().edges.some(e=>e.from===from&&e.to===to))return;
  this.graph.update(g=>({...g,edges:[...g.edges,{from,to}]}))}
 linkSelected(){const target=this.selectedNode();if(!target)return;const last=this.graph().nodes.find(x=>x.id!==target.id&&x.type==='agent');if(last)this.connect(last.id,target.id)}
 removeSelected(){const id=this.chosen();if(id==='start')return;this.graph.update(g=>({...g,nodes:g.nodes.filter(n=>n.id!==id),edges:g.edges.filter(e=>e.from!==id&&e.to!==id)}));this.chosen.set('')}
 dragStart(ev:PointerEvent,id:string){if(ev.button!==0)return;const n=this.node(id);if(!n)return;this.chosen.set(id);this.dragging={id,startX:ev.clientX,startY:ev.clientY,x:n.x,y:n.y};
  const svg=(ev.currentTarget as SVGGElement).ownerSVGElement;if(!svg)return;
  const move=(event:PointerEvent)=>{const box=svg.getBoundingClientRect();if(!this.dragging)return;
   const dx=(event.clientX-this.dragging.startX)*820/box.width,dy=(event.clientY-this.dragging.startY)*380/box.height;
   this.graph.update(g=>({...g,nodes:g.nodes.map(node=>node.id===id?{...node,x:Math.max(0,Math.min(695,this.dragging!.x+dx)),y:Math.max(0,Math.min(310,this.dragging!.y+dy))}:node)}))}
  const stop=()=>{svg.removeEventListener('pointermove',move);svg.removeEventListener('pointerup',stop);svg.removeEventListener('pointercancel',stop);this.dragging=null}
  svg.addEventListener('pointermove',move);svg.addEventListener('pointerup',stop);svg.addEventListener('pointercancel',stop);
 }
 private async request(url:string,options?:RequestInit):Promise<any>{const r=await fetch(url,{credentials:'same-origin',...options,headers:{'Content-Type':'application/json',...options?.headers}});
  const j=await r.json();if(!r.ok)throw new Error(j.error||('HTTP '+r.status));return j}
 async refresh(){try{const [data,definitions]=await Promise.all([this.request('/api/admin/flows'),this.request('/api/admin/blueprint-triggers')]);this.flows.set(data.flows||[]);this.triggers.set(definitions.triggers||[])}catch(e){this.message.set(String(e))}}
 async refreshRuns(){if(!this.selectedId())return;try{const d=await this.request('/api/admin/flows/'+this.selectedId()+'/runs');this.runs.set(d.runs||[])}catch(e){this.message.set(String(e))}}
 async save(){this.busy.set(true);try{const id=this.selectedId();const body={name:this.name(),graph:this.graph(),enabled:this.enabled(),interval_minutes:this.interval(),event_key:this.eventKey()};
   const flow:SavedFlow=await this.request(id?'/api/admin/flows/'+id:'/api/admin/flows',{method:id?'PUT':'POST',body:JSON.stringify(body)});
   this.selectedId.set(flow.id);this.message.set('Flow saved');await this.refresh()}catch(e){this.message.set(String(e))}finally{this.busy.set(false)}}
 async run(){this.busy.set(true);try{await this.request('/api/admin/flows/'+this.selectedId()+'/run',{method:'POST'});this.message.set('Agent run queued; refresh history shortly');setTimeout(()=>void this.refreshRuns(),3000)}catch(e){this.message.set(String(e))}finally{this.busy.set(false)}}
 async emitEvent(){this.busy.set(true);try{const definition=this.triggers().find(t=>t.event_key===this.eventKey());if(!definition)throw Error('Create the custom trigger first');let value:string|number|boolean=this.triggerValue();if(definition.payload_type==='number'){value=Number(value);if(!Number.isFinite(value))throw Error('Event requires a finite number')}if(definition.payload_type==='boolean'){if(value!=='true'&&value!=='false')throw Error('Use true or false for a boolean event');value=value==='true'}const data=await this.request('/api/admin/flow-events/'+encodeURIComponent(this.eventKey()),{method:'POST',body:JSON.stringify({value})});this.message.set(data.started+' flow(s) triggered');}catch(e){this.message.set(String(e))}finally{this.busy.set(false)}}
}
