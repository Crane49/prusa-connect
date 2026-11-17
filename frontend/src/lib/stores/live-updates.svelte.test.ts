// The app's one event stream, when the server ends it because the credential
// that opened it stopped being accepted. A password change does that to the
// stream the person opened before they changed it.
import { describe, expect, it, vi } from 'vitest';

import {
  Fleet,
  type FleetIdentity,
  Notices,
  ORIGIN_WITHDRAWN,
} from '@prusa/connect-ui/data';

import { frame, json, type Stream, stream, stubClient } from '../../test/stub';
import { LiveUpdates } from './live-updates.svelte';

// The frame the core writes just before it closes a stream whose credential
// stopped being accepted.
const CREDENTIAL_ENDED = 'event: prusa-connect.auth.expired\ndata: {}\n\n';

const printer = {
  id: 'printer-1',
  name: 'Printer 1',
  kind: 'printer',
  labels: {},
  state: { status: 'IDLE' },
};

/** Who is signed in before and after the identity is read again. */
const identityOf = (before: string, after: string | undefined) => {
  let principal: { id: string } | undefined = { id: before };
  const identity: FleetIdentity = {
    get principal() {
      return principal;
    },
    read: () => {
      principal = after === undefined ? undefined : { id: after };

      return Promise.resolve();
    },
  };

  return identity;
};

/** A client whose event streams are handed out in order, and the streams. */
const built = () => {
  const opened: Stream[] = [];
  const { client, calls } = stubClient({
    'GET /api/v1/devices': json(200, { items: [printer] }),
    'GET /api/v1/events': () => {
      const next = stream();
      opened.push(next);

      return next.response;
    },
  });
  const fleet = new Fleet(client);

  return {
    fleet,
    opened,
    eventCalls: () =>
      calls.filter((call) => call.path === '/api/v1/events').length,
    eventQuery: () =>
      calls.find((call) => call.path === '/api/v1/events')?.query ?? '',
    client,
  };
};

/**
 * An identity a test moves by hand: `become` changes who is signed in at once,
 * the way another reader of the session can, and `hold` makes the next read
 * wait until the test lets it finish.
 */
const movableIdentity = (first: string) => {
  let principal: { id: string } | undefined = { id: first };
  let release: (() => void) | undefined;
  let held = false;
  const identity: FleetIdentity = {
    get principal() {
      return principal;
    },
    read: () => {
      if (!held) return Promise.resolve();

      return new Promise<void>((resolve) => {
        release = resolve;
      });
    },
  };

  return {
    identity,
    become: (id: string | undefined) => {
      principal = id === undefined ? undefined : { id };
    },
    hold: () => {
      held = true;
    },
    finish: () => {
      release?.();
    },
    reading: () => release !== undefined,
  };
};

// The add-printer page and the bell wait on the registration frames, and one
// stream serves the whole page (ADR-20260911-4p6 rule 23).
describe('the one stream the app opens', () => {
  it('asks for each registration type once, beside the fleet', async () => {
    const { client, fleet, eventQuery } = built();
    const live = new LiveUpdates(client, fleet);
    const close = live.open();

    await vi.waitFor(() => {
      expect(eventQuery()).not.toBe('');
    });
    const query = eventQuery();
    for (const kind of ['announced', 'approved', 'rejected', 'refused']) {
      const type = `com.prusa.connect.device.registration.${kind}`;
      expect(query.split(type).length - 1).toBe(1);
    }
    expect(query).toContain('com.prusa.connect.device.arrived');

    close();
  });
});

describe('the live updates after the server ends the stream', () => {
  it('opens it again for the same person and keeps the fleet', async () => {
    const { client, fleet, opened, eventCalls } = built();
    await fleet.load();

    const live = new LiveUpdates(
      client,
      fleet,
      identityOf('user:ada', 'user:ada'),
    );
    const stop = live.open();

    await expect.poll(() => opened.length).toBe(1);
    opened[0]?.push(CREDENTIAL_ENDED);

    await expect.poll(eventCalls).toBe(2);
    expect(live.error).toBeUndefined();
    expect(live.connection).toBe('online');
    expect(fleet.devices).toHaveLength(1);

    stop();
  });

  it('drops the fleet and opens nothing when somebody else signed in', async () => {
    const { client, fleet, opened, eventCalls } = built();
    await fleet.load();

    const live = new LiveUpdates(
      client,
      fleet,
      identityOf('user:ada', 'user:grace'),
    );
    const stop = live.open();

    await expect.poll(() => opened.length).toBe(1);
    opened[0]?.push(CREDENTIAL_ENDED);

    await expect.poll(() => fleet.devices.length).toBe(0);
    expect(eventCalls()).toBe(1);
    expect(live.error).toBeUndefined();
    expect(live.connection).toBe('offline');

    stop();
  });

  it('shows the error when it cannot tell who is signed in', async () => {
    const { client, fleet, opened, eventCalls } = built();
    await fleet.load();

    const live = new LiveUpdates(client, fleet);
    const stop = live.open();

    await expect.poll(() => opened.length).toBe(1);
    opened[0]?.push(CREDENTIAL_ENDED);

    await expect.poll(() => live.error).toBeDefined();
    expect(eventCalls()).toBe(1);
    expect(live.connection).toBe('offline');

    stop();
  });

  it('shows the error when the stream it opened again is ended the same way', async () => {
    const { client, fleet, opened } = built();
    await fleet.load();

    const live = new LiveUpdates(
      client,
      fleet,
      identityOf('user:ada', 'user:ada'),
    );
    const stop = live.open();

    await expect.poll(() => opened.length).toBe(1);
    opened[0]?.push(CREDENTIAL_ENDED);

    await expect.poll(() => opened.length).toBe(2);
    opened[1]?.push(CREDENTIAL_ENDED);

    await expect.poll(() => live.error).toBeDefined();
    expect(live.connection).toBe('offline');

    stop();
  });

  it('drops the fleet and opens nothing when nobody is signed in any more', async () => {
    const { client, fleet, opened, eventCalls } = built();
    await fleet.load();

    const live = new LiveUpdates(
      client,
      fleet,
      identityOf('user:ada', undefined),
    );
    const stop = live.open();

    await expect.poll(() => opened.length).toBe(1);
    opened[0]?.push(CREDENTIAL_ENDED);

    await expect.poll(() => fleet.devices.length).toBe(0);
    expect(eventCalls()).toBe(1);
    expect(live.error).toBeUndefined();
    expect(live.connection).toBe('offline');

    stop();
  });

  // Another tab signed in as somebody else, and then the connection dropped.
  // The SDK asks who the cookie belongs to before it reconnects, so the
  // stream does not carry on for the other person.
  it('opens nothing for another person after the connection drops', async () => {
    const opened: Stream[] = [];
    const { client, calls } = stubClient({
      'GET /api/v1/devices': json(200, { items: [printer] }),
      'GET /api/v1/principal': json(200, { principal: 'user:grace' }),
      'GET /api/v1/events': () => {
        const next = stream();
        opened.push(next);

        return next.response;
      },
    });
    const fleet = new Fleet(client);
    await fleet.load();

    const live = new LiveUpdates(
      client,
      fleet,
      identityOf('user:ada', 'user:grace'),
    );
    const stop = live.open();

    await expect.poll(() => opened.length).toBe(1);
    // A short retry, so the reconnection comes at once, then the drop.
    opened[0]?.push('retry: 1\n\n');
    opened[0]?.end();

    await expect.poll(() => fleet.devices.length).toBe(0);
    expect(
      calls.filter((call) => call.path === '/api/v1/principal'),
    ).toHaveLength(1);
    expect(opened).toHaveLength(1);
    expect(live.connection).toBe('offline');

    stop();
  });

  it('compares with who opened the stream, not with who a reader saw last', async () => {
    const { client, fleet, opened, eventCalls } = built();
    await fleet.load();

    const moving = movableIdentity('user:ada');
    const live = new LiveUpdates(client, fleet, moving.identity);
    const stop = live.open();
    await expect.poll(() => opened.length).toBe(1);

    // Another reader of the session saw the new person before the stream ended.
    moving.become('user:grace');
    opened[0]?.push(CREDENTIAL_ENDED);

    await expect.poll(() => fleet.devices.length).toBe(0);
    expect(eventCalls()).toBe(1);

    stop();
  });

  it('opens nothing when the stream was closed while the identity was read', async () => {
    const { client, fleet, opened, eventCalls } = built();
    await fleet.load();

    const moving = movableIdentity('user:ada');
    moving.hold();
    const live = new LiveUpdates(client, fleet, moving.identity);
    const stop = live.open();
    await expect.poll(() => opened.length).toBe(1);

    opened[0]?.push(CREDENTIAL_ENDED);
    await expect.poll(moving.reading).toBe(true);

    stop();
    moving.finish();
    await new Promise((resolve) => setTimeout(resolve, 50));

    expect(eventCalls()).toBe(1);
    expect(live.connection).toBe('offline');
    expect(fleet.devices).toHaveLength(1);
  });
});

describe('a withdrawn origin on the stream', () => {
  const withdrawn = (id: string, origin: string): string =>
    `id: ${id}\nevent: ${ORIGIN_WITHDRAWN}\ndata: ${JSON.stringify({ origin })}\n\n`;

  it('asks the server for the withdrawn frame', async () => {
    const { client, fleet, eventQuery } = built();
    const stop = new LiveUpdates(client, fleet).open();

    await vi.waitFor(() => {
      expect(eventQuery()).toContain(ORIGIN_WITHDRAWN);
    });

    stop();
  });

  it('routes a withdrawn origin by its kind', async () => {
    const { client, fleet, opened } = built();
    await fleet.load();
    const onResource = vi.fn();
    const onDashboards = vi.fn();
    const live = new LiveUpdates(
      client,
      fleet,
      undefined,
      onResource,
      undefined,
      onDashboards,
    );
    const stop = live.open();

    await expect.poll(() => opened.length).toBe(1);
    opened[0]?.push(withdrawn('1', 'dashboard:operators'));
    opened[0]?.push(withdrawn('2', 'module:parts'));
    opened[0]?.push(withdrawn('3', `device:${printer.id}`));

    await expect.poll(() => fleet.devices.length).toBe(0);
    expect(onDashboards).toHaveBeenCalledTimes(1);
    expect(onResource).toHaveBeenCalledTimes(1);

    stop();
  });
});

describe('a resource state change on the stream', () => {
  it('goes to its listener and not to the fleet', async () => {
    const { client, fleet, opened } = built();
    await fleet.load();
    const apply = vi.spyOn(fleet, 'apply');
    const onResource = vi.fn();

    const live = new LiveUpdates(
      client,
      fleet,
      identityOf('user:ada', 'user:ada'),
      onResource,
    );
    const stop = live.open();

    await expect.poll(() => opened.length).toBe(1);
    opened[0]?.push(
      frame('1', 'com.prusa.connect.resource.state.changed', {
        data: {
          resource: 'asset:fleet-stats',
          state: 'stopped',
          serving: false,
        },
      }),
    );

    await expect.poll(() => onResource.mock.calls.length).toBe(1);
    expect(apply).not.toHaveBeenCalled();

    stop();
  });

  it('tells the bell of a switch-off once and of no other change', async () => {
    const { client, fleet, opened } = built();
    await fleet.load();
    const apply = vi.spyOn(fleet, 'apply');
    const notices = new Notices();
    notices.watch(fleet);
    const onResource = vi.fn();

    const live = new LiveUpdates(
      client,
      fleet,
      identityOf('user:ada', 'user:ada'),
      onResource,
    );
    const stop = live.open();

    await expect.poll(() => opened.length).toBe(1);
    const change = (id: string, result: string) =>
      frame(id, 'com.prusa.connect.resource.state.changed', {
        data: { resource: 'sink/spools', state: 'stopped', result },
      });
    opened[0]?.push(change('1', 'over-memory'));
    opened[0]?.push(change('2', 'switched-off-by-host'));
    opened[0]?.push(change('3', 'switched-off-by-host'));

    await expect.poll(() => onResource.mock.calls.length).toBe(3);
    expect(apply).toHaveBeenCalledTimes(2);
    expect(notices.items).toHaveLength(1);
    expect(notices.items[0]).toMatchObject({
      kind: 'module-switched-off',
      module: 'spools',
    });

    stop();
  });
});

// A server that restarts, as after an applied update, comes back with a new
// log. Whatever the listeners would have heard in the gap is gone, so each
// reads its own state again.
describe('a lost place in the log', () => {
  it('goes to every listener and to the fleet', async () => {
    const { client, fleet, opened } = built();
    await fleet.load();
    const apply = vi.spyOn(fleet, 'apply');
    const onResource = vi.fn();
    const onMaintenance = vi.fn();
    const onDashboards = vi.fn();

    const live = new LiveUpdates(
      client,
      fleet,
      identityOf('user:ada', 'user:ada'),
      onResource,
      onMaintenance,
      onDashboards,
    );
    const stop = live.open();

    await expect.poll(() => opened.length).toBe(1);
    opened[0]?.push(
      frame('1', 'com.prusa.connect.stream.continuity.lost', { data: {} }),
    );

    await expect.poll(() => onMaintenance.mock.calls.length).toBe(1);
    expect(onResource).toHaveBeenCalledOnce();
    expect(onDashboards).toHaveBeenCalledOnce();
    expect(apply).toHaveBeenCalledOnce();

    stop();
  });
});
