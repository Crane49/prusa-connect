<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import MemoryEstimate from './memory-estimate.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Extensions/Memory Estimate',
    component: MemoryEstimate,
  });
</script>

<Story name="Default Source" asChild>
  <MemoryEstimate
    estimate={{
      mib: 128,
      source: 'default',
      overPool: false,
      committedMiB: 640,
      poolMiB: 3072,
    }}
  />
</Story>

<Story name="Author Source" asChild>
  <MemoryEstimate
    estimate={{
      mib: 512,
      source: 'author',
      overPool: false,
      committedMiB: 1024,
      poolMiB: 3072,
    }}
  />
</Story>

<Story name="Operator Source" asChild>
  <MemoryEstimate
    estimate={{
      mib: 256,
      source: 'operator',
      overPool: false,
      committedMiB: 1024,
      poolMiB: 3072,
    }}
  />
</Story>

<Story name="Over Pool" asChild>
  <MemoryEstimate
    estimate={{
      mib: 2560,
      source: 'author',
      overPool: true,
      committedMiB: 3584,
      poolMiB: 3072,
    }}
  />
</Story>

<Story name="Pool Unknown" asChild>
  <MemoryEstimate
    estimate={{
      mib: 128,
      source: 'default',
      overPool: false,
      committedMiB: 128,
    }}
  />
</Story>
