<!--
  A two state control a person flips, and the design system's own shape of it.

  The track is `rounded-sm`, not the pill upstream draws: at this size the
  corner radius is what says "switch" rather than the outline, and a pill reads
  as a tag. The focus ring is the Button's recipe, so a keyboard walk across a
  form ring every control the same way.

  The disabled colours use compound `disabled:data-[state=*]:*` selectors. A
  plain `disabled:*` ties on specificity with `data-[state=*]:*` and can lose,
  which left a disabled switch painted in the live colour.

  On and off must not differ by hue alone, since the on track and the off track
  can land on nearly the same luminance. Off is an outline: a transparent track
  with a `border-input` edge and an `input`-coloured thumb on the left. On is a
  solid `control-on` track with a light thumb on the right that carries a tick
  in `control-on`. A disabled switch drops the outline and the tick colour for
  the disabled pair, so it reads as inert in either state.

  The thumb has no background of its own. The root sets `text-*` and the thumb
  reads it through `bg-current`, so one colour decision covers both. The tick
  is aria-hidden: the state is announced by the root's `aria-checked`.
-->
<script lang="ts">
  import CheckIcon from '@lucide/svelte/icons/check';
  import { Switch as SwitchPrimitive } from 'bits-ui';

  import type { SwitchProps } from './types';

  let {
    ref = $bindable(null),
    checked = $bindable(false),
    class: className,
    size = 'md',
    ...rest
  }: SwitchProps = $props();
</script>

<SwitchPrimitive.Root
  bind:ref
  bind:checked
  data-slot="switch"
  data-size={size}
  class={[
    'peer group/switch inline-flex shrink-0 items-center rounded-sm border border-transparent transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:pointer-events-none data-[size=md]:h-[1.15rem] data-[size=md]:w-8 data-[size=sm]:h-3.5 data-[size=sm]:w-6 data-[state=checked]:bg-control-on data-[state=checked]:text-control-on-foreground data-[state=checked]:hover:bg-primary-hovered data-[state=checked]:active:bg-primary-pressed disabled:data-[state=checked]:bg-disabled disabled:data-[state=checked]:text-disabled-foreground data-[state=unchecked]:border-input data-[state=unchecked]:bg-transparent data-[state=unchecked]:text-input data-[state=unchecked]:hover:bg-accent disabled:data-[state=unchecked]:border-transparent disabled:data-[state=unchecked]:bg-disabled disabled:data-[state=unchecked]:text-disabled-foreground',
    className,
  ]}
  {...rest}
>
  <SwitchPrimitive.Thumb
    data-slot="switch-thumb"
    class="pointer-events-none grid place-content-center rounded-sm bg-current ring-0 transition-transform group-data-[size=md]/switch:size-4 group-data-[size=sm]/switch:size-3 data-[state=checked]:translate-x-[calc(100%-2px)] data-[state=unchecked]:translate-x-0"
  >
    {#snippet children({ checked: on })}
      {#if on}
        <CheckIcon
          aria-hidden="true"
          strokeWidth={3}
          class="text-control-on group-disabled/switch:text-disabled group-data-[size=md]/switch:size-[11px] group-data-[size=sm]/switch:size-2"
        />
      {/if}
    {/snippet}
  </SwitchPrimitive.Thumb>
</SwitchPrimitive.Root>
