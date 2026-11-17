<!-- One place in the trail: a link, the current page, or the ellipsis. -->
<script lang="ts">
  import type { BreadcrumbItemProps } from './types';

  let { children, class: className, ...rest }: BreadcrumbItemProps = $props();
</script>

<li
  data-slot="breadcrumb-item"
  class={['inline-flex min-w-0 items-center gap-1.5', className]}
  {...rest}
>
  {@render children?.()}
</li>
