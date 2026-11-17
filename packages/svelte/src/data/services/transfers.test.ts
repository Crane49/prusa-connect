import { describe, expect, it } from 'vitest';

import {
  lastSentBefore,
  thumbnailByLanding,
  type TransferRow,
} from './transfers';

const row = (over: Partial<TransferRow>): TransferRow => ({
  transferId: 't1',
  storage: 'local',
  source: '/plate.bgcode',
  path: '/usb/plate.bgcode',
  bytesTotal: 10,
  bytesSent: 10,
  state: 'done',
  reason: undefined,
  ...over,
});

const urlOf = (storage: string, source: string): string =>
  `${storage}:${source}`;

describe('thumbnailByLanding', () => {
  it('keys the picture by where the file landed on the printer', () => {
    const map = thumbnailByLanding([row({})], urlOf);

    expect(map.get('/usb/plate.bgcode')).toBe('local:/plate.bgcode');
  });

  it('knows nothing of a file this server never sent', () => {
    const map = thumbnailByLanding([row({})], urlOf);

    expect(map.get('/usb/carried-over.bgcode')).toBeUndefined();
  });

  // The listing is newest first, so the first row naming a path is the
  // transfer that wrote the file the printer holds now.
  it('lets the newest transfer decide a landing path two of them share', () => {
    const map = thumbnailByLanding(
      [
        row({ transferId: 't2', source: '/second.bgcode' }),
        row({ transferId: 't1', source: '/first.bgcode' }),
      ],
      urlOf,
    );

    expect(map.get('/usb/plate.bgcode')).toBe('local:/second.bgcode');
  });

  // The server deletes a temporary copy once it lands, so its picture would
  // never load. The path is left to the picture the printer reads out of its
  // own file, and an older kept copy does not stand in for the newer file.
  it('draws no picture from a temporary copy, and lets no older copy stand in', () => {
    const map = thumbnailByLanding(
      [
        row({ transferId: 't2', source: '.pcl-incoming/1-a/plate.bgcode' }),
        row({ transferId: 't1', source: '/plate.bgcode' }),
      ],
      urlOf,
    );

    expect(map.has('/usb/plate.bgcode')).toBe(false);
  });

  it('draws no picture from a transfer that did not finish', () => {
    for (const state of ['failed', 'cancelled', 'interrupted', 'sending']) {
      const map = thumbnailByLanding([row({ state })], urlOf);

      expect(map.get('/usb/plate.bgcode')).toBeUndefined();
    }
  });

  // A newer transfer that failed may have left a part of its file, or the
  // older one, and nothing says which. No picture is better than a wrong one.
  it('draws no picture where the newest transfer to a path failed', () => {
    const map = thumbnailByLanding(
      [
        row({ transferId: 't2', source: '/second.bgcode', state: 'failed' }),
        row({ transferId: 't1', source: '/first.bgcode' }),
      ],
      urlOf,
    );

    expect(map.get('/usb/plate.bgcode')).toBeUndefined();
  });
});

describe('lastSentBefore', () => {
  /** A send that ran from one time to another. */
  const send = (id: string, started: string, ended: string, state = 'done') =>
    row({
      transferId: id,
      source: id,
      startedAt: `2026-09-01T${started}:00Z`,
      endedAt: `2026-09-01T${ended}:00Z`,
      state,
    });

  const x = send('x', '07:00', '07:05');
  const y = send('y', '07:30', '07:35');
  const late = send('late', '09:00', '09:05');
  const failed = send('failed', '07:45', '07:50', 'failed');
  const START = '2026-09-01T08:00:00Z';
  const PATH = '/usb/plate.bgcode';

  // The server lists the newest first, and a store may hold them the other
  // way, so the order of the rows decides nothing.
  it('answers the newest finished send before the print, in either order', () => {
    const rows = [late, failed, y, x];

    expect(lastSentBefore(rows, PATH, START)?.transferId).toBe('y');
    expect(lastSentBefore([...rows].reverse(), PATH, START)?.transferId).toBe(
      'y',
    );
  });

  // A send still running when the print started had not replaced the file,
  // so the print ran from the copy before it.
  it('leaves out a send that finished after the print started', () => {
    const overlapping = send('overlapping', '07:59', '08:05');

    expect(lastSentBefore([overlapping, y], PATH, START)?.transferId).toBe('y');
  });

  it('counts a send that finished at the moment the print started', () => {
    const edge = send('edge', '07:50', '08:00');

    expect(lastSentBefore([y, edge], PATH, START)?.transferId).toBe('edge');
  });

  // Two sends that finished at the same moment are told apart by which one
  // started last, whichever order the rows come in.
  it('breaks a tie on the finish by the later start', () => {
    const early = send('early', '07:10', '07:40');
    const later = send('later', '07:20', '07:40');

    expect(lastSentBefore([early, later], PATH, START)?.transferId).toBe(
      'later',
    );
    expect(lastSentBefore([later, early], PATH, START)?.transferId).toBe(
      'later',
    );
  });

  it('answers nothing for another path or with no send before the print', () => {
    expect(lastSentBefore([x], '/usb/other.bgcode', START)).toBeUndefined();
    expect(lastSentBefore([late], PATH, START)).toBeUndefined();
    // A row known only from the event stream carries no finish time.
    expect(
      lastSentBefore([row({ endedAt: undefined })], PATH, START),
    ).toBeUndefined();
  });
});
