<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import ClippedName from './clipped-name.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/ClippedName',
    component: ClippedName,
  });

  const LONG = 'Bracket_Left_Reinforced_v4_FINAL.gcode';
  const SIBLING = 'Bracket_Left_Reinforced_v6_verified.gcode';
</script>

<Story name="Clipped" asChild>
  <div class="w-56 border border-border p-2 font-mono text-sm">
    <ClippedName name={LONG} />
  </div>
</Story>

<Story name="Two Names Stay Distinct" asChild>
  <div
    class="flex w-56 flex-col gap-2 border border-border p-2 font-mono text-sm"
  >
    <ClippedName name={LONG} />
    <ClippedName name={SIBLING} />
  </div>
</Story>

<Story name="Short Name Is Untouched" asChild>
  <div class="w-56 border border-border p-2 font-mono text-sm">
    <ClippedName name="benchy.3mf" />
  </div>
</Story>

<!-- Beyond the React set -->

<Story name="A Longer Tail" asChild>
  <div class="w-56 border border-border p-2 font-mono text-sm">
    <ClippedName name="Enclosure_Panel_Left_rev12_draft.3mf" tailLength={22} />
  </div>
</Story>
