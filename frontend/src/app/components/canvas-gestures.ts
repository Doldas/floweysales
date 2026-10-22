// Shared pointer-based pan and pinch controller for SVG viewBox canvases.
// Viewport updates never mutate saved node coordinates.
export interface CanvasViewport { x:number; y:number; width:number; height:number }
type Point = {x:number;y:number};
export class CanvasGestures {
 private pointers=new Map<number,Point>();
 private lastCenter:Point|null=null;
 private lastDistance=0;
 constructor(private readonly read:()=>CanvasViewport,private readonly set:(view:CanvasViewport)=>void,
             private readonly baseWidth:number,private readonly baseHeight:number){}
 private relative(svg:SVGSVGElement,p:Point):Point {
  const rect=svg.getBoundingClientRect();
  return {x:(p.x-rect.left)/Math.max(rect.width,1),y:(p.y-rect.top)/Math.max(rect.height,1)};
 }
 private midpoint():Point|null {
  const pts=[...this.pointers.values()];
  if(!pts.length)return null;
  if(pts.length===1)return pts[0];
  return {x:(pts[0].x+pts[1].x)/2,y:(pts[0].y+pts[1].y)/2};
 }
 private distance():number {
  const pts=[...this.pointers.values()];
  return pts.length<2?0:Math.hypot(pts[0].x-pts[1].x,pts[0].y-pts[1].y);
 }
 private updateReference():void {this.lastCenter=this.midpoint();this.lastDistance=this.distance();}
 pointerDown(ev:PointerEvent):void {
  if(ev.pointerType==='mouse'&&ev.button!==0)return;
  // Mouse drags of node bodies remain the editor's own node repositioning gesture.
  if(ev.pointerType==='mouse'&&(ev.target as Element)?.closest('[data-canvas-node]'))return;
  const svg=ev.currentTarget as SVGSVGElement;
  this.pointers.set(ev.pointerId,{x:ev.clientX,y:ev.clientY});
  svg.setPointerCapture(ev.pointerId);
  this.updateReference();
 }
 pointerMove(ev:PointerEvent):void {
  if(!this.pointers.has(ev.pointerId))return;
  const svg=ev.currentTarget as SVGSVGElement;
  const prior=this.lastCenter;
  const priorDistance=this.lastDistance;
  this.pointers.set(ev.pointerId,{x:ev.clientX,y:ev.clientY});
  const now=this.midpoint();
  if(!now||!prior){this.updateReference();return}
  const rect=svg.getBoundingClientRect();
  const v=this.read();
  const x=v.x-(now.x-prior.x)*v.width/Math.max(rect.width,1);
  const y=v.y-(now.y-prior.y)*v.height/Math.max(rect.height,1);
  this.set({...v,x,y});
  if(this.pointers.size>=2&&priorDistance>0){
   const distance=this.distance();
   if(distance>0)this.zoomAt(svg,now,priorDistance/distance);
  }
  this.updateReference();
 }
 pointerEnd(ev:PointerEvent):void {
  this.pointers.delete(ev.pointerId);
  this.updateReference();
  const svg=ev.currentTarget as SVGSVGElement;
  if(svg.hasPointerCapture(ev.pointerId))svg.releasePointerCapture(ev.pointerId);
 }
 wheel(ev:WheelEvent):void {
  ev.preventDefault();
  const factor=Math.exp(Math.max(-100,Math.min(100,ev.deltaY))*0.002);
  this.zoomAt(ev.currentTarget as SVGSVGElement,{x:ev.clientX,y:ev.clientY},factor);
 }
 zoomAt(svg:SVGSVGElement,anchor:Point,factor:number):void {
  const v=this.read();
  const nextWidth=Math.max(this.baseWidth/3,Math.min(this.baseWidth*4,v.width*factor));
  const nextHeight=nextWidth*this.baseHeight/this.baseWidth;
  const norm=this.relative(svg,anchor);
  this.set({x:v.x+norm.x*(v.width-nextWidth),y:v.y+norm.y*(v.height-nextHeight),width:nextWidth,height:nextHeight});
 }
 reset():void {this.pointers.clear();this.lastCenter=null;this.lastDistance=0;}
}
