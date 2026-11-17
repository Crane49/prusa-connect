<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import HeatbedIcon from './heatbed-icon.svelte';
  import NozzleIcon from './nozzle-icon.svelte';
  import SpoolIcon from './spool-icon.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Telemetry Icons',
  });

  const glyphs = [
    { name: 'Nozzle', Icon: NozzleIcon },
    { name: 'Heatbed', Icon: HeatbedIcon },
    { name: 'Spool', Icon: SpoolIcon },
  ];
</script>

<!-- Beyond the React set -->

<Story name="Every Glyph" asChild>
  <div class="flex flex-wrap gap-4">
    {#each glyphs as { name, Icon } (name)}
      <div
        class="flex w-32 flex-col items-center gap-2 rounded-sm border border-border p-3 text-center"
      >
        <Icon aria-hidden="true" class="size-8" />
        <span class="text-xs text-muted-foreground">{name}</span>
      </div>
    {/each}
  </div>
</Story>

<!-- A telemetry row labels its reading with one of these, so the glyph has to
     read at the size of the text beside it. -->
<Story name="At Reading Size" asChild>
  <div class="flex items-center gap-2 text-sm text-muted-foreground">
    <NozzleIcon aria-hidden="true" />
    <span>215 / 215 &deg;C</span>
  </div>
</Story>
