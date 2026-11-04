import { inject } from '@angular/core';
import { Router, type CanMatchFn, type Routes } from '@angular/router';

import { Access } from './core/access';

type Capability = 'flowsRead' | 'inbox' | 'liveMarkets' | 'workspace' | 'settings' | 'notifications';

/** Matches a route only when the session has the capability; otherwise goes home. */
function requires(capability: Capability): CanMatchFn {
  return () => inject(Access)[capability]() || inject(Router).parseUrl('/');
}

export const routes: Routes = [
  { path: '', pathMatch: 'full', title: 'Command Center · FloweySales', data: { title: 'Command Center', instrument: 'used' },
    loadComponent: () => import('./pages/command-center').then(m => m.CommandCenterPage) },
  { path: 'agent', title: 'Research Agent · FloweySales', data: { title: 'Research Agent', instrument: 'used' },
    loadComponent: () => import('./pages/agent-page').then(m => m.AgentPage) },
  { path: 'team', title: 'Agent team · FloweySales', data: { title: 'Agent team', instrument: 'used' },
    loadComponent: () => import('./pages/team-page').then(m => m.TeamPage) },
  { path: 'flows', title: 'Flows · FloweySales', data: { title: 'Flows', instrument: 'ignored' }, canMatch: [requires('flowsRead')],
    loadComponent: () => import('./pages/flows-page').then(m => m.FlowsPage) },
  { path: 'inbox', title: 'Inbox · FloweySales', data: { title: 'Inbox', instrument: 'ignored' }, canMatch: [requires('inbox')],
    loadComponent: () => import('./pages/inbox-page').then(m => m.InboxPage) },
  { path: 'markets', title: 'Markets · FloweySales', data: { title: 'Markets', instrument: 'used' },
    loadComponent: () => import('./pages/markets-page').then(m => m.MarketsPage) },
  { path: 'strategies', title: 'Strategies · FloweySales', data: { title: 'Strategies', instrument: 'used' },
    loadComponent: () => import('./pages/strategies-page').then(m => m.StrategiesPage) },
  { path: 'world', title: 'World · FloweySales', data: { title: 'World', instrument: 'used' },
    loadComponent: () => import('./pages/world-page').then(m => m.WorldPage) },
  { path: 'plan', title: 'Wealth plan · FloweySales', data: { title: 'Wealth plan', instrument: 'ignored' },
    loadComponent: () => import('./pages/plan-page').then(m => m.PlanPage) },
  { path: 'financials', title: 'Financials · FloweySales', data: { title: 'Financials', instrument: 'used' },
    loadComponent: () => import('./pages/financials-page').then(m => m.FinancialsPage) },
  { path: 'portfolio', title: 'Paper portfolio · FloweySales', data: { title: 'Paper portfolio', instrument: 'used' }, canMatch: [requires('liveMarkets')],
    loadComponent: () => import('./pages/portfolio-page').then(m => m.PortfolioPage) },
  { path: 'workspace', title: 'Workspace · FloweySales', data: { title: 'Workspace', instrument: 'ignored' }, canMatch: [requires('workspace')],
    loadComponent: () => import('./pages/workspace-page').then(m => m.WorkspacePage) },
  { path: 'community', title: 'Community · FloweySales', data: { title: 'Community', instrument: 'used' }, canMatch: [requires('liveMarkets')],
    loadComponent: () => import('./pages/community-page').then(m => m.CommunityPage) },
  { path: 'notifications', title: 'Notifications · FloweySales', data: { title: 'Notifications', instrument: 'ignored' }, canMatch: [requires('notifications')],
    loadComponent: () => import('./pages/notifications-page').then(m => m.NotificationsPage) },
  { path: 'settings', title: 'Settings · FloweySales', data: { title: 'Settings', instrument: 'ignored' }, canMatch: [requires('settings')],
    loadComponent: () => import('./pages/settings-page').then(m => m.SettingsPage) },
  { path: 'help', title: 'Help · FloweySales', data: { title: 'Help', instrument: 'ignored' },
    loadComponent: () => import('./pages/help-page').then(m => m.HelpPage) },
  { path: 'about', title: 'About · FloweySales', data: { title: 'About', instrument: 'ignored' },
    loadComponent: () => import('./pages/about-page').then(m => m.AboutPage) },
  { path: 'tools/:plugin', title: 'Tools · FloweySales', data: { title: 'Tools', instrument: 'ignored' },
    loadComponent: () => import('./pages/plugin-tools-page').then(m => m.PluginToolsPage) },
  { path: '**', redirectTo: '' },
];
