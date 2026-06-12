import type { RouteConfig } from './lib/router.ts';

// Route table. Each page module self-registers a custom element on import.
// The old separate Dispatch Board + Load Builder pages are merged into the
// single guided /plan workflow; their paths redirect for old bookmarks.
export const routes: RouteConfig[] = [
  { path: '/', redirect: '/plan', layout: 'app' },
  { path: '/dispatch', redirect: '/plan', layout: 'app' },
  { path: '/load', redirect: '/plan', layout: 'app' },
  {
    path: '/plan',
    layout: 'app',
    load: () => import('./pages/PlanWorkflow.ts'),
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
