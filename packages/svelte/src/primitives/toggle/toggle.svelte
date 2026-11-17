<script lang="ts">
  import { Toggle as TogglePrimitive } from 'bits-ui';

  import type { ToggleProps } from './types';
  import { base, radius, sizes, variants } from './variants';

  // bits-ui declares `pressed` bindable. A consumer that passes it without
  // `bind:` gets a starting state the toggle then owns, which is what React's
  // `defaultPressed` does.
  let {
    ref = $bindable(null),
    pressed = $bindable(false),
    class: className,
    variant = 'default',
    size = 'md',
    ...rest
  }: ToggleProps = $props();
</script>

<TogglePrimitive.Root
  bind:ref
  bind:pressed
  data-slot="toggle"
  class={[base, radius, variants[variant], sizes[size], className]}
  {...rest}
/>
