<!--
  The sentence under the title. bits-ui reports its `id` to the content, which
  points its `aria-describedby` here.
-->
<script lang="ts">
  import { Dialog as SheetPrimitive } from 'bits-ui';

  import type { SheetDescriptionProps } from './types';

  let {
    ref = $bindable(null),
    class: className,
    ...rest
  }: SheetDescriptionProps = $props();
</script>

<SheetPrimitive.Description
  bind:ref
  data-slot="sheet-description"
  class={['text-sm text-muted-foreground', className]}
  {...rest}
/>
