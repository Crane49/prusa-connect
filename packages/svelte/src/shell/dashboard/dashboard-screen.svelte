<!--
  The dashboards, on the home page and at their own address (ADR-20261008-sww rules
  10 to 14).

  The application draws this screen once per address and keys it by the id,
  so the first read is here, in onMount, and never in an `$effect`
  (svelte-5.md). An address with an id draws that dashboard. The home page
  draws the default dashboard, else the first one the person sees, else a
  dashboard written from the fleet, which is stored only when a person edits
  it.

  Each state is drawn as itself (ui-integration.md). Nothing read yet is a
  skeleton the shape of the page. A list that could not be read on the home
  page is drawn by its code, with a retry. A dashboard that is gone, such as
  one a colleague deleted while its link was kept, says so and offers the
  home page.

  With `kiosk` the same dashboard is drawn over the whole screen, with
  nothing that changes it (kiosk-frame.svelte). A wall has nobody to press
  a retry, so the frame asks the screen to read again while there is nothing
  to show and the dashboard is not gone.
-->
<script lang="ts">
  import { onMount } from 'svelte';

  import { dashboardForFleet } from '../../domain';
  import { describeFault, t } from '../../i18n';
  import {
    Button,
    EmptyState,
    FaultAlert,
    NoLongerHere,
    TooltipProvider,
  } from '../../primitives';
  import { type DashboardContext, setDashboardContext } from './context';
  import DashboardView from './dashboard-view.svelte';
  import FirstPrinter from './first-printer.svelte';
  import type { DashboardAct } from './editor/acts';
  import BoardDialog from './editor/board-dialog.svelte';
  import BoardGrid from './grid/board-grid.svelte';
  import KioskFrame from './kiosk-frame.svelte';
  import PageSkeleton from './page-skeleton.svelte';
  import { type ShownDashboard, shownOf } from './shown';

  let {
    context,
    id,
    kiosk = false,
    edit = false,
  }: {
    context: DashboardContext;
    /** The dashboard the address names, or undefined for the home page. */
    id?: number | undefined;
    /** Draws the dashboard over the whole screen. */
    kiosk?: boolean;
    /** Starts the editor on the dashboard once it is read, for one just made. */
    edit?: boolean;
  } = $props();

  // The context is set once, as the screen is made: a context is not
  // reactive, and the application hands one set of stores for its life.
  // svelte-ignore state_referenced_locally
  setDashboardContext(context);
  // svelte-ignore state_referenced_locally
  const { dashboards, editor, printers, cameras, cards, navigation } = context;
  // svelte-ignore state_referenced_locally
  const asked = id;
  // svelte-ignore state_referenced_locally
  const editAsked = edit;

  let act = $state<DashboardAct | undefined>(undefined);
  /** True once the home page chose and there was nothing to choose. */
  let fromFleet = $state(false);

  /** The dashboard the home page opens: the default, else the first. */
  const chosen = (): number | undefined =>
    dashboards.rows.some(
      (row: { id: number }) => row.id === dashboards.defaultId,
    )
      ? (dashboards.defaultId ?? undefined)
      : dashboards.rows[0]?.id;

  /**
   * Starts the editor on a dashboard just made, when the person may change
   * it, and takes `edit` off the address, so a reload only looks.
   */
  const startEditing = async (
    dashboardId: number,
    framing: Promise<void>,
  ): Promise<void> => {
    await framing;
    const read = dashboards.dashboard;
    if (dashboards.rights.write && read?.id === dashboardId)
      editor.start(shownOf(read));
    navigation.open(dashboardId, { replace: true });
  };

  const follow = async (): Promise<void> => {
    if (asked !== undefined) {
      const framing = dashboards.frame();
      await dashboards.open(asked);
      if (editAsked) await startEditing(asked, framing);

      return;
    }

    await dashboards.frame();
    if (!dashboards.listed) return;
    const pick = chosen();
    if (pick === undefined) {
      dashboards.close();
      fromFleet = true;

      return;
    }
    fromFleet = false;
    await dashboards.open(pick);
  };

  onMount(() => {
    void follow();
    // The modules' listing names every card they declare, so the gallery and
    // the grid know a module's card before its bundle loads. It is read once
    // for the page, and it loads no bundle.
    void cards.discover();

    return () => {
      editor.stop();
    };
  });

  const stored = $derived(
    dashboards.dashboard !== undefined &&
      dashboards.dashboard.id === dashboards.dashboardId
      ? dashboards.dashboard
      : undefined,
  );

  // The dashboard the fleet writes follows the fleet, so a printer that
  // joins after the page drew is on it too.
  const written = $derived(
    fromFleet && dashboards.dashboard === undefined
      ? dashboardForFleet(
          printers.views.map((view) => view.id),
          (printer) => cameras.of(printer)?.name,
          t('dashboard.fleet.name'),
        )
      : undefined,
  );

  const shown = $derived.by((): ShownDashboard | undefined => {
    if (stored !== undefined) return shownOf(stored);

    return written === undefined
      ? undefined
      : { ...written, id: undefined, revision: 0 };
  });

  const listFault = $derived(
    dashboards.listFault === undefined
      ? undefined
      : describeFault(dashboards.listFault),
  );
  const fault = $derived(
    dashboards.fault === undefined
      ? undefined
      : describeFault(dashboards.fault),
  );
  const gone = $derived(fault?.code === 'core.resource/DASHBOARD_NOT_FOUND');
  // What a person may do decides which words the page says, so the page
  // waits for that answer too rather than saying a reader's sentence to a
  // person who may write. The fleet decides what a dashboard written from it
  // holds, so that one waits for the fleet.
  // A server with no printer yet is one somebody just set up, and adding a
  // printer is what they came for.
  const noPrinter = $derived(printers.loaded && printers.views.length === 0);

  const ready = $derived(
    dashboards.rightsRead &&
      (stored !== undefined || (written !== undefined && printers.loaded)),
  );
</script>

{#snippet goneNote(level: 'h1' | 'h2')}
  <NoLongerHere
    headingLevel={level}
    heading={t('dashboard.gone.heading')}
    description={t('dashboard.gone.note')}
    linkLabel={t('dashboard.gone.home')}
    onLink={() => {
      navigation.home();
    }}
  />
{/snippet}

<!-- The cards' controls carry tooltips, and a tooltip reads its delays from
     a provider. The screen brings its own, so it does not rely on the frame
     it is drawn in. -->
<TooltipProvider>
  {#if kiosk}
    <KioskFrame
      title={shown?.name ?? ''}
      retry={() => {
        if (shown === undefined && !gone) void follow();
      }}
    >
      {#if shown !== undefined && ready}
        <BoardGrid
          tiles={shown.cards}
          columns={shown.columns}
          density={shown.density}
          editing={false}
          fit
        />
      {:else if gone}
        {@render goneNote('h2')}
      {:else if fault !== undefined || listFault !== undefined}
        <FaultAlert fault={fault ?? listFault} />
      {:else}
        <PageSkeleton />
      {/if}
    </KioskFrame>
  {:else}
    <section
      class="@container/dashboard grid gap-y-4"
      data-part="dashboard-screen"
    >
      {#if noPrinter && navigation.addPrinter !== undefined}
        <FirstPrinter
          onadd={() => {
            navigation.addPrinter?.();
          }}
        />
      {/if}
      {#if gone}
        {@render goneNote('h1')}
      {:else if fault !== undefined}
        <FaultAlert {fault} />
      {:else if asked === undefined && !dashboards.listed && listFault !== undefined}
        <FaultAlert fault={listFault} />
        <EmptyState
          headingLevel="h1"
          heading={t('dashboard.unreadable.heading')}
          description={t('dashboard.unreadable.note')}
        >
          {#snippet action()}
            <Button
              variant="secondary"
              data-part="frame-retry"
              onclick={() => void follow()}
            >
              {t('dashboard.unreadable.retry')}
            </Button>
          {/snippet}
        </EmptyState>
      {:else if shown === undefined || !ready}
        <PageSkeleton />
      {:else}
        <DashboardView
          {shown}
          onAct={(next: DashboardAct) => {
            act = next;
          }}
        />
      {/if}
    </section>
  {/if}

  <BoardDialog
    {act}
    onClose={() => {
      act = undefined;
    }}
  />
</TooltipProvider>
