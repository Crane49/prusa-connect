<!--
  A warning that the catalogue is nearly out of room.

  The overview answers how much room the catalogue takes and the limit it is
  held to. Its own database names no limit, so nothing is drawn. Where a limit
  is named, nothing is drawn until the room is nearly used up, and past that
  the bar says how close the next refusal is. The files themselves are in a
  storage and are not counted here.
-->
<script lang="ts">
  import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';
  import { formatBytes } from '@prusa/connect-ui/i18n';
  import { Progress } from '@prusa/connect-ui/primitives';

  import { heldSession } from '../session';

  const { catalogue, host } = heldSession();
  const t = host.t;

  /** The share past which the room is running out and the warning is drawn. */
  const NEARLY_FULL = 0.85;

  const usage = $derived(catalogue.overview?.usage);
  const share = $derived(
    usage === undefined || usage.limit === 0 ? 0 : usage.bytes / usage.limit,
  );
</script>

{#if usage && share > NEARLY_FULL}
  <section
    class="grid gap-y-2 rounded-sm border border-border p-3"
    aria-labelledby="parts-usage"
    data-part="usage"
  >
    <h2
      id="parts-usage"
      class="flex items-center gap-2 text-sm font-medium text-warning-text"
    >
      <TriangleAlertIcon aria-hidden="true" class="size-4 shrink-0" />
      {t('parts.usage.heading')}
    </h2>
    <Progress
      value={Math.round(share * 100)}
      max={100}
      tone="muted"
      aria-label={t('parts.usage.heading')}
    />
    <p class="text-xs text-muted-foreground">
      {t('parts.usage.used', {
        used: formatBytes(usage.bytes),
        limit: formatBytes(usage.limit),
      })}
      {t('parts.usage.nearlyFull')}
    </p>
  </section>
{/if}
