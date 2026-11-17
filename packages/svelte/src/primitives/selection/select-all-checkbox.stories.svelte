<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import SelectionProbe from '../../test/selection-probe.svelte';
  import SelectAllCheckbox from './select-all-checkbox.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Select All Checkbox',
    component: SelectAllCheckbox,
  });
</script>

<Story name="None Ticked" asChild>
  <SelectionProbe />
</Story>

<!-- A dash: some of the rows it covers are ticked. -->
<Story name="Some Ticked" asChild>
  <SelectionProbe ticked={['cube.gcode']} />
</Story>

<Story name="All Ticked" asChild>
  <SelectionProbe
    ticked={['benchy.gcode', 'cube.gcode', 'vase.gcode', 'hook.gcode']}
  />
</Story>
