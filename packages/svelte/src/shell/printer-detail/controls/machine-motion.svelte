<script lang="ts">
  import { AXIS_X_PATH, AXIS_Y_PATH, AXIS_Z_PATH } from '../../../domain';
  import { formatNumber, formatUnit, t } from '../../../i18n';
  import {
    ArrowDownIcon,
    ArrowLeftIcon,
    ArrowRightIcon,
    ArrowUpIcon,
    Button,
    ConfirmDialog,
    DisableSteppersIcon,
    HomeIcon,
    RestartIcon,
    ToggleGroup,
    ToggleGroupItem,
    ZAxisIcon,
  } from '../../../primitives';
  import {
    blocked,
    groupReason,
    JOG_STEPS_MM,
    JOG_XY_FEEDRATE,
    JOG_Z_FEEDRATE,
    SECTION_TITLE,
    VERBS,
    type SectionProps,
  } from './machine-controls';

  let {
    resolve,
    reading,
    busy,
    onCommand,
    printing,
    bedOnZ,
  }: SectionProps & { printing: boolean; bedOnZ: boolean } = $props();

  const move = $derived(resolve(VERBS.move));
  const moveZ = $derived(resolve(VERBS.moveZ));
  const home = $derived(resolve(VERBS.home));
  const disableSteppers = $derived(resolve(VERBS.disableSteppers));
  const reset = $derived(resolve(VERBS.resetPrinter));
  const reason = $derived(
    groupReason([move, moveZ, home, disableSteppers, reset]),
  );

  const xyBlocked = $derived(blocked(move, busy));
  const zBlocked = $derived(blocked(moveZ, busy));

  let step = $state(1);

  const uid = $props.id();

  const millimetres = (value: number | undefined, digits: number) =>
    value === undefined
      ? undefined
      : formatUnit(value, 'mm', {
          minimumFractionDigits: digits,
          maximumFractionDigits: digits,
        });

  const xy = $derived.by(() => {
    if (xyBlocked) return undefined;
    const parts = [
      millimetres(reading(AXIS_X_PATH), 1),
      millimetres(reading(AXIS_Y_PATH), 1),
    ].filter((part) => part !== undefined);

    return parts.length === 0 ? undefined : parts.join(' · ');
  });
  const z = $derived(millimetres(reading(AXIS_Z_PATH), 2));

  const jogXy = (axis: 'x' | 'y', direction: 1 | -1) =>
    onCommand(VERBS.move, {
      feedrate: String(JOG_XY_FEEDRATE),
      [axis]: String(direction * step),
    });

  const jogZ = (direction: 1 | -1) =>
    onCommand(VERBS.moveZ, {
      feedrate: String(JOG_Z_FEEDRATE),
      distance: String(direction * step),
    });

  // The up arrow moves up what the reader sees move: the bed comes up as Z
  // falls, and the nozzle goes up as Z grows.
  const zUp = $derived(
    bedOnZ
      ? { label: t('printerDetailControls.jog.bedUp'), direction: -1 as const }
      : { label: t('printerDetailControls.jog.zUp'), direction: 1 as const },
  );
  const zDown = $derived(
    bedOnZ
      ? { label: t('printerDetailControls.jog.bedDown'), direction: 1 as const }
      : { label: t('printerDetailControls.jog.zDown'), direction: -1 as const },
  );
</script>

{#snippet axisLabel(id: string, label: string, value: string | undefined)}
  <span
    {id}
    class="flex items-baseline gap-1.5 text-xs font-medium text-muted-foreground"
  >
    {label}
    {#if value !== undefined}
      <span class="font-mono text-foreground tabular-nums">{value}</span>
    {/if}
  </span>
{/snippet}

<section class="flex flex-col gap-4 @2xl:col-start-1 @2xl:row-start-1">
  <h3 class={SECTION_TITLE}>
    <ZAxisIcon aria-hidden="true" class="size-4 text-muted-foreground" />
    {t('printerDetailControls.motion.heading')}
  </h3>

  {#if move.kind !== 'absent' || moveZ.kind !== 'absent'}
    <div class="flex flex-col gap-1">
      <span id="{uid}-step" class="text-xs font-medium text-muted-foreground">
        {t('printerDetailControls.jog.step')}
      </span>
      <ToggleGroup
        type="single"
        variant="outline"
        size="lg"
        value={String(step)}
        onValueChange={(next) => {
          if (next) step = Number(next);
        }}
        aria-labelledby="{uid}-step"
      >
        {#each JOG_STEPS_MM as millimetre (millimetre)}
          <!-- The cell carries the bare number and the unit rides on the. -->
          <ToggleGroupItem
            value={String(millimetre)}
            aria-label={formatUnit(millimetre, 'mm')}
          >
            {formatNumber(millimetre)}
          </ToggleGroupItem>
        {/each}
      </ToggleGroup>
    </div>

    <div class="flex flex-wrap items-start gap-3">
      {#if move.kind !== 'absent'}
        <div class="flex flex-col gap-1">
          {@render axisLabel(
            `${uid}-xy`,
            t('printerDetailControls.jog.xy.label'),
            xy,
          )}
          <div
            role="group"
            aria-labelledby="{uid}-xy"
            class="grid grid-cols-3 grid-rows-3 gap-1"
          >
            <div></div>
            <Button
              variant="secondary"
              size="icon-md"
              class="size-12"
              label={t('printerDetailControls.jog.yForward')}
              disabled={xyBlocked}
              onclick={() => jogXy('y', 1)}
            >
              <ArrowUpIcon aria-hidden="true" />
            </Button>
            <div></div>
            <Button
              variant="secondary"
              size="icon-md"
              class="size-12"
              label={t('printerDetailControls.jog.xLeft')}
              disabled={xyBlocked}
              onclick={() => jogXy('x', -1)}
            >
              <ArrowLeftIcon aria-hidden="true" />
            </Button>
            <div></div>
            <Button
              variant="secondary"
              size="icon-md"
              class="size-12"
              label={t('printerDetailControls.jog.xRight')}
              disabled={xyBlocked}
              onclick={() => jogXy('x', 1)}
            >
              <ArrowRightIcon aria-hidden="true" />
            </Button>
            <div></div>
            <Button
              variant="secondary"
              size="icon-md"
              class="size-12"
              label={t('printerDetailControls.jog.yBack')}
              disabled={xyBlocked}
              onclick={() => jogXy('y', -1)}
            >
              <ArrowDownIcon aria-hidden="true" />
            </Button>
            <div></div>
          </div>
        </div>
      {/if}

      {#if moveZ.kind !== 'absent'}
        <div class="flex flex-col gap-1">
          {@render axisLabel(
            `${uid}-z`,
            t('printerDetailControls.jog.z.label'),
            z,
          )}
          <div
            role="group"
            aria-labelledby="{uid}-z"
            class="flex flex-col gap-1"
          >
            <Button
              variant="secondary"
              size="icon-md"
              class="size-12"
              label={zUp.label}
              disabled={zBlocked}
              onclick={() => jogZ(zUp.direction)}
            >
              <ArrowUpIcon aria-hidden="true" />
            </Button>
            <Button
              variant="secondary"
              size="icon-md"
              class="size-12"
              label={zDown.label}
              disabled={zBlocked}
              onclick={() => jogZ(zDown.direction)}
            >
              <ArrowDownIcon aria-hidden="true" />
            </Button>
          </div>
        </div>
      {/if}
    </div>
  {/if}

  <div class="flex flex-wrap gap-2">
    {#if home.kind !== 'absent'}
      <Button
        variant="secondary"
        size="lg"
        disabled={blocked(home, busy)}
        onclick={() => onCommand(VERBS.home)}
      >
        <HomeIcon aria-hidden="true" />
        {t('printerDetailControls.home.label')}
      </Button>
    {/if}
    {#if disableSteppers.kind !== 'absent'}
      <Button
        variant="secondary"
        size="lg"
        disabled={blocked(disableSteppers, busy)}
        onclick={() => onCommand(VERBS.disableSteppers)}
      >
        <DisableSteppersIcon aria-hidden="true" />
        {t('printerDetailControls.disableSteppers.label')}
      </Button>
    {/if}
    {#if reset.kind !== 'absent'}
      <ConfirmDialog
        trigger={resetTrigger}
        title={t('printerDetailControls.reset.title')}
        description={printing
          ? t('printerDetailControls.reset.printing')
          : t('printerDetailControls.reset.idle')}
        confirmLabel={t('printerDetailControls.reset.label')}
        onConfirm={() => onCommand(VERBS.resetPrinter)}
      />
    {/if}
  </div>

  {#if reason}
    <p class="text-xs text-muted-foreground">{reason}</p>
  {/if}
</section>

{#snippet resetTrigger({ props }: { props: Record<string, unknown> })}
  <Button
    {...props}
    variant="destructive"
    size="lg"
    disabled={blocked(reset, busy)}
  >
    <RestartIcon aria-hidden="true" />
    {t('printerDetailControls.reset.label')}
  </Button>
{/snippet}
