<!--
  One printer, the size of the tile a person gave it. What it draws before
  there is a printer to draw is one-printer.svelte's.
-->
<script lang="ts">
  import { PrinterGlyphIcon, TilePanel } from '../../../primitives';
  import { onMount } from 'svelte';

  import type { PrinterView } from '../printers';
  import { t } from '../../../i18n';
  import { dashboardContext } from '../context';
  import { printerSettings } from './config';
  import OnePrinter from './one-printer.svelte';
  import PrinterFace from './printer-face.svelte';
  import type { CardProps } from '../../../slots';

  let { config, title }: CardProps = $props();

  const { cameras } = dashboardContext();

  const settings = $derived(printerSettings(config));
  // A word, so a tile replaced by a move or by a new read of the dashboard
  // does not let the frames go and take them again.
  const wantsFrame = $derived(settings.picture === 'camera');

  /** The newest still frame of a printer's camera, when it has one. */
  const frameOf = (id: string): string | undefined => {
    if (!wantsFrame) return undefined;
    const camera = cameras.of(id);

    return camera === undefined ? undefined : cameras.frame(camera);
  };

  // The list of cameras is read once, as the tile mounts. A tile switched to
  // a camera later finds it read by the settings form it was switched in.
  onMount(() => {
    if (wantsFrame) cameras.load();
  });

  // The frames tick only while a tile shows one.
  $effect(() => {
    if (!wantsFrame) return;

    return cameras.hold();
  });
</script>

<TilePanel {title}>
  <OnePrinter
    device={settings.device}
    icon={PrinterGlyphIcon}
    pickNote={t('dashboard.printer.pickNote')}
  >
    {#snippet children(view: PrinterView)}
      <PrinterFace
        {view}
        fields={settings.fields}
        picture={settings.picture}
        frame={frameOf(view.id)}
      />
    {/snippet}
  </OnePrinter>
</TilePanel>
