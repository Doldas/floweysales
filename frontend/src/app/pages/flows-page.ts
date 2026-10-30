import { ChangeDetectionStrategy, Component, effect, inject, input, untracked, viewChild } from '@angular/core';
import { Router, RouterLink } from '@angular/router';

import { FlowStudioComponent } from '../components/flow-studio';
import { Access } from '../core/access';
import { ActivityStore } from '../core/activity';

/**
 * Flows: design, run, version and audit agent automations. Query parameters
 * (`flow`, `tab`, `template`) deep-link into the studio from anywhere.
 */
@Component({
  selector: 'fs-flows-page',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [FlowStudioComponent, RouterLink],
  template: `
    <div class="fs-page">
      <header class="fs-page-header">
        <div>
          <div class="fs-eyebrow">Automation</div>
          <h1>Agent flows</h1>
          <p>Chain research agents, data steps and approvals into repeatable workflows. Read-only research, so no flow can place live trades.</p>
        </div>
        @if (activity.inboxCount()) {
          <a class="btn btn-outline-secondary" routerLink="/inbox"><i class="bi bi-inbox" aria-hidden="true"></i>{{ activity.inboxCount() }} waiting for review</a>
        }
      </header>
      <fse-flow-studio />
    </div>
  `,
})
export class FlowsPage {
  readonly access = inject(Access);
  readonly activity = inject(ActivityStore);
  private readonly router = inject(Router);
  private readonly studio = viewChild.required(FlowStudioComponent);

  readonly flow = input<string>();
  readonly tab = input<string>();
  readonly template = input<string>();

  constructor() {
    effect(() => {
      const link = { flow: this.flow() ? Number(this.flow()) : undefined, tab: this.tab(), template: this.template() };
      const studio = this.studio();
      if (!link.flow && !link.tab && !link.template) return;
      untracked(() => {
        void studio.openDeepLink(link);
        // Consume the link so a reload or back navigation does not re-apply a template over edits.
        void this.router.navigate([], { queryParams: {}, replaceUrl: true });
      });
    });
  }
}
