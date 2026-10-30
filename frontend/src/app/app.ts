import { ChangeDetectionStrategy, Component, inject } from '@angular/core';

import { AuthState } from './auth.service';
import { LoginComponent } from './components/login';
import { ShellComponent } from './shell/shell';

@Component({
  selector: 'app-root',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [LoginComponent, ShellComponent],
  template: `
    @if (auth.enabled() && !auth.user()) {
      <fse-login />
    } @else {
      <fs-shell />
    }
  `,
})
export class App {
  readonly auth = inject(AuthState);
}
