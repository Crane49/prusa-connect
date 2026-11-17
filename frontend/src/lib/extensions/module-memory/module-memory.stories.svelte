<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import ModuleMemory from './module-memory.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Extensions/Module Memory',
    component: ModuleMemory,
  });

  const MIB = 1_048_576;
  const memory = (over: Record<string, unknown> = {}) => ({
    budget_bytes: 128 * MIB,
    budget_source: 'default' as const,
    failures_in_window: 0,
    held_until_restart: false,
    near_budget: false,
    on_over_memory: 'stop' as const,
    over_budget: false,
    switch_off_failures: 3,
    switch_off_source: 'default' as const,
    switch_off_window_seconds: 3600,
    ...over,
  });
</script>

<Story name="Within Budget" asChild>
  <ModuleMemory running memory={memory({ used_bytes: 40 * MIB })} />
</Story>

<Story name="Near Budget" asChild>
  <ModuleMemory
    running
    memory={memory({ used_bytes: 115 * MIB, near_budget: true })}
  />
</Story>

<Story name="Over Budget But Only Warns" asChild>
  <ModuleMemory
    running
    memory={memory({
      used_bytes: 200 * MIB,
      over_budget: true,
      on_over_memory: 'warn',
    })}
  />
</Story>

<Story name="Use Unknown" asChild>
  <ModuleMemory running memory={memory()} />
</Story>

<Story name="Failed Lately" asChild>
  <ModuleMemory
    running
    memory={memory({
      used_bytes: 40 * MIB,
      failures_in_window: 2,
      switch_off_window_seconds: 600,
    })}
  />
</Story>

<Story name="Not Running" asChild>
  <ModuleMemory running={false} memory={memory()} />
</Story>
