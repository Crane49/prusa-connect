import { describe, expect, it, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import type { CommandEntry } from '../../../data';
import type { StateTransition } from '../../../domain';
import { expectNoViolations } from '../../../test/axe';
import { fakeDevice } from '../../../test/fakes';
import PrinterCommandsPanel from './printer-commands-panel.svelte';
import type { CommandsSource } from './types';

const CAPABILITIES = [
  'core.command.home',
  'core.command.home#axis',
  'core.command.pause_print',
  'core.command.stop_print',
];

const TRANSITIONS: readonly StateTransition[] = [
  { from: 'PRINTING', to: 'PAUSED', verb: 'core.command.pause_print' },
  { from: 'PRINTING', to: 'STOPPED', verb: 'core.command.stop_print' },
];

const printer = (state = 'PRINTING') => ({
  ...fakeDevice('printer-1', 'MK4S #1042'),
  capabilities: CAPABILITIES,
  state: { '/state': state },
});

const fakeCommands = (over: Partial<CommandsSource> = {}): CommandsSource => ({
  answer: undefined,
  pending: undefined,
  clear: () => undefined,
  error: undefined,
  limit: 100,
  loadPresets: () => Promise.resolve(),
  log: [],
  presets: [],
  refused: 0,
  runPreset: () => Promise.resolve(),
  send: () => Promise.resolve({ kind: 'accepted' as const }),
  sendGcode: () => Promise.resolve(),
  sending: false,
  ...over,
});

const entry = (over: Partial<CommandEntry> = {}): CommandEntry => ({
  id: 1,
  kind: 'command',
  sent: 'core.command.home',
  ok: true,
  answer: 'ok',
  code: undefined,
  reason: undefined,
  ignored: [],
  at: Date.UTC(2026, 8, 17, 9, 30),
  ...over,
});

describe('PrinterCommandsPanel', () => {
  // The one surface that lists everything.
  it('lists a refused verb with the reason the domain gives', async () => {
    await render(PrinterCommandsPanel, {
      props: {
        commands: fakeCommands(),
        device: printer('IDLE'),
        transitions: TRANSITIONS,
      },
    });

    await expect.element(page.getByText('Stop print')).toBeVisible();
    // Pausing and stopping both run from PRINTING.
    expect(
      page.getByText('This works when the printer is PRINTING.').elements(),
    ).toHaveLength(2);
  });

  it('sends nothing for a verb the printer refuses', async () => {
    const send = vi.fn(() => Promise.resolve({ kind: 'accepted' as const }));
    await render(PrinterCommandsPanel, {
      props: {
        commands: fakeCommands({ send }),
        device: printer('IDLE'),
        transitions: TRANSITIONS,
      },
    });

    const button = page.getByRole('button', {
      name: 'Run Stop print',
    });
    await expect.element(button).toBeDisabled();
    expect(send).not.toHaveBeenCalled();
  });

  // Stopping a print throws the object away.
  it('asks before a send that cannot be taken back', async () => {
    const send = vi.fn(() => Promise.resolve({ kind: 'accepted' as const }));
    await render(PrinterCommandsPanel, {
      props: {
        commands: fakeCommands({ send }),
        device: printer(),
        transitions: TRANSITIONS,
      },
    });

    await page.getByRole('button', { name: 'Run Stop print' }).click();
    expect(send).not.toHaveBeenCalled();

    await expect
      .element(page.getByText('This changes the printer and cannot be undone.'))
      .toBeVisible();
    await page
      .getByRole('alertdialog')
      .getByRole('button', { name: 'Run', exact: true })
      .click();
    expect(send).toHaveBeenCalledWith('core.command.stop_print', undefined);
  });

  // The capability list declares the arguments as `verb#name`.
  it('collects the arguments a verb declares and sends them', async () => {
    const send = vi.fn(() => Promise.resolve({ kind: 'accepted' as const }));
    await render(PrinterCommandsPanel, {
      props: {
        commands: fakeCommands({ send }),
        device: printer(),
        transitions: TRANSITIONS,
      },
    });

    await page.getByRole('button', { name: 'Run Home axes' }).click();
    await page.getByLabelText('axis').fill('Z');
    await page
      .getByRole('alertdialog')
      .getByRole('button', { name: 'Run', exact: true })
      .click();

    expect(send).toHaveBeenCalledWith('core.command.home', { axis: 'Z' });
  });

  // A capability key is what a bug report quotes.
  it('names a verb in words rather than by its wire token', async () => {
    const { container } = await render(PrinterCommandsPanel, {
      props: {
        commands: fakeCommands(),
        device: printer(),
        transitions: TRANSITIONS,
      },
    });

    await expect.element(page.getByText('Home axes')).toBeVisible();
    await expect
      .element(page.getByText('Starts the homing sequence for the named axes.'))
      .toBeVisible();
    expect(container.textContent).not.toContain('core.command.home');
    // The heading the group sits under, shared with the Controls tab.
    await expect.element(page.getByText('Movement')).toBeVisible();
  });

  // A verb another surface owns is not a verb this catalogue offers.
  it('leaves out the verbs another surface sends', async () => {
    const { container } = await render(PrinterCommandsPanel, {
      props: {
        commands: fakeCommands(),
        device: {
          ...printer(),
          capabilities: [
            ...CAPABILITIES,
            'core.command.start_print',
            'core.command.send_gcode',
          ],
        },
        transitions: TRANSITIONS,
      },
    });

    // Neither reaches the catalogue.
    expect(container.textContent).not.toContain('Start print');
    expect(container.textContent).not.toContain('Send gcode');
    expect(container.textContent).toContain('Home axes');
  });

  // Search reads the name a person types as well as the token.
  it('narrows the list as the reader types, and says when nothing matches', async () => {
    const { container } = await render(PrinterCommandsPanel, {
      props: {
        commands: fakeCommands(),
        device: printer(),
        transitions: TRANSITIONS,
      },
    });

    await page.getByLabelText('Search commands').fill('home');
    expect(container.textContent).not.toContain('Stop print');
    await expect.element(page.getByText('Home axes')).toBeVisible();

    await page.getByLabelText('Search commands').fill('nothing-like-this');
    await expect.element(page.getByText('No matching commands')).toBeVisible();
    await expect
      .element(page.getByText('No command matches “nothing-like-this”.'))
      .toBeVisible();
  });

  // A printer that offers no verb is an absence the panel names.
  it('says so when the printer offers no verb', async () => {
    await render(PrinterCommandsPanel, {
      props: {
        commands: fakeCommands(),
        device: { ...fakeDevice('printer-1'), capabilities: [] },
      },
    });

    await expect
      .element(page.getByText('This printer offers no command.'))
      .toBeVisible();
  });

  it('names the history this server does not publish', async () => {
    await render(PrinterCommandsPanel, {
      props: { commands: fakeCommands(), device: printer() },
    });

    await expect
      .element(
        page.getByText(
          'This server does not publish a command history, so this log holds only what this page sent.',
        ),
      )
      .toBeVisible();
  });

  // The third absence: the registry code travels with the server's own line.
  it('draws a refusal from the server by its code', async () => {
    await render(PrinterCommandsPanel, {
      props: {
        commands: fakeCommands({
          error: {
            code: 'core.printer/VERB_UNKNOWN',
            traceId: undefined,
            status: 400,
            params: {},
            message: 'This build has no such capability verb',
          },
        }),
        device: printer(),
      },
    });

    await expect
      .element(page.getByText('core.printer/VERB_UNKNOWN'))
      .toBeVisible();
  });

  // The panel waits in the shape of what it will draw.
  it('draws its own loading state and no catalogue', async () => {
    await render(PrinterCommandsPanel, {
      props: { commands: fakeCommands(), device: printer(), loading: true },
    });

    await expect
      .element(
        page.getByRole('status', {
          name: 'Reading the commands this printer accepts',
        }),
      )
      .toBeVisible();
    expect(page.getByText('Home axes').elements()).toHaveLength(0);
  });

  // A chip on the Printer tab sends the reader here for one command.
  it('marks the row a handed verb names, and no row for one it does not draw', async () => {
    const quiet = await render(PrinterCommandsPanel, {
      props: {
        commands: fakeCommands(),
        device: printer(),
        transitions: TRANSITIONS,
        highlight: 'core.command.send_gcode',
      },
    });
    const lit = await render(PrinterCommandsPanel, {
      props: {
        commands: fakeCommands(),
        device: printer(),
        transitions: TRANSITIONS,
        highlight: 'core.command.home',
      },
    });

    await expect
      .poll(() =>
        [...lit.container.querySelectorAll('[aria-current]')].map(
          (row) => row.id,
        ),
      )
      .toEqual(['command-core.command.home']);
    expect(quiet.container.querySelectorAll('[aria-current]')).toHaveLength(0);
  });

  // The printer answers a command only once it has finished reading its drive.
  it('says a pending command waits for the drive read', async () => {
    const hint = 'The printer is finishing a read of its drive.';
    const { rerender } = await render(PrinterCommandsPanel, {
      props: {
        commands: fakeCommands({
          pending: 'core.command.pause_print',
          sending: true,
        }),
        device: printer(),
        transitions: TRANSITIONS,
        readingDrive: true,
      },
    });

    await expect.element(page.getByText(hint)).toBeVisible();
    expect(page.getByText(hint).elements()).toHaveLength(1);

    await rerender({ readingDrive: false });
    await expect.element(page.getByText(hint)).not.toBeInTheDocument();
  });

  it('has nothing axe objects to', async () => {
    const { container } = await render(PrinterCommandsPanel, {
      props: {
        commands: fakeCommands({ log: [entry()], refused: 0 }),
        device: printer(),
        transitions: TRANSITIONS,
      },
    });

    await expectNoViolations(container);
  });
});
