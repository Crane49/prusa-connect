<!--
  A control that opens a list of choices and keeps the one a person picks.
  bits-ui carries the open state, the typeahead, the keyboard walk and the
  hidden input a form reads.

  The root renders no element, so it takes no `data-slot` and no class. Every
  class is on the trigger and on the surface.

  bits-ui's root is a union over a `type` prop, `single` against `multiple`.
  The design system has one select and it holds one value, so this component
  fixes the arm and a caller never names it. That also gives `value` a single
  type, which is what lets a caller bind it: one bindable typed
  `string | string[]` satisfies neither arm of the union.

  A caller passes `items`, the value and label of every option. Without it the
  trigger falls back to the raw value once the list shuts, because bits-ui
  reads an option's text off an element that is no longer mounted.
-->
<script lang="ts">
  import { Select as SelectPrimitive } from 'bits-ui';

  import type { SelectProps } from './types';

  let { value = $bindable(''), ...rest }: SelectProps = $props();
</script>

<SelectPrimitive.Root type="single" bind:value {...rest} />
