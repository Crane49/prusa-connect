<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import Button from '../button/button.svelte';
  import TooltipContent from './tooltip-content.svelte';
  import TooltipProvider from './tooltip-provider.svelte';
  import TooltipTrigger from './tooltip-trigger.svelte';
  import Tooltip from './tooltip.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Tooltip',
    component: Tooltip,
  });
</script>

<!-- bits-ui asks for a provider above every tooltip, and none is supplied
     globally, so each story carries its own. -->
{#snippet pauseButton()}
  <TooltipProvider>
    <Tooltip>
      <TooltipTrigger>
        {#snippet child({ props })}
          <Button {...props} variant="secondary">Pause</Button>
        {/snippet}
      </TooltipTrigger>
      <TooltipContent>Pause the current job</TooltipContent>
    </Tooltip>
  </TooltipProvider>
{/snippet}

<Story name="Default" asChild children={pauseButton} />

<Story name="Open State" asChild>
  <TooltipProvider>
    <Tooltip open>
      <TooltipTrigger>
        {#snippet child({ props })}
          <Button {...props} variant="secondary">Pause</Button>
        {/snippet}
      </TooltipTrigger>
      <TooltipContent>Pause the current job</TooltipContent>
    </Tooltip>
  </TooltipProvider>
</Story>

<Story
  name="Opens On Focus And Escape Closes Without Moving Focus"
  tags={['!dev', '!autodocs']}
  asChild
  children={pauseButton}
/>
