<!--
  The mark between two crumbs. It is a chevron unless the caller passes its
  own, and it is hidden from a screen reader: the list order already says what
  it says.
-->
<script lang="ts">
  import ChevronRightIcon from '@lucide/svelte/icons/chevron-right';

  import type { BreadcrumbSeparatorProps } from './types';

  let {
    children,
    class: className,
    ...rest
  }: BreadcrumbSeparatorProps = $props();
</script>

<li
  data-slot="breadcrumb-separator"
  role="presentation"
  aria-hidden="true"
  class={['[&>svg]:size-3.5', className]}
  {...rest}
>
  {#if children}
    {@render children()}
  {:else}
    <ChevronRightIcon />
  {/if}
</li>
