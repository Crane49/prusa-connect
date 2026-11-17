<!--
  The key combination that runs an item, drawn at the end of its row. It is a
  plain span, and it carries no behaviour: pressing the keys is the caller's
  own handler.
-->
<script lang="ts">
  import type { DropdownMenuShortcutProps } from './types';

  let {
    children,
    class: className,
    ...rest
  }: DropdownMenuShortcutProps = $props();
</script>

<span
  data-slot="dropdown-menu-shortcut"
  class={[
    'ml-auto font-mono text-xs tracking-widest text-muted-foreground',
    className,
  ]}
  {...rest}
>
  {@render children?.()}
</span>
