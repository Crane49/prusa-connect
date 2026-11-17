<script module lang="ts">
  import type { LucideIconData } from '@lucide/svelte';

  // The outline is a filled shape on a 24 by 28 box, so it is scaled to the
  // 24 pixel height of lucide's grid and centred; strokeWidth has no effect.
  const icon: LucideIconData = {
    name: 'printer-glyph',
    size: 24,
    node: [
      [
        'path',
        {
          d: 'M22.7201 0H1.29104C0.581217 0 0.00585938 0.575357 0.00585938 1.28571V22.7143C0.00585938 23.4241 0.581217 24 1.29157 24H5.14872V26.1429C5.14872 26.8527 5.72407 27.4286 6.43443 27.4286H17.5773C18.2871 27.4286 18.863 26.8532 18.863 26.1429V24H22.7201C23.43 24 24.0059 23.4246 24.0059 22.7143V1.28571C24.0059 0.575893 23.4305 0 22.7201 0ZM17.1487 25.7143H6.863V20.5714H17.1487V25.7143ZM22.2916 22.2857H18.863V20.1429C18.863 19.433 18.2876 18.8571 17.5773 18.8571H6.4339C5.72407 18.8571 5.14818 19.4325 5.14818 20.1429V22.2857H1.72015V8.57143H5.14872V10.7143C5.14872 11.4241 5.72407 12 6.43443 12H9.00586C9.71568 12 10.2916 11.4246 10.2916 10.7143V8.57143H22.2916V22.2857ZM6.863 10.2857V5.14286H8.57729V10.2857H6.863ZM22.2916 6.85714H10.2916V4.71429C10.2916 4.00446 9.71622 3.42857 9.00586 3.42857H6.43443C5.72461 3.42857 5.14872 4.00393 5.14872 4.71429V6.85714H1.72015V1.71429H22.2916V6.85714Z',
          fill: 'currentColor',
          stroke: 'none',
          transform: 'translate(1.5 0) scale(0.875)',
        },
      ],
    ],
  };
</script>

<script lang="ts">
  import { Icon, type LucideProps } from '@lucide/svelte';

  let props: LucideProps = $props();
</script>

<Icon {...props} {icon} />
