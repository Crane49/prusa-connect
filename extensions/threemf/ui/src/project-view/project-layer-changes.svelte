<!--
  The changes a slicer inserts at given heights of one bed, lowest first: a
  new filament, a pause, a switch to another tool, or G-code a person wrote.

  A colour change shows the colour of the new filament, and a tool change
  the tool it switches to. A pause shows its message and custom G-code its
  first line.
-->
<script lang="ts">
  import { t } from '../i18n';
  import { MaterialSwatch } from '@prusa/connect-ui/primitives';
  import ExtruderChips from './extruder-chips.svelte';
  import { millimetres } from './labels';
  import type { ProjectFilament, ProjectLayerChange } from './model';

  interface Props {
    changes: readonly ProjectLayerChange[];
    filaments: readonly ProjectFilament[];
  }

  let { changes, filaments }: Props = $props();

  const KIND_KEY: Readonly<Record<ProjectLayerChange['kind'], string>> = {
    color_change: 'projectView.change.color_change',
    pause: 'projectView.change.pause',
    tool_change: 'projectView.change.tool_change',
    template: 'projectView.change.template',
    custom: 'projectView.change.custom',
  };

  const firstLine = (text: string | undefined): string =>
    (text ?? '').split('\n')[0] ?? '';
</script>

{#if changes.length > 0}
  <div
    class="grid grid-cols-1 content-start gap-1.5"
    data-slot="project-layer-changes"
  >
    <h4 class="text-xs text-muted-foreground">
      {t('projectView.changes.heading')}
    </h4>
    <ol class="grid grid-cols-1 content-start gap-1">
      {#each changes as change, at (at)}
        <li class="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-xs">
          <span class="font-mono tabular-nums">
            {millimetres(change.height_mm)}
          </span>
          <span>{t(KIND_KEY[change.kind])}</span>
          {#if change.kind === 'color_change' && change.color !== undefined}
            <MaterialSwatch
              class="size-3"
              material={{
                name: change.color,
                colorRgba: change.color,
                colorName: change.color,
              }}
            />
          {:else if change.kind === 'tool_change' && change.extruder !== undefined}
            <ExtruderChips
              extruders={[change.extruder]}
              {filaments}
              label={t('projectView.change.tool_change')}
            />
          {/if}
          {#if change.kind === 'pause' || change.kind === 'custom'}
            <span
              class="min-w-0 truncate font-mono text-muted-foreground"
              title={change.extra}
            >
              {firstLine(change.extra)}
            </span>
          {/if}
        </li>
      {/each}
    </ol>
  </div>
{/if}
