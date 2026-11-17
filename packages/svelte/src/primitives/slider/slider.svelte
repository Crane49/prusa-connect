<!--
  A value a person drags along a track, with one thumb or two.

  Track and thumb are `rounded-sm`, the shape the rest of the library uses, and
  the thumb answers hover and press with its border colour rather than the
  growing ring upstream draws. The drag position is deliberately untransitioned
  so the thumb follows the pointer one to one.

  Disabled is read from `data-disabled`: the thumb is a `<span role="slider">`
  and not a native control, so `:disabled` never matches it. The disabled thumb
  takes a border and a fill of its own, because `border-transparent` made it
  disappear into the page.

  `aria-label` and `aria-labelledby` go to every thumb and not to the root,
  since `role="slider"` lives on the thumb and that is the element axe reads a
  name off (`aria-input-field-name`).
-->
<script lang="ts">
  import { Slider as SliderPrimitive } from 'bits-ui';

  import type { SliderProps } from './types';

  let {
    ref = $bindable(null),
    value = $bindable(),
    orientation = 'horizontal',
    class: className,
    'aria-label': ariaLabel,
    'aria-labelledby': ariaLabelledby,
    ...rest
  }: SliderProps = $props();
</script>

<SliderPrimitive.Root
  bind:ref
  bind:value={value as never}
  {orientation}
  data-slot="slider"
  class={[
    'relative flex w-full touch-none items-center select-none data-[disabled]:pointer-events-none data-[orientation=vertical]:h-full data-[orientation=vertical]:min-h-44 data-[orientation=vertical]:w-auto data-[orientation=vertical]:flex-col',
    className,
  ]}
  {...rest}
>
  {#snippet children({ thumbItems })}
    <!-- The headless layer has no track part, so the track is plain markup and
         takes the orientation from here rather than from the primitive. -->
    <span
      data-slot="slider-track"
      data-orientation={orientation}
      class="relative grow overflow-hidden rounded-sm bg-muted data-[orientation=horizontal]:h-1.5 data-[orientation=horizontal]:w-full data-[orientation=vertical]:h-full data-[orientation=vertical]:w-1.5"
    >
      <SliderPrimitive.Range
        data-slot="slider-range"
        class="absolute bg-control-on data-[disabled]:bg-disabled data-[orientation=horizontal]:h-full data-[orientation=vertical]:w-full"
      />
    </span>
    {#each thumbItems as thumb (thumb.index)}
      <SliderPrimitive.Thumb
        index={thumb.index}
        data-slot="slider-thumb"
        aria-label={ariaLabel}
        aria-labelledby={ariaLabelledby}
        class="block size-4 shrink-0 rounded-sm border border-control-on bg-background transition-colors outline-none hover:border-primary-hovered focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background active:border-primary-pressed data-[disabled]:border-disabled-foreground data-[disabled]:bg-disabled"
      />
    {/each}
  {/snippet}
</SliderPrimitive.Root>
