<script lang="ts">
  import type { ControlIconProps } from './types';
  import { base } from './variants';

  let { class: className, ...rest }: ControlIconProps = $props();
</script>

<svg
  viewBox="2 2 18 18"
  xmlns="http://www.w3.org/2000/svg"
  data-slot="control-icon"
  class={[base, className]}
  {...rest}
>
  <path
    fill="currentColor"
    opacity="0.6"
    stroke="currentColor"
    d="m6.52174,6.57609l-4.47826,0l0,-4.42391l4.47826,0l0,4.42391zm6.71739,-4.42391l-4.47826,0l0,4.42391l4.47826,0l0,-4.42391zm6.71739,0l-4.47826,0l0,4.42391l4.47826,0l0,-4.42391zm-13.43478,6.63587l-4.47826,0l0,4.42391l4.47826,0l0,-4.42391zm6.71739,0l-4.47826,0l0,4.42391l4.47826,0l0,-4.42391zm6.71739,0l-4.47826,0l0,4.42391l4.47826,0l0,-4.42391zm-13.43478,6.63587l-4.47826,0l0,4.42391l4.47826,0l0,-4.42391zm6.71739,0l-4.47826,0l0,4.42391l4.47826,0l0,-4.42391zm6.71739,0l-4.47826,0l0,4.42391l4.47826,0l0,-4.42391z"
  />
</svg>
