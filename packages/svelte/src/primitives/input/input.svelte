<!--
  A single line of text a person types into.

  The height, the inset and the type size come from the control scale in
  `control-trigger`, the same scale a select and a facet stand on. A search
  field and the facets beside it in a listing toolbar are then one control at
  one height. `size` shadows the native attribute of that name, which counts
  characters and no caller here wants.

  It paints nothing for an invalid value. `Field` adds the `aria-invalid:*`
  classes when it has an error to show, so a control wired by hand and a
  control inside a field cannot drift apart.

  The focus ring is the Button's recipe, so a keyboard walk across a form rings
  every control the same way.
-->
<script lang="ts">
  import { CONTROL_SIZES } from '../control-trigger/control-trigger';
  import type { InputProps } from './types';

  let { size = 'md', class: className, ...rest }: InputProps = $props();
</script>

<input
  data-slot="input"
  data-size={size}
  class={[
    'w-full min-w-0 rounded-sm border border-input bg-transparent transition-colors outline-none selection:bg-primary selection:text-primary-foreground file:inline-flex file:h-7 file:border-0 file:bg-transparent file:text-sm file:font-medium file:text-foreground placeholder:text-muted-foreground disabled:pointer-events-none disabled:text-disabled-foreground',
    CONTROL_SIZES[size],
    'focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background',
    className,
  ]}
  {...rest}
/>
