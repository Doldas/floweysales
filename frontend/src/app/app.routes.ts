import { inject } from '@angular/core';
import { Router, type CanMatchFn, type Routes } from '@angular/router';

import { Access } from './core/access';

type Capability = 'flowsRead' | 'inbox' | 'liveMarkets' | 'workspace' | 'settings' | 'notifications';

/** Matches a route only when the session has the capability; otherwise goes home. */
function requires(capability: Capability): CanMatchFn {
  return () => inject(Access)[capability]() || inject(Router).parseUrl('/');
}

export const routes: Routes = [
  { path: '', pathMatch: 'full', title: 'Command Center · FloweySales', data: { title: 'Command Center' },
    loadComponent: () => import('./pages/command-center').then(m => m.CommandCenterPage) },
  { path: 'agent', title: 'Research Agent · FloweySales', data: { title: 'Research Agent' },
    loadComponent: () => import('./pages/agent-page').then(m => m.AgentPage) },
  { path: 'flows', title: 'Flows · FloweySales', data: { title: 'Flows' }, canMatch: [requires('flowsRead')],
    loadComponent: () => import('./pages/flows-page').then(m => m.FlowsPage) },
  { path: 'inbox', title: 'Inbox · FloweySales', data: { title: 'Inbox' }, canMatch: [requires('inbox')],
    loadComponent: () => import('./pages/inbox-page').then(m => m.InboxPage) },
  { path: 'markets', title: 'Markets · FloweySales', data: { title: 'Markets' },
    loadComponent: () => import('./pages/markets-page').then(m => m.MarketsPage) },
  { path: 'plan', title: 'Wealth plan · FloweySales', data: { title: 'Wealth plan' },
    loadComponent: () => import('./pages/plan-page').then(m => m.PlanPage) },
  { path: 'financials', title: 'Financials · FloweySales', data: { title: 'Financials' },
    loadComponent: () => import('./pages/financials-page').then(m => m.FinancialsPage) },
  { path: 'portfolio', title: 'Paper portfolio · FloweySales', data: { title: 'Paper portfolio' }, canMatch: [requires('liveMarkets')],
    loadComponent: () => import('./pages/portfolio-page').then(m => m.PortfolioPage) },
  { path: 'workspace', title: 'Workspace · FloweySales', data: { title: 'Workspace' }, canMatch: [requires('workspace')],
    loadComponent: () => import('./pages/workspace-page').then(m => m.WorkspacePage) },
  { path: 'community', title: 'Community · FloweySales', data: { title: 'Community' }, canMatch: [requires('liveMarkets')],
    loadComponent: () => import('./pages/community-page').then(m => m.CommunityPage) },
  { path: 'notifications', title: 'Notifications · FloweySales', data: { title: 'Notifications' }, canMatch: [requires('notifications')],
    loadComponent: () => import('./pages/notifications-page').then(m => m.NotificationsPage) },
  { path: 'settings', title: 'Settings · FloweySales', data: { title: 'Settings' }, canMatch: [requires('settings')],
    loadComponent: () => import('./pages/settings-page').then(m => m.SettingsPage) },
  { path: '**', redirectTo: '' },
];
