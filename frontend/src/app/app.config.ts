import { DATE_PIPE_DEFAULT_OPTIONS } from '@angular/common';
import { ApplicationConfig, inject, isDevMode, provideAppInitializer, provideBrowserGlobalErrorListeners, provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient } from '@angular/common/http';
import { provideServiceWorker } from '@angular/service-worker';
import { provideRouter, withComponentInputBinding, withHashLocation, withInMemoryScrolling } from '@angular/router';

import { routes } from './app.routes';
import { AuthState } from './auth.service';

export const appConfig: ApplicationConfig = {
  providers: [
    provideBrowserGlobalErrorListeners(),
    provideZonelessChangeDetection(),
    provideHttpClient(),
    // The date pipe shows UTC, like every date in the app (see ui/datetime.ts).
    { provide: DATE_PIPE_DEFAULT_OPTIONS, useValue: { timezone: 'UTC' } },
    // The session role is resolved before the first navigation so route
    // guards and the navigation can rely on it.
    provideAppInitializer(() => inject(AuthState).check()),
    // Hash URLs work from the embedded Go file server and any static host
    // without a server-side SPA fallback.
    // The installable app caches its shell and fonts only (ngsw-config.json);
    // /api/* is never cached, so figures always come from the server.
    provideServiceWorker('ngsw-worker.js', { enabled: !isDevMode(), registrationStrategy: 'registerWhenStable:30000' }),
    provideRouter(routes, withHashLocation(), withComponentInputBinding(), withInMemoryScrolling({ scrollPositionRestoration: 'top' })),
  ],
};
