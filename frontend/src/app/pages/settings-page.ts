import { ChangeDetectionStrategy, Component } from '@angular/core';
import { ApiSettingsComponent } from '../components/api-settings';

@Component({
  selector: 'fs-settings-page',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ApiSettingsComponent],
  template: `
    <div class="fs-page">
      <header class="fs-page-header">
        <div>
          <div class="fs-eyebrow">System administration</div>
          <h1>Settings</h1>
          <p>AI and search integrations, people and roles, mail and updates. Credentials are encrypted on the server and never shown again.</p>
        </div>
      </header>
      <fse-api-settings />
    </div>
  `,
})
export class SettingsPage {}
