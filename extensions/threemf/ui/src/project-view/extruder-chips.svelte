<!--
  The extruders a part, an object or a project prints with, each as a chip
  with the colour of its filament and its tool number.

  Colour is never the only carrier: each chip spells out its tool, and its
  title names the material.
-->
<script lang="ts">
  import { t } from '../i18n';
  import { MaterialSwatch } from '@prusa/connect-ui/primitives';
  import type { ProjectFilament } from './model';

  interface Props {
    /** The extruders, counted from 1. */
    extruders: readonly number[];
    /** The filaments of the setup, in extruder order. */
    filaments: readonly ProjectFilament[];
    /** What the list is, for a screen reader. */
    label: string;
  }

  let { extruders, filaments, label }: Props = $props();

  const chips = $derived(
    extruders.map((extruder) => {
      const filament = filaments[extruder - 1];
      const tool = t('projectView.toolNumber', { n: extruder - 1 });

      return {
        extruder,
        tool,
        colour: filament?.colour,
        title:
          filament?.type === undefined || filament.type === ''
            ? tool
            : t('projectView.extruder.title', { tool, type: filament.type }),
      };
    }),
  );
</script>

{#if chips.length > 0}
  <ul
    class="flex flex-wrap items-center gap-1"
    aria-label={label}
    data-slot="extruder-chips"
  >
    {#each chips as chip (chip.extruder)}
      <li
        class="inline-flex items-center gap-1 rounded-xs border border-border px-1.5 py-0.5"
        title={chip.title}
      >
        <MaterialSwatch
          class="size-3"
          material={{
            name: chip.title,
            colorRgba: chip.colour,
            colorName: chip.colour,
          }}
        />
        <span class="font-mono text-xs tabular-nums">{chip.tool}</span>
      </li>
    {/each}
  </ul>
{/if}
