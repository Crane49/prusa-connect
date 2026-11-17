<script lang="ts">
  import { heaterState, isHeating } from '@prusa/connect-ui/domain';
  import { Badge } from '@prusa/connect-ui/primitives';

  import { heldHost } from '../host';
  import { celsius, nozzleOf, targetTextOf } from '../nozzle';
  import { HEATER_VARIANT, HEATER_WORD_KEY } from './words';

  interface Props {
    /** The printer this card is about. It is all a section is handed (ADR-20260911-4p6 rule 10). */
    deviceId: string;
  }

  let { deviceId }: Props = $props();

  // The fleet is already on the host: the app reads it once and keeps it
  // current from the event stream, and a module receives it read-only
  // (ADR-20260911-4p6 rule 18). So this section makes no request of its own, holds no
  // store of its own, and redraws when a frame moves this printer's row.
  // Every text this module draws comes from its own catalogue, through the
  // `t` the host bound to this module's namespace (frontend-i18n.md rule 7).
  const host = heldHost();
  const fleet = host.fleet;
  const t = host.t;

  const device = $derived(fleet.device(deviceId));
  const reading = $derived(nozzleOf(device));
  const state = $derived(heaterState(reading));

  // The numbers are built here and never in the markup, because a cast or an
  // annotation is not valid in a template (svelte-5.md). One decimal is what
  // firmware reports and what a person can read at a glance.
  const nowText = $derived(
    reading.current === undefined ? '—' : celsius(reading.current, 1),
  );
  const targetText = $derived(targetTextOf(reading.target, t));

  // The one condition marked with motion: a nozzle still climbing. A reader
  // who has asked for less motion gets the badge and the number without it.
  const climbing = $derived(isHeating(reading));
</script>

{#if device === undefined}
  <p class="text-sm text-muted-foreground">{t('nozzle.absent')}</p>
{:else}
  <div class="flex flex-wrap items-baseline gap-x-3 gap-y-1">
    <!-- The reading changes under the reader, so the region announces itself
         rather than changing in silence (svelte-5.md). -->
    <span
      aria-live="polite"
      data-part="reading"
      class={[
        'text-lg font-medium tabular-nums',
        climbing && 'motion-safe:animate-pulse',
      ]}
    >
      {nowText}
    </span>

    <span data-part="target" class="text-sm text-muted-foreground">
      {targetText}
    </span>

    <Badge variant={HEATER_VARIANT[state]} data-part="state">
      {t(HEATER_WORD_KEY[state])}
    </Badge>
  </div>
{/if}
