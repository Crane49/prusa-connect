<!--
  The Settings tab of a Buddy3D camera. The module keeps how often it sends a
  picture and which stream it runs, and tells the camera each time it
  connects. The camera keeps its timelapse and its speaker volume itself, so
  those change only while it is connected.
-->
<script lang="ts">
  import { CopyField, Label, SliderField, Switch } from '@prusa/connect-ui/primitives';

  import type { PairedCamera, Stream } from './api';
  import Choice from './choice.svelte';
  import { held } from './held';

  let { paired, controls }: { paired: PairedCamera; controls: boolean } = $props();

  const { store, t } = held();

  const has = (feature: string): boolean => paired.features?.includes(feature) === true;
  const mayChange = $derived(store.mayOperate(paired.name) && !store.busy);
  const live = $derived(controls && !store.busy);

  /** The intervals offered, in seconds: 0 is the module's default, 255 none. */
  const INTERVALS = [0, 10, 30, 60, 120, 240, 255];
  const intervalLabel = (seconds: number): string => {
    if (seconds === 0)
      return t('buddy3d.intervalDefault', { seconds: store.status?.snapshot_interval ?? 0 });
    if (seconds === 255) return t('buddy3d.intervalOff');

    return t('buddy3d.seconds', { seconds });
  };
  const chosen = $derived(paired.settings.snapshot_interval);
  // A value set elsewhere, such as through the API, is offered too.
  const intervals = $derived(
    (INTERVALS.includes(chosen) ? INTERVALS : [...INTERVALS, chosen].sort((a, b) => a - b)).map(
      (seconds) => ({ value: String(seconds), label: intervalLabel(seconds) }),
    ),
  );

  const stream = $derived<Stream>(paired.settings.stream ?? 'webrtc');
  const streams = $derived<readonly { value: Stream; label: string }[]>([
    { value: 'webrtc', label: t('buddy3d.streamWebRTC') },
    ...(has('RtspStream') || stream === 'rtsp' ? [{ value: 'rtsp' as const, label: t('buddy3d.streamRTSP') }] : []),
    { value: 'off', label: t('buddy3d.streamOff') },
  ]);
  const rtspURL = $derived(
    paired.status?.rtsp ?? (paired.status?.address ? `rtsp://${paired.status.address}/live` : ''),
  );

  const TIMELAPSE_INTERVALS = [5, 10, 15, 30, 60, 300];
  const timelapseIntervals = $derived(
    [...new Set([...TIMELAPSE_INTERVALS, paired.status?.timelapse_interval ?? 5])]
      .sort((a, b) => a - b)
      .map((seconds) => ({ value: String(seconds), label: t('buddy3d.seconds', { seconds }) })),
  );
</script>

<div class="flex flex-col gap-4" data-part="settings">
  <dl class="grid max-w-md grid-cols-2 gap-x-4 gap-y-2">
    <Choice
      label={t('buddy3d.interval')}
      value={String(chosen)}
      items={intervals}
      disabled={!mayChange}
      onchange={(next) => void store.saveSettings(paired.name, { snapshot_interval: Number(next) })}
    />
    <Choice
      label={t('buddy3d.stream')}
      value={stream}
      items={streams}
      disabled={!mayChange}
      onchange={(next) => {
        const picked = streams.find((option) => option.value === next);
        if (picked !== undefined) void store.saveSettings(paired.name, { stream: picked.value });
      }}
    />
  </dl>
  {#if stream === 'webrtc'}
    <p class="text-muted-foreground">{t('buddy3d.streamWebRTCSaid')}</p>
  {:else if stream === 'rtsp'}
    <p class="text-muted-foreground">{t('buddy3d.streamRTSPSaid')}</p>
    {#if rtspURL !== ''}
      <CopyField class="max-w-md" label={t('buddy3d.rtspURL')} value={rtspURL} />
    {/if}
  {:else}
    <p class="text-muted-foreground">{t('buddy3d.streamOffSaid')}</p>
  {/if}

  {#if has('TimelapseEn')}
    <section class="flex flex-col gap-2" data-part="timelapse">
      <Label class="flex w-fit items-center gap-3 font-normal">
        <Switch
          checked={paired.status?.timelapse === true}
          disabled={!live}
          onCheckedChange={(on: boolean) => void store.control(paired.name, { timelapse: on })}
        />
        {t('buddy3d.timelapse')}
      </Label>
      {#if paired.status?.timelapse && has('TimelapseInterval')}
        <dl class="grid max-w-md grid-cols-2 gap-x-4 gap-y-2">
          <Choice
            label={t('buddy3d.timelapseInterval')}
            value={String(paired.status.timelapse_interval ?? '')}
            items={timelapseIntervals}
            disabled={!live}
            onchange={(next) => void store.control(paired.name, { timelapse_interval: Number(next) })}
          />
        </dl>
      {/if}
    </section>
  {/if}

  {#if has('SpeakerVolume')}
    <section class="flex flex-col gap-2">
      <h4 class="font-medium">{t('buddy3d.volume')}</h4>
      <SliderField
        class="max-w-md"
        label={t('buddy3d.volume')}
        value={paired.status?.volume ?? 50}
        min={5}
        max={100}
        step={5}
        unit="%"
        disabled={!live}
        onCommit={(volume: number) => void store.control(paired.name, { volume })}
      />
    </section>
  {/if}
</div>
