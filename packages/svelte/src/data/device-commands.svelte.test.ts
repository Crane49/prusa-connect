import { describe, expect, it, vi } from 'vitest';

import {
  createDeviceCommands,
  type DeviceCommandsClient,
} from './device-commands.svelte';
import type { CommandRecord } from './services/command-log';
import type { GcodeOutcome } from './services/gcode';
import { StubApiError } from './stubs';
import type { Timers } from './timers';

interface Calls {
  verbs: string[];
  keys: (string | undefined)[];
  reads: number;
  lines: string[][];
  presets: string[];
  listed: number;
}

const outcomeOf = (lines: readonly string[], refusedAt = -1): GcodeOutcome => ({
  completed: refusedAt < 0 ? lines.length : refusedAt,
  stopped: refusedAt >= 0,
  steps: lines.map((gcode, at) => {
    if (refusedAt >= 0 && at === refusedAt) {
      return {
        code: 'core.device/GCODE_REFUSED',
        gcode,
        status: 'refused' as const,
      };
    }
    if (refusedAt >= 0 && at > refusedAt) {
      return { gcode, status: 'not_attempted' as const };
    }

    return { answer: 'ok', gcode, status: 'accepted' as const };
  }),
});

const stubClient = (
  options: {
    readonly refusedAt?: number;
    readonly refuse?: Error;
    readonly commandOk?: boolean;
    readonly ignored?: readonly string[];
    readonly duplicate?: boolean;
    /** What each read of the command record answers, the last one repeated. */
    readonly records?: readonly (CommandRecord | StubApiError)[];
  } = {},
): { client: DeviceCommandsClient; calls: Calls } => {
  const calls: Calls = {
    verbs: [],
    keys: [],
    reads: 0,
    lines: [],
    presets: [],
    listed: 0,
  };
  const client: DeviceCommandsClient = {
    sendDeviceCommand: (request) => {
      calls.keys.push(request.body.key);
      if (options.refuse) throw options.refuse;
      calls.verbs.push(request.body.verb);
      if (options.duplicate === true) {
        return Promise.resolve({
          command_id: 'c-1',
          duplicate: true,
          ok: false,
        });
      }

      return Promise.resolve({
        answer: 'ok',
        command_id: 'c-1',
        ok: options.commandOk ?? true,
        ...(options.commandOk === false
          ? {
              fault_code: 'core.printer/COMMAND_REFUSED',
              machine_reason: 'Printer is busy',
            }
          : {}),
        ...(options.ignored === undefined
          ? {}
          : { ignored: [...options.ignored] }),
      });
    },
    sendPrinterGcode: (request) => {
      if (options.refuse) throw options.refuse;
      const lines = request.body.lines ?? [];
      calls.lines.push([...lines]);

      return Promise.resolve(outcomeOf(lines, options.refusedAt ?? -1));
    },
    listGcodePresets: () => {
      calls.listed += 1;

      return Promise.resolve({
        items: [{ name: 'home', steps: ['G28'], version: '1' }],
      });
    },
    getCommand: () => {
      const answers = options.records ?? [];
      const answer = answers[Math.min(calls.reads, answers.length - 1)];
      calls.reads += 1;
      if (answer === undefined || answer instanceof StubApiError) {
        return Promise.reject(
          answer ?? new StubApiError('core.resource/NOT_FOUND', 404),
        );
      }

      return Promise.resolve(answer);
    },
    runPrinterGcodePreset: (request) => {
      calls.presets.push(request.path.preset);

      return Promise.resolve(outcomeOf(['G28']));
    },
  };

  return { client, calls };
};

describe('DeviceCommands', () => {
  it('starts with an empty log', () => {
    const { client } = stubClient();
    const store = createDeviceCommands(client, 'printer-1');

    expect(store.log).toEqual([]);
    expect(store.last).toBeUndefined();
    expect(store.answer).toBeUndefined();
    expect(store.deviceId).toBe('printer-1');
  });

  it('sends a verb and writes what the printer answered', async () => {
    const { client, calls } = stubClient();
    const store = createDeviceCommands(client, 'printer-1');

    await store.send('core.command.pause');

    expect(calls.verbs).toEqual(['core.command.pause']);
    expect(store.log).toHaveLength(1);
    expect(store.last?.kind).toBe('command');
    expect(store.answer).toBe('ok');
    expect(store.refused).toBe(0);
  });

  // A caller that sends several verbs in a row waits on each, and learns
  // which ones the printer took.
  it('answers whether the printer took the verb', async () => {
    const took = createDeviceCommands(stubClient().client, 'printer-1');
    const refused = createDeviceCommands(
      stubClient({ commandOk: false }).client,
      'printer-1',
    );

    await expect(took.send('core.command.pause')).resolves.toEqual({
      kind: 'accepted',
    });
    // The printer's own words and the code come back with the refusal.
    await expect(refused.send('core.command.pause')).resolves.toEqual({
      kind: 'refused',
      fault: {
        code: 'core.printer/COMMAND_REFUSED',
        status: undefined,
        params: {},
        message: 'Printer is busy',
      },
    });
  });

  // A verb sent while another is on its way is not sent at all, and a caller
  // has to be able to tell that from a refusal.
  it('answers unsent for a verb sent while another is on its way', async () => {
    const { client, calls } = stubClient();
    const store = createDeviceCommands(client, 'printer-1');

    const first = store.send('core.command.pause');
    await expect(store.send('core.command.resume')).resolves.toEqual({
      kind: 'unsent',
    });
    await expect(first).resolves.toEqual({ kind: 'accepted' });
    expect(calls.verbs).toEqual(['core.command.pause']);
  });

  // A caller that reports the outcome itself marks the entry, so the page's
  // own announcer does not say it a second time.
  it('marks an entry a caller asked to report itself', async () => {
    const { client } = stubClient({ commandOk: false });
    const store = createDeviceCommands(client, 'printer-1');

    await store.send(
      'core.command.delete_file',
      { path: 'usb/a.gcode' },
      {
        quiet: true,
      },
    );
    expect(store.last?.quiet).toBe(true);

    await store.send('core.command.pause');
    expect(store.last?.quiet).toBe(false);
  });

  it('keeps the fault code when a printer refused a verb', async () => {
    const { client } = stubClient({ commandOk: false });
    const store = createDeviceCommands(client, 'printer-1');

    await store.send('core.command.start_print');

    expect(store.last?.ok).toBe(false);
    expect(store.last?.code).toBe('core.printer/COMMAND_REFUSED');
    expect(store.last?.reason).toBe('Printer is busy');
    expect(store.refused).toBe(1);
  });

  // A driver whose wire form cannot carry an argument runs the command
  // without it and says which one it dropped.
  it('keeps the arguments the printer did not take', async () => {
    const { client } = stubClient({ ignored: ['filament'] });
    const store = createDeviceCommands(client, 'printer-1');

    await store.send('core.command.load_filament', { filament: 'PETG' });

    expect(store.last?.ok).toBe(true);
    expect(store.last?.ignored).toEqual(['filament']);
  });

  it('keeps no ignored argument when the printer took them all', async () => {
    const { client } = stubClient();
    const store = createDeviceCommands(client, 'printer-1');

    await store.send('core.command.load_filament', { filament: 'PETG' });

    expect(store.last?.ignored).toEqual([]);
  });

  it('writes one entry per G-code line, with its status', async () => {
    const { client, calls } = stubClient();
    const store = createDeviceCommands(client, 'printer-1');

    await store.sendGcode('G28\n; a comment\n\nM104 S200');

    expect(calls.lines).toEqual([['G28', 'M104 S200']]);
    expect(store.log.map((entry) => entry.sent)).toEqual(['G28', 'M104 S200']);
    expect(store.log.every((entry) => entry.kind === 'gcode')).toBe(true);
    expect(store.answer).toBe('ok');
  });

  it('says which line stopped the run', async () => {
    const { client } = stubClient({ refusedAt: 1 });
    const store = createDeviceCommands(client, 'printer-1');

    await store.sendGcode('G28\nM999\nM104 S200');

    expect(store.log.map((entry) => entry.code)).toEqual([
      'accepted',
      'core.device/GCODE_REFUSED',
      'not_attempted',
    ]);
    expect(store.refused).toBe(2);
  });

  it('sends nothing when there is nothing to send', async () => {
    const { client, calls } = stubClient();
    const store = createDeviceCommands(client, 'printer-1');

    await store.sendGcode('  \n; only a comment\n');

    expect(calls.lines).toEqual([]);
    expect(store.log).toEqual([]);
  });

  it('reports a refusal by its code and never as a sentence', async () => {
    const { client } = stubClient({
      refuse: new StubApiError('core.auth/ACCESS_DENIED', 403),
    });
    const store = createDeviceCommands(client, 'printer-1');

    await store.sendGcode('G28');

    expect(store.error?.code).toBe('core.auth/ACCESS_DENIED');
    expect(store.error?.status).toBe(403);
    expect(store.last?.code).toBe('core.auth/ACCESS_DENIED');
  });

  it('keeps the log bounded, oldest first out', async () => {
    const { client } = stubClient();
    const store = createDeviceCommands(client, 'printer-1', { limit: 3 });

    await store.sendGcode('G1\nG2\nG3\nG4\nG5');

    expect(store.limit).toBe(3);
    expect(store.log.map((entry) => entry.sent)).toEqual(['G3', 'G4', 'G5']);
  });

  it('reads the presets and runs one', async () => {
    const { client, calls } = stubClient();
    const store = createDeviceCommands(client, 'printer-1');

    await store.loadPresets();
    await store.runPreset('home');

    expect(calls.listed).toBe(1);
    expect(store.presets.map((row) => row.name)).toEqual(['home']);
    expect(calls.presets).toEqual(['home']);
    expect(store.log.map((entry) => entry.sent)).toEqual(['G28']);
  });

  it('empties the log when a person clears it', async () => {
    const { client } = stubClient();
    const store = createDeviceCommands(client, 'printer-1');
    await store.send('core.command.pause');

    store.clear();

    expect(store.log).toEqual([]);
  });

  // A proxy replaying the request is answered by the key, so each press needs
  // a key of its own.
  it('sends each verb under a new idempotency key', async () => {
    const { client, calls } = stubClient();
    const store = createDeviceCommands(client, 'printer-1');

    await store.send('core.command.pause');
    await store.send('core.command.pause');

    expect(calls.keys).toHaveLength(2);
    expect(calls.keys[0]).toMatch(/^[0-9a-f]{32}$/);
    expect(calls.keys[1]).not.toBe(calls.keys[0]);
  });

  it('says how long a command the printer answered took', async () => {
    const { client, calls } = stubClient();
    const store = createDeviceCommands(client, 'printer-1');

    await store.send('core.command.pause');

    expect(store.last?.status).toBe('succeeded');
    expect(store.last?.elapsedMs).toBeGreaterThanOrEqual(0);
    expect(calls.reads).toBe(0);
  });
});

/** Runs a waiting task as soon as the current one yields. */
const instant: Timers = {
  set: (task) => {
    queueMicrotask(task);

    return 0;
  },
  clear: () => undefined,
};

const record = (over: Partial<CommandRecord> = {}): CommandRecord => ({
  command_id: 'c-1',
  elapsed_ms: 1200,
  ok: true,
  origin: 'prusalink:printer-1',
  verb: 'core.command.pause',
  ...over,
});

describe('DeviceCommands, a command still open after the send', () => {
  // The record is written a moment after the command ends, so the first reads
  // answer not found or open.
  it('reads the record until it settles, and stays sending until then', async () => {
    const { client, calls } = stubClient({
      duplicate: true,
      records: [
        new StubApiError('core.resource/NOT_FOUND', 404),
        record({ ok: false }),
        record({ status: 'succeeded' }),
      ],
    });
    const store = createDeviceCommands(client, 'printer-1', {
      timers: instant,
    });

    const sent = store.send('core.command.pause');
    expect(store.sending).toBe(true);

    await expect(sent).resolves.toEqual({ kind: 'accepted' });
    expect(calls.reads).toBe(3);
    expect(store.sending).toBe(false);
    expect(store.last).toMatchObject({
      ok: true,
      status: 'succeeded',
      elapsedMs: 1200,
    });
  });

  it('says the outcome is unknown when the record never settles', async () => {
    const { client, calls } = stubClient({
      duplicate: true,
      records: [record({ ok: false })],
    });
    const store = createDeviceCommands(client, 'printer-1', {
      timers: instant,
    });

    await store.send('core.command.pause');

    // Ten seconds of reads, half a second apart, and the first one.
    expect(calls.reads).toBe(21);
    expect(store.last).toMatchObject({ ok: false, unknown: true });
  });

  it('reads the record a failed send names', async () => {
    const { client } = stubClient({
      refuse: new StubApiError('core.printer/UNREACHABLE', 503, {
        command_id: 'c-1',
      }),
      records: [record({ ok: false, outcome_unknown: true, status: 'failed' })],
    });
    const store = createDeviceCommands(client, 'printer-1', {
      timers: instant,
    });

    await store.send('core.command.pause');

    expect(store.last).toMatchObject({ unknown: true, status: 'failed' });
  });

  // A server that keeps no command record will never have one to read.
  it('stops reading at any refusal other than not found', async () => {
    const { client, calls } = stubClient({
      refuse: new StubApiError('core.printer/TIMEOUT', 504, {
        command_id: 'c-1',
      }),
      records: [new StubApiError('core.resource/HISTORY_NOT_CONFIGURED', 501)],
    });
    const store = createDeviceCommands(client, 'printer-1', {
      timers: instant,
    });

    await store.send('core.command.pause');

    expect(calls.reads).toBe(1);
    expect(store.last).toMatchObject({
      ok: false,
      code: 'core.printer/TIMEOUT',
      unknown: false,
    });
  });

  it('says the outcome is unknown when no answer arrived', async () => {
    const lost = new Error('fetch failed');
    lost.name = 'NetworkError';
    const { client, calls } = stubClient({ refuse: lost });
    const store = createDeviceCommands(client, 'printer-1');

    await store.send('core.command.pause');

    expect(calls.reads).toBe(0);
    expect(store.last?.unknown).toBe(true);
  });

  it('stops reading when the page leaves the printer', async () => {
    const held: (() => void)[] = [];
    const { client, calls } = stubClient({
      duplicate: true,
      records: [record({ ok: false })],
    });
    const store = createDeviceCommands(client, 'printer-1', {
      timers: {
        set: (task) => held.push(task),
        clear: () => undefined,
      },
    });

    const sent = store.send('core.command.pause');
    await vi.waitFor(() => {
      expect(held).toHaveLength(1);
    });
    store.abort();

    await sent;
    expect(calls.reads).toBe(1);
    expect(store.sending).toBe(false);
  });
});

describe('DeviceCommands, the verb under way', () => {
  const manual = () => {
    const tasks = new Map<number, () => void>();
    let next = 1;
    const timers: Timers = {
      set: (task) => {
        tasks.set(next, task);

        return next++;
      },
      clear: (handle) => {
        tasks.delete(handle);
      },
    };

    return { timers, tasks };
  };

  it('is the verb while it is sent, and stays until the state changes', async () => {
    const { client } = stubClient();
    const { timers } = manual();
    const store = createDeviceCommands(client, 'printer-1', { timers });
    store.observe('PAUSED');

    const sent = store.send('core.command.resume_print');
    expect(store.pending).toBe('core.command.resume_print');
    await sent;
    expect(store.sending).toBe(false);
    expect(store.pending).toBe('core.command.resume_print');

    store.observe('PAUSED');
    expect(store.pending).toBe('core.command.resume_print');
    store.observe('PRINTING');
    expect(store.pending).toBeUndefined();
  });

  it('lets go when the state never changes', async () => {
    const { client } = stubClient();
    const { timers, tasks } = manual();
    const store = createDeviceCommands(client, 'printer-1', { timers });

    await store.send('core.command.resume_print');
    expect(store.pending).toBe('core.command.resume_print');
    for (const task of [...tasks.values()]) task();
    expect(store.pending).toBeUndefined();
  });

  it('is not pending after a refusal', async () => {
    const { client } = stubClient({ commandOk: false });
    const store = createDeviceCommands(client, 'printer-1');

    await store.send('core.command.resume_print');
    expect(store.pending).toBeUndefined();
  });
});
