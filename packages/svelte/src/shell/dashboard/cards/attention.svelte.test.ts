import { describe, expect, it } from 'vitest';

import type { Device } from '../../../domain';
import { fakeDevice } from '../../../test/fakes';
import { heldFleet } from '../fixtures';
import { FleetViews } from '../fleet-views.svelte';
import { needsPerson, viewOf } from '../printers';
import { inScope } from './scope';

const inState = (id: string, state: string): Device => ({
  ...fakeDevice(id),
  state: { '/state': state },
});

const withIssue: Device = {
  ...inState('runout', 'PRINTING'),
  issues: [
    { reason: 'filament_runout', severity: 'warning', detail: 'raw words' },
  ],
};

const withNotice: Device = {
  ...inState('klipper', 'IDLE'),
  issues: [
    {
      reason: 'profile_missing',
      severity: 'info',
      detail: 'No capability profile covered this model',
    },
  ],
};

const offline: Device = {
  ...withIssue,
  id: 'offline',
  connected: false,
};

const entry = {
  action: 'Load filament.',
  description: '',
  reason: 'filament_runout',
  severity: 'warning',
  summary: 'The filament ran out.',
};

describe('a printer that needs attention', () => {
  it('is one with an error, a question, a pause or a warning', () => {
    const asked = [
      inState('error', 'ERROR'),
      inState('attention', 'ATTENTION'),
      inState('paused', 'PAUSED'),
      withIssue,
    ].map((device) => viewOf(device));
    const left = [
      inState('finished', 'FINISHED'),
      inState('stopped', 'STOPPED'),
      inState('printing', 'PRINTING'),
      inState('idle', 'IDLE'),
      inState('ready', 'READY'),
      withNotice,
      offline,
    ].map((device) => viewOf(device));

    expect(asked.every(needsPerson)).toBe(true);
    expect(left.some(needsPerson)).toBe(false);
  });

  it('is the printer the attention scope of a card of many keeps', () => {
    const views = [
      inState('error', 'ERROR'),
      inState('finished', 'FINISHED'),
      inState('printing', 'PRINTING'),
      withIssue,
    ].map((device) => viewOf(device));

    for (const view of views) {
      expect(inScope(view, 'attention')).toBe(needsPerson(view));
    }
  });
});

describe('the issue a card shows', () => {
  it('reads in the words of the vocabulary the fleet views were given', () => {
    const views = new FleetViews(heldFleet([withIssue]), {
      entryOf: (reason) => (reason === entry.reason ? entry : undefined),
    });

    expect(views.view('runout')?.statusNote).toBe('The filament ran out.');
  });

  it('reads in the machine words without a vocabulary', () => {
    const views = new FleetViews(heldFleet([withIssue]));

    expect(views.view('runout')?.statusNote).toBe('raw words');
  });
});
