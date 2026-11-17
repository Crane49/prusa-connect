<script lang="ts">
  import type { ControlIconProps } from './types';
  import { base } from './variants';

  let { class: className, ...rest }: ControlIconProps = $props();
</script>

<svg
  viewBox="0 0 24 24"
  xmlns="http://www.w3.org/2000/svg"
  data-slot="control-icon"
  class={[base, className]}
  {...rest}
>
  <path
    fill="currentColor"
    d="m15.89 2 2.356 2.356-7.781 7.647 7.781 7.642-2.356 2.356-10.14-9.997z"
  />
</svg>
