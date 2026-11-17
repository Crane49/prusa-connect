<script module lang="ts">
  // The drawings, each on its own. A screen reaches them through the `art`
  // slot of the component it is drawing an empty state for.
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import Art from './art.svelte';
  import { ART_NAMES } from './types';

  const { Story } = defineMeta({
    title: 'Primitives/Art',
    component: Art,
  });
</script>

<Story
  name="Default"
  args={{ name: 'empty-fleet', class: 'h-40 text-muted-foreground' }}
/>

<Story name="Every drawing" asChild>
  <div class="flex flex-col gap-8">
    {#each ART_NAMES as name (name)}
      <figure class="flex flex-col items-center gap-2">
        <!-- The shelf is a band and takes a width; the rest take a height. -->
        <Art
          {name}
          fit={name === 'camera-grid' ? 'tile' : 'contain'}
          class={[
            'text-muted-foreground',
            name === 'camera-grid' && 'h-32 w-80',
            name === 'shelf' && 'w-60',
            name !== 'camera-grid' && name !== 'shelf' && 'h-32',
          ]}
        />
        <figcaption class="text-caption text-muted-foreground">
          {name}
        </figcaption>
      </figure>
    {/each}
  </div>
</Story>

<Story name="The camera bed" asChild>
  <div class="rounded-panel bg-tone-raised relative h-48 w-80 overflow-hidden">
    <Art
      name="camera-grid"
      decorative
      fit="tile"
      class="absolute inset-0 size-full text-muted-foreground"
    />
    <div
      class="rounded-panel bg-tone-panel absolute inset-x-12 inset-y-8 border border-border"
    ></div>
  </div>
</Story>
