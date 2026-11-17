<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import Button from '../button/button.svelte';
  import PopoverContent from './popover-content.svelte';
  import PopoverDescription from './popover-description.svelte';
  import PopoverHeader from './popover-header.svelte';
  import PopoverTitle from './popover-title.svelte';
  import PopoverTrigger from './popover-trigger.svelte';
  import Popover from './popover.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Popover',
    component: Popover,
  });
</script>

{#snippet printerOptions()}
  <Popover>
    <PopoverTrigger>
      {#snippet child({ props })}
        <Button {...props} variant="secondary">Printer options</Button>
      {/snippet}
    </PopoverTrigger>
    <PopoverContent>
      <PopoverHeader>
        <PopoverTitle>Printer options</PopoverTitle>
        <PopoverDescription>
          Settings apply to this printer only.
        </PopoverDescription>
      </PopoverHeader>
      <Button size="sm" class="mt-3">Open settings</Button>
    </PopoverContent>
  </Popover>
{/snippet}

<Story name="Default" asChild children={printerOptions} />

<Story name="Open State" asChild>
  <Popover open>
    <PopoverTrigger>
      {#snippet child({ props })}
        <Button {...props} variant="secondary">Printer options</Button>
      {/snippet}
    </PopoverTrigger>
    <PopoverContent>
      <PopoverHeader>
        <PopoverTitle>Printer options</PopoverTitle>
        <PopoverDescription>
          Settings apply to this printer only.
        </PopoverDescription>
      </PopoverHeader>
      <Button size="sm" class="mt-3">Open settings</Button>
    </PopoverContent>
  </Popover>
</Story>

<Story
  name="Opens By Keyboard And Returns Focus On Escape"
  tags={['!dev', '!autodocs']}
  asChild
  children={printerOptions}
/>
