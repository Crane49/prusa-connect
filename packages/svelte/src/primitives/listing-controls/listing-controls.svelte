<!--
  The cluster every listing carries at the trailing end of its toolbar: which
  fields it shows, and how it is drawn.

  Six screens grew this pair on their own and had already drifted. One put the
  field picker before the view toggle and one after, one spaced them by 8px
  and one by 12, one wrapped them in `ml-auto` and one relied on
  `justify-between`. None of that is a per-screen decision; it is one control
  group that happens to appear six times.

  It is deliberately only those two. Sorting and filtering are the obvious
  next members, since a listing's toolbar is filters and then this, and both
  are still per-screen: the printers filter by state and type, the catalogue
  by brand, colour and nozzle, and none of that generalises until the screens
  agree on what a filter is. When they do it joins here rather than being
  copied a seventh time. `children` is the seam, and takes whatever a screen
  puts to the left of the pair in the meantime.
-->
<script lang="ts">
  import FieldPicker from '../field-picker/field-picker.svelte';
  import ViewModeToggle from '../view-mode/view-mode.svelte';
  import type { ListingControlsProps } from './types';

  let {
    fields,
    fieldState,
    view,
    children,
    class: className,
  }: ListingControlsProps = $props();
</script>

<div
  data-slot="listing-controls"
  class={['flex items-center gap-2', className]}
>
  {@render children?.()}
  {#if fields && fieldState}
    <FieldPicker {fields} state={fieldState} />
  {/if}
  <ViewModeToggle
    value={view.value}
    onValueChange={view.onValueChange}
    options={view.options}
    label={view.label}
  />
</div>
