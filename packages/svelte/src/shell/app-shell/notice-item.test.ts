// The words and the way on that the bell draws for each notice.
import { describe, expect, it } from 'vitest';

import type { Notice } from '../../data';
import { noticeItem } from './notice-item';

const notice = (over: Partial<Notice>): Notice => ({
  id: '1',
  kind: 'job-finished',
  printer: 'MK4S',
  at: 0,
  read: false,
  ...over,
});

describe('noticeItem', () => {
  it('names a waiting printer and the code to compare', () => {
    const item = noticeItem(
      notice({ kind: 'printer-pending', code: 'K7QF2' }),
      '/printers/add',
    );

    expect(item.title).toBe('MK4S is waiting for approval');
    expect(item.body).toBe('Code K7QF2');
    expect(item.actions).toEqual([
      { id: 'open', label: 'Review', href: '/printers/add' },
    ]);
  });

  it('names a printer it knows nothing about in a sentence of its own', () => {
    expect(
      noticeItem(notice({ kind: 'printer-pending', printer: '' })).title,
    ).toBe('A printer is waiting for approval');
    expect(
      noticeItem(notice({ kind: 'printer-refused', printer: '' })).title,
    ).toBe('This server refused a printer');
  });

  it('says why a printer was refused, by its reason', () => {
    expect(
      noticeItem(notice({ kind: 'printer-refused', reason: 'not_admitted' }))
        .body,
    ).toMatch(/does not accept this printer/);
    expect(
      noticeItem(
        notice({ kind: 'printer-refused', reason: 'unknown_credentials' }),
      ).body,
    ).toMatch(/Add Printer to Connect/);
  });

  it('offers a way on only where the host names one and the kind has one', () => {
    expect(
      noticeItem(notice({ kind: 'printer-connected', deviceId: 'a' }), '/p/a')
        .actions,
    ).toEqual([{ id: 'open', label: 'Open the printer', href: '/p/a' }]);
    expect(
      noticeItem(notice({ kind: 'printer-connected' })).actions,
    ).toBeUndefined();
    expect(noticeItem(notice({}), '/jobs/j1').actions).toEqual([
      { id: 'open', label: 'Open the print', href: '/jobs/j1' },
    ]);
    expect(noticeItem(notice({})).actions).toBeUndefined();
  });

  it('names the module the server switched off, without a count', () => {
    const item = noticeItem(
      notice({ kind: 'module-switched-off', printer: '', module: 'spools' }),
      '/settings/extensions/acme',
    );

    expect(item.title).toBe('spools was switched off');
    expect(item.body).toBe('It kept failing and the server switched it off');
    expect(item.actions).toEqual([
      {
        id: 'open',
        label: 'Open the module',
        href: '/settings/extensions/acme',
      },
    ]);
  });

  it('reads a printer issue from the vocabulary and never shows its token', () => {
    const issue = notice({
      kind: 'printer-error',
      deviceId: 'a',
      issue: 'awaiting_operator',
    });
    const summary = (reason: string) =>
      reason === 'awaiting_operator' ? 'Waiting for a person.' : undefined;

    const known = noticeItem(issue, '/p/a', summary);
    expect(known.body).toBe('Waiting for a person.');
    expect(known.actions).toEqual([
      { id: 'open', label: 'Open the printer', href: '/p/a' },
    ]);

    for (const kind of ['printer-error', 'printer-attention'] as const) {
      const body = noticeItem(
        { ...issue, kind, issue: 'unlisted' },
        '/p/a',
      ).body;
      expect(body).toBe('Open the printer to see what it needs.');
    }
    expect(
      noticeItem(notice({ kind: 'printer-offline' }), '/p/a').actions,
    ).toEqual([{ id: 'open', label: 'Open the printer', href: '/p/a' }]);
  });
});
