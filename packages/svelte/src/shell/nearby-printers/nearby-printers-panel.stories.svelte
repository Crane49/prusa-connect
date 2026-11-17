<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import type { NearbyPrinter } from '../../data';
  import NearbyPrintersPanel from './nearby-printers-panel.svelte';
  import type { NearbySource } from './types';

  const { Story } = defineMeta({
    title: 'Parts/Nearby Printers Panel',
    component: NearbyPrintersPanel,
  });

  const DESK: NearbyPrinter = {
    address: '192.168.1.20',
    name: 'Desk MK4',
    port: 80,
    services: ['_prusalink._tcp'],
    tls: false,
    url: 'http://192.168.1.20:80/',
  };

  const KNOWN: NearbyPrinter = {
    ...DESK,
    address: '192.168.1.21',
    name: 'Lab XL',
    url: 'http://192.168.1.21:80/',
    device_id: 'dev-lab',
  };

  const OTHER_SERVER: NearbyPrinter = {
    address: '192.168.1.30',
    name: 'Office Prusa Connect',
    port: 8080,
    services: ['_prusa-connect._tcp', '_prusalink._tcp'],
    tls: false,
    url: 'http://192.168.1.30:8080/',
  };

  const sourceOf = (over: Partial<NearbySource> = {}): NearbySource => ({
    items: [DESK, KNOWN, OTHER_SERVER],
    loaded: true,
    error: undefined,
    ...over,
  });
</script>

<!--
  The panel is a hint and draws nothing while it has nothing to say, so the
  "Nobody Heard" and "Still Listening" stories are empty frames on purpose.
-->

<Story name="Printers Heard" asChild>
  <NearbyPrintersPanel source={sourceOf()} addHref="/printers/add" />
</Story>

<Story name="Without The Add Guide" asChild>
  <NearbyPrintersPanel source={sourceOf()} />
</Story>

<Story name="Nobody Heard" asChild>
  <NearbyPrintersPanel source={sourceOf({ items: [] })} />
</Story>

<Story name="Still Listening" asChild>
  <NearbyPrintersPanel source={sourceOf({ items: [], loaded: false })} />
</Story>

<Story name="Refused" asChild>
  <NearbyPrintersPanel
    source={sourceOf({
      items: [],
      error: {
        code: 'core.transport/NETWORK',
        status: undefined,
        params: {},
        message: 'the server could not be reached',
        traceId: undefined,
      },
    })}
  />
</Story>
