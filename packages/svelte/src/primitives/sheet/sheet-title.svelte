<!--
  The name of the panel. bits-ui reports its `id` to the content, which points
  its `aria-labelledby` here, so a panel with a title is named.

  bits-ui draws a `div` with `role="heading"` and `aria-level="2"`, where Radix
  draws an `h2`. A caller that needs another level passes `level`.
-->
<script lang="ts">
  import { Dialog as SheetPrimitive } from 'bits-ui';

  import type { SheetTitleProps } from './types';

  let {
    ref = $bindable(null),
    class: className,
    ...rest
  }: SheetTitleProps = $props();
</script>

<SheetPrimitive.Title
  bind:ref
  data-slot="sheet-title"
  class={['text-lg leading-none font-semibold', className]}
  {...rest}
/>
