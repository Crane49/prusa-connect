<!--
  A turning arc for work in flight.

  It is hidden from assistive technology. What is happening is announced by
  the parent's live region, so the glyph does not announce it a second time.
  Under reduced motion `--animate-spin` is `none` (tokens.css), so the arc
  stands still and nothing at a call site needs `motion-reduce:`.
-->
<script lang="ts">
  import LoaderCircleIcon from '@lucide/svelte/icons/loader-circle';

  import type { SpinnerProps } from './types';

  let {
    size = 'sm',
    tone = 'inherit',
    class: className,
  }: SpinnerProps = $props();

  const SIZE = { sm: 'size-4', md: 'size-5', lg: 'size-6' };
  const TONE = {
    primary: 'text-progress',
    muted: 'text-muted-foreground',
    inherit: undefined,
  };
</script>

<LoaderCircleIcon
  data-slot="spinner"
  aria-hidden="true"
  class={['animate-spin', SIZE[size], TONE[tone], className]}
/>
