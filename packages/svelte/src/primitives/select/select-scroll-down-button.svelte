<!--
  The strip at the foot of a list longer than the surface. bits-ui draws it only
  while there is something below the viewport to scroll to.

  The element comes from bits-ui rather than from a `child` snippet here:
  bits-ui hands that snippet the caller's props alone, so an element drawn
  through it would lose the scroll handlers.
-->
<script lang="ts">
  import ChevronDownIcon from '@lucide/svelte/icons/chevron-down';
  import { Select as SelectPrimitive } from 'bits-ui';

  import type { SelectScrollDownButtonProps } from './types';

  let {
    ref = $bindable(null),
    class: className,
    ...rest
  }: SelectScrollDownButtonProps = $props();
</script>

<SelectPrimitive.ScrollDownButton
  bind:ref
  data-slot="select-scroll-down-button"
  class={['flex cursor-default items-center justify-center py-1', className]}
  {...rest}
>
  <ChevronDownIcon class="size-4" />
</SelectPrimitive.ScrollDownButton>
