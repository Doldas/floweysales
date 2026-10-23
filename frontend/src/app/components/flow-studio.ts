import { CanvasGestures } from './canvas-gestures';
import { HostListener, Component, ChangeDetectionStrategy, signal } from '@angular/core';
import { MarkdownRunOutputComponent } from './markdown-run-output';
import { UserspaceFilesComponent } from './userspace-files';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatIconModule } from '@angular/material/icon';
import { MatDialog, MatDialogModule, MatDialogRef, MAT_DIALOG_DATA } from '@angular/material/dialog';
import { inject } from '@angular/core';
import { BlueprintLibraryComponent } from './blueprint-library';
import { AuthState } from '../auth.service';

interface FlowNode { id:string; type:'trigger'|'agent'|'flow'|'constant'|'function'|'save_result'|'load_result'|'transform'|'workspace_read'|'workspace_write'|'output'|'condition'|'merge'|'json_get'|'http_get'|'web_crawl'|'loop'; label:string; operator?:string; property?:string; output_mode?:'markdown'|'json'; model?:string; json_contract?:string; timeout_seconds?:number; max_retries?:number; variable?:string; transform?:string; loop_mode?:string; loop_limit?:number; loop_condition?:string; content_type?:string; prompt?:string; flow_id?:number; function_id?:number; value_type?:'text'|'number'|'boolean'|'json'|'xml'; value?:string; x:number; y:number }
interface FlowEdge { from:string; to:string; from_port?:string; to_port?:string }
interface FlowGraph { version?:number; nodes:FlowNode[]; edges:FlowEdge[] }
interface SavedFlow { id:number; name:string; graph:FlowGraph; enabled:boolean; interval_minutes:number; event_key:string }
interface FlowRunStep {id:number;node:string;label:string;status:string;output:string}
interface FlowRun { id:number; trigger:string; status:string; output:string; error:string }
interface TriggerDefinition {event_key:string;payload_type:'text'|'number'|'boolean'}

@Component({
 selector: 'fse-confirm-delete-flow',
 standalone: true,
 imports: [MatDialogModule, MatButtonModule],
 template: `
   <h2 mat-dialog-title>Delete flow?</h2>
   <mat-dialog-content>
     Delete <strong>{{ data.name }}</strong> and its execution history? This cannot be undone.
   </mat-dialog-content>
   <mat-dialog-actions align="end">
     <button mat-stroked-button (click)="dialog.close(false)">Cancel</button>
     <button mat-flat-button color="warn" (click)="dialog.close(true)">Confirm delete</button>
   </mat-dialog-actions>
 `
})
export class ConfirmDeleteFlowDialog {
 readonly dialog = inject(MatDialogRef<ConfirmDeleteFlowDialog>);
 readonly data = inject<{ name: string }>(MAT_DIALOG_DATA);
}

@Component({
 selector:'fse-flow-studio', standalone:true,
 imports:[CommonModule,FormsModule,MatButtonModule,MatCardModule,MatIconModule,MatDialogModule,BlueprintLibraryComponent,MarkdownRunOutputComponent,UserspaceFilesComponent],
 changeDetection:ChangeDetectionStrategy.Default,
 template:`
 <mat-card class="flow-studio glass">
  <mat-card-header><mat-card-title><mat-icon>account_tree</mat-icon> Agent Flow Studio</mat-card-title></mat-card-header>
  <mat-card-content>
   <p class="muted">Design read-only research automations. No live trades.</p>
   <nav class="studio-tabs" role="tablist" aria-label="Flow Studio sections">
     <button type="button" role="tab" [attr.aria-selected]="studioTab()==='editor'" [class.selected]="studioTab()==='editor'" (click)="studioTab.set('editor')">Flow editor</button>
     @if(auth.user()?.role==='SYS_ADMIN'){
     <button type="button" role="tab" [attr.aria-selected]="studioTab()==='library'" [class.selected]="studioTab()==='library'" (click)="studioTab.set('library')">Function library &amp; triggers</button>
     }
     <button type="button" role="tab" [attr.aria-selected]="studioTab()==='files'" [class.selected]="studioTab()==='files'" (click)="studioTab.set('files')">My Workspace</button>
     <button type="button" role="tab" [attr.aria-selected]="studioTab()==='runs'" [class.selected]="studioTab()==='runs'" (click)="studioTab.set('runs')">Run history</button>
   </nav>
   @if (studioTab()==='editor') {
   <div class="flow-toolbar">
     <label>Saved flows <select [ngModel]="selectedId()" (ngModelChange)="loadFlow(+$event)"><option [ngValue]="0">New flow</option>@for(f of flows();track f.id){<option [ngValue]="f.id">{{f.name}}</option>}</select></label>
     <button mat-stroked-button (click)="newFlow()">New</button>
     <label>Starter templates
      <select [ngModel]="''" (ngModelChange)="applyFlowTemplate($event)">
       <option value="">Choose a template…</option>
       <option value="research">Market research report</option>
       <option value="risk">Risk-score decision</option>
       <option value="event">Event-driven analysis</option>
      </select>
     </label>
     <button mat-stroked-button color="warn" (click)="confirmDeleteFlow()" [disabled]="!selectedId()||busy()">Delete flow</button>
     <button mat-stroked-button (click)="addAgent()">+ Agent</button>
     <button mat-stroked-button (click)="addV2Node('condition')">+ If / Else</button>
     <button mat-stroked-button (click)="addV2Node('merge')">+ Merge</button>
     <button mat-stroked-button (click)="addV2Node('json_get')">+ JSON Field</button>
     <button mat-stroked-button (click)="addPureLoop()">+ Loop</button>
     <button mat-stroked-button (click)="addHTTPGet()">+ HTTP JSON/XML</button>
     <button mat-stroked-button (click)="addWebCrawler()">+ Web crawler</button>
     <button mat-stroked-button (click)="addOutput()">+ Output</button>
     @if(auth.user()?.role==='SYS_ADMIN'){<button mat-stroked-button (click)="addSubflow()">+ Call Flow</button>}
     <button mat-stroked-button (click)="addConstant()">+ Value</button>
     @if(auth.user()?.role==='SYS_ADMIN'){<button mat-stroked-button (click)="addFunctionCall()">+ Function</button>}
     @if(auth.user()?.role==='SYS_ADMIN'){<button mat-stroked-button (click)="addUtility('save_result')">+ Save Result</button>}
     @if(auth.user()?.role==='SYS_ADMIN'){<button mat-stroked-button (click)="addUtility('load_result')">+ Load Result</button>}
     <button mat-stroked-button (click)="addUtility('transform')">+ Transform</button>
     @if(auth.user()?.role==='SYS_ADMIN'){<button mat-stroked-button (click)="addUtility('workspace_read')">+ Read File</button>}
     @if(auth.user()?.role==='SYS_ADMIN'){<button mat-stroked-button (click)="addUtility('workspace_write')">+ Write File</button>}
     <button mat-stroked-button (click)="linkSelected()" [disabled]="!selectedNode()||graph().nodes.length<3">Connect to…</button>
     <button mat-stroked-button (click)="removeSelected()" [disabled]="!selectedNode()||selectedNode()?.type==='trigger'">Remove node</button>
   </div>
   <div class="flow-meta">
    <label>Flow name <input [ngModel]="name()" (ngModelChange)="name.set($event)" maxlength="120"></label>
    @if(auth.user()?.role==='SYS_ADMIN'){
    <label>Repeat every (minutes, 0 = off) <input type="number" min="0" max="10080" [ngModel]="interval()" (ngModelChange)="interval.set(+$event)"></label>
    <label>Event key <input [ngModel]="eventKey()" (ngModelChange)="eventKey.set($event)" placeholder="e.g. market.close"></label>
    <label class="flow-check"><input type="checkbox" [ngModel]="enabled()" (ngModelChange)="enabled.set($event)"> Enabled</label>
    } @else {<p class="muted">Your flows are manual-only and cannot use admin-only nodes.</p>}
   </div>
   <div class="blueprint-layout">
   <div class="canvas-shell">
    <div class="canvas-navigation" aria-label="Canvas navigation">
      <button type="button" (click)="zoomCanvas(0.8)" aria-label="Zoom in">＋</button>
      <span>{{ canvasZoomPercent() }}%</span>
      <button type="button" (click)="zoomCanvas(1.25)" aria-label="Zoom out">－</button>
      <button type="button" (click)="resetCanvas()" aria-label="Reset canvas view">Reset view</button>
    </div>
    <div class="flow-workspace" aria-label="Agent flow graph editor">
    <svg [attr.viewBox]="canvasViewBox()" (wheel)="onCanvasWheel($event)" (pointerdown)="onCanvasPointerDown($event)" (click)="closeQuickNodeOnCanvas($event)" (pointermove)="onCanvasPointerMove($event)" (pointerup)="onCanvasPointerEnd($event)" (pointercancel)="onCanvasPointerEnd($event)" (contextmenu)="onCanvasContextMenu($event)" role="img" aria-label="Agent nodes and directed connections">
     <defs><marker id="flow-arrow" markerWidth="9" markerHeight="9" refX="8" refY="4" orient="auto"><path d="M0,0 L8,4 L0,8 Z" fill="#72b7ef"/></marker></defs>
     @for(edge of graph().edges;track edge.from+edge.to){
       <line [attr.x1]="node(edge.from)?.x!+125" [attr.y1]="node(edge.from)?.y!+35"
             [attr.x2]="node(edge.to)?.x" [attr.y2]="node(edge.to)?.y!+35"
             stroke="#72b7ef" stroke-width="2.5" marker-end="url(#flow-arrow)"/>
     }
     @for(n of graph().nodes;track n.id){
       <g data-canvas-node (click)="$event.stopPropagation();openQuickNode(n.id)" (pointerdown)="dragStart($event,n.id)" style="cursor:move" tabindex="0"
          (keydown.enter)="openQuickNode(n.id)" [attr.aria-label]="'Select '+n.label">
         <rect [attr.x]="n.x" [attr.y]="n.y" width="125" height="70" rx="12"
           [attr.fill]="n.id===selectedNode()?.id?'#244970':'#1c2a3b'"
           [attr.stroke]="n.id===selectedNode()?.id?'#94caff':'#607993'" stroke-width="2"/>
         <text [attr.x]="n.x+12" [attr.y]="n.y+24" fill="#dceaf8" font-size="13">{{n.type==='trigger'?'⏱ TRIGGER':n.type==='flow'?'↪ SUBFLOW':n.type==='constant'?'◆ VALUE':n.type==='function'?'ƒ FUNCTION':n.type==='save_result'?'▣ SAVE':n.type==='load_result'?'▤ LOAD':n.type==='transform'?'⇄ TRANSFORM':n.type==='workspace_read'?'▤ READ FILE':n.type==='workspace_write'?'▣ WRITE FILE':n.type==='output'?'→ OUTPUT':n.type==='condition'?'◇ IF / ELSE':n.type==='merge'?'⇉ MERGE':n.type==='json_get'?'{} JSON FIELD':'✦ AGENT'}}</text>
         <text [attr.x]="n.x+12" [attr.y]="n.y+48" fill="#dceaf8" font-size="12">{{n.label.slice(0,16)}}</text>
       </g>
     }
    </svg>
   </div>
   @if(quickNode();as q){<div class="node-quick-tooltip" (click)="$event.stopPropagation()" aria-label="Quick node settings">
     <div class="quick-tooltip-title"><strong>⚙ {{q.label}}</strong><button type="button" class="quick-tooltip-close" (click)="closeQuickNode()" aria-label="Close quick settings" title="Close">×</button></div>
     <label>Name <input [ngModel]="q.label" (ngModelChange)="updateNodeFor(q.id,'label',$event)" maxlength="100"></label>
     @if(q.type==='agent'){
       <label>Format <select [ngModel]="q.output_mode||'markdown'" (ngModelChange)="updateNodeFor(q.id,'output_mode',$event)">
         <option value="markdown">Markdown</option><option value="json">JSON only</option>
       </select></label>
       <label>Prompt <textarea rows="3" [ngModel]="q.prompt||''" (ngModelChange)="updateNodeFor(q.id,'prompt',$event)" maxlength="2000"></textarea></label>
     }
     @if(q.type==='constant'||q.type==='workspace_read'||q.type==='workspace_write'){
       <label>{{q.type==='constant'?'Value':'File path'}} <input [ngModel]="q.value||''" (ngModelChange)="updateNodeFor(q.id,'value',$event)"></label>
     }
     <button type="button" (click)="select(q.id);hoveredNode.set('')">Full details</button>
     <button type="button" (click)="closeQuickNode()">Close</button>
   </div>}
   @if(canvasMenu()){<div class="canvas-context-menu" role="menu" aria-label="Create node">
     <strong>Add node</strong>
     <button type="button" role="menuitem" (click)="addAgent();closeCanvasMenu()">Research Agent</button>
     <button type="button" role="menuitem" (click)="addOutput();closeCanvasMenu()">Output Result</button>
     <button type="button" role="menuitem" (click)="addSubflow();closeCanvasMenu()">Call Flow</button>
     <button type="button" role="menuitem" (click)="addConstant();closeCanvasMenu()">Value</button>
     <button type="button" role="menuitem" (click)="addFunctionCall();closeCanvasMenu()">Function</button>
     <button type="button" role="menuitem" (click)="addUtility('save_result');closeCanvasMenu()">Save Result</button>
     <button type="button" role="menuitem" (click)="addUtility('load_result');closeCanvasMenu()">Load Result</button>
     <button type="button" role="menuitem" (click)="addUtility('transform');closeCanvasMenu()">Transform Text</button>
     <button type="button" role="menuitem" (click)="addUtility('workspace_read');closeCanvasMenu()">Read Workspace File</button>
     <button type="button" role="menuitem" (click)="addUtility('workspace_write');closeCanvasMenu()">Write Workspace File</button>
     <button type="button" (click)="closeCanvasMenu()">Cancel</button>
   </div>}
   </div>
   <aside class="flow-node-editor" aria-label="Node details" aria-live="polite">
   <header class="inspector-heading"><span>DETAILS</span><strong>{{ selectedNode()?.label || 'No node selected' }}</strong></header>
   @if(selectedNode();as n){
     <div class="inspector-fields">
       @if(n.type!=='trigger'){
         <label>Connect output from <select #nodeSource><option value="">Choose upstream node</option>@for(other of graph().nodes;track other.id){@if(other.id!==n.id&&other.type!=='output'){<option [value]="other.id">{{other.label}}</option>}}</select></label>
         <button mat-stroked-button (click)="connect(nodeSource.value,n.id)" [disabled]="!nodeSource.value">Connect nodes</button>
         <div class="field-mapper">
          <strong>Map JSON field → input</strong>
          <label>Source agent<select #mappingSource><option value="">Choose JSON agent</option>
           @for(other of graph().nodes;track other.id){@if(other.id!==n.id&&other.type==='agent'&&other.output_mode==='json'){<option [value]="other.id">{{other.label}}</option>}}
          </select></label>
          @if(mappingSource.value){
           <label>Available fields<select #mappingField><option value="">Choose property</option>
             @for(field of nodeContractFields(mappingSource.value);track field){<option [value]="field">{{field}}</option>}
           </select></label>
           <button mat-stroked-button [disabled]="!mappingField.value" (click)="mapField(mappingSource.value,n.id,mappingField.value)">Connect field</button>
          }
          @for(edge of mappedInputs(n.id);track edge.from+edge.from_port){
           <div class="mapped-edge"><span>{{node(edge.from)?.label}}.{{edge.from_port?.slice(6)}} → input</span>
             <button type="button" (click)="removeMappedEdge(edge)" [attr.aria-label]="'Remove mapping from '+edge.from">Remove</button>
           </div>
          }
         </div>
       }
       <label>Label <input [ngModel]="n.label" (ngModelChange)="updateNode('label',$event)" maxlength="100"></label>
       @if(n.type==='agent') {
        <label>AI Model
         <select [ngModel]="n.model||''" (ngModelChange)="updateNodeFor(n.id,'model',$event)">
          <option value="">System default ({{defaultAIModel()||'configured by administrator'}})</option>
          @for(model of availableModels();track model.model){<option [value]="model.model">{{model.label}} · {{model.model}}</option>}
         </select>
        </label>
        <p class="muted">Leave blank to use the system default. Overrides use the configured AI provider endpoint; the named model must exist there.</p>
        <label>Output format <select [ngModel]="n.output_mode||'markdown'" (ngModelChange)="updateNode('output_mode',$event)">
          <option value="markdown">Markdown / Default</option><option value="json">JSON only (strict validation)</option>
        </select></label>
        <label>Timeout (seconds, 0 = default 45) <input type="number" min="0" max="90" step="1" [ngModel]="n.timeout_seconds||0" (ngModelChange)="updateAgentPolicy(n.id,'timeout_seconds',+$event)"></label>
        <label>Retries on failure (0–2) <input type="number" min="0" max="2" step="1" [ngModel]="n.max_retries||0" (ngModelChange)="updateAgentPolicy(n.id,'max_retries',+$event)"></label>
        <p class="muted">Retries only apply to read-only agent calls. Backoff: 1s, then 2s. Other nodes will never repeat automatically.</p>
        @if(n.output_mode==='json'){
          <label>JSON output contract <textarea rows="8" spellcheck="false" [ngModel]="n.json_contract||''" (ngModelChange)="updateNodeFor(n.id,'json_contract',$event)" maxlength="3000" placeholder='{"type":"object","required":["symbol"],"properties":{"symbol":{"type":"string"},"score":{"type":"number"}}}'></textarea></label>
          <p class="muted">Optional contract: object properties, required fields and primitive types. Invalid results fail rather than reaching downstream nodes.</p>
        }
        <label>Research instructions<textarea rows="3" [ngModel]="n.prompt" (ngModelChange)="updateNode('prompt',$event)" maxlength="2000"></textarea></label>

       }
       @if(n.type==='condition'){
         <label>Condition <select [ngModel]="n.operator||'equals'" (ngModelChange)="updateNodeFor(n.id,'operator',$event)">
           <option value="equals">Equals</option><option value="not_equals">Not equals</option>
           <option value="contains">Contains</option><option value="greater_than">Greater than</option><option value="less_than">Less than</option>
         </select></label>
         <label>Compare with <input [ngModel]="n.value||''" (ngModelChange)="updateNodeFor(n.id,'value',$event)"></label>
         <label>True → <select #trueTarget><option value="">Select node</option>@for(other of graph().nodes;track other.id){@if(other.id!==n.id){<option [value]="other.id">{{other.label}}</option>}}</select></label>
         <button mat-stroked-button (click)="connectBranch(n.id,trueTarget.value,'true')">Connect TRUE</button>
         <label>False → <select #falseTarget><option value="">Select node</option>@for(other of graph().nodes;track other.id){@if(other.id!==n.id){<option [value]="other.id">{{other.label}}</option>}}</select></label>
         <button mat-stroked-button (click)="connectBranch(n.id,falseTarget.value,'false')">Connect FALSE</button>
       }
       @if(n.type==='json_get'){
         <label>JSON property <input [ngModel]="n.property||''" (ngModelChange)="updateNodeFor(n.id,'property',$event)" maxlength="48" placeholder="score"></label>
       }
       @if(n.type==='merge'){<p class="muted">Combines active incoming values into a JSON array.</p>}
       @if(n.type==='output'){
        <p class="muted">Default behavior: display the incoming agent result. Connect an Agent node to this output, or connect the Agent directly to another processing node.</p>

       }
       @if(n.type==='constant'){
         <label>Value type <select [ngModel]="n.value_type||'text'" (ngModelChange)="updateConstant('value_type',$event)"><option value="text">Text</option><option value="number">Number</option><option value="boolean">Boolean</option></select></label>
         <label>Value <input [ngModel]="n.value||''" (ngModelChange)="updateConstant('value',$event)" maxlength="2000" placeholder="Literal value"></label>
         <p class="muted">Use a text value as input to an agent. Number and Boolean values have separate types and cannot connect directly to text inputs.</p>
         <label>Connect value to <select #valueTarget><option value="">Select target</option>@for(other of graph().nodes;track other.id){@if(other.type==='agent'||other.type==='flow'){<option [value]="other.id">{{other.label}}</option>}}</select></label>
         <button mat-stroked-button (click)="connectValue(n.id,valueTarget.value)">Connect typed value</button>
       }
       @if(n.type==='save_result'||n.type==='load_result'){
         <label>Run variable <input [ngModel]="n.variable||''" (ngModelChange)="updateUtility('variable',$event)" placeholder="e.g. summary" maxlength="48"></label>
         <p class="muted">Variables exist only while the current flow is executing, not across runs.</p>
       }
       @if(n.type==='transform'){
         <label>Transformation <select [ngModel]="n.transform||'trim'" (ngModelChange)="updateUtility('transform',$event)">
           <option value="trim">Trim whitespace</option><option value="uppercase">UPPERCASE</option><option value="lowercase">lowercase</option>
           <option value="prefix">Add prefix</option><option value="suffix">Add suffix</option><option value="replace">Replace text</option>
         </select></label>
         @if(n.transform==='prefix'||n.transform==='suffix'||n.transform==='replace'){
           <label>Operand <input [ngModel]="n.value||''" (ngModelChange)="updateUtility('value',$event)" maxlength="1000" [placeholder]="n.transform==='replace'?'find=>replacement':'Text to add'"></label>
         }
       }
       @if(n.type==='web_crawl'){
        <label>Public start URL<input [ngModel]="n.value||''" (ngModelChange)="updateUtility('value',$event)" maxlength="2048" placeholder="https://example.com/research"></label>
        <label>Maximum pages (1–3)<input type="number" min="1" max="3" step="1" [ngModel]="n.loop_limit||1" (ngModelChange)="updateLoop(n.id,'loop_limit',+$event)"></label>
        <p class="muted">Fetches text from public HTML pages on the same origin only. No redirects or scripts; max 64 KiB per page. Output is a JSON array of pages and extracted text.</p>
       }
       @if(n.type==='http_get'){
        <label>Public URL (HTTP GET only) <input [ngModel]="n.value||''" (ngModelChange)="updateUtility('value',$event)" maxlength="2048" placeholder="https://api.example.com/data"></label>
        <label>Expected response<select [ngModel]="n.value_type||'json'" (ngModelChange)="updateUtility('value_type',$event)">
           <option value="json">JSON</option><option value="xml">XML</option>
         </select></label>
        <p class="muted">Read-only. Max 64 KiB and 15 seconds. Internal network addresses and redirects are blocked.</p>
       }
       @if(n.type==='loop'){
        <label>Loop type<select [ngModel]="n.loop_mode||'for'" (ngModelChange)="updateLoop(n.id,'loop_mode',$event)">
          <option value="for">For</option><option value="foreach">For each JSON array element</option>
          <option value="while">While</option><option value="do_while">Do while</option>
        </select></label>
        <label>Max iterations (1–100)<input type="number" min="1" max="100" [ngModel]="n.loop_limit||10" (ngModelChange)="updateLoop(n.id,'loop_limit',+$event)"></label>
        <label>Transformation<select [ngModel]="n.transform||'increment'" (ngModelChange)="updateLoop(n.id,'transform',$event)">
         <option value="increment">Increment number</option><option value="decrement">Decrement number</option>
         <option value="trim">Trim text</option><option value="uppercase">Uppercase</option><option value="lowercase">Lowercase</option>
         <option value="prefix">Prefix</option><option value="suffix">Suffix</option>
        </select></label>
        @if(n.transform==='prefix'||n.transform==='suffix'){
          <label>Text operand<input [ngModel]="n.value||''" (ngModelChange)="updateLoop(n.id,'value',$event)"></label>
        }
        @if(n.loop_mode==='while'||n.loop_mode==='do_while'){
         <label>Continue while<select [ngModel]="n.operator||'less_than'" (ngModelChange)="updateLoop(n.id,'operator',$event)">
          <option value="less_than">Less than</option><option value="greater_than">Greater than</option>
          <option value="equals">Equals</option><option value="not_equals">Not equals</option>
         </select></label>
         <label>Comparison<input [ngModel]="n.loop_condition||'10'" (ngModelChange)="updateLoop(n.id,'loop_condition',$event)"></label>
        }
        <p class="muted">Pure data loop only. Never executes AI models, HTTP calls or workspace writes inside iterations.</p>
       }
       @if(n.type==='workspace_read'||n.type==='workspace_write'){
        <label>Userspace path <input [ngModel]="n.value||''" (ngModelChange)="updateUtility('value',$event)" maxlength="512" placeholder="/research/summary.md"></label>
        @if(n.type==='workspace_write'){
         <label>File format <select [ngModel]="n.content_type||'text/plain'" (ngModelChange)="updateUtility('content_type',$event)">
          <option value="text/plain">Plain text (.txt)</option><option value="text/markdown">Markdown (.md)</option>
          <option value="application/json">JSON (.json)</option><option value="text/csv">CSV (.csv)</option>
         </select></label>
        }
        <p class="muted">Uses the flow owner's private userspace. Write File saves the previous result; Read File loads its contents.</p>
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
   } @else { <p class="muted">Select a node in the graph to edit its properties here.</p> }
   </aside>
   </div>
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
   @if (studioTab()==='files') { <fse-userspace-files></fse-userspace-files> }
   @if (studioTab()==='runs') {
   <div class="run-history-heading"><h3>Recent runs</h3>
     <label>Output view <select [ngModel]="logView()" (ngModelChange)="logView.set($event)">
       <option value="markdown">Markdown (default)</option><option value="raw">RAW</option><option value="json">Structured JSON</option>
     </select></label>
     <button mat-stroked-button (click)="refreshRuns()" [disabled]="!selectedId()">Refresh history</button>
     <label>Compare with run<select [ngModel]="compareRun()" (ngModelChange)="compareAgainst(+$event)">
      <option [ngValue]="0">No comparison</option>
      @for(other of runs();track other.id){<option [ngValue]="other.id">#{{other.id}} · {{other.status}}</option>}
     </select></label>
   </div>
   @if(!runs().length){<p class="muted">No executions yet. Save and run a flow to see a trace.</p>}
   @for(run of runs();track run.id){
    <details class="flow-run"><summary>#{{run.id}} · {{run.trigger}} · {{run.status}}</summary>
      @if(run.status==='RUNNING'){<button mat-stroked-button color="warn" (click)="cancelRun(run.id)" [disabled]="busy()">Cancel run</button>}
      <button mat-stroked-button (click)="showRunSteps(run.id)">{{traceRunId()===run.id?'Hide node trace':'Inspect nodes'}}</button>
      @if(traceRunId()===run.id){
       @if(traceLoading()){<p role="status">Loading node trace…</p>}
       @else if(!traceSteps().length){<p class="muted">No node trace available for this run. Earlier runs may predate node tracing.</p>}
       @else{
        <div class="run-step-list" aria-label="Node execution trace">
         @for(step of traceSteps();track step.id){
          <details class="run-step">
           <summary><strong [class.step-skipped]="step.status==='SKIPPED'">{{step.status}}</strong> · {{step.label||step.node}} <span class="muted">{{step.node}}</span>
            @if(compareRun()&&compareRun()!==run.id){<span class="muted"> · {{compareStatus(step.node,step.status,step.output)}}</span>}
           </summary>
           @if(step.output){<pre>{{step.output}}</pre>}
           @if(step.status==='SUCCESS'&&step.output&&step.output.length<4000){
            <button mat-stroked-button (click)="previewReplay(run.id,step.node)">Replay preview from this node</button>
           }
          </details>
         }
        </div>
       }
      }
      @if(replaySource()===run.id&&replaySteps().length){
       <section class="run-step-list" aria-label="Side-effect-free replay preview">
        <strong>Replay preview (saved graph version)</strong>
        @for(step of replaySteps();track $index){
         <details class="run-step">
          <summary>{{step.status}} · {{step.label||step.node}}</summary>
          @if(step.input){<p>Input</p><pre>{{step.input}}</pre>}
          @if(step.output){<p>Output</p><pre>{{step.output}}</pre>}
         </details>
        }
       </section>
      }
      @if(run.error){<p>{{run.error}}</p>}
      @if(run.output){
        @if(logView()==='raw'){<pre>{{run.output}}</pre>}
        @else if(logView()==='json'){<pre>{{formattedRunJSON(run.output)}}</pre>}
        @else {<fse-markdown-run-output [raw]="run.output"></fse-markdown-run-output>}
      }
    </details>
   }
   }
  </mat-card-content>
 </mat-card>`,
 styles:[`
 .canvas-shell{position:relative;min-width:0;min-height:0}
 .canvas-navigation{display:flex;align-items:center;gap:5px;flex-wrap:wrap;padding:8px;background:#14263a;border:1px solid #45627b;border-radius:8px 8px 0 0}
 .canvas-navigation button,.canvas-context-menu button{background:#203a53;color:#e5f3ff;border:1px solid #527b99;border-radius:5px;min-height:34px;padding:6px 10px;cursor:pointer}
 .canvas-navigation button:focus-visible,.canvas-context-menu button:focus-visible{outline:2px solid #80caff}
 .canvas-navigation span{min-width:48px;text-align:center;font-variant-numeric:tabular-nums}
 .canvas-context-menu{position:absolute;top:55px;left:16px;z-index:5;display:flex;flex-direction:column;gap:6px;min-width:180px;padding:12px;background:#15283d;border:1px solid #77b9e9;border-radius:10px;box-shadow:0 10px 35px #0008}
 
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
 svg{min-width:660px;width:100%;height:auto;display:block;touch-action:none;cursor:grab}
 .node-quick-tooltip{position:absolute;right:14px;top:62px;z-index:7;display:flex;flex-direction:column;gap:8px;padding:12px;width:min(290px,85%);max-height:65vh;overflow:auto;background:rgba(26,51,74,.89);backdrop-filter:blur(10px);border:1px solid #8abce4;box-shadow:0 12px 28px #000a;border-radius:10px}
 .quick-tooltip-title{display:flex;align-items:center;justify-content:space-between;gap:12px}
 .quick-tooltip-close{width:26px;height:26px;min-height:26px!important;padding:0!important;flex:0 0 auto;color:#fff!important;background:#b52b38!important;border:1px solid #e15d66!important;border-radius:5px!important;font-size:20px!important;line-height:1!important;cursor:pointer}
 .quick-tooltip-close:hover{background:#db3545!important}
 .node-quick-tooltip label{display:flex;flex-direction:column;gap:4px;min-width:0}
 .node-quick-tooltip input,.node-quick-tooltip textarea,.node-quick-tooltip select{width:100%;box-sizing:border-box}
 .node-quick-tooltip button{min-height:32px}
 .blueprint-layout{display:grid;grid-template-columns:minmax(0,1fr) minmax(270px,315px);align-items:stretch;gap:12px;min-height:470px}
 .blueprint-layout .flow-workspace{min-width:0;min-height:470px}
 .flow-node-editor{margin:0;padding:16px;border:1px solid #355a75;border-radius:10px;background:#142438;min-width:0;max-height:min(75vh,780px);overflow-y:auto}
 .inspector-heading{display:flex;flex-direction:column;gap:6px;border-bottom:1px solid #355a75;padding-bottom:12px;margin-bottom:14px}
 .inspector-heading span{font-size:11px;letter-spacing:.14em;color:#a4c9e7}
 .inspector-fields{display:flex;flex-direction:column;gap:12px}
 .field-mapper{display:flex;flex-direction:column;gap:8px;border:1px solid #4b718b;border-radius:8px;padding:10px}
 .field-mapper strong{font-size:13px}
 .mapped-edge{display:flex;justify-content:space-between;gap:8px;align-items:center;font-size:12px;overflow-wrap:anywhere}
 .mapped-edge button{color:#bce3ff;background:#1b3850;border:1px solid #527b99;border-radius:5px;padding:5px}
 .inspector-fields label{min-width:0;width:100%;flex:auto}
 .inspector-fields input,.inspector-fields select,.inspector-fields textarea{box-sizing:border-box;width:100%;min-width:0}
 @media(max-width:850px){.blueprint-layout{grid-template-columns:minmax(0,1fr)}.blueprint-layout .flow-workspace{min-height:280px}.flow-node-editor{max-height:360px}}

.flow-message{border-left:3px solid #73baff;padding-left:12px}
 .run-history-heading{display:flex;align-items:center;flex-wrap:wrap;gap:16px;margin-bottom:14px}
 .run-history-heading h3{margin:0}
 .run-history-heading label{flex:0 1 220px}
 .flow-run{padding:10px;border-top:1px solid #3d5268}.run-step-list{display:flex;flex-direction:column;gap:5px;margin:12px 0;padding:10px;border:1px solid #50738e;border-radius:8px}.run-step{padding:9px;background:#162d40;border-radius:6px}.run-step summary{cursor:pointer}.step-skipped{opacity:.6}.flow-run pre{white-space:pre-wrap;word-break:break-word;max-height:250px;overflow:auto}
 `]
})
export class FlowStudioComponent {
 readonly auth=inject(AuthState);
 private readonly dialog = inject(MatDialog);
 readonly hoveredNode=signal<string>('');
 readonly quickNode=()=>this.node(this.hoveredNode());
 openQuickNode(id:string):void {this.select(id);this.hoveredNode.set(id);}
 // Closing the quick panel never changes the selected graph node.
 closeQuickNode():void {this.hoveredNode.set('');}
 @HostListener('document:click',['$event'])
 onDocumentClick(event:MouseEvent):void {
  const el=event.target;
  if(el instanceof Element && !el.closest('[data-canvas-node], .node-quick-tooltip'))this.closeQuickNode();
 }
 closeQuickNodeOnCanvas(event:MouseEvent):void {if(!(event.target as Element).closest('[data-canvas-node]'))this.closeQuickNode();}

 readonly canvasView = signal({ x: 0, y: 0, width: 820, height: 380 });
 readonly canvasMenu = signal(false);
 readonly canvasViewBox = () => {
   const v = this.canvasView();
   return `${v.x} ${v.y} ${v.width} ${v.height}`;
 };
 readonly canvasZoomPercent = () => Math.round(820 / this.canvasView().width * 100);
 zoomCanvas(factor: number): void {
   const v = this.canvasView();
   const width = Math.max(820 / 3, Math.min(820 * 4, v.width * factor));
   const height = width * 380 / 820;
   this.canvasView.set({ x: v.x + (v.width - width) / 2, y: v.y + (v.height - height) / 2, width, height });
 }
 panCanvas(dx: number, dy: number): void {
   this.canvasView.update(v => ({ ...v, x: v.x + dx * v.width / 820, y: v.y + dy * v.height / 380 }));
 }
 resetCanvas(): void { this.gestures.reset(); this.canvasView.set({ x: 0, y: 0, width: 820, height: 380 }); this.closeCanvasMenu(); }
 readonly gestures = new CanvasGestures(() => this.canvasView(), view => this.canvasView.set(view), 820, 380);
 onCanvasPointerDown(event: PointerEvent): void { this.gestures.pointerDown(event); }
 onCanvasPointerMove(event: PointerEvent): void { this.gestures.pointerMove(event); }
 onCanvasPointerEnd(event: PointerEvent): void { this.gestures.pointerEnd(event); }
 onCanvasWheel(event: WheelEvent): void { this.gestures.wheel(event); }
 onCanvasContextMenu(event: MouseEvent): void {
   event.preventDefault();
   this.canvasMenu.set(true);
 }
 closeCanvasMenu(): void { this.canvasMenu.set(false); }

 readonly logView=signal<'markdown'|'raw'|'json'>('markdown');
 readonly studioTab=signal<'editor'|'library'|'runs'|'files'>('editor');
 readonly flows=signal<SavedFlow[]>([]);
 readonly availableModels=signal<{model:string;label:string}[]>([]);
 readonly defaultAIModel=signal('');
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
 readonly traceRunId=signal(0);
 readonly traceSteps=signal<FlowRunStep[]>([]);
 readonly traceLoading=signal(false);
 readonly replaySteps=signal<{node:string;label:string;status:string;input?:string;output?:string}[]>([]);
 readonly replaySource=signal(0);
 readonly compareRun=signal(0);
 readonly comparedSteps=signal<{node:string;status:string;output:string}[]>([]);
 readonly busy=signal(false);
 readonly message=signal('');
 private dragging:{id:string;startX:number;startY:number;x:number;y:number}|null=null;
 constructor(){void this.refresh();if(this.auth.user()?.role==='SYS_ADMIN')void this.loadFunctions();void this.loadModels()}
 async loadModels():Promise<void>{
   try{
    const response=await fetch('/api/ai/models/available',{credentials:'same-origin'});
    if(!response.ok)return;
    const data=await response.json();
    this.availableModels.set(data.models||[]);
    this.defaultAIModel.set(data.default_model||'');
   }catch{}
 }
 private defaultGraph():FlowGraph{return {nodes:[
  {id:'start',type:'trigger',label:'Start',x:65,y:150},
  {id:'agent-1',type:'agent',label:'Research',prompt:'Analyze the current market and summarize significant opportunities, risks and evidence.',x:310,y:150}
 ],edges:[{from:'start',to:'agent-1'}]}}
 node(id:string):FlowNode|undefined{return this.graph().nodes.find(n=>n.id===id)}
 select(id:string){this.chosen.set(id)}
 applyFlowTemplate(id:string):void{
  if(!id)return;
  this.newFlow();
  const start:FlowNode={id:'start',type:'trigger',label:'Start',x:45,y:145};
  const agent:FlowNode={id:'research',type:'agent',label:'Research',prompt:'Analyze this company using verifiable evidence and identify uncertainties.',output_mode:'json',
    json_contract:JSON.stringify({type:'object',required:['symbol','score'],properties:{symbol:{type:'string'},score:{type:'number'},summary:{type:'string'}}}),x:230,y:145};
  if(id==='research'){
   this.name.set('Template: Market research report');
   const output:FlowNode={id:'report',type:'output',label:'Report',x:440,y:145};
   this.graph.set({version:2,nodes:[start,agent,output],edges:[{from:'start',to:'research'},{from:'research',to:'report'}]});
  } else if(id==='risk'){
   this.name.set('Template: Risk score decision');
   const condition:FlowNode={id:'risk-check',type:'condition',label:'Risk above 70?',operator:'greater_than',value:'70',x:500,y:145};
   const high:FlowNode={id:'high',type:'output',label:'Review required',x:730,y:90};
   const low:FlowNode={id:'low',type:'output',label:'Low risk',x:730,y:210};
   this.graph.set({version:2,nodes:[start,agent,condition,high,low],edges:[{from:'start',to:'research'},{from:'research',to:'risk-check',from_port:'field:score',to_port:'input'},{from:'risk-check',to:'high',from_port:'true'},{from:'risk-check',to:'low',from_port:'false'}]});
  }else if(id==='event'){
   this.name.set('Template: Event-driven analysis');
   this.eventKey.set('research.requested');
   const output:FlowNode={id:'output',type:'output',label:'Report',x:440,y:145};
   this.graph.set({version:2,nodes:[start,agent,output],edges:[{from:'start',to:'research'},{from:'research',to:'output'}]});
  }
  this.enabled.set(false);
  this.interval.set(0);
  this.message.set('Template copied as a new, disabled flow. Review nodes and save it to keep your own version.');
 }
 newFlow(){this.selectedId.set(0);this.name.set('New research flow');this.interval.set(0);this.eventKey.set('');this.enabled.set(false);this.graph.set(this.defaultGraph());this.runs.set([]);this.traceRunId.set(0);this.traceSteps.set([]);this.compareRun.set(0);this.comparedSteps.set([]);this.replaySteps.set([]);this.replaySource.set(0);this.message.set('')}
 loadFlow(id:number){const f=this.flows().find(x=>x.id===id);if(!f){this.newFlow();return}
  this.selectedId.set(f.id);this.name.set(f.name);this.interval.set(f.interval_minutes);this.eventKey.set(f.event_key||'');this.enabled.set(f.enabled);this.graph.set(structuredClone(f.graph));this.chosen.set('');this.traceRunId.set(0);this.traceSteps.set([]);this.compareRun.set(0);this.comparedSteps.set([]);this.replaySteps.set([]);this.replaySource.set(0);void this.refreshRuns()}
 async loadFunctions(){try{const data=await this.request("/api/admin/blueprint-functions");this.functions.set(data.functions||[])}catch(e){this.message.set(String(e))}}
 addWebCrawler():void {
   const count=this.graph().nodes.length;
   const id='web-crawl-'+Date.now().toString(36);
   this.graph.update(g=>({...g,version:2,nodes:[...g.nodes,{id,type:'web_crawl',label:'Web crawler',value:'https://example.com/',loop_limit:1,x:260+(count%4)*140,y:40+Math.floor(count/4)*100}]}));
   this.chosen.set(id);
 }
 addHTTPGet():void {
   const count=this.graph().nodes.length;
   const id='http-get-'+Date.now().toString(36);
   this.graph.update(g=>({...g,version:2,nodes:[...g.nodes,{id,type:'http_get',label:'HTTP Data',value:'https://api.example.com/data',value_type:'json',x:260+(count%4)*140,y:40+Math.floor(count/4)*100}]}));
   this.chosen.set(id);
 }
 addPureLoop():void{
   const count=this.graph().nodes.length;
   const id='loop-'+Date.now().toString(36);
   this.graph.update(g=>({...g,version:2,nodes:[...g.nodes,{id,type:'loop',label:'Loop',loop_mode:'for',loop_limit:10,transform:'increment',operator:'less_than',loop_condition:'10',x:260+(count%4)*140,y:40+Math.floor(count/4)*100}]}));
   this.chosen.set(id);
 }
 updateLoop(id:string,field:'loop_mode'|'loop_limit'|'transform'|'value'|'operator'|'loop_condition',value:string|number):void{
   const safe=field==='loop_limit'?(Number.isFinite(Number(value))?Math.max(1,Math.min(this.node(id)?.type==='web_crawl'?3:100,Math.trunc(Number(value)))):1):value;
   this.graph.update(g=>({...g,nodes:g.nodes.map(n=>n.id===id?{...n,[field]:safe}:n)}));
 }
 addUtility(type:'save_result'|'load_result'|'transform'|'workspace_read'|'workspace_write'){
   const count=this.graph().nodes.length;
   const id=type+'-'+Date.now().toString(36);
   const label=type==='save_result'?'Save Result':type==='load_result'?'Load Result':type==='workspace_read'?'Read File':type==='workspace_write'?'Write File':'Transform Text';
   this.graph.update(g=>({...g,nodes:[...g.nodes,{id,type,label,variable:'summary',transform:'trim',content_type:'text/plain',value:type==='workspace_read'||type==='workspace_write'?'/research/summary.md':'',x:260+(count%4)*140,y:40+Math.floor(count/4)*100}]}));
   this.chosen.set(id);
 }
 updateUtility(field:'variable'|'transform'|'value'|'content_type'|'value_type',value:string){
   this.graph.update(g=>({...g,nodes:g.nodes.map(n=>n.id===this.chosen()?{...n,[field]:value}:n)}));
 }
 formattedRunJSON(raw:string):string{
   try{return JSON.stringify(JSON.parse(raw),null,2)}catch{return raw}
 }
 addFunctionCall(){const n=this.graph().nodes;const id="fn-"+Date.now().toString(36);this.graph.update(g=>({...g,nodes:[...g.nodes,{id,type:"function",label:"Call function",function_id:0,x:260+(n.length%4)*140,y:40+Math.floor(n.length/4)*100}]}));this.chosen.set(id)}
 updateFunctionReference(target:number){this.graph.update(g=>({...g,nodes:g.nodes.map(n=>n.id===this.chosen()?{...n,function_id:target}:n)}))}
 addConstant(){const n=this.graph().nodes;const id='value-'+Date.now().toString(36);this.graph.update(g=>({...g,nodes:[...g.nodes,{id,type:'constant',label:'Value',value_type:'text',value:'Research this signal',x:260+(n.length%4)*140,y:40+Math.floor(n.length/4)*100}]}));this.chosen.set(id)}
 updateConstant(field:'value_type'|'value',value:string){this.graph.update(g=>({...g,nodes:g.nodes.map(n=>n.id===this.chosen()?{...n,[field]:value}:n)}))}
 connectValue(from:string,to:string){if(!to||this.node(from)?.value_type!=='text'){this.message.set('Typed link requires a text value and an agent or flow input');return}if(this.graph().edges.some(e=>e.from===from&&e.to===to))return;this.graph.update(g=>({...g,edges:[...g.edges,{from,to,from_port:'value',to_port:'input'}]}))}
 addSubflow(){const n=this.graph().nodes;const id='flow-'+Date.now().toString(36);this.graph.update(g=>({...g,nodes:[...g.nodes,{id,type:'flow',label:'Call flow',flow_id:0,x:260+(n.length%4)*140,y:40+Math.floor(n.length/4)*100}]}));this.chosen.set(id)}
 updateFlowReference(target:number){this.graph.update(g=>({...g,nodes:g.nodes.map(n=>n.id===this.chosen()?{...n,flow_id:target}:n)}))}
 updateAgentPolicy(id:string,field:'timeout_seconds'|'max_retries',value:number):void {
   const max=field==='max_retries'?2:90;
   const safe=Number.isFinite(value)?Math.max(0,Math.min(max,Math.trunc(value))):0;
   this.graph.update(g=>({...g,nodes:g.nodes.map(n=>n.id===id?{...n,[field]:safe}:n)}));
 }
  addV2Node(type:'condition'|'merge'|'json_get'){
   const id=type+'-'+Date.now().toString(36);
   this.graph.update(g=>({...g,version:2,nodes:[...g.nodes,{id,type,label:type==='condition'?'If / Else':type==='merge'?'Merge Results':'JSON Field',operator:type==='condition'?'equals':undefined,property:type==='json_get'?'score':undefined,x:260+(g.nodes.length%3)*130,y:65+Math.floor(g.nodes.length/3)*110}]}));
   this.chosen.set(id);
 }
 connectBranch(from:string,to:string,branch:'true'|'false'){
   if(!from||!to||from===to)return;
   this.graph.update(g=>({...g,version:2,edges:[...g.edges.filter(e=>!(e.from===from&&e.from_port===branch)),{from,to,from_port:branch}]}));
 }
 addOutput(){
   const count=this.graph().nodes.length;
   const upstream=this.selectedNode();
   const id='output-'+Date.now().toString(36);
   this.graph.update(g=>({...g,nodes:[...g.nodes,{id,type:'output',label:'Output Result',x:260+(count%4)*140,y:40+Math.floor(count/4)*100}],edges:upstream&&upstream.type!=='output'?[...g.edges,{from:upstream.id,to:id}]:g.edges}));
   this.chosen.set(id);
 }
 addAgent(){const n=this.graph().nodes;const id='agent-'+Date.now().toString(36);const x=260+(n.length%4)*140,y=40+Math.floor(n.length/4)*100;this.graph.update(g=>({...g,nodes:[...g.nodes,{id,type:'agent',label:'Research agent',prompt:'Analyze the evidence and summarize findings.',x,y}]}));this.chosen.set(id)}
 updateNodeFor(id:string,field:'label'|'prompt'|'output_mode'|'value'|'operator'|'property'|'json_contract'|'model',value:string):void {
   this.graph.update(g=>({...g,nodes:g.nodes.map(n=>n.id===id?{...n,[field]:value}:n)}));
 }
 updateNode(field:'label'|'prompt'|'output_mode',value:string){this.graph.update(g=>({...g,nodes:g.nodes.map(n=>n.id===this.chosen()?{...n,[field]:value}:n)}))}
 nodeContractFields(id:string):string[]{
   const contract=this.node(id)?.json_contract;
   if(!contract)return [];
   try{
    const parsed=JSON.parse(contract) as {properties?:Record<string,unknown>};
    return parsed.properties&&typeof parsed.properties==='object'?Object.keys(parsed.properties).filter(k=>/^[a-zA-Z][a-zA-Z0-9_]{0,47}$/.test(k)).slice(0,20):[];
   }catch{return []}
 }
 mappedInputs(id:string):FlowEdge[]{return this.graph().edges.filter(e=>e.to===id&&(e.from_port||'').startsWith('field:'))}
 mapField(from:string,to:string,field:string):void{
   if(!from||!to||from===to||!this.nodeContractFields(from).includes(field))return;
   this.graph.update(g=>({...g,version:2,edges:[...g.edges.filter(e=>e.to!==to),{from,to,from_port:'field:'+field,to_port:'input'}]}));
 }
 removeMappedEdge(edge:FlowEdge):void{
   this.graph.update(g=>({...g,edges:g.edges.filter(e=>e!==edge)}));
 }
  connect(from:string,to:string){if(!from||from===to||this.graph().edges.some(e=>e.from===from&&e.to===to))return;
  this.graph.update(g=>({...g,edges:[...g.edges,{from,to}]}))}
 linkSelected(){const target=this.selectedNode();if(!target)return;const last=this.graph().nodes.find(x=>x.id!==target.id&&x.type==='agent');if(last)this.connect(last.id,target.id)}
 removeSelected(){const id=this.chosen();if(id==='start')return;this.graph.update(g=>({...g,nodes:g.nodes.filter(n=>n.id!==id),edges:g.edges.filter(e=>e.from!==id&&e.to!==id)}));this.chosen.set('')}
 dragStart(ev:PointerEvent,id:string){if(ev.button!==0||ev.pointerType==='touch')return;const n=this.node(id);if(!n)return;this.chosen.set(id);this.dragging={id,startX:ev.clientX,startY:ev.clientY,x:n.x,y:n.y};
  const svg=(ev.currentTarget as SVGGElement).ownerSVGElement;if(!svg)return;
  const move=(event:PointerEvent)=>{const box=svg.getBoundingClientRect();if(!this.dragging)return;
   const view=this.canvasView();const dx=(event.clientX-this.dragging.startX)*view.width/box.width,dy=(event.clientY-this.dragging.startY)*view.height/box.height;
   this.graph.update(g=>({...g,nodes:g.nodes.map(node=>node.id===id?{...node,x:Math.max(0,Math.min(695,this.dragging!.x+dx)),y:Math.max(0,Math.min(310,this.dragging!.y+dy))}:node)}))}
  const stop=()=>{svg.removeEventListener('pointermove',move);svg.removeEventListener('pointerup',stop);svg.removeEventListener('pointercancel',stop);this.dragging=null}
  svg.addEventListener('pointermove',move);svg.addEventListener('pointerup',stop);svg.addEventListener('pointercancel',stop);
 }
 private async request(url:string,options?:RequestInit):Promise<any>{const r=await fetch(url,{credentials:'same-origin',...options,headers:{'Content-Type':'application/json',...options?.headers}});
  const j=await r.json();if(!r.ok)throw new Error(j.error||('HTTP '+r.status));return j}
 async refresh(){
  try{
   const data=await this.request('/api/admin/flows');
   this.flows.set(data.flows||[]);
   if(this.auth.user()?.role==='SYS_ADMIN'){
    try{const definitions=await this.request('/api/admin/blueprint-triggers');this.triggers.set(definitions.triggers||[])}
    catch{this.triggers.set([])}
   }else{this.triggers.set([])}
  }catch(e){this.message.set(String(e))}
 }
 async cancelRun(runId:number){
  if(!this.selectedId()||this.busy())return;
  this.busy.set(true);
  try{
   await this.request('/api/admin/flows/'+this.selectedId()+'/runs/'+runId+'/cancel',{method:'POST'});
   this.message.set('Run cancellation requested');
   await this.refreshRuns();
  }catch(e){this.message.set('Cannot cancel run: '+String(e))}
  finally{this.busy.set(false)}
 }
 async compareAgainst(runId:number){
  if(!this.selectedId()||!runId){this.compareRun.set(0);this.comparedSteps.set([]);return}
  this.compareRun.set(runId);this.comparedSteps.set([]);
  try{
   const data=await this.request('/api/admin/flows/'+this.selectedId()+'/runs/'+runId+'/steps');
   this.comparedSteps.set(data.steps||[]);
  }catch(e){this.message.set('Cannot compare runs: '+String(e))}
 }
 compareStatus(node:string,status:string,output:string):string{
   const other=this.comparedSteps().find(x=>x.node===node);
   if(!this.compareRun()||!other)return 'No baseline';
   return other.status===status&&other.output===output?'Unchanged':other.status!==status?'Status changed':'Output changed';
 }
 async previewReplay(runId:number,nodeId:string){
  if(!this.selectedId())return;
  this.replaySteps.set([]);this.replaySource.set(runId);
  try{
   const result=await this.request('/api/admin/flows/'+this.selectedId()+'/runs/'+runId+'/preview',{method:'POST',body:JSON.stringify({node:nodeId})});
   this.replaySteps.set(result.steps||[]);
   this.message.set('Preview only: no agents or side effects were executed. Uses the saved graph snapshot.');
  }catch(e){this.message.set('Replay preview unavailable: '+String(e))}
 }
 async showRunSteps(runId:number){
  if(!this.selectedId())return;
  if(this.traceRunId()===runId){this.traceRunId.set(0);this.traceSteps.set([]);this.compareRun.set(0);this.comparedSteps.set([]);this.replaySteps.set([]);this.replaySource.set(0);return}
  this.traceRunId.set(runId);this.traceLoading.set(true);this.traceSteps.set([]);
  try{const data=await this.request('/api/admin/flows/'+this.selectedId()+'/runs/'+runId+'/steps');
      if(this.traceRunId()===runId)this.traceSteps.set(data.steps||[]);
  }catch(e){this.message.set('Could not load run trace: '+String(e))}
  finally{this.traceLoading.set(false)}
 }
 async refreshRuns(){if(!this.selectedId())return;try{const d=await this.request('/api/admin/flows/'+this.selectedId()+'/runs');this.runs.set(d.runs||[])}catch(e){this.message.set(String(e))}}
 async confirmDeleteFlow(){
   const id=this.selectedId();
   if(!id||this.busy())return;
   const name=this.flows().find(f=>f.id===id)?.name||this.name();
   const confirmed=await new Promise<boolean>(resolve=>{
     this.dialog.open(ConfirmDeleteFlowDialog,{data:{name},width:'420px',maxWidth:'95vw',autoFocus:'first-tabbable'})
       .afterClosed().subscribe(value=>resolve(value===true));
   });
   if(!confirmed||this.selectedId()!==id)return;
   this.busy.set(true);
   try{
     await this.request('/api/admin/flows/'+id,{method:'DELETE'});
     this.newFlow();
     await this.refresh();
     this.message.set('Flow deleted');
   }catch(e){this.message.set('Delete failed: '+String(e))}
   finally{this.busy.set(false)}
 }
 async save(){this.busy.set(true);try{const id=this.selectedId();const body={name:this.name(),graph:this.graph(),enabled:this.enabled(),interval_minutes:this.interval(),event_key:this.eventKey()};
   const flow:SavedFlow=await this.request(id?'/api/admin/flows/'+id:'/api/admin/flows',{method:id?'PUT':'POST',body:JSON.stringify(body)});
   this.selectedId.set(flow.id);this.message.set('Flow saved');await this.refresh()}catch(e){this.message.set(String(e))}finally{this.busy.set(false)}}
 async run(){this.busy.set(true);try{await this.request('/api/admin/flows/'+this.selectedId()+'/run',{method:'POST'});this.message.set('Agent run queued; refresh history shortly');setTimeout(()=>void this.refreshRuns(),3000)}catch(e){this.message.set(String(e))}finally{this.busy.set(false)}}
 async emitEvent(){this.busy.set(true);try{const definition=this.triggers().find(t=>t.event_key===this.eventKey());if(!definition)throw Error('Create the custom trigger first');let value:string|number|boolean=this.triggerValue();if(definition.payload_type==='number'){value=Number(value);if(!Number.isFinite(value))throw Error('Event requires a finite number')}if(definition.payload_type==='boolean'){if(value!=='true'&&value!=='false')throw Error('Use true or false for a boolean event');value=value==='true'}const data=await this.request('/api/admin/flow-events/'+encodeURIComponent(this.eventKey()),{method:'POST',body:JSON.stringify({value})});this.message.set(data.started+' flow(s) triggered');}catch(e){this.message.set(String(e))}finally{this.busy.set(false)}}
}
