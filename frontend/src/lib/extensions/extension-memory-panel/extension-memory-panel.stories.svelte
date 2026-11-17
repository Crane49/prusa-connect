<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { instanceOf } from '../../../../test/extension-fixtures';
  import ExtensionMemoryPanel from './extension-memory-panel.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Extensions/Extension Memory Panel',
    component: ExtensionMemoryPanel,
  });

  const MIB = 1_048_576;
  const instance = instanceOf({ name: 'spools' });
  const memory = {
    budget_bytes: 128 * MIB,
    budget_source: 'default',
    held_until_restart: false,
    near_budget: false,
    on_over_memory: 'stop',
    over_budget: false,
    failures_in_window: 0,
    switch_off_failures: 3,
    switch_off_window_seconds: 3600,
    switch_off_source: 'default',
    used_bytes: 96 * MIB,
  } as const;
  const minutesAgo = (minutes: number): string =>
    new Date(Date.now() - minutes * 60_000).toISOString();
  const history = {
    items: Array.from({ length: 30 }, (_, at) => ({
      at: minutesAgo(60 - at * 2),
      value: (60 + ((at * 7) % 40)) * MIB,
    })),
    stops: [],
  };
  const write = (): Promise<boolean> => Promise.resolve(true);
</script>

<Story name="Last Hour" asChild>
  <ExtensionMemoryPanel
    {instance}
    {memory}
    pool={undefined}
    saving={false}
    conflict={false}
    missing={false}
    {history}
    {write}
  />
</Story>

<Story name="Loading" asChild>
  <ExtensionMemoryPanel
    {instance}
    {memory}
    pool={undefined}
    saving={false}
    conflict={false}
    missing={false}
    {write}
  />
</Story>
