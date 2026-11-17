<script lang="ts">
  import type { Snippet } from 'svelte';

  import { formatUnit, t } from '../../../i18n';
  import { Button, SliderField } from '../../../primitives';
  import {
    blocked,
    READING_TILE,
    SECTION_TITLE,
    TUNING_MAX_PERCENT,
    TUNING_MIN_PERCENT,
    TUNING_REFERENCE_PERCENT,
  } from './machine-controls';
  import type { CommandProjection } from '../../../domain';

  let {
    label,
    icon,
    percent,
    projection,
    busy,
    onCommit,
  }: {
    /** `Speed` or `Flow`. Also the accessible name of the track. */
    label: string;
    icon: Snippet;
    percent: number | undefined;
    projection: CommandProjection;
    busy: boolean;
    onCommit: (next: number) => void;
  } = $props();

  const disabled = $derived(blocked(projection, busy));
  const tuned = $derived(
    percent !== undefined && percent !== TUNING_REFERENCE_PERCENT,
  );

  // How it stands against the reference.
  const deviation = $derived.by(() => {
    if (percent === undefined) return t('printerDetailControls.noReading');
    if (percent === TUNING_REFERENCE_PERCENT) {
      return t('printerDetailControls.tuning.asSliced');
    }

    return formatUnit(percent - TUNING_REFERENCE_PERCENT, '%', {
      signDisplay: 'exceptZero',
    });
  });
</script>

<div class="flex flex-col gap-2 {READING_TILE}">
  <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
    <h3 class={SECTION_TITLE}>
      {@render icon()}
      {label}
    </h3>
    <span class="ms-auto text-xs text-muted-foreground">{deviation}</span>
  </div>

  <SliderField
    value={percent ?? TUNING_REFERENCE_PERCENT}
    {onCommit}
    min={TUNING_MIN_PERCENT}
    max={TUNING_MAX_PERCENT}
    unit="%"
    label={t('printerDetailControls.tuning.override.label', { label })}
    {disabled}
  />

  {#if tuned}
    <!-- Dragging a track back to exactly its reference is the one value. -->
    <Button
      variant="ghost"
      size="sm"
      class="self-start"
      {disabled}
      onclick={() => onCommit(TUNING_REFERENCE_PERCENT)}
    >
      {t('printerDetailControls.tuning.reset')}
    </Button>
  {/if}
</div>
