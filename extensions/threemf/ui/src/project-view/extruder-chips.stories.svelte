<!-- The extruders of a part or an object, by the colour of their filament. -->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import ExtruderChips from './extruder-chips.svelte';

  const { Story } = defineMeta({
    title: '3MF/Extruder chips',
    component: ExtruderChips,
  });

  const filaments = [
    { type: 'PLA', colour: '#FF8000' },
    { type: 'PETG', colour: '#DB5182' },
    { type: 'PLA', colour: '#3EC0FF' },
    { type: 'ASA', colour: '#FF4F4F' },
    { type: 'PLA', colour: '#FBEB7D' },
  ];
</script>

<Story name="Five Extruders" asChild>
  <ExtruderChips extruders={[1, 2, 3, 4, 5]} {filaments} label="Extruders" />
</Story>

<Story name="One Extruder" asChild>
  <ExtruderChips extruders={[2]} {filaments} label="Extruders" />
</Story>

<!-- An extruder past the filaments the file lists has no colour. -->
<Story name="No Colour Known" asChild>
  <ExtruderChips extruders={[1, 7]} {filaments} label="Extruders" />
</Story>
