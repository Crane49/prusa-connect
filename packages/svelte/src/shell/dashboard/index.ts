// The dashboard screen and what the application builds its context from
// (ADR-20261008-sww). It is its own subpath, `./dashboard`, so the application loads
// it with a dynamic import and a page without a dashboard loads none of it
// (rule 25).
export { CardCatalogue, type CardKind } from './card-catalogue';
export { CameraFrames } from './cameras.svelte';
export { registerCoreCards } from './cards';
export { browserTicker, Clock, type Ticker } from './clock.svelte';
export { type DashboardContext, type DashboardNavigation } from './context';
export { default as DashboardScreen } from './dashboard-screen.svelte';
export { FleetViews } from './fleet-views.svelte';
