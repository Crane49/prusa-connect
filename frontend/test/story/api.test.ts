import { afterEach, describe, expect, it } from 'vitest';

import { fleetOf } from '../../.storybook/fleet';
import { emptyList, installApi } from './api';

let restore = (): void => undefined;

afterEach(() => {
  restore();
});

describe('the story core', () => {
  it('lists the printers of the chosen fleet size', async () => {
    restore = installApi('two');
    const body = (await (await fetch('/api/v1/devices')).json()) as {
      items: unknown[];
    };
    expect(body.items).toHaveLength(2);
  });

  it('answers a story row over the table', async () => {
    restore = installApi('many', { 'GET /api/v1/jobs': emptyList() });
    const first = (await (await fetch('/api/v1/jobs')).json()) as {
      total: number;
    };
    const again = await fetch('/api/v1/jobs');
    expect(first.total).toBe(0);
    expect(again.status).toBe(200);
  });

  it('answers a recorded row that carries its query', async () => {
    restore = installApi('many');
    const answer = await fetch(
      '/api/v1/storages/local/files?path=q3_rush_jobs',
    );
    expect(answer.status).toBe(200);
  });

  it('answers a copied printer from the rows of the one it copies', async () => {
    restore = installApi('many');
    const copy = fleetOf('many')[5]?.id ?? '';
    const answer = await fetch(`/api/v1/devices/${copy}/jobs/history`);
    const body = (await answer.json()) as { items: { device_id: string }[] };
    expect(body.items.length).toBeGreaterThan(0);
    expect(body.items.every((job) => job.device_id === copy)).toBe(true);
  });

  it('answers 404 for a request the table does not name', async () => {
    restore = installApi('many');
    expect((await fetch('/api/v1/nowhere')).status).toBe(404);
  });

  it('puts the real fetch back', () => {
    const original = globalThis.fetch;
    installApi('one')();
    expect(globalThis.fetch).toBe(original);
  });

  it('puts the real fetch back after two installs', () => {
    const original = globalThis.fetch;
    installApi('one');
    installApi('two')();
    expect(globalThis.fetch).toBe(original);
  });

  it('holds the event stream open', async () => {
    restore = installApi('many');
    const events = await fetch('/api/v1/events');
    expect(events.status).toBe(200);
    expect(events.headers.get('content-type')).toBe('text/event-stream');

    const reader = events.body?.getReader();
    const read = reader?.read().then(() => 'closed');
    const waited = new Promise((resolve) => setTimeout(resolve, 100, 'open'));
    expect(await Promise.race([read, waited])).toBe('open');
  });
});
