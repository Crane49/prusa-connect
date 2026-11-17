<script lang="ts">
  import { Select as SelectPrimitive } from 'bits-ui';

  import type { SelectGroupProps } from './types';

  let { ref = $bindable(null), ...rest }: SelectGroupProps = $props();
</script>

<SelectPrimitive.Group bind:ref data-slot="select-group" {...rest} />
