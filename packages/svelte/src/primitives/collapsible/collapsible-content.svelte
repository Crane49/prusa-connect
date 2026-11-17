<!--
  What the section shows once it is open. It draws no colour and no spacing of
  its own, so a caller styles it for the place it sits in.

  While the section is shut bits-ui keeps the element and sets `hidden` on it.
  The content is then out of the tab order, out of the accessibility tree and
  off the screen, and bits-ui can measure its height for the open animation.
-->
<script lang="ts">
  import { Collapsible as CollapsiblePrimitive } from 'bits-ui';

  import type { CollapsibleContentProps } from './types';

  let { ref = $bindable(null), ...rest }: CollapsibleContentProps = $props();
</script>

<CollapsiblePrimitive.Content
  bind:ref
  data-slot="collapsible-content"
  {...rest}
/>
