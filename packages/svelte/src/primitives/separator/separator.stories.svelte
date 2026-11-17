<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import Separator from './separator.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Separator',
    component: Separator,
  });
</script>

<Story name="Default" asChild>
  <div class="w-64">
    <div class="font-mono text-sm tracking-wider uppercase">MK4S</div>
    <Separator class="my-2" />
    <div class="text-sm text-muted-foreground">Ready to print</div>
  </div>
</Story>

<Story name="Vertical" asChild>
  <div class="flex h-8 items-center gap-3 text-sm">
    <span>Printers</span>
    <Separator orientation="vertical" />
    <span>Jobs</span>
    <Separator orientation="vertical" />
    <span>Files</span>
  </div>
</Story>
