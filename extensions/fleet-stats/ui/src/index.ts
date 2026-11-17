// The bundle's entry. The app imports this file and calls what it default
// exports, handing it the one object a module receives.
//
// `defineModule` is an identity function. It exists so an author gets the
// host's types at this call site without importing the host.
import { defineModule } from '@prusa/connect-ui/slots';

import { holdHost } from './host';
import Page from './page.svelte';
import DeviceCard from './device-card.svelte';
import DeviceSection from './device-section.svelte';
import FailuresCard from './cards/failures-card.svelte';
import KpiCard from './cards/kpi-card.svelte';
import TrendCard from './cards/trend-card.svelte';
import UtilizationCard from './cards/utilization-card.svelte';
import './styles.css';

export default defineModule((host) => {
  // Everything a component of this module needs is built here, from the host
  // and from nothing else. The host arrives before any component is mounted,
  // so it is held rather than threaded down.
  holdHost(host);

  // host.signal fires when the person who signed in leaves. Every request of
  // host.client ends with it. Pass it to a call that takes a signal, and stop
  // every timer and stream you start when it fires, for example:
  //
  //   const timer = setInterval(refresh, 5000);
  //   host.signal.addEventListener('abort', () => clearInterval(timer));
  //   await client.listThings({}, host.signal);

  // Every registration below is held to `contributes` in module.yaml: a name
  // the manifest does not declare is refused at run time and the app drops
  // this module. The generator wrote both from one answer, so they agree; keep
  // them in step by hand after that.
  host.register.page('fleet-stats', Page);
  host.register.deviceCard('fleet-stats', DeviceCard);
  host.register.deviceSection('fleet-stats', DeviceSection);
  host.register.card('kpis', KpiCard);
  host.register.card('trend', TrendCard);
  host.register.card('utilization', UtilizationCard);
  host.register.card('failures', FailuresCard);
});
