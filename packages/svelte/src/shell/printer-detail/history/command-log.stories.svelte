<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import type { CommandRecord } from '../../../data';
  import CommandLog from './command-log.svelte';
  import type { CommandLogSource } from './types';

  const { Story } = defineMeta({
    title: 'Parts/Command Log',
    component: CommandLog,
  });

  const ROWS: readonly CommandRecord[] = [
    {
      command_id: 'c3',
      verb: 'print.start',
      origin: 'user:alice',
      ok: true,
      status: 'completed',
      elapsed_ms: 840,
      requested_at: '2026-09-23T10:00:00.000Z',
      settled_at: '2026-09-23T10:00:01.000Z',
    },
    {
      command_id: 'c2',
      verb: 'print.pause',
      origin: 'user:bob',
      ok: false,
      status: 'failed',
      fault_code: 'core.command/REFUSED',
      elapsed_ms: 120,
      requested_at: '2026-09-23T09:40:00.000Z',
    },
    {
      command_id: 'c1',
      verb: 'print.stop',
      origin: 'module:fleet-stats',
      ok: false,
      outcome_unknown: true,
      elapsed_ms: 30000,
      requested_at: '2026-09-23T09:00:00.000Z',
    },
  ];

  const sourceOf = (
    over: Partial<CommandLogSource> = {},
  ): CommandLogSource => ({
    rows: ROWS,
    loading: false,
    loaded: true,
    error: undefined,
    more: false,
    record: () => undefined,
    recordError: () => undefined,
    load: () => Promise.resolve(),
    loadMore: () => Promise.resolve(),
    open: () => Promise.resolve(),
    abort: () => undefined,
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

<!--
  The lifecycle a row draws when opened comes from `record(id)`, which these
  fakes leave empty; opening a row shows the state before that read answers.
-->

<Story name="Stored Commands" asChild>
  <CommandLog source={sourceOf()} />
</Story>

<Story name="Older Page Waits" asChild>
  <CommandLog source={sourceOf({ more: true })} />
</Story>

<Story name="Nothing Stored" asChild>
  <CommandLog source={sourceOf({ rows: [] })} />
</Story>

<Story name="Still Reading" asChild>
  <CommandLog source={sourceOf({ rows: [], loaded: false, loading: true })} />
</Story>

<Story name="No History Store" asChild>
  <CommandLog
    source={sourceOf({
      rows: [],
      error: {
        code: 'core.resource/HISTORY_NOT_CONFIGURED',
        status: 501,
        params: {},
        message: 'no history store is declared',
        traceId: undefined,
      },
    })}
  />
</Story>

<Story name="Failed Read Keeps The Rows" asChild>
  <CommandLog source={sourceOf({ more: true, error: FAULT })} />
</Story>

<Story name="Refused" asChild>
  <CommandLog source={sourceOf({ rows: [], error: FAULT })} />
</Story>
