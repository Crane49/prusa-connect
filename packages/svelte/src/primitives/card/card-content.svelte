<!-- The body of the card. It takes the card's own padding step. -->
<script lang="ts">
  import type { CardContentProps } from './types';

  let { class: className, children, ...rest }: CardContentProps = $props();
</script>

<div
  data-slot="card-content"
  class={['px-4 @[24rem]/card:px-6', className]}
  {...rest}
>
  {@render children?.()}
</div>
