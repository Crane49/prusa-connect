import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import {
  LONG_NOTIFICATION_FIXTURE,
  NOTIFICATION_FIXTURE,
} from '../../test/notification-fixture';
import NotificationsLive from '../../test/notifications-live.svelte';

// bits-ui puts no role and no name on the popover surface, so the panel is
// found by its slot rather than by the label the React stories look for.
const panel = (): HTMLElement | null =>
  document.querySelector<HTMLElement>(
    '[data-slot="popover-content"], [data-slot="sheet-content"]',
  );

const rows = (): HTMLElement[] => [
  ...(panel()?.querySelectorAll<HTMLElement>('[data-slot="notification"]') ??
    []),
];

const bell = (name: string) => page.getByRole('button', { name });

const openBell = async (name: string): Promise<void> => {
  await bell(name).click();
  await expect.poll(() => rows().length).toBeGreaterThan(-1);
  await expect.poll(panel).not.toBe(null);
};

describe('Notifications', () => {
  it('counts the unread on the bell and orders the list by what needs doing', async () => {
    await render(NotificationsLive);

    await openBell('Notifications, 2 unread');

    await expect.poll(() => rows().length).toBe(4);
    expect(rows().map((row) => row.getAttribute('data-tone'))).toEqual([
      'critical',
      'attention',
      'success',
      'info',
    ]);
    // The row spends no line on the tone in capitals. The word is in the
    // accessibility tree, in front of the title it qualifies.
    expect(
      rows()[0]?.querySelector('p')?.textContent.replace(/\s+/g, ' ').trim(),
    ).toBe('Critical: MK4S — Print Farm Rack 3 stopped');
    await expectNoViolations(panel()!);
  });

  it('leads a row with the machine or the plate, and offers buttons not links', async () => {
    await render(NotificationsLive);

    await openBell('Notifications, 2 unread');
    await expect.poll(() => rows().length).toBe(4);

    const art = [...(panel()?.querySelectorAll('img') ?? [])];
    // The printer's own artwork carries a name; the plate is decoration
    // beside a title that already names the file, so its alt is empty.
    expect(art.filter((image) => image.alt !== '')).toHaveLength(2);
    expect(art.filter((image) => image.alt === '')).toHaveLength(1);

    await expect
      .element(page.getByRole('button', { name: 'View printer' }))
      .toBeVisible();
    await expect
      .element(page.getByRole('button', { name: 'Answer' }))
      .toBeVisible();
    expect(
      panel()?.querySelectorAll('[data-slot="notification"] a'),
    ).toHaveLength(0);
  });

  it('settles a row from its own action, and the whole list from the header', async () => {
    await render(NotificationsLive);
    await openBell('Notifications, 2 unread');
    await expect.poll(() => rows().length).toBe(4);

    await page.getByRole('button', { name: 'Acknowledge' }).click();
    await expect.element(bell('Notifications, 1 unread')).toBeInTheDocument();

    await page.getByRole('button', { name: 'Mark all read' }).click();
    await expect
      .element(bell('Notifications, none unread'))
      .toBeInTheDocument();
  });

  // A printer waiting for approval leads somewhere, so its action is a link,
  // and following it leaves the list: the panel closes behind it.
  it('draws an action that goes somewhere as a link, and closes on it', async () => {
    await render(NotificationsLive, {
      props: {
        initial: [
          {
            id: 'pending',
            kind: 'printer-pending',
            title: 'MK4S is waiting for approval',
            body: 'Code K7QF2',
            at: '09:41',
            read: false,
            actions: [{ id: 'open', label: 'Review', href: '#approvals' }],
          },
        ],
      },
    });
    await openBell('Notifications, 1 unread');

    const review = page.getByRole('link', { name: 'Review' });
    await expect.element(review).toHaveAttribute('href', '#approvals');
    await review.click();

    await expect.poll(panel).toBe(null);
  });

  it('marks one row read and unread again from the same control', async () => {
    await render(NotificationsLive);
    await openBell('Notifications, 2 unread');
    await expect.poll(() => rows().length).toBe(4);

    const toggle = page.getByRole('button', {
      name: 'Mark “CORE One is waiting for a person” read',
    });
    await expect.element(toggle).toHaveAttribute('aria-pressed', 'false');
    await toggle.click();

    await expect.element(bell('Notifications, 1 unread')).toBeInTheDocument();
    await expect
      .element(
        page.getByRole('button', {
          name: 'Mark “CORE One is waiting for a person” unread',
        }),
      )
      .toHaveAttribute('aria-pressed', 'true');
  });

  // The count is a state that is still true, so it is pinned above the list
  // rather than being one of its rows, and it is what the bell counts while it
  // is above zero.
  it('pins the printers waiting above the history, and keeps them there', async () => {
    await render(NotificationsLive, { props: { needsAttention: 5 } });

    await openBell('Notifications, 5 printers need you, 2 unread');
    await expect.poll(() => rows().length).toBe(4);

    const pinned = page.getByRole('link', { name: '5 printers need you' });
    await expect.element(pinned).toHaveAttribute('href', '/printers');
    expect(pinned.element().getBoundingClientRect().top).toBeLessThan(
      rows()[0]!.getBoundingClientRect().top,
    );

    // Reading everything does not make printers stop waiting.
    await page.getByRole('button', { name: 'Mark all read' }).click();
    await expect
      .element(bell('Notifications, 5 printers need you, none unread'))
      .toBeInTheDocument();
  });

  /**
   * The two things a list this long has to do: stay inside its own box, and
   * put what has not been seen above what has.
   */
  it('splits unread from earlier and scrolls inside its own box', async () => {
    await render(NotificationsLive, {
      props: { initial: LONG_NOTIFICATION_FIXTURE },
    });

    await openBell('Notifications, 2 unread');
    await expect.poll(() => rows().length).toBe(16);

    // Two groups with a rule between them, and no bars of capitals over
    // either: a row already says whether it has been read, in a dot and the
    // weight of its own title.
    expect(panel()?.querySelectorAll('section')).toHaveLength(2);
    const reads = rows().map((row) => row.getAttribute('data-read'));
    expect(reads).toEqual([...reads].sort());

    const viewport = panel()?.querySelector<HTMLElement>(
      '[data-slot="notification-list"]',
    );
    expect(viewport!.scrollHeight).toBeGreaterThan(viewport!.clientHeight);
  });

  it('says so when there is nothing behind the bell', async () => {
    await render(NotificationsLive, { props: { initial: [] } });

    await openBell('Notifications, none unread');

    await expect
      .element(page.getByText('Nothing has happened since you last looked.'))
      .toBeVisible();
    expect(panel()?.textContent.includes('Mark all read')).toBe(false);
  });

  it('opens the phone panel as a sheet, and the bell becomes its close', async () => {
    await render(NotificationsLive, {
      props: { presentation: 'sheet', initial: NOTIFICATION_FIXTURE },
    });

    await openBell('Notifications, 2 unread');
    await expect.poll(() => rows().length).toBe(4);
    // The row's own paragraph leads with the tone for a screen reader, so the
    // title is read off the row rather than matched on its own.
    expect(
      rows().some((row) =>
        row.textContent.includes('CORE One is waiting for a person'),
      ),
    ).toBe(true);

    // The bell keeps its place and becomes the close control. The press is
    // dispatched on the element, because this harness stacks the panel over
    // the bell, where a shell docks its bar above the panel with `sheetClass`
    // and `sheetOverlayClass`.
    const close = bell('Close notifications').element() as HTMLButtonElement;
    close.click();
    await expect.poll(panel).toBe(null);
    await expect.element(bell('Notifications, 2 unread')).toBeVisible();
  });
});
