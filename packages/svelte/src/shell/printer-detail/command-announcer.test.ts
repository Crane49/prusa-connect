import { afterEach, describe, expect, it, vi } from 'vitest';

import type { CommandEntry } from '../../data';
import { toast } from '../../primitives';
import { CommandAnnouncer } from './command-announcer';

const entry = (over: Partial<CommandEntry>): CommandEntry => ({
  id: 1,
  kind: 'command',
  sent: 'core.command.load_filament',
  ok: true,
  answer: undefined,
  code: undefined,
  reason: undefined,
  ignored: [],
  at: 0,
  ...over,
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('CommandAnnouncer', () => {
  // The title comes from the error catalogue by the code, and the printer's
  // own words go under it, untranslated.
  it('names a refusal by its code, with the printer reason as the detail', () => {
    const error = vi.spyOn(toast, 'error').mockReturnValue('');
    const announcer = new CommandAnnouncer();

    announcer.observe(
      'printer-1',
      entry({
        ok: false,
        code: 'core.printer/COMMAND_REFUSED',
        reason: 'Printer is busy',
      }),
    );

    expect(error).toHaveBeenCalledWith('The printer refused the command', {
      description: 'Printer is busy · core.printer/COMMAND_REFUSED',
    });
  });

  it('names a refusal with no reason as a printer busy with another command', () => {
    const error = vi.spyOn(toast, 'error').mockReturnValue('');
    const announcer = new CommandAnnouncer();

    announcer.observe(
      'printer-1',
      entry({ ok: false, code: 'core.printer/COMMAND_REFUSED_WITHOUT_REASON' }),
    );

    expect(error).toHaveBeenCalledWith(
      'The printer is busy with another command and refused this one',
      { description: 'core.printer/COMMAND_REFUSED_WITHOUT_REASON' },
    );
  });

  it('says a refusal with no code and no reason plainly', () => {
    const error = vi.spyOn(toast, 'error').mockReturnValue('');
    const announcer = new CommandAnnouncer();

    announcer.observe('printer-1', entry({ ok: false }));

    expect(error).toHaveBeenCalledWith(
      'The printer would not run that command.',
      {},
    );
  });

  it('names each argument the printer ran the command without', () => {
    const warning = vi.spyOn(toast, 'warning').mockReturnValue('');
    const announcer = new CommandAnnouncer();

    announcer.observe('printer-1', entry({ ignored: ['filament'] }));

    expect(warning).toHaveBeenCalledWith('Load filament: Done', {
      description:
        'The printer ran the command but did not take the filament type.',
    });
  });

  // An argument this page has no word for is named as the printer spells it.
  it('names an argument it has no word for as the wire spells it', () => {
    const warning = vi.spyOn(toast, 'warning').mockReturnValue('');
    const announcer = new CommandAnnouncer();

    announcer.observe('printer-1', entry({ ignored: ['tool'] }));

    expect(warning).toHaveBeenCalledWith('Load filament: Done', {
      description: 'The printer ran the command but did not take tool.',
    });
  });

  it('says a command the printer took whole is done, and how long it took', () => {
    const success = vi.spyOn(toast, 'success').mockReturnValue('');
    const announcer = new CommandAnnouncer();

    announcer.observe('printer-1', entry({ elapsedMs: 834.4 }));

    expect(success).toHaveBeenCalledWith('Load filament: Done', {
      description: 'Took 834 ms',
    });
  });

  it('gives a command that took longer than a second in seconds', () => {
    const success = vi.spyOn(toast, 'success').mockReturnValue('');
    const announcer = new CommandAnnouncer();

    announcer.observe('printer-1', entry({ elapsedMs: 18845 }));

    expect(success).toHaveBeenCalledWith('Load filament: Done', {
      description: 'Took 18 sec',
    });
  });

  it('names an argument the printer replaced, and with what', () => {
    const warning = vi.spyOn(toast, 'warning').mockReturnValue('');
    const announcer = new CommandAnnouncer();

    announcer.observe('printer-1', entry({ substituted: { filament: 'PLA' } }));

    expect(warning).toHaveBeenCalledWith('Load filament: Done', {
      description: 'The printer ran the command with filament set to PLA.',
    });
  });

  it('says when nobody saw how the command ended', () => {
    const warning = vi.spyOn(toast, 'warning').mockReturnValue('');
    const announcer = new CommandAnnouncer();

    announcer.observe('printer-1', entry({ ok: false, unknown: true }));

    expect(warning).toHaveBeenCalledWith('Load filament: Outcome unknown', {
      description:
        'This server did not see how the command ended, so the printer may have run it.',
    });
  });

  // The command record's own word for how it ended is the title.
  it('names an ending other than a refusal in the command log wording', () => {
    const error = vi.spyOn(toast, 'error').mockReturnValue('');
    const announcer = new CommandAnnouncer();

    announcer.observe(
      'printer-1',
      entry({
        ok: false,
        status: 'timed_out',
        code: 'core.printer/TIMEOUT',
        elapsedMs: 30000,
      }),
    );

    expect(error).toHaveBeenCalledWith('Load filament: Timed out', {
      description: 'core.printer/TIMEOUT · Took 30 sec',
    });
  });

  it('says nothing about a G-code line the printer took', () => {
    const success = vi.spyOn(toast, 'success').mockReturnValue('');
    const announcer = new CommandAnnouncer();

    announcer.observe('printer-1', entry({ kind: 'gcode', sent: 'G28' }));

    expect(success).not.toHaveBeenCalled();
  });

  // The Files tab names the deletes it could not do in one toast of its own.
  it('says nothing about an entry its sender reports itself', () => {
    const error = vi.spyOn(toast, 'error').mockReturnValue('');
    const announcer = new CommandAnnouncer();

    announcer.observe('printer-1', entry({ ok: false, quiet: true }));

    expect(error).not.toHaveBeenCalled();
  });

  it('announces one entry once', () => {
    const error = vi.spyOn(toast, 'error').mockReturnValue('');
    const announcer = new CommandAnnouncer();
    const refused = entry({ ok: false });

    announcer.observe('printer-1', refused);
    announcer.observe('printer-1', refused);

    expect(error).toHaveBeenCalledTimes(1);
  });
});
