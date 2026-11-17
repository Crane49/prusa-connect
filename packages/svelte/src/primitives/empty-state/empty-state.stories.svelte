<script module lang="ts">
  import InboxIcon from '@lucide/svelte/icons/inbox';
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import Button from '../button/button.svelte';
  import CardContent from '../card/card-content.svelte';
  import Card from '../card/card.svelte';
  import EmptyState from './empty-state.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/EmptyState',
    component: EmptyState,
    args: {
      icon: InboxIcon,
      heading: 'No printers yet',
      headingLevel: 'h2',
      description: 'Printers you add will show up here.',
    },
  });
</script>

{#snippet addPrinter()}
  <Button size="sm">Add a printer</Button>
{/snippet}

<Story name="Default" />

<Story name="With Action" args={{ action: addPrinter }} />

<Story name="Small" args={{ size: 'sm' }} />

<Story
  name="Small With Action"
  args={{ size: 'sm', headingLevel: 'h3', action: addPrinter }}
/>

<Story name="Inside A Card" asChild>
  <Card class="w-80">
    <CardContent>
      <EmptyState
        icon={InboxIcon}
        heading="No printers yet"
        headingLevel="h3"
        description="Printers you add will show up here."
      />
    </CardContent>
  </Card>
</Story>

<Story
  name="Heading Level Reaches The Rendered Tag"
  tags={['!dev', '!autodocs']}
  args={{ headingLevel: 'h3' }}
/>

<!-- Beyond the React set -->

<Story
  name="Heading Alone"
  args={{ icon: undefined, description: undefined }}
/>
