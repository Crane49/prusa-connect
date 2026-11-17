<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import AspectRatio from './aspect-ratio.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/AspectRatio',
    component: AspectRatio,
  });
</script>

<Story name="Default" asChild>
  <div class="w-64">
    <AspectRatio
      ratio={16 / 9}
      class="overflow-hidden rounded-sm border border-border bg-muted"
    >
      <div
        class="flex h-full items-center justify-center text-sm text-muted-foreground"
      >
        Camera preview
      </div>
    </AspectRatio>
  </div>
</Story>

<Story name="Square" asChild>
  <div class="w-40">
    <AspectRatio
      ratio={1}
      class="overflow-hidden rounded-sm border border-border bg-muted"
    >
      <div
        class="flex h-full items-center justify-center text-sm text-muted-foreground"
      >
        Thumbnail
      </div>
    </AspectRatio>
  </div>
</Story>
