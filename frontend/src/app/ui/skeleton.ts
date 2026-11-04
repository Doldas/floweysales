import { ChangeDetectionStrategy, Component, input } from '@angular/core';

/**
 * A placeholder shaped like the content it stands in for, so nothing jumps when
 * the real content arrives. Decorative: pair it with a labelled status region.
 * The shimmer is CSS only and stops under prefers-reduced-motion.
 */
@Component({
  selector: 'fs-skeleton',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @switch (shape()) {
      @case ('kpi') { <span class="sk sk-line w-40"></span><span class="sk sk-block sk-kpi"></span><span class="sk sk-line w-60"></span> }
      @case ('rows') { @for (n of rowList(); track n) { <span class="sk sk-row"></span> } }
      @case ('block') { <span class="sk sk-block" [style.height]="height()"></span> }
      @default { <span class="sk sk-line" [style.width]="width()"></span> }
    }
  `,
  styles: [`
    :host { display: block; }
    .sk {
      display: block; border-radius: 4px; background: var(--fs-surface-3);
      background-image: linear-gradient(90deg, transparent, color-mix(in srgb, var(--fs-text) 7%, transparent), transparent);
      background-size: 200% 100%; animation: fs-shimmer 1.6s linear infinite;
    }
    .sk-line { height: .75rem; margin-bottom: .6rem; }
    .sk-block { width: 100%; height: 6rem; }
    .sk-kpi { height: 1.6rem; margin: .5rem 0; width: 55%; }
    .sk-row { height: 2.4rem; margin-bottom: .4rem; }
    .w-40 { width: 40%; } .w-60 { width: 60%; }
    @keyframes fs-shimmer { from { background-position: 200% 0; } to { background-position: -200% 0; } }
    @media (prefers-reduced-motion: reduce) { .sk { animation: none; background-image: none; } }
  `],
})
export class SkeletonComponent {
  readonly shape = input<'line' | 'block' | 'kpi' | 'rows'>('line');
  readonly rows = input(5);
  readonly width = input('100%');
  readonly height = input('6rem');
  rowList(): number[] { return Array.from({ length: this.rows() }, (_, i) => i); }
}
