<!-- One file of this server's own storage. -->
<script lang="ts">
  import { FileDetailScreen, PrintDialog } from '@prusa/connect-ui/shell';
  import {
    FileView,
    fileRefOf,
    getSlotContext,
    moduleTabs,
    SlotActions,
    SlotOutlet,
  } from '@prusa/connect-ui/slots';
  import { onMount, type Snippet, untrack } from 'svelte';

  import { printedHref, storageHref } from '../routes';
  import type { AppContext } from '../screens';

  let { ctx }: { ctx: AppContext } = $props();

  // The page uses the shared store, so the storage page opens on this file's
  // folder.
  const storage = $derived(ctx.storage);

  const name = $derived(ctx.params.storage ?? '');
  const path = $derived(ctx.params.path ?? '');
  const folder = $derived(path.slice(0, Math.max(path.lastIndexOf('/'), 0)));

  $effect(() => {
    void storage.load();
  });

  // The names of the models a driver declared, for a print's printer icon.
  onMount(() => {
    void ctx.deviceModels.read();
  });

  // The print dialog grades the printers by their state.
  $effect(() => {
    void ctx.fleet.load();
    void ctx.deviceStates.read();
  });
  const devices = $derived(ctx.fleet.devices);
  const transitions = $derived(
    ctx.deviceStates.transitionsOf(devices.map((device) => device.kind)),
  );
  // The printers with no USB drive to put the file on, once they answered.
  const noStorage = $derived(
    devices
      .filter((device) => ctx.printerStorages.noStorage(device.id))
      .map((device) => device.id),
  );
  let printing = $state(false);

  // The store owns both reads and stops the file read when the reader leaves.
  // The call is untracked because the store reads its own record of described
  // files, and tracking that read would start the reads again on every answer.
  $effect(() => {
    const [asked, file] = [name, path];
    untrack(() => {
      void storage.openFile(asked, file);
    });

    return () => {
      storage.leaveFile();
    };
  });

  const chosen = $derived(storage.storages.find((row) => row.name === name));

  // Rows of another folder are not this file's, even while they are listed.
  const listed = $derived(
    storage.storage === name && storage.directory === folder,
  );
  const entry = $derived(
    listed ? storage.entries.find((row) => row.path === path) : undefined,
  );

  const description = $derived(storage.describedFile(name, path));

  // The open tab sits in the query, because the file's path already fills
  // the rest of the address.
  const tab = $derived(new URLSearchParams(ctx.search).get('tab') ?? undefined);
  const showTab = (next: string): void => {
    const rest = new URLSearchParams([
      ...[...new URLSearchParams(ctx.search)].filter(([key]) => key !== 'tab'),
      ...(next === 'contents' ? [] : [['tab', next]]),
    ]).toString();
    ctx.go(`${ctx.path}${rest === '' ? '' : `?${rest}`}`);
  };

  // A deep link to a module tab draws no outlet until the strip knows the
  // tab, so the page wakes the loader itself.
  const slots = getSlotContext();
  onMount(() => {
    void slots?.loader?.activate();
  });
  const fileRef = $derived(fileRefOf(name, path));
  const tabsForModules = $derived(
    (slots ? moduleTabs(slots.registry, 'file-tab', fileRef) : []).map(
      (row) => ({ ...row, panel: moduleTab }),
    ),
  );

  const acts = $derived({
    // The content route takes the permission that lists the storage, which
    // the listing this file sits in already proved.
    downloadHref:
      entry === undefined || entry.folder
        ? undefined
        : storage.contentUrl(name, path),
    // The screen offers it for a file a printer takes as it is, once this
    // server holds a printer to send it to.
    print:
      devices.length === 0
        ? undefined
        : () => {
            printing = true;
            // Only a printer that is here can be asked.
            void ctx.printerStorages.check(
              devices
                .filter((device) => device.connected)
                .map((device) => device.id),
            );
          },
    // A removed file leaves this page with nothing to show.
    remove:
      chosen?.write !== true
        ? undefined
        : () => {
            void storage.remove(name, path).then(() => {
              if (storage.error === undefined)
                ctx.go(storageHref(name, folder));
            });
          },
  });
</script>

<FileDetailScreen
  {path}
  {entry}
  rootLabel={chosen?.display_name ?? name}
  rootHref={storageHref(name)}
  folderHref={(at: string) => storageHref(name, at)}
  {acts}
  {description}
  fault={storage.filesError ?? storage.error}
  loading={storage.loading || storage.filesLoading || !listed}
  {menu}
  {contents}
  {tab}
  onTabChange={showTab}
  moduleTabs={tabsForModules}
/>

<PrintDialog
  open={printing}
  onOpenChange={(next: boolean) => {
    printing = next;
  }}
  file={entry === undefined
    ? undefined
    : {
        details: description?.details,
        name: entry.name,
        source: entry.path,
        storage: name,
        thumbnailUrl: description?.thumbnailUrl,
      }}
  {devices}
  {transitions}
  {noStorage}
  transfers={ctx.running.rows}
  onPrint={ctx.print}
  onPrinted={(printers: readonly string[]) => {
    ctx.go(printedHref(printers));
  }}
/>

<!-- What the file holds: a module's view for the file's type, or the
     screen's own part (ADR-20261027-wvj rule 16). -->
{#snippet contents(own: Snippet)}
  <FileView {fileRef} fallback={own} />
{/snippet}

<!-- A module's own tab (ADR-20261029-dgv). -->
{#snippet moduleTab()}
  <SlotOutlet screen="storage" name="file-tab" {fileRef} entry={tab} />
{/snippet}

<!-- The file's own menu, with a module's file-action entries in it. -->
{#snippet menu(own: Snippet | undefined)}
  <SlotActions
    name="file-action"
    {fileRef}
    label={entry?.name ?? path.split('/').at(-1) ?? path}
    items={own}
    buttons
  />
{/snippet}
