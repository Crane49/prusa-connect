<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import type { CommandEntry } from '../../../data';
  import type { StateTransition } from '../../../domain';
  import { fakeDevice } from '../../../test/fakes';
  import PrinterCommandsPanel from './printer-commands-panel.svelte';
  import type { CommandsSource } from './types';

  const { Story } = defineMeta({
    title: 'Parts/Screen components/Printer Detail/Commands',
    component: PrinterCommandsPanel,
  });

  // What a CORE One reports, verbs and their arguments on the one list.
  const CAPABILITIES = [
    'core.command.cancel_object',
    'core.command.cancel_object#id',
    'core.command.disable_steppers',
    'core.command.flash',
    'core.command.flash#path',
    'core.command.home',
    'core.command.home#axis',
    'core.command.load_filament',
    'core.command.load_filament#filament',
    'core.command.mesh_bed_leveling',
    'core.command.move_z',
    'core.command.move_z#distance',
    'core.command.move_z#feedrate',
    'core.command.pause_print',
    'core.command.reset_printer',
    'core.command.resume_print',
    'core.command.set_bed_temperature',
    'core.command.set_bed_temperature#bed_temperature',
    'core.command.set_nozzle_temperature',
    'core.command.set_nozzle_temperature#nozzle_temperature',
    'core.command.set_printer_ready',
    'core.command.set_speed',
    'core.command.set_speed#speed',
    'core.command.stop_print',
    'core.command.unload_filament',
  ];

  const TRANSITIONS: readonly StateTransition[] = [
    { from: 'PRINTING', to: 'PAUSED', verb: 'core.command.pause_print' },
    { from: 'PAUSED', to: 'PRINTING', verb: 'core.command.resume_print' },
    { from: 'PRINTING', to: 'STOPPED', verb: 'core.command.stop_print' },
    { from: 'PAUSED', to: 'STOPPED', verb: 'core.command.stop_print' },
    { from: 'IDLE', to: 'READY', verb: 'core.command.set_printer_ready' },
    { from: 'IDLE', to: 'IDLE', verb: 'core.command.home' },
    { from: 'IDLE', to: 'IDLE', verb: 'core.command.mesh_bed_leveling' },
  ];

  const printer = (state: string, capabilities = CAPABILITIES) => ({
    ...fakeDevice('printer-1', 'MK4S #1042'),
    capabilities,
    state: { '/state': state },
  });

  const idle = printer('IDLE');
  const printing = printer('PRINTING');
  const bare = printer('IDLE', []);

  const commands = (over: Partial<CommandsSource> = {}): CommandsSource => ({
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

  const minute = 60_000;
  const start = Date.UTC(2026, 8, 17, 9, 30);

  const LOG: readonly CommandEntry[] = [
    {
      id: 1,
      kind: 'command',
      sent: 'core.command.home',
      ok: true,
      answer: 'ok',
      code: undefined,
      reason: undefined,
      ignored: [],
      at: start,
    },
    {
      id: 2,
      kind: 'gcode',
      sent: 'M104 S215',
      ok: true,
      answer: 'ok',
      code: 'accepted',
      reason: undefined,
      ignored: [],
      at: start + minute,
    },
    {
      id: 3,
      kind: 'gcode',
      sent: 'M997',
      ok: false,
      answer: 'This server never sends that gcode word',
      code: 'core.printer/GCODE_NOT_ALLOWED',
      reason: undefined,
      ignored: [],
      at: start + 2 * minute,
    },
  ];

  const PRESETS = [
    {
      name: 'preheat-pla',
      steps: ['M104 S215', 'M140 S60'],
      summary: 'Warm the nozzle and the bed for PLA.',
      version: 'v7',
    },
    {
      name: 'park-head',
      steps: ['G28', 'G1 Z100'],
      summary: 'Home, then lift the head clear of the bed.',
      version: 'v7',
    },
  ];
</script>

<!-- Every verb a CORE One accepts while it sits idle. -->
<Story name="Default" asChild>
  <PrinterCommandsPanel
    commands={commands({ presets: PRESETS })}
    device={idle}
    transitions={TRANSITIONS}
  />
</Story>

<Story name="With Refusals" asChild>
  <PrinterCommandsPanel
    commands={commands()}
    device={printing}
    transitions={TRANSITIONS}
  />
</Story>

<!-- A page that fetched no device-state vocabulary, mid-print. -->
<Story name="Without A Vocabulary" asChild>
  <PrinterCommandsPanel commands={commands()} device={printing} />
</Story>

<!-- A machine holding the printer: every verb is refused by one reason. -->
<Story name="Locked" asChild>
  <PrinterCommandsPanel
    commands={commands()}
    device={idle}
    transitions={TRANSITIONS}
    locked
  />
</Story>

<!-- A driver that reports no capability at all. -->
<!-- Firmware adds setters faster than a catalogue is translated. -->
<Story name="A Verb Nobody Named" asChild>
  <PrinterCommandsPanel
    commands={commands()}
    device={printer('IDLE', [
      ...CAPABILITIES,
      'core.command.set_new_gizmo',
      'core.command.set_new_gizmo#level',
    ])}
    transitions={TRANSITIONS}
  />
</Story>

<Story name="No Commands" asChild>
  <PrinterCommandsPanel commands={commands()} device={bare} />
</Story>

<!-- The console after a few sends, one of which the server refused. -->
<Story name="With A Log" asChild>
  <PrinterCommandsPanel
    commands={commands({
      log: LOG,
      presets: PRESETS,
      refused: 1,
      answer: 'ok',
    })}
    device={idle}
    transitions={TRANSITIONS}
  />
</Story>

<!-- The server refused the last read, and the code travels with its line. -->
<Story name="Refused By The Server" asChild>
  <PrinterCommandsPanel
    commands={commands({
      error: {
        code: 'core.printer/VERB_UNKNOWN',
        traceId: undefined,
        status: 400,
        params: {},
        message: 'This build has no such capability verb',
      },
    })}
    device={idle}
    transitions={TRANSITIONS}
  />
</Story>

<!-- A send in flight: no row offers a second one until it answers. -->
<Story name="Sending" asChild>
  <PrinterCommandsPanel
    commands={commands({ sending: true, presets: PRESETS })}
    device={idle}
    transitions={TRANSITIONS}
  />
</Story>

<!-- The panel's own shape while the record is on its way. -->
<Story name="Loading" asChild>
  <PrinterCommandsPanel commands={commands()} loading />
</Story>
