<!--
  The parts catalogue, on one page.

  A page takes no props: the route is the whole context, and the place inside
  the catalogue rides in the query (navigation.svelte.ts). What it draws comes
  from the stores the module's setup built, and those reach the core through
  the client the host handed over.

  Two views of one page. With no part open it is the Storage screen's shape:
  the folder tree beside the listing, split at the one width where both fit.
  With a part open it is the file detail's shape: a trail up to the folder, the
  part's header, and its tabs.

  Each state is drawn as itself (ui-integration.md). Nothing read yet is a
  skeleton the shape of the page. A refusal of the frame is drawn by its code,
  which is how a person who may not read the catalogue learns so. A person who
  may read and not write sees every part and no control that changes one.
-->
<script lang="ts">
  import { EmptyState, FaultAlert } from '@prusa/connect-ui/primitives';
  import PackageIcon from '@lucide/svelte/icons/package';
  import { onMount } from 'svelte';

  import { faultText } from '../faults';
  import { byteLimits } from './byte-limits';
  import { heldSession } from '../session';
  import CatalogueView from './catalogue-view.svelte';
  import FaultNote from './fault-note.svelte';
  import PageSkeleton from './page-skeleton.svelte';
  import PartView from './part-view.svelte';

  const { catalogue, navigation, host } = heldSession();
  const t = host.t;

  const spot = $derived(navigation.spot);
  const frameFault = $derived(
    catalogue.overviewFault === undefined
      ? undefined
      : faultText(catalogue.overviewFault, t),
  );

  // The first read is here and never in an `$effect` (svelte-5.md). Every
  // later one follows a move the person made, in the handler that made it.
  onMount(() => {
    // Every move is followed: a link on this page, the back button, and the
    // rail entry the router took.
    const stop = navigation.onMove((next) => {
      void catalogue.follow(next);
      window.scrollTo({ top: 0 });
    });
    const detach = navigation.attach();
    void catalogue.follow(navigation.spot);

    return () => {
      stop();
      detach();
    };
  });
</script>

<section
  class="@container/parts grid gap-y-4"
  data-part="parts-page"
  {@attach byteLimits}
>
  {#if catalogue.overview === undefined && frameFault === undefined}
    <PageSkeleton />
  {:else if frameFault !== undefined && catalogue.overview === undefined}
    <FaultAlert fault={frameFault} />
    <EmptyState
      icon={PackageIcon}
      headingLevel="h1"
      heading={t('parts.unreadable.heading')}
      description={t('parts.unreadable.note')}
    />
  {:else}
    <FaultNote
      fault={catalogue.fault}
      onDismiss={() => {
        catalogue.dismiss();
      }}
    />
    {#if spot.part === undefined}
      <CatalogueView />
    {:else}
      <PartView />
    {/if}
  {/if}
</section>
