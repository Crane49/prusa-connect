<!--
  A grey block standing where content has not arrived yet.

  The caller gives it the size and the shape of what it stands for, so the box
  it leaves is the box the real content fills. It draws no text and no border:
  the pulse is the only thing that says the page is still working.
-->
<script lang="ts">
  import type { SkeletonProps } from './types';

  let { class: className, ...rest }: SkeletonProps = $props();
</script>

<div
  {...rest}
  data-slot="skeleton"
  class={['animate-pulse rounded-sm bg-accent', className]}
></div>
