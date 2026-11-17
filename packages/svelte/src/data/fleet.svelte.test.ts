import type {
  EventSubscription,
  EventsOptions,
  StreamEvent,
} from '@prusa/connect';
import { describe, expect, it } from 'vitest';

import { FAKE_PAGE_REST, fakeDevice, fakeFleetClient } from '../test/fakes';
import {
  createFleet,
  FLEET_JOB_EVENTS,
  FLEET_STREAM_EVENTS,
  FLEET_STREAM_INTERVAL_MS,
  type FleetClient,
  type FleetIdentity,
  ORIGIN_WITHDRAWN,
} from './fleet.svelte';

const CHANGED = 'com.prusa.connect.device.record.changed';
const SAMPLED = 'com.prusa.connect.device.telemetry.sampled';

const envelopeOf = (deviceId: string | undefined) => ({
  specversion: '1.0',
  id: '1',
  source: `urn:prusa:connect:printer/${deviceId ?? 'none'}`,
  type: CHANGED,
  time: '2026-09-11T00:00:00Z',
  ...(deviceId === undefined ? {} : { subject: `device:${deviceId}` }),
  hops: 0,
  evclass: 'state',
});

const recordChanged = (deviceId: string): StreamEvent => ({
  type: CHANGED,
  id: '1',
  payload: { device_id: deviceId },
  envelope: envelopeOf(deviceId),
});

/** A frame that names no device: no subject, and no `device_id` in the payload. */
const unnamed: StreamEvent = {
  type: CHANGED,
  id: '1',
  payload: {},
  envelope: envelopeOf(undefined),
};

/** One reading off the state tree, as the core publishes it. */
const sampled = (
  deviceId: string,
  path: string,
  value: number | string | readonly unknown[] | null,
): StreamEvent => ({
  type: SAMPLED,
  id: '2',
  payload: { path, value },
  envelope: { ...envelopeOf(deviceId), type: SAMPLED, evclass: 'telemetry' },
});

/**
 * A stream a test drives by hand: `push` hands the store one frame, and
 * `end` closes the loop that is reading them. It is the whole of what the SDK
 * gives the store, because a subscription is an async iterable and a
 * `close()` and nothing else.
 */
const fakeStream = () => {
  const waiting: ((event: StreamEvent | undefined) => void)[] = [];
  const queue: StreamEvent[] = [];
  let open = true;

  const next = (): Promise<StreamEvent | undefined> => {
    const held = queue.shift();
    if (held !== undefined) return Promise.resolve(held);
    if (!open) return Promise.resolve(undefined);

    return new Promise((resolve) => waiting.push(resolve));
  };

  const deliver = (event: StreamEvent | undefined): void => {
    const first = waiting.shift();
    if (first === undefined && event !== undefined) {
      queue.push(event);

      return;
    }
    first?.(event);
  };

  const subscription: EventSubscription = {
    lastEventId: undefined,
    close: () => {
      open = false;
      deliver(undefined);
    },
    [Symbol.asyncIterator]: async function* () {
      for (
        let event = await next();
        event !== undefined;
        event = await next()
      ) {
        yield event;
      }
    },
  };

  return {
    subscription,
    push: (event: StreamEvent) => {
      deliver(event);
    },
    end: () => {
      subscription.close();
    },
  };
};

/** A client whose stream a test drives, over the rows it was given. */
const fakeStreamingClient = (
  rows: readonly ReturnType<typeof fakeDevice>[],
) => {
  const stream = fakeStream();
  const base = fakeFleetClient(rows);
  const asked: EventsOptions[] = [];
  // The narrowing cast is what lets one fake stand in for a generic
  // operation: this fake answers the same subscription whatever types are
  // asked for, and the store reads no type off it (typescript.md).
  const events = ((options: EventsOptions) => {
    asked.push(options);

    return stream.subscription;
  }) as FleetClient['events'];
  const client: FleetClient = { ...base.client, events };

  return { ...stream, asked, calls: base.calls, client };
};

/** Waits for the store to have read everything the stream has handed it. */
const settle = (): Promise<void> =>
  new Promise((resolve) => {
    setTimeout(resolve, 0);
  });

describe('Fleet', () => {
  it('starts empty, and an unread fleet is not an empty one', () => {
    const fleet = createFleet(fakeFleetClient([]).client);

    expect(fleet.devices).toEqual([]);
    expect(fleet.loaded).toBe(false);
    expect(fleet.loading).toBe(false);
    expect(fleet.error).toBeUndefined();
  });

  it('reads the fleet and tells an empty answer from an unread one', async () => {
    const { client } = fakeFleetClient([]);
    const fleet = createFleet(client);

    await fleet.load();

    expect(fleet.devices).toEqual([]);
    expect(fleet.loaded).toBe(true);
  });

  // svelte-data.md: two screens drawing the fleet cost one request.
  it('costs one request however many readers ask at once', async () => {
    const { client, calls } = fakeFleetClient([fakeDevice('printer-1')]);
    const fleet = createFleet(client);

    await Promise.all([fleet.load(), fleet.load(), fleet.load()]);

    expect(calls.list).toBe(1);
    expect(fleet.devices).toHaveLength(1);
  });

  it('reads every page of a fleet larger than one', async () => {
    const rows = Array.from({ length: 120 }, (_, index) =>
      fakeDevice(`printer-${String(index)}`),
    );
    const asked: { offset: number; sort: string | undefined }[] = [];
    const client: FleetClient = {
      ...fakeFleetClient([]).client,
      listDevices: (request) => {
        const offset = request?.query?.offset ?? 0;
        const limit = request?.query?.limit ?? 50;
        asked.push({ offset, sort: request?.query?.sort });

        return Promise.resolve({
          ...FAKE_PAGE_REST,
          items: rows.slice(offset, offset + 50),
          limit,
          offset,
          total: rows.length,
        });
      },
    };
    const fleet = createFleet(client);

    await fleet.load();

    expect(fleet.devices).toHaveLength(120);
    expect(asked.map((page) => page.offset)).toEqual([0, 50, 100]);
    expect(asked.every((page) => page.sort === 'id')).toBe(true);
  });

  it('stops at a short page when the server sends no total', async () => {
    let calls = 0;
    const client: FleetClient = {
      ...fakeFleetClient([]).client,
      listDevices: () => {
        calls += 1;

        return Promise.resolve({
          items: [fakeDevice('a'), fakeDevice('b'), fakeDevice('c')],
        } as Awaited<ReturnType<FleetClient['listDevices']>>);
      },
    };
    const fleet = createFleet(client);

    await fleet.load();

    expect(fleet.devices).toHaveLength(3);
    expect(calls).toBe(1);
  });

  it('tells a listener each event it applied, until it stops', async () => {
    const { client } = fakeFleetClient([fakeDevice('printer-1')]);
    const fleet = createFleet(client);
    const heard: string[] = [];
    const stop = fleet.subscribe((event) => heard.push(event.type));

    await fleet.apply(recordChanged('printer-1'));
    await fleet.apply(unnamed);
    stop();
    await fleet.apply(recordChanged('printer-1'));

    expect(heard).toEqual([CHANGED, CHANGED]);
  });

  it('drops a withdrawn printer and tells its listeners', async () => {
    const { client, calls } = fakeFleetClient([
      fakeDevice('printer-1', 'One'),
      fakeDevice('printer-2', 'Two'),
    ]);
    const fleet = createFleet(client);
    await fleet.load();
    const heard: string[] = [];
    fleet.subscribe((event) => heard.push(event.type));

    await fleet.apply({
      type: ORIGIN_WITHDRAWN,
      id: '3',
      payload: { origin: 'device:printer-1' },
      envelope: undefined,
    });

    expect(fleet.devices.map((row) => row.id)).toEqual(['printer-2']);
    expect(calls.get).toEqual([]);
    expect(heard).toEqual([ORIGIN_WITHDRAWN]);
  });

  it('applies a record-changed event by replacing that row alone', async () => {
    const rows = [
      fakeDevice('printer-1', 'One'),
      fakeDevice('printer-2', 'Two'),
    ];
    const { client, calls } = fakeFleetClient(rows);
    const fleet = createFleet(client);
    await fleet.load();
    const before = fleet.devices;

    await fleet.apply(recordChanged('printer-2'));

    expect(calls.get).toEqual(['printer-2']);
    expect(fleet.device('printer-2')?.name).toBe('Two');
    expect(fleet.devices).toHaveLength(2);
    // The collection is replaced wholesale, which is one recomputation and
    // not one per row (svelte-5.md).
    expect(fleet.devices).not.toBe(before);
  });

  it('finds a printer by its id after the effect that built the store has ended', async () => {
    const { client } = fakeFleetClient([fakeDevice('printer-1')]);
    let fleet: ReturnType<typeof createFleet> | undefined;
    const stop = $effect.root(() => {
      fleet = createFleet(client);
      $effect(() => {
        void fleet?.device('printer-1');
      });
    });
    await fleet?.load();
    stop();

    await fleet?.apply(sampled('printer-1', '/job/progress', 42));

    expect(fleet?.device('printer-1')?.job?.progress).toBe(42);
  });

  it('finds the row every write updates when two rows share an id', async () => {
    const { client } = fakeFleetClient([
      fakeDevice('printer-1', 'First'),
      fakeDevice('printer-1', 'Second'),
    ]);
    const fleet = createFleet(client);
    await fleet.load();

    await fleet.apply(sampled('printer-1', '/job/progress', 42));

    expect(fleet.device('printer-1')?.name).toBe('First');
    expect(fleet.device('printer-1')?.job?.progress).toBe(42);
  });

  it('adds a printer the fleet had not seen', async () => {
    const { client } = fakeFleetClient([fakeDevice('printer-9')]);
    const fleet = createFleet(client);

    await fleet.apply(recordChanged('printer-9'));

    expect(fleet.devices.map((row) => row.id)).toEqual(['printer-9']);
  });

  it('ignores an event that names no printer', async () => {
    const { client, calls } = fakeFleetClient([]);
    const fleet = createFleet(client);

    await fleet.apply(unnamed);

    expect(calls.get).toEqual([]);
  });

  // An approval names the device it bound before the printer has dialled in,
  // and the record may not exist yet. The fleet learns of the device from its
  // arrival instead, and still tells its listeners about the frame.
  it('asks for nothing on a registration frame and still passes it on', async () => {
    const { client, calls } = fakeFleetClient([]);
    const fleet = createFleet(client);
    const heard: string[] = [];
    fleet.subscribe((event) => heard.push(event.type));

    const approved: StreamEvent = {
      type: 'com.prusa.connect.device.registration.approved',
      id: '1',
      payload: { code: 'a1', device_id: 'printer-9' },
      envelope: {
        ...envelopeOf(undefined),
        source: 'urn:prusa:connect:driver/xbuddy',
      },
    };
    await fleet.apply(approved);

    expect(calls.get).toEqual([]);
    expect(heard).toEqual(['com.prusa.connect.device.registration.approved']);
  });

  // A file on its way changes nothing the printer listing answers, so the
  // fleet reads no row for it and passes it to the header indicator.
  it('asks for nothing on a transfer frame and still passes it on', async () => {
    const { client, calls } = fakeFleetClient([fakeDevice('printer-1')]);
    const fleet = createFleet(client);
    const heard: string[] = [];
    fleet.subscribe((event) => heard.push(event.type));

    const progressed: StreamEvent = {
      type: 'com.prusa.connect.device.transfer.progressed',
      id: '1',
      payload: { transfer_id: 't-1', bytes_sent: 10, bytes_total: 100 },
      envelope: envelopeOf('printer-1'),
    };
    await fleet.apply(progressed);

    expect(calls.get).toEqual([]);
    expect(heard).toEqual(['com.prusa.connect.device.transfer.progressed']);
  });

  // A folder read changes nothing the printer listing answers either, and a
  // large drive sends one such frame per folder it reads.
  it('asks for nothing on a files frame and still passes it on', async () => {
    const { client, calls } = fakeFleetClient([fakeDevice('printer-1')]);
    const fleet = createFleet(client);
    const heard: string[] = [];
    fleet.subscribe((event) => heard.push(event.type));

    await fleet.apply({
      type: 'com.prusa.connect.device.files.changed',
      id: '1',
      payload: { path: 'usb', reading: false },
      envelope: envelopeOf('printer-1'),
    });

    expect(calls.get).toEqual([]);
    expect(heard).toEqual(['com.prusa.connect.device.files.changed']);
  });

  // A camera frame's subject names the camera, which no printer is.
  it('asks for nothing on a camera frame and still passes it on', async () => {
    const { client, calls } = fakeFleetClient([fakeDevice('printer-1')]);
    const fleet = createFleet(client);
    const heard: string[] = [];
    fleet.subscribe((event) => heard.push(event.type));

    await fleet.apply({
      type: 'com.prusa.connect.camera.frame.received',
      id: '1',
      payload: {},
      envelope: envelopeOf('cam-1'),
    });

    expect(calls.get).toEqual([]);
    expect(fleet.error).toBeUndefined();
    expect(heard).toEqual(['com.prusa.connect.camera.frame.received']);
  });

  it('reads the whole fleet again when its place in the log is gone', async () => {
    const { client, calls } = fakeFleetClient([fakeDevice('printer-1')]);
    const fleet = createFleet(client);

    await fleet.apply({
      type: 'com.prusa.connect.stream.continuity.lost',
      id: '9',
      payload: { cursor: '9', dropped: 3 },
      envelope: undefined,
    });

    expect(calls.list).toBe(1);
    expect(fleet.loaded).toBe(true);
  });

  // A fleet card draws the print on the bed, so the store has to keep it
  // current between two reads of the listing.
  it('moves a print on the row it names, and asks the API for nothing', async () => {
    const { client, calls } = fakeFleetClient([fakeDevice('printer-1')]);
    const fleet = createFleet(client);
    await fleet.load();

    await fleet.apply(sampled('printer-1', '/job/display', 'bracket.bgcode'));
    await fleet.apply(sampled('printer-1', '/job/progress', 42));
    await fleet.apply(sampled('printer-1', '/job/time_remaining', 900));
    await fleet.apply(sampled('printer-1', '/job/state', 'printing'));
    await fleet.apply(sampled('printer-1', '/state', 'PRINTING'));

    const row = fleet.device('printer-1');
    expect(row?.job?.display_name).toBe('bracket.bgcode');
    expect(row?.job?.progress).toBe(42);
    expect(row?.job?.time_remaining_ms).toBe(900_000);
    expect(row?.job?.state).toBe('printing');
    expect(row?.state['/state']).toBe('PRINTING');
    // A refetch per frame is the cost `live-telemetry.svelte.ts` refuses.
    expect(calls.get).toEqual([]);
  });

  it('drops a reading the fleet list does not draw', async () => {
    const { client } = fakeFleetClient([fakeDevice('printer-1')]);
    const fleet = createFleet(client);
    await fleet.load();
    const before = fleet.devices;

    await fleet.apply(sampled('printer-1', '/tools/0/nozzle_temp', 214.8));

    expect(fleet.devices).toBe(before);
  });

  // The dialog is a state-tree node like the print, and the core removes it
  // with a null once the machine stops waiting.
  it('raises a dialog on the row and removes it on a null', async () => {
    const { client } = fakeFleetClient([fakeDevice('printer-1')]);
    const fleet = createFleet(client);
    await fleet.load();

    await fleet.apply(sampled('printer-1', '/dialog/id', 7));
    await fleet.apply(sampled('printer-1', '/dialog/buttons', ['Retry', 1]));
    expect(fleet.device('printer-1')?.state['/dialog/buttons']).toBeUndefined();

    await fleet.apply(sampled('printer-1', '/dialog/buttons', ['Retry']));
    expect(fleet.device('printer-1')?.state).toMatchObject({
      '/dialog/id': 7,
      '/dialog/buttons': ['Retry'],
    });

    await fleet.apply(sampled('printer-1', '/dialog/id', null));
    expect(fleet.device('printer-1')?.state).not.toHaveProperty('/dialog/id');
  });

  it('ignores a print on a printer this caller cannot see', async () => {
    const { client, calls } = fakeFleetClient([fakeDevice('printer-1')]);
    const fleet = createFleet(client);
    await fleet.load();

    await fleet.apply(sampled('printer-9', '/job/progress', 12));

    expect(fleet.devices).toHaveLength(1);
    expect(calls.get).toEqual([]);
  });

  it('asks the stream for the prints as well as the rows, folded over a window', () => {
    const { client, asked } = fakeStreamingClient([]);
    const fleet = createFleet(client);

    const stop = fleet.listen();

    expect(asked[0]?.types).toEqual(FLEET_STREAM_EVENTS);
    expect(asked[0]?.types).toEqual(
      expect.arrayContaining([...FLEET_JOB_EVENTS]),
    );
    expect(asked[0]?.interval).toBe(FLEET_STREAM_INTERVAL_MS);
    stop();
  });

  it('applies what the stream hands it while it is open', async () => {
    const { client, push, end } = fakeStreamingClient([
      fakeDevice('printer-1'),
    ]);
    const fleet = createFleet(client);
    await fleet.load();

    const stop = fleet.listen();
    push(sampled('printer-1', '/job/progress', 63));
    await settle();

    expect(fleet.device('printer-1')?.job?.progress).toBe(63);

    stop();
    end();
  });

  it('keeps the last failure and clears it on the next good read', async () => {
    const { client } = fakeFleetClient([]);
    const fleet = createFleet(client);

    await fleet.apply(recordChanged('gone'));
    expect(fleet.error?.message).toBe('no device gone');

    await fleet.load();
    expect(fleet.error).toBeUndefined();
  });
});

/**
 * A stream the server ends the way it ends one whose credential stopped being
 * accepted: the loop throws a 401 that carries no code. A password change does
 * this to the stream a person opened before they changed it.
 */
const endedByTheServer = (
  lastEventId: string | undefined,
): EventSubscription => ({
  lastEventId,
  close: () => undefined,
  [Symbol.asyncIterator]: async function* () {
    await Promise.resolve();
    yield* [];
    throw Object.assign(new Error('the credential is no longer accepted'), {
      name: 'ApiError',
      status: 401,
      code: undefined,
    });
  },
});

/** A signed-in identity a test controls: who is signed in, before and after a read. */
const fakeIdentity = (before: string, after: string | undefined) => {
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

/** A client whose streams are answered in order, then by the last one. */
const streamsInOrder = (
  rows: readonly ReturnType<typeof fakeDevice>[],
  answers: EventSubscription[],
) => {
  const asked: EventsOptions[] = [];
  const last = answers[answers.length - 1];
  const base = fakeFleetClient(rows);
  const events = ((options: EventsOptions) => {
    asked.push(options);

    return answers.shift() ?? last;
  }) as FleetClient['events'];

  return { asked, client: { ...base.client, events } };
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

describe('Fleet after the server ends the stream', () => {
  it('opens the stream again for the same person, from where it stopped', async () => {
    const reopened = fakeStream();
    const { asked, client } = streamsInOrder(
      [fakeDevice('printer-1')],
      [endedByTheServer('41'), reopened.subscription],
    );
    const fleet = createFleet(client, fakeIdentity('user:ada', 'user:ada'));
    await fleet.load();

    const stop = fleet.listen();
    await settle();

    expect(asked).toHaveLength(2);
    expect(asked[1]?.since).toBe('41');
    expect(fleet.error).toBeUndefined();
    // The SDK checks the person again before it reconnects after a drop.
    expect(asked.map((options) => options.principal)).toEqual([
      'user:ada',
      'user:ada',
    ]);

    reopened.push(sampled('printer-1', '/job/progress', 12));
    await settle();
    expect(fleet.device('printer-1')?.job?.progress).toBe(12);

    stop();
  });

  it('drops the fleet and opens nothing when somebody else signed in', async () => {
    const { asked, client } = streamsInOrder(
      [fakeDevice('printer-1')],
      [endedByTheServer('41')],
    );
    const fleet = createFleet(client, fakeIdentity('user:ada', 'user:grace'));
    await fleet.load();

    const stop = fleet.listen();
    await settle();

    expect(asked).toHaveLength(1);
    expect(fleet.devices).toEqual([]);
    expect(fleet.loaded).toBe(false);
    expect(fleet.error).toBeUndefined();

    stop();
  });

  it('drops the fleet and opens nothing when nobody is signed in any more', async () => {
    const { asked, client } = streamsInOrder(
      [fakeDevice('printer-1')],
      [endedByTheServer('41')],
    );
    const fleet = createFleet(client, fakeIdentity('user:ada', undefined));
    await fleet.load();

    const stop = fleet.listen();
    await settle();

    expect(asked).toHaveLength(1);
    expect(fleet.devices).toEqual([]);

    stop();
  });

  it('shows the error when it cannot tell who is signed in', async () => {
    const { asked, client } = streamsInOrder([], [endedByTheServer(undefined)]);
    const fleet = createFleet(client);

    const stop = fleet.listen();
    await settle();

    expect(asked).toHaveLength(1);
    expect(fleet.error?.message).toBe('the credential is no longer accepted');

    stop();
  });

  it('shows the error when the stream it opened again is ended the same way', async () => {
    const { asked, client } = streamsInOrder([], [endedByTheServer(undefined)]);
    const fleet = createFleet(client, fakeIdentity('user:ada', 'user:ada'));

    const stop = fleet.listen();
    await settle();
    await settle();

    expect(asked).toHaveLength(2);
    expect(fleet.error?.message).toBe('the credential is no longer accepted');

    stop();
  });

  it('compares with who opened the stream, not with who a reader saw last', async () => {
    const { asked, client } = streamsInOrder(
      [fakeDevice('printer-1')],
      [endedByTheServer('41'), fakeStream().subscription],
    );
    const moving = movableIdentity('user:ada');
    const fleet = createFleet(client, moving.identity);
    await fleet.load();

    const stop = fleet.listen();
    // Another reader of the session sees the new person before the end lands.
    moving.become('user:grace');
    await settle();
    await settle();

    expect(asked).toHaveLength(1);
    expect(fleet.devices).toEqual([]);

    stop();
  });

  it('opens nothing when the stream was closed while the identity was read', async () => {
    const { asked, client } = streamsInOrder(
      [fakeDevice('printer-1')],
      [endedByTheServer('41'), fakeStream().subscription],
    );
    const moving = movableIdentity('user:ada');
    moving.hold();
    const fleet = createFleet(client, moving.identity);
    await fleet.load();

    const stop = fleet.listen();
    await settle();
    expect(moving.reading()).toBe(true);

    stop();
    moving.finish();
    await settle();

    expect(asked).toHaveLength(1);
    expect(fleet.devices).toHaveLength(1);
  });
});

const tagged = (id: string, tags: readonly string[]) => ({
  ...fakeDevice(id),
  tags: [...tags],
});

describe('Fleet one-row read', () => {
  // A list read already on its way may have started before the write, so a
  // write asks for its own row.
  it('reads one printer again and replaces its row', async () => {
    const rows = [tagged('printer-1', ['a'])];
    const { client, calls } = fakeFleetClient(rows);
    const fleet = createFleet(client);
    await fleet.load();

    rows[0] = tagged('printer-1', ['a', 'b']);
    await fleet.readDevice('printer-1');

    expect(calls.get).toEqual(['printer-1']);
    expect(fleet.device('printer-1')?.tags).toEqual(['a', 'b']);
  });
});

// A browser can sign out and sign in as somebody else without the page
// reloading. The fleet then holds nothing of the first person.
describe('Fleet reset for another person', () => {
  it('forgets every printer of the last person', async () => {
    const { client } = fakeFleetClient([fakeDevice('printer-1')]);
    const fleet = createFleet(client);
    await fleet.load();

    fleet.reset();

    expect(fleet.devices).toEqual([]);
    expect(fleet.loaded).toBe(false);
    expect(fleet.loading).toBe(false);
    expect(fleet.error).toBeUndefined();
  });

  it('drops a listing and a row that were asked for before the reset', async () => {
    const answers: (() => void)[] = [];
    const base = fakeFleetClient([fakeDevice('printer-1')]).client;
    const client: FleetClient = {
      ...base,
      listDevices: (...args) =>
        new Promise((resolve, reject) => {
          answers.push(() => {
            base.listDevices(...args).then(resolve, reject);
          });
        }),
      getDevice: (...args) =>
        new Promise((resolve, reject) => {
          answers.push(() => {
            base.getDevice(...args).then(resolve, reject);
          });
        }),
    };
    const fleet = createFleet(client);
    const reads = [fleet.load(), fleet.readDevice('printer-1')];

    fleet.reset();
    for (const answer of answers) answer();
    await Promise.all(reads);

    expect(fleet.devices).toEqual([]);
    expect(fleet.loaded).toBe(false);
  });

  it('closes the stream it opened', () => {
    const { client, subscription } = fakeStreamingClient([]);
    let closed = false;
    const close = subscription.close.bind(subscription);
    subscription.close = () => {
      closed = true;
      close();
    };
    const fleet = createFleet(client);
    const stop = fleet.listen();

    fleet.reset();

    expect(closed).toBe(true);
    stop();
  });

  // A change of person resets the store first, so the rows a failed read
  // keeps are the same person's.
  it('keeps the rows of the same person beside a failed read', async () => {
    let refused = false;
    const base = fakeFleetClient([fakeDevice('printer-1')]).client;
    const client: FleetClient = {
      ...base,
      listDevices: (...args) =>
        refused
          ? Promise.reject(new Error('refused'))
          : base.listDevices(...args),
    };
    const fleet = createFleet(client);
    await fleet.load();

    refused = true;
    await fleet.load();

    expect(fleet.error?.message).toBe('refused');
    expect(fleet.devices.map((row) => row.id)).toEqual(['printer-1']);
  });
});
