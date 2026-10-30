import { ChangeDetectionStrategy, Component } from '@angular/core';
import { TrackerEmailSettingsComponent } from '../components/tracker-email-settings';

@Component({
  selector: 'fs-notifications-page',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [TrackerEmailSettingsComponent],
  template: `
    <div class="fs-page">
      <header class="fs-page-header">
        <div>
          <div class="fs-eyebrow">Account</div>
          <h1>Notifications</h1>
          <p>Email alerts for tracker signals: golden and death crosses and stop-loss events.</p>
        </div>
      </header>
      <fse-tracker-email-settings />
    </div>
  `,
})
export class NotificationsPage {}
