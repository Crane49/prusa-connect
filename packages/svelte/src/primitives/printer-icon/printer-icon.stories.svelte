<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import PrinterIcon from './printer-icon.svelte';
  import { PRINTER_MODELS } from './types';

  const { Story } = defineMeta({
    title: 'Primitives/PrinterIcon',
    component: PrinterIcon,
    args: { model: 'mk4s' },
  });
</script>

<Story name="Default" args={{ model: 'mk4s', class: 'size-14' }} />

<Story name="All Models" asChild>
  <div class="grid grid-cols-4 gap-6">
    {#each PRINTER_MODELS as model (model)}
      <div class="flex flex-col items-center gap-2">
        <PrinterIcon {model} class="size-14" />
        <span class="font-mono text-xs text-muted-foreground">{model}</span>
      </div>
    {/each}
  </div>
</Story>

<Story
  name="Every Model Has A Readable Name"
  tags={['!dev', '!autodocs']}
  asChild
>
  <div class="flex flex-wrap gap-4">
    {#each PRINTER_MODELS as model (model)}
      <PrinterIcon {model} class="size-10" />
    {/each}
  </div>
</Story>
