<!--
  What the card is about.

  It is a div and carries no heading level, because the right level depends on
  where the card sits. A caller that needs a heading nests one with
  `class="contents"`, which keeps the heading out of the header's grid sizing.
-->
<script lang="ts">
  import type { CardTitleProps } from './types';

  let { class: className, children, ...rest }: CardTitleProps = $props();
</script>

<div
  data-slot="card-title"
  class={['leading-none font-semibold', className]}
  {...rest}
>
  {@render children?.()}
</div>
