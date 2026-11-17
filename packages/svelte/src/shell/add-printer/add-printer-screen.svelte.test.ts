import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { DECLARED_MODELS } from '../../primitives/printer-icon/declared';
import { expectNoViolations } from '../../test/axe';
import AddPrinterScreen from './add-printer-screen.svelte';
import type { AddPrinterScreenProps, PendingPrinter } from './types';

const connection = {
  host: 'printers.local',
  port: 8080,
  tls: false,
  wsUrl: 'ws://printers.local:8080/p/ws',
  registerUrl: 'http://printers.local:8080/p/register',
};

const pending: PendingPrinter[] = [
  {
    code: 'K7QF2RXA9C',
    fingerprint: 'a1b2c3d4e5f60718',
    model: 'MK4S',
    firmware: '6.5.1',
    serial: 'CZPX4021X004XK12345',
  },
  {
    code: 'M3PZ9WQHTE',
    fingerprint: '99ffee8877665544',
    model: 'CORE One',
    firmware: '6.4.0',
  },
];

/** What every case hands in, so a case names only what it is about. */
const screen = (
  over: Partial<AddPrinterScreenProps> = {},
): AddPrinterScreenProps => ({
  connection,
  admission: 'approval',
  pending: [],
  joining: [],
  joined: [],
  refused: undefined,
  printersHref: '/printers',
  onBegin: () => undefined,
  onApprove: () => undefined,
  onReject: () => undefined,
  ...over,
});

const next = async (): Promise<void> => {
  await page.getByRole('button', { name: 'Next' }).click();
};

describe('AddPrinterScreen', () => {
  it('names itself and says the way back', async () => {
    await render(AddPrinterScreen, { props: screen() });

    await expect
      .element(page.getByRole('heading', { level: 1 }))
      .toHaveTextContent('Add a printer');
    await expect
      .element(page.getByRole('link', { name: 'Printers' }))
      .toHaveAttribute('href', '/printers');
  });

  it('starts with the file, which never carries a token', async () => {
    await render(AddPrinterScreen, { props: screen() });

    await expect
      .element(page.getByRole('heading', { level: 2 }))
      .toHaveTextContent('Prepare the settings file');
    await expect
      .element(page.getByText(/hostname = printers\.local/))
      .toBeInTheDocument();
    expect(page.getByText(/token/).query()).toBe(null);
    expect(page.getByText(/connect\.der/).query()).toBe(null);

    const download = page.getByRole('link', {
      name: 'Download prusa_printer_settings.ini',
    });
    await expect
      .element(download)
      .toHaveAttribute('download', 'prusa_printer_settings.ini');
    const href = download.element().getAttribute('href') ?? '';
    expect(decodeURIComponent(href)).toContain('hostname = printers.local');
    expect(decodeURIComponent(href)).not.toContain('token');
  });

  it('keeps the text of the file and the values shut until asked', async () => {
    await render(AddPrinterScreen, { props: screen() });

    const trigger = page.getByRole('button', {
      name: 'Show the file and connection values',
    });
    await expect.element(trigger).toHaveAttribute('aria-expanded', 'false');
    await expect
      .element(page.getByText(/hostname = printers\.local/))
      .not.toBeVisible();

    await trigger.click();
    await expect
      .element(page.getByText(/hostname = printers\.local/))
      .toBeVisible();
    await expect.element(page.getByText('WebSocket URL')).toBeVisible();
  });

  it('shows the steps down the side of a wide region and a bar in a narrow one', async () => {
    const { container } = await render(AddPrinterScreen, {
      props: screen({ initialStep: 'printer' }),
    });
    const rail = page.getByRole('list', { name: 'Steps to add a printer' });
    const position = page.getByText('Step 2 of 4 · On the printer');

    // The switch reads the region's width, never the window's, so the
    // region is sized here and the window is left alone.
    container.style.width = '1100px';
    await expect.element(rail).toBeVisible();
    await expect.element(position).not.toBeVisible();
    const current = container.querySelector('[aria-current="step"]');
    expect(current?.textContent).toContain('On the printer');
    expect(current?.textContent).toContain('A few taps on the printer');

    container.style.width = '600px';
    await expect.element(position).toBeVisible();
    // Hidden, a list leaves the accessibility tree, so the role finds nothing.
    await expect.poll(() => rail.query()).toBe(null);
    await expect
      .element(
        page.getByRole('progressbar', { name: 'Steps to add a printer' }),
      )
      .toHaveAttribute('aria-valuenow', '50');

    container.style.width = '';
  });

  it('draws every step as done once the printer is connected', async () => {
    const { container } = await render(AddPrinterScreen, {
      props: screen({
        initialStep: 'connected',
        joined: [{ deviceId: 'd-1', name: 'MK4S', href: '/printers/d-1' }],
      }),
    });
    container.style.width = '1100px';

    await expect
      .element(page.getByRole('list', { name: 'Steps to add a printer' }))
      .toBeVisible();
    expect(
      Array.from(
        container.querySelectorAll<HTMLElement>('[data-slot="step-list-item"]'),
      ).map((item) => item.dataset.state),
    ).toEqual(['done', 'done', 'done', 'done']);
    container.style.width = '';
  });

  it('draws the menu to open as the path the display shows', async () => {
    await render(AddPrinterScreen, {
      props: screen({ initialStep: 'printer' }),
    });

    const path = page.getByRole('list', { name: 'Menu path on the printer' });
    await expect.element(path).toBeVisible();
    expect(
      Array.from(path.element().querySelectorAll('li')).map((item) =>
        item.textContent.trim(),
      ),
    ).toEqual(['Settings', 'Network', 'Prusa Connect']);
    await expect
      .element(page.getByText(/says No Config\. That is expected/))
      .toBeVisible();
    await expect
      .element(
        page.getByText('You do not need to switch Connect off and on again.'),
      )
      .toBeVisible();
  });

  it('asks for connect.der only where the authority is our own', async () => {
    await render(AddPrinterScreen, {
      props: screen({
        connection: { ...connection, tls: true, customCert: true },
      }),
    });

    await expect
      .element(page.getByText(/Download connect\.der and copy it/))
      .toBeInTheDocument();
  });

  it('offers connect.der as a download when the server has it', async () => {
    await render(AddPrinterScreen, {
      props: screen({
        connection: { ...connection, tls: true, customCert: true },
        authority: new Blob(['DER']),
      }),
    });

    await expect
      .element(page.getByRole('link', { name: 'Download connect.der' }))
      .toHaveAttribute('download', 'connect.der');
  });

  it('says how to make connect.der when the server has none', async () => {
    await render(AddPrinterScreen, {
      props: screen({
        connection: { ...connection, tls: true, customCert: true },
        authority: null,
        docsHref: 'https://docs.example/',
      }),
    });

    expect(
      page.getByRole('link', { name: 'Download connect.der' }).query(),
    ).toBe(null);
    await expect
      .element(page.getByText(/no certificate file to offer/))
      .toBeInTheDocument();
    await expect
      .element(
        page.getByRole('link', { name: 'How to export the certificate' }),
      )
      .toHaveAttribute('href', 'https://docs.example/certificates/');
  });

  it('offers no connect.der where the authority is not our own', async () => {
    await render(AddPrinterScreen, {
      props: screen({
        authority: new Blob(['DER']),
        docsHref: 'https://docs.example/',
      }),
    });

    expect(page.getByText(/connect\.der/).query()).toBe(null);
    expect(page.getByRole('link', { name: /certificate/ }).query()).toBe(null);
  });

  it('warns that a printer cannot dial a loopback host, and only then', async () => {
    const { rerender } = await render(AddPrinterScreen, {
      props: screen({ connection: { ...connection, host: 'localhost' } }),
    });

    await expect
      .element(page.getByText(/cannot reach this server at localhost/))
      .toBeInTheDocument();

    await rerender(screen());
    await expect
      .element(page.getByText(/cannot reach this server/))
      .not.toBeInTheDocument();
  });

  it('walks to the printer and moves the focus to the step it opens', async () => {
    await render(AddPrinterScreen, { props: screen() });

    await next();

    const heading = page.getByRole('heading', { level: 2 });
    await expect.element(heading).toHaveTextContent('Set up the printer');
    await expect.element(heading).toHaveFocus();
    await expect
      .element(page.getByText(/Choose Load Settings/))
      .toBeInTheDocument();
    await expect
      .element(page.getByText(/Leave it on the screen/))
      .toBeInTheDocument();
  });

  it('tells the printer step that nobody approves in automatic admission', async () => {
    await render(AddPrinterScreen, {
      props: screen({ admission: 'automatic', initialStep: 'printer' }),
    });

    await expect
      .element(page.getByText(/This server approves it at once/))
      .toBeInTheDocument();
  });

  // A server that approves at once may approve the printer while the person
  // is still on the printer step, so the count starts when the guide opens
  // and moving between steps does not restart it.
  it('begins counting when it opens, not when the waiting step opens', async () => {
    let begins = 0;
    await render(AddPrinterScreen, {
      props: screen({
        initialStep: 'printer',
        onBegin: () => {
          begins += 1;
        },
      }),
    });

    expect(begins).toBe(1);
    await next();
    await page.getByRole('button', { name: 'Back' }).click();
    await next();

    expect(begins).toBe(1);
    await expect
      .element(page.getByText('Waiting for the printer'))
      .toBeVisible();
  });

  it('shows one card per printer asking, and approves the one pressed', async () => {
    const approved: string[] = [];
    await render(AddPrinterScreen, {
      props: screen({
        pending,
        initialStep: 'waiting',
        onApprove: (code) => {
          approved.push(code);
        },
      }),
    });

    await expect.element(page.getByText('K7QF2RXA9C')).toBeVisible();
    await expect.element(page.getByText('M3PZ9WQHTE')).toBeVisible();
    expect(page.getByText('Waiting for the printer').query()).toBe(null);

    await page
      .getByRole('button', { name: 'Approve CORE One, code M3PZ9WQHTE' })
      .click();

    expect(approved).toEqual(['M3PZ9WQHTE']);
  });

  // A guide that drew the icon without the reported spelling read a model a
  // driver declared as unrecognised, while the text beside it named it.
  it('names a declared printer on its icon while it asks and once it joins', async () => {
    const declared = new Map([
      [
        DECLARED_MODELS,
        (reported: string) =>
          reported === 'Bench_One'
            ? { name: 'Bench Works Tool Changer' }
            : undefined,
      ],
    ]);
    const bench = {
      model: 'Bench Works Tool Changer',
      icon: 'Bench_One',
      reported: 'Bench_One',
    };

    const asking = await render(AddPrinterScreen, {
      props: screen({
        pending: [
          { code: 'B3NCH0NE00', fingerprint: '0011223344556677', ...bench },
        ],
        initialStep: 'waiting',
      }),
      context: declared,
    });
    await expect
      .element(page.getByRole('img', { name: 'Bench Works Tool Changer' }))
      .toBeVisible();
    await asking.unmount();

    await render(AddPrinterScreen, {
      props: screen({
        joined: [
          { deviceId: 'd-9', name: 'Bench 9', href: '/printers/d-9', ...bench },
        ],
        initialStep: 'connected',
      }),
      context: declared,
    });
    await expect
      .element(page.getByRole('img', { name: 'Bench Works Tool Changer' }))
      .toBeVisible();
  });

  it('asks before a rejection, and drops nothing until the answer', async () => {
    const rejected: string[] = [];
    await render(AddPrinterScreen, {
      props: screen({
        pending,
        initialStep: 'waiting',
        onReject: (code) => {
          rejected.push(code);
        },
      }),
    });

    await page
      .getByRole('button', { name: 'Reject MK4S, code K7QF2RXA9C' })
      .click();
    await expect
      .element(page.getByRole('alertdialog', { name: 'Reject MK4S?' }))
      .toBeVisible();
    expect(rejected).toEqual([]);

    await page.getByRole('button', { name: 'Reject', exact: true }).click();

    expect(rejected).toEqual(['K7QF2RXA9C']);
  });

  it('offers no decision in automatic admission, and says the printer is joining', async () => {
    await render(AddPrinterScreen, {
      props: screen({
        admission: 'automatic',
        pending: pending.slice(0, 1),
        initialStep: 'waiting',
      }),
    });

    await expect.element(page.getByText('MK4S is joining')).toBeVisible();
    expect(page.getByRole('button', { name: /^Approve/ }).query()).toBe(null);
    await expect
      .element(page.getByRole('heading', { level: 2 }))
      .toHaveTextContent('Wait for the printer');
  });

  it('keeps an approved printer on screen until it dials in', async () => {
    await render(AddPrinterScreen, {
      props: screen({
        joining: [{ code: 'K7QF2RXA9C', model: 'MK4S' }],
        initialStep: 'waiting',
      }),
    });

    await expect
      .element(page.getByText('MK4S is approved and is connecting.'))
      .toBeVisible();
  });

  it('says why a printer was turned away, by its reason', async () => {
    const { rerender } = await render(AddPrinterScreen, {
      props: screen({
        refused: { reason: 'unknown_credentials', model: 'MK4S' },
        initialStep: 'waiting',
      }),
    });

    await expect
      .element(page.getByText('This server refused MK4S'))
      .toBeVisible();
    await expect.element(page.getByText(/did not give it/)).toBeInTheDocument();

    await rerender(
      screen({ refused: { reason: 'not_admitted' }, initialStep: 'waiting' }),
    );
    await expect
      .element(page.getByText('This server refused a printer'))
      .toBeVisible();
    await expect
      .element(page.getByText(/does not accept this printer/))
      .toBeInTheDocument();
  });

  it('opens the help when nothing has asked for a while, and not before', async () => {
    const { rerender } = await render(AddPrinterScreen, {
      props: screen({ initialStep: 'waiting', helpAfterMs: 60_000 }),
    });

    const trigger = page.getByRole('button', {
      name: 'The printer does not appear',
    });
    await expect.element(trigger).toHaveAttribute('aria-expanded', 'false');

    await rerender(screen({ initialStep: 'waiting', helpAfterMs: 0 }));
    await expect.element(trigger).toHaveAttribute('aria-expanded', 'true');
    await expect.element(page.getByText('Srv error')).toBeVisible();
    await expect
      .element(page.getByText(/firewall on this computer/))
      .toBeVisible();
  });

  it('moves to the last step by itself when the printer joins', async () => {
    let begins = 0;
    const { rerender } = await render(AddPrinterScreen, {
      props: screen({
        initialStep: 'waiting',
        onBegin: () => {
          begins += 1;
        },
      }),
    });
    expect(begins).toBe(1);

    const joined = [
      {
        deviceId: 'd-1',
        name: 'MK4S CZPX',
        model: 'MK4S',
        href: '/printers/d-1',
      },
    ];
    await rerender(
      screen({
        initialStep: 'waiting',
        joined,
        onBegin: () => {
          begins += 1;
        },
      }),
    );

    await expect
      .element(page.getByText('MK4S CZPX is connected'))
      .toBeVisible();
    await expect
      .element(page.getByRole('link', { name: 'Open the printer' }))
      .toHaveAttribute('href', '/printers/d-1');

    await page.getByRole('button', { name: 'Add another printer' }).click();
    expect(begins).toBe(2);
    await expect
      .element(page.getByRole('heading', { level: 2 }))
      .toHaveTextContent('Set up the printer');
  });

  it('opens the waiting step by itself when a printer asks, and lists it once', async () => {
    const approved: string[] = [];
    await render(AddPrinterScreen, {
      props: screen({
        pending,
        onApprove: (code) => {
          approved.push(code);
        },
      }),
    });

    await expect.element(page.getByText('K7QF2RXA9C')).toBeVisible();
    await page
      .getByRole('button', { name: 'Approve MK4S, code K7QF2RXA9C' })
      .click();
    expect(approved).toEqual(['K7QF2RXA9C']);
    expect(
      page
        .getByRole('heading', { name: 'Printers waiting for approval' })
        .query(),
    ).toBe(null);
  });

  it('follows a printer that asks or joins while an earlier step is open', async () => {
    const { rerender } = await render(AddPrinterScreen, {
      props: screen(),
    });
    await expect
      .element(page.getByRole('heading', { level: 2 }))
      .toHaveTextContent('Prepare the settings file');

    await rerender(screen({ pending: [pending[0]!] }));
    await expect.element(page.getByText('K7QF2RXA9C')).toBeVisible();

    await rerender(
      screen({
        joined: [
          {
            deviceId: 'd-1',
            name: 'MK4S CZPX',
            model: 'MK4S',
            href: '/printers/d-1',
          },
        ],
      }),
    );
    await expect
      .element(page.getByText('MK4S CZPX is connected'))
      .toBeVisible();
  });

  it('keeps a step chosen by hand open while the same printer waits', async () => {
    await render(AddPrinterScreen, {
      props: screen({ pending: [pending[0]!] }),
    });
    await expect.element(page.getByText('K7QF2RXA9C')).toBeVisible();

    await page.getByRole('button', { name: 'Back' }).click();
    await expect
      .element(page.getByRole('heading', { name: 'Set up the printer' }))
      .toBeVisible();
    await expect
      .element(
        page.getByRole('heading', { name: 'Printers waiting for approval' }),
      )
      .toBeVisible();
  });

  it('draws no step before the server says how printers join', async () => {
    await render(AddPrinterScreen, {
      props: screen({ admission: undefined }),
    });

    await expect
      .element(
        page.getByRole('status', {
          name: 'Loading how printers join this server',
        }),
      )
      .toBeVisible();
    expect(page.getByRole('button', { name: 'Next' }).query()).toBe(null);
  });

  it('draws a refusal by its code', async () => {
    await render(AddPrinterScreen, {
      props: screen({
        error: { code: 'core.auth/ACCESS_DENIED', status: 403 },
      }),
    });

    await expect
      .element(
        page.getByText('The caller may not perform that action on that object'),
      )
      .toBeVisible();
  });

  it('keeps the pending table under the width its container switches at', async () => {
    // The table is the list beside the guide, drawn on the last step.
    const { container } = await render(AddPrinterScreen, {
      props: screen({ pending, initialStep: 'connected' }),
    });

    const table = container.querySelector('table');
    if (table === null) throw new Error('no pending table rendered');

    // Re-measures the 66rem floor the two tiers switch at, so a sixth column
    // cannot push the table past its own container in silence.
    const probe = document.createElement('div');
    probe.style.cssText =
      'position:absolute;left:-9999px;top:0;width:min-content';
    probe.append(table.cloneNode(true));
    document.body.append(probe);
    const smallest = probe.getBoundingClientRect().width;
    probe.remove();

    expect(smallest).toBeLessThanOrEqual(66 * 16);
  });

  it('has nothing axe refuses on any step', async () => {
    const { container, rerender } = await render(AddPrinterScreen, {
      props: screen(),
    });
    await expectNoViolations(container);

    await next();
    await expectNoViolations(container);

    await next();
    await expectNoViolations(container);

    await rerender(screen({ pending }));
    await expectNoViolations(container);

    await rerender(
      screen({
        initialStep: 'waiting',
        joined: [{ deviceId: 'd-1', name: 'MK4S', href: '/printers/d-1' }],
      }),
    );
    await expectNoViolations(container);
  });
});
