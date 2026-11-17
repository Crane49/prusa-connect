<!-- The commands with no reading of their own. -->
<script lang="ts">
  import { t } from '../../../i18n';
  import { Button, MeshBedLevelingIcon } from '../../../primitives';
  import {
    blocked,
    groupReason,
    SECTION_TITLE,
    VERBS,
    type SectionProps,
  } from './machine-controls';
  import MachineHostname from './machine-hostname.svelte';

  let {
    resolve,
    busy,
    onCommand,
    ready,
  }: Omit<SectionProps, 'reading'> & { ready: boolean } = $props();

  const readyVerb = $derived(
    ready ? VERBS.cancelPrinterReady : VERBS.setPrinterReady,
  );
  const readyProjection = $derived(resolve(readyVerb));
  const idle = $derived(resolve(VERBS.setIdle));
  const levelling = $derived(resolve(VERBS.meshBedLevelling));
  const beep = $derived(resolve(VERBS.beep));
  const hostname = $derived(resolve(VERBS.hostname));
  const systemDrawn = $derived(
    beep.kind !== 'absent' || hostname.kind !== 'absent',
  );

  const stateDrawn = $derived(
    readyProjection.kind !== 'absent' || idle.kind !== 'absent',
  );
  const drawn = $derived(
    stateDrawn || levelling.kind !== 'absent' || systemDrawn,
  );

  const plateReason = $derived(groupReason([readyProjection, idle, levelling]));
  const systemReason = $derived(groupReason([beep, hostname]));

  const uid = $props.id();
</script>

{#snippet subLabel(id: string, text: string)}
  <span {id} class="text-xs font-medium text-muted-foreground">{text}</span>
{/snippet}

{#if drawn}
  <section
    class="flex flex-col gap-3 border-t border-border pt-6 @2xl:col-start-1 @2xl:row-start-2"
  >
    <h3 class={SECTION_TITLE}>
      {t('printerDetailControls.utilities.heading')}
    </h3>

    {#if stateDrawn}
      <div class="flex flex-col gap-1">
        {@render subLabel(
          `${uid}-state`,
          t('printerDetailControls.utilities.state'),
        )}
        <div
          role="group"
          aria-labelledby="{uid}-state"
          class="flex flex-wrap gap-2"
        >
          {#if readyProjection.kind !== 'absent'}
            <Button
              variant="secondary"
              size="lg"
              disabled={blocked(readyProjection, busy)}
              onclick={() => onCommand(readyVerb)}
            >
              {ready
                ? t('printerDetailControls.unmarkReady')
                : t('printerDetailControls.markReady')}
            </Button>
          {/if}
          {#if idle.kind !== 'absent'}
            <Button
              variant="secondary"
              size="lg"
              disabled={blocked(idle, busy)}
              onclick={() => onCommand(VERBS.setIdle)}
            >
              {t('printerDetailControls.exitIdle')}
            </Button>
          {/if}
        </div>
      </div>
    {/if}

    {#if levelling.kind !== 'absent'}
      <div class="flex flex-col gap-1">
        {@render subLabel(
          `${uid}-calibration`,
          t('printerDetailControls.utilities.calibration'),
        )}
        <div
          role="group"
          aria-labelledby="{uid}-calibration"
          class="flex flex-wrap gap-2"
        >
          <Button
            variant="secondary"
            size="lg"
            disabled={blocked(levelling, busy)}
            onclick={() => onCommand(VERBS.meshBedLevelling)}
          >
            <MeshBedLevelingIcon aria-hidden="true" />
            {t('printerDetailControls.meshBedLevelling')}
          </Button>
        </div>
      </div>
    {/if}

    {#if plateReason}
      <p class="text-xs text-muted-foreground">{plateReason}</p>
    {/if}

    {#if systemDrawn}
      <div class="flex flex-col gap-1">
        {@render subLabel(
          `${uid}-system`,
          t('printerDetailControls.utilities.system'),
        )}
        <div
          role="group"
          aria-labelledby="{uid}-system"
          class="flex flex-wrap gap-2"
        >
          {#if beep.kind !== 'absent'}
            <Button
              variant="secondary"
              size="lg"
              disabled={blocked(beep, busy)}
              onclick={() => onCommand(VERBS.beep)}
            >
              {t('printerDetailControls.beep')}
            </Button>
          {/if}
          {#if hostname.kind !== 'absent'}
            <MachineHostname projection={hostname} {busy} {onCommand} />
          {/if}
        </div>
      </div>
    {/if}

    {#if systemReason}
      <p class="text-xs text-muted-foreground">{systemReason}</p>
    {/if}
  </section>
{/if}
