<!--
  The section as the host draws it, over a host whose client mints a
  credential. Create an API key shows the key the fake client answers.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { holdHost } from '../../host';
  import { stubHost } from '../../test/fakes';
  import PrusalinkRoute from './prusalink-route.svelte';

  holdHost(stubHost().host);

  const { Story } = defineMeta({
    title: 'PrusaSlicer 2 Connector/PrusalinkRoute',
    component: PrusalinkRoute,
    args: { deviceId: 'mk4-01', origin: 'http://192.168.1.20:8080' },
  });
</script>

<Story name="On a printer" />
