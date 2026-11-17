// Moved from pull request 185, packages/domain/src/notification.test.ts, 2026-09-11
import { describe, expect, it } from 'vitest';

import {
  compareByTone,
  type NotificationKind,
  unreadCount,
  unreadTone,
} from './notification';

const item = (kind: NotificationKind, read?: boolean) => ({ kind, read });

describe('unreadCount', () => {
  it('counts only what has not been looked at', () => {
    expect(
      unreadCount([item('job-finished'), item('job-finished', true)]),
    ).toBe(1);
  });
});

describe('unreadTone', () => {
  it('is undefined with nothing unread, whatever is in the list', () => {
    expect(unreadTone([item('printer-error', true)])).toBe(undefined);
  });

  // The mark on the bell is the loudest thing behind it: one printer in error
  // under nine finished prints is still an error, and a bell painted for the
  // majority would hide exactly the one row a person is needed for.
  it('takes the loudest tone among the unread, not the commonest', () => {
    expect(
      unreadTone([
        item('job-finished'),
        item('job-finished'),
        item('printer-error'),
      ]),
    ).toBe('critical');
    expect(unreadTone([item('job-finished'), item('printer-attention')])).toBe(
      'attention',
    );
  });

  // A critical event already read does not keep the bell red.
  it('ignores the tone of what has been read', () => {
    expect(
      unreadTone([item('printer-error', true), item('job-finished')]),
    ).toBe('success');
  });
});

describe('compareByTone', () => {
  // The list is read to find out what to do next: a stopped printer belongs
  // above a print that went well, whichever happened first.
  it('sorts what needs doing above what merely happened', () => {
    const kinds: NotificationKind[] = [
      'job-finished',
      'printer-connected',
      'printer-error',
      'printer-attention',
    ];
    expect([...kinds].sort(compareByTone)).toEqual([
      'printer-error',
      'printer-attention',
      'job-finished',
      'printer-connected',
    ]);
  });
});
