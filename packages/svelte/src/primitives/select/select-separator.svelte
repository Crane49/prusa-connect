<!--
  A rule between two groups of options. bits-ui has no separator part for a
  select, so this is a plain element. It is hidden from the accessibility tree,
  because the group headings already say where one group ends.
-->
<script lang="ts">
  import type { SelectSeparatorProps } from './types';

  let { class: className, ...rest }: SelectSeparatorProps = $props();
</script>

<div
  data-slot="select-separator"
  aria-hidden="true"
  class={['pointer-events-none -mx-1 my-1 h-px bg-border', className]}
  {...rest}
></div>
