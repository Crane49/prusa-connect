<!-- The changes a slicer inserts at given heights of one bed. -->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import {
    colourChanges,
    customLayers,
  } from '../test/project-fixtures-multimaterial';
  import ProjectLayerChanges from './project-layer-changes.svelte';

  const { Story } = defineMeta({
    title: '3MF/Project layer changes',
    component: ProjectLayerChanges,
  });

  const filaments = [
    { type: 'PLA', colour: '#FF8000' },
    { type: 'PETG', colour: '#DB5182' },
  ];

  const mixed = [
    { height_mm: 0.6, kind: 'tool_change' as const, extruder: 2 },
    {
      height_mm: 4.2,
      kind: 'pause' as const,
      extruder: 1,
      extra: 'Insert the magnets',
    },
    { height_mm: 8, kind: 'template' as const, extruder: 1 },
  ];
</script>

<Story name="Filament Changes" asChild>
  <ProjectLayerChanges
    changes={colourChanges.setups[0]?.beds[0]?.layer_changes ?? []}
    filaments={colourChanges.setups[0]?.settings.filaments ?? []}
  />
</Story>

<Story name="Custom G-code" asChild>
  <ProjectLayerChanges
    changes={customLayers.setups[0]?.beds[0]?.layer_changes ?? []}
    filaments={customLayers.setups[0]?.settings.filaments ?? []}
  />
</Story>

<Story name="Tool Change, Pause And Template" asChild>
  <ProjectLayerChanges changes={mixed} {filaments} />
</Story>

<!-- Omitted None: a bed with no changes draws nothing. -->
