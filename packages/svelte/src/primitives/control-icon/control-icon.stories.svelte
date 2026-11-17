<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import HeatbedIcon from '../telemetry-icon/heatbed-icon.svelte';
  import NozzleIcon from '../telemetry-icon/nozzle-icon.svelte';
  import SpoolIcon from '../telemetry-icon/spool-icon.svelte';
  import ArrowDownIcon from './arrow-down-icon.svelte';
  import ArrowLeftIcon from './arrow-left-icon.svelte';
  import ArrowRightIcon from './arrow-right-icon.svelte';
  import ArrowUpIcon from './arrow-up-icon.svelte';
  import ChamberIcon from './chamber-icon.svelte';
  import DisableSteppersIcon from './disable-steppers-icon.svelte';
  import HomeIcon from './home-icon.svelte';
  import MeshBedLevelingIcon from './mesh-bed-leveling-icon.svelte';
  import RestartIcon from './restart-icon.svelte';
  import SpeedIcon from './speed-icon.svelte';
  import TemperatureIcon from './temperature-icon.svelte';
  import ZAxisIcon from './z-axis-icon.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Control Icons',
  });

  // The order the Machine tab reads in: the telemetry a printer reports, then
  // the sections it is commanded through, then the jog pad.
  const glyphs = [
    { name: 'Nozzle', Icon: NozzleIcon },
    { name: 'Heatbed', Icon: HeatbedIcon },
    { name: 'Spool', Icon: SpoolIcon },
    { name: 'Temperature', Icon: TemperatureIcon },
    { name: 'Chamber', Icon: ChamberIcon },
    { name: 'Speed', Icon: SpeedIcon },
    { name: 'Z axis', Icon: ZAxisIcon },
    { name: 'Home', Icon: HomeIcon },
    { name: 'Disable steppers', Icon: DisableSteppersIcon },
    { name: 'Restart', Icon: RestartIcon },
    { name: 'Mesh bed leveling', Icon: MeshBedLevelingIcon },
    { name: 'Arrow up', Icon: ArrowUpIcon },
    { name: 'Arrow down', Icon: ArrowDownIcon },
    { name: 'Arrow left', Icon: ArrowLeftIcon },
    { name: 'Arrow right', Icon: ArrowRightIcon },
  ];
</script>

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

<!-- Tinted by its container, never by itself: this is what lets a glyph sit on
     a destructive button and a muted heading alike. -->
<Story
  name="Glyphs Take Their Containers Colour"
  tags={['!dev', '!autodocs']}
  asChild
>
  <div class="flex gap-4 text-destructive-text">
    <HomeIcon aria-hidden="true" class="size-8" />
  </div>
</Story>
