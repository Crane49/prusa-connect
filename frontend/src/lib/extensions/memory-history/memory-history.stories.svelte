<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { ApiError } from '@prusa/connect';

  import MemoryHistory from './memory-history.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Extensions/Memory History',
    component: MemoryHistory,
  });

  const minutesAgo = (minutes: number): string =>
    new Date(Date.now() - minutes * 60_000).toISOString();
  const MIB = 1_048_576;

  const memory = {
    budget_bytes: 128 * MIB,
    budget_source: 'default',
    held_until_restart: false,
    near_budget: false,
    on_over_memory: 'stop',
    over_budget: false,
    failures_in_window: 1,
    switch_off_failures: 3,
    switch_off_window_seconds: 3600,
    switch_off_source: 'default',
  } as const;

  const history = {
    items: Array.from({ length: 30 }, (_, at) => ({
      at: minutesAgo(60 - at * 2),
      ...(at === 12 ? {} : { value: (60 + ((at * 7) % 40)) * MIB }),
    })),
    stops: [
      {
        at: minutesAgo(20),
        result: 'over-memory',
        used_bytes: 140 * MIB,
        budget_bytes: 128 * MIB,
      },
    ],
  };
</script>

<Story name="Last Hour" asChild>
  <MemoryHistory {history} {memory} />
</Story>

<Story name="Short Switch Off Window" asChild>
  <MemoryHistory
    {history}
    memory={{ ...memory, switch_off_window_seconds: 900 }}
  />
</Story>

<Story name="Switched Off" asChild>
  <MemoryHistory
    {history}
    memory={{
      ...memory,
      held_until_restart: true,
      switched_off: { at: minutesAgo(20), stops: history.stops },
    }}
  />
</Story>

<Story name="Loading" asChild>
  <MemoryHistory {memory} />
</Story>

<Story name="Failed" asChild>
  <MemoryHistory
    {memory}
    fault={new ApiError(500, {
      type: 'about:blank',
      status: 500,
      retryable: true,
      code: 'core.internal/FAILED',
      title: 'Failed',
      detail: 'The server could not read it.',
      params: {},
      trace_id: 'trace-1',
    })}
  />
</Story>

<Story name="No Readings" asChild>
  <MemoryHistory history={{ items: [], stops: [] }} {memory} />
</Story>

<Story name="Gone" asChild>
  <MemoryHistory {memory} missing />
</Story>
