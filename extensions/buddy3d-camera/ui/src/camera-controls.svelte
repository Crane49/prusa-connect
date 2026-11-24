<!--
  The Control tab of a Buddy3D camera: the turn pad, the infrared mode and
  the video quality, each only on a camera that said it has it.
-->
<script lang="ts">
  import { Button } from '@prusa/connect-ui/primitives';

  import type { Direction, PairedCamera, Quality } from './api';
  import Choice from './choice.svelte';
  import { held } from './held';

  let { paired, controls }: { paired: PairedCamera; controls: boolean } = $props();

  const { store, t } = held();

  const has = (feature: string): boolean => paired.features?.includes(feature) === true;

  /** The steps the pad turns by, in degrees. */
  const STEPS = [1, 5, 10, 15, 30, 45, 90];
  let step = $state('10');
  const steps = STEPS.map((degrees) => ({ value: String(degrees), label: t('buddy3d.degrees', { degrees }) }));

  const pad: readonly { direction: Direction; key: string; glyph: string; place: string }[] = [
    { direction: 'up', key: 'buddy3d.turnUp', glyph: '↑', place: 'col-start-2 row-start-1' },
    { direction: 'left', key: 'buddy3d.turnLeft', glyph: '←', place: 'col-start-1 row-start-2' },
    { direction: 'right', key: 'buddy3d.turnRight', glyph: '→', place: 'col-start-3 row-start-2' },
    { direction: 'down', key: 'buddy3d.turnDown', glyph: '↓', place: 'col-start-2 row-start-3' },
  ];
  const turns = $derived(
    pad.filter(({ direction }) =>
      direction === 'left' || direction === 'right' ? has('RotationZ') : has('RotationX'),
    ),
  );
  const turn = (direction: Direction): void => {
    void store.control(paired.name, { move: { direction, angle: Number(step) } });
  };
  const onkeydown = (event: KeyboardEvent): void => {
    const keys: Partial<Record<string, Direction>> = {
      ArrowUp: 'up',
      ArrowDown: 'down',
      ArrowLeft: 'left',
      ArrowRight: 'right',
    };
    const direction = keys[event.key];
    const allowed = turns.find((option) => option.direction === direction);
    if (allowed === undefined || !controls || store.busy) return;
    event.preventDefault();
    turn(allowed.direction);
  };

  const modes = [
    { value: 'auto', label: t('buddy3d.modeAuto') },
    { value: 'day', label: t('buddy3d.modeDay') },
    { value: 'night', label: t('buddy3d.modeNight') },
  ] as const;
  const qualities: readonly { value: Quality; label: string }[] = [
    { value: 'sd', label: t('buddy3d.qualitySD') },
    { value: 'hd', label: t('buddy3d.qualityHD') },
    { value: 'fhd', label: t('buddy3d.qualityFHD') },
  ];
</script>

<div class="flex flex-wrap gap-8" data-part="controls">
  {#if turns.length > 0}
    <section class="flex flex-col gap-2" aria-label={t('buddy3d.turn')}>
      <h4 class="font-medium">{t('buddy3d.turn')}</h4>
      <!-- An arrow key on a focused button turns the camera too. -->
      <div class="grid w-fit grid-cols-3 grid-rows-3 gap-1" role="group">
        {#each turns as option (option.direction)}
          <Button
            size="sm"
            variant="secondary"
            class={option.place}
            disabled={!controls || store.busy}
            aria-label={t(option.key)}
            title={t(option.key)}
            {onkeydown}
            onclick={() => {
              turn(option.direction);
            }}
          >
            {option.glyph}
          </Button>
        {/each}
      </div>
      <dl class="grid max-w-md grid-cols-2 gap-x-4 gap-y-1">
        <Choice
          label={t('buddy3d.step')}
          value={step}
          items={steps}
          disabled={!controls}
          onchange={(next: string) => (step = next)}
        />
      </dl>
    </section>
  {/if}

  {#if has('IrMode') || has('VideoQuality')}
    <dl class="grid h-fit max-w-md grid-cols-2 gap-x-4 gap-y-2">
      {#if has('IrMode')}
        <Choice
          label={t('buddy3d.mode')}
          value={paired.status?.mode ?? ''}
          items={modes}
          disabled={!controls || store.busy}
          onchange={(next) => {
            const mode = modes.find((option) => option.value === next);
            if (mode !== undefined) void store.control(paired.name, { mode: mode.value });
          }}
        />
      {/if}
      {#if has('VideoQuality')}
        <Choice
          label={t('buddy3d.quality')}
          value={paired.status?.video_quality ?? ''}
          items={qualities}
          disabled={!controls || store.busy}
          onchange={(next) => {
            const quality = qualities.find((option) => option.value === next);
            if (quality !== undefined) void store.control(paired.name, { video_quality: quality.value });
          }}
        />
      {/if}
    </dl>
  {/if}

  {#if turns.length === 0 && !has('IrMode') && !has('VideoQuality')}
    <p class="text-muted-foreground">{t('buddy3d.noControls')}</p>
  {/if}
</div>
