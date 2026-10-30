import { ChangeDetectionStrategy, Component } from '@angular/core';
import { UserspaceFilesComponent } from '../components/userspace-files';

/** Private research workspace: files flows read and write, plus your own notes. */
@Component({
  selector: 'fs-workspace-page',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [UserspaceFilesComponent],
  template: `
    <div class="fs-page">
      <header class="fs-page-header">
        <div>
          <div class="fs-eyebrow">Workspace</div>
          <h1>Files</h1>
          <p>Your private research space. Flows read inputs and save reports here; you can edit, preview and download them.</p>
        </div>
      </header>
      <fse-userspace-files />
    </div>
  `,
})
export class WorkspacePage {}
