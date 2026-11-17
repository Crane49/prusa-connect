<!--
  The title row: a title, an optional description, and an optional action.

  The action shares the title's line only where there is room for both. Below
  24rem of header, which is a card on a 320px phone, a two-column split left
  the title a 40px column and wrapped `API tokens` one letter per line, so
  there the action drops to its own row underneath (see `card-action.svelte`).
-->
<script lang="ts">
  import type { CardHeaderProps } from './types';

  let { class: className, children, ...rest }: CardHeaderProps = $props();
</script>

<div
  data-slot="card-header"
  class={[
    '@container/card-header grid auto-rows-min grid-rows-[auto_auto] items-start gap-2 px-4 @[24rem]/card:px-6 @[24rem]/card-header:has-data-[slot=card-action]:grid-cols-[1fr_auto] [.border-b]:pb-6',
    className,
  ]}
  {...rest}
>
  {@render children?.()}
</div>
