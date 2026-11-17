<script lang="ts">
  import { formatNumber } from '@prusa/connect-ui/i18n';

  import { heldHost } from '../host';
  import {
    celsius,
    LOADED_MATERIAL,
    NOZZLE_DIAMETER,
    numberAt,
    nozzleOf,
    stringAt,
    targetTextOf,
  } from '../nozzle';

  interface Props {
    /** The printer this line is about. It is all a device-card line is handed (ADR-20260911-4p6 rule 10). */
    deviceId: string;
  }

  let { deviceId }: Props = $props();

  // Every text this module draws comes from its own catalogue, through the
  // `t` the host bound to this module's namespace (frontend-i18n.md rule 7).
  const host = heldHost();
  const fleet = host.fleet;
  const t = host.t;

  const device = $derived(fleet.device(deviceId));

  // The nozzle's diameter and the material loaded in the head, beside its
  // temperature. Neither path is reported by every driver, so a fleet may
  // fall to the reading below.
  const diameter = $derived(numberAt(device, NOZZLE_DIAMETER));
  const material = $derived(stringAt(device, LOADED_MATERIAL));

  // The same two readings `nozzle-section` shows, on one line: a driver that
  // names neither the diameter nor the material still leaves a fact worth
  // showing.
  const reading = $derived(nozzleOf(device));
  const nowText = $derived(
    reading.current === undefined
      ? t('nozzle.state.notReported')
      : celsius(reading.current, 1),
  );
  const targetText = $derived(targetTextOf(reading.target, t));

  const text = $derived(
    diameter === undefined || material === undefined
      ? t('nozzle.line.reading', { current: nowText, target: targetText })
      : t('nozzle.line', {
          diameter: formatNumber(diameter, {
            style: 'unit',
            unit: 'millimeter',
            unitDisplay: 'short',
            maximumFractionDigits: 1,
          }),
          material,
        }),
  );
</script>

{#if device === undefined}
  <span>{t('nozzle.absent')}</span>
{:else}
  <span>{text}</span>
{/if}
