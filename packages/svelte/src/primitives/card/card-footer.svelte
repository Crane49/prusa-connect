<!--
  The row of actions at the bottom. A caller that adds `border-t` gets the
  extra top padding that goes with a divided footer.
-->
<script lang="ts">
  import type { CardFooterProps } from './types';

  let { class: className, children, ...rest }: CardFooterProps = $props();
</script>

<div
  data-slot="card-footer"
  class={[
    'flex items-center px-4 @[24rem]/card:px-6 [.border-t]:pt-6',
    className,
  ]}
  {...rest}
>
  {@render children?.()}
</div>
