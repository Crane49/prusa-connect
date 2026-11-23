<!--
  The route reads the printers through the session the setup holds. The story
  frame has no PrusaSlicer in it, so it shows the page as a browser does.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';
  import type { ModuleHost } from '@prusa/connect-ui/slots';

  import { holdSession } from '../../session';
  import { farm, t } from '../../test/fakes';
  import SelectPrinterRoute from './select-printer-route.svelte';

  holdSession({
    host: { t, signal: new AbortController().signal } as unknown as ModuleHost,
    api: {
      listSlicerPrinters: () => Promise.resolve({ printers: farm }),
    },
  });

  const { Story } = defineMeta({
    title: 'PrusaSlicer 3/SelectPrinterRoute',
    component: SelectPrinterRoute,
  });
</script>

<Story name="Live" />
