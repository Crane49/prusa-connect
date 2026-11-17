<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import {
    fakeSettings,
    listingOf,
    NOT_WRITABLE,
    picturesOf,
    PRINTER,
    PRINTER_NAME,
    PRINTERS,
    pushUrlOf,
  } from './cameras.fixtures';
  import PrinterCamerasPanel from './printer-cameras-panel.svelte';

  const { Story } = defineMeta({
    title: 'Screens/Printer Detail/Cameras',
    component: PrinterCamerasPanel,
  });
</script>

<!--
  The Cameras tab of a printer's page. A module's section is a snippet the
  page hands in, so the stories draw a plain line where it goes.
-->

{#snippet foot(camera: string)}
  <p class="text-muted-foreground">A module's section for {camera}</p>
{/snippet}

<Story name="For A Person Who May Manage Cameras" asChild>
  <PrinterCamerasPanel
    deviceId={PRINTER}
    printerName={PRINTER_NAME}
    cameras={listingOf()}
    loaded
    {picturesOf}
    settings={fakeSettings()}
    printers={PRINTERS}
    {pushUrlOf}
    {foot}
  />
</Story>

<Story name="For A Person Who May Only Watch" asChild>
  <PrinterCamerasPanel
    deviceId={PRINTER}
    printerName={PRINTER_NAME}
    cameras={listingOf()}
    loaded
    {picturesOf}
    settings={fakeSettings(undefined, { allowed: false })}
    printers={PRINTERS}
    {pushUrlOf}
  />
</Story>

<Story name="Empty" asChild>
  <PrinterCamerasPanel
    deviceId={PRINTER}
    printerName={PRINTER_NAME}
    cameras={[]}
    loaded
    {picturesOf}
    settings={fakeSettings([])}
    printers={PRINTERS}
    {pushUrlOf}
  />
</Story>

<Story name="Loading" asChild>
  <PrinterCamerasPanel
    deviceId={PRINTER}
    printerName={PRINTER_NAME}
    cameras={[]}
    loaded={false}
    {picturesOf}
    settings={fakeSettings([], { loaded: false })}
    printers={PRINTERS}
    {pushUrlOf}
  />
</Story>

<Story name="A Change Is In Flight" asChild>
  <PrinterCamerasPanel
    deviceId={PRINTER}
    printerName={PRINTER_NAME}
    cameras={listingOf()}
    loaded
    {picturesOf}
    settings={fakeSettings(undefined, { pending: 'save' })}
    printers={PRINTERS}
    {pushUrlOf}
  />
</Story>

<Story name="Failed" asChild>
  <PrinterCamerasPanel
    deviceId={PRINTER}
    printerName={PRINTER_NAME}
    cameras={listingOf()}
    loaded
    {picturesOf}
    settings={fakeSettings(undefined, {
      error: NOT_WRITABLE,
      failed: 'remove',
    })}
    printers={PRINTERS}
    {pushUrlOf}
  />
</Story>
