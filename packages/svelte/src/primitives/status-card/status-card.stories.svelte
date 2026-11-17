<script module lang="ts">
  import { PrinterGlyphIcon } from '../printer-glyph';
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import Button from '../button/button.svelte';
  import StatusCard from './status-card.svelte';
  import type { StatusTone } from './types';

  const { Story } = defineMeta({
    title: 'Primitives/StatusCard',
    component: StatusCard,
    args: { title: 'Bench one' },
  });

  const tones: StatusTone[] = [
    'active',
    'done',
    'idle',
    'attention',
    'fault',
    'quiet',
  ];
</script>

<Story name="Default" args={{ value: 'Idle', caption: 'MK4S' }} />

<Story name="Tones" asChild>
  <div class="grid gap-4 @md:grid-cols-3">
    {#each tones as tone (tone)}
      <StatusCard title="Bench one" {tone} toneLabel={tone} value="52 %" />
    {/each}
  </div>
</Story>

<Story name="Edge" asChild>
  <div class="grid gap-4 @md:grid-cols-3">
    {#each tones as tone (tone)}
      <StatusCard title="Printing" {tone} toneLabel={tone} value="3" edge />
    {/each}
  </div>
</Story>

<Story
  name="With a trend"
  args={{
    value: '18 prints',
    caption: 'This week',
    trend: 'up',
    trendLabel: '+4',
    timestamp: 'Updated a minute ago',
  }}
/>

<Story name="With an icon and an action" asChild>
  <StatusCard
    title="Bench one"
    tone="active"
    value="52 %"
    caption="tower.gcode"
  >
    {#snippet icon()}
      <PrinterGlyphIcon aria-hidden="true" class="size-5" />
    {/snippet}
    {#snippet action()}
      <Button size="sm" variant="secondary">Open</Button>
    {/snippet}
  </StatusCard>
</Story>

<Story name="Edge, fluid figure" asChild>
  <div class="grid h-40 w-64 grid-cols-2 gap-2">
    <div class="size-full">
      <StatusCard
        title="Printing"
        tone="active"
        value="12"
        figure="fluid"
        edge
      />
    </div>
    <div class="size-full">
      <StatusCard title="Idle" tone="quiet" value="0" figure="fluid" edge />
    </div>
  </div>
</Story>

<Story name="Heading level" asChild>
  <StatusCard title="Printing" tone="active" value="3" headingLevel="h3" />
</Story>
