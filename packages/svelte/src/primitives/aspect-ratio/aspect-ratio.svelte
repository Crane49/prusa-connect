<!--
  A box that keeps one shape at any width: a camera frame, a thumbnail, a
  placeholder that must not jump when its picture arrives.

  The ratio goes into CSS as one custom property and the browser computes the
  height (frontend-css.md). The element takes no class names of its own, so
  the surface, the border and the corners all come from the caller.
-->
<script lang="ts">
  import type { AspectRatioProps } from './types';

  let {
    ratio = 1,
    class: className,
    children,
    ...rest
  }: AspectRatioProps = $props();
</script>

<div
  {...rest}
  data-slot="aspect-ratio"
  style="--aspect-ratio: {ratio}"
  class={className}
>
  {@render children?.()}
</div>

<style>
  [data-slot='aspect-ratio'] {
    aspect-ratio: var(--aspect-ratio);
  }
</style>
