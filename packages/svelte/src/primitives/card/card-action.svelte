<!--
  A control or a status mark in the header: a badge, a menu button.

  It sits on its own row under the title while the header is narrow, and moves
  to the right of the title once the header clears 24rem.
-->
<script lang="ts">
  import type { CardActionProps } from './types';

  let { class: className, children, ...rest }: CardActionProps = $props();
</script>

<div
  data-slot="card-action"
  class={[
    'col-start-1 row-start-3 self-start justify-self-start @[24rem]/card-header:col-start-2 @[24rem]/card-header:row-span-2 @[24rem]/card-header:row-start-1 @[24rem]/card-header:justify-self-end',
    className,
  ]}
  {...rest}
>
  {@render children?.()}
</div>
