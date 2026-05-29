import type { RouteConfig } from './lib/router.ts';

// Route table. Each page module self-registers a custom element on import.
export const routes: RouteConfig[] = [
  { path: '/', redirect: '/dispatch', layout: 'app' },
  {
    path: '/dispatch',
    layout: 'app',
    load: () => import('./pages/DispatchBoard.ts'),
  },
  {
    path: '/load/:planId',
    layout: 'app',
    load: () => import('./pages/YardLoadView.ts'),
  },
  {
    path: '/load',
    layout: 'app',
    load: () => import('./pages/YardLoadView.ts'),
  },
  {
    path: '/fleet',
    layout: 'app',
    load: () => import('./pages/FleetProfiles.ts'),
  },
  {
    path: '/compliance',
    layout: 'app',
    load: () => import('./pages/CompliancePoints.ts'),
  },
];
