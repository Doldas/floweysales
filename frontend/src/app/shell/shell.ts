import { ChangeDetectionStrategy, Component, DestroyRef, HostListener, computed, effect, inject, signal } from '@angular/core';
import { DecimalPipe } from '@angular/common';
import { ActivatedRoute, NavigationEnd, Router, RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { filter } from 'rxjs';

import { AuthState } from '../auth.service';
import { Access } from '../core/access';
import { ActivityStore } from '../core/activity';
import { AgentSession } from '../core/agent-session';
import { MarketState } from '../core/market-state';
import { ThemeService } from '../theme.service';
import { AgentDockComponent } from './agent-dock';
import { CommandPaletteComponent, CommandPaletteService } from './command-palette';
import { SymbolSearchComponent } from './symbol-search';
import { ToastHostComponent } from './toast-host';

interface NavItem {
  path: string;
  label: string;
  icon: string;
  exact?: boolean;
  visible: () => boolean;
  badge?: () => number;
}

interface NavSection {
  label: string;
  items: NavItem[];
}

@Component({
  selector: 'fs-shell',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterOutlet, RouterLink, RouterLinkActive, DecimalPipe, AgentDockComponent, CommandPaletteComponent, SymbolSearchComponent, ToastHostComponent],
  templateUrl: './shell.html',
  styleUrl: './shell.scss',
})
export class ShellComponent {
  readonly auth = inject(AuthState);
  readonly access = inject(Access);
  readonly market = inject(MarketState);
  readonly agent = inject(AgentSession);
  readonly activity = inject(ActivityStore);
  readonly theme = inject(ThemeService);
  readonly palette = inject(CommandPaletteService);
  private readonly router = inject(Router);
  private readonly route = inject(ActivatedRoute);

  readonly navOpen = signal(false);
  readonly userMenuOpen = signal(false);
  readonly pageTitle = signal('Command Center');

  readonly sections: NavSection[] = [
    {
      label: '',
      items: [
        { path: '/', label: 'Command Center', icon: 'bi-grid-1x2', exact: true, visible: () => true },
        { path: '/agent', label: 'Research Agent', icon: 'bi-stars', visible: () => true },
      ],
    },
    {
      label: 'Automation',
      items: [
        { path: '/flows', label: 'Flows', icon: 'bi-diagram-3', visible: () => this.access.flowsRead() },
        { path: '/inbox', label: 'Inbox', icon: 'bi-inbox', visible: () => this.access.inbox(), badge: () => this.activity.inboxCount() },
      ],
    },
    {
      label: 'Markets',
      items: [
        { path: '/markets', label: 'Markets', icon: 'bi-graph-up-arrow', visible: () => true },
        { path: '/financials', label: 'Financials', icon: 'bi-bank', visible: () => true },
        { path: '/portfolio', label: 'Paper portfolio', icon: 'bi-wallet2', visible: () => this.access.liveMarkets() },
      ],
    },
    {
      label: 'Workspace',
      items: [
        { path: '/workspace', label: 'Files', icon: 'bi-folder2-open', visible: () => this.access.workspace() },
        { path: '/community', label: 'Community', icon: 'bi-chat-square-text', visible: () => this.access.liveMarkets() },
      ],
    },
  ];

  readonly footerItems: NavItem[] = [
    { path: '/notifications', label: 'Notifications', icon: 'bi-bell', visible: () => this.access.notifications() },
    { path: '/settings', label: 'Settings', icon: 'bi-gear', visible: () => this.access.settings() },
  ];

  readonly initials = computed(() => {
    const email = this.auth.user()?.email ?? '';
    return (email.split('@')[0] || 'FS').slice(0, 2).toUpperCase();
  });

  readonly statusTone = computed(() => {
    const label = this.market.statusLabel();
    if (label === 'Live') return 'is-success';
    if (label === 'Disconnected' || label === 'Stopped out') return 'is-danger';
    if (label === 'Research mode') return 'is-info';
    return 'is-warning';
  });

  readonly priceChange = computed(() => this.market.dayChange() ?? this.market.rangeChange());

  constructor() {
    this.market.init(this.access.liveMarkets() ? 'live' : 'research');
    void this.agent.loadConfig();
    if (this.access.inbox()) void this.activity.refreshInbox();
    const timer = setInterval(() => {
      if (this.access.inbox() && document.visibilityState === 'visible') void this.activity.refreshInbox();
    }, 60_000);
    inject(DestroyRef).onDestroy(() => clearInterval(timer));

    this.router.events.pipe(filter(e => e instanceof NavigationEnd)).subscribe(() => {
      let r = this.route.snapshot;
      while (r.firstChild) r = r.firstChild;
      this.pageTitle.set((r.data?.['title'] as string) ?? 'FloweySales');
      this.navOpen.set(false);
      this.userMenuOpen.set(false);
    });

    // Lock background scroll while the mobile navigation drawer is open.
    effect(() => document.body.classList.toggle('overflow-hidden', this.navOpen()));
  }

  signOut(): void {
    void this.auth.logout();
  }

  @HostListener('document:keydown', ['$event'])
  onKeydown(event: KeyboardEvent): void {
    const key = event.key.toLowerCase();
    if ((event.metaKey || event.ctrlKey) && key === 'k') {
      event.preventDefault();
      this.palette.toggle();
      return;
    }
    if ((event.metaKey || event.ctrlKey) && key === 'j') {
      event.preventDefault();
      this.agent.toggleDock();
      return;
    }
    if (key === 'escape') {
      this.navOpen.set(false);
      this.userMenuOpen.set(false);
    }
  }

  @HostListener('document:click', ['$event'])
  onDocumentClick(event: MouseEvent): void {
    if (!(event.target as Element | null)?.closest('.user-menu')) this.userMenuOpen.set(false);
  }
}
