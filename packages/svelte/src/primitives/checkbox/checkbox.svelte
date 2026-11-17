<!--
  A box a person ticks. bits-ui, for the role, the keyboard and the `data-state`
  every colour below is keyed on.

  The focus ring is the Button's recipe, so a keyboard walk across a form rings
  every control the same way.

  The disabled colours use compound `disabled:data-[state=checked]:*` selectors.
  A plain `disabled:*` ties on specificity with `data-[state=checked]:*` and can
  lose, which leaves a disabled ticked box painted in the live colour. The tick
  itself is `text-current`, so the root's `text-*` covers both.
-->
<script lang="ts">
  import CheckIcon from '@lucide/svelte/icons/check';
  import MinusIcon from '@lucide/svelte/icons/minus';
  import { Checkbox as CheckboxPrimitive } from 'bits-ui';

  import type { CheckboxProps } from './types';

  let {
    ref = $bindable(null),
    checked = $bindable(false),
    indeterminate = $bindable(false),
    class: className,
    ...rest
  }: CheckboxProps = $props();
</script>

<CheckboxPrimitive.Root
  bind:ref
  bind:checked
  bind:indeterminate
  data-slot="checkbox"
  class={[
    'peer size-4 shrink-0 rounded-sm border border-input transition-colors outline-none hover:border-primary focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:pointer-events-none disabled:text-disabled-foreground data-[state=checked]:border-control-on data-[state=checked]:bg-control-on data-[state=checked]:text-control-on-foreground data-[state=checked]:hover:border-primary-hovered data-[state=checked]:hover:bg-primary-hovered data-[state=checked]:active:bg-primary-pressed disabled:data-[state=checked]:border-transparent disabled:data-[state=checked]:bg-disabled disabled:data-[state=checked]:text-disabled-foreground data-[state=indeterminate]:border-control-on data-[state=indeterminate]:bg-control-on data-[state=indeterminate]:text-control-on-foreground',
    className,
  ]}
  {...rest}
>
  {#snippet children({ checked: ticked, indeterminate: mixed })}
    <!-- bits-ui has no indicator part of its own, so the mark is drawn here:
         a tick, or a dash for a box that stands for rows of which only some
         are ticked. -->
    {#if mixed}
      <span
        data-slot="checkbox-indicator"
        class="grid place-content-center text-current transition-none"
      >
        <MinusIcon class="size-3.5" />
      </span>
    {:else if ticked}
      <span
        data-slot="checkbox-indicator"
        class="grid place-content-center text-current transition-none"
      >
        <CheckIcon class="size-3.5" />
      </span>
    {/if}
  {/snippet}
</CheckboxPrimitive.Root>
