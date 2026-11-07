import { ChangeDetectionStrategy, Component, DestroyRef, HostListener, computed, effect, inject, signal } from '@angular/core';
import { DecimalPipe } from '@angular/common';
import { ActivatedRoute, NavigationEnd, Router, RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { filter } from 'rxjs';
import { instrumentLabel } from '../core/instrument-ref';

import { AuthState } from '../auth.service';
import { Access } from '../core/access';
import { ActivityStore } from '../core/activity';
import { AgentSession } from '../core/agent-session';
import { MarketState } from '../core/market-state';
import { PluginTools } from '../core/plugin-tools';
import { tabIcon } from '../components/plugin-tools';
import { Workspace } from '../core/workspace.service';
import { ThemeService } from '../theme.service';
import { AgentDockComponent } from './agent-dock';
import { FeedStatusComponent } from './feed-status';
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
  imports: [RouterOutlet, RouterLink, RouterLinkActive, DecimalPipe, AgentDockComponent, CommandPaletteComponent, SymbolSearchComponent, ToastHostComponent, FeedStatusComponent],
  templateUrl: './shell.html',
  styleUrl: './shell.scss',
})
export class ShellComponent {
  readonly auth = inject(AuthState);
  readonly access = inject(Access);
  readonly market = inject(MarketState);
  readonly agent = inject(AgentSession);
  readonly activity = inject(ActivityStore);
  readonly pluginTools = inject(PluginTools);
  readonly theme = inject(ThemeService);
  readonly workspace = inject(Workspace);
  readonly palette = inject(CommandPaletteService);
  private readonly router = inject(Router);
  private readonly route = inject(ActivatedRoute);

  readonly navOpen = signal(false);
  readonly userMenuOpen = signal(false);
  /** Overflow menu for the low-frequency header actions on phone widths. */
  readonly moreOpen = signal(false);
  readonly pageTitle = signal('Command Center');
  /** Whether the current page follows the selected instrument (route `data.instrument`); unknown pages say nothing. */
  readonly instrumentUse = signal<'used' | 'ignored' | null>(null);
  /** Full-screen instrument picker, for narrow screens where the topbar search is hidden. */
  readonly pickerOpen = signal(false);

  readonly sections: NavSection[] = [
    {
      label: '',
      items: [
        { path: '/', label: 'Command Center', icon: 'bi-grid-1x2', exact: true, visible: () => true },
        { path: '/agent', label: 'Research Agent', icon: 'bi-stars', visible: () => true },
        { path: '/team', label: 'Agent team', icon: 'bi-people', visible: () => true },
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
        { path: '/strategies', label: 'Strategies', icon: 'bi-diagram-2', visible: () => true },
        { path: '/world', label: 'World', icon: 'bi-globe2', visible: () => true },
        { path: '/plan', label: 'Wealth plan', icon: 'bi-bullseye', visible: () => true },
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

  /** One entry per installed tools plugin (for example Tax). */
  readonly toolItems = computed<NavItem[]>(() => this.pluginTools.plugins().map(p => ({
    path: '/tools/' + encodeURIComponent(p.plugin_id), label: p.tools.tab.label, icon: tabIcon(p.tools.tab), visible: () => true,
  })));

  readonly footerItems: NavItem[] = [
    { path: '/help', label: 'Help', icon: 'bi-question-circle', visible: () => true },
    { path: '/about', label: 'About', icon: 'bi-info-circle', visible: () => true },
    { path: '/notifications', label: 'Notifications', icon: 'bi-bell', visible: () => this.access.notifications() },
    { path: '/settings', label: 'Settings', icon: 'bi-gear', visible: () => this.access.settings() },
  ];

  readonly initials = computed(() => {
    const email = this.auth.user()?.email ?? '';
    return (email.split('@')[0] || 'FS').slice(0, 2).toUpperCase();
  });

  /** Full name of the selected instrument, for the chip's tooltip and screen readers. */
  readonly instrumentTitle = computed(() => instrumentLabel({
    symbol: this.market.trackedSymbol(), provider: this.market.trackedProvider(),
    name: this.market.trackedName() || undefined, exchange: this.market.trackedExchange() || undefined,
  }));

  /** The chip's visible words in order; its accessible name must contain them (WCAG 2.5.3). */
  readonly chipVisibleText = computed(() => {
    const exch = this.market.trackedExchange();
    return [
      this.market.trackedSymbol(),
      this.market.trackedName(),
      `${exch ? exch + ' · ' : ''}${this.market.trackedProvider()}`,
      this.instrumentUse() === 'ignored' ? 'Not used on this page' : '',
    ].filter(Boolean).join(' ');
  });

  readonly priceChange = computed(() => this.market.dayChange() ?? this.market.rangeChange());

  constructor() {
    this.market.init(this.access.liveMarkets() ? 'live' : 'research');
    void this.agent.loadConfig();
    void this.pluginTools.refresh();
    if (this.access.inbox()) void this.activity.refreshInbox();
    const timer = setInterval(() => {
      if (this.access.inbox() && document.visibilityState === 'visible') void this.activity.refreshInbox();
    }, 60_000);
    inject(DestroyRef).onDestroy(() => clearInterval(timer));

    this.router.events.pipe(filter(e => e instanceof NavigationEnd)).subscribe(() => {
      let r = this.route.snapshot;
      while (r.firstChild) r = r.firstChild;
      const plugin = r.paramMap.get('plugin');
      const tab = plugin ? this.pluginTools.plugins().find(p => p.plugin_id === plugin)?.tools.tab.label : undefined;
      this.pageTitle.set(tab ?? (r.data?.['title'] as string) ?? 'FloweySales');
      const use = r.data?.['instrument'];
      this.instrumentUse.set(use === 'used' || use === 'ignored' ? use : null);
      this.navOpen.set(false);
      this.userMenuOpen.set(false);
      this.moreOpen.set(false);
      this.pickerOpen.set(false);
    });

    // Keep the selection in the address so a copied link opens the same instrument.
    effect(() => {
      const symbol = this.market.trackedSymbol();
      const provider = this.market.trackedProvider();
      if (!symbol || !this.router.url.split('?')[0].match(/^\/(markets|financials|strategies|portfolio)$/)) return;
      void this.router.navigate([], { queryParams: { symbol, provider }, queryParamsHandling: 'merge', replaceUrl: true });
    });

    // Lock background scroll while the mobile navigation drawer is open.
    effect(() => document.body.classList.toggle('overflow-hidden', this.navOpen() || this.pickerOpen()));
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
      this.moreOpen.set(false);
      this.pickerOpen.set(false);
    }
  }

  /** Phone layout (matches the 767.98px breakpoint in shell.scss), where the chip is shown without its details. */
  isNarrow(): boolean {
    try {
      return window.matchMedia('(max-width: 767.98px)').matches;
    } catch {
      return false;
    }
  }

  /** On narrow screens the chip opens the picker, which shows the full instrument identity and the favorite action. */
  onChipClick(event: Event): void {
    if (!this.isNarrow()) return;
    event.preventDefault();
    this.openPicker();
  }

  openPicker(): void {
    this.pickerOpen.set(true);
    // Focus the search once the sheet is in the DOM; the keyboard then opens on phones.
    setTimeout(() => document.querySelector<HTMLInputElement>('.picker-sheet input')?.focus());
  }

  @HostListener('document:click', ['$event'])
  onDocumentClick(event: MouseEvent): void {
    const target = event.target as Element | null;
    if (!target?.closest('.user-menu')) this.userMenuOpen.set(false);
    if (!target?.closest('.more-menu')) this.moreOpen.set(false);
  }
}
