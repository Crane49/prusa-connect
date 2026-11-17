<!--
  What to do about it, under the title. It inherits the alert's colour rather
  than naming one, so a destructive alert reads in one tone throughout.
-->
<script lang="ts">
  import type { AlertDescriptionProps } from './types';

  let { class: className, children, ...rest }: AlertDescriptionProps = $props();
</script>

<div
  data-slot="alert-description"
  class={[
    'col-start-2 grid justify-items-start gap-1 text-sm [&_p]:leading-relaxed',
    className,
  ]}
  {...rest}
>
  {@render children?.()}
</div>
