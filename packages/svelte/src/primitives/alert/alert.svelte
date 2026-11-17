<!--
  A short message about the state of something: a finished print, a clog, a
  firmware update waiting.

  `role="alert"` makes the box a live region, so a screen reader reads it as
  soon as it appears without being asked. That suits a message that arrives on
  its own and is wrong for a notice that is simply part of the page.

  The title and the description sit in the second grid column. An icon passed
  as a direct child takes the first.
-->
<script lang="ts">
  import type { AlertProps } from './types';
  import { base, variants } from './variants';

  let {
    variant = 'default',
    class: className,
    children,
    ...rest
  }: AlertProps = $props();
</script>

<div
  data-slot="alert"
  data-variant={variant}
  role="alert"
  class={[base, variants[variant], className]}
  {...rest}
>
  {@render children?.()}
</div>
