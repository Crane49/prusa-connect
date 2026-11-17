<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import MaterialsGlyphIcon from './materials-glyph-icon.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Materials Glyph',
  });
</script>

<Story name="Sizes" asChild>
  <div class="flex items-end gap-4">
    <MaterialsGlyphIcon aria-hidden="true" class="size-3" />
    <MaterialsGlyphIcon aria-hidden="true" class="size-4" />
    <MaterialsGlyphIcon aria-hidden="true" class="size-6" />
    <MaterialsGlyphIcon aria-hidden="true" class="size-8 text-primary" />
  </div>
</Story>

<!-- A navigation entry and a table cell label their text with it, so it has
     to read at the size of the text beside it. -->
<Story name="Beside Text" asChild>
  <div class="flex items-center gap-2 text-sm text-muted-foreground">
    <MaterialsGlyphIcon aria-hidden="true" class="size-4 shrink-0" />
    <span>Materials</span>
  </div>
</Story>
