<!--
  The time of day, large, with the date under it when the settings ask for it.
  It reads the clock the person chose in the app, 24-hour or 12-hour, through
  the library's formatter, and grows with its tile so a wall reads it from
  across the room.
-->
<script lang="ts">
  import { formatDate } from '../../../i18n';
  import { TilePanel } from '../../../primitives';
  import { dashboardContext } from '../context';
  import { clockSettings } from './config';
  import type { CardProps } from '../../../slots';

  let { config, title }: CardProps = $props();

  const { clock } = dashboardContext();

  const settings = $derived(clockSettings(config));
  // A clock without seconds reads the minute, so it redraws once a minute
  // and not on every tick of the page's clock.
  const at = $derived(settings.seconds ? clock.now : clock.minute);
  const time = $derived(
    formatDate(at, {
      timeStyle: settings.seconds ? 'medium' : 'short',
    }),
  );
  const date = $derived(
    formatDate(clock.minute, {
      weekday: 'long',
      day: 'numeric',
      month: 'long',
    }),
  );
  const stamp = $derived(new Date(at).toISOString());

  $effect(() => clock.hold());
</script>

<TilePanel {title}>
  <div
    data-part="clock"
    class="clock-box flex min-h-0 flex-1 flex-col items-center justify-center"
  >
    <time class="clock-time font-semibold tabular-nums" datetime={stamp}>
      {time}
    </time>
    {#if settings.date}
      <span class="text-sm text-muted-foreground">{date}</span>
    {/if}
  </div>
</TilePanel>

<style>
  /* The box the time measures its tile by, in both directions. */
  .clock-box {
    container-type: size;
  }

  /* As large as the tile allows, so a clock across a wall reads from the far
     side of the room, and a short clock stays inside its tile. */
  .clock-time {
    font-size: clamp(
      calc(var(--text-2xl) * 0.75),
      min(22cqi, 55cqb),
      calc(var(--text-2xl) * 4)
    );
    line-height: 1;
  }
</style>
