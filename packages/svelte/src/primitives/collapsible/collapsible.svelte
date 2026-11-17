<!--
  A section a person opens and shuts. bits-ui, for the open state, the
  `aria-expanded` and `aria-controls` pair, and the `data-state` the trigger and
  the content are styled by.

  The root draws no colour of its own. The trigger carries the whole recipe.
-->
<script lang="ts">
  import { Collapsible as CollapsiblePrimitive } from 'bits-ui';

  import type { CollapsibleProps } from './types';

  let {
    ref = $bindable(null),
    open = $bindable(false),
    ...rest
  }: CollapsibleProps = $props();
</script>

<CollapsiblePrimitive.Root
  bind:ref
  bind:open
  data-slot="collapsible"
  {...rest}
/>
