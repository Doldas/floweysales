import { CanvasGestures } from './canvas-gestures';
import { HostListener, Component, ChangeDetectionStrategy, signal, effect } from '@angular/core';
import { MarkdownRunOutputComponent } from './markdown-run-output';
import { FlowSideEffectsComponent } from './flow-side-effects';
import { clampToolBudget, describeToolCall, toggleTool, toolLabel, type GrantableTool, type ToolCall } from './agent-tools';
import { FlowEventAdaptersComponent } from './flow-event-adapters';
import { capabilityManifest, describeCapability, nodeCapability, readinessHeadline, type Readiness } from './side-effects';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { inject } from '@angular/core';
import { BlueprintLibraryComponent } from './blueprint-library';
import { NotifyService } from '../core/notify.service';
import { AuthState } from '../auth.service';
import { canCancelRun, canRetryRun, compareRunSteps, executedNodeOrder, filterFlowRuns, isActiveRun, pushHistorySnapshot, runAttemptLabel, runStatusLabel } from './flow-history';
import { listOverviewFlows } from './flow-overview';
import { semanticIssues } from './flow-semantics';
import { nearestPort, portPoints, wireFromDrag, edgeKey, wirePath, type PortPoint } from './wire-drag';
import { bindInputPin, declaredPins, mixesPinAndPlainInputs, pinBindings, pinEdgeProblem, pinOffset, routeOutputPin, unboundInputPins } from './function-pins';
import { FlowNotifyChannelsComponent } from './flow-notify-channels';
import { INVESTOR_NODES, INVESTOR_PLAYBOOKS, LEADERBOARD_STRATEGIES, LEADERBOARD_UNIVERSES, SCREEN_FIELDS, buildPlaybook, investorNode, isInvestorNode, watchlistProblem } from './investor-nodes';
import { PublishState, RevisionSummary, canPublish, pinDescription, publishStatusLabel, revisionOptionLabel, runRevisionLabel } from './flow-versions';

interface FlowNode { id:string; type:'trigger'|'agent'|'flow'|'constant'|'function'|'save_result'|'load_result'|'transform'|'workspace_read'|'workspace_write'|'output'|'condition'|'merge'|'json_get'|'http_get'|'web_crawl'|'web_search'|'loop'|'emit_event'|'watchlist_quotes'|'technical_scan'|'fundamentals'|'sec_filings'|'news'|'leaderboard'|'screen'|'only_new'|'report_table'|'notify'; label:string; symbols?:string; universe?:string; strategy?:string; provider?:string; rule?:string; sort_by?:string; limit?:number; columns?:string; title?:string; key_fields?:string; cooldown_hours?:number; lookback_days?:number; forms?:string; channel?:'email'|'webhook'; subject?:string; skip_empty?:boolean; event?:string; operator?:string; property?:string; output_mode?:'markdown'|'json'; model?:string; json_contract?:string; timeout_seconds?:number; max_retries?:number; tools?:string[]; max_tool_calls?:number;require_grounding?:boolean; variable?:string; transform?:string; loop_mode?:string; loop_limit?:number; loop_condition?:string; content_type?:string; prompt?:string; flow_id?:number; flow_revision?:number; function_id?:number; function_revision?:number; approval?:'required'; value_type?:'text'|'number'|'boolean'|'json'|'xml'; value?:string; x:number; y:number }
interface FlowEdge { from:string; to:string; from_port?:string; to_port?:string }
interface FunctionDef { id:number; name:string; graph?:unknown }
interface FlowGraph { version?:number; nodes:FlowNode[]; edges:FlowEdge[] }
interface SavedFlow extends PublishState { id:number; name:string; graph:FlowGraph; enabled:boolean; interval_minutes:number; event_key:string }
interface FlowRunStep {id:number;node:string;label:string;status:string;input?:string;output:string;error?:string;started_at?:string;created_at?:string;duration_ms?:number|null;attempts?:number|null;worker_attempt?:number|null;tool_calls?:ToolCall[]}
interface FlowRun { id:number; parent_run_id?:number|null; trigger:string; status:string; output:string; error:string; started_at?:string; finished_at?:string|null; duration_ms?:number|null; failed_node?:string|null; flow_revision?:number|null; attempts?:number; max_attempts?:number; queued_at?:string|null }
interface TriggerDefinition {event_key:string;payload_type:'text'|'number'|'boolean'}

@Component({
 selector:'fse-flow-studio', standalone:true,
 imports:[CommonModule,FormsModule,BlueprintLibraryComponent,MarkdownRunOutputComponent,FlowSideEffectsComponent,FlowEventAdaptersComponent,FlowNotifyChannelsComponent],
 host:{class:'fs-native-forms'},
 changeDetection:ChangeDetectionStrategy.Default,
 template:`
 <div class="flow-studio">
   <nav class="studio-tabs" role="tablist" aria-label="Flow Studio sections">
     <button type="button" role="tab" class="studio-tab" [attr.aria-selected]="studioTab()==='overview'" [class.active]="studioTab()==='overview'" (click)="studioTab.set('overview')"><i class="bi bi-grid" aria-hidden="true"></i>My Flows</button>
     <button type="button" role="tab" class="studio-tab" [attr.aria-selected]="studioTab()==='editor'" [class.active]="studioTab()==='editor'" (click)="studioTab.set('editor')"><i class="bi bi-bezier2" aria-hidden="true"></i>Flow editor</button>
     <button type="button" role="tab" class="studio-tab" [attr.aria-selected]="studioTab()==='runs'" [class.active]="studioTab()==='runs'" (click)="studioTab.set('runs')"><i class="bi bi-clock-history" aria-hidden="true"></i>Run history</button>
     <button type="button" role="tab" class="studio-tab" [attr.aria-selected]="studioTab()==='approvals'" [class.active]="studioTab()==='approvals'" (click)="studioTab.set('approvals')"><i class="bi bi-shield-check" aria-hidden="true"></i>Approvals &amp; audit</button>
     @if(auth.user()?.role==='SYS_ADMIN'){
     <button type="button" role="tab" class="studio-tab" [attr.aria-selected]="studioTab()==='library'" [class.active]="studioTab()==='library'" (click)="studioTab.set('library')"><i class="bi bi-boxes" aria-hidden="true"></i>Function library &amp; triggers</button>
     }
   </nav>
   @if(studioTab()==='overview'){
    <section class="flows-overview" aria-label="My Flows">
      <div class="overview-heading"><div><h2>My Flows</h2><p class="muted">Agent automations you can run, schedule and audit.</p></div><div class="overview-actions"><button class="btn btn-sm btn-outline-secondary" (click)="refresh()"><i class="bi bi-arrow-clockwise" aria-hidden="true"></i>Refresh</button><button class="btn btn-sm btn-primary" (click)="startFlowFromOverview()"><i class="bi bi-plus-lg" aria-hidden="true"></i>New flow</button></div></div>
      <div class="overview-filters">
       <label>Search<input type="search" [ngModel]="flowSearch()" (ngModelChange)="flowSearch.set($event)" placeholder="Flow name or event key"></label>
       <label>Status<select [ngModel]="flowFilter()" (ngModelChange)="flowFilter.set($event)"><option value="all">All</option><option value="enabled">Enabled</option><option value="disabled">Disabled</option><option value="scheduled">Scheduled</option><option value="manual">Not scheduled</option></select></label>
       <label>Sort<select [ngModel]="flowSort()" (ngModelChange)="flowSort.set($event)"><option value="name">Name A–Z</option><option value="name-desc">Name Z–A</option><option value="newest">Newest first</option><option value="oldest">Oldest first</option></select></label>
      </div>
      <p class="muted">{{listedFlows().length}} of {{flows().length}} flows · {{enabledFlowCount()}} enabled · {{scheduledFlowCount()}} scheduled</p>
      @if(!flows().length){<div class="overview-empty"><i class="bi bi-diagram-3" aria-hidden="true"></i><h3>No saved flows</h3><p class="muted mb-1">Start blank or pick a template from the editor.</p><button class="btn btn-sm btn-primary" (click)="startFlowFromOverview()">Create your first flow</button></div>}
      @else if(!listedFlows().length){<div class="overview-empty"><h3>No matching flows</h3><button class="btn btn-sm btn-outline-secondary" (click)="clearFlowFilters()">Clear filters</button></div>}
      @else{<div class="flow-overview-grid">
       @for(flow of listedFlows();track flow.id){<article class="overview-flow-card">
        <div class="overview-flow-top"><span class="flow-glyph" aria-hidden="true"><i class="bi bi-diagram-3"></i></span><div class="flex-grow-1 min-w-0"><h3>{{flow.name}}</h3><p class="muted">{{flow.graph.nodes.length}} nodes · {{flow.graph.edges.length}} links</p></div><span class="fs-status" [class.is-success]="flow.enabled">{{flow.enabled?'Enabled':'Disabled'}}</span></div>
        <p class="overview-flow-trigger"><i class="bi" [class.bi-clock]="flow.interval_minutes>0" [class.bi-lightning-charge]="!(flow.interval_minutes>0)&&!!flow.event_key" [class.bi-hand-index]="!(flow.interval_minutes>0)&&!flow.event_key" aria-hidden="true"></i> @if(flow.interval_minutes>0){Every {{flow.interval_minutes}} min}@else if(flow.event_key){Event: {{flow.event_key}}}@else{Manual execution}</p>
        <p class="muted">{{publishStatusLabel(flow)}}</p>
        <div class="overview-actions"><button class="btn btn-sm btn-primary" (click)="openFlowFromOverview(flow.id)">Edit</button><button class="btn btn-sm btn-outline-secondary" (click)="openHistoryForFlow(flow.id)">History</button><button class="btn btn-sm btn-outline-secondary" (click)="runSavedFlow(flow.id)" [disabled]="busy()"><i class="bi bi-play-fill" aria-hidden="true"></i>Run now</button></div>
       </article>}
      </div>}
      @if(message()){<p class="flow-message" role="status">{{message()}}</p>}
    </section>
    }
    @if (studioTab()==='editor') {
   <div class="flow-toolbar">
     <label>Saved flows <select [ngModel]="selectedId()" (ngModelChange)="loadFlow(+$event)"><option [ngValue]="0">New flow</option>@for(f of flows();track f.id){<option [ngValue]="f.id">{{f.name}}</option>}</select></label>
     <button class="btn btn-sm btn-outline-secondary" (click)="newFlow()">New</button>
     <label>Starter templates
      <select [ngModel]="''" (ngModelChange)="applyFlowTemplate($event)">
       <option value="">Choose a template…</option>
       <option value="research">Market research report</option>
       <option value="risk">Risk-score decision</option>
       <option value="event">Event-driven analysis</option>
       <optgroup label="Investor playbooks">@for(p of playbooks;track p.id){<option [value]="p.id">{{p.label}} ({{p.schedule}})</option>}</optgroup>
      </select>
     </label>
     <button class="btn btn-sm btn-outline-danger" (click)="confirmDeleteFlow()" [disabled]="!selectedId()||busy()">Delete flow</button>
     <button class="btn btn-sm btn-primary" type="button" (click)="toggleNodePicker()" [attr.aria-expanded]="nodePickerOpen()" aria-controls="node-picker">+ Add node</button>
     <button class="btn btn-sm btn-outline-secondary" (click)="linkSelected()" [disabled]="!selectedNode()||graph().nodes.length<3">Connect to…</button>
     <button class="btn btn-sm btn-outline-secondary" (click)="removeSelected()" [disabled]="!selectedNode()||selectedNode()?.type==='trigger'">Remove node</button>
   </div>
   @if(nodePickerOpen()){
    <section id="node-picker" class="node-picker" aria-label="Add a node">
      <div class="node-picker-top"><strong>Add a node</strong><button type="button" (click)="nodePickerOpen.set(false)" aria-label="Close node picker">×</button></div>
      <label>Search nodes<input type="search" [ngModel]="nodePickerQuery()" (ngModelChange)="nodePickerQuery.set($event)" (keydown.escape)="nodePickerOpen.set(false)" placeholder="Search node type"></label>
      <div class="node-picker-items">
        @for(item of visibleNodeTypes();track item.id){
          <button type="button" (click)="addPickedNode(item.id)"><i class="bi bi-plus-circle" aria-hidden="true"></i>{{item.label}}</button>
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
      <button type="button" (click)="zoomCanvas(0.8)" aria-label="Zoom in"><i class="bi bi-zoom-in" aria-hidden="true"></i></button>
      <span>{{ canvasZoomPercent() }}%</span>
      <button type="button" (click)="zoomCanvas(1.25)" aria-label="Zoom out"><i class="bi bi-zoom-out" aria-hidden="true"></i></button>
      <button type="button" (click)="fitCanvas()" title="Fit all nodes in view"><i class="bi bi-fullscreen" aria-hidden="true"></i> Fit flow</button>
      <button type="button" (click)="resetCanvas()" title="Reset canvas view"><i class="bi bi-bootstrap-reboot" aria-hidden="true"></i> Reset view</button>
      @if(connectingFrom()){<span class="canvas-connect-hint" role="status">Select an input port <button type="button" (click)="cancelConnecting()">Cancel</button></span>}
    </div>
    <div class="flow-workspace" aria-label="Agent flow graph editor">
    <svg [attr.viewBox]="canvasViewBox()" (wheel)="onCanvasWheel($event)" (pointerdown)="onCanvasPointerDown($event)" (click)="closeQuickNodeOnCanvas($event)" (pointermove)="onCanvasPointerMove($event)" (pointerup)="onCanvasPointerEnd($event)" (pointercancel)="onCanvasPointerEnd($event)" (contextmenu)="onCanvasContextMenu($event)" role="group" aria-label="Flow canvas: nodes and directed connections">
     <defs><marker id="flow-arrow" markerWidth="9" markerHeight="9" refX="8" refY="4" orient="auto"><path d="M0,0 L8,4 L0,8 Z" class="edge-arrow"/></marker></defs>
     @for(edge of graph().edges;track edgeKey(edge)){
       <path [attr.d]="edgePath(edge)" class="flow-edge" marker-end="url(#flow-arrow)"/>
     }
     @if(wireDrag();as w){<path class="wire-ghost" [attr.d]="wireGhostPath()" pointer-events="none"/>@if(w.target;as t){<circle class="wire-target" [attr.cx]="t.x" [attr.cy]="t.y" r="10" pointer-events="none"/>}}
     @for(n of graph().nodes;track n.id){
       <g data-canvas-node (click)="$event.stopPropagation();openQuickNode(n.id)" (pointerdown)="dragStart($event,n.id)" style="cursor:move" tabindex="0"
          (keydown.enter)="openQuickNode(n.id)" [attr.aria-label]="'Select '+n.label">
         <rect class="flow-node" [class.selected]="n.id===selectedNode()?.id" [attr.x]="n.x" [attr.y]="n.y" width="125" height="70" rx="12"/>
         <rect class="flow-node-accent" [attr.data-type]="n.type" [attr.x]="n.x" [attr.y]="n.y" width="4" height="70" rx="2"/>
         <text class="node-type" [attr.x]="n.x+12" [attr.y]="n.y+24" font-size="11">{{n.type==='trigger'?'⏱ TRIGGER':n.type==='flow'?'↪ SUBFLOW':n.type==='constant'?'◆ VALUE':n.type==='function'?'ƒ FUNCTION':n.type==='save_result'?'▣ SAVE':n.type==='load_result'?'▤ LOAD':n.type==='transform'?'⇄ TRANSFORM':n.type==='workspace_read'?'▤ READ FILE':n.type==='workspace_write'?'▣ WRITE FILE':n.type==='output'?'→ OUTPUT':n.type==='condition'?'◇ IF / ELSE':n.type==='merge'?'⇉ MERGE':n.type==='json_get'?'{} JSON FIELD':n.type==='emit_event'?'⇢ EMIT EVENT':investorTag(n.type)}}</text>
         <text class="node-label" [attr.x]="n.x+12" [attr.y]="n.y+48" font-size="13">{{n.label.slice(0,16)}}</text>
          @for(pin of functionPins(n).inputs;track pin.name;let i=$index){<g><circle [attr.cx]="n.x" [attr.cy]="n.y+pinOffset(i,functionPins(n).inputs.length,70)" r="6" class="canvas-port pin-in" (pointerdown)="$event.stopPropagation()" (click)="$event.stopPropagation()"><title>Input {{pin.name}} ({{pin.type}})</title></circle><text [attr.x]="n.x+8" [attr.y]="n.y+pinOffset(i,functionPins(n).inputs.length,70)+3" font-size="8" class="pin-label">{{pin.name.slice(0,8)}}</text></g>}
          @for(pin of functionPins(n).outputs;track pin.name;let i=$index){<g><circle [attr.cx]="n.x+125" [attr.cy]="n.y+pinOffset(i,functionPins(n).outputs.length,70)" r="6" class="canvas-port pin-out" (pointerdown)="startWire($event,n.id,pin.name)" (click)="$event.stopPropagation()"><title>Output {{pin.name}} ({{pin.type}})</title></circle><text [attr.x]="n.x+117" [attr.y]="n.y+pinOffset(i,functionPins(n).outputs.length,70)+3" font-size="8" class="pin-label" text-anchor="end">{{pin.name.slice(0,8)}}</text></g>}
          @if(n.type!=='trigger'&&!functionPins(n).inputs.length){<circle class="canvas-port input-port" [attr.cx]="n.x" [attr.cy]="n.y+35" r="8" tabindex="0" role="button" [attr.aria-label]="'Connect into '+n.label" (pointerdown)="$event.stopPropagation()" (click)="$event.stopPropagation();onInputPortClick(n.id)" (keydown.enter)="$event.stopPropagation();chooseInputPort(n.id)"/>}
          @if(n.type!=='output'&&!functionPins(n).outputs.length){<circle class="canvas-port output-port" [class.port-active]="connectingFrom()===n.id" [attr.cx]="n.x+125" [attr.cy]="n.y+35" r="8" tabindex="0" role="button" [attr.aria-label]="'Start connection from '+n.label" (pointerdown)="startWire($event,n.id)" (click)="$event.stopPropagation();onOutputPortClick(n.id)" (keydown.enter)="$event.stopPropagation();chooseOutputPort(n.id)"/>}
       </g>
     }
    </svg>
   </div>
   @if(quickNode();as n){<section class="node-quick-tooltip node-full-tooltip" role="region" aria-label="Complete node settings" (click)="$event.stopPropagation()"><header class="quick-tooltip-title"><strong>⚙ {{n.label}} · {{n.type}}</strong><button type="button" class="btn-close quick-tooltip-close" (click)="closeQuickNode()" aria-label="Close node settings"></button></header><div class="inspector-fields">
       @if(n.type!=='trigger'){
         <label>Connect output from <select #nodeSource><option value="">Choose upstream node</option>@for(other of graph().nodes;track other.id){@if(other.id!==n.id&&other.type!=='output'){<option [value]="other.id">{{other.label}}</option>}}</select></label>
         <button class="btn btn-sm btn-outline-secondary" (click)="connect(nodeSource.value,n.id)" [disabled]="!nodeSource.value">Connect nodes</button>
         <div class="field-mapper">
          <strong>Map JSON field → input</strong>
          <label>Source agent<select #mappingSource><option value="">Choose JSON agent</option>
           @for(other of graph().nodes;track other.id){@if(other.id!==n.id&&other.type==='agent'&&other.output_mode==='json'){<option [value]="other.id">{{other.label}}</option>}}
          </select></label>
          @if(mappingSource.value){
           <label>Available fields<select #mappingField><option value="">Choose property</option>
             @for(field of nodeContractFields(mappingSource.value);track field){<option [value]="field">{{field}}</option>}
           </select></label>
           <button class="btn btn-sm btn-outline-secondary" [disabled]="!mappingField.value" (click)="mapField(mappingSource.value,n.id,mappingField.value)">Connect field</button>
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
        @if(auth.user()?.role==='SYS_ADMIN'){
         <fieldset class="tool-grants">
          <legend>Research tools ({{n.tools?.length||0}} of {{toolLimits().max_tools}})</legend>
          <p class="muted">Granted tools are read-only: market data, SEC filings, web search and calculators. Every call is recorded in Run history. Without a grant the agent answers from its prompt alone.</p>
          @for(tool of agentTools();track tool.name){
           <label class="checkbox-row" [title]="tool.description"><input type="checkbox" [checked]="n.tools?.includes(tool.name)" (change)="toggleAgentTool(n.id,tool.name)"> {{toolLabel(tool.name)}}</label>
          }
          @if(n.tools?.length){<label>Max tool calls (0 = default {{toolLimits().default_tool_calls}}) <input type="number" min="0" [max]="toolLimits().max_tool_calls" step="1" [ngModel]="n.max_tool_calls||0" (ngModelChange)="updateToolBudget(n.id,+$event)"></label>}
         </fieldset>
        } @else if(n.tools?.length){<p class="muted">This agent has {{n.tools.length}} research tool(s) granted by an administrator.</p>}
        <label class="checkbox-row"><input type="checkbox" [checked]="!!n.require_grounding" (change)="setGrounding(n.id,$any($event.target).checked)"> Require grounded figures</label>
        <p class="muted">Every figure in the answer must appear in {{n.tools?.length?'this attempt\'s tool results or ':''}}the node input, for example rows from a Watchlist or Fundamentals node. Otherwise the attempt fails and lists the unsupported figures.</p>
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
           <option value="not_empty">Has rows / is not empty</option><option value="is_empty">Is empty</option>
         </select></label>
         @if(n.operator!=='is_empty'&&n.operator!=='not_empty'){<label>Compare with <input [ngModel]="n.value||''" (ngModelChange)="updateNodeFor(n.id,'value',$event)"></label>}
         <label>True → <select #trueTarget><option value="">Select node</option>@for(other of graph().nodes;track other.id){@if(other.id!==n.id){<option [value]="other.id">{{other.label}}</option>}}</select></label>
         <button class="btn btn-sm btn-outline-secondary" (click)="connectBranch(n.id,trueTarget.value,'true')">Connect TRUE</button>
         <label>False → <select #falseTarget><option value="">Select node</option>@for(other of graph().nodes;track other.id){@if(other.id!==n.id){<option [value]="other.id">{{other.label}}</option>}}</select></label>
         <button class="btn btn-sm btn-outline-secondary" (click)="connectBranch(n.id,falseTarget.value,'false')">Connect FALSE</button>
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
         <button class="btn btn-sm btn-outline-secondary" (click)="connectValue(n.id,valueTarget.value)">Connect typed value</button>
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
       @if(isInvestorNode(n.type)){
        <p class="muted investor-help">{{investorNode(n.type)?.description}}</p>
        @if(n.type==='watchlist_quotes'||n.type==='technical_scan'||n.type==='fundamentals'||n.type==='sec_filings'||n.type==='news'){
         <label>Watchlist (up to 25 symbols) <textarea rows="2" spellcheck="false" [ngModel]="n.symbols||''" (ngModelChange)="updateInvestor(n.id,'symbols',$event)" maxlength="600" placeholder="AAPL, MSFT, VOLV-B.ST"></textarea></label>
         <p class="muted">Leave empty to use the symbols in this node's input, for example the rows of a screen or another watchlist node.</p>
         @if(n.type!=='sec_filings'){<label>Provider <select [ngModel]="n.provider||''" (ngModelChange)="updateInvestor(n.id,'provider',$event)"><option value="">{{n.type==='news'?'First provider with news':'Server default'}}</option>@for(p of providers();track p.id){<option [value]="p.id">{{p.label||p.id}}</option>}</select></label>}
        }
        @if(n.type==='leaderboard'){
         <label>Rank <select [ngModel]="n.universe||''" (ngModelChange)="setLeaderboardUniverse(n.id,$event)">@for(u of leaderboardUniverses;track u.id){<option [value]="u.id">{{u.label}}</option>}<option value="">My watchlist, or the symbols in the input</option></select></label>
         @if(!n.universe){
          <label>Watchlist (up to 25 symbols) <textarea rows="2" spellcheck="false" [ngModel]="n.symbols||''" (ngModelChange)="updateInvestor(n.id,'symbols',$event)" maxlength="600" placeholder="AAPL, MSFT, VOLV-B.ST"></textarea></label>
         }
         <p class="muted">With a universe or watchlist set and symbol rows in the input (for example new filings), the whole list is ranked and only the input's symbols are passed on, with their rank.</p>
         <label>Strategy <select [ngModel]="n.strategy||'best'" (ngModelChange)="updateInvestor(n.id,'strategy',$event==='best'?'':$event)">@for(s of leaderboardStrategies;track s.id){<option [value]="s.id">{{s.label}}</option>}</select></label>
         <label>Provider <select [ngModel]="n.provider||''" (ngModelChange)="updateInvestor(n.id,'provider',$event)"><option value="">Server default</option>@for(p of providers();track p.id){<option [value]="p.id">{{p.label||p.id}}</option>}</select></label>
         <label>Pass on the top (1–25) <input type="number" min="1" max="25" [ngModel]="n.limit||10" (ngModelChange)="updateInvestorNumber(n.id,'limit',+$event,1,25)"></label>
         <p class="muted">Each factor is a 0–100 percentile within the ranked list; a missing factor counts as 40. Rows carry rank, score, grade, the six factor scores, key figures, reasons and flags such as overbought or below_sma200.</p>
        }
        @if(n.type==='sec_filings'){
         <label>Forms <input [ngModel]="n.forms||''" (ngModelChange)="updateInvestor(n.id,'forms',$event)" maxlength="80" placeholder="8-K, 10-Q, 10-K (empty: all)"></label>
        }
        @if(n.type==='sec_filings'||n.type==='news'){
         <label>Look back (days, 1–90) <input type="number" min="1" max="90" [ngModel]="n.lookback_days||(n.type==='news'?3:7)" (ngModelChange)="updateInvestorNumber(n.id,'lookback_days',+$event,1,90)"></label>
        }
        @if(n.type==='news'){<label>Headlines per symbol (1–20) <input type="number" min="1" max="20" [ngModel]="n.limit||5" (ngModelChange)="updateInvestorNumber(n.id,'limit',+$event,1,20)"></label>}
        @if(n.type==='screen'){
         <label>Rule <textarea rows="2" spellcheck="false" [ngModel]="n.rule||''" (ngModelChange)="updateInvestor(n.id,'rule',$event)" maxlength="500" placeholder="rsi14 < 30 and above_sma200"></textarea></label>
         <p class="muted">Compare fields with &lt; &lt;= &gt; &gt;= == != or contains, and combine with and, or, not and parentheses. Rows without a field never match. Empty keeps every row.</p>
         @if(screenFields(n.id).length){<p class="muted field-hints">Fields from {{screenSourceLabel(n.id)}}: @for(f of screenFields(n.id);track f){<button type="button" class="field-chip" (click)="appendRuleField(n.id,f)">{{f}}</button>}</p>}
         <label>Sort by <input [ngModel]="n.sort_by||''" (ngModelChange)="updateInvestor(n.id,'sort_by',$event)" maxlength="64" placeholder="change_1d_pct (prefix - for descending)"></label>
         <label>Keep at most (0 = all) <input type="number" min="0" max="100" [ngModel]="n.limit||0" (ngModelChange)="updateInvestorNumber(n.id,'limit',+$event,0,100)"></label>
        }
        @if(n.type==='only_new'){
         <label>Identify rows by <input [ngModel]="n.key_fields||''" (ngModelChange)="updateInvestor(n.id,'key_fields',$event)" maxlength="120" placeholder="id, url or symbol by default"></label>
         <label>Let a row through again after (hours, 0 = never) <input type="number" min="0" max="2160" [ngModel]="n.cooldown_hours||0" (ngModelChange)="updateInvestorNumber(n.id,'cooldown_hours',+$event,0,2160)"></label>
         <p class="muted">Remembers up to 2,000 rows for 180 days per node. Use a cooldown for conditions that last, such as "RSI below 30", so you hear about them once a day instead of every run.</p>
        }
        @if(n.type==='report_table'){
         <label>Title <input [ngModel]="n.title||''" (ngModelChange)="updateInvestor(n.id,'title',$event)" maxlength="150"></label>
         <label>Columns <input [ngModel]="n.columns||''" (ngModelChange)="updateInvestor(n.id,'columns',$event)" maxlength="300" placeholder="symbol, price, change_1d_pct (empty: automatic)"></label>
         <label>Rows (1–100) <input type="number" min="0" max="100" [ngModel]="n.limit||0" (ngModelChange)="updateInvestorNumber(n.id,'limit',+$event,0,100)"></label>
        }
        @if(n.type==='notify'){
         <label>Send to <select [ngModel]="n.channel||'email'" (ngModelChange)="updateInvestor(n.id,'channel',$event)"><option value="email">Email the flow owner</option><option value="webhook">Chat webhook (Slack, Discord, Teams)</option></select></label>
         <label>Subject <input [ngModel]="n.subject||''" (ngModelChange)="updateInvestor(n.id,'subject',$event)" maxlength="150" placeholder="{{'{{flow}}'}}: {{'{{count}}'}} alerts on {{'{{date}}'}}"></label>
         <p class="muted">Placeholders: {{'{{flow}}'}}, {{'{{date}}'}} and {{'{{count}}'}} (rows in the input). JSON rows are sent as a table. Nothing is sent when the input is empty. Configure the webhook under Function library &amp; triggers → Notifications.</p>
        }
       }
       @if(graph().version===2&&n.type!=='trigger'&&n.type!=='merge'&&n.type!=='constant'){
        <label class="checkbox-row"><input type="checkbox" [checked]="!!n.skip_empty" (change)="updateInvestor(n.id,'skip_empty',$any($event.target).checked)"> Skip when the input is empty</label>
        @if(n.skip_empty){<p class="muted">When there is nothing in the input (no rows, no new items) this node and everything after it are skipped, so agents and notifications only run when there is something to say.</p>}
       }
       @if(nodeCapability(n);as cap){
        <fieldset class="capability-box">
         <legend>Side effect · {{cap.capability}}</legend>
         <p class="muted">{{describeCapability(cap.capability,cap.target)}}. This is the only change the node can make, and every run that makes it is recorded under Approvals &amp; audit.</p>
         <label class="checkbox-row"><input type="checkbox" [checked]="n.approval==='required'" (change)="setApproval($any($event.target).checked)"> Require SYS_ADMIN approval before this runs</label>
         @if(n.approval==='required'){<p class="muted">The run pauses here and resumes only after the exact content is approved. Unapproved requests expire after 7 days. Not available inside flows used as a Call Flow.</p>}
        </fieldset>
       }
       @if(n.type==='emit_event'){
        <label>Event to emit <select [ngModel]="n.event||''" (ngModelChange)="updateUtility('event',$event)">
          <option value="">Choose a custom trigger</option>
          @for(t of triggers();track t.event_key){<option [value]="t.event_key">{{t.event_key}} ({{t.payload_type}})</option>}
        </select></label>
        <p class="muted">Queues one run of every other enabled flow subscribed to this event, with this node's input as the payload (checked against the trigger's type). The input passes on unchanged. Event chains stop after {{maxEventDepth}} levels and each emission reaches at most 10 flows; a retried run never queues a subscriber twice. Define events under Function library &amp; triggers.</p>
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
         @if(functionPins(n).inputs.length||functionPins(n).outputs.length){
          <div class="field-mapper">
           <strong>Typed input pins</strong>
           @for(pin of functionPins(n).inputs;track pin.name){
            <label>{{pin.name}} ({{pin.type}})<select [ngModel]="pinSources(n)[pin.name]||''" (ngModelChange)="bindPin(pin.name,$event)">
             <option value="">Not connected</option>
             @for(other of graph().nodes;track other.id){@if(other.id!==n.id&&other.type!=='output'){<option [value]="other.id">{{other.label}}</option>}}
            </select></label>
           }
           <strong>Typed output pins</strong>
           @for(pin of functionPins(n).outputs;track pin.name){
            <label>{{pin.name}} ({{pin.type}}) → <select #pinTarget (change)="routePin(pin.name,pinTarget.value)">
             <option value="">Send to node…</option>
             @for(other of graph().nodes;track other.id){@if(other.id!==n.id&&other.type!=='trigger'){<option [value]="other.id">{{other.label}}</option>}}
            </select></label>
            @if(pinTargets(n,pin.name).length){<p class="muted">Routed to: {{pinTargets(n,pin.name).join(', ')}}</p>}
           }
           <p class="muted">Each declared input must be connected. Outputs are read from the function's result by name; unrouted outputs are simply not used. Types are checked when you save or publish.</p>
          </div>
         }
         @if(n.function_id){<label>Function revision <select [ngModel]="n.function_revision||0" (ngModelChange)="updateRevisionPin('function_revision',+$event)"><option [ngValue]="0">Follow active revision</option>@for(r of targetRevisions()['function:'+n.function_id]||[];track r.revision){<option [ngValue]="r.revision">{{revisionOptionLabel(r)}}</option>}</select></label>
         <p class="muted">{{pinDescription(n.function_revision)}}</p>}
         <p class="muted">Applies the function to the preceding node's typed value; invalid inputs fail the run safely.</p>
       }
       @if(n.type==='flow'){
         <label>Reusable flow to call <select [ngModel]="n.flow_id||0" (ngModelChange)="updateFlowReference(+$event)"><option [ngValue]="0">Choose target flow</option>@for(f of flows();track f.id){@if(f.id!==selectedId()){<option [ngValue]="f.id">{{f.name}} (#{{f.id}})</option>}}</select></label>
         @if(n.flow_id){<label>Flow revision <select [ngModel]="n.flow_revision||0" (ngModelChange)="updateRevisionPin('flow_revision',+$event)"><option [ngValue]="0">Follow active revision</option>@for(r of targetRevisions()['flow:'+n.flow_id]||[];track r.revision){<option [ngValue]="r.revision">{{revisionOptionLabel(r)}}</option>}</select></label>
         <p class="muted">{{pinDescription(n.flow_revision)}}</p>}
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
     @if(auth.user()?.role==='SYS_ADMIN'){<button type="button" role="menuitem" (click)="addEmitEvent();closeCanvasMenu()">Emit Event</button>}
     @if(auth.user()?.role==='SYS_ADMIN'){<button type="button" role="menuitem" (click)="addUtility('save_result');closeCanvasMenu()">Save Result</button>}
     @if(auth.user()?.role==='SYS_ADMIN'){<button type="button" role="menuitem" (click)="addUtility('load_result');closeCanvasMenu()">Load Result</button>}
     <button type="button" role="menuitem" (click)="addUtility('transform');closeCanvasMenu()">Transform Text</button>
     @if(auth.user()?.role==='SYS_ADMIN'){<button type="button" role="menuitem" (click)="addUtility('workspace_read');closeCanvasMenu()">Read Workspace File</button>}
     @if(auth.user()?.role==='SYS_ADMIN'){<button type="button" role="menuitem" (click)="addUtility('workspace_write');closeCanvasMenu()">Write Workspace File</button>}
     <button type="button" (click)="closeCanvasMenu()">Cancel</button>
   </div>}
   </div>
   
   </div>
   @if(readiness();as r){@if(r.applies||automated()){<section class="readiness-panel" [class.readiness-ok]="r.ready" aria-label="Production readiness"><header><strong>Production readiness</strong> <button type="button" (click)="loadReadiness()" aria-label="Re-check production readiness">Re-check</button></header><p role="status">{{readinessHeadline(r,automated())}}</p>@if(r.applies){<ul>@for(c of r.checks;track c.id){<li [class.check-ok]="c.ok"><span aria-hidden="true">{{c.ok?'✓':'✗'}}</span> {{c.message}}</li>}</ul>}</section>}}
   @if(sideEffects().length){<section class="capability-summary" aria-label="Side effects this flow can perform"><strong>Side effects</strong><ul>@for(use of sideEffects();track use.node){<li><button type="button" (click)="openQuickNode(use.node)">{{use.label}}</button>: {{describeCapability(use.capability,use.target)}}{{use.approvalRequired?' · approval required':' · runs without approval'}}</li>}</ul></section>}
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
     <button class="btn btn-sm btn-outline-secondary" type="button" (click)="undoGraph()" [disabled]="!canUndo()||busy()" aria-label="Undo graph change" title="Undo (Ctrl/Cmd+Z)">↶ Undo</button>
     <button class="btn btn-sm btn-outline-secondary" type="button" (click)="redoGraph()" [disabled]="!canRedo()||busy()" aria-label="Redo graph change" title="Redo (Ctrl/Cmd+Shift+Z or Ctrl+Y)">↷ Redo</button>
     <button class="btn btn-sm btn-primary" (click)="save()" [disabled]="busy()">Save flow</button>
      <span class="muted" role="status">{{hasUnsavedChanges()?'Unsaved changes':'All changes saved'}}</span>
     <button class="btn btn-sm btn-outline-secondary" (click)="run()" [disabled]="busy()||!selectedId()" [title]="currentFlow()?.active_revision?'Runs the active revision':'Runs the saved draft'">Run now</button>
     @if(currentFlow()?.active_revision&&currentFlow()?.has_unpublished_changes){<button class="btn btn-sm btn-outline-secondary" (click)="run('draft')" [disabled]="busy()||hasUnsavedChanges()" title="Test unpublished edits without publishing them">Run draft</button>}
     <label>Event value <input [ngModel]="triggerValue()" (ngModelChange)="triggerValue.set($event)" placeholder="Value matching trigger type"></label>
     <button class="btn btn-sm btn-outline-secondary" (click)="emitEvent()" [disabled]="busy()||!eventKey()">Trigger event</button>
     <button class="btn btn-sm btn-outline-secondary" (click)="refresh()">Refresh</button>
   </div>
   @if(currentFlow();as current){
   <section class="flow-versions" aria-label="Published versions">
     <header><strong>Versions</strong><span role="status">{{publishStatusLabel(current)}}</span></header>
     <div class="flow-version-publish">
       <label>Publish note <input [ngModel]="publishNote()" (ngModelChange)="publishNote.set($event)" maxlength="500" placeholder="What changed (optional)"></label>
       <button class="btn btn-sm btn-primary" (click)="publish()" [disabled]="busy()||!canPublish(current,hasUnsavedChanges())" [title]="hasUnsavedChanges()?'Save the draft before publishing':'Freeze the saved draft as the next revision and make it active'">Publish revision</button>
     </div>
     <p class="muted">Scheduled, event and manual runs use the active revision. Save edits freely; they reach runs only when published. Activate an older revision to roll back.</p>
     @if(revisions().length){<ul class="flow-version-list">
       @for(r of revisions();track r.revision){<li [class.active]="r.active">
         <span><strong>Revision {{r.revision}}</strong>@if(r.active){ · active} · {{displayRunTime(r.published_at)}}@if(r.note){ · {{r.note}}}</span>
         <span class="flow-version-actions">
           <button class="btn btn-sm btn-outline-secondary" (click)="activateRevision(r.revision)" [disabled]="busy()||r.active">Activate</button>
           <button class="btn btn-sm btn-outline-secondary" (click)="restoreRevision(r.revision)" [disabled]="busy()">Restore to draft</button>
         </span>
       </li>}
     </ul>} @else {<p class="muted">No published revisions yet.</p>}
   </section>
   }
   @if(message()) { <p class="flow-message" role="status">{{message()}}</p> }
   }
   @if (studioTab()==='library') {
     <fse-blueprint-library (functionsChanged)="loadFunctions()"></fse-blueprint-library>
     @if(auth.user()?.role==='SYS_ADMIN'){ <fse-flow-event-adapters></fse-flow-event-adapters><fse-flow-notify-channels></fse-flow-notify-channels> }
   }
   @if (studioTab()==='approvals') { <fse-flow-side-effects></fse-flow-side-effects> }
   @if (studioTab()==='runs') {
   <div class="run-history-heading"><h3>Recent runs</h3>
     <label>Flow <select [ngModel]="historyFlowId()" (ngModelChange)="selectHistoryFlow(+$event)" aria-label="Select flow for history"><option [ngValue]="0">Select a flow…</option>@for(f of flows();track f.id){<option [ngValue]="f.id">{{f.name}}</option>}</select></label>
     <label>Find run <input [ngModel]="historySearch()" (ngModelChange)="historySearch.set($event)" placeholder="Run ID or trigger"></label>
     <label>Status <select [ngModel]="historyStatusFilter()" (ngModelChange)="historyStatusFilter.set($event)"><option value="all">All statuses</option><option value="SUCCESS">Success</option><option value="FAILED">Failed</option><option value="QUEUED">Queued</option><option value="RUNNING">Running</option><option value="AWAITING_APPROVAL">Awaiting approval</option><option value="CANCELLED">Cancelled</option><option value="DEAD">Dead-lettered</option></select></label>
     <label>Output view <select [ngModel]="logView()" (ngModelChange)="logView.set($event)">
       <option value="markdown">Markdown (default)</option><option value="raw">RAW</option><option value="json">Structured JSON</option>
     </select></label>
     <button class="btn btn-sm btn-outline-secondary" (click)="refreshRuns()" [disabled]="!historyFlowId()||historyLoading()">Refresh history</button>
     <label>Compare with run<select [ngModel]="compareRun()" (ngModelChange)="compareAgainst(+$event)">
      <option [ngValue]="0">No comparison</option>
      @for(other of runs();track other.id){<option [ngValue]="other.id">#{{other.id}} · {{runStatusLabel(other.status)}}</option>}
     </select></label>
   </div>
   @if(message()){<p class="flow-message" role="status">{{message()}}</p>}
   @if(!historyFlowId()){<p class="muted">Select a flow above to view its history.</p>}
    @else if(!runs().length&&!historyLoading()){<p class="muted">No executions yet for this flow.</p>}
    @if(historyLoading()){<p role="status">Loading run history…</p>}
    @if(runs().some(run=>isActiveRun(run.status))){<p class="muted" role="status">Queued or running run detected · history refreshes automatically every 5 seconds while this tab is open.</p>}
    @if(historyFlowId()&&runs().length&&!filteredRuns().length){<p class="muted">No runs match your filters.</p>}
   @for(run of filteredRuns();track run.id){
    <details class="flow-run">
      <summary><span class="fs-status" [class.is-success]="run.status==='SUCCESS'" [class.is-danger]="run.status==='FAILED'||run.status==='DEAD'" [class.is-warning]="canCancelRun(run.status)">{{runStatusLabel(run.status)}}</span> #{{run.id}} · {{run.trigger}} · {{runRevisionLabel(run.flow_revision)}}@if(runAttemptLabel(run.attempts,run.max_attempts);as attempt){ · {{attempt}}} <span class="muted">· {{displayRunTime(run.status==='QUEUED'?run.queued_at:run.started_at)}} · {{displayRunDuration(run)}}</span></summary>
      <div class="run-diagnostics">
       @if(run.queued_at){<span><strong>Queued:</strong> {{displayRunTime(run.queued_at)}}</span>}
       <span><strong>Started:</strong> {{run.status==='QUEUED'&&!run.attempts?'Waiting for a worker':displayRunTime(run.started_at)}}</span>
       <span><strong>Finished:</strong> {{run.status==='AWAITING_APPROVAL'?'Waiting for approval':isActiveRun(run.status)?'In progress':displayRunTime(run.finished_at)}}</span>
       @if(run.attempts){<span><strong>Worker attempts:</strong> {{run.attempts}} of {{run.max_attempts}}</span>}
       <span><strong>Duration:</strong> {{displayRunDuration(run)}}</span>
       <span><strong>Trigger:</strong> {{run.trigger}}@if(run.parent_run_id){ · triggered by run #{{run.parent_run_id}}}</span>
       <span><strong>Graph:</strong> {{runRevisionLabel(run.flow_revision)}}</span>
      </div>
      @if(run.status==='QUEUED'&&run.error){<p class="muted" role="status">{{run.error}}</p>}
      @if((run.status==='FAILED'||run.status==='DEAD')&&run.error){
       <div class="run-error-panel" role="alert">
        <strong>{{run.status==='DEAD'?'Dead-lettered':'Execution failed'}}</strong>
        <p>{{run.error}}</p>
        @if(run.failed_node){
          <button class="btn btn-sm btn-outline-secondary" (click)="inspectFailedNode(run.id,run.failed_node)">Inspect failed node: {{run.failed_node}}</button>
        }@else if(suspectedFailedNode(run);as nodeId){
          <button class="btn btn-sm btn-outline-secondary" (click)="inspectFailedNode(run.id,nodeId)">Inspect suspected node: {{nodeId}}</button>
        }
       </div>
      }
      @if(run.status==='AWAITING_APPROVAL'){<button class="btn btn-sm btn-outline-secondary" type="button" (click)="studioTab.set('approvals')">Review in Approvals &amp; audit</button>}
      @if(canCancelRun(run.status)){<button class="btn btn-sm btn-outline-danger" (click)="cancelRun(run.id)" [disabled]="busy()">{{run.status==='QUEUED'?'Remove from queue':'Cancel run'}}</button>}
      @if(canRetryRun(run.status)){<button class="btn btn-sm btn-outline-secondary" (click)="retryRun(run.id)" [disabled]="busy()" title="Queue this run again; it resumes from its last checkpoint">Retry from checkpoint</button>}
      <button class="btn btn-sm btn-outline-secondary" (click)="showRunSteps(run.id)">{{traceRunId()===run.id?'Hide node trace':'Inspect nodes'}}</button>
      @if(traceRunId()===run.id){
       @if(executionGraph();as graph){
        <section class="exec-graph" aria-label="Historical execution graph">
          <strong>Execution graph · run #{{run.id}}</strong>
          <p class="muted">Saved graph from this run. Select a node to inspect its status and recorded output.</p>
          <div class="exec-graph-canvas">
            <svg [attr.viewBox]="executionViewBox()" role="group" [attr.aria-label]="'Execution graph for run '+run.id">
              @for(edge of graph.edges;track $index){<path [attr.d]="executionEdgePath(edge,graph)" class="exec-edge"/>}
              @for(n of graph.nodes;track n.id){
                <g tabindex="0" role="button" class="exec-node" [attr.aria-label]="n.label+' '+executionNodeStatus(n.id)"
                 (click)="executionSelectedNode.set(n.id)" (keydown.enter)="executionSelectedNode.set(n.id)" (keydown.space)="$event.preventDefault();executionSelectedNode.set(n.id)">
                  <rect [attr.class]="'exec-rect tone-'+executionNodeColor(n.id)" [class.selected]="executionSelectedNode()===n.id" [attr.x]="n.x" [attr.y]="n.y" width="125" height="70" rx="10"/>
                  <text class="exec-text" [attr.x]="n.x+10" [attr.y]="n.y+24" font-size="12">{{n.label.slice(0,17)}}</text>
                  <text class="exec-sub" [attr.x]="n.x+10" [attr.y]="n.y+46" font-size="11"> {{executionNodeStatus(n.id)}}</text>
                </g>
              }
            </svg>
          </div>
          @if(executionSelectedNode();as nodeId){
            <div class="exec-selected">
              <div class="debug-head">
                <strong>{{executionNodeLabel(nodeId)}} · {{executionNodeStatus(nodeId)}}</strong>
                @if(executedNodes().length>1){<span class="debug-nav">
                  <button class="btn btn-sm btn-outline-secondary" type="button" (click)="stepExecution(-1)" [disabled]="executedIndex(nodeId)<=0" aria-label="Previous executed node">◀ Previous</button>
                  <span class="muted">Step {{executedIndex(nodeId)+1}} of {{executedNodes().length}}</span>
                  <button class="btn btn-sm btn-outline-secondary" type="button" (click)="stepExecution(1)" [disabled]="executedIndex(nodeId)<0||executedIndex(nodeId)>=executedNodes().length-1" aria-label="Next executed node">Next ▶</button>
                </span>}
              </div>
              @if(executionStep(nodeId);as step){
                <dl class="debug-meta">
                  <dt>Started</dt><dd>{{step.started_at?displayRunTime(step.started_at):'Not recorded'}}</dd>
                  <dt>Duration</dt><dd>{{step.duration_ms==null?'Unavailable':displayStepDuration(step.duration_ms)}}</dd>
                  @if(step.attempts!=null){<dt>Node attempts</dt><dd>{{step.attempts}}</dd>}
                  @if(step.worker_attempt!=null){<dt>Worker attempt</dt><dd>{{step.worker_attempt}}</dd>}
                </dl>
                @if(step.tool_calls?.length){<section class="tool-calls"><h5>Tool calls ({{step.tool_calls!.length}})</h5><ul>@for(call of step.tool_calls!;track $index){<li [class.tool-failed]="!call.ok" [title]="call.sha256?'Retrieved '+call.retrieved_at+' · SHA-256 '+call.sha256:''">{{describeToolCall(call)}}@if(call.sources?.length){<span class="tool-sources">Sources: {{call.sources!.join(', ')}}</span>}</li>}</ul></section>}
                @if(step.error){<div class="exec-error-hint" role="alert"><strong>Error</strong><pre>{{step.error}}</pre></div>}
                <div class="debug-io">
                  <section><h5>Input</h5>@if(step.input){<pre>{{step.input}}</pre>}@else{<p class="muted">{{step.status==='SKIPPED'?'Skipped: no active input branch.':'No input recorded (start node or a run recorded before input capture).'}}</p>}</section>
                  <section><h5>Output</h5>@if(step.output){<pre>{{step.output}}</pre>}@else{<p class="muted">No output.</p>}</section>
                </div>
                <p class="muted">Credentials are masked and previews are capped at 4,000 characters.</p>
              }
              @else{
                @if(activeRunError();as failure){<div class="exec-error-hint"><strong>Run error:</strong> {{failure}}</div>}
                <p class="muted">No recorded step for this node.</p>
              }
            </div>
          }
        </section>
       } @else if(!traceLoading()&&executionSnapshotUnavailable()){<p class="muted">Graph snapshot unavailable for this run. The trace remains available below.</p>}
       @if(traceLoading()){<p role="status">Loading node trace…</p>}
       @else if(!traceSteps().length){<p class="muted">No node trace available for this run. Earlier runs may predate node tracing.</p>}
       @else{
        @if(compareRun()&&compareRun()!==run.id&&comparedSteps().length){
         <div class="run-compare" role="region" [attr.aria-label]="'Comparison of run '+run.id+' with run '+compareRun()">
          <strong>Run #{{run.id}} compared with #{{compareRun()}}</strong>
          <div class="run-compare-scroll"><table>
           <thead><tr><th scope="col">Node</th><th scope="col">This run</th><th scope="col">#{{compareRun()}}</th><th scope="col">Duration change</th><th scope="col">Difference</th></tr></thead>
           <tbody>@for(row of stepComparison();track row.node){<tr [class]="'change-'+row.change">
            <td>{{row.label}}</td><td>{{row.status||'—'}}</td><td>{{row.baselineStatus||'—'}}</td>
            <td>{{durationDelta(row.durationDeltaMs)}}</td>
            <td>{{changeLabel(row.change)}}</td></tr>}</tbody>
          </table></div>
         </div>
        }
        <div class="run-step-list" aria-label="Node execution trace">
         @for(step of traceSteps();track step.id){
          <details class="run-step">
           <summary><strong [class.step-skipped]="step.status==='SKIPPED'">{{step.status}}</strong> · {{step.label||step.node}} <span class="muted">{{step.node}} · {{displayRunTime(step.created_at)}} · {{step.duration_ms==null?'Duration unavailable':displayStepDuration(step.duration_ms)}}@if(step.attempts!=null){ · {{step.attempts}} attempt(s)}</span>
            @if(compareRun()&&compareRun()!==run.id){<span class="muted"> · {{compareStatus(step.node,step.status,step.output)}}</span>}
           </summary>
           @if(step.worker_attempt!=null&&step.worker_attempt>1){<p class="muted">Worker attempt {{step.worker_attempt}}</p>}
           @if(step.error){<p><strong>Error</strong></p><pre>{{step.error}}</pre>}
           @if(step.input){<p><strong>Input</strong></p><pre>{{step.input}}</pre>}
           @if(step.output){<p><strong>Output</strong></p><pre>{{step.output}}</pre>}
           @if(step.status==='SUCCESS'&&step.output&&step.output.length<4000){
            <button class="btn btn-sm btn-outline-secondary" (click)="previewReplay(run.id,step.node)">Replay preview from this node</button>
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
 </div>`,
 styles:[`
 :host{display:block;margin:0 0 24px;color:var(--fs-text)}
 .flow-studio{display:block;min-width:0}
 .muted{color:var(--fs-muted)}
 label{display:flex;flex-direction:column;gap:4px;font-size:13px;font-weight:500;flex:1;min-width:160px;color:var(--fs-text-2)}
 textarea{width:100%}
 h2{font-size:1.05rem}h3{font-size:.95rem}

 .studio-tabs{display:flex;gap:2px;overflow-x:auto;margin:0 0 20px;border-bottom:1px solid var(--fs-border)}
 .studio-tab{display:inline-flex;align-items:center;gap:8px;flex:0 0 auto;padding:10px 14px;border:0;border-bottom:2px solid transparent;margin-bottom:-1px;background:transparent;color:var(--fs-muted);font:inherit;font-size:.875rem;font-weight:500;white-space:nowrap}
 .studio-tab:hover{color:var(--fs-text)}
 .studio-tab.active{color:var(--fs-text);border-bottom-color:var(--fs-accent)}
 .studio-tab.active .bi{color:var(--fs-accent-text)}

 .flows-overview{display:grid;gap:16px;margin:0 0 24px}
 .overview-heading,.overview-actions{display:flex;flex-wrap:wrap;align-items:center;gap:8px}
 .overview-heading{justify-content:space-between}.overview-heading h2{margin:0 0 4px}.overview-heading p{margin:0}
 .overview-filters{display:flex;flex-wrap:wrap;gap:12px}.overview-filters label{flex:1 1 190px}
 .flow-overview-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(min(100%,300px),1fr));gap:14px}
 .overview-flow-card{display:flex;flex-direction:column;gap:12px;padding:16px;border:1px solid var(--fs-border);border-radius:var(--bs-border-radius-lg);background:var(--fs-surface);box-shadow:var(--fs-shadow-sm);transition:border-color .15s,box-shadow .15s,transform .15s}
 .overview-flow-card:hover{border-color:color-mix(in srgb,var(--fs-accent) 45%,var(--fs-border));box-shadow:var(--fs-shadow);transform:translateY(-1px)}
 .overview-flow-top{display:flex;align-items:flex-start;gap:12px}
 .overview-flow-top h3{font-size:1rem;margin:0 0 2px;overflow-wrap:anywhere}.overview-flow-top p{margin:0;font-size:12px}
 .flow-glyph{display:grid;place-items:center;flex:0 0 auto;width:36px;height:36px;border-radius:10px;background:var(--fs-accent-soft);color:var(--fs-accent-text)}
 .overview-flow-trigger{margin:0;color:var(--fs-text-2);font-size:13px;overflow-wrap:anywhere;display:flex;gap:6px;align-items:center}
 .overview-flow-card>.muted{margin:0;font-size:12px}
 .overview-flow-card>.overview-actions{margin-top:auto}.overview-flow-card>.overview-actions .btn{flex:1 1 auto}
 .overview-empty{display:grid;justify-items:center;gap:8px;padding:36px 16px;border:1px dashed var(--fs-border-strong);border-radius:var(--bs-border-radius-lg);text-align:center}
 .overview-empty>.bi{font-size:1.8rem;color:var(--fs-accent)}.overview-empty h3{margin:0}

 .flow-toolbar,.flow-meta,.flow-actions{display:flex;flex-wrap:wrap;gap:10px 12px;align-items:end;margin:0 0 14px}
 .flow-toolbar{padding:12px;border:1px solid var(--fs-border);border-radius:var(--bs-border-radius-lg);background:var(--fs-surface)}
 .flow-toolbar label{flex:0 1 220px}
 .flow-meta label{flex:1 1 200px}
 .flow-check{flex-direction:row!important;align-items:center;gap:8px!important;flex:0 0 auto!important;min-width:0!important}

 .node-picker{display:grid;gap:12px;margin:0 0 14px;padding:16px;border:1px solid var(--fs-border);border-radius:var(--bs-border-radius-lg);background:var(--fs-surface);box-shadow:var(--fs-shadow)}
 .node-picker-top{display:flex;align-items:center;justify-content:space-between}
 .node-picker-top button{background:var(--fs-surface-3);border:1px solid var(--fs-border);border-radius:6px;color:var(--fs-text);min-width:32px;min-height:32px}
 .node-picker label{width:100%;min-width:0}
 .node-picker-items{display:grid;grid-template-columns:repeat(auto-fill,minmax(min(100%,180px),1fr));gap:8px;max-height:310px;overflow:auto}
 .node-picker-items button{display:flex;align-items:center;gap:8px;text-align:left;background:var(--fs-surface-2);color:var(--fs-text);border:1px solid var(--fs-border);border-radius:var(--bs-border-radius);padding:10px 12px;font:inherit;font-size:.875rem}
 .node-picker-items button:hover{border-color:var(--fs-accent);background:var(--fs-accent-soft)}
 .node-picker-items .bi{color:var(--fs-accent-text)}

 .blueprint-layout{display:grid;grid-template-columns:minmax(0,1fr);gap:12px;min-height:470px}
 .canvas-shell{position:relative;min-width:0;min-height:0;border:1px solid var(--fs-border);border-radius:var(--bs-border-radius-lg);overflow:hidden;background:var(--fs-canvas-bg)}
 .canvas-navigation{display:flex;align-items:center;gap:4px;flex-wrap:wrap;padding:8px;background:var(--fs-surface);border-bottom:1px solid var(--fs-border)}
 .canvas-navigation button,.canvas-context-menu button{display:inline-flex;align-items:center;gap:6px;background:var(--fs-surface);color:var(--fs-text-2);border:1px solid var(--fs-border-strong);border-radius:6px;min-height:32px;padding:4px 10px;font:inherit;font-size:.8125rem}
 .canvas-navigation button:hover,.canvas-context-menu button:hover{background:var(--fs-surface-3);color:var(--fs-text)}
 .canvas-navigation span{min-width:48px;text-align:center;font-variant-numeric:tabular-nums;font-size:.8125rem;color:var(--fs-muted)}
 .canvas-connect-hint{display:flex;align-items:center;gap:8px;font-size:12px;color:var(--fs-warn);margin-left:auto}
 .canvas-connect-hint button{border-color:var(--fs-warn)!important;color:var(--fs-warn)!important}
 .canvas-context-menu{position:absolute;top:55px;left:16px;z-index:5;display:flex;flex-direction:column;gap:4px;min-width:200px;padding:10px;background:var(--fs-surface);border:1px solid var(--fs-border);border-radius:var(--bs-border-radius-lg);box-shadow:var(--fs-shadow-lg)}
 .canvas-context-menu button{justify-content:flex-start;border-color:transparent}

 .flow-workspace{background:radial-gradient(var(--fs-canvas-dot) 1px,transparent 1px) 0 0/20px 20px,var(--fs-canvas-bg);overflow:auto;min-height:470px}
 .flow-workspace svg{user-select:none;-webkit-user-select:none}
 svg{min-width:660px;width:100%;height:auto;display:block;touch-action:none;cursor:grab}
 .flow-edge{fill:none;stroke:var(--fs-edge);stroke-width:2.2}
 .edge-arrow{fill:var(--fs-edge)}
 .wire-ghost{fill:none;stroke:var(--fs-wire-ghost);stroke-width:2.5;stroke-dasharray:6 4}
 .wire-target{fill:none;stroke:var(--fs-wire-ghost);stroke-width:3}
 .flow-node{fill:var(--fs-node-bg);stroke:var(--fs-node-border);stroke-width:1.5;filter:drop-shadow(0 2px 4px rgba(15,23,42,.12))}
 .flow-node.selected{fill:var(--fs-node-selected-bg);stroke:var(--fs-node-selected);stroke-width:2.5}
 [data-canvas-node]:focus-visible .flow-node{stroke:var(--fs-node-selected);stroke-width:3}
 [data-canvas-node]:focus,[data-canvas-node]:focus-visible,.exec-node:focus{outline:none}
 .flow-node-accent{fill:var(--fs-accent)}
 .flow-node-accent[data-type=trigger]{fill:#0ea5e9}.flow-node-accent[data-type=output]{fill:#10b981}.flow-node-accent[data-type=condition],.flow-node-accent[data-type=loop]{fill:#f59e0b}
 .flow-node-accent[data-type=workspace_write],.flow-node-accent[data-type=emit_event]{fill:#ef4444}.flow-node-accent[data-type=http_get],.flow-node-accent[data-type=web_search],.flow-node-accent[data-type=web_crawl]{fill:#14b8a6}
 .flow-node-accent[data-type=watchlist_quotes],.flow-node-accent[data-type=technical_scan],.flow-node-accent[data-type=fundamentals],.flow-node-accent[data-type=sec_filings],.flow-node-accent[data-type=news],.flow-node-accent[data-type=leaderboard]{fill:#f59e0b}.flow-node-accent[data-type=screen],.flow-node-accent[data-type=only_new],.flow-node-accent[data-type=report_table]{fill:#8b5cf6}.flow-node-accent[data-type=notify]{fill:#ef4444}
 .field-hints{display:flex;flex-wrap:wrap;gap:4px;align-items:center}.field-chip{border:1px solid var(--fs-border);background:var(--fs-surface-2);color:var(--fs-text);border-radius:999px;padding:1px 8px;font:inherit;font-size:.75rem}.field-chip:hover{border-color:var(--fs-accent)}
 .node-type{fill:var(--fs-node-sub);font-weight:600;letter-spacing:.04em}
 .node-label{fill:var(--fs-node-text);font-weight:600}
 .pin-label{fill:var(--fs-node-sub)}
 .canvas-port{touch-action:none;stroke:var(--fs-port-stroke);stroke-width:2;cursor:crosshair}
 .input-port,.pin-in{fill:var(--fs-port-in)}.output-port,.pin-out{fill:var(--fs-port-out)}
 .port-active,.canvas-port:focus-visible{stroke:var(--fs-wire-ghost);stroke-width:4;outline:none}

 .node-quick-tooltip{position:absolute;right:12px;top:58px;z-index:7;display:flex;flex-direction:column;gap:10px;padding:14px;width:min(480px,calc(100% - 24px));box-sizing:border-box;max-height:min(78vh,780px);overflow:auto;background:color-mix(in srgb,var(--fs-surface) 94%,transparent);backdrop-filter:blur(10px);border:1px solid var(--fs-border);box-shadow:var(--fs-shadow-lg);border-radius:var(--bs-border-radius-lg)}
 .quick-tooltip-title{display:flex;align-items:center;justify-content:space-between;gap:12px;padding-bottom:8px;border-bottom:1px solid var(--fs-border)}
 .quick-tooltip-close{flex:0 0 auto}
 .node-full-tooltip .inspector-fields{display:flex;flex-direction:column;gap:12px}
 .node-quick-tooltip label{display:flex;flex-direction:column;gap:4px;min-width:0}
 .node-quick-delete{background:var(--fs-neg-soft);color:var(--fs-neg);border:1px solid color-mix(in srgb,var(--fs-neg) 45%,transparent);border-radius:var(--bs-border-radius);padding:8px;font-weight:500}
 .inspector-fields{display:flex;flex-direction:column;gap:12px}
 .inspector-fields label{min-width:0;width:100%;flex:auto}
 .field-mapper{display:flex;flex-direction:column;gap:8px;border:1px solid var(--fs-border);border-radius:var(--bs-border-radius);padding:10px;background:var(--fs-surface-2)}
 .field-mapper strong{font-size:13px}
 .mapped-edge{display:flex;justify-content:space-between;gap:8px;align-items:center;font-size:12px;overflow-wrap:anywhere}
 .mapped-edge button{color:var(--fs-text-2);background:var(--fs-surface);border:1px solid var(--fs-border-strong);border-radius:5px;padding:3px 8px}
 .checkbox-row{display:flex!important;flex-direction:row!important;gap:8px;align-items:center}
 .tool-grants,.capability-box{border:1px solid var(--fs-border);border-radius:var(--bs-border-radius);padding:8px 12px 10px;display:flex;flex-direction:column;gap:6px;margin:4px 0;background:var(--fs-surface-2)}
 .tool-grants legend,.capability-box legend{float:none;width:auto;padding:0 4px;margin:0;font-size:.8125rem;font-weight:600}
 .capability-box{border-color:color-mix(in srgb,var(--fs-warn) 45%,var(--fs-border));background:var(--fs-warn-soft)}.capability-box legend{color:var(--fs-warn)}

 .readiness-panel,.capability-summary,.graph-validation,.flow-versions{border:1px solid var(--fs-border);border-radius:var(--bs-border-radius-lg);padding:14px 16px;margin:12px 0;background:var(--fs-surface)}
 .readiness-panel{border-left:3px solid var(--fs-warn)}.readiness-panel.readiness-ok{border-left-color:var(--fs-pos)}
 .readiness-panel header,.graph-validation header,.flow-versions header{display:flex;justify-content:space-between;flex-wrap:wrap;align-items:center;gap:10px}
 .readiness-panel header button{background:var(--fs-surface);color:var(--fs-text-2);border:1px solid var(--fs-border-strong);border-radius:6px;padding:3px 10px;font-size:.8125rem}
 .readiness-panel p{margin:6px 0}.readiness-panel ul{list-style:none;margin:0;padding:0}
 .readiness-panel li{color:var(--fs-warn);overflow-wrap:anywhere;margin:3px 0}.readiness-panel li.check-ok{color:var(--fs-pos)}
 .capability-summary ul{margin:6px 0 0;padding-left:18px}.capability-summary li{overflow-wrap:anywhere}
 .capability-summary button,.graph-validation li button{background:none;border:0;text-decoration:underline;color:var(--fs-accent-text);font:inherit;padding:0;text-align:left}
 .graph-validation ul{padding-left:20px;margin:8px 0 0}.graph-validation li{padding:3px 0;overflow-wrap:anywhere}
 .graph-validation p{margin:6px 0 0}
 .flow-message{border-left:3px solid var(--fs-accent);background:var(--fs-accent-soft);padding:8px 12px;border-radius:0 6px 6px 0;margin:10px 0}
 .flow-actions{padding:12px;border:1px solid var(--fs-border);border-radius:var(--bs-border-radius-lg);background:var(--fs-surface);align-items:center}
 .flow-actions label{flex:0 1 220px;min-width:160px}

 .flow-version-publish{display:flex;flex-wrap:wrap;align-items:end;gap:12px;margin:12px 0}
 .flow-version-publish label{flex:1;min-width:200px}
 .flow-version-list{list-style:none;padding:0;margin:8px 0 0;display:flex;flex-direction:column;gap:6px}
 .flow-version-list li{display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:8px;padding:8px 12px;border:1px solid var(--fs-border);border-radius:var(--bs-border-radius);overflow-wrap:anywhere}
 .flow-version-list li.active{border-color:var(--fs-accent);background:var(--fs-accent-soft)}
 .flow-version-actions{display:flex;flex-wrap:wrap;gap:6px}

 .run-history-heading{display:flex;align-items:flex-end;flex-wrap:wrap;gap:12px;margin-bottom:14px;padding:12px;border:1px solid var(--fs-border);border-radius:var(--bs-border-radius-lg);background:var(--fs-surface)}
 .run-history-heading h3{margin:0 auto 6px 0;flex:1 0 100%}
 .run-history-heading label{flex:1 1 180px}
 .flow-run{padding:12px 14px;margin:0 0 8px;border:1px solid var(--fs-border);border-radius:var(--bs-border-radius-lg);background:var(--fs-surface)}
 .flow-run>summary{cursor:pointer;display:flex;flex-wrap:wrap;gap:6px 8px;align-items:center;font-size:.875rem}
 .flow-run>summary::marker{color:var(--fs-muted)}
 .flow-run>.btn{margin:4px 6px 4px 0}
 .run-diagnostics{display:flex;flex-wrap:wrap;gap:8px 18px;padding:10px 12px;margin:12px 0;border-radius:var(--bs-border-radius);background:var(--fs-surface-2);font-size:12px;color:var(--fs-text-2)}
 .run-error-panel{border-left:3px solid var(--fs-neg);background:var(--fs-neg-soft);padding:12px;margin:12px 0;border-radius:0 6px 6px 0}
 .run-error-panel p{overflow-wrap:anywhere;white-space:pre-wrap;margin:6px 0}
 .run-step-list{display:flex;flex-direction:column;gap:4px;margin:12px 0;padding:8px;border:1px solid var(--fs-border);border-radius:var(--bs-border-radius)}
 .run-step{padding:8px 10px;background:var(--fs-surface-2);border-radius:6px}.run-step summary{cursor:pointer;font-size:.8125rem}
 .step-skipped{opacity:.6}
 .flow-run pre{white-space:pre-wrap;word-break:break-word;max-height:250px;overflow:auto}
 .run-compare{border:1px solid var(--fs-border);border-radius:var(--bs-border-radius);padding:10px;margin:8px 0}
 .run-compare-scroll{overflow-x:auto}
 .run-compare table{border-collapse:collapse;width:100%;min-width:480px;font-size:13px}
 .run-compare th,.run-compare td{text-align:left;padding:5px 8px;border-bottom:1px solid var(--fs-border)}
 .run-compare tr.change-status td,.run-compare tr.change-removed td{color:var(--fs-neg)}
 .run-compare tr.change-input td,.run-compare tr.change-output td,.run-compare tr.change-added td{color:var(--fs-warn)}

 .exec-graph{margin:14px 0;display:grid;gap:10px;padding:14px;border:1px solid var(--fs-border);border-radius:var(--bs-border-radius-lg);background:var(--fs-surface-2)}
 .exec-graph-canvas{overflow:auto;border:1px solid var(--fs-border);border-radius:var(--bs-border-radius);background:radial-gradient(var(--fs-canvas-dot) 1px,transparent 1px) 0 0/20px 20px,var(--fs-canvas-bg)}
 .exec-graph-canvas svg{min-width:0;width:100%;min-height:210px;max-height:520px;height:auto;cursor:default}
 .exec-edge{fill:none;stroke:var(--fs-edge);stroke-width:2;opacity:.7}
 .exec-rect{stroke-width:2;stroke:var(--fs-node-border);fill:var(--fs-node-bg)}
 .exec-rect.tone-ok{fill:var(--fs-pos-soft);stroke:var(--fs-pos)}
 .exec-rect.tone-fail{fill:var(--fs-neg-soft);stroke:var(--fs-neg)}
 .exec-rect.tone-run{fill:var(--fs-warn-soft);stroke:var(--fs-warn)}
 .exec-rect.tone-skip{opacity:.6}
 .exec-rect.selected{stroke:var(--fs-node-selected);stroke-width:4}
 .exec-text{fill:var(--fs-node-text);font-weight:600}.exec-sub{fill:var(--fs-node-sub)}
 .exec-node{cursor:pointer}.exec-node:focus-visible{outline:none}.exec-node:focus-visible rect{stroke:var(--fs-node-selected);stroke-width:4}
 .exec-selected{padding:12px;border-radius:var(--bs-border-radius);border:1px solid var(--fs-border);background:var(--fs-surface)}
 .exec-selected pre{max-height:300px;overflow:auto;white-space:pre-wrap;overflow-wrap:anywhere}
 .exec-error-hint{border-left:3px solid var(--fs-neg);background:var(--fs-neg-soft);padding:8px 12px;border-radius:0 6px 6px 0;margin:8px 0}
 .debug-head{display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:8px}
 .debug-nav{display:flex;flex-wrap:wrap;align-items:center;gap:8px}
 .debug-meta{display:grid;grid-template-columns:max-content 1fr;gap:4px 12px;margin:8px 0;font-size:.8125rem}.debug-meta dt{color:var(--fs-muted);font-weight:500}.debug-meta dd{margin:0}
 .debug-io{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,280px),1fr));gap:10px}
 .debug-io section{min-width:0}.debug-io h5{margin:4px 0;font-size:.8125rem}.debug-io pre,.exec-error-hint pre{white-space:pre-wrap;overflow-wrap:anywhere;max-height:260px;overflow:auto}
 .tool-calls h5{font-size:.8125rem;margin:6px 0}.tool-calls ul{margin:4px 0 0;padding-left:18px;font-size:13px;overflow-wrap:anywhere}
 .tool-calls li.tool-failed{color:var(--fs-neg)}.tool-calls .tool-sources{display:block;color:var(--fs-muted);font-size:12px}

 @media(max-width:850px){.blueprint-layout .flow-workspace{min-height:280px}.node-full-tooltip{width:calc(100% - 16px);right:8px}}
 `]
})
export class FlowStudioComponent {
 readonly auth=inject(AuthState);
 private readonly notify = inject(NotifyService);
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
  /** Declared pins of the function a node calls (empty for other nodes and legacy functions). */
  functionPins(n:FlowNode){return n.type==='function'?declaredPins(this.functions().find(f=>f.id===n.function_id)?.graph):{inputs:[],outputs:[]}}
  /** Vertical position of a wire end on a node; typed pins are spread along its edge. */
  pinY(n:FlowNode,side:'in'|'out',port?:string):number{
    const prefix=side==='in'?'in:':'out:';
    if(n.type==='function'&&port?.startsWith(prefix)){
      const pins=side==='in'?this.functionPins(n).inputs:this.functionPins(n).outputs;
      const i=pins.findIndex(p=>p.name===port.slice(prefix.length));
      if(i>=0)return pinOffset(i,pins.length,70);
    }
    return 35;
  }
  readonly pinOffset=pinOffset;
  readonly edgeKey=edgeKey;
  /** Wire being dragged from an output port; `target` is the input port it would snap to. */
  readonly wireDrag=signal<{source:PortPoint;x:number;y:number;target:PortPoint|null}|null>(null);
  private suppressPortClick=false;
  private ports():PortPoint[]{return portPoints(this.graph().nodes,n=>this.functionPins(n as FlowNode))}
  wireGhostPath():string{const w=this.wireDrag();return w?wirePath(w.source.x,w.source.y,w.target?.x??w.x,w.target?.y??w.y):''}
  onOutputPortClick(id:string):void{if(this.suppressPortClick)return;this.chooseOutputPort(id)}
  onInputPortClick(id:string):void{if(this.suppressPortClick)return;this.chooseInputPort(id)}
  /** Starts dragging a wire from an output port (or a typed output pin); dropping on an input port connects them. */
  startWire(ev:PointerEvent,nodeId:string,pin?:string):void{
    ev.stopPropagation();
    if(ev.button!==0)return;
    ev.preventDefault();
    const source=this.ports().find(p=>p.side==='out'&&p.nodeId===nodeId&&p.pin===pin);
    const svg=(ev.currentTarget as SVGElement).ownerSVGElement;
    if(!source||!svg)return;
    const at=(e:PointerEvent)=>{const m=svg.getScreenCTM();if(!m)return {x:source.x,y:source.y};const p=new DOMPoint(e.clientX,e.clientY).matrixTransform(m.inverse());return {x:p.x,y:p.y}};
    let moved=false;const origin=at(ev);
    const move=(e:PointerEvent)=>{
      const p=at(e);if(Math.hypot(p.x-origin.x,p.y-origin.y)>4)moved=true;if(!moved)return;
      this.wireDrag.set({source,x:p.x,y:p.y,target:nearestPort(this.ports(),p.x,p.y,'in')});
    };
    const stop=(e:PointerEvent)=>{
      svg.removeEventListener('pointermove',move);svg.removeEventListener('pointerup',stop);svg.removeEventListener('pointercancel',stop);
      const target=e.type==='pointerup'&&moved?this.wireDrag()?.target??null:null;
      this.wireDrag.set(null);
      if(!moved)return;
      this.suppressPortClick=true;setTimeout(()=>this.suppressPortClick=false,0);
      if(!target)return;
      const result=wireFromDrag(this.graph().edges,source,target);
      if('problem' in result){this.message.set(result.problem);return}
      this.graph.update(g=>({...g,version:source.pin||target.pin?2:g.version,edges:result.edges}));
      this.chosen.set(target.nodeId);this.cancelConnecting();
      if(!this.warnAboutInputs(target.nodeId))this.message.set('Connected '+(this.node(source.nodeId)?.label||source.nodeId)+(source.pin?'.'+source.pin:'')+' → '+(this.node(target.nodeId)?.label||target.nodeId)+(target.pin?'.'+target.pin:'')+'; save the flow to persist.');
    };
    svg.addEventListener('pointermove',move);svg.addEventListener('pointerup',stop);svg.addEventListener('pointercancel',stop);
  }
  pinSources(n:FlowNode):Record<string,string>{return pinBindings(this.graph().edges,n.id)}
  bindPin(pin:string,source:string){this.graph.update(g=>({...g,version:2,edges:bindInputPin(g.edges,this.chosen(),pin,source)}))}
  routePin(pin:string,target:string){this.graph.update(g=>({...g,version:2,edges:routeOutputPin(g.edges,this.chosen(),pin,target)}))}
  pinTargets(n:FlowNode,pin:string):string[]{return this.graph().edges.filter(e=>e.from===n.id&&e.from_port==='out:'+pin).map(e=>e.to)}
  edgePath(e:FlowEdge):string{
    const a=this.node(e.from),b=this.node(e.to);if(!a||!b)return '';
    const x1=a.x+125,y1=a.y+this.pinY(a,'out',e.from_port),x2=b.x,y2=b.y+this.pinY(b,'in',e.to_port),d=Math.max(45,Math.abs(x2-x1)*0.45);
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
  readonly nodeTypes=[{"id":0,"label":"Agent","admin":false},{"id":1,"label":"If / Else","admin":false},{"id":2,"label":"Merge","admin":false},{"id":3,"label":"JSON Field","admin":false},{"id":4,"label":"Loop","admin":false},{"id":5,"label":"HTTP JSON/XML","admin":false},{"id":6,"label":"Web crawler","admin":false},{"id":7,"label":"Web search","admin":false},{"id":8,"label":"Output","admin":false},{"id":9,"label":"Call Flow","admin":true},{"id":10,"label":"Value","admin":false},{"id":11,"label":"Function","admin":true},{"id":12,"label":"Save Result","admin":true},{"id":13,"label":"Load Result","admin":true},{"id":14,"label":"Transform","admin":false},{"id":15,"label":"Read File","admin":true},{"id":16,"label":"Write File","admin":true},{"id":17,"label":"Emit Event","admin":true},...INVESTOR_NODES.map((n,i)=>({id:100+i,label:n.label,admin:n.admin,investor:true}))];
  readonly visibleNodeTypes=()=>this.nodeTypes.filter(n=>(!n.admin||this.auth.user()?.role==='SYS_ADMIN')&&n.label.toLowerCase().includes(this.nodePickerQuery().trim().toLowerCase()));
  toggleNodePicker(){this.nodePickerOpen.update(v=>!v);this.nodePickerQuery.set('')}
  addPickedNode(id:number){
    if(!this.visibleNodeTypes().some(n=>n.id===id))return;
    switch(id){case 0:this.addAgent();break;case 1:this.addV2Node('condition');break;case 2:this.addV2Node('merge');break;case 3:this.addV2Node('json_get');break;case 4:this.addPureLoop();break;case 5:this.addHTTPGet();break;case 6:this.addWebCrawler();break;case 7:this.addWebSearch();break;case 8:this.addOutput();break;case 9:this.addSubflow();break;case 10:this.addConstant();break;case 11:this.addFunctionCall();break;case 12:this.addUtility('save_result');break;case 13:this.addUtility('load_result');break;case 14:this.addUtility('transform');break;case 15:this.addUtility('workspace_read');break;case 16:this.addUtility('workspace_write');break;case 17:this.addEmitEvent();break;default:{const spec=INVESTOR_NODES[id-100];if(!spec)return;this.addInvestorNode(spec.type);}}
    this.nodePickerOpen.set(false);
  }
  readonly studioTab=signal<'overview'|'editor'|'library'|'runs'|'files'|'approvals'>('overview');
  readonly flowSearch=signal('');readonly flowFilter=signal('all');readonly flowSort=signal('name');
  readonly listedFlows=()=>{
    return listOverviewFlows(this.flows(),this.flowSearch(),this.flowFilter(),this.flowSort());
  };
  enabledFlowCount(){return this.flows().filter(f=>f.enabled).length}
  scheduledFlowCount(){return this.flows().filter(f=>f.interval_minutes>0).length}
  clearFlowFilters(){this.flowSearch.set('');this.flowFilter.set('all');this.flowSort.set('name')}
  startFlowFromOverview(){if(!this.allowDiscard())return;this.resetFlow();this.studioTab.set('editor')}
  openFlowFromOverview(id:number){this.loadFlow(id);if(this.selectedId()===id)this.studioTab.set('editor')}
  openHistoryForFlow(id:number){this.selectHistoryFlow(id);this.studioTab.set('runs')}
  /** Opens a deep link from the Flows page: ?template=, ?flow= (with tab=runs for history) or ?tab=. */
  async openDeepLink(link:{flow?:number;tab?:string;template?:string}):Promise<void>{
    if(link.template){this.applyFlowTemplate(link.template);this.studioTab.set('editor');return}
    if(link.flow){
      await this.refresh();
      if(link.tab==='runs')this.openHistoryForFlow(link.flow);
      else this.openFlowFromOverview(link.flow);
      return;
    }
    const tabs=['overview','editor','library','runs','approvals'] as const;
    const tab=tabs.find(t=>t===link.tab);
    if(tab&&(tab!=='library'||this.auth.user()?.role==='SYS_ADMIN'))this.studioTab.set(tab);
  }
  async runSavedFlow(id:number){
    if(this.busy()||!this.flows().some(f=>f.id===id))return;
    this.busy.set(true);
    try{await this.request('/api/admin/flows/'+id+'/run',{method:'POST'});this.openHistoryForFlow(id);this.message.set('Run queued; check History for results.');setTimeout(()=>{if(this.historyFlowId()===id)void this.refreshRuns()},3000)}
    catch(e){this.message.set('Could not start run: '+String(e))}finally{this.busy.set(false)}
  }
 readonly flows=signal<SavedFlow[]>([]);
 readonly availableModels=signal<{model:string;label:string}[]>([]);
 readonly defaultAIModel=signal('');
 readonly functions=signal<(FunctionDef&PublishState)[]>([]);
 readonly agentTools=signal<GrantableTool[]>([]);
 readonly toolLimits=signal({max_tools:8,max_tool_calls:16,default_tool_calls:6});
 readonly toolLabel=toolLabel;
 readonly describeToolCall=describeToolCall;
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
   this.redoHistory.update(items=>pushHistorySnapshot(items,current));
   this.lastGraphVersion=previous;
   this.graph.set(JSON.parse(previous) as FlowGraph);
   this.chosen.set('');this.closeQuickNode();this.cancelConnecting();
 }
 redoGraph():void{
   const history=this.redoHistory();if(!history.length||this.busy())return;
   const next=history[history.length-1],current=JSON.stringify(this.graph());
   this.redoHistory.set(history.slice(0,-1));
   this.undoHistory.update(items=>pushHistorySnapshot(items,current));
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
      if(n.type==='emit_event'&&!n.event)add('Choose the event to emit: '+n.label,n.id);
      else if(n.type==='emit_event'&&this.auth.user()?.role==='SYS_ADMIN'&&!this.triggers().some(t=>t.event_key===n.event))add('Event '+n.event+' is not defined; create it under Function library & triggers: '+n.label,n.id);
      if(n.type==='json_get'&&!/^[A-Za-z][A-Za-z0-9_]{0,47}$/.test(n.property||''))add('Enter a valid JSON property: '+n.label,n.id);
      if(n.type==='condition'&&!['equals','not_equals','contains','greater_than','less_than','is_empty','not_empty'].includes(n.operator||''))add('Select a condition operator: '+n.label,n.id);
      if(isInvestorNode(n.type)){for(const problem of this.investorProblems(n))add(problem+': '+n.label,n.id)}
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
        }else if(e.to_port?.startsWith('in:')||e.from_port?.startsWith('out:')){
          const problem=pinEdgeProblem(e,from.type,to.type,this.functionPins(from).outputs,this.functionPins(to).inputs);
          if(problem)add(problem+' ('+from.label+' → '+to.label+')',problem.includes('input')?e.to:e.from);
        }else add('Unsupported connection port combination: '+(e.from_port||'default')+' → '+(e.to_port||'default'),e.from);
      }
      adjacency.get(e.from)?.push(e.to);incoming.set(e.to,(incoming.get(e.to)||0)+1);
    }
    for(const n of g.nodes)if(n.type!=='trigger'&&!incoming.get(n.id))add('Disconnected node: '+n.label,n.id);
    for(const n of g.nodes){
      if(n.type!=='function')continue;
      const pins=this.functionPins(n).inputs;
      if(mixesPinAndPlainInputs(g.edges,n.id))add('Connect every input of this function to a typed pin, not a mix: '+n.label,n.id);
      else if(pins.length>1||(pins.length===1&&pinBindings(g.edges,n.id)[pins[0].name]===undefined&&g.edges.some(e=>e.to===n.id&&e.to_port?.startsWith('in:')))){
        for(const pin of unboundInputPins(g.edges,n.id,pins))add('Connect input pin "'+pin.name+'" ('+pin.type+'): '+n.label,n.id);
      }
    }
    const visited=new Set<string>(),active=new Set<string>();let cycle=false;
    const dfs=(id:string):void=>{
      if(active.has(id)){cycle=true;return}
      if(visited.has(id))return;
      visited.add(id);active.add(id);for(const to of adjacency.get(id)||[])dfs(to);active.delete(id);
    };
    if(triggers.length===1)dfs(triggers[0].id);
    if(cycle)add('Graph contains a cycle.');
    for(const n of g.nodes)if(!visited.has(n.id))add('Unreachable node: '+n.label,n.id);
    for(const issue of semanticIssues(g.version,g.nodes,g.edges,n=>this.functionPins(n as FlowNode)))add(issue.message,issue.nodeId);
    return issues;
  };
  openValidationIssue(id:string):void{if(this.node(id))this.openQuickNode(id)}
  readonly historyLoading=signal(false);
  private historyRequestVersion=0;
  readonly historyStatusFilter=signal('all');
  readonly historySearch=signal('');
  readonly filteredRuns=()=>filterFlowRuns(this.runs(),this.historyStatusFilter(),this.historySearch());
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
 displayStepDuration(ms:number|null|undefined):string{
   if(ms==null)return 'Unavailable';
   const value=Math.max(0,ms);
   return value<1000?value+' ms':value<60000?(value/1000).toFixed(1)+' s':Math.floor(value/60000)+' min '+Math.round(value%60000/1000)+' s';
 }
 displayRunDuration(run:FlowRun):string{
   if(run.status==='QUEUED')return 'Queued';
   if(run.duration_ms==null)return run.status==='RUNNING'?'In progress':'Unavailable';
   const ms=Math.max(0,run.duration_ms);
   return ms<1000?ms+' ms':ms<60000?(ms/1000).toFixed(1)+' s':Math.floor(ms/60000)+' min '+Math.round(ms%60000/1000)+' s';
 }
 suspectedFailedNode(run:FlowRun):string{
   if(run.status!=='FAILED'||!run.error||run.failed_node||!this.executionGraph())return '';
   // Runs from before structured failed-node attribution only carry the message.
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
 /** Status tone of an executed node, used as its canvas class. */
 executionNodeColor(id:string):string{switch(this.executionNodeStatus(id)){case 'SUCCESS':return 'ok';case 'FAILED':return 'fail';case 'RUNNING':return 'run';case 'SKIPPED':return 'skip';default:return 'none'}}
 executionNodeLabel(id:string):string{return this.executionGraph()?.nodes.find(n=>n.id===id)?.label||id}
 executionViewBox():string{const n=this.executionGraph()?.nodes||[];if(!n.length)return '0 0 820 380';const l=Math.min(...n.map(x=>x.x))-60,t=Math.min(...n.map(x=>x.y))-60,r=Math.max(...n.map(x=>x.x+125))+60,b=Math.max(...n.map(x=>x.y+70))+60;return l+' '+t+' '+(r-l)+' '+(b-t)}
 executionEdgePath(e:FlowEdge,g:FlowGraph):string{const a=g.nodes.find(n=>n.id===e.from),b=g.nodes.find(n=>n.id===e.to);if(!a||!b)return '';const x=a.x+125,y=a.y+35,tx=b.x,ty=b.y+35,d=Math.max(45,Math.abs(tx-x)*.45);return 'M '+x+' '+y+' C '+(x+d)+' '+y+', '+(tx-d)+' '+ty+', '+tx+' '+ty}
 readonly traceLoading=signal(false);
 readonly replaySteps=signal<{node:string;label:string;status:string;input?:string;output?:string}[]>([]);
 readonly replaySource=signal(0);
 readonly compareRun=signal(0);
 readonly comparedSteps=signal<FlowRunStep[]>([]);
 readonly busy=signal(false);
 readonly message=signal('');
 private dragging:{id:string;startX:number;startY:number;x:number;y:number}|null=null;
 constructor(){
  // Load the revision list of the selected call node's target for its pin selector.
  effect(()=>{const n=this.selectedNode();if(n?.type==='flow'&&n.flow_id)void this.loadTargetRevisions('flow',n.flow_id);if(n?.type==='function'&&n.function_id)void this.loadTargetRevisions('function',n.function_id)});
  this.resetGraphHistory();this.savedSnapshot.set(this.currentSnapshot());void this.refresh();if(this.auth.user()?.role==='SYS_ADMIN'){void this.loadFunctions();void this.loadAgentTools()}void this.loadModels();void this.loadProviders();
    effect(()=>{
      const current=JSON.stringify(this.graph());
      if(current===this.lastGraphVersion||this.dragHistoryStart!==null)return;
      if(this.lastGraphVersion)this.undoHistory.update(items=>pushHistorySnapshot(items,this.lastGraphVersion));
      this.lastGraphVersion=current;this.redoHistory.set([]);
    });
    effect((onCleanup)=>{
      if(this.studioTab()!=='runs'||!this.historyFlowId()||!this.runs().some(run=>isActiveRun(run.status)))return;
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
 readonly playbooks=INVESTOR_PLAYBOOKS;
 readonly isInvestorNode=isInvestorNode;
 readonly investorNode=investorNode;
 readonly providers=signal<{id:string;label:string}[]>([]);
 investorTag(type:string):string{return investorNode(type)?.tag||'✦ AGENT'}
 async loadProviders():Promise<void>{try{const r=await fetch('/api/providers',{credentials:'same-origin'});if(r.ok){const data=await r.json();this.providers.set((Array.isArray(data)?data:data.providers||[]).filter((p:{enabled?:boolean})=>p.enabled!==false))}}catch{}}
 addInvestorNode(type:string):void{
   const spec=investorNode(type);if(!spec)return;
   const count=this.graph().nodes.length;
   const id=type.replace(/_/g,'-')+'-'+Date.now().toString(36);
   this.graph.update(g=>({...g,version:2,nodes:[...g.nodes,{id,type:spec.type,label:spec.label,...spec.defaults,x:260+(count%4)*140,y:40+Math.floor(count/4)*100} as FlowNode]}));
   this.chosen.set(id);
 }
 updateInvestor(id:string,field:'symbols'|'universe'|'strategy'|'provider'|'rule'|'sort_by'|'columns'|'title'|'key_fields'|'forms'|'channel'|'subject'|'skip_empty',value:string|boolean):void{
   this.graph.update(g=>({...g,nodes:g.nodes.map(n=>{if(n.id!==id)return n;const next:FlowNode={...n};if(value===''||value===false)delete (next as unknown as Record<string,unknown>)[field];else (next as unknown as Record<string,unknown>)[field]=value;return next})}));
 }
 updateInvestorNumber(id:string,field:'limit'|'lookback_days'|'cooldown_hours',value:number,min:number,max:number):void{
   const safe=Number.isFinite(value)?Math.max(min,Math.min(max,Math.trunc(value))):min;
   this.graph.update(g=>({...g,nodes:g.nodes.map(n=>{if(n.id!==id)return n;const next:FlowNode={...n};if(!safe)delete next[field];else next[field]=safe;return next})}));
 }
 readonly leaderboardUniverses=LEADERBOARD_UNIVERSES;
 readonly leaderboardStrategies=LEADERBOARD_STRATEGIES;
 /** A preset universe replaces the node's own watchlist. */
 setLeaderboardUniverse(id:string,universe:string):void{this.updateInvestor(id,'universe',universe);if(universe)this.updateInvestor(id,'symbols','')}
 /** The node feeding a screen, used to suggest field names. */
 private screenSource(id:string):FlowNode|undefined{const e=this.graph().edges.find(x=>x.to===id);return e?this.node(e.from):undefined}
 screenFields(id:string):string[]{
   let source=this.screenSource(id);
   for(let hops=0;source&&hops<4&&!SCREEN_FIELDS[source.type];hops++)source=this.screenSource(source.id);
   return source?SCREEN_FIELDS[source.type]||[]:[];
 }
 screenSourceLabel(id:string):string{return this.screenSource(id)?.label||''}
 appendRuleField(id:string,field:string):void{const rule=(this.node(id)?.rule||'').trim();this.updateInvestor(id,'rule',rule?rule+' and '+field+' ':field+' ')}
 investorProblems(n:FlowNode):string[]{
   const problems:string[]=[];
   if(this.graph().version!==2)problems.push('Investor nodes need a version 2 graph');
   const symbolsProblem=watchlistProblem(n.symbols||'');if(symbolsProblem)problems.push(symbolsProblem);
   if(n.type==='screen'&&(n.rule||'').length>500)problems.push('Screen rule is longer than 500 characters');
   if(n.type==='notify'&&this.auth.user()?.role!=='SYS_ADMIN')problems.push('Only administrators can add Notify nodes');
   return problems;
 }
 applyFlowTemplate(id:string):void{
  if(!id||!this.allowDiscard())return;
  this.resetFlow();
  const playbook=buildPlaybook(id,{admin:this.auth.user()?.role==='SYS_ADMIN'});
  if(playbook){
   this.name.set(playbook.name);
   this.graph.set(playbook.graph as FlowGraph);
   this.enabled.set(false);
   // The schedule is a suggestion; only administrators can automate flows.
   this.interval.set(this.auth.user()?.role==='SYS_ADMIN'?playbook.intervalMinutes:0);
   this.resetGraphHistory();
   this.message.set('Playbook copied as a new, disabled flow. Set your watchlist on the data node, save, run it once, then publish and enable it. '+playbook.note);
   return;
  }
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
 private resetFlow(){this.selectedId.set(0);this.name.set('New research flow');this.interval.set(0);this.eventKey.set('');this.enabled.set(false);this.graph.set(this.defaultGraph());this.resetGraphHistory();this.message.set('');this.savedSnapshot.set(this.currentSnapshot());this.revisions.set([])}
 loadFlow(id:number){if(!this.allowDiscard())return;this.cancelConnecting();this.resetCanvas();const f=this.flows().find(x=>x.id===id);if(!f){this.resetFlow();return}
  this.selectedId.set(f.id);this.name.set(f.name);this.interval.set(f.interval_minutes);this.eventKey.set(f.event_key||'');this.enabled.set(f.enabled);this.graph.set(structuredClone(f.graph));this.resetGraphHistory();this.fitCanvas();this.chosen.set('');this.savedSnapshot.set(this.currentSnapshot());this.publishNote.set('');void this.loadRevisions()}
 async loadFunctions(){try{const data=await this.request("/api/admin/blueprint-functions");this.functions.set(data.functions||[])}catch(e){this.message.set(String(e))}}
 readonly publishStatusLabel=publishStatusLabel;
 readonly canPublish=canPublish;
 readonly runRevisionLabel=runRevisionLabel;
 readonly revisionOptionLabel=revisionOptionLabel;
 readonly pinDescription=pinDescription;
 readonly currentFlow=()=>this.flows().find(f=>f.id===this.selectedId());
 readonly revisions=signal<RevisionSummary[]>([]);
 readonly publishNote=signal('');
 /** Revisions of call targets, keyed "flow:<id>" or "function:<id>", for pin selectors. */
 readonly targetRevisions=signal<Record<string,RevisionSummary[]>>({});
 async loadRevisions(){
  const id=this.selectedId();
  if(!id){this.revisions.set([]);return}
  void this.loadReadiness();
  try{const data=await this.request('/api/admin/flows/'+id+'/revisions');if(this.selectedId()===id)this.revisions.set(data.revisions||[])}
  catch(e){this.revisions.set([]);this.message.set(String(e))}
 }
 /** Production-readiness gate of the selected flow's published revision. */
 readonly readiness=signal<Readiness|null>(null);
 readonly readinessHeadline=readinessHeadline;
 readonly automated=()=>this.enabled()&&(this.interval()>0||!!this.eventKey());
 async loadReadiness(){
  const id=this.selectedId();
  if(!id){this.readiness.set(null);return}
  try{const r=await this.request('/api/admin/flows/'+id+'/readiness');if(this.selectedId()===id)this.readiness.set(r)}
  catch{this.readiness.set(null)}
 }
 /** Publishing or activating an untried revision pauses automation; keep the editor in step. */
 private notePausedAutomation(data:any):boolean{
  if(!data?.automation_paused)return false;
  this.enabled.set(false);this.savedSnapshot.set(this.currentSnapshot());
  this.message.set('Revision '+data.active_revision+' is active, and automation is paused until it passes the readiness checks: '+(data.readiness?.checks||[]).filter((c:any)=>!c.ok).map((c:any)=>c.message).join(' '));
  return true;
 }
 async loadTargetRevisions(kind:'flow'|'function',id:number){
  if(!id)return;
  const url=kind==='flow'?'/api/admin/flows/'+id+'/revisions':'/api/admin/blueprint-functions/'+id+'/revisions';
  try{const data=await this.request(url);this.targetRevisions.update(m=>({...m,[kind+':'+id]:data.revisions||[]}))}
  catch{this.targetRevisions.update(m=>({...m,[kind+':'+id]:[]}))}
 }
 updateRevisionPin(field:'flow_revision'|'function_revision',revision:number){
  this.graph.update(g=>({...g,nodes:g.nodes.map(n=>{
   if(n.id!==this.chosen())return n;
   const next={...n};
   if(revision>0)next[field]=revision;else delete next[field];
   return next;
  })}));
 }
 async publish(){
  const id=this.selectedId();
  if(!id||this.hasUnsavedChanges()){this.message.set('Save the draft before publishing.');return}
  this.busy.set(true);
  try{const data=await this.request('/api/admin/flows/'+id+'/publish',{method:'POST',body:JSON.stringify({note:this.publishNote().trim()})});this.publishNote.set('');if(!this.notePausedAutomation(data))this.message.set('Published revision '+data.active_revision+'; runs now use it.');await this.refresh();await this.loadRevisions()}
  catch(e){this.message.set(String(e))}finally{this.busy.set(false)}
 }
 async activateRevision(revision:number){
  const id=this.selectedId();
  if(!id||!confirm('Make revision '+revision+' active? Scheduled, event and manual runs will use it.'))return;
  this.busy.set(true);
  try{const data=await this.request('/api/admin/flows/'+id+'/revisions/'+revision+'/activate',{method:'POST'});if(!this.notePausedAutomation(data))this.message.set('Revision '+revision+' is now active.');await this.refresh();await this.loadRevisions()}
  catch(e){this.message.set(String(e))}finally{this.busy.set(false)}
 }
 async restoreRevision(revision:number){
  const id=this.selectedId();
  if(!id||!this.allowDiscard())return;
  if(!confirm('Replace the draft with revision '+revision+'? The active revision does not change.'))return;
  this.busy.set(true);
  try{const flow:SavedFlow=await this.request('/api/admin/flows/'+id+'/revisions/'+revision+'/restore',{method:'POST'});
   this.flows.update(list=>list.map(f=>f.id===id?flow:f));
   this.savedSnapshot.set(this.currentSnapshot());this.loadFlow(id);await this.loadRevisions();
   this.message.set('Draft restored from revision '+revision+'. Publish it to make it the next revision.')}
  catch(e){this.message.set(String(e))}finally{this.busy.set(false)}
 }
 readonly maxEventDepth=3;
 addEmitEvent():void {
  const count=this.graph().nodes.length;
  const id='emit-'+Date.now().toString(36);
  const event=this.triggers()[0]?.event_key||'';
  this.graph.update(g=>({...g,version:2,nodes:[...g.nodes,{id,type:'emit_event',label:'Emit event',event,x:260+(count%4)*140,y:40+Math.floor(count/4)*100}]}));
  this.chosen.set(id);
 }
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
 readonly nodeCapability=nodeCapability;
 readonly describeCapability=describeCapability;
 readonly sideEffects=()=>capabilityManifest(this.graph().nodes);
 setApproval(required:boolean){this.graph.update(g=>({...g,nodes:g.nodes.map(n=>{if(n.id!==this.chosen())return n;const {approval,...rest}=n;return required?{...rest,approval:'required' as const}:rest})}))}
 updateUtility(field:'variable'|'transform'|'value'|'content_type'|'value_type'|'event',value:string){
   this.graph.update(g=>({...g,nodes:g.nodes.map(n=>n.id===this.chosen()?{...n,[field]:value}:n)}));
 }
 formattedRunJSON(raw:string):string{
   try{return JSON.stringify(JSON.parse(raw),null,2)}catch{return raw}
 }
 addFunctionCall(){const n=this.graph().nodes;const id="fn-"+Date.now().toString(36);this.graph.update(g=>({...g,nodes:[...g.nodes,{id,type:"function",label:"Call function",function_id:0,x:260+(n.length%4)*140,y:40+Math.floor(n.length/4)*100}]}));this.chosen.set(id)}
 updateFunctionReference(target:number){this.graph.update(g=>({...g,nodes:g.nodes.map(n=>{if(n.id!==this.chosen())return n;const next={...n,function_id:target};delete next.function_revision;return next})}))}
 addConstant(){const n=this.graph().nodes;const id='value-'+Date.now().toString(36);this.graph.update(g=>({...g,nodes:[...g.nodes,{id,type:'constant',label:'Value',value_type:'text',value:'Research this signal',x:260+(n.length%4)*140,y:40+Math.floor(n.length/4)*100}]}));this.chosen.set(id)}
 updateConstant(field:'value_type'|'value',value:string){this.graph.update(g=>({...g,nodes:g.nodes.map(n=>n.id===this.chosen()?{...n,[field]:value}:n)}))}
 connectValue(from:string,to:string){if(!to||this.node(from)?.value_type!=='text'){this.message.set('Typed link requires a text value and an agent or flow input');return}if(this.graph().edges.some(e=>e.from===from&&e.to===to))return;this.graph.update(g=>({...g,edges:[...g.edges,{from,to,from_port:'value',to_port:'input'}]}))}
 addSubflow(){const n=this.graph().nodes;const id='flow-'+Date.now().toString(36);this.graph.update(g=>({...g,nodes:[...g.nodes,{id,type:'flow',label:'Call flow',flow_id:0,x:260+(n.length%4)*140,y:40+Math.floor(n.length/4)*100}]}));this.chosen.set(id)}
 updateFlowReference(target:number){this.graph.update(g=>({...g,nodes:g.nodes.map(n=>{if(n.id!==this.chosen())return n;const next={...n,flow_id:target};delete next.flow_revision;return next})}))}
 async loadAgentTools(){try{const data=await this.request('/api/admin/flow-agent-tools');this.agentTools.set(data.tools||[]);this.toolLimits.set({max_tools:data.max_tools,max_tool_calls:data.max_tool_calls,default_tool_calls:data.default_tool_calls})}catch{/* Tool grants stay hidden when the catalogue is unavailable. */}}
 toggleAgentTool(id:string,name:string){
   const n=this.node(id);const next=toggleTool(n?.tools,name,this.toolLimits().max_tools);
   if(!next){this.message.set('An agent can be granted at most '+this.toolLimits().max_tools+' tools');return}
   this.graph.update(g=>({...g,nodes:g.nodes.map(x=>{if(x.id!==id)return x;const {tools:_t,max_tool_calls:_m,require_grounding:_g,...rest}=x;return next.length?{...rest,tools:next,...(_m?{max_tool_calls:_m}:{}),...(_g?{require_grounding:true}:{})}:{...rest,...(_g?{require_grounding:true}:{})}})}));
 }
 setGrounding(id:string,on:boolean){
   this.graph.update(g=>({...g,nodes:g.nodes.map(x=>{if(x.id!==id)return x;const {require_grounding:_g,...rest}=x;return on?{...rest,require_grounding:true}:rest})}));
 }
 updateToolBudget(id:string,value:number){
   const budget=clampToolBudget(value,this.toolLimits().max_tool_calls);
   this.graph.update(g=>({...g,nodes:g.nodes.map(x=>{if(x.id!==id)return x;const {max_tool_calls:_m,...rest}=x;return budget?{...rest,max_tool_calls:budget}:rest})}));
 }
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
  this.graph.update(g=>({...g,edges:[...g.edges,{from,to}]}));this.warnAboutInputs(to)}
  /** Points out right away when a new connection breaks the branch/merge rules for its target. */
  private warnAboutInputs(nodeId:string):boolean{
    const g=this.graph(),issue=semanticIssues(g.version,g.nodes,g.edges,n=>this.functionPins(n as FlowNode)).find(i=>i.nodeId===nodeId);
    if(issue)this.message.set(issue.message);
    return !!issue;
  }
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
   if(before!==null&&before!==after){this.undoHistory.update(items=>pushHistorySnapshot(items,before));this.redoHistory.set([]);}
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
 readonly isActiveRun=isActiveRun;
 readonly canCancelRun=canCancelRun;
 readonly canRetryRun=canRetryRun;
 readonly runStatusLabel=runStatusLabel;
 readonly runAttemptLabel=runAttemptLabel;
 async retryRun(runId:number){
  if(!this.historyFlowId()||this.busy())return;
  this.busy.set(true);
  try{
   await this.request('/api/admin/flows/'+this.historyFlowId()+'/runs/'+runId+'/retry',{method:'POST'});
   this.message.set('Run #'+runId+' queued again; it resumes from its last checkpoint.');
   await this.refreshRuns();
  }catch(e){this.message.set('Cannot retry run: '+String(e))}
  finally{this.busy.set(false)}
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
 readonly executedNodes=()=>executedNodeOrder(this.traceSteps());
 executedIndex(nodeId:string):number{return this.executedNodes().indexOf(nodeId)}
 stepExecution(delta:number):void{
  const order=this.executedNodes();const index=this.executedIndex(this.executionSelectedNode());
  const next=order[index<0?0:index+delta];if(next)this.executionSelectedNode.set(next);
 }
 readonly stepComparison=()=>compareRunSteps(this.traceSteps(),this.comparedSteps());
 durationDelta(ms:number|null):string{if(ms==null)return '—';if(ms===0)return 'Same';return (ms>0?'+':'−')+this.displayStepDuration(Math.abs(ms))}
 changeLabel(change:string):string{switch(change){case 'unchanged':return 'Unchanged';case 'status':return 'Status changed';case 'input':return 'Input changed';case 'output':return 'Output changed';case 'added':return 'Only in this run';case 'removed':return 'Only in baseline'}return change}
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
   const confirmed=await this.notify.confirm({title:'Delete flow?',message:'Delete “'+name+'” and its execution history? This cannot be undone.',confirmLabel:'Delete flow',danger:true});
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
   this.selectedId.set(flow.id);this.historyFlowId.set(flow.id);this.savedSnapshot.set(this.currentSnapshot());this.message.set(flow.active_revision?'Draft saved; publish it to change what runs.':'Flow saved');await this.refresh();await this.loadRevisions()}catch(e){this.message.set(String(e));void this.loadReadiness()}finally{this.busy.set(false)}}
 async run(target:'active'|'draft'='active'){
   const issues=this.graphIssues();
   if(issues.length){this.message.set('Resolve '+issues.length+' graph issue(s) before running.');return}
   if(this.hasUnsavedChanges()){this.message.set('Save current edits before running.');return}
   this.busy.set(true);try{const queued=await this.request('/api/admin/flows/'+this.selectedId()+'/run',{method:'POST',body:JSON.stringify({target})});this.historyFlowId.set(this.selectedId());this.message.set('Agent run queued ('+runRevisionLabel(queued.flow_revision)+'); open Run history to see its progress');setTimeout(()=>void this.refreshRuns(),3000)}catch(e){this.message.set(String(e))}finally{this.busy.set(false)}}
 async emitEvent(){this.busy.set(true);try{const definition=this.triggers().find(t=>t.event_key===this.eventKey());if(!definition)throw Error('Create the custom trigger first');let value:string|number|boolean=this.triggerValue();if(definition.payload_type==='number'){value=Number(value);if(!Number.isFinite(value))throw Error('Event requires a finite number')}if(definition.payload_type==='boolean'){if(value!=='true'&&value!=='false')throw Error('Use true or false for a boolean event');value=value==='true'}const data=await this.request('/api/admin/flow-events/'+encodeURIComponent(this.eventKey()),{method:'POST',body:JSON.stringify({value})});this.message.set(data.started+' flow(s) triggered');}catch(e){this.message.set(String(e))}finally{this.busy.set(false)}}
}
