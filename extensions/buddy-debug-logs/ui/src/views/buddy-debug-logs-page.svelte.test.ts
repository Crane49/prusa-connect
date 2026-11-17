import { flushSync } from 'svelte';
import { beforeEach, describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import setup from '../index';
import {
  LINES,
  MOUNT,
  refusal,
  STATUS,
  stubHost,
  type StubOptions,
} from '../test/host';

/** Runs the module's setup on a stub host and draws its page. */
const drawPage = async (options: StubOptions = {}) => {
  const stub = stubHost(options);
  await setup(stub.host);
  const component = stub.pages['buddy-debug-logs'];
  if (component === undefined) throw new Error('the page was not registered');
  await render(component);

  return stub;
};

describe('the Prusa printer logs page', () => {
  // The page keeps its tab and filters in the address, and the tests share
  // one window, so each starts from the bare address.
  beforeEach(() => {
    window.history.replaceState(null, '', window.location.pathname);
  });

  it('registers the page and the printer tab its manifest declares', async () => {
    const stub = stubHost();
    await setup(stub.host);

    expect(Object.keys(stub.pages)).toEqual(['buddy-debug-logs']);
    expect(Object.keys(stub.sections)).toEqual(['device-tab:logs']);
  });

  it('shows both ports receiving and the newest lines, with the printer by name', async () => {
    await drawPage();

    // Folded, the row still says each stream is receiving.
    await expect
      .element(page.getByTestId('summary-syslog').getByText('Receiving'))
      .toBeVisible();
    await expect.element(page.getByTestId('stream-syslog')).not.toBeVisible();

    await page.getByRole('button', { name: /^Receiving/u }).click();
    await expect
      .element(page.getByTestId('stream-syslog').getByText('Receiving').first())
      .toBeVisible();
    expect(window.location.search).toContain('status=open');
    await expect.element(page.getByText('Connection lost')).toBeVisible();
    await expect
      .element(page.getByRole('cell', { name: '192.168.1.20' }))
      .toBeVisible();
    await expect
      .element(page.getByRole('cell', { name: 'MK4 bench' }).first())
      .toBeVisible();
    await expect
      .element(page.getByText('Error', { exact: true }))
      .toBeVisible();
  });

  it('opens on the tab and the filters the address names, and keeps a change there', async () => {
    window.history.replaceState(
      null,
      '',
      `${window.location.pathname}?q=heatbreak&level=warning`,
    );
    const stub = await drawPage();

    await expect.element(page.getByRole('searchbox')).toHaveValue('heatbreak');
    await expect
      .poll(() => stub.made(`GET ${MOUNT}/logs`).at(-1)?.search ?? '')
      .toContain('q=heatbreak');

    await page.getByRole('tab', { name: 'Metrics' }).click();
    expect(window.location.search).toContain('tab=metrics');
    expect(window.location.search).toContain('q=heatbreak');
  });

  it('stops a printer sending with a bare M334, confirmed on its display', async () => {
    const stub = await drawPage({
      routes: {
        [`POST ${MOUNT}/devices/mk4/setup`]: {
          status: 202,
          body: { device_id: 'mk4', gcode: 'M334', confirm: 'Press Yes' },
        },
      },
    });

    await page.getByRole('button', { name: 'Set up a printer' }).click();
    await page
      .getByTestId('setup-dialog')
      .getByRole('combobox', { name: 'Printer' })
      .click();
    await page.getByRole('option', { name: 'MK4 bench' }).click();
    await page.getByRole('button', { name: 'Stop sending' }).click();

    await expect
      .element(page.getByText(/stops sending its log lines and metrics/u))
      .toBeVisible();
    expect(
      stub.made(`POST ${MOUNT}/devices/mk4/setup`).map((call) => call.body),
    ).toEqual([expect.objectContaining({ syslog: false, metrics: false })]);
  });

  it('reads the lines again on refresh, and leaves live mode as it was', async () => {
    const arrived = {
      cursor: 'c3',
      at: '2026-10-19T14:03:25Z',
      device_id: 'mk4',
      mac: '10:9c:70:2a:bb:cd',
      severity: 'warning',
      component: 'Connect',
      text: 'Connection restored',
    };
    let held = LINES;
    // A live poll is answered with nothing new, so only a fresh read shows
    // the line that arrived.
    const stub = await drawPage({
      routes: {
        [`GET ${MOUNT}/logs`]: (call) =>
          call.search.includes('after=')
            ? { status: 200, body: { lines: [], newest: 'c2' } }
            : { status: 200, body: { lines: held, newest: held[0]?.cursor } },
      },
    });

    await expect.element(page.getByText('Connection lost')).toBeVisible();
    await expect
      .element(page.getByText('Connection restored'))
      .not.toBeInTheDocument();
    await page.getByRole('switch', { name: 'Live' }).click();
    const read = stub.made(`GET ${MOUNT}/status`).length;
    held = [arrived, ...LINES];

    await page.getByRole('button', { name: 'Refresh' }).click();

    await expect.element(page.getByText('Connection restored')).toBeVisible();
    await expect.element(page.getByText('Connection lost')).toBeVisible();
    await expect
      .element(page.getByRole('switch', { name: 'Live' }))
      .toBeChecked();
    expect(stub.made(`GET ${MOUNT}/status`).length).toBeGreaterThan(read);
  });

  it('deletes what is older than an hour after the person confirms', async () => {
    const stub = await drawPage({
      routes: {
        [`POST ${MOUNT}/data/purge`]: {
          status: 200,
          body: {
            purged: {
              syslog: { records: 12, bytes: 2048, segments: 1 },
              metrics: { records: 30, bytes: 4096, segments: 1 },
            },
          },
        },
      },
    });

    await page.getByRole('button', { name: 'Delete data' }).click();
    await page.getByRole('menuitem', { name: 'Older than 1 hour' }).click();
    await expect
      .element(
        page.getByRole('alertdialog').getByText(/from the last hour stay/u),
      )
      .toBeVisible();
    expect(stub.made(`POST ${MOUNT}/data/purge`)).toHaveLength(0);

    await page
      .getByRole('alertdialog')
      .getByRole('button', { name: 'Delete' })
      .click();

    await expect
      .element(page.getByTestId('purged').getByText(/Removed 42 records/u))
      .toBeVisible();
    expect(
      stub.made(`POST ${MOUNT}/data/purge`).map((call) => call.body),
    ).toEqual([{ older_than_hours: 1 }]);
  });

  it('says a stream that received nothing lately is waiting, not receiving', async () => {
    const quiet = {
      ...STATUS,
      streams: STATUS.streams.map((one) =>
        one.stream === 'metrics'
          ? { ...one, last_received: '2026-10-19T10:00:00Z' }
          : one,
      ),
    };
    await drawPage({
      routes: { [`GET ${MOUNT}/status`]: { status: 200, body: quiet } },
    });

    await expect
      .element(
        page.getByTestId('summary-metrics').getByText('Waiting for data'),
      )
      .toBeVisible();
    await expect
      .element(page.getByTestId('summary-syslog').getByText('Receiving'))
      .toBeVisible();
  });

  it('says in words that the ports were not allowed', async () => {
    const ungranted = {
      ...STATUS,
      streams: STATUS.streams.map((one) => ({
        ...one,
        listener: { granted: false, listening: false },
      })),
    };
    await drawPage({
      routes: { [`GET ${MOUNT}/status`]: { status: 200, body: ungranted } },
    });

    await expect
      .element(
        page
          .getByTestId('not-granted')
          .getByText(/tick Allow it to listen on these ports/u),
      )
      .toBeVisible();
  });

  it('tells nothing arrived from nothing matched', async () => {
    const stub = await drawPage({
      routes: { [`GET ${MOUNT}/logs`]: { status: 200, body: { lines: [] } } },
    });

    await expect.element(page.getByTestId('log-empty')).toBeVisible();

    await page.getByRole('searchbox').fill('lost');
    await expect.element(page.getByTestId('log-no-match')).toBeVisible();
    expect(stub.made(`GET ${MOUNT}/logs`).at(-1)?.search).toContain('q=lost');
  });

  it('offers to clear the filters while any narrows the lines', async () => {
    await drawPage();
    const clear = page.getByRole('button', { name: 'Clear filters' });

    await expect.element(page.getByText('Connection lost')).toBeVisible();
    await expect.element(clear).not.toBeInTheDocument();

    await page.getByRole('searchbox').fill('lost');
    await clear.click();

    await expect.element(page.getByRole('searchbox')).toHaveValue('');
    await expect.element(clear).not.toBeInTheDocument();
  });

  it('draws a refusal by its code and not as an empty log', async () => {
    await drawPage({
      routes: {
        [`GET ${MOUNT}/logs`]: refusal(
          503,
          'buddy-debug-logs/STORE_FAILED',
          'disk full',
        ),
      },
    });

    await expect
      .element(page.getByText('The module could not read or write its files'))
      .toBeVisible();
    await expect.element(page.getByTestId('log-empty')).not.toBeInTheDocument();
  });

  it('sends the setup for one stream and tells the person to confirm on the printer', async () => {
    const stub = await drawPage({
      routes: {
        [`POST ${MOUNT}/devices/mk4/setup`]: {
          status: 202,
          body: {
            device_id: 'mk4',
            gcode: 'M334 192.168.1.5 0 13514',
            confirm: 'Press Yes',
          },
        },
      },
    });

    await page.getByRole('button', { name: 'Set up a printer' }).click();
    await page
      .getByTestId('setup-dialog')
      .getByRole('combobox', { name: 'Printer' })
      .click();
    await page.getByRole('option', { name: 'MK4 bench' }).click();
    await expect
      .element(page.getByRole('textbox', { name: /Address of this server/ }))
      .toHaveValue('192.168.1.5');
    await page.getByRole('switch', { name: 'Metrics' }).click();
    // Before anything is sent, the person reads that the printer asks.
    await expect
      .element(page.getByText('The printer asks you to confirm'))
      .toBeVisible();
    await page.getByRole('button', { name: 'Send to the printer' }).click();

    await expect
      .element(page.getByText('Waiting for the printer'))
      .toBeVisible();
    await expect
      .element(
        page.getByText('Its display shows G-CODE METRICS CONFIG CHANGE.'),
      )
      .toBeVisible();
    await expect.element(page.getByText(/^Press Yes\./u)).toBeVisible();
    expect(
      stub.made(`POST ${MOUNT}/devices/mk4/setup`).map((call) => call.body),
    ).toEqual([
      {
        host: '192.168.1.5',
        syslog: true,
        metrics: false,
        syslog_port: 13514,
        metrics_port: 8514,
      },
    ]);
  });

  it('shows the steps on the printer to a person who may not send G-code', async () => {
    await drawPage({
      routes: {
        [`POST ${MOUNT}/devices/mk4/setup`]: refusal(
          403,
          'buddy-debug-logs/GCODE_NOT_ALLOWED',
        ),
      },
    });

    await page.getByRole('button', { name: 'Set up a printer' }).click();
    await page
      .getByTestId('setup-dialog')
      .getByRole('combobox', { name: 'Printer' })
      .click();
    await page.getByRole('option', { name: 'MK4 bench' }).click();
    await page.getByRole('button', { name: 'Send to the printer' }).click();

    await expect
      .element(page.getByText('You may not send G-code to this printer'))
      .toBeVisible();
    await expect.element(page.getByTestId('setup-fallback')).toBeVisible();
    await expect
      .element(
        page
          .getByTestId('setup-dialog')
          .getByText(/Settings, then Network, then Metrics/u),
      )
      .toBeVisible();
  });
});

describe('the Logs tab on a printer', () => {
  it('asks for that printer alone and leaves its column out', async () => {
    const stub = stubHost();
    await setup(stub.host);
    const tab = stub.sections['device-tab:logs'];
    if (tab === undefined) throw new Error('the tab was not registered');
    await render(tab, { deviceId: 'mk4' });
    flushSync();

    await expect.element(page.getByText('Connection lost')).toBeVisible();
    expect(stub.made(`GET ${MOUNT}/logs`)[0]?.search).toContain('device=mk4');
    await expect
      .element(page.getByRole('columnheader', { name: 'Printer' }))
      .not.toBeInTheDocument();
    // The printer the tab belongs to is not a filter to clear.
    await expect
      .element(page.getByRole('button', { name: 'Clear filters' }))
      .not.toBeInTheDocument();
  });
});
