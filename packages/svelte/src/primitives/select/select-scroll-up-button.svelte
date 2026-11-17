<!--
  The strip at the top of a list longer than the surface. bits-ui draws it only
  while there is something above the viewport to scroll to.

  The element comes from bits-ui rather than from a `child` snippet here:
  bits-ui hands that snippet the caller's props alone, so an element drawn
  through it would lose the scroll handlers.
-->
<script lang="ts">
  import ChevronUpIcon from '@lucide/svelte/icons/chevron-up';
  import { Select as SelectPrimitive } from 'bits-ui';

  import type { SelectScrollUpButtonProps } from './types';

  let {
    ref = $bindable(null),
    class: className,
    ...rest
  }: SelectScrollUpButtonProps = $props();
</script>

<SelectPrimitive.ScrollUpButton
  bind:ref
  data-slot="select-scroll-up-button"
  class={['flex cursor-default items-center justify-center py-1', className]}
  {...rest}
>
  <ChevronUpIcon class="size-4" />
</SelectPrimitive.ScrollUpButton>
