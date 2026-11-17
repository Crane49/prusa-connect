import type { StreamEvent } from '@prusa/connect';
import { describe, expect, it } from 'vitest';

import type { Device } from '../domain/command';
import { fakeDevice } from '../test/fakes';
import { type Notice, type NoticeFleet, Notices } from './notices.svelte';

const eventOf = (
  type: string,
  deviceId: string,
  payload: Record<string, unknown> = {},
): StreamEvent =>
  // The cast stands in for the narrowed union the SDK yields for one type.
  ({
    type,
    id: '1',
    payload,
    envelope: {
      specversion: '1.0',
      id: '1',
      source: `urn:prusa:connect:printer/${deviceId}`,
      type,
      time: '2026-10-02T00:00:00Z',
      subject: `device:${deviceId}`,
      hops: 0,
      evclass: 'state',
    },
  }) as StreamEvent;

const settled = (deviceId: string, outcome: string) =>
  eventOf('com.prusa.connect.device.job.settled', deviceId, {
    outcome,
    display_name: 'benchy.gcode',
    job_id: `job-${outcome}`,
  });

/** A fleet whose rows a test changes before it emits the event. */
const fakeFleet = () => {
  let rows: Device[] = [];
  let listener: ((event: StreamEvent) => void) | undefined;
  const fleet: NoticeFleet = {
    get devices() {
      return rows;
    },
    device: (id) => rows.find((row) => row.id === id),
    subscribe: (next) => {
      listener = next;

      return () => {
        listener = undefined;
      };
    },
  };

  return {
    fleet,
    set: (next: Device[]) => {
      rows = next;
    },
    emit: (event: StreamEvent) => listener?.(event),
  };
};

const withIssue = (
  device: Device,
  reason: string,
  severity: 'info' | 'warning' | 'error',
): Device => ({ ...device, issues: [{ reason, severity }] });

describe('Notices', () => {
  it('lists a finished and a failed print, newest first, and toasts only through onNotice', () => {
    const heard: Notice[] = [];
    const notices = new Notices({ onNotice: (one) => heard.push(one) });
    const { fleet, set, emit } = fakeFleet();
    set([fakeDevice('a', 'Alpha')]);
    notices.watch(fleet);

    emit(settled('a', 'finished'));
    emit(settled('a', 'failed'));
    emit(settled('a', 'cancelled'));

    expect(notices.items.map((one) => one.kind)).toEqual([
      'job-failed',
      'job-finished',
    ]);
    expect(notices.items[0]).toMatchObject({
      printer: 'Alpha',
      file: 'benchy.gcode',
      jobId: 'job-failed',
      read: false,
    });
    expect(heard).toHaveLength(2);
  });

  it('lists a printer that stopped answering and not one that came back', () => {
    const notices = new Notices();
    const { fleet, set, emit } = fakeFleet();
    set([fakeDevice('a')]);
    notices.watch(fleet);

    const presence = 'com.prusa.connect.device.presence.changed';
    emit(eventOf(presence, 'a', { printer: 'a', connected: true }));
    emit(eventOf(presence, 'a', { printer: 'a', connected: false }));

    expect(notices.items.map((one) => one.kind)).toEqual(['printer-offline']);
  });

  it('lists an issue a printer did not have when the fleet was read, once', () => {
    const notices = new Notices();
    const { fleet, set, emit } = fakeFleet();
    const old = withIssue(fakeDevice('a'), 'LINK_SILENT', 'warning');
    set([old]);
    notices.seed(fleet.devices);
    notices.watch(fleet);

    const changed = eventOf('com.prusa.connect.device.record.changed', 'a');
    emit(changed);
    expect(notices.items).toEqual([]);

    set([withIssue(old, 'AUTH_REJECTED', 'error')]);
    emit(changed);
    emit(changed);

    expect(notices.items).toHaveLength(1);
    expect(notices.items[0]).toMatchObject({
      kind: 'printer-error',
      issue: 'AUTH_REJECTED',
    });
  });

  it('marks one read, then all, and forgets everything on reset', () => {
    const notices = new Notices();
    const { fleet, set, emit } = fakeFleet();
    set([fakeDevice('a')]);
    notices.watch(fleet);
    emit(settled('a', 'finished'));
    emit(settled('a', 'failed'));

    const first = notices.items[0];
    expect(first).toBeDefined();
    notices.toggleRead(first?.id ?? '');
    expect(notices.items.map((one) => one.read)).toEqual([true, false]);

    notices.markAllRead();
    expect(notices.items.every((one) => one.read)).toBe(true);

    notices.reset();
    expect(notices.items).toEqual([]);
  });
});

/** A registration frame, on the connector's origin and naming no device. */
const registration = (
  kind: string,
  payload: Record<string, unknown>,
): StreamEvent =>
  // The cast stands in for the narrowed union the SDK yields for one type.
  ({
    type: `com.prusa.connect.device.registration.${kind}`,
    id: '1',
    payload: { connector: 'xbuddy', ...payload },
    envelope: undefined,
  }) as unknown as StreamEvent;

describe('Notices about printers joining', () => {
  it('says a printer is waiting, and stops asking once somebody decides', () => {
    const heard: Notice[] = [];
    const notices = new Notices({
      onNotice: (notice) => heard.push(notice),
      modelName: (model) => (model === '1.4.0' ? 'MK4S' : undefined),
    });
    const { fleet, emit } = fakeFleet();
    notices.watch(fleet);

    emit(registration('announced', { code: 'K7Q', model: '1.4.0' }));
    // A retry is one printer, and one notice.
    emit(registration('announced', { code: 'K7Q', model: '1.4.0' }));

    expect(notices.items).toHaveLength(1);
    expect(notices.items[0]).toMatchObject({
      kind: 'printer-pending',
      code: 'K7Q',
      printer: 'MK4S',
      read: false,
    });
    expect(notices.items[0]?.deviceId).toBeUndefined();
    expect(heard).toHaveLength(1);

    emit(registration('rejected', { code: 'K7Q' }));
    expect(notices.items[0]?.read).toBe(true);
  });

  it('raises nothing for a code where every printer is approved on arrival', () => {
    const notices = new Notices({ admitsOnArrival: () => true });
    const { fleet, emit } = fakeFleet();
    notices.watch(fleet);

    emit(registration('announced', { code: 'K7Q', model: 'MK4S' }));

    expect(notices.items).toEqual([]);
  });

  it('says a printer approved here is connected on its first arrival only', () => {
    const notices = new Notices();
    const { fleet, set, emit } = fakeFleet();
    notices.watch(fleet);

    emit(
      registration('approved', { code: 'K7Q', device_id: 'a', model: 'MK4S' }),
    );
    set([{ ...fakeDevice('a'), name: 'Rack 4' }]);
    const arrived = eventOf('com.prusa.connect.device.arrived', 'a');
    emit(arrived);
    emit(arrived);

    // A printer that comes back after a restart was never approved here.
    set([{ ...fakeDevice('a'), name: 'Rack 4' }, fakeDevice('b')]);
    emit(eventOf('com.prusa.connect.device.arrived', 'b'));

    const connected = notices.items.filter(
      (one) => one.kind === 'printer-connected',
    );
    expect(connected).toHaveLength(1);
    expect(connected[0]).toMatchObject({ deviceId: 'a', printer: 'Rack 4' });
  });

  // The stream folds frames over a window, and the arrival can come first.
  it('says a printer is connected when its approval comes after its arrival', () => {
    const notices = new Notices();
    const { fleet, set, emit } = fakeFleet();
    notices.watch(fleet);

    set([{ ...fakeDevice('a'), name: 'Rack 4', connected: true }]);
    emit(eventOf('com.prusa.connect.device.arrived', 'a'));
    emit(registration('approved', { code: 'K7Q', device_id: 'a' }));
    // The arrival is not announced twice when the device comes back.
    emit(eventOf('com.prusa.connect.device.arrived', 'a'));

    const connected = notices.items.filter(
      (one) => one.kind === 'printer-connected',
    );
    expect(connected).toHaveLength(1);
    expect(connected[0]).toMatchObject({ deviceId: 'a', printer: 'Rack 4' });
  });

  it('says a printer was refused once per printer for the session, and never for a caller with no identity', () => {
    const notices = new Notices();
    const { fleet, emit } = fakeFleet();
    notices.watch(fleet);

    const refused = registration('refused', {
      reason: 'unknown_credentials',
      admission: 'approval',
      fingerprint_digest: 'abc',
      model: 'MK4S',
    });
    emit(refused);
    emit(refused);
    emit(
      registration('refused', {
        reason: 'unknown_credentials',
        admission: 'approval',
      }),
    );

    expect(notices.items).toHaveLength(1);
    expect(notices.items[0]).toMatchObject({
      kind: 'printer-refused',
      printer: 'MK4S',
      reason: 'unknown_credentials',
    });

    // Read or not, the same printer calling again every minute is not news.
    notices.markAllRead();
    emit(refused);
    expect(notices.items).toHaveLength(1);
  });

  it('lists a module the server switched off, and no other transition', () => {
    const notices = new Notices();
    const { fleet, emit } = fakeFleet();
    notices.watch(fleet);
    const changed = (result: string) =>
      eventOf('com.prusa.connect.resource.state.changed', '', {
        resource: 'sink/spools',
        result,
      });

    emit(changed('over-memory'));
    expect(notices.items).toHaveLength(0);

    emit(changed('switched-off-by-host'));
    expect(notices.items).toHaveLength(1);

    // A replayed frame while the first is unread adds nothing; after it is
    // read, a new switch-off is news again.
    emit(changed('switched-off-by-host'));
    expect(notices.items).toHaveLength(1);
    notices.markAllRead();
    emit(changed('switched-off-by-host'));
    expect(notices.items).toHaveLength(2);
    expect(notices.items[0]).toMatchObject({
      kind: 'module-switched-off',
      module: 'spools',
      printer: '',
    });
  });
});
