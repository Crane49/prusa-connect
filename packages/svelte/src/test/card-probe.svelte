<!--
  A whole card, for the test to render. The parts only work together, and a
  test cannot write markup of its own.

  Every word comes in as a prop, so the test says what it asserts on.
-->
<script lang="ts">
  import CardAction from '../primitives/card/card-action.svelte';
  import CardContent from '../primitives/card/card-content.svelte';
  import CardDescription from '../primitives/card/card-description.svelte';
  import CardFooter from '../primitives/card/card-footer.svelte';
  import CardHeader from '../primitives/card/card-header.svelte';
  import CardTitle from '../primitives/card/card-title.svelte';
  import type { CardTone } from '../primitives/card/types';
  import Card from '../primitives/card/card.svelte';

  interface Props {
    title: string;
    description: string;
    body: string;
    footer: string;
    /** The word on the mark beside the title. Omit it to leave the action out. */
    action?: string;
    /** Names the card as a landmark region, the way a page section does. */
    landmark?: boolean;
    tone?: CardTone;
  }

  let {
    title,
    description,
    body,
    footer,
    action,
    landmark = false,
    tone = 'default',
  }: Props = $props();
</script>

<Card
  {tone}
  role={landmark ? 'region' : undefined}
  aria-labelledby={landmark ? 'card-probe-title' : undefined}
>
  <CardHeader>
    <CardTitle id="card-probe-title">
      <h2 class="contents">{title}</h2>
    </CardTitle>
    <CardDescription>{description}</CardDescription>
    {#if action}
      <CardAction>{action}</CardAction>
    {/if}
  </CardHeader>
  <CardContent>{body}</CardContent>
  <CardFooter>{footer}</CardFooter>
</Card>
