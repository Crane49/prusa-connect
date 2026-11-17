<!--
  What the core refused when a dashboard was written, by its code
  (frontend-i18n.md rule 5).

  A change somebody else saved first is the one refusal a person acts on from
  here, so it carries the button that reads the dashboard again. It draws
  nothing when nothing was refused, so the page can leave it in the markup.
-->
<script lang="ts">
  import { describeFault, t } from '../../i18n';
  import { Button, FaultAlert } from '../../primitives';

  let {
    fault,
    onDismiss,
    onReload,
  }: {
    fault: unknown;
    onDismiss?: (() => void) | undefined;
    /** Reads the dashboard again, dropping what was not saved. */
    onReload?: (() => void) | undefined;
  } = $props();

  /** The refusal a person answers by reading again (ADR-20261008-sww rule 4). */
  const CHANGED = 'core.resource/DASHBOARD_CHANGED';

  const shown = $derived(
    fault === undefined ? undefined : describeFault(fault),
  );
</script>

{#if shown}
  <div data-part="fault" aria-live="polite">
    <FaultAlert fault={shown}>
      {#snippet actions()}
        {#if shown.code === CHANGED && onReload}
          <Button
            size="sm"
            variant="secondary"
            data-part="fault-reload"
            onclick={onReload}
          >
            {t('dashboard.fault.reload')}
          </Button>
        {/if}
        {#if onDismiss}
          <Button size="sm" variant="ghost" onclick={onDismiss}>
            {t('dashboard.fault.dismiss')}
          </Button>
        {/if}
      {/snippet}
    </FaultAlert>
  </div>
{/if}
