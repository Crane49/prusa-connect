<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import CodeBlock from './code-block.svelte';

  const printerObject = {
    id: 'p-01',
    uuid: 'a1b2c3d4-5e6f-4a1b-9c3d-e5f6a7b8f9e0',
    name: 'MK4S, Print Farm Rack 3, Shelf B (Office Wing)',
    model: 'MK4S',
    firmwareVersion: '5.2.1',
    state: 'ATTENTION',
    connection: {
      status: 'connected',
      protocolVersion: '2.4',
      transport: 'wifi',
      lastSeenAt: '2026-08-19T09:41:12Z',
    },
    network: {
      hostname: 'mk4s-rack3-shelfb.local',
      ipAddress: '192.168.1.42',
      macAddress: 'A4:CF:12:9B:3D:07',
      wifiSsid: 'PrintFarm-5G',
      signalStrengthDbm: -52,
    },
    job: {
      name: 'Articulated_Dragon_Body_Support_Structure_Reinforced_v12_FINAL.gcode',
      path: '/usb/projects/dragon/Articulated_Dragon_Body_Support_Structure_Reinforced_v12_FINAL.gcode',
      progressPercent: 73,
      estimatedTimeRemainingSeconds: 1860,
      printStartedAt: '2026-08-19T07:02:41Z',
      layerCurrent: 604,
      layerTotal: 812,
    },
    telemetry: {
      nozzleTemperatureCelsius: 235,
      nozzleTargetTemperatureCelsius: 235,
      bedTemperatureCelsius: 60,
      bedTargetTemperatureCelsius: 60,
      printSpeedPercent: 100,
      flowRatePercent: 100,
      fanSpeedPercent: 100,
      axisPositionMm: { x: 124.5, y: 98.2, z: 182.64 },
    },
    material: {
      type: 'ABS',
      vendor: 'Example Filaments',
      colorName: 'Signal White',
      spoolWeightRemainingGrams: 214,
    },
    storage: { usbPresent: true, sdCardPresent: false },
    capabilities: [
      'UPLOAD',
      'START_PRINT',
      'PAUSE_PRINT',
      'STOP_PRINT',
      'SET_TEMPERATURE',
      'SET_TOOL_MAPPING',
    ],
    lastErrorMessage:
      'Filament runout detected on tool 2 while printing a support structure that spans multiple materials; the print was paused automatically and the machine is waiting for a filament swap before it can resume the current layer.',
  };

  const printerObjectJson = JSON.stringify(printerObject, null, 2);

  const { Story } = defineMeta({
    title: 'Primitives/CodeBlock',
    component: CodeBlock,
  });
</script>

<Story name="Default" asChild>
  <div class="w-full max-w-md">
    <CodeBlock label="Printer object" value={printerObjectJson} />
  </div>
</Story>

<Story name="No Label" asChild>
  <div class="w-full max-w-md">
    <CodeBlock value={printerObjectJson} />
  </div>
</Story>

<Story name="Empty" args={{ label: 'Printer object', value: '' }} />

<Story
  name="Copy Succeeds And Is Announced"
  tags={['!dev', '!autodocs']}
  args={{ label: 'Printer object', value: '{"id":"p-01"}' }}
/>

<Story
  name="Copy Failure Is Surfaced"
  tags={['!dev', '!autodocs']}
  args={{ label: 'Printer object', value: '{"id":"p-01"}' }}
/>
