<!-- One quieter line under the title. -->
<script lang="ts">
  import type { CardDescriptionProps } from './types';

  let { class: className, children, ...rest }: CardDescriptionProps = $props();
</script>

<div
  data-slot="card-description"
  class={['text-sm text-muted-foreground', className]}
  {...rest}
>
  {@render children?.()}
</div>
