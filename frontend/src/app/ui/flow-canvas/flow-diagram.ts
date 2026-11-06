import { ChangeDetectionStrategy, Component, computed, effect, input, output, signal, untracked } from '@angular/core';
import { CanvasGestures } from '../../components/canvas-gestures';
import {
  DiagramEdge, DiagramNode, NODE_HEIGHT, NODE_WIDTH, ViewBox, edgeBetween, fitViewBox, neighborInDirection, portOffset, toneLabel, viewBoxAttr, zoomViewBox,
} from './flow-canvas-geometry';
import { FLOW_CANVAS_STYLES } from './flow-canvas-styles';

const BASE_ASPECT = 820 / 380;

/**
 * The shared graph canvas. Functions, Triggers, typed-function previews and
 * run snapshots render through it, so they get the Flow Editor's node cards,
 * typed ports, connectors, state tones, pan/zoom/fit controls and keyboard
 * navigation. It never mutates node coordinates: it reports selection and
 * context-menu requests, and the owning editor decides what they change.
 * Contract: docs/flow-diagram-system.md.
 */
@Component({
  selector: 'fse-flow-diagram',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
  <div class="diagram-shell" [attr.data-diagram]="variant()" (keydown)="onKey($event)">
   <div class="canvas-navigation" role="toolbar" aria-label="Canvas view controls">
    <button type="button" (click)="zoom(0.8)" aria-label="Zoom in"><i class="bi bi-plus-lg" aria-hidden="true"></i></button>
    <span aria-live="polite">{{zoomPercent()}}%</span>
    <button type="button" (click)="zoom(1.25)" aria-label="Zoom out"><i class="bi bi-dash-lg" aria-hidden="true"></i></button>
    <button type="button" (click)="fit()" title="Fit all nodes in view"><i class="bi bi-arrows-fullscreen" aria-hidden="true"></i> Fit</button>
    <button type="button" (click)="reset()" title="Reset canvas view"><i class="bi bi-bootstrap-reboot" aria-hidden="true"></i> Reset view</button>
    @if(readonlyHint()){<span class="readonly-note">{{readonlyHint()}}</span>}
   </div>
   <div class="diagram-surface canvas-surface">
    <svg [attr.viewBox]="viewBoxText()" role="group" [attr.aria-label]="ariaLabel()"
     (wheel)="gestures.wheel($event)" (pointerdown)="gestures.pointerDown($event)" (pointermove)="gestures.pointerMove($event)"
     (pointerup)="gestures.pointerEnd($event)" (pointercancel)="gestures.pointerEnd($event)" (click)="canvasClick.emit()" (contextmenu)="canvasContextMenu.emit($event)">
     <defs><marker [attr.id]="arrowId" markerWidth="9" markerHeight="9" refX="8" refY="4" orient="auto"><path d="M0,0 L8,4 L0,8 Z" class="edge-arrow"/></marker></defs>
     @for(edge of edges(); track $index){
      <path [attr.d]="path(edge)" class="flow-edge" [class.kind-exec]="edge.kind==='exec'" [class.kind-event]="edge.kind==='event'" [attr.marker-end]="'url(#'+arrowId+')'"/>
     }
     @for(n of nodes(); track n.id){
      <g data-canvas-node [attr.data-node-id]="n.id" tabindex="0" role="button" [attr.aria-pressed]="n.id===selectedId()" [attr.aria-label]="nodeName(n)"
       (click)="$event.stopPropagation();select(n.id)" (keydown.enter)="select(n.id)" (keydown.space)="$event.preventDefault();select(n.id)">
       <rect class="flow-node" [class]="'tone-'+(n.tone||'none')" [class.selected]="n.id===selectedId()"
        [attr.x]="n.x" [attr.y]="n.y" [attr.width]="nodeWidth" [attr.height]="nodeHeight" rx="12"/>
       <rect class="flow-node-accent" [attr.data-type]="n.kind" [attr.x]="n.x" [attr.y]="n.y" width="4" [attr.height]="nodeHeight" rx="2"/>
       <text class="node-type" [attr.x]="n.x+12" [attr.y]="n.y+24" font-size="11">{{n.tag}}</text>
       <text class="node-label" [attr.x]="n.x+12" [attr.y]="n.y+46" font-size="13">{{n.label.slice(0,16)}}</text>
       @if(subline(n);as s){<text class="node-sub" [attr.x]="n.x+12" [attr.y]="n.y+62" font-size="10">{{s.slice(0,22)}}</text>}
       @for(p of n.inputs||[]; track p.name; let i=$index){
        <circle class="canvas-port pin-in" [attr.cx]="n.x" [attr.cy]="n.y+offset(i,(n.inputs||[]).length)" r="6" pointer-events="none"><title>Input {{p.name}}@if(p.type){ ({{p.type}})}</title></circle>
       }
       @for(p of n.outputs||[]; track p.name; let i=$index){
        <circle class="canvas-port pin-out" [attr.cx]="n.x+nodeWidth" [attr.cy]="n.y+offset(i,(n.outputs||[]).length)" r="6" pointer-events="none"><title>Output {{p.name}}@if(p.type){ ({{p.type}})}</title></circle>
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
  @media(max-width:850px){svg{min-height:240px}}
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

  readonly nodeSelect = output<string>();
  readonly canvasClick = output<void>();
  readonly canvasContextMenu = output<MouseEvent>();

  readonly view = signal<ViewBox>({ x: 0, y: 0, width: 820, height: 380 });
  readonly viewBoxText = computed(() => viewBoxAttr(this.view()));
  readonly zoomPercent = computed(() => Math.round(820 / this.view().width * 100));
  readonly gestures = new CanvasGestures(() => this.view(), v => this.view.set(v), 820, 380);

  constructor() {
    effect(() => {
      this.fitKey();
      const count = this.nodes().length;
      untracked(() => { if (count) this.fit(); });
    });
  }

  fit(): void { this.view.set(fitViewBox(this.nodes(), BASE_ASPECT, 60, 820)); }
  reset(): void { this.gestures.reset(); this.view.set({ x: 0, y: 0, width: 820, height: 380 }); }
  zoom(factor: number): void { this.view.update(v => zoomViewBox(v, factor, 820 / 3, 820 * 4)); }
  select(id: string): void { this.nodeSelect.emit(id); }
  path(edge: DiagramEdge): string { return edgeBetween(this.nodes(), edge); }
  offset(index: number, count: number): number { return portOffset(index, count); }
  subline(n: DiagramNode): string { return [n.sub, toneLabel(n.tone)].filter(Boolean).join(' · '); }
  nodeName(n: DiagramNode): string { return n.ariaLabel || `${n.tag.replace(/^\S+\s/, '')} ${n.label}${toneLabel(n.tone) ? ', ' + toneLabel(n.tone) : ''}`; }

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
