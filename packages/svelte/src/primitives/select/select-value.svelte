<!--
  The chosen option written in the trigger, or the placeholder while nothing is
  chosen. The trigger's own classes select this element through its
  `data-slot`, so the attribute is what lines the text up beside the chevron.
-->
<script lang="ts">
  import { Select as SelectPrimitive } from 'bits-ui';

  import type { SelectValueProps } from './types';

  let { ref = $bindable(null), ...rest }: SelectValueProps = $props();
</script>

<SelectPrimitive.Value bind:ref data-slot="select-value" {...rest} />
