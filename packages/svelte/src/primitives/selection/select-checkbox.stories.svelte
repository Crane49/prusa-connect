<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import SelectionProbe from '../../test/selection-probe.svelte';
  import SelectCheckbox from './select-checkbox.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Select Checkbox',
    component: SelectCheckbox,
  });
</script>

<!-- Press one box, then shift and press another: the rows between tick too. -->
<Story name="Default" asChild>
  <SelectionProbe />
</Story>

<Story name="Some Ticked" asChild>
  <SelectionProbe ticked={['cube.gcode', 'vase.gcode']} />
</Story>
