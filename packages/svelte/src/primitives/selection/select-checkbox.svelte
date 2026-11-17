<!--
  A row's tick box, bound to a Selection.

  Shift with a press ticks the range from the last row pressed. bits-ui calls
  `onclick` with the event before it toggles, so the range is taken there and
  its own toggle is cancelled. Space toggles without a click event, so the
  keyboard ticks one row at a time.
-->
<script lang="ts" generics="Key">
  import { Checkbox } from '../checkbox';
  import type { SelectCheckboxProps } from './types';

  let {
    selection,
    key,
    label,
    id,
    disabled,
    class: className,
  }: SelectCheckboxProps<Key> = $props();
</script>

<Checkbox
  checked={selection.has(key)}
  {...id === undefined ? {} : { id }}
  {disabled}
  class={className}
  aria-label={label}
  onclick={(event: MouseEvent) => {
    if (!event.shiftKey) return;
    event.preventDefault();
    selection.toggle(key, true);
  }}
  onCheckedChange={() => {
    selection.toggle(key);
  }}
/>
