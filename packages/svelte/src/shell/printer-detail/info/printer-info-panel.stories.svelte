<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { fakeDevice } from '../../../test/fakes';
  import PrinterInfoPanel from './printer-info-panel.svelte';
  import type {
    PrinterAdministration,
    PrinterHistorySource,
    PrintRow,
  } from './types';

  const { Story } = defineMeta({
    title: 'Parts/Screen components/Printer Detail/Info',
    component: PrinterInfoPanel,
  });

  // What a printer of this generation reports.
  const capabilities = [
    'core.command.pause_print',
    'core.command.resume_print',
    'core.command.stop_print',
    'core.command.cancel_object',
    'core.command.set_speed',
    'core.command.set_flow',
    'core.command.set_printer_ready',
    'core.command.cancel_printer_ready',
    'core.command.set_idle',
    'core.command.home',
    'core.command.disable_steppers',
    'core.command.move_z',
    'core.command.set_nozzle_temperature',
    'core.command.set_bed_temperature',
    'core.command.mesh_bed_leveling',
    'core.command.load_filament',
    'core.command.unload_filament',
    'core.command.reset_printer',
    'core.command.set_hostname',
    'core.command.upgrade',
    'core.command.send_state_info',
    'core.command.start_print',
    'core.command.delete_file',
    'core.command.stop_transfer',
  ];

  // The material package instance of a tool, nested as the API serves it.
  const packageOf = (abbreviation: string) => ({
    package: { material: { abbreviation } },
  });

  // A printer whose driver reports no tools and no build volume.
  const toolsUnknown = {
    ...fakeDevice('printer-1', 'MK4S #1042'),
    actions: ['observe', 'operate', 'configure', 'administer'],
    capabilities,
    firmware: '6.2.3',
    tags: ['assembly', 'workshop 2'],
    link: {
      counted: true,
      dropped_telemetry: 14,
      dropped_telemetry_per_minute: 0.4,
      measured_over_seconds: 300,
      refused_frames: 0,
      refused_frames_per_minute: 0,
    },
    model: 'MK4S',
  };

  const healthy = {
    ...toolsUnknown,
    build_volume: { x_mm: 250, y_mm: 210, z_mm: 220 },
    tools: {
      tool_count: 1,
      tools: {
        '0': {
          type: 'FFF_PRINT_HEAD',
          active: true,
          features: {
            nozzle_diameter: 0.4,
            nozzle_high_flow: false,
            nozzle_hardened: false,
          },
          feeder: {
            slot_count: 5,
            model: 'MMU3',
            base_model: 'MMU3',
            type: 'MMU',
            mmu_progress_code: 17,
            mmu_command_in_progress: 'L',
          },
          nozzle_temp: 215,
        },
        '0.0': {
          type: 'FFF_FEEDER_SLOT',
          active: false,
          material_package_instance: packageOf('PLA'),
        },
        '0.1': {
          type: 'FFF_FEEDER_SLOT',
          active: true,
          material_package_instance: packageOf('PETG'),
        },
        '0.2': {
          type: 'FFF_FEEDER_SLOT',
          active: false,
          material_package_instance: null,
        },
        '0.3': {
          type: 'FFF_FEEDER_SLOT',
          active: false,
          material_package_instance: packageOf('ASA'),
        },
        '0.4': null,
      },
    },
  };

  // An XL with five heads and no feeder. Head 3 is switched off, and head 4
  // has not said what nozzle it carries, how hot it is or what it holds.
  // A printer with one nozzle: head 0, no feeder, PLA loaded and heating.
  const oneHead = {
    ...healthy,
    model: 'MK4S',
    tools: {
      tool_count: 1,
      tools: {
        '0': {
          type: 'FFF_PRINT_HEAD',
          active: true,
          features: {
            nozzle_diameter: 0.4,
            nozzle_high_flow: false,
            nozzle_hardened: true,
          },
          feeder: null,
          material_package_instance: packageOf('PLA'),
          nozzle_temp: 214.6,
          nozzle_target_temp: 215,
        },
      },
    },
  };

  const fiveHeads = {
    ...healthy,
    model: 'XL',
    build_volume: { x_mm: 360, y_mm: 360, z_mm: 360 },
    tools: {
      tool_count: 5,
      tools: {
        '0': {
          type: 'FFF_PRINT_HEAD',
          active: false,
          features: {
            nozzle_diameter: 0.4,
            nozzle_high_flow: false,
            nozzle_hardened: false,
          },
          material_package_instance: packageOf('PLA'),
          nozzle_temp: 170,
        },
        '1': {
          type: 'FFF_PRINT_HEAD',
          active: true,
          features: {
            nozzle_diameter: 0.6,
            nozzle_high_flow: true,
            nozzle_hardened: true,
          },
          material_package_instance: packageOf('PETG'),
          nozzle_temp: 250,
        },
        '2': {
          type: 'FFF_PRINT_HEAD',
          active: false,
          features: {
            nozzle_diameter: 0.4,
            nozzle_high_flow: false,
            nozzle_hardened: false,
          },
          material_package_instance: null,
          nozzle_temp: 24,
        },
        '3': null,
        '4': { type: 'FFF_PRINT_HEAD', active: false },
      },
    },
  };

  const troubled = {
    ...healthy,
    connected: false,
    issues: [
      {
        detail: 'The board answered a frame this build has no reading of.',
        reason: 'unknown_frame',
        severity: 'error' as const,
      },
      {
        detail: 'The filament sensor saw nothing at the extruder.',
        reason: 'filament_missing',
        severity: 'warning' as const,
      },
    ],
    profile_matched: false,
  };

  const loaded: PrinterHistorySource = {
    jobs: {
      current: {
        display_name: 'bracket-v4.bgcode',
        id: '1bfb4ee3-d6f2-49d2-83d6-bc23b64a5a08',
        device_id: 'printer-1',
        device_job_id: '42',
        path: '/usb/bracket-v4.bgcode',
        progress: 37,
        started: '2026-09-17T09:14:05Z',
        state: 'PRINTING',
        time_remaining_ms: 2160000,
      },
      last: {
        display_name: 'benchy.bgcode',
        ended: '2026-09-16T11:20:00Z',
        id: '3fe9fea7-d1bc-4cd7-8eca-e8b8db1aa7a3',
        device_id: 'printer-1',
        device_job_id: '41',
        path: '/usb/benchy.bgcode',
        progress: 100,
        started: '2026-09-16T10:20:00Z',
        state: 'FINISHED',
        time_remaining_ms: 0,
      },
    },
    jobsError: undefined,
    jobsLoaded: true,
  };

  const neverPrinted: PrinterHistorySource = {
    jobs: {},
    jobsError: undefined,
    jobsLoaded: true,
  };

  const reading: PrinterHistorySource = {
    jobs: undefined,
    jobsError: undefined,
    jobsLoaded: false,
  };

  const recent: readonly PrintRow[] = [
    {
      display_name: 'benchy.bgcode',
      ended: '2026-09-16T11:20:00Z',
      id: '3fe9fea7-d1bc-4cd7-8eca-e8b8db1aa7a3',
      device_id: 'printer-1',
      device_job_id: '41',
      path: '/usb/benchy.bgcode',
      progress: 100,
      started: '2026-09-16T10:20:00Z',
      state: 'FINISHED',
      time_remaining_ms: 0,
    },
    {
      display_name: 'enclosure-panel.bgcode',
      ended: '2026-09-15T18:02:00Z',
      id: '658dadab-fba5-4901-8383-7f08727760bb',
      device_id: 'printer-1',
      device_job_id: '40',
      path: '/usb/enclosure-panel.bgcode',
      progress: 41,
      started: '2026-09-15T16:31:00Z',
      state: 'STOPPED',
      time_remaining_ms: 4_260_000,
    },
    {
      display_name: 'spool-holder.bgcode',
      ended: '2026-09-15T09:48:00Z',
      id: 'f4730659-f514-4b81-8e7b-f17c8607fee9',
      device_id: 'printer-1',
      device_job_id: '39',
      path: '/usb/spool-holder.bgcode',
      progress: 100,
      started: '2026-09-15T08:05:00Z',
      state: 'FINISHED',
      time_remaining_ms: 0,
    },
  ];

  // A store that takes every write at once.
  const administration: PrinterAdministration = {
    pending: () => false,
    error: undefined,
    failed: undefined,
    tags: (record) => record,
    editTags: () => Promise.resolve(true),
    rename: () => Promise.resolve(true),
    retire: () => Promise.resolve(true),
  };

  // A host that can change tab.
  const onPickCommand = () => undefined;

  const refused: PrinterHistorySource = {
    jobs: undefined,
    jobsError: Object.assign(
      new Error('no job provider is configured for this device'),
      { code: 'core.jobs/unavailable' },
    ),
    jobsLoaded: false,
  };
</script>

<!-- A printer with nothing wrong. -->
<Story name="Healthy" asChild>
  <PrinterInfoPanel
    device={healthy}
    history={loaded}
    {recent}
    jobsHref="#/jobs/printer/printer-1"
    {administration}
    {onPickCommand}
  />
</Story>

<!-- The printer this server runs on: an administrator renames and tags it, and
     the Danger zone says why it offers no removal. -->
<Story name="Runs This Server" asChild>
  <PrinterInfoPanel
    device={{ ...healthy, resident: true }}
    history={loaded}
    {recent}
    {administration}
  />
</Story>

<!-- A reader who may look and not touch: no tag controls, no Danger zone. -->
<Story name="Read Only" asChild>
  <PrinterInfoPanel device={healthy} history={loaded} {recent} />
</Story>

<!-- A host that reads only the jobs operation. -->
<Story name="Prints Not Paged" asChild>
  <PrinterInfoPanel device={healthy} history={loaded} {administration} />
</Story>

<!-- One nozzle, head 0 of the tool tree. -->
<Story name="One Head" asChild>
  <PrinterInfoPanel device={oneHead} history={loaded} />
</Story>

<!-- Five heads, one switched off and one that has not said what it carries. -->
<Story name="Five Heads" asChild>
  <PrinterInfoPanel device={fiveHeads} history={loaded} />
</Story>

<!-- The Tools card of a printer that reports no tools. -->
<Story name="Tools Not Reported" asChild>
  <PrinterInfoPanel device={toolsUnknown} history={loaded} />
</Story>

<!-- Two issues, a session that has closed and a model no profile covered. -->
<Story name="With Issues" asChild>
  <PrinterInfoPanel device={troubled} history={loaded} />
</Story>

<Story name="No Tags" asChild>
  <PrinterInfoPanel
    device={{ ...healthy, tags: [] }}
    history={loaded}
    {administration}
  />
</Story>

<!-- A machine straight out of the box. -->
<Story name="Never Printed" asChild>
  <PrinterInfoPanel device={healthy} history={neverPrinted} />
</Story>

<!-- A driver that counts no frames leaves the connection block absent. -->
<Story name="Link Not Counted" asChild>
  <PrinterInfoPanel
    device={{ ...healthy, link: { ...healthy.link, counted: false } }}
    history={loaded}
  />
</Story>

<!-- The server refused the prints: the registry code and its own line. -->
<Story name="History Refused" asChild>
  <PrinterInfoPanel device={healthy} history={refused} />
</Story>

<!-- Nothing has arrived yet. Every part waits in its own shape. -->
<Story name="Loading" asChild>
  <PrinterInfoPanel device={undefined} history={reading} />
</Story>
