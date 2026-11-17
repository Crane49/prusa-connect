<script lang="ts">
  import { deviceThumbnailUrlOf } from '@prusa/connect-ui/data';
  import { translateFor } from '@prusa/connect-ui/i18n';
  import { type JobRow, JobsScreen } from '@prusa/connect-ui/shell';

  import { client } from '../client';
  import { APP_NAMESPACE } from '../i18n';
  import { PATHS, printedHref } from '../routes';
  import type { AppContext } from '../screens';

  const t = translateFor(APP_NAMESPACE);

  let { ctx }: { ctx: AppContext } = $props();

  const fleet = $derived(ctx.fleet);
  const jobs = $derived(ctx.jobs);

  $effect(() => {
    void fleet.load();
    void ctx.deviceStates.read();
  });

  // The printer the address names, when the page lists one printer only.
  const printer = $derived(ctx.params.id);

  // The read waits for the printers, which name the rows.
  $effect(() => {
    if (!fleet.loaded) return;

    void jobs.load(printer);
  });

  // A print that starts or ends while the page is open is read at once.
  $effect(() => jobs.watch(fleet));

  const printerName = $derived(
    fleet.devices.find((device) => device.id === printer)?.name ?? printer,
  );

  // The dialog needs the moves of the kinds this server holds.
  const transitions = $derived(
    ctx.deviceStates.transitionsOf(fleet.devices.map((device) => device.kind)),
  );
</script>

<div class="flex flex-col gap-3">
  {#if printerName !== undefined}
    <!-- The line sits above the screen, which has no place for it. -->
    <p class="text-sm text-muted-foreground">
      {t('app.jobs.printer', { name: printerName })}
      <a
        href={PATHS.jobs}
        class="rounded-sm underline underline-offset-4 hover:text-link focus-visible:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background focus-visible:outline-none"
      >
        {t('app.jobs.everyPrinter')}
      </a>
    </p>
  {:else if jobs.cut}
    <!-- The line sits above the screen, which has no place for it. -->
    <p class="text-sm text-muted-foreground">
      {t('app.jobs.cut', { shown: jobs.shown })}
    </p>
  {/if}
  <JobsScreen
    {jobs}
    devices={fleet.devices}
    {transitions}
    onPrint={ctx.print}
    onPrinted={(printers: readonly string[]) => {
      ctx.go(printedHref(printers));
    }}
    thumbnailFor={(row: JobRow) =>
      deviceThumbnailUrlOf(client, row.printerId, row.path)}
  />
</div>
