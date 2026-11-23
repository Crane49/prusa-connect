<!--
  "Set as current" on a listed printer and on its page, with PrusaSlicer
  played by a fake. Outside PrusaSlicer it draws nothing, which the last
  story shows.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { fakeSlicer, t } from '../../test/fakes';
  import SetAsCurrent from './set-as-current.svelte';

  const inside = fakeSlicer().slicer;
  const outside = fakeSlicer(false).slicer;

  const { Story } = defineMeta({
    title: 'PrusaSlicer 3/SetAsCurrent',
    component: SetAsCurrent,
    args: { deviceId: 'mk4s-1', t },
  });
</script>

<Story name="On a listed printer" args={{ look: 'row', slicer: inside }} />
<Story name="On the printer page" args={{ look: 'header', slicer: inside }} />
<Story name="Outside PrusaSlicer" args={{ look: 'header', slicer: outside }} />
