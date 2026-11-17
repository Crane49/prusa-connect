<script lang="ts">
  import type { ControlIconProps } from './types';
  import { base } from './variants';

  let { class: className, ...rest }: ControlIconProps = $props();
</script>

<svg
  viewBox="0 0 27.41 19.19"
  xmlns="http://www.w3.org/2000/svg"
  data-slot="control-icon"
  class={[base, className]}
  {...rest}
>
  <path
    fill="currentColor"
    opacity="0.6"
    fill-rule="nonzero"
    d="M7.54 5.14c0-.57-.46-1.03-1.03-1.03-.57 0-1.03.46-1.03 1.03v8.91c0 .57.46 1.03 1.03 1.03.57 0 1.03-.46 1.03-1.03zm12.33 0c0-.57.46-1.03 1.03-1.03.57 0 1.03.46 1.03 1.03v8.91c0 .57-.46 1.03-1.03 1.03-.57 0-1.03-.46-1.03-1.03z"
  />
  <path
    fill="currentColor"
    fill-rule="nonzero"
    d="M27.41 1.37c0-.65-.52-1.37-1.37-1.37H1.37C.52 0 0 .71 0 1.37v16.45c0 .85.71 1.37 1.37 1.37h24.67c.66 0 1.37-.52 1.37-1.37V1.37Zm-25.35.69h23.3v15.07H2.06Z"
  />
  <path
    fill="none"
    stroke="currentColor"
    stroke-width="1.84224"
    stroke-linecap="round"
    stroke-linejoin="round"
    d="m4.805 4.598 2.91 2.545-2.888 2.68 2.871 2.539-2.861 2.42M22.605 4.598l-2.91 2.545 2.888 2.68-2.871 2.539 2.861 2.42"
  />
</svg>
