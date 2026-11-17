<!--
  A dashboard over the whole screen, for a screen left running on a wall.

  It is a modal dialog, which covers the app's own frame from the browser's
  top layer, and it draws nothing that changes the dashboard. The rows of the
  grid stretch so the dashboard fills the height of the screen.
  The pointer and the two buttons hide after a few quiet seconds and come
  back when the pointer moves or a key is pressed, so a wall shows the
  printers and nothing else. Hidden, the buttons are inert: a press only
  brings them back, and Tab does not land on a button nobody can see.
  Escape leaves.

  Three things keep a wall honest over hours:

    the screen stays on   the browser is asked to keep the screen awake while
                          this frame is up, and asked again whenever it lets
                          go, such as when the tab hides (screen-awake.ts)
    the dashboard is read again
                          when the core says it changed, and every ten
                          minutes besides, for a stream that dropped an
                          event (ADR-20261008-sww rule 31). A wall whose read
                          failed asks the screen to try again every half
                          minute and when the screen is shown again
    the printers          come from the app's fleet stream, as on every page
-->
<script lang="ts">
  import ExpandIcon from '@lucide/svelte/icons/expand';
  import ShrinkIcon from '@lucide/svelte/icons/shrink';
  import XIcon from '@lucide/svelte/icons/x';
  import type { Snippet } from 'svelte';
  import { onMount } from 'svelte';

  import { t } from '../../i18n';
  import { Button } from '../../primitives';
  import { dashboardContext } from './context';
  import { keepScreenAwake } from './screen-awake';

  let {
    title,
    retry,
    children,
  }: {
    title: string;
    /** Reads again what the wall could not read. The screen decides whether anything is missing. */
    retry: () => void;
    children: Snippet;
  } = $props();

  const { dashboards, navigation } = dashboardContext();

  /** How long the pointer and the buttons stay after the last movement. */
  const QUIET_MS = 3000;
  /** How often the dashboard is read again when no event said it changed. */
  const POLL_MS = 600_000;
  /** How often a wall asks again for what it could not read. */
  const RETRY_MS = 30_000;

  let frame = $state<HTMLElement | undefined>(undefined);
  let quiet = $state(false);
  let full = $state(false);

  const leave = (): void => {
    if (document.fullscreenElement !== null) void document.exitFullscreen();
    navigation.kiosk(false);
  };

  const toggleFull = async (): Promise<void> => {
    try {
      if (document.fullscreenElement === null) await frame?.requestFullscreen();
      else await document.exitFullscreen();
    } catch {
      // A browser that refuses the full screen, such as one in a frame that
      // does not allow it, keeps the frame over the window, which is the
      // same dashboard at the size of the window.
    }
  };

  onMount(() => {
    // A modal dialog is drawn in the browser's top layer, above the app's own
    // frame whatever its stacking.
    if (frame instanceof HTMLDialogElement && !frame.open) frame.showModal();

    let timer: ReturnType<typeof setTimeout> | undefined;
    const wake = (): void => {
      quiet = false;
      clearTimeout(timer);
      timer = setTimeout(() => {
        quiet = true;
      }, QUIET_MS);
    };
    wake();

    const fullChanged = (): void => {
      full = document.fullscreenElement !== null;
    };
    const shown = (): void => {
      if (document.visibilityState === 'visible') {
        retry();
        void dashboards.poll();
      }
    };
    const sleep = keepScreenAwake(
      'wakeLock' in navigator ? navigator.wakeLock : undefined,
      document,
    );

    const poll = setInterval(() => {
      void dashboards.poll();
    }, POLL_MS);
    const again = setInterval(() => {
      retry();
    }, RETRY_MS);

    window.addEventListener('pointermove', wake);
    window.addEventListener('pointerdown', wake);
    window.addEventListener('keydown', wake);
    document.addEventListener('fullscreenchange', fullChanged);
    document.addEventListener('visibilitychange', shown);

    return () => {
      clearTimeout(timer);
      clearInterval(poll);
      clearInterval(again);
      window.removeEventListener('pointermove', wake);
      window.removeEventListener('pointerdown', wake);
      window.removeEventListener('keydown', wake);
      document.removeEventListener('fullscreenchange', fullChanged);
      document.removeEventListener('visibilitychange', shown);
      sleep();
    };
  });
</script>

<!-- eslint-disable-next-line svelte/no-restricted-html-elements -- a native fullscreen dialog; Dialog and Sheet are modals -->
<dialog
  bind:this={frame}
  data-part="kiosk"
  data-quiet={quiet ? '' : undefined}
  aria-label={title}
  class="kiosk flex flex-col gap-4 overflow-y-auto bg-background p-4 text-foreground"
  oncancel={(event: Event) => {
    // Escape leaves the wall view rather than closing the dialog under it.
    event.preventDefault();
    leave();
  }}
>
  <header class="flex items-center justify-between gap-4">
    <h1 class="min-w-0 truncate text-2xl font-semibold">{title}</h1>
    <div
      inert={quiet}
      class={[
        'flex shrink-0 items-center gap-2 transition-opacity duration-300 motion-reduce:transition-none',
        quiet && 'opacity-0',
      ]}
    >
      <Button
        variant="secondary"
        size="icon-lg"
        label={full ? t('dashboard.kiosk.window') : t('dashboard.kiosk.full')}
        onclick={() => void toggleFull()}
      >
        {#if full}
          <ShrinkIcon aria-hidden="true" />
        {:else}
          <ExpandIcon aria-hidden="true" />
        {/if}
      </Button>
      <Button
        variant="secondary"
        size="icon-lg"
        label={t('dashboard.kiosk.leave')}
        data-part="kiosk-leave"
        onclick={leave}
      >
        <XIcon aria-hidden="true" />
      </Button>
    </div>
  </header>
  {@render children()}
</dialog>

<style>
  /* The whole screen, with none of the space and the frame a browser gives
     a dialog of its own. */
  .kiosk {
    inline-size: 100vw;
    block-size: 100dvh;
    max-inline-size: none;
    max-block-size: none;
    margin: 0;
    border: 0;
  }

  /* A wall shows no pointer once nobody moves it. */
  .kiosk[data-quiet] {
    cursor: none;
  }
</style>
