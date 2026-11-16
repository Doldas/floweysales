import { ChangeDetectionStrategy, Component, computed, effect, input, output, signal, untracked } from '@angular/core';
import { CanvasGestures } from '../../components/canvas-gestures';
import {
  DiagramEdge, DiagramNode, DiagramPortPoint, NODE_HEIGHT, NODE_WIDTH, ViewBox, connectorPath, diagramPorts, draggedPosition, edgeBetween,
  fitViewBox, nearestDiagramPort, neighborInDirection, portOffset, toneLabel, viewBoxAttr, zoomViewBox,
} from './flow-canvas-geometry';
import { FLOW_CANVAS_STYLES } from './flow-canvas-styles';

const BASE_ASPECT = 820 / 380;

/** Pointer travel, in canvas units, before a press on a port counts as drawing a wire. */
const WIRE_DRAG_THRESHOLD = 4;

/**
 * The shared graph canvas. Functions, Triggers, typed-function previews, run
 * snapshots and the Flow Editor render through it, so they get the same node
 * cards, typed ports, connectors, state tones, pan/zoom/fit controls and
 * keyboard navigation.
 *
 * It never changes the graph itself. Read-only surfaces only report selection
 * and context-menu requests. With `editable` it also reports what the user
 * did with the pointer (a card dragged, a wire drawn between two ports, a port
 * activated), and the owning editor decides what that changes. The canvas then
 * draws whatever graph it is given next. Contract: docs/flow-diagram-system.md.
 */
@Component({
  selector: 'fse-flow-diagram',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
  <div class="diagram-shell" [attr.data-diagram]="variant()" [class.is-editable]="editable()" (keydown)="onKey($event)">
   <div class="canvas-navigation" role="toolbar" aria-label="Canvas view controls">
    <button type="button" (click)="zoom(0.8)" aria-label="Zoom in"><i class="bi bi-plus-lg" aria-hidden="true"></i></button>
    <span aria-live="polite">{{zoomPercent()}}%</span>
    <button type="button" (click)="zoom(1.25)" aria-label="Zoom out"><i class="bi bi-dash-lg" aria-hidden="true"></i></button>
    <button type="button" (click)="fit()" title="Fit all nodes in view"><i class="bi bi-arrows-fullscreen" aria-hidden="true"></i> {{fitLabel()}}</button>
    <button type="button" (click)="reset()" title="Reset canvas view"><i class="bi bi-bootstrap-reboot" aria-hidden="true"></i> Reset view</button>
    @if(readonlyHint()){<span class="readonly-note">{{readonlyHint()}}</span>}
    <ng-content select="[canvasToolbar]"/>
   </div>
   <div class="diagram-surface canvas-surface">
    <svg [attr.viewBox]="viewBoxText()" role="group" [attr.aria-label]="ariaLabel()"
     (wheel)="gestures.wheel($event)" (pointerdown)="gestures.pointerDown($event)" (pointermove)="gestures.pointerMove($event)"
     (pointerup)="gestures.pointerEnd($event)" (pointercancel)="gestures.pointerEnd($event)" (click)="canvasClick.emit()" (contextmenu)="canvasContextMenu.emit($event)">
     <defs><marker [attr.id]="arrowId" markerWidth="9" markerHeight="9" refX="8" refY="4" orient="auto"><path d="M0,0 L8,4 L0,8 Z" class="edge-arrow"/></marker></defs>
     @for(edge of edges(); track $index){
      <path [attr.d]="path(edge)" class="flow-edge" [class.kind-exec]="edge.kind==='exec'" [class.kind-event]="edge.kind==='event'" [attr.marker-end]="'url(#'+arrowId+')'"/>
     }
     @if(wireGhost();as w){
      <path class="wire-ghost" [attr.d]="ghostPath(w)" pointer-events="none"/>
      @if(w.target;as t){<circle class="wire-target" [attr.cx]="t.x" [attr.cy]="t.y" r="10" pointer-events="none"/>}
     }
     @for(n of nodes(); track n.id){
      <g data-canvas-node [attr.data-node-id]="n.id" tabindex="0" [attr.role]="editable()?null:'button'" [attr.aria-pressed]="editable()?null:n.id===selectedId()" [attr.aria-label]="nodeName(n)"
       [style.cursor]="editable()?'move':null"
       (click)="$event.stopPropagation();select(n.id)" (keydown.enter)="select(n.id)" (keydown.space)="$event.preventDefault();select(n.id)" (pointerdown)="nodePointerDown($event,n)">
       <rect class="flow-node" [class]="'tone-'+(n.tone||'none')" [class.selected]="n.id===selectedId()"
        [attr.x]="n.x" [attr.y]="n.y" [attr.width]="nodeWidth" [attr.height]="nodeHeight" rx="12"/>
       <rect class="flow-node-accent" [attr.data-type]="n.kind" [attr.x]="n.x" [attr.y]="n.y" width="4" [attr.height]="nodeHeight" rx="2"/>
       <text class="node-type" [attr.x]="n.x+12" [attr.y]="n.y+24" font-size="11">{{n.tag}}</text>&ngsp;
       <text class="node-label" [attr.x]="n.x+12" [attr.y]="n.y+48" font-size="13">{{n.label.slice(0,16)}}</text>&ngsp;
       @if(subline(n);as s){<text class="node-sub" [class]="n.subClass||''" [attr.x]="n.x+12" [attr.y]="n.y+63" font-size="10">{{s.slice(0,22)}}</text>}
       @for(p of n.inputs||[]; track p.name; let i=$index){
        <g>
         <circle class="canvas-port pin-in" [attr.cx]="n.x" [attr.cy]="n.y+offset(i,(n.inputs||[]).length)" r="6" [attr.pointer-events]="editable()?null:'none'"
          (pointerdown)="$event.stopPropagation()" (click)="$event.stopPropagation()"><title>Input {{p.name}}{{p.type?' ('+p.type+')':''}}</title></circle>
         @if(showPinLabels()){<text class="pin-label" [attr.x]="n.x+8" [attr.y]="n.y+offset(i,(n.inputs||[]).length)+3" font-size="8">{{p.name.slice(0,8)}}</text>}
        </g>
       }
       @for(p of n.outputs||[]; track p.name; let i=$index){
        <g>
         <circle class="canvas-port pin-out" [attr.cx]="n.x+nodeWidth" [attr.cy]="n.y+offset(i,(n.outputs||[]).length)" r="6" [attr.pointer-events]="editable()?null:'none'"
          (pointerdown)="startWire($event,n.id,p.name)" (click)="$event.stopPropagation()"><title>Output {{p.name}}{{p.type?' ('+p.type+')':''}}</title></circle>
         @if(showPinLabels()){<text class="pin-label" [attr.x]="n.x+nodeWidth-8" [attr.y]="n.y+offset(i,(n.outputs||[]).length)+3" font-size="8" text-anchor="end">{{p.name.slice(0,8)}}</text>}
        </g>
       }
       @if(editable()&&n.inPort&&!(n.inputs||[]).length){
        <circle class="canvas-port input-port" [attr.cx]="n.x" [attr.cy]="n.y+nodeHeight/2" r="8" tabindex="0" role="button" [attr.aria-label]="n.inPort"
         (pointerdown)="$event.stopPropagation()" (click)="$event.stopPropagation();portClick(n.id,'in')" (keydown.enter)="$event.stopPropagation();portActivate.emit({nodeId:n.id,side:'in'})"/>
       }
       @if(editable()&&n.outPort&&!(n.outputs||[]).length){
        <circle class="canvas-port output-port" [class.port-active]="connectingFrom()===n.id" [attr.cx]="n.x+nodeWidth" [attr.cy]="n.y+nodeHeight/2" r="8" tabindex="0" role="button" [attr.aria-label]="n.outPort"
         (pointerdown)="startWire($event,n.id)" (click)="$event.stopPropagation();portClick(n.id,'out')" (keydown.enter)="$event.stopPropagation();portActivate.emit({nodeId:n.id,side:'out'})"/>
       }
      </g>
     }
    </svg>
    @if(!nodes().length){<p class="empty">{{emptyText()}}</p>}
   </div>
   <ng-content/>
  </div>`,
  styles: [FLOW_CANVAS_STYLES, `
  :host{display:block;min-width:0}
  .diagram-shell{position:relative;min-width:0;border:1px solid var(--fs-border);border-radius:var(--bs-border-radius-lg);overflow:hidden;background:var(--fs-canvas-bg)}
  .diagram-surface{position:relative;min-height:280px;overflow:hidden}
  svg{display:block;width:100%;height:auto;min-height:280px;max-height:640px;touch-action:none;cursor:grab;user-select:none;-webkit-user-select:none}
  [data-canvas-node]{cursor:pointer}
  .readonly-note{margin-left:auto;font-size:.75rem;color:var(--fs-muted)}
  .empty{position:absolute;inset:0;display:grid;place-items:center;margin:0;color:var(--fs-muted);pointer-events:none}
  /* The Flow Editor sits in a frame of its own, with the dotted grid behind its nodes and room to draw. */
  .diagram-shell[data-diagram=editor]{border:0;border-radius:0;overflow:visible}
  [data-diagram=editor] .diagram-surface{background:radial-gradient(var(--fs-canvas-dot) 1px,transparent 1px) 0 0/20px 20px,var(--fs-canvas-bg);overflow:auto;min-height:470px}
  [data-diagram=editor] svg{min-width:660px;max-height:none}
  @media(max-width:850px){svg{min-height:240px}[data-diagram=editor] .diagram-surface{min-height:280px}}
  `],
})
export class FlowDiagramComponent {
  private static nextId = 0;
  readonly arrowId = 'flow-diagram-arrow-' + FlowDiagramComponent.nextId++;
  readonly nodeWidth = NODE_WIDTH;
  readonly nodeHeight = NODE_HEIGHT;

  readonly nodes = input<DiagramNode[]>([]);
  readonly edges = input<DiagramEdge[]>([]);
  readonly selectedId = input<string>('');
  readonly ariaLabel = input<string>('Flow diagram: nodes and directed connections');
  /** Names the surface (editor, function, trigger, run) for tests and docs. */
  readonly variant = input<string>('flow');
  readonly readonlyHint = input<string>('');
  readonly emptyText = input<string>('Nothing to show yet.');
  /** Changing this value refits the view (for example the open function's id). */
  readonly fitKey = input<string | number>('');
  /** Refit whenever the node count changes. An editor turns it off: the user decides when to fit. */
  readonly autoFit = input<boolean>(true);
  /** Text of the Fit button; the Flow Editor calls it "Fit flow". */
  readonly fitLabel = input<string>('Fit');
  /** Margin around the nodes when fitting, in canvas units, and the narrowest view Fit may zoom in to. */
  readonly fitPadding = input<number>(60);
  readonly fitMinWidth = input<number>(820);

  /** Lets the user move cards, draw wires and activate ports. The canvas only reports it. */
  readonly editable = input<boolean>(false);
  /** Names the pins of typed ports next to them. */
  readonly showPinLabels = input<boolean>(false);
  /** The node whose output port was chosen to start a connection; its port is marked. */
  readonly connectingFrom = input<string>('');

  readonly nodeSelect = output<string>();
  readonly canvasClick = output<void>();
  readonly canvasContextMenu = output<MouseEvent>();
  /** A card is being dragged: where it would be, and whether the drag started, moved or ended. */
  readonly nodeDragged = output<{ id: string; x: number; y: number; phase: 'start' | 'move' | 'end' }>();
  /** A wire was dragged from an output port and dropped on an input port. */
  readonly wireDrawn = output<{ source: DiagramPortPoint; target: DiagramPortPoint }>();
  /** A plain port was clicked, or Enter was pressed on it. */
  readonly portActivate = output<{ nodeId: string; side: 'in' | 'out' }>();

  readonly view = signal<ViewBox>({ x: 0, y: 0, width: 820, height: 380 });
  readonly viewBoxText = computed(() => viewBoxAttr(this.view()));
  readonly zoomPercent = computed(() => Math.round(820 / this.view().width * 100));
  readonly gestures = new CanvasGestures(() => this.view(), v => this.view.set(v), 820, 380);

  /** The wire being drawn, and the input port it would snap to. */
  readonly wireGhost = signal<{ source: DiagramPortPoint; x: number; y: number; target: DiagramPortPoint | null } | null>(null);
  private suppressPortClick = false;

  constructor() {
    effect(() => {
      this.fitKey();
      const count = this.nodes().length;
      const auto = this.autoFit();
      untracked(() => { if (auto && count) this.fit(); });
    });
  }

  /** Fits the view to the nodes. An editor that has just replaced its graph passes the new nodes: the inputs are not updated yet. */
  fit(nodes: readonly { x: number; y: number }[] = this.nodes()): void { this.view.set(fitViewBox(nodes, BASE_ASPECT, this.fitPadding(), this.fitMinWidth())); }
  reset(): void { this.gestures.reset(); this.view.set({ x: 0, y: 0, width: 820, height: 380 }); }
  zoom(factor: number): void { this.view.update(v => zoomViewBox(v, factor, 820 / 3, 820 * 4)); }
  select(id: string): void { this.nodeSelect.emit(id); }
  path(edge: DiagramEdge): string { return edgeBetween(this.nodes(), edge); }
  offset(index: number, count: number): number { return portOffset(index, count); }
  ghostPath(w: { source: DiagramPortPoint; x: number; y: number; target: DiagramPortPoint | null }): string {
    return connectorPath(w.source.x, w.source.y, w.target?.x ?? w.x, w.target?.y ?? w.y);
  }
  subline(n: DiagramNode): string { return n.plainSub ? (n.sub ?? '') : [n.sub, toneLabel(n.tone)].filter(Boolean).join(' · '); }
  nodeName(n: DiagramNode): string {
    if (n.accessibleName) return n.accessibleName;
    // WCAG 2.5.3: the accessible name must contain the visible text, so lead with exactly what is rendered.
    const visible = [n.tag, n.label.slice(0, 16), this.subline(n).slice(0, 22)].filter(Boolean).join(' ');
    return n.ariaLabel && !visible.includes(n.ariaLabel) ? `${visible}, ${n.ariaLabel}` : visible;
  }

  /** A plain port was clicked; a click that ends a wire drag is not one. */
  portClick(nodeId: string, side: 'in' | 'out'): void {
    if (!this.suppressPortClick) this.portActivate.emit({ nodeId, side });
  }

  /** Where a pointer event is on the canvas, in view box units. */
  private at(svg: SVGSVGElement, e: PointerEvent, fallback: { x: number; y: number }): { x: number; y: number } {
    const m = svg.getScreenCTM();
    if (!m) return fallback;
    const p = new DOMPoint(e.clientX, e.clientY).matrixTransform(m.inverse());
    return { x: p.x, y: p.y };
  }

  /** Drags a card with the mouse. A touch screen pans the canvas instead. */
  nodePointerDown(ev: PointerEvent, n: DiagramNode): void {
    if (!this.editable() || ev.button !== 0 || ev.pointerType === 'touch') return;
    const svg = (ev.currentTarget as SVGGElement).ownerSVGElement;
    if (!svg) return;
    const origin = { x: n.x, y: n.y };
    const [startX, startY] = [ev.clientX, ev.clientY];
    let last = origin;
    this.nodeDragged.emit({ id: n.id, ...origin, phase: 'start' });
    const move = (e: PointerEvent) => {
      last = draggedPosition(origin, e.clientX - startX, e.clientY - startY, this.view(), svg.getBoundingClientRect());
      this.nodeDragged.emit({ id: n.id, ...last, phase: 'move' });
    };
    const stop = () => {
      svg.removeEventListener('pointermove', move);
      svg.removeEventListener('pointerup', stop);
      svg.removeEventListener('pointercancel', stop);
      this.nodeDragged.emit({ id: n.id, ...last, phase: 'end' });
    };
    svg.addEventListener('pointermove', move);
    svg.addEventListener('pointerup', stop);
    svg.addEventListener('pointercancel', stop);
  }

  /** Starts a wire from an output port (or a typed output pin); dropping it on an input port reports it. */
  startWire(ev: PointerEvent, nodeId: string, pin?: string): void {
    if (!this.editable()) return;
    ev.stopPropagation();
    if (ev.button !== 0) return;
    ev.preventDefault();
    const points = diagramPorts(this.nodes());
    const source = points.find(p => p.side === 'out' && p.nodeId === nodeId && p.pin === pin);
    const svg = (ev.currentTarget as SVGElement).ownerSVGElement;
    if (!source || !svg) return;
    const origin = this.at(svg, ev, source);
    let moved = false;
    const move = (e: PointerEvent) => {
      const p = this.at(svg, e, source);
      if (Math.hypot(p.x - origin.x, p.y - origin.y) > WIRE_DRAG_THRESHOLD) moved = true;
      if (!moved) return;
      this.wireGhost.set({ source, x: p.x, y: p.y, target: nearestDiagramPort(points, p.x, p.y, 'in') });
    };
    const stop = (e: PointerEvent) => {
      svg.removeEventListener('pointermove', move);
      svg.removeEventListener('pointerup', stop);
      svg.removeEventListener('pointercancel', stop);
      const target = e.type === 'pointerup' && moved ? this.wireGhost()?.target ?? null : null;
      this.wireGhost.set(null);
      if (!moved) return;
      // The click that follows a drag is not a request to start a connection.
      this.suppressPortClick = true;
      setTimeout(() => (this.suppressPortClick = false), 0);
      if (target) this.wireDrawn.emit({ source, target });
    };
    svg.addEventListener('pointermove', move);
    svg.addEventListener('pointerup', stop);
    svg.addEventListener('pointercancel', stop);
  }

  /** Arrow keys move focus between nodes; + / - / 0 zoom and reset like the toolbar. */
  onKey(event: KeyboardEvent): void {
    const el = event.target as Element | null;
    const id = el?.closest?.('[data-node-id]')?.getAttribute('data-node-id');
    const dir: Record<string, [number, number]> = { ArrowRight: [1, 0], ArrowLeft: [-1, 0], ArrowDown: [0, 1], ArrowUp: [0, -1] };
    if (id && dir[event.key]) {
      const next = neighborInDirection(this.nodes(), id, dir[event.key][0], dir[event.key][1]);
      if (next !== id) {
        event.preventDefault();
        (el!.closest('svg')?.querySelector(`[data-node-id="${CSS.escape(next)}"]`) as SVGElement | null)?.focus();
      }
      return;
    }
    if (el?.closest('button') || el instanceof HTMLInputElement || el instanceof HTMLSelectElement) return;
    if (event.key === '+' || event.key === '=') { event.preventDefault(); this.zoom(0.8); }
    else if (event.key === '-') { event.preventDefault(); this.zoom(1.25); }
    else if (event.key === '0') { event.preventDefault(); this.reset(); }
  }
}
