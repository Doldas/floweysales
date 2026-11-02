import { ChangeDetectionStrategy, Component } from '@angular/core';

import { ForumComponent } from '../components/forum';

@Component({
  selector: 'fs-community-page',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ForumComponent],
  template: `
    <div class="fs-page">
      <header class="fs-page-header">
        <div>
          <div class="fs-eyebrow">Workspace</div>
          <h1>Community</h1>
          <p>Theses and evidence shared by everyone on this workspace. Pick a topic to focus on one market.</p>
        </div>
      </header>
      <fse-forum />
    </div>
  `,
})
export class CommunityPage {}
