<!--
  The header box over a listing's tick boxes: ticked when every row it covers
  is, a dash when only some are. Pressing a dash ticks them all, as bits-ui
  does for a mixed box.
-->
<script lang="ts" generics="Key">
  import { Checkbox } from '../checkbox';
  import type { SelectAllCheckboxProps } from './types';

  let {
    selection,
    keys,
    label,
    disabled,
    class: className,
  }: SelectAllCheckboxProps<Key> = $props();

  const state = $derived(selection.stateOf(keys));
</script>

<Checkbox
  checked={state === 'all'}
  indeterminate={state === 'some'}
  disabled={(disabled ?? false) || keys.length === 0}
  class={className}
  aria-label={label}
  onCheckedChange={(on: boolean) => {
    selection.set(keys, on);
  }}
/>
