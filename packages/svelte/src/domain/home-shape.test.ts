import { describe, expect, it } from 'vitest';

import { homeOn, homeShapeFor } from './home-shape';

describe('homeShapeFor', () => {
  it('reads the count when the reader has stated no preference', () => {
    expect(homeShapeFor({ printerCount: 1 })).toBe('single');
    expect(homeShapeFor({ printerCount: 2 })).toBe('pair');
    expect(homeShapeFor({ printerCount: 3 })).toBe('fleet');
    expect(homeShapeFor({ printerCount: 40 })).toBe('fleet');
  });

  it('is a fleet with nothing on it, whatever the preference', () => {
    // An instance with no printers cannot honour `my-printer`, and the fleet
    // shape is the only one with an empty state to draw.
    expect(homeShapeFor({ printerCount: 0, preference: 'my-printer' })).toBe(
      'fleet',
    );
    expect(homeShapeFor({ printerCount: 0, preference: 'auto' })).toBe('fleet');
  });

  it('honours a stated preference over the count', () => {
    expect(homeShapeFor({ printerCount: 1, preference: 'all-printers' })).toBe(
      'fleet',
    );
    expect(homeShapeFor({ printerCount: 9, preference: 'my-printer' })).toBe(
      'single',
    );
  });

  // The dashboard is the home page, so the printers page is the plain list.
  it('is the fleet when the dashboard is the home page', () => {
    expect(homeShapeFor({ printerCount: 1, preference: 'dashboard' })).toBe(
      'fleet',
    );
  });
});

describe('homeOn', () => {
  it('reads the dashboard as automatic on a server without dashboards', () => {
    expect(homeOn('dashboard', false)).toBe('auto');
    expect(homeOn('dashboard', true)).toBe('dashboard');
  });

  it('keeps every other choice as it is', () => {
    expect(homeOn('my-printer', false)).toBe('my-printer');
    expect(homeOn('all-printers', false)).toBe('all-printers');
  });
});
