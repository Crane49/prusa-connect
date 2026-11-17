<!--
  The dashboards: one at its own address, or the one the home page shows
  (ADR-20261008-sww rules 10 to 14). The screen's code is a chunk of its own, loaded
  when a dashboard is first drawn, so the other pages never load it.

  A chunk that does not load is most often one a newer build of the server
  replaced after this page opened. The page then says so and offers a reload,
  which fetches the new build.
-->
<script lang="ts">
  import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
  import { translateFor } from '@prusa/connect-ui/i18n';
  import { Alert, Button, EmptyState } from '@prusa/connect-ui/primitives';

  import { APP_NAMESPACE } from '../i18n';
  import type { AppContext } from '../screens';
  import DashboardFrame from './dashboard-frame.svelte';
  import NotFound from './not-found.svelte';

  const t = translateFor(APP_NAMESPACE);

  const loadChunk = () => import('@prusa/connect-ui/dashboard');

  let {
    ctx,
    load = loadChunk,
  }: {
    ctx: AppContext;
    /** Loads the library's dashboard chunk. A test hands one that fails. */
    load?: typeof loadChunk;
  } = $props();

  // svelte-ignore state_referenced_locally
  const chunk = load();

  /** The id the address names, or undefined for the dashboard the home page shows. */
  const id = $derived.by(() => {
    const raw = ctx.params.id;
    if (raw === undefined) return undefined;
    const parsed = Number(raw);

    return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : Number.NaN;
  });
  const query = $derived(new URLSearchParams(ctx.search));
  const kiosk = $derived(query.get('kiosk') === '1');
  // A dashboard just made opens with the editor started (ADR-20261008-sww).
  const edit = $derived(query.get('edit') === '1');

  $effect(() => {
    void ctx.fleet.load();
  });
</script>

<!-- An id that is not one is an address this product does not answer, and
     neither is a dashboard on a server that leaves dashboards out. -->
{#if Number.isNaN(id) || !ctx.settings.dashboards}
  <NotFound {ctx} />
{:else}
  {#await chunk then lib}
    <DashboardFrame {ctx} {lib} {id} {kiosk} {edit} />
  {:catch}
    <div data-part="dashboard-unloaded">
      <Alert variant="destructive">
        <EmptyState
          class="col-start-2"
          icon={RefreshCwIcon}
          headingLevel="h1"
          heading={t('app.dashboard.unloaded.heading')}
          description={t('app.dashboard.unloaded.note')}
        >
          {#snippet action()}
            <Button
              onclick={() => {
                window.location.reload();
              }}
            >
              {t('app.dashboard.unloaded.reload')}
            </Button>
          {/snippet}
        </EmptyState>
      </Alert>
    </div>
  {/await}
{/if}
