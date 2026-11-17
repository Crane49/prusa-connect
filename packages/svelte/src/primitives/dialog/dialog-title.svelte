<script lang="ts">
  import { Dialog as DialogPrimitive } from 'bits-ui';

  import type { DialogTitleProps } from './types';

  let {
    ref = $bindable(null),
    class: className,
    ...rest
  }: DialogTitleProps = $props();
</script>

<DialogPrimitive.Title
  bind:ref
  data-slot="dialog-title"
  class={['text-lg leading-tight font-semibold break-words', className]}
  {...rest}
/>
