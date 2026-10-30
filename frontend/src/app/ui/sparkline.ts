import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';

/** Minimal inline trend line. Colour follows the direction of the series. */
@Component({
  selector: 'fs-sparkline',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (path()) {
      <svg [attr.viewBox]="'0 0 ' + width() + ' ' + height()" preserveAspectRatio="none" role="img" [attr.aria-label]="label()">
        <path [attr.d]="area()" class="area" [class.down]="down()" />
        <path [attr.d]="path()" class="line" [class.down]="down()" />
      </svg>
    }
  `,
  styles: [`
    :host { display: block; width: 100%; height: 100%; }
    svg { display: block; width: 100%; height: 100%; overflow: visible; }
    .line { fill: none; stroke: var(--fs-pos); stroke-width: 1.6; vector-effect: non-scaling-stroke; stroke-linejoin: round; }
    .line.down { stroke: var(--fs-neg); }
    .area { fill: var(--fs-pos-soft); stroke: none; }
    .area.down { fill: var(--fs-neg-soft); }
  `],
})
export class SparklineComponent {
  readonly values = input<number[]>([]);
  readonly width = input(120);
  readonly height = input(32);
  readonly label = input('Price trend');

  readonly down = computed(() => {
    const v = this.values();
    return v.length > 1 && v[v.length - 1] < v[0];
  });

  private readonly points = computed(() => {
    const v = this.values().filter(n => Number.isFinite(n));
    if (v.length < 2) return [] as [number, number][];
    const min = Math.min(...v);
    const max = Math.max(...v);
    const span = max - min || 1;
    const w = this.width();
    const h = this.height();
    return v.map((n, i) => [(i / (v.length - 1)) * w, h - ((n - min) / span) * (h - 2) - 1] as [number, number]);
  });

  readonly path = computed(() => this.points().map(([x, y], i) => `${i ? 'L' : 'M'}${x.toFixed(1)},${y.toFixed(1)}`).join(' '));
  readonly area = computed(() => {
    const p = this.points();
    if (!p.length) return '';
    return `${this.path()} L${this.width()},${this.height()} L0,${this.height()} Z`;
  });
}
