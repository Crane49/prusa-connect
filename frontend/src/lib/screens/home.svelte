<!--
  Where this application opens. With the dashboard chosen, which is the
  default, the home page is the dashboard (ADR-20261008-sww rule 10). Otherwise the
  page decides which page of printers to open and then steps aside.
-->
<script lang="ts">
  import { homeOn } from '@prusa/connect-ui/domain';
  import { printerHref } from '@prusa/connect-ui/shell';

  import { PATHS } from '../routes';
  import type { AppContext } from '../screens';
  import DashboardPage from './dashboard.svelte';

  let { ctx }: { ctx: AppContext } = $props();

  // A server that leaves dashboards out reads the choice as automatic.
  const preference = $derived(
    homeOn(ctx.preferences.home, ctx.settings.dashboards),
  );
  // The server says whether it keeps dashboards, so the page waits for that
  // answer before it draws one or moves on.
  const onDashboard = $derived(
    ctx.settings.asked && preference === 'dashboard',
  );

  const fleet = $derived(ctx.fleet);

  $effect(() => {
    void fleet.load();
  });

  // The app asks at sign-in. Asking here as well joins that request, and lets
  // the page stand on its own.
  $effect(() => {
    if (!ctx.settings.asked) void ctx.settings.read();
  });

  // The recorded printer counts only while the fleet still holds it.
  const remembered = $derived(
    fleet.devices.find((device) => device.id === ctx.preferences.printer)?.id,
  );

  const only = $derived(
    fleet.devices.length === 1 ? fleet.devices[0]?.id : undefined,
  );

  const wanted = $derived.by(() => {
    if (!ctx.settings.asked || preference === 'dashboard') return undefined;
    // A fleet that was never read leaves nothing to choose from, and the
    // printers list is the page that says why. A later error on a fleet
    // that loaded, such as a stream that ended, still leaves the choice.
    if (!fleet.loaded) {
      return fleet.error === undefined ? undefined : PATHS.printers;
    }

    if (preference === 'all-printers') return PATHS.printers;

    const one = remembered ?? only;
    if (preference === 'my-printer' && one !== undefined) {
      return printerHref(one);
    }
    // Automatic opens on a printer only when there is no other to choose.
    if (preference === 'auto' && only !== undefined) return printerHref(only);

    return PATHS.printers;
  });

  // The root takes no history entry, so the back button skips it.
  $effect(() => {
    if (wanted !== undefined) ctx.go(wanted, { replace: true });
  });
</script>

{#if onDashboard}
  <DashboardPage {ctx} />
{/if}
