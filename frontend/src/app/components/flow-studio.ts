import { CanvasGestures } from './canvas-gestures';
import { HostListener, Component, ChangeDetectionStrategy, signal, effect } from '@angular/core';
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

interface FlowNode { id:string; type:'trigger'|'agent'|'flow'|'constant'|'function'|'save_result'|'load_result'|'transform'|'workspace_read'|'workspace_write'|'output'|'condition'|'merge'|'json_get'|'http_get'|'web_crawl'|'web_search'|'loop'; label:string; operator?:string; property?:string; output_mode?:'markdown'|'json'; model?:string; json_contract?:string; timeout_seconds?:number; max_retries?:number; variable?:string; transform?:string; loop_mode?:string; loop_limit?:number; loop_condition?:string; content_type?:string; prompt?:string; flow_id?:number; function_id?:number; value_type?:'text'|'number'|'boolean'|'json'|'xml'; value?:string; x:number; y:number }
interface FlowEdge { from:string; to:string; from_port?:string; to_port?:string }
interface FlowGraph { version?:number; nodes:FlowNode[]; edges:FlowEdge[] }
interface SavedFlow { id:number; name:string; graph:FlowGraph; enabled:boolean; interval_minutes:number; event_key:string }
interface FlowRunStep {id:number;node:string;label:string;status:string;output:string;created_at?:string;duration_ms?:number|null}
interface FlowRun { id:number; trigger:string; status:string; output:string; error:string; started_at?:string; finished_at?:string|null; duration_ms?:number|null }
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
     <button type="button" role="tab" [attr.aria-selected]="studioTab()==='overview'" [class.selected]="studioTab()==='overview'" (click)="studioTab.set('overview')">My Flows</button>
     <button type="button" role="tab" [attr.aria-selected]="studioTab()==='editor'" [class.selected]="studioTab()==='editor'" (click)="studioTab.set('editor')">Flow editor</button>
     @if(auth.user()?.role==='SYS_ADMIN'){
     <button type="button" role="tab" [attr.aria-selected]="studioTab()==='library'" [class.selected]="studioTab()==='library'" (click)="studioTab.set('library')">Function library &amp; triggers</button>
     }
     <button type="button" role="tab" [attr.aria-selected]="studioTab()==='files'" [class.selected]="studioTab()==='files'" (click)="studioTab.set('files')">My Workspace</button>
     <button type="button" role="tab" [attr.aria-selected]="studioTab()==='runs'" [class.selected]="studioTab()==='runs'" (click)="studioTab.set('runs')">Run history</button>
   </nav>
   @if(studioTab()==='overview'){
    <section class="flows-overview" aria-label="My Flows">
      <div class="overview-heading"><div><h2>My Flows</h2><p class="muted">Manage your saved automations without opening the editor.</p></div><div class="overview-actions"><button mat-stroked-button (click)="refresh()">Refresh</button><button mat-flat-button color="primary" (click)="startFlowFromOverview()">+ New flow</button></div></div>
      <div class="overview-filters">
       <label>Search<input type="search" [ngModel]="flowSearch()" (ngModelChange)="flowSearch.set($event)" placeholder="Flow name or event key"></label>
       <label>Status<select [ngModel]="flowFilter()" (ngModelChange)="flowFilter.set($event)"><option value="all">All</option><option value="enabled">Enabled</option><option value="disabled">Disabled</option><option value="scheduled">Scheduled</option><option value="manual">Not scheduled</option></select></label>
       <label>Sort<select [ngModel]="flowSort()" (ngModelChange)="flowSort.set($event)"><option value="name">Name A–Z</option><option value="name-desc">Name Z–A</option><option value="newest">Newest first</option><option value="oldest">Oldest first</option></select></label>
      </div>
      <p class="muted">{{listedFlows().length}} of {{flows().length}} flows · {{enabledFlowCount()}} enabled · {{scheduledFlowCount()}} scheduled</p>
      @if(!flows().length){<div class="overview-empty"><h3>No saved flows</h3><button mat-flat-button color="primary" (click)="startFlowFromOverview()">Create your first flow</button></div>}
      @else if(!listedFlows().length){<div class="overview-empty"><h3>No matching flows</h3><button mat-stroked-button (click)="clearFlowFilters()">Clear filters</button></div>}
      @else{<div class="flow-overview-grid">
       @for(flow of listedFlows();track flow.id){<article class="overview-flow-card">
        <div class="overview-flow-top"><div><h3>{{flow.name}}</h3><p class="muted">{{flow.graph.nodes.length}} nodes · {{flow.graph.edges.length}} links</p></div><span class="overview-flow-status" [class.active]="flow.enabled">{{flow.enabled?'Enabled':'Disabled'}}</span></div>
        <p class="overview-flow-trigger">@if(flow.interval_minutes>0){Every {{flow.interval_minutes}} min}@else if(flow.event_key){Event: {{flow.event_key}}}@else{Manual execution}</p>
        <div class="overview-actions"><button mat-flat-button color="primary" (click)="openFlowFromOverview(flow.id)">Edit</button><button mat-stroked-button (click)="openHistoryForFlow(flow.id)">History</button><button mat-stroked-button (click)="runSavedFlow(flow.id)" [disabled]="busy()">Run now</button></div>
       </article>}
      </div>}
      @if(message()){<p class="flow-message" role="status">{{message()}}</p>}
    </section>
    }
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
     <button mat-flat-button type="button" (click)="toggleNodePicker()" [attr.aria-expanded]="nodePickerOpen()" aria-controls="node-picker">+ Add node</button>
     <button mat-stroked-button (click)="linkSelected()" [disabled]="!selectedNode()||graph().nodes.length<3">Connect to…</button>
     <button mat-stroked-button (click)="removeSelected()" [disabled]="!selectedNode()||selectedNode()?.type==='trigger'">Remove node</button>
   </div>
   @if(nodePickerOpen()){
    <section id="node-picker" class="node-picker" aria-label="Add a node">
      <div class="node-picker-top"><strong>Add a node</strong><button type="button" (click)="nodePickerOpen.set(false)" aria-label="Close node picker">×</button></div>
      <label>Search nodes<input type="search" [ngModel]="nodePickerQuery()" (ngModelChange)="nodePickerQuery.set($event)" (keydown.escape)="nodePickerOpen.set(false)" placeholder="Search node type"></label>
      <div class="node-picker-items">
        @for(item of visibleNodeTypes();track item.id){
          <button type="button" (click)="addPickedNode(item.id)"><mat-icon aria-hidden="true">add_circle_outline</mat-icon>{{item.label}}</button>
        } @empty {<p class="muted">No matching nodes.</p>}
      </div>
    </section>
   }
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
      <button type="button" (click)="fitCanvas()" aria-label="Fit all nodes in view">Fit flow</button>
      <button type="button" (click)="resetCanvas()" aria-label="Reset canvas view">Reset view</button>
      @if(connectingFrom()){<span class="canvas-connect-hint" role="status">Select an input port <button type="button" (click)="cancelConnecting()">Cancel</button></span>}
    </div>
    <div class="flow-workspace" aria-label="Agent flow graph editor">
    <svg [attr.viewBox]="canvasViewBox()" (wheel)="onCanvasWheel($event)" (pointerdown)="onCanvasPointerDown($event)" (click)="closeQuickNodeOnCanvas($event)" (pointermove)="onCanvasPointerMove($event)" (pointerup)="onCanvasPointerEnd($event)" (pointercancel)="onCanvasPointerEnd($event)" (contextmenu)="onCanvasContextMenu($event)" role="img" aria-label="Agent nodes and directed connections">
     <defs><marker id="flow-arrow" markerWidth="9" markerHeight="9" refX="8" refY="4" orient="auto"><path d="M0,0 L8,4 L0,8 Z" fill="#72b7ef"/></marker></defs>
     @for(edge of graph().edges;track edge.from+edge.to){
       <path [attr.d]="edgePath(edge)" fill="none" stroke="#72b7ef" stroke-width="2.5" marker-end="url(#flow-arrow)"/>
     }
     @for(n of graph().nodes;track n.id){
       <g data-canvas-node (click)="$event.stopPropagation();openQuickNode(n.id)" (pointerdown)="dragStart($event,n.id)" style="cursor:move" tabindex="0"
          (keydown.enter)="openQuickNode(n.id)" [attr.aria-label]="'Select '+n.label">
         <rect [attr.x]="n.x" [attr.y]="n.y" width="125" height="70" rx="12"
           [attr.fill]="n.id===selectedNode()?.id?'#244970':'#1c2a3b'"
           [attr.stroke]="n.id===selectedNode()?.id?'#94caff':'#607993'" stroke-width="2"/>
         <text [attr.x]="n.x+12" [attr.y]="n.y+24" fill="#dceaf8" font-size="13">{{n.type==='trigger'?'⏱ TRIGGER':n.type==='flow'?'↪ SUBFLOW':n.type==='constant'?'◆ VALUE':n.type==='function'?'ƒ FUNCTION':n.type==='save_result'?'▣ SAVE':n.type==='load_result'?'▤ LOAD':n.type==='transform'?'⇄ TRANSFORM':n.type==='workspace_read'?'▤ READ FILE':n.type==='workspace_write'?'▣ WRITE FILE':n.type==='output'?'→ OUTPUT':n.type==='condition'?'◇ IF / ELSE':n.type==='merge'?'⇉ MERGE':n.type==='json_get'?'{} JSON FIELD':'✦ AGENT'}}</text>
         <text [attr.x]="n.x+12" [attr.y]="n.y+48" fill="#dceaf8" font-size="12">{{n.label.slice(0,16)}}</text>
          @if(n.type!=='trigger'){<circle class="canvas-port input-port" [attr.cx]="n.x" [attr.cy]="n.y+35" r="8" tabindex="0" role="button" [attr.aria-label]="'Connect into '+n.label" (pointerdown)="$event.stopPropagation()" (click)="$event.stopPropagation();chooseInputPort(n.id)" (keydown.enter)="$event.stopPropagation();chooseInputPort(n.id)"/>}
          @if(n.type!=='output'){<circle class="canvas-port output-port" [class.port-active]="connectingFrom()===n.id" [attr.cx]="n.x+125" [attr.cy]="n.y+35" r="8" tabindex="0" role="button" [attr.aria-label]="'Start connection from '+n.label" (pointerdown)="$event.stopPropagation()" (click)="$event.stopPropagation();chooseOutputPort(n.id)" (keydown.enter)="$event.stopPropagation();chooseOutputPort(n.id)"/>}
       </g>
     }
    </svg>
   </div>
   @if(quickNode();as n){<section class="node-quick-tooltip node-full-tooltip" role="region" aria-label="Complete node settings" (click)="$event.stopPropagation()"><header class="quick-tooltip-title"><strong>⚙ {{n.label}} · {{n.type}}</strong><button type="button" class="quick-tooltip-close" (click)="closeQuickNode()" aria-label="Close node settings">×</button></header><div class="inspector-fields">
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
       @if(n.type==='web_search'){
        <label>Search query<input [ngModel]="n.value||''" (ngModelChange)="updateUtility('value',$event)" maxlength="400" placeholder="Quarterly earnings guidance"></label>
        <p class="muted">Searches with the system administrator's Brave or Tavily configuration. Returns up to 5 sourced results as JSON. API keys never reach the browser or graph.</p>
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
     </div>@if(n.type!=='trigger'){<button type="button" class="node-quick-delete" (click)="removeNode(n.id)">Delete node</button>}</section>}
    @if(canvasMenu()){<div class="canvas-context-menu" role="menu" aria-label="Create node">
     <strong>Add node</strong>
     <button type="button" role="menuitem" (click)="addAgent();closeCanvasMenu()">Research Agent</button>
     <button type="button" role="menuitem" (click)="addOutput();closeCanvasMenu()">Output Result</button>
     @if(auth.user()?.role==='SYS_ADMIN'){<button type="button" role="menuitem" (click)="addSubflow();closeCanvasMenu()">Call Flow</button>}
     <button type="button" role="menuitem" (click)="addConstant();closeCanvasMenu()">Value</button>
     @if(auth.user()?.role==='SYS_ADMIN'){<button type="button" role="menuitem" (click)="addFunctionCall();closeCanvasMenu()">Function</button>}
     @if(auth.user()?.role==='SYS_ADMIN'){<button type="button" role="menuitem" (click)="addUtility('save_result');closeCanvasMenu()">Save Result</button>}
     @if(auth.user()?.role==='SYS_ADMIN'){<button type="button" role="menuitem" (click)="addUtility('load_result');closeCanvasMenu()">Load Result</button>}
     <button type="button" role="menuitem" (click)="addUtility('transform');closeCanvasMenu()">Transform Text</button>
     @if(auth.user()?.role==='SYS_ADMIN'){<button type="button" role="menuitem" (click)="addUtility('workspace_read');closeCanvasMenu()">Read Workspace File</button>}
     @if(auth.user()?.role==='SYS_ADMIN'){<button type="button" role="menuitem" (click)="addUtility('workspace_write');closeCanvasMenu()">Write Workspace File</button>}
     <button type="button" (click)="closeCanvasMenu()">Cancel</button>
   </div>}
   </div>
   
   </div>
   <section class="graph-validation" aria-label="Flow graph validation">
     <header><strong>Graph validation</strong><span role="status">{{graphIssues().length?graphIssues().length+' issues':'No structural issues'}}</span></header>
     @if(graphIssues().length){<p>Fix these issues before saving and running:</p><ul>
       @for(issue of graphIssues();track $index){<li>
         @if(issue.nodeId){<button type="button" (click)="openValidationIssue(issue.nodeId)">{{issue.message}} · Edit node</button>}
         @else{ {{issue.message}} }
       </li>}
     </ul>} @else {<p class="muted">The server will perform final validation when saving or running.</p>}
   </section>
   <div class="flow-actions">
     <button mat-stroked-button type="button" (click)="undoGraph()" [disabled]="!canUndo()||busy()" aria-label="Undo graph change" title="Undo (Ctrl/Cmd+Z)">↶ Undo</button>
     <button mat-stroked-button type="button" (click)="redoGraph()" [disabled]="!canRedo()||busy()" aria-label="Redo graph change" title="Redo (Ctrl/Cmd+Shift+Z or Ctrl+Y)">↷ Redo</button>
     <button mat-flat-button color="primary" (click)="save()" [disabled]="busy()">Save flow</button>
      <span class="muted" role="status">{{hasUnsavedChanges()?'Unsaved changes':'All changes saved'}}</span>
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
     <label>Flow <select [ngModel]="historyFlowId()" (ngModelChange)="selectHistoryFlow(+$event)" aria-label="Select flow for history"><option [ngValue]="0">Select a flow…</option>@for(f of flows();track f.id){<option [ngValue]="f.id">{{f.name}}</option>}</select></label>
     <label>Find run <input [ngModel]="historySearch()" (ngModelChange)="historySearch.set($event)" placeholder="Run ID or trigger"></label>
     <label>Status <select [ngModel]="historyStatusFilter()" (ngModelChange)="historyStatusFilter.set($event)"><option value="all">All statuses</option><option value="SUCCESS">Success</option><option value="FAILED">Failed</option><option value="RUNNING">Running</option><option value="CANCELLED">Cancelled</option></select></label>
     <label>Output view <select [ngModel]="logView()" (ngModelChange)="logView.set($event)">
       <option value="markdown">Markdown (default)</option><option value="raw">RAW</option><option value="json">Structured JSON</option>
     </select></label>
     <button mat-stroked-button (click)="refreshRuns()" [disabled]="!historyFlowId()||historyLoading()">Refresh history</button>
     <label>Compare with run<select [ngModel]="compareRun()" (ngModelChange)="compareAgainst(+$event)">
      <option [ngValue]="0">No comparison</option>
      @for(other of runs();track other.id){<option [ngValue]="other.id">#{{other.id}} · {{other.status}}</option>}
     </select></label>
   </div>
   @if(!historyFlowId()){<p class="muted">Select a flow above to view its history.</p>}
    @else if(!runs().length&&!historyLoading()){<p class="muted">No executions yet for this flow.</p>}
    @if(historyLoading()){<p role="status">Loading run history…</p>}
    @if(runs().some(run=>run.status==='RUNNING')){<p class="muted" role="status">Active run detected · history refreshes automatically every 5 seconds while this tab is open.</p>}
    @if(historyFlowId()&&runs().length&&!filteredRuns().length){<p class="muted">No runs match your filters.</p>}
   @for(run of filteredRuns();track run.id){
    <details class="flow-run">
      <summary><span class="run-summary-status" [class.run-success]="run.status==='SUCCESS'" [class.run-failed]="run.status==='FAILED'" [class.run-active]="run.status==='RUNNING'">{{run.status}}</span> #{{run.id}} · {{run.trigger}} <span class="muted">· {{displayRunTime(run.started_at)}} · {{displayRunDuration(run)}}</span></summary>
      <div class="run-diagnostics">
       <span><strong>Started:</strong> {{displayRunTime(run.started_at)}}</span>
       <span><strong>Finished:</strong> {{run.status==='RUNNING'?'In progress':displayRunTime(run.finished_at)}}</span>
       <span><strong>Duration:</strong> {{displayRunDuration(run)}}</span>
       <span><strong>Trigger:</strong> {{run.trigger}}</span>
      </div>
      @if(run.status==='FAILED'&&run.error){
       <div class="run-error-panel" role="alert">
        <strong>Execution failed</strong>
        <p>{{run.error}}</p>
        @if(suspectedFailedNode(run);as nodeId){
          <button mat-stroked-button (click)="inspectFailedNode(run.id,nodeId)">Inspect suspected node: {{nodeId}}</button>
        }
       </div>
      }
      @if(run.status==='RUNNING'){<button mat-stroked-button color="warn" (click)="cancelRun(run.id)" [disabled]="busy()">Cancel run</button>}
      <button mat-stroked-button (click)="showRunSteps(run.id)">{{traceRunId()===run.id?'Hide node trace':'Inspect nodes'}}</button>
      @if(traceRunId()===run.id){
       @if(executionGraph();as graph){
        <section class="exec-graph" aria-label="Historical execution graph">
          <strong>Execution graph · run #{{run.id}}</strong>
          <p class="muted">Saved graph from this run. Select a node to inspect its status and recorded output.</p>
          <div class="exec-graph-canvas">
            <svg [attr.viewBox]="executionViewBox()" role="group" [attr.aria-label]="'Execution graph for run '+run.id">
              @for(edge of graph.edges;track $index){<path [attr.d]="executionEdgePath(edge,graph)" stroke="#8eacc6" stroke-width="2" fill="none"/>}
              @for(n of graph.nodes;track n.id){
                <g tabindex="0" role="button" class="exec-node" [attr.aria-label]="n.label+', '+executionNodeStatus(n.id)"
                 (click)="executionSelectedNode.set(n.id)" (keydown.enter)="executionSelectedNode.set(n.id)" (keydown.space)="$event.preventDefault();executionSelectedNode.set(n.id)">
                  <rect [attr.x]="n.x" [attr.y]="n.y" width="125" height="70" rx="10"
                   [attr.fill]="executionNodeColor(n.id)" [attr.stroke]="executionSelectedNode()===n.id?'#ffe59c':'#8fa6bc'" [attr.stroke-width]="executionSelectedNode()===n.id?4:2"/>
                  <text [attr.x]="n.x+10" [attr.y]="n.y+24" font-size="12" fill="#fff">{{n.label.slice(0,17)}}</text>
                  <text [attr.x]="n.x+10" [attr.y]="n.y+46" font-size="11" fill="#fff">{{executionNodeStatus(n.id)}}</text>
                </g>
              }
            </svg>
          </div>
          @if(executionSelectedNode();as nodeId){
            <div class="exec-selected">
              <strong>{{executionNodeLabel(nodeId)}} · {{executionNodeStatus(nodeId)}}</strong>
              @if(executionStep(nodeId);as selectedStep){<p class="muted">Recorded {{displayRunTime(selectedStep.created_at)}} · Duration: {{selectedStep.duration_ms==null?'Unavailable':displayRunDuration(selectedStep.duration_ms)}}</p>}
              <p class="muted">Only persisted output previews are displayed. Raw inputs and retry counts are not available in historical records. Older runs may lack duration data.</p>
              @if(activeRunError();as failure){<div class="exec-error-hint"><strong>Run error:</strong> {{failure}}</div>}
              @if(executionStep(nodeId);as step){@if(step.output){<pre>{{step.output}}</pre>}@else{<p class="muted">No recorded output.</p>}}
              @else{<p class="muted">No recorded step for this node.</p>}
            </div>
          }
        </section>
       } @else if(!traceLoading()&&executionSnapshotUnavailable()){<p class="muted">Graph snapshot unavailable for this run. The trace remains available below.</p>}
       @if(traceLoading()){<p role="status">Loading node trace…</p>}
       @else if(!traceSteps().length){<p class="muted">No node trace available for this run. Earlier runs may predate node tracing.</p>}
       @else{
        <div class="run-step-list" aria-label="Node execution trace">
         @for(step of traceSteps();track step.id){
          <details class="run-step">
           <summary><strong [class.step-skipped]="step.status==='SKIPPED'">{{step.status}}</strong> · {{step.label||step.node}} <span class="muted">{{step.node}} · {{displayRunTime(step.created_at)}} · {{step.duration_ms==null?'Duration unavailable':displayRunDuration(step.duration_ms)}}</span>
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
 .flows-overview{display:grid;gap:18px;margin:16px 0 24px}.overview-heading,.overview-actions{display:flex;flex-wrap:wrap;align-items:center;gap:10px}.overview-heading{justify-content:space-between}.overview-heading h2,.overview-flow-top h3{margin:0 0 6px}.overview-heading p,.overview-flow-top p{margin:0}.overview-filters{display:flex;flex-wrap:wrap;gap:12px}.overview-filters label{flex:1 1 190px}.flow-overview-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(min(100%,290px),1fr));gap:14px}.overview-flow-card{display:flex;flex-direction:column;gap:14px;padding:16px;border:1px solid #456b87;border-radius:12px;background:#142438}.overview-flow-top{display:flex;justify-content:space-between;gap:12px}.overview-flow-top h3{font-size:17px;overflow-wrap:anywhere}.overview-flow-top p{font-size:12px}.overview-flow-status{padding:4px 8px;white-space:nowrap;border:1px solid #7e8290;border-radius:20px;font-size:11px}.overview-flow-status.active{border-color:#52bd93;color:#a6f3ce}.overview-flow-trigger{margin:0;color:#c7d9eb;font-size:13px;overflow-wrap:anywhere}.overview-flow-card>.overview-actions{margin-top:auto}.overview-flow-card>.overview-actions button{flex:1 1 auto;min-height:38px}.overview-empty{padding:32px 16px;border:1px dashed #55718a;border-radius:12px;text-align:center}
.node-picker{display:grid;gap:12px;margin:14px 0;padding:16px;border:1px solid #6086a1;border-radius:10px;background:#15293e}
  .node-picker-top{display:flex;align-items:center;justify-content:space-between}.node-picker-top button{background:#27445d;border:1px solid #7394ad;border-radius:6px;color:white;min-width:34px;min-height:34px;cursor:pointer}
  .node-picker label{width:100%;min-width:0}.node-picker-items{display:grid;grid-template-columns:repeat(auto-fill,minmax(min(100%,180px),1fr));gap:8px;max-height:310px;overflow:auto}
  .node-picker-items button{display:flex;align-items:center;gap:8px;text-align:left;background:#213c54;color:#edf7ff;border:1px solid #547d9b;border-radius:7px;padding:12px;cursor:pointer;font:inherit}
  .node-picker-items button:hover{background:#305773}.node-picker-items button:focus-visible{outline:2px solid #94caff}
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
 .canvas-port{stroke:#c6edff;stroke-width:2;cursor:crosshair}.input-port{fill:#2c789a}.output-port{fill:#329f78}.port-active{stroke:#ffe19a;stroke-width:4}.canvas-port:focus-visible{stroke:#ffe19a;stroke-width:4;outline:none}
  .canvas-connect-hint{display:flex;align-items:center;gap:8px;font-size:12px;color:#ffe19a;margin-left:auto}.canvas-connect-hint button{background:#3b3540;color:#ffe19a;border:1px solid #ae8c67;border-radius:5px;cursor:pointer;padding:5px}
  .flow-workspace{background:radial-gradient(#294057 1px,transparent 1px) 0 0/20px 20px,#0d1b2a;border:1px solid #45627b;overflow:auto;border-radius:12px}
 svg{min-width:660px;width:100%;height:auto;display:block;touch-action:none;cursor:grab}
 .node-quick-tooltip{position:absolute;right:14px;top:62px;z-index:7;display:flex;flex-direction:column;gap:8px;padding:12px;width:min(480px,calc(100% - 28px));box-sizing:border-box;max-height:min(78vh,780px);overflow:auto;background:rgba(26,51,74,.89);backdrop-filter:blur(10px);border:1px solid #8abce4;box-shadow:0 12px 28px #000a;border-radius:10px}
 .quick-tooltip-title{display:flex;align-items:center;justify-content:space-between;gap:12px}
 .quick-tooltip-close{width:26px;height:26px;min-height:26px!important;padding:0!important;flex:0 0 auto;color:#fff!important;background:#b52b38!important;border:1px solid #e15d66!important;border-radius:5px!important;font-size:20px!important;line-height:1!important;cursor:pointer}
 .quick-tooltip-close:hover{background:#db3545!important}
 .node-full-tooltip .inspector-fields{display:flex;flex-direction:column;gap:12px}.node-quick-tooltip label{display:flex;flex-direction:column;gap:4px;min-width:0}
 .node-quick-tooltip input,.node-quick-tooltip textarea,.node-quick-tooltip select{width:100%;box-sizing:border-box}
 .node-quick-tooltip button{min-height:32px}
  .node-quick-delete{background:#562632;color:#ffe5e5;border:1px solid #b65f6b;border-radius:6px;padding:8px;cursor:pointer}
 .blueprint-layout{display:grid;grid-template-columns:minmax(0,1fr);align-items:stretch;gap:12px;min-height:470px}
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
 @media(max-width:850px){.blueprint-layout{grid-template-columns:minmax(0,1fr)}.blueprint-layout .flow-workspace{min-height:280px}.node-full-tooltip{width:calc(100% - 16px);right:8px}}

.graph-validation{border:1px solid #527590;border-radius:10px;padding:14px;margin:12px 0;background:#152b40}
 .graph-validation header{display:flex;justify-content:space-between;flex-wrap:wrap;gap:10px}
 .graph-validation ul{padding-left:22px}.graph-validation li{padding:4px 0;overflow-wrap:anywhere}
 .graph-validation li button{background:none;border:0;text-decoration:underline;color:#aedfff;cursor:pointer;font:inherit;text-align:left;padding:2px 0}
 .flow-message{border-left:3px solid #73baff;padding-left:12px}
 .exec-graph{margin:14px 0;display:grid;gap:10px;padding:14px;border:1px solid #557c99;border-radius:12px;background:#12293d}
 .exec-graph-canvas{overflow:auto;border:1px solid #3c5d77;border-radius:8px;background:#0b1c2e}
 .exec-graph-canvas svg{min-width:0;width:100%;min-height:210px;max-height:520px;height:auto;cursor:default}
 .exec-node{cursor:pointer}.exec-node:focus-visible rect{stroke:#ffe59c;stroke-width:4}
 .exec-selected{padding:12px;border-radius:8px;border:1px solid #52758f;background:#1a354b}
 .exec-selected pre{max-height:300px;overflow:auto;white-space:pre-wrap;overflow-wrap:anywhere}
 .run-history-heading{display:flex;align-items:center;flex-wrap:wrap;gap:16px;margin-bottom:14px}
 .run-history-heading h3{margin:0}
 .run-history-heading label{flex:0 1 220px}
 .run-diagnostics{display:flex;flex-wrap:wrap;gap:10px 18px;padding:12px;margin:12px 0;border:1px solid #48647e;border-radius:8px;font-size:12px}
 .run-summary-status{font-weight:700;color:#bbc7d5}.run-success{color:#79e0a7}.run-failed{color:#ff9da7}.run-active{color:#f4d686}
 .run-error-panel{border-left:4px solid #d26c79;background:#442c39;padding:12px;margin:12px 0;border-radius:6px}
 .run-error-panel p{overflow-wrap:anywhere;white-space:pre-wrap}
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
 @HostListener('document:keydown',['$event'])
 onEditorKeydown(event:KeyboardEvent):void{
  if(event.key==='Escape'){
   if(this.studioTab()==='editor'){this.closeQuickNode();this.closeCanvasMenu();this.cancelConnecting();}
   return;
  }
  if(this.studioTab()!=='editor'||this.busy()||event.altKey)return;
  const target=event.target;
  if(target instanceof Element&&target.closest('input,textarea,select,[contenteditable="true"],[role="textbox"]'))return;
  if(!(event.ctrlKey||event.metaKey))return;
  const key=event.key.toLowerCase();
  if(key==='z'&&!event.shiftKey&&this.canUndo()){event.preventDefault();this.undoGraph();}
  else if(((key==='z'&&event.shiftKey)||key==='y')&&this.canRedo()){event.preventDefault();this.redoGraph();}
 }
 @HostListener('document:click',['$event'])
 onDocumentClick(event:MouseEvent):void {
  const el=event.target;
  if(el instanceof Element && !el.closest('[data-canvas-node], .node-quick-tooltip'))this.closeQuickNode();
 }
 closeQuickNodeOnCanvas(event:MouseEvent):void {if(!(event.target as Element).closest('[data-canvas-node]'))this.closeQuickNode();}

 readonly canvasView = signal({ x: 0, y: 0, width: 820, height: 380 });
 readonly canvasMenu = signal(false);
  readonly connectingFrom=signal('');
  cancelConnecting():void{this.connectingFrom.set('')}
  chooseOutputPort(id:string):void{if(this.node(id)?.type==='output')return;this.chosen.set(id);this.connectingFrom.set(id);this.closeQuickNode()}
  chooseInputPort(id:string):void{
    const from=this.connectingFrom();
    if(!from){this.message.set('Select an output port first.');return}
    if(from===id||this.node(id)?.type==='trigger')return;
    if(this.graph().edges.some(e=>e.from===from&&e.to===id)){this.cancelConnecting();this.message.set('These nodes are already connected.');return}
    this.connect(from,id);this.chosen.set(id);this.connectingFrom.set('');
  }
  edgePath(e:FlowEdge):string{
    const a=this.node(e.from),b=this.node(e.to);if(!a||!b)return '';
    const x1=a.x+125,y1=a.y+35,x2=b.x,y2=b.y+35,d=Math.max(45,Math.abs(x2-x1)*0.45);
    return 'M '+x1+' '+y1+' C '+(x1+d)+' '+y1+', '+(x2-d)+' '+y2+', '+x2+' '+y2;
  }
  fitCanvas():void{
    const nodes=this.graph().nodes;if(!nodes.length){this.resetCanvas();return}
    const padding=65,left=Math.min(...nodes.map(n=>n.x)),top=Math.min(...nodes.map(n=>n.y));
    const right=Math.max(...nodes.map(n=>n.x+125)),bottom=Math.max(...nodes.map(n=>n.y+70));
    const contentWidth=right-left+2*padding,contentHeight=bottom-top+2*padding;
    const width=Math.max(contentWidth,contentHeight*820/380,410),height=width*380/820;
    this.canvasView.set({x:left-padding-(width-contentWidth)/2,y:top-padding-(height-contentHeight)/2,width,height});
  }
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
 readonly nodePickerOpen=signal(false);
  readonly nodePickerQuery=signal('');
  readonly nodeTypes=[{"id":0,"label":"Agent","admin":false},{"id":1,"label":"If / Else","admin":false},{"id":2,"label":"Merge","admin":false},{"id":3,"label":"JSON Field","admin":false},{"id":4,"label":"Loop","admin":false},{"id":5,"label":"HTTP JSON/XML","admin":false},{"id":6,"label":"Web crawler","admin":false},{"id":7,"label":"Web search","admin":false},{"id":8,"label":"Output","admin":false},{"id":9,"label":"Call Flow","admin":true},{"id":10,"label":"Value","admin":false},{"id":11,"label":"Function","admin":true},{"id":12,"label":"Save Result","admin":true},{"id":13,"label":"Load Result","admin":true},{"id":14,"label":"Transform","admin":false},{"id":15,"label":"Read File","admin":true},{"id":16,"label":"Write File","admin":true}];
  readonly visibleNodeTypes=()=>this.nodeTypes.filter(n=>(!n.admin||this.auth.user()?.role==='SYS_ADMIN')&&n.label.toLowerCase().includes(this.nodePickerQuery().trim().toLowerCase()));
  toggleNodePicker(){this.nodePickerOpen.update(v=>!v);this.nodePickerQuery.set('')}
  addPickedNode(id:number){
    if(!this.visibleNodeTypes().some(n=>n.id===id))return;
    switch(id){case 0:this.addAgent();break;case 1:this.addV2Node('condition');break;case 2:this.addV2Node('merge');break;case 3:this.addV2Node('json_get');break;case 4:this.addPureLoop();break;case 5:this.addHTTPGet();break;case 6:this.addWebCrawler();break;case 7:this.addWebSearch();break;case 8:this.addOutput();break;case 9:this.addSubflow();break;case 10:this.addConstant();break;case 11:this.addFunctionCall();break;case 12:this.addUtility('save_result');break;case 13:this.addUtility('load_result');break;case 14:this.addUtility('transform');break;case 15:this.addUtility('workspace_read');break;case 16:this.addUtility('workspace_write');break;default:return;}
    this.nodePickerOpen.set(false);
  }
  readonly studioTab=signal<'overview'|'editor'|'library'|'runs'|'files'>('overview');
  readonly flowSearch=signal('');readonly flowFilter=signal('all');readonly flowSort=signal('name');
  readonly listedFlows=()=>{
    const q=this.flowSearch().trim().toLowerCase(), filter=this.flowFilter(),sort=this.flowSort();
    return this.flows().filter(f=>(!q||f.name.toLowerCase().includes(q)||(f.event_key||'').toLowerCase().includes(q))&&(filter==='all'||filter==='enabled'&&f.enabled||filter==='disabled'&&!f.enabled||filter==='scheduled'&&f.interval_minutes>0||filter==='manual'&&f.interval_minutes===0)).sort((a,b)=>sort==='newest'?b.id-a.id:sort==='oldest'?a.id-b.id:sort==='name-desc'?b.name.localeCompare(a.name):a.name.localeCompare(b.name));
  };
  enabledFlowCount(){return this.flows().filter(f=>f.enabled).length}
  scheduledFlowCount(){return this.flows().filter(f=>f.interval_minutes>0).length}
  clearFlowFilters(){this.flowSearch.set('');this.flowFilter.set('all');this.flowSort.set('name')}
  startFlowFromOverview(){if(!this.allowDiscard())return;this.resetFlow();this.studioTab.set('editor')}
  openFlowFromOverview(id:number){this.loadFlow(id);if(this.selectedId()===id)this.studioTab.set('editor')}
  openHistoryForFlow(id:number){this.selectHistoryFlow(id);this.studioTab.set('runs')}
  async runSavedFlow(id:number){
    if(this.busy()||!this.flows().some(f=>f.id===id))return;
    this.busy.set(true);
    try{await this.request('/api/admin/flows/'+id+'/run',{method:'POST'});this.openHistoryForFlow(id);this.message.set('Run queued; check History for results.');setTimeout(()=>{if(this.historyFlowId()===id)void this.refreshRuns()},3000)}
    catch(e){this.message.set('Could not start run: '+String(e))}finally{this.busy.set(false)}
  }
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
 readonly undoHistory=signal<string[]>([]);
 readonly redoHistory=signal<string[]>([]);
 readonly canUndo=()=>this.undoHistory().length>0;
 readonly canRedo=()=>this.redoHistory().length>0;
 private lastGraphVersion='';
 private dragHistoryStart:string|null=null;
 private resetGraphHistory():void{
   this.lastGraphVersion=JSON.stringify(this.graph());
   this.undoHistory.set([]);this.redoHistory.set([]);
 }
 undoGraph():void{
   const history=this.undoHistory();if(!history.length||this.busy())return;
   const previous=history[history.length-1],current=JSON.stringify(this.graph());
   this.undoHistory.set(history.slice(0,-1));
   this.redoHistory.update(items=>[...items.slice(-39),current]);
   this.lastGraphVersion=previous;
   this.graph.set(JSON.parse(previous) as FlowGraph);
   this.chosen.set('');this.closeQuickNode();this.cancelConnecting();
 }
 redoGraph():void{
   const history=this.redoHistory();if(!history.length||this.busy())return;
   const next=history[history.length-1],current=JSON.stringify(this.graph());
   this.redoHistory.set(history.slice(0,-1));
   this.undoHistory.update(items=>[...items.slice(-39),current]);
   this.lastGraphVersion=next;
   this.graph.set(JSON.parse(next) as FlowGraph);
   this.chosen.set('');this.closeQuickNode();this.cancelConnecting();
 }
 readonly chosen=signal('');
 readonly selectedNode=()=>this.graph().nodes.find(n=>n.id===this.chosen());
 readonly runs=signal<FlowRun[]>([]);
  readonly historyFlowId=signal(0);
  readonly savedSnapshot=signal('');
  readonly hasUnsavedChanges=()=>this.savedSnapshot()!==this.currentSnapshot();
  private currentSnapshot():string{return JSON.stringify({name:this.name(),graph:this.graph(),enabled:this.enabled(),interval:this.interval(),eventKey:this.eventKey()})}
  private allowDiscard():boolean{return !this.hasUnsavedChanges()||confirm('Discard unsaved changes to this flow?')}
  readonly graphIssues=():{nodeId?:string;message:string}[]=>{
    const g=this.graph(),issues:{nodeId?:string;message:string}[]=[];
    const add=(message:string,nodeId?:string)=>issues.push({message,nodeId});
    const nodes=new Map(g.nodes.map(n=>[n.id,n]));
    if(this.name().trim().length<2||this.name().length>120)add('Flow name must be 2–120 characters.');
    if(this.interval()!==0&&(this.interval()<5||this.interval()>10080))add('Schedule interval must be off or 5–10080 minutes.');
    if(g.nodes.length<2||g.nodes.length>16)add('A flow needs 2–16 nodes.');
    if(g.edges.length<1||g.edges.length>32)add('A flow needs 1–32 connections.');
    const triggers=g.nodes.filter(n=>n.type==='trigger');
    if(triggers.length!==1)add('Exactly one trigger node is required.');
    const ids=new Set<string>();
    for(const n of g.nodes){
      if(!n.id||n.id.length>48||ids.has(n.id))add('Duplicate or invalid node ID: '+n.id,n.id);
      ids.add(n.id);
      if(n.type==='agent'&&(n.prompt||'').trim().length<5)add('Agent needs research instructions: '+n.label,n.id);
      if(n.type==='flow'&&!n.flow_id)add('Select a saved flow: '+n.label,n.id);
      if(n.type==='function'&&!n.function_id)add('Select a saved function: '+n.label,n.id);
      if(n.type==='json_get'&&!/^[A-Za-z][A-Za-z0-9_]{0,47}$/.test(n.property||''))add('Enter a valid JSON property: '+n.label,n.id);
      if(n.type==='condition'&&!['equals','not_equals','contains','greater_than','less_than'].includes(n.operator||''))add('Select a condition operator: '+n.label,n.id);
      if(n.type==='loop'&&(!n.loop_limit||n.loop_limit<1||n.loop_limit>100))add('Set loop limit from 1 to 100: '+n.label,n.id);
      if((n.type==='save_result'||n.type==='load_result')&&!/^[A-Za-z][A-Za-z0-9_]{0,47}$/.test(n.variable||''))add('Run variable must start with a letter and be at most 48 characters: '+n.label,n.id);
      if(n.type==='agent'&&(n.timeout_seconds||0)>90)add('Agent timeout exceeds 90 seconds: '+n.label,n.id);
      if(n.type==='agent'&&(n.max_retries||0)>2)add('Agent retries exceed 2: '+n.label,n.id);
      if(n.type==='constant'&&n.value_type==='boolean'&&n.value!=='true'&&n.value!=='false')add('Boolean value must be true or false: '+n.label,n.id);
      if(n.type==='constant'&&n.value_type==='number'&&(!n.value?.trim()||!Number.isFinite(Number(n.value))))add('Number constant must be finite: '+n.label,n.id);
      if(n.type==='workspace_read'||n.type==='workspace_write'){
        const path=n.value||'';
        if(!path.startsWith('/')||path==='/'||path.includes('\\')||path.split('/').some(p=>p==='.'||p==='..')||path.length>512)add('Workspace path must be an absolute safe file path: '+n.label,n.id);
      }
      if(n.type==='agent'&&n.output_mode==='json'&&n.json_contract){try{
        const contract=JSON.parse(n.json_contract);if(!contract||typeof contract!=='object'||Array.isArray(contract))throw Error('Invalid schema');
      }catch{add('JSON output contract is invalid: '+n.label,n.id)}}
    }
    if(g.version!==2&&g.edges.some(e=>!!e.from_port||!!e.to_port))add('Typed ports and branch selectors require graph version 2.');
    const adjacency=new Map(g.nodes.map(n=>[n.id,[] as string[]]));
    const incoming=new Map(g.nodes.map(n=>[n.id,0]));
    for(const e of g.edges){
      if(!nodes.has(e.from)||!nodes.has(e.to)||e.from===e.to){add('Connection references a missing node or itself.');continue}
      if(nodes.get(e.to)?.type==='trigger')add('Trigger cannot have incoming connections.',e.to);
      const from=nodes.get(e.from)!,to=nodes.get(e.to)!;
      if(e.from_port||e.to_port){
        if(e.from_port?.startsWith('field:')&&e.to_port==='input'){
          const field=e.from_port.slice(6);
          if(!/^[A-Za-z][A-Za-z0-9_]{0,47}$/.test(field))add('Mapped JSON field name is invalid: '+field,e.from);
          if(['trigger','condition','output'].includes(from.type))add('This node cannot expose a mapped JSON field: '+from.label,e.from);
          if(['trigger','constant'].includes(to.type))add('This node cannot accept a mapped value: '+to.label,e.to);
          if(from.type==='agent'){
            if(from.output_mode!=='json')add('Agent must use JSON-only output to map a field: '+from.label,e.from);
            if(from.json_contract){try{
              const contract=JSON.parse(from.json_contract);
              if(contract?.properties&&typeof contract.properties==='object'&&!Object.prototype.hasOwnProperty.call(contract.properties,field))
                add('Mapped field is absent from JSON contract: '+field,e.from);
            }catch{/* invalid contract is reported by the node validator */}
            }
          }
        }else if(from.type==='condition'&&(e.from_port==='true'||e.from_port==='false')&&!e.to_port){
          // Valid v2 branch selector.
        }else if(e.from_port==='value'&&e.to_port==='input'){
          if(from.type==='trigger')add('Trigger has no typed value output.',e.from);
          if(to.type==='trigger'||to.type==='constant')add('Target cannot accept typed input: '+to.label,e.to);
          if(from.type==='constant'&&from.value_type!=='text')add('Convert number/boolean constants to text before connecting: '+from.label,e.from);
        }else add('Unsupported connection port combination: '+(e.from_port||'default')+' → '+(e.to_port||'default'),e.from);
      }
      adjacency.get(e.from)?.push(e.to);incoming.set(e.to,(incoming.get(e.to)||0)+1);
    }
    for(const n of g.nodes)if(n.type!=='trigger'&&!incoming.get(n.id))add('Disconnected node: '+n.label,n.id);
    const visited=new Set<string>(),active=new Set<string>();let cycle=false;
    const dfs=(id:string):void=>{
      if(active.has(id)){cycle=true;return}
      if(visited.has(id))return;
      visited.add(id);active.add(id);for(const to of adjacency.get(id)||[])dfs(to);active.delete(id);
    };
    if(triggers.length===1)dfs(triggers[0].id);
    if(cycle)add('Graph contains a cycle.');
    for(const n of g.nodes)if(!visited.has(n.id))add('Unreachable node: '+n.label,n.id);
    return issues;
  };
  openValidationIssue(id:string):void{if(this.node(id))this.openQuickNode(id)}
  readonly historyLoading=signal(false);
  private historyRequestVersion=0;
  readonly historyStatusFilter=signal('all');
  readonly historySearch=signal('');
  readonly filteredRuns=()=>this.runs().filter(run=>(this.historyStatusFilter()==='all'||run.status===this.historyStatusFilter())&&(!this.historySearch()||String(run.id).includes(this.historySearch().trim())||run.trigger.toLowerCase().includes(this.historySearch().trim().toLowerCase())));
 readonly traceRunId=signal(0);
 readonly traceSteps=signal<FlowRunStep[]>([]);
 readonly executionGraph=signal<FlowGraph|null>(null);
 readonly executionSnapshotUnavailable=signal(false);
 readonly executionSelectedNode=signal('');
 activeRunError():string{
   return this.runs().find(r=>r.id===this.traceRunId()&&r.status==='FAILED')?.error||'';
 }
 displayRunTime(value?:string|null):string{
   if(!value)return 'Unavailable';
   const date=new Date(value);
   return Number.isNaN(date.getTime())?'Unavailable':date.toLocaleString();
 }
 displayRunDuration(run:FlowRun):string{
   if(run.duration_ms==null)return run.status==='RUNNING'?'In progress':'Unavailable';
   const ms=Math.max(0,run.duration_ms);
   return ms<1000?ms+' ms':ms<60000?(ms/1000).toFixed(1)+' s':Math.floor(ms/60000)+' min '+Math.round(ms%60000/1000)+' s';
 }
 suspectedFailedNode(run:FlowRun):string{
   if(run.status!=='FAILED'||!run.error||!this.executionGraph())return '';
   // Failure messages can name a node without an explicit persisted FAILED step.
   // We show this as a hint, never as a confirmed node status.
   return this.executionGraph()!.nodes.find(n=>run.error.includes('node '+n.id+':')||run.error.includes('node '+n.id+' '))?.id||'';
 }
 async inspectFailedNode(runId:number,nodeId:string):Promise<void>{
   if(this.traceRunId()!==runId)await this.showRunSteps(runId);
   if(this.traceRunId()===runId&&this.executionGraph()?.nodes.some(n=>n.id===nodeId))
     this.executionSelectedNode.set(nodeId);
 }
 executionStep(id:string):FlowRunStep|undefined{const steps=this.traceSteps().filter(s=>s.node===id);return steps[steps.length-1]}
 executionNodeStatus(id:string):string{return this.executionStep(id)?.status||'NOT RECORDED'}
 executionNodeColor(id:string):string{switch(this.executionNodeStatus(id)){case 'SUCCESS':return '#176349';case 'FAILED':return '#953643';case 'RUNNING':return '#8e6927';case 'SKIPPED':return '#596374';default:return '#24394f'}}
 executionNodeLabel(id:string):string{return this.executionGraph()?.nodes.find(n=>n.id===id)?.label||id}
 executionViewBox():string{const n=this.executionGraph()?.nodes||[];if(!n.length)return '0 0 820 380';const l=Math.min(...n.map(x=>x.x))-60,t=Math.min(...n.map(x=>x.y))-60,r=Math.max(...n.map(x=>x.x+125))+60,b=Math.max(...n.map(x=>x.y+70))+60;return l+' '+t+' '+(r-l)+' '+(b-t)}
 executionEdgePath(e:FlowEdge,g:FlowGraph):string{const a=g.nodes.find(n=>n.id===e.from),b=g.nodes.find(n=>n.id===e.to);if(!a||!b)return '';const x=a.x+125,y=a.y+35,tx=b.x,ty=b.y+35,d=Math.max(45,Math.abs(tx-x)*.45);return 'M '+x+' '+y+' C '+(x+d)+' '+y+', '+(tx-d)+' '+ty+', '+tx+' '+ty}
 readonly traceLoading=signal(false);
 readonly replaySteps=signal<{node:string;label:string;status:string;input?:string;output?:string}[]>([]);
 readonly replaySource=signal(0);
 readonly compareRun=signal(0);
 readonly comparedSteps=signal<{node:string;status:string;output:string}[]>([]);
 readonly busy=signal(false);
 readonly message=signal('');
 private dragging:{id:string;startX:number;startY:number;x:number;y:number}|null=null;
 constructor(){this.resetGraphHistory();this.savedSnapshot.set(this.currentSnapshot());void this.refresh();if(this.auth.user()?.role==='SYS_ADMIN')void this.loadFunctions();void this.loadModels();
    effect(()=>{
      const current=JSON.stringify(this.graph());
      if(current===this.lastGraphVersion||this.dragHistoryStart!==null)return;
      if(this.lastGraphVersion)this.undoHistory.update(items=>[...items.slice(-39),this.lastGraphVersion]);
      this.lastGraphVersion=current;this.redoHistory.set([]);
    });
    effect((onCleanup)=>{
      if(this.studioTab()!=='runs'||!this.historyFlowId()||!this.runs().some(run=>run.status==='RUNNING'))return;
      const timer=setInterval(()=>void this.refreshRuns(),5000);
      onCleanup(()=>clearInterval(timer));
    });
  }
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
  if(!id||!this.allowDiscard())return;
  this.resetFlow();
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
  this.resetGraphHistory();
  this.message.set('Template copied as a new, disabled flow. Review nodes and save it to keep your own version.');
 }
 newFlow(){if(!this.allowDiscard())return;this.resetFlow()}
 private resetFlow(){this.selectedId.set(0);this.name.set('New research flow');this.interval.set(0);this.eventKey.set('');this.enabled.set(false);this.graph.set(this.defaultGraph());this.resetGraphHistory();this.message.set('');this.savedSnapshot.set(this.currentSnapshot())}
 loadFlow(id:number){if(!this.allowDiscard())return;this.cancelConnecting();this.resetCanvas();const f=this.flows().find(x=>x.id===id);if(!f){this.resetFlow();return}
  this.selectedId.set(f.id);this.name.set(f.name);this.interval.set(f.interval_minutes);this.eventKey.set(f.event_key||'');this.enabled.set(f.enabled);this.graph.set(structuredClone(f.graph));this.resetGraphHistory();this.fitCanvas();this.chosen.set('');this.savedSnapshot.set(this.currentSnapshot())}
 async loadFunctions(){try{const data=await this.request("/api/admin/blueprint-functions");this.functions.set(data.functions||[])}catch(e){this.message.set(String(e))}}
 addWebSearch():void {
  const count=this.graph().nodes.length;
  const id='web-search-'+Date.now().toString(36);
  this.graph.update(g=>({...g,version:2,nodes:[...g.nodes,{id,type:'web_search',label:'Web search',value:'Quarterly earnings analysis',x:260+(count%4)*140,y:40+Math.floor(count/4)*100}]}));
  this.chosen.set(id);
 }
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
 removeSelected(){this.removeNode(this.chosen())}
  removeNode(id:string){const n=this.node(id);if(!n||n.type==='trigger')return;
    this.graph.update(g=>({...g,nodes:g.nodes.filter(n=>n.id!==id),edges:g.edges.filter(e=>e.from!==id&&e.to!==id)}));
    if(this.chosen()===id)this.chosen.set('');
    this.closeQuickNode();this.message.set('Node removed; save the flow to persist.');
  }
 dragStart(ev:PointerEvent,id:string){if(ev.button!==0||ev.pointerType==='touch')return;const n=this.node(id);if(!n)return;this.chosen.set(id);this.dragHistoryStart=JSON.stringify(this.graph());this.dragging={id,startX:ev.clientX,startY:ev.clientY,x:n.x,y:n.y};
  const svg=(ev.currentTarget as SVGGElement).ownerSVGElement;if(!svg)return;
  const move=(event:PointerEvent)=>{const box=svg.getBoundingClientRect();if(!this.dragging)return;
   const view=this.canvasView();const dx=(event.clientX-this.dragging.startX)*view.width/box.width,dy=(event.clientY-this.dragging.startY)*view.height/box.height;
   this.graph.update(g=>({...g,nodes:g.nodes.map(node=>node.id===id?{...node,x:Math.round(this.dragging!.x+dx),y:Math.round(this.dragging!.y+dy)}:node)}))}
  const stop=()=>{
   svg.removeEventListener('pointermove',move);svg.removeEventListener('pointerup',stop);svg.removeEventListener('pointercancel',stop);
   const before=this.dragHistoryStart,after=JSON.stringify(this.graph());
   if(before!==null&&before!==after){this.undoHistory.update(items=>[...items.slice(-39),before]);this.redoHistory.set([]);}
   this.lastGraphVersion=after;this.dragHistoryStart=null;this.dragging=null;
  }
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
  if(!this.historyFlowId()||this.busy())return;
  this.busy.set(true);
  try{
   await this.request('/api/admin/flows/'+this.historyFlowId()+'/runs/'+runId+'/cancel',{method:'POST'});
   this.message.set('Run cancellation requested');
   await this.refreshRuns();
  }catch(e){this.message.set('Cannot cancel run: '+String(e))}
  finally{this.busy.set(false)}
 }
 async compareAgainst(runId:number){
  if(!this.historyFlowId()||!runId){this.compareRun.set(0);this.comparedSteps.set([]);return}
  this.compareRun.set(runId);this.comparedSteps.set([]);
  try{
   const data=await this.request('/api/admin/flows/'+this.historyFlowId()+'/runs/'+runId+'/steps');
   this.comparedSteps.set(data.steps||[]);
  }catch(e){this.message.set('Cannot compare runs: '+String(e))}
 }
 compareStatus(node:string,status:string,output:string):string{
   const other=this.comparedSteps().find(x=>x.node===node);
   if(!this.compareRun()||!other)return 'No baseline';
   return other.status===status&&other.output===output?'Unchanged':other.status!==status?'Status changed':'Output changed';
 }
 async previewReplay(runId:number,nodeId:string){
  if(!this.historyFlowId())return;
  this.replaySteps.set([]);this.replaySource.set(runId);
  try{
   const result=await this.request('/api/admin/flows/'+this.historyFlowId()+'/runs/'+runId+'/preview',{method:'POST',body:JSON.stringify({node:nodeId})});
   this.replaySteps.set(result.steps||[]);
   this.message.set('Preview only: no agents or side effects were executed. Uses the saved graph snapshot.');
  }catch(e){this.message.set('Replay preview unavailable: '+String(e))}
 }
 async showRunSteps(runId:number){
  const flowId=this.historyFlowId();if(!flowId)return;
  if(this.traceRunId()===runId){this.traceRunId.set(0);this.traceSteps.set([]);this.traceLoading.set(false);this.executionGraph.set(null);this.executionSelectedNode.set('');return}
  this.traceRunId.set(runId);this.traceSteps.set([]);this.traceLoading.set(true);this.executionGraph.set(null);this.executionSelectedNode.set('');this.executionSnapshotUnavailable.set(false);
  const base='/api/admin/flows/'+flowId+'/runs/'+runId;
  const [trace,snapshot]=await Promise.allSettled([this.request(base+'/steps'),this.request(base+'/snapshot')]);
  if(this.historyFlowId()!==flowId||this.traceRunId()!==runId)return;
  if(trace.status==='fulfilled')this.traceSteps.set(trace.value.steps||[]);
  else this.message.set('Could not load node trace: '+String(trace.reason));
  if(snapshot.status==='fulfilled'&&Array.isArray(snapshot.value.graph?.nodes)&&Array.isArray(snapshot.value.graph?.edges))this.executionGraph.set(snapshot.value.graph);
  else this.executionSnapshotUnavailable.set(true);
  this.traceLoading.set(false);
 }
 selectHistoryFlow(id:number):void{this.executionGraph.set(null);this.executionSnapshotUnavailable.set(false);this.executionSelectedNode.set('');this.historyFlowId.set(id);this.historyRequestVersion++;this.historyLoading.set(false);this.runs.set([]);this.traceRunId.set(0);this.traceSteps.set([]);this.traceLoading.set(false);this.compareRun.set(0);this.comparedSteps.set([]);this.replaySteps.set([]);this.replaySource.set(0);if(id)void this.refreshRuns()}
  async refreshRuns(){const id=this.historyFlowId();if(!id)return;const version=++this.historyRequestVersion;this.historyLoading.set(true);
   try{
      const d=await this.request('/api/admin/flows/'+id+'/runs');
      if(this.historyFlowId()===id&&this.historyRequestVersion===version){
        this.runs.set(d.runs||[]);
        const runId=this.traceRunId();
        if(runId&&(d.runs||[]).some((run:FlowRun)=>run.id===runId)){
          const trace=await this.request('/api/admin/flows/'+id+'/runs/'+runId+'/steps');
          if(this.historyFlowId()===id&&this.traceRunId()===runId&&this.historyRequestVersion===version)this.traceSteps.set(trace.steps||[]);
        }
      }
    }
   catch(e){if(this.historyFlowId()===id&&this.historyRequestVersion===version)this.message.set('Could not refresh run history: '+String(e))}
   finally{if(this.historyRequestVersion===version)this.historyLoading.set(false)}}
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
     if(this.historyFlowId()===id)this.selectHistoryFlow(0);
     this.resetFlow();
     await this.refresh();
     this.message.set('Flow deleted');
   }catch(e){this.message.set('Delete failed: '+String(e))}
   finally{this.busy.set(false)}
 }
 async save(){this.busy.set(true);try{const id=this.selectedId();const body={name:this.name(),graph:this.graph(),enabled:this.enabled(),interval_minutes:this.interval(),event_key:this.eventKey()};
   const flow:SavedFlow=await this.request(id?'/api/admin/flows/'+id:'/api/admin/flows',{method:id?'PUT':'POST',body:JSON.stringify(body)});
   this.selectedId.set(flow.id);this.historyFlowId.set(flow.id);this.savedSnapshot.set(this.currentSnapshot());this.message.set('Flow saved');await this.refresh()}catch(e){this.message.set(String(e))}finally{this.busy.set(false)}}
 async run(){
   const issues=this.graphIssues();
   if(issues.length){this.message.set('Resolve '+issues.length+' graph issue(s) before running.');return}
   if(this.hasUnsavedChanges()){this.message.set('Save current edits before running.');return}
   this.busy.set(true);try{await this.request('/api/admin/flows/'+this.selectedId()+'/run',{method:'POST'});this.historyFlowId.set(this.selectedId());this.message.set('Agent run queued; open Run history to see its progress');setTimeout(()=>void this.refreshRuns(),3000)}catch(e){this.message.set(String(e))}finally{this.busy.set(false)}}
 async emitEvent(){this.busy.set(true);try{const definition=this.triggers().find(t=>t.event_key===this.eventKey());if(!definition)throw Error('Create the custom trigger first');let value:string|number|boolean=this.triggerValue();if(definition.payload_type==='number'){value=Number(value);if(!Number.isFinite(value))throw Error('Event requires a finite number')}if(definition.payload_type==='boolean'){if(value!=='true'&&value!=='false')throw Error('Use true or false for a boolean event');value=value==='true'}const data=await this.request('/api/admin/flow-events/'+encodeURIComponent(this.eventKey()),{method:'POST',body:JSON.stringify({value})});this.message.set(data.started+' flow(s) triggered');}catch(e){this.message.set(String(e))}finally{this.busy.set(false)}}
}
