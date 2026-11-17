// The screens the lane measures, and the shift each one is allowed.
//
// Earlier research: nothing on a page moves when something loads or appears.
// The ceiling is 0.02 on every screen. The lane also sweeps every one of them
// for contrast, so a route added here is measured twice.
//
// One line per destination the rail offers. A screen mounted without a line
// here is a screen nothing measures, so the line and the screen land in the
// same commit.

export interface UxRoute {
  readonly name: string;
  readonly path: string;
  /** Cumulative layout shift from the first paint until the screen settles. */
  readonly ceiling: number;
}

export const ROUTES: readonly UxRoute[] = [
  // The home page, which draws the default dashboard (ADR-20261008-sww).
  { name: 'app', path: '/', ceiling: 0.02 },
  { name: 'printers', path: '/printers', ceiling: 0.02 },
  { name: 'jobs', path: '/jobs', ceiling: 0.02 },
  // One print, at the address the jobs listing links to. The id is the
  // print's own, which the page reads at GET /api/v1/jobs/{job_id}.
  {
    name: 'job',
    path: '/jobs/49b4ad5f-9db1-4930-8fe6-2aefa3c1f7a0',
    ceiling: 0.02,
  },
  { name: 'storage', path: '/storage', ceiling: 0.02 },
  { name: 'tags', path: '/tags', ceiling: 0.02 },
  { name: 'account', path: '/settings/account', ceiling: 0.02 },
  { name: 'tokens', path: '/settings/tokens', ceiling: 0.02 },
  { name: 'people', path: '/settings/people', ceiling: 0.02 },
];
