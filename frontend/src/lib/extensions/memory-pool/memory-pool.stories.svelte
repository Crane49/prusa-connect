<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import MemoryPool from './memory-pool.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Extensions/Memory Pool',
    component: MemoryPool,
  });

  const MIB = 1_048_576;
  const budgets = [
    { key: 'a', title: 'Spools', bytes: 128 * MIB },
    { key: 'b', title: 'Nozzle watch', bytes: 256 * MIB },
  ];
  const docs =
    'https://docs.example.com/config/extensions/#key-core-modules-memory';
</script>

<Story name="Room Left" asChild>
  <MemoryPool
    pool={{
      committed_bytes: 384 * MIB,
      pool_bytes: 1024 * MIB,
      source: 'detected',
    }}
    {budgets}
    configHref={docs}
  />
</Story>

<Story name="Many Modules" asChild>
  <MemoryPool
    pool={{
      committed_bytes: 1280 * MIB,
      pool_bytes: 2048 * MIB,
      source: 'detected',
    }}
    budgets={[
      { key: 'a', title: 'Spools', bytes: 128 * MIB },
      { key: 'b', title: 'Nozzle watch', bytes: 256 * MIB },
      { key: 'c', title: 'Slicer', bytes: 512 * MIB },
      { key: 'd', title: 'Fleet stats', bytes: 128 * MIB },
      { key: 'e', title: 'Queue board', bytes: 128 * MIB },
      { key: 'f', title: 'Notes', bytes: 128 * MIB },
    ]}
    configHref={docs}
  />
</Story>

<Story name="Hidden Modules" asChild>
  <MemoryPool
    pool={{
      committed_bytes: 640 * MIB,
      pool_bytes: 1024 * MIB,
      source: 'configured',
    }}
    {budgets}
    configHref={docs}
  />
</Story>

<Story name="Over The Pool" asChild>
  <MemoryPool
    pool={{
      committed_bytes: 384 * MIB,
      pool_bytes: 256 * MIB,
      source: 'configured',
    }}
    {budgets}
    configHref={docs}
  />
</Story>

<Story name="Pool Unknown" asChild>
  <MemoryPool
    pool={{ committed_bytes: 384 * MIB }}
    {budgets}
    configHref={docs}
  />
</Story>

<Story name="Not Readable By This Person" asChild>
  <MemoryPool pool={undefined} {budgets} />
</Story>
