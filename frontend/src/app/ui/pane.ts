import { ChangeDetectionStrategy, Component, ElementRef, HostListener, afterNextRender, computed, inject, input, output, signal, viewChild, DestroyRef } from '@angular/core';

import { Workspace } from '../core/workspace.service';
import { MAX_PANE_HEIGHT, MIN_PANE_HEIGHT, clampHeight, heightForKey } from '../core/workspace-prefs';

let nextId = 0;

/** What the body is showing: how far it is scrolled and how tall its visible area is. */
export interface PaneView { top: number; height: number }

/**
 * A workspace pane: a titled region the user can fold, resize and open
 * full screen. What it does and does not do:
 *
 * - Folding hides only the body. The title, the `[paneSummary]` line and the
 *   `[paneMeta]` block (source, as-of time, units, warnings) stay visible, so
 *   critical context is never hidden by a fold.
 * - The body stays rendered while folded, so charts, scroll positions and
 *   form input keep their state. Nothing is removed from the page.
 * - Fold state and height are saved per user, in this browser, under `paneId`.
 * - The splitter works with the keyboard (arrow keys, Page Up/Down, Home, End)
 *   and the pointer; double-click or Enter-then-Escape is not needed to reach
 *   any content, resizing only changes how much is in view.
 * - Full screen is a focus mode; Escape leaves it and focus returns to its button.
 *
 * `mode="scroll"` gives the body a fixed height with its own scrollbar (tables,
 * logs). `mode="var"` leaves the body at its natural height and publishes the
 * chosen height as `--fs-pane-h` for content that sizes itself (charts).
 */
@Component({
  selector: 'fs-pane',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <section class="pane" [class.is-focus]="focus()" [class.is-collapsed]="collapsed()" [attr.aria-labelledby]="titleId">
      <header class="pane-head">
        <div role="heading" [attr.aria-level]="level()" class="pane-heading" [id]="titleId">
          <button type="button" class="pane-toggle" [attr.aria-expanded]="!collapsed()" [attr.aria-controls]="bodyId" (click)="toggle()">
            <i class="bi" [class.bi-chevron-down]="!collapsed()" [class.bi-chevron-right]="collapsed()" aria-hidden="true"></i>
            <span class="pane-title">{{ heading() }}</span>
          </button>
        </div>
        <span class="pane-summary"><ng-content select="[paneSummary]" /></span>
        <span class="pane-actions">
          <ng-content select="[paneActions]" />
          @if (focusable()) {
            <button #focusBtn type="button" class="btn btn-ghost btn-icon btn-sm" (click)="toggleFocus()" [attr.aria-pressed]="focus()"
                    [attr.aria-label]="focus() ? 'Leave full screen: ' + heading() : 'Full screen: ' + heading()" [title]="focus() ? 'Leave full screen (Esc)' : 'Full screen'">
              <i class="bi" [class.bi-fullscreen]="!focus()" [class.bi-fullscreen-exit]="focus()" aria-hidden="true"></i>
            </button>
          }
        </span>
      </header>
      <div class="pane-meta"><ng-content select="[paneMeta]" /></div>
      <div #body class="pane-body" [id]="bodyId" (scroll)="emitView()" [hidden]="collapsed()" [class.is-scroll]="mode() === 'scroll'" [class.is-sized]="sized()"
           [style.--fs-pane-h]="bodyHeight()" [attr.tabindex]="mode() === 'scroll' && sized() ? 0 : null"
           [attr.role]="mode() === 'scroll' && sized() ? 'group' : null" [attr.aria-label]="mode() === 'scroll' && sized() ? heading() + ', scrollable content' : null">
        <ng-content />
      </div>
      @if (resizable() && !collapsed() && !focus()) {
        <div class="pane-resizer" role="separator" aria-orientation="horizontal" tabindex="0" [attr.aria-controls]="bodyId"
             [attr.aria-label]="'Resize ' + heading() + '. Arrow keys change the height.'" [attr.aria-valuemin]="min()" [attr.aria-valuemax]="max()"
             [attr.aria-valuenow]="height()" [attr.aria-valuetext]="height() + ' pixels'"
             (keydown)="resizeKey($event)" (pointerdown)="dragStart($event)" (pointermove)="dragMove($event)" (pointerup)="dragEnd($event)"
             (pointercancel)="dragEnd($event)" (dblclick)="reset()"><span aria-hidden="true"></span></div>
      }
    </section>
  `,
  styles: [`
    :host { display: block; min-width: 0; }
    .pane { background: var(--fs-surface); border: 1px solid var(--fs-border); border-radius: var(--bs-border-radius-lg); min-width: 0; }
    .pane.is-focus { position: fixed; inset: 0; z-index: 1050; border-radius: 0; display: flex; flex-direction: column; overflow: hidden; }
    .pane-head { display: flex; flex-wrap: wrap; align-items: center; gap: var(--fs-gap-sm, .5rem); padding: var(--fs-pane-pad-y, .6rem) var(--fs-pane-pad-x, 1rem); }
    .pane:not(.is-collapsed) .pane-head { border-bottom: 1px solid var(--fs-border); }
    .pane-heading { margin: 0; font-size: .95rem; font-weight: 650; }
    .pane-toggle { display: inline-flex; align-items: center; gap: .45rem; border: 0; background: transparent; color: var(--fs-text); font: inherit; padding: .15rem .25rem; margin-left: -.25rem; border-radius: 6px; }
    .pane-toggle:focus-visible { box-shadow: var(--fs-focus); outline: 0; }
    .pane-toggle .bi { color: var(--fs-muted); font-size: .8rem; }
    .pane-summary { flex: 1 1 12rem; min-width: 0; color: var(--fs-muted); font-size: .8125rem; }
    .pane-actions { display: inline-flex; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: .35rem; margin-left: auto; min-width: 0; max-width: 100%; }
    .pane-actions > * { min-width: 0; }
    @media (max-width: 575.98px) { .pane-actions { flex: 1 1 100%; justify-content: space-between; } .pane-summary { flex-basis: 100%; order: 3; } }
    .pane-meta { padding: 0 var(--fs-pane-pad-x, 1rem); }
    .pane-meta:empty { display: none; }
    .pane-body { padding: var(--fs-pane-pad-y, .75rem) var(--fs-pane-pad-x, 1rem); min-width: 0; }
    .pane-body[hidden] { display: none; }
    .pane-body.is-scroll.is-sized { height: var(--fs-pane-h); overflow: auto; }
    .pane-body.is-scroll { padding: 0; }
    .pane-body.is-scroll.is-sized:focus-visible { outline: 2px solid var(--fs-accent); outline-offset: -2px; }
    .is-focus .pane-body { flex: 1 1 auto; overflow: auto; --fs-pane-h: max(320px, calc(100vh - 11rem)); height: auto; }
    .is-focus .pane-body.is-scroll { height: auto; }
    .pane-resizer { display: grid; place-items: center; height: .9rem; cursor: row-resize; touch-action: none; border-top: 1px solid var(--fs-border); border-radius: 0 0 var(--bs-border-radius-lg) var(--bs-border-radius-lg); }
    .pane-resizer span { width: 2.5rem; height: 3px; border-radius: 2px; background: var(--fs-border-strong); }
    .pane-resizer:hover span, .pane-resizer:focus-visible span { background: var(--fs-accent); }
    .pane-resizer:focus-visible { outline: 2px solid var(--fs-accent); outline-offset: -2px; }
  `],
})
export class PaneComponent {
  /** Stable id used to save this pane's state. Lower case letters, digits, dot, dash. */
  readonly paneId = input.required<string>();
  readonly heading = input.required<string>();
  readonly level = input<2 | 3 | 4>(2);
  readonly collapsible = input(true);
  readonly resizable = input(false);
  readonly focusable = input(false);
  readonly startCollapsed = input(false);
  readonly defaultHeight = input(380);
  readonly min = input(MIN_PANE_HEIGHT);
  readonly max = input(MAX_PANE_HEIGHT);
  readonly mode = input<'scroll' | 'var'>('scroll');

  private readonly workspace = inject(Workspace);
  private readonly destroyRef = inject(DestroyRef);
  /** Fires when the body scrolls or changes size; used to draw only the rows in view. */
  readonly view = output<PaneView>();
  private readonly body = viewChild<ElementRef<HTMLElement>>('body');
  private readonly focusBtn = viewChild<ElementRef<HTMLButtonElement>>('focusBtn');

  readonly id = nextId++;
  readonly titleId = `fs-pane-t-${this.id}`;
  readonly bodyId = `fs-pane-b-${this.id}`;
  readonly focus = signal(false);
  private readonly draft = signal<number | null>(null);
  private readonly rev = signal(0);
  private drag: { y: number; h: number } | null = null;

  readonly collapsed = computed(() => {
    this.rev();
    if (!this.collapsible()) return false;
    const saved = this.workspace.panel(this.paneId()).collapsed;
    return saved ?? this.startCollapsed();
  });

  readonly height = computed(() => {
    this.rev();
    const saved = this.draft() ?? this.workspace.panel(this.paneId()).height ?? this.defaultHeight();
    return clampHeight(saved, this.min(), this.max());
  });

  /** True when the body has a height of its own. */
  readonly sized = computed(() => this.resizable());
  readonly bodyHeight = computed(() => (this.resizable() && !this.focus() ? `${this.height()}px` : null));

  constructor() {
    afterNextRender(() => {
      const el = this.body()?.nativeElement;
      if (!el || typeof ResizeObserver === 'undefined') return;
      const ro = new ResizeObserver(() => this.emitView());
      ro.observe(el);
      this.destroyRef.onDestroy(() => ro.disconnect());
      this.emitView();
    });
  }

  emitView(): void {
    const el = this.body()?.nativeElement;
    if (el && this.mode() === 'scroll' && el.clientHeight > 0) this.view.emit({ top: el.scrollTop, height: el.clientHeight });
  }

  toggle(): void {
    if (!this.collapsible()) return;
    this.workspace.setPanel(this.paneId(), { collapsed: !this.collapsed() });
    this.rev.update(v => v + 1);
  }

  toggleFocus(): void {
    const on = !this.focus();
    this.focus.set(on);
    if (!on) queueMicrotask(() => this.focusBtn()?.nativeElement.focus());
  }

  @HostListener('document:keydown.escape')
  leaveFocus(): void {
    if (this.focus()) this.toggleFocus();
  }

  resizeKey(event: KeyboardEvent): void {
    const next = heightForKey(event.key, this.height(), this.min(), this.max());
    if (next === null) return;
    event.preventDefault();
    this.setHeight(next);
  }

  dragStart(event: PointerEvent): void {
    this.drag = { y: event.clientY, h: this.height() };
    (event.currentTarget as HTMLElement).setPointerCapture?.(event.pointerId);
  }

  dragMove(event: PointerEvent): void {
    if (!this.drag) return;
    this.draft.set(clampHeight(this.drag.h + event.clientY - this.drag.y, this.min(), this.max()));
  }

  dragEnd(event: PointerEvent): void {
    if (!this.drag) return;
    (event.currentTarget as HTMLElement).releasePointerCapture?.(event.pointerId);
    const h = this.draft();
    this.drag = null;
    this.draft.set(null);
    if (h !== null) this.setHeight(h);
  }

  reset(): void {
    this.workspace.setPanel(this.paneId(), { height: undefined });
    this.rev.update(v => v + 1);
  }

  private setHeight(h: number): void {
    this.workspace.setPanel(this.paneId(), { height: clampHeight(h, this.min(), this.max()) });
    this.rev.update(v => v + 1);
  }
}
