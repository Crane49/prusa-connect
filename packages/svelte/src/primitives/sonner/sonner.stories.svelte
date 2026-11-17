<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';
  import { toast } from 'svelte-sonner';

  import Button from '../button/button.svelte';
  import Toaster from './sonner.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Sonner',
    component: Toaster,
  });
</script>

<!-- A toaster draws nothing until a page calls `toast(...)`, so every story
     comes with the buttons that call it. -->
{#snippet launcher()}
  <Toaster />
  <div class="flex flex-wrap gap-3">
    <Button onclick={() => toast('Print queued')}>Default</Button>
    <Button variant="secondary" onclick={() => toast.success('Print finished')}>
      Success
    </Button>
    <Button variant="destructive" onclick={() => toast.error('Print failed')}>
      Error
    </Button>
    <Button variant="ghost" onclick={() => toast.warning('Bed cooling')}>
      Warning
    </Button>
    <Button variant="ghost" onclick={() => toast.info('Filament low')}>
      Info
    </Button>
  </div>
{/snippet}

<Story name="Default" asChild children={launcher} />

<Story name="All Kinds" asChild children={launcher} />

<Story
  name="Firing A Toast Announces It And It Can Be Dismissed"
  tags={['!dev', '!autodocs']}
  asChild
>
  <Toaster closeButton />
  <Button onclick={() => toast.success('Nozzle calibrated')}>Notify</Button>
</Story>
