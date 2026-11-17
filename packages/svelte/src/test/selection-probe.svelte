<!--
  A listing of tick boxes over one Selection, for the tests and the stories.
-->
<script lang="ts">
  import SelectAllCheckbox from '../primitives/selection/select-all-checkbox.svelte';
  import SelectCheckbox from '../primitives/selection/select-checkbox.svelte';
  import { Selection } from '../primitives/selection/selection.svelte';

  let {
    rows = ['benchy.gcode', 'cube.gcode', 'vase.gcode', 'hook.gcode'],
    ticked = [],
  }: { rows?: readonly string[]; ticked?: readonly string[] } = $props();

  const selection = new Selection<string>({ rows: () => rows });
  // svelte-ignore state_referenced_locally
  selection.set(ticked, true);
</script>

<div class="flex flex-col gap-2">
  <div class="flex items-center gap-2 text-sm font-medium">
    <SelectAllCheckbox {selection} keys={rows} label="Select every file" />
    {selection.count} of {selection.total} selected
  </div>
  {#each rows as row (row)}
    <div class="flex items-center gap-2 text-sm">
      <SelectCheckbox {selection} key={row} label={`Select ${row}`} />
      {row}
    </div>
  {/each}
</div>
