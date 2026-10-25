import { Component, input, output } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import {
  MAX_PINS, Pin, PinType, V2Edge, V2Graph, V2Node, WireKind, connectWire, inputPorts, outputPorts, pinProblems, removePin, renamePin,
} from './function-pins';

const OPERATIONS = ['add', 'subtract', 'multiply', 'divide', 'greater', 'less', 'equal', 'and', 'or', 'not', 'if', 'to_text'];

/**
 * Form-based editor for Blueprint functions with declared typed pins
 * (graph version 2): any number of typed inputs and outputs, internal
 * nodes, and data/execution wires between named ports.
 */
@Component({
  selector: 'fse-typed-function-editor',
  standalone: true,
  imports: [FormsModule, MatButtonModule],
  template: `
  <section class="typed" aria-label="Typed function editor">
   <div class="pins">
    <fieldset>
     <legend>Input pins</legend>
     @for(p of graph().inputs; track $index){
      <div class="row">
       <input [ngModel]="p.name" (change)="renameInput(p.name, $any($event.target).value)" aria-label="Input pin name" maxlength="48">
       <select [ngModel]="p.type" (ngModelChange)="retype('input', p.name, $event)" aria-label="Input pin type"><option value="number">number</option><option value="text">text</option><option value="boolean">boolean</option></select>
       <button type="button" mat-stroked-button (click)="drop('input', p.name)" [disabled]="graph().inputs.length<=1" [attr.aria-label]="'Remove input '+p.name">Remove</button>
      </div>
     }
     <button type="button" mat-stroked-button (click)="addPin('input')" [disabled]="graph().inputs.length>=maxPins">+ Input pin</button>
    </fieldset>
    <fieldset>
     <legend>Output pins</legend>
     @for(p of graph().outputs; track $index){
      <div class="row">
       <input [ngModel]="p.name" (change)="renameOutput(p.name, $any($event.target).value)" aria-label="Output pin name" maxlength="48">
       <select [ngModel]="p.type" (ngModelChange)="retype('output', p.name, $event)" aria-label="Output pin type"><option value="number">number</option><option value="text">text</option><option value="boolean">boolean</option></select>
       <button type="button" mat-stroked-button (click)="drop('output', p.name)" [disabled]="graph().outputs.length<=1" [attr.aria-label]="'Remove output '+p.name">Remove</button>
      </div>
     }
     <button type="button" mat-stroked-button (click)="addPin('output')" [disabled]="graph().outputs.length>=maxPins">+ Output pin</button>
    </fieldset>
   </div>
   @for(problem of problems(); track problem){<p class="problem" role="alert">{{problem}}</p>}

   <h4>Internal nodes</h4>
   <div class="row">
    <button type="button" mat-stroked-button (click)="addNode('constant')">+ Value</button>
    <button type="button" mat-stroked-button (click)="addNode('operation')">+ Operation</button>
    <button type="button" mat-stroked-button (click)="addNode('branch')">+ Branch</button>
    <button type="button" mat-stroked-button (click)="addNode('output')" title="A second exit; each exit must connect every output pin">+ Extra exit</button>
   </div>
   <ul class="nodes">
    @for(n of graph().nodes; track n.id){
     <li>
      <strong>{{n.id}}</strong> <span class="muted">{{n.type}}</span>
      @if(n.type==='constant'){
       <select [ngModel]="n.value_type||'number'" (ngModelChange)="patch(n.id,{value_type:$event})" aria-label="Value type"><option value="number">number</option><option value="text">text</option><option value="boolean">boolean</option></select>
       <input [ngModel]="n.value||''" (ngModelChange)="patch(n.id,{value:$event})" aria-label="Constant value" maxlength="200">
      }
      @if(n.type==='operation'){
       <select [ngModel]="n.operation||'add'" (ngModelChange)="patch(n.id,{operation:$event})" aria-label="Operation">@for(op of operations; track op){<option [value]="op">{{op}}</option>}</select>
      }
      @if(n.type!=='input' && !(n.type==='output' && outputNodeCount()<=1)){<button type="button" mat-stroked-button (click)="removeNode(n.id)" [attr.aria-label]="'Remove node '+n.id">Remove</button>}
     </li>
    }
   </ul>

   <h4>Wires</h4>
   <p class="muted">Data wires carry values between pins; execution wires set the order (<code>next</code>, or <code>true</code>/<code>false</code> from a branch). Connecting to an input port that already has a wire replaces it.</p>
   <div class="row wire-form">
    <label>Kind <select #kind (change)="resetPorts()"><option value="data">data</option><option value="exec">execution</option></select></label>
    <label>From <select #from>@for(n of sourceNodes(kind.value); track n.id){<option [value]="n.id">{{n.id}}</option>}</select></label>
    <label>Port <select #fromPort>@for(p of sourcePorts(from.value, kind.value); track p.port){<option [value]="p.port">{{p.port}}@if(p.type){ ({{p.type}})}</option>}</select></label>
    <label>To <select #to>@for(n of targetNodes(kind.value); track n.id){<option [value]="n.id">{{n.id}}</option>}</select></label>
    <label>Port <select #toPort>@for(p of targetPorts(to.value, kind.value); track p.port){<option [value]="p.port">{{p.port}}@if(p.type){ ({{p.type}})}</option>}</select></label>
    <button type="button" mat-flat-button (click)="wire(kind.value, from.value, fromPort.value, to.value, toPort.value)">Connect</button>
   </div>
   <ul class="nodes">
    @for(e of graph().edges; track $index){
     <li><span class="muted">{{e.kind}}</span> {{e.from}}.{{e.from_port}} → {{e.to}}.{{e.to_port}}
      <button type="button" mat-stroked-button (click)="unwire($index)" aria-label="Remove wire">Remove</button></li>
    }
   </ul>
  </section>`,
  styles: [`
   .typed{display:flex;flex-direction:column;gap:10px}
   .pins{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,320px),1fr));gap:12px}
   fieldset{border:1px solid #50738e;border-radius:8px;padding:10px;display:flex;flex-direction:column;gap:8px;min-width:0}
   .row{display:flex;flex-wrap:wrap;align-items:end;gap:8px}
   .row > input,.row > select{min-width:0;flex:1 1 110px}
   .wire-form select{width:100%;min-width:0}
   .nodes{list-style:none;margin:0;padding:0;display:flex;flex-direction:column;gap:6px}
   .nodes li{display:flex;flex-wrap:wrap;align-items:center;gap:8px;padding:6px 8px;border:1px solid #3d5d77;border-radius:6px;overflow-wrap:anywhere}
   .wire-form label{display:flex;flex-direction:column;gap:3px;font-size:12px;flex:1 1 110px}
   .problem{margin:0;padding:8px;border-left:4px solid #e36a6a;background:#45222a}
   .muted{opacity:.75}h4{margin:6px 0 0}
  `],
})
export class TypedFunctionEditorComponent {
  readonly graph = input.required<V2Graph>();
  readonly graphChange = output<V2Graph>();
  readonly operations = OPERATIONS;
  readonly maxPins = MAX_PINS;

  private set(next: V2Graph): void { this.graphChange.emit(next); }

  problems(): string[] {
    const g = this.graph();
    return [...pinProblems(g.inputs, 'input'), ...pinProblems(g.outputs, 'output')];
  }

  outputNodeCount(): number { return this.graph().nodes.filter(n => n.type === 'output').length; }

  private nextName(base: string, taken: string[]): string {
    for (let i = 1; ; i++) { const name = base + i; if (!taken.includes(name)) return name; }
  }

  addPin(side: 'input' | 'output'): void {
    const g = this.graph();
    const pins = side === 'input' ? g.inputs : g.outputs;
    const pin: Pin = { name: this.nextName(side === 'input' ? 'in' : 'out', pins.map(p => p.name)), type: 'number' };
    this.set(side === 'input' ? { ...g, inputs: [...pins, pin] } : { ...g, outputs: [...pins, pin] });
  }
  renameInput(from: string, to: string): void { this.rename('input', from, to.trim()); }
  renameOutput(from: string, to: string): void { this.rename('output', from, to.trim()); }
  private rename(side: 'input' | 'output', from: string, to: string): void {
    const g = this.graph();
    const taken = (side === 'input' ? g.inputs : g.outputs).some(p => p.name === to);
    if (!to || to === from || taken) { this.set({ ...g }); return; }
    this.set(renamePin(g, side, from, to));
  }
  retype(side: 'input' | 'output', name: string, type: PinType): void {
    const g = this.graph();
    const map = (pins: Pin[]) => pins.map(p => p.name === name ? { ...p, type } : p);
    this.set(side === 'input' ? { ...g, inputs: map(g.inputs) } : { ...g, outputs: map(g.outputs) });
  }
  drop(side: 'input' | 'output', name: string): void { this.set(removePin(this.graph(), side, name)); }

  addNode(type: V2Node['type']): void {
    const g = this.graph();
    const id = this.nextName(type === 'output' ? 'exit' : type, g.nodes.map(n => n.id));
    const node: V2Node = { id, type, x: 0, y: 0, ...(type === 'constant' ? { value_type: 'number' as PinType, value: '1' } : {}), ...(type === 'operation' ? { operation: 'add' } : {}) };
    this.set({ ...g, nodes: [...g.nodes, node] });
  }
  patch(id: string, change: Partial<V2Node>): void {
    const g = this.graph();
    this.set({ ...g, nodes: g.nodes.map(n => n.id === id ? { ...n, ...change } : n) });
  }
  removeNode(id: string): void {
    const g = this.graph();
    this.set({ ...g, nodes: g.nodes.filter(n => n.id !== id), edges: g.edges.filter(e => e.from !== id && e.to !== id) });
  }

  sourceNodes(kind: string) { return this.graph().nodes.filter(n => outputPorts(n, this.graph()).some(p => p.kind === kind)); }
  targetNodes(kind: string) { return this.graph().nodes.filter(n => inputPorts(n, this.graph()).some(p => p.kind === kind)); }
  sourcePorts(id: string, kind: string) { const n = this.graph().nodes.find(x => x.id === id); return n ? outputPorts(n, this.graph()).filter(p => p.kind === kind) : []; }
  targetPorts(id: string, kind: string) { const n = this.graph().nodes.find(x => x.id === id); return n ? inputPorts(n, this.graph()).filter(p => p.kind === kind) : []; }
  resetPorts(): void { /* the port lists re-render from the selected kind */ }

  wire(kind: string, from: string, fromPort: string, to: string, toPort: string): void {
    if (!from || !fromPort || !to || !toPort) return;
    const g = this.graph();
    const wire: V2Edge = { kind: kind as WireKind, from, from_port: fromPort, to, to_port: toPort };
    this.set({ ...g, edges: connectWire(g.edges, wire, g) });
  }
  unwire(index: number): void {
    const g = this.graph();
    this.set({ ...g, edges: g.edges.filter((_, i) => i !== index) });
  }
}
