<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import type { TransferRow } from '../../../data';
  import TransferLog from './transfer-log.svelte';
  import type { TransferLogSource } from './types';

  const { Story } = defineMeta({
    title: 'Parts/Transfer Log',
    component: TransferLog,
  });

  const ROWS: readonly TransferRow[] = [
    {
      transferId: 't4',
      storage: 'local',
      source: 'benchy.gcode',
      path: 'usb/benchy.gcode',
      bytesTotal: 31_457_346,
      bytesSent: 12_000_000,
      state: 'sending',
      reason: undefined,
      startedAt: '2026-09-23T10:30:00.000Z',
    },
    {
      transferId: 't3',
      storage: 'local',
      source: 'bracket.gcode',
      path: 'usb/bracket.gcode',
      bytesTotal: 31_457_346,
      bytesSent: 50_242,
      state: 'cancelled',
      reason: 'the caller stopped waiting',
      startedAt: '2026-09-23T10:00:00.000Z',
      endedAt: '2026-09-23T10:00:02.000Z',
    },
    {
      transferId: 't2',
      storage: 'local',
      source: 'hook.gcode',
      path: 'usb/hook.gcode',
      bytesTotal: 2_400_000,
      bytesSent: 800_000,
      state: 'failed',
      reason: 'the printer closed the connection',
      startedAt: '2026-09-23T09:40:00.000Z',
      endedAt: '2026-09-23T09:40:05.000Z',
    },
    {
      transferId: 't1',
      storage: 'local',
      source: 'clip.gcode',
      path: 'usb/clip.gcode',
      bytesTotal: 1_200_000,
      bytesSent: 1_200_000,
      state: 'done',
      reason: undefined,
      startedAt: '2026-09-23T09:00:00.000Z',
      endedAt: '2026-09-23T09:00:04.000Z',
    },
  ];

  const sourceOf = (
    over: Partial<TransferLogSource> = {},
  ): TransferLogSource => ({
    rows: ROWS,
    loaded: true,
    error: undefined,
    ...over,
  });

  const FAULT = {
    code: 'core.transport/NETWORK',
    status: undefined,
    params: {},
    message: 'the server could not be reached',
    traceId: undefined,
  };
</script>

<Story name="Sent Files" asChild>
  <TransferLog source={sourceOf()} />
</Story>

<Story name="Nothing Sent" asChild>
  <TransferLog source={sourceOf({ rows: [] })} />
</Story>

<Story name="Still Reading" asChild>
  <TransferLog source={sourceOf({ rows: [], loaded: false })} />
</Story>

<Story name="Failed Read Keeps The Rows" asChild>
  <TransferLog source={sourceOf({ error: FAULT })} />
</Story>

<Story name="Refused" asChild>
  <TransferLog source={sourceOf({ rows: [], loaded: false, error: FAULT })} />
</Story>
