<!--
  One object in 3D, in the colours of its filaments. Omitted No WebGL: the
  panel shows the object from above instead, which its own stories show.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { ringMesh, twoPartMesh } from '../test/project-meshes';
  import ProjectObject3d from './project-object-3d.svelte';

  const { Story } = defineMeta({
    title: '3MF/Project object 3D',
    component: ProjectObject3d,
  });

  const filaments = [
    { type: 'PLA', colour: '#E25C15' },
    { type: 'PETG', colour: '#2C7BE5' },
  ];
</script>

<Story name="One Part" asChild>
  <div class="size-80 bg-muted/40 p-2">
    <ProjectObject3d mesh={ringMesh} {filaments} label="A ring in 3D" />
  </div>
</Story>

<Story name="Two Parts In Two Colours" asChild>
  <div class="size-80 bg-muted/40 p-2">
    <ProjectObject3d mesh={twoPartMesh} {filaments} label="Two rings in 3D" />
  </div>
</Story>

<Story name="No Colour Named" asChild>
  <div class="size-80 bg-muted/40 p-2">
    <ProjectObject3d mesh={ringMesh} filaments={[]} label="A ring in 3D" />
  </div>
</Story>

<Story name="Black Filament" asChild>
  <div class="size-80 bg-muted/40 p-2">
    <ProjectObject3d
      mesh={ringMesh}
      filaments={[{ type: 'PLA', colour: '#000000' }]}
      label="A black ring in 3D"
    />
  </div>
</Story>
