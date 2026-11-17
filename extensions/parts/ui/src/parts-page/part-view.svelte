<!--
  One part, on the page of its own: the trail up to its folder, its header,
  and four tabs.

  The shape is the file detail's: a trail, a header with the picture, the
  name and the acts, and a card under it. The acts lead with what the part is
  for. A part whose newest version holds a print file offers the print first,
  on a printer the person picks; a new version is beside it.

  Files dropped anywhere on the page open the new version with those files
  already going up, so a new revision of a model is one drag away.

  A part that is not there, deleted or never made, says so with a way back to
  its folder. One that could not be read is drawn by its code.
-->
<script lang="ts">
  import CloudUploadIcon from '@lucide/svelte/icons/cloud-upload';
  import FilePlus2Icon from '@lucide/svelte/icons/file-plus-2';
  import PencilLineIcon from '@lucide/svelte/icons/pencil-line';
  import PlayIcon from '@lucide/svelte/icons/play';
  import SearchXIcon from '@lucide/svelte/icons/search-x';
  import Trash2Icon from '@lucide/svelte/icons/trash-2';
  import {
    Badge,
    Button,
    Card,
    CardContent,
    Chip,
    ConfirmDialog,
    DetailHeader,
    DetailTabs,
    DropdownMenuItem,
    EmptyState,
    FaultAlert,
    PageTrail,
    RowActions,
  } from '@prusa/connect-ui/primitives';
  import {
    PrintDialog,
    printerHref,
    type PrintableFile,
  } from '@prusa/connect-ui/shell';

  import type { PrintRequest } from '@prusa/connect-ui/domain';

  import { faultText } from '../faults';
  import { TOP } from '../navigation.svelte';
  import { fileThumbnail, isPrintable, pictureUrl } from '../presentation';
  import type { PartFile } from '../parts';
  import { heldSession } from '../session';
  import { fileDrop } from './file-drop';
  import FolderLink from './folder-link.svelte';
  import PartComments from './part-comments.svelte';
  import PartDialog from './part-dialog.svelte';
  import PartFiles from './part-files.svelte';
  import PartOverview from './part-overview.svelte';
  import PartPicture from './part-picture.svelte';
  import PartVersions from './part-versions.svelte';
  import VersionDialog from './version-dialog.svelte';

  const { catalogue, navigation, host, files, states } = heldSession();
  const t = host.t;

  /** The code the module answers for a part it does not hold. */
  const NOT_FOUND = 'parts/PART_NOT_FOUND';

  let tab = $state('overview');
  let editing = $state(false);
  let versioning = $state(false);
  /** What a drop onto the page hands the version dialog. */
  let dropped = $state.raw<File[]>([]);
  let deleting = $state(false);
  let printing = $state<PartFile | undefined>(undefined);

  const part = $derived(catalogue.detail);
  const fault = $derived(
    catalogue.detailFault === undefined
      ? undefined
      : faultText(catalogue.detailFault, t),
  );
  const missing = $derived(fault?.code === NOT_FOUND);
  const folder = $derived(part?.folder ?? navigation.spot.folder);

  const trail = $derived([
    { label: t('parts.title'), onSelect: () => navigation.openFolder(0) },
    ...catalogue.trailOf(folder).map((row) => ({
      label: row.name,
      onSelect: () => navigation.openFolder(row.id),
    })),
  ]);

  const latest = $derived(part?.versions.at(-1));
  /** The print file a press of the header's button prints. */
  const printFile = $derived(
    latest?.files.find((file) => isPrintable(file.name)),
  );

  const allowed = $derived(catalogue.allowed);
  const mayPrint = $derived(allowed.files && host.fleet.devices.length > 0);
  const mayUpload = $derived(allowed.write && allowed.upload);
  // Deleting a part deletes its files from the storage too, which takes
  // write_storage, so the act is offered only with both permissions, as
  // deleting a version is.
  const mayDelete = $derived(allowed.write && allowed.upload);

  const tabs = $derived([
    { id: 'overview', label: t('parts.tab.overview'), panel: overview },
    {
      id: 'files',
      label: t('parts.tab.files', { count: latest?.files.length ?? 0 }),
      panel: filesPanel,
    },
    {
      id: 'versions',
      label: t('parts.tab.versions', { count: part?.versions.length ?? 0 }),
      panel: versions,
    },
    {
      id: 'comments',
      label: t('parts.tab.comments', { count: part?.comments.length ?? 0 }),
      panel: comments,
    },
  ]);

  const kinds = $derived(host.fleet.devices.map((device) => device.kind));
  const transitions = $derived(states.transitionsOf(kinds));

  const sheetFile = $derived.by((): PrintableFile | undefined => {
    const file = printing;
    if (file === undefined) return undefined;

    return {
      name: file.name,
      storage: file.storage,
      source: file.path,
      thumbnailUrl: fileThumbnail(files, file),
      details: catalogue.described(file),
    };
  });

  /** Opens the printer picker on one file, and reads what the slicer wrote into it. */
  const print = (file: PartFile): void => {
    printing = file;
    void states.read();
    void catalogue.describe(file);
  };

  const remove = async (): Promise<void> => {
    deleting = false;
    if (part === undefined) return;
    const leaving = part.folder;
    if (await catalogue.deletePart(part.id)) navigation.openFolder(leaving);
  };
</script>

<div
  class="group/drop relative mx-auto grid w-full max-w-7xl gap-y-6"
  data-part="part-view"
  {@attach fileDrop(
    (files) => {
      if (versioning) return;
      dropped = files;
      versioning = true;
    },
    part !== undefined && mayUpload,
  )}
>
  <PageTrail {trail} page={part?.name ?? t('parts.part.loading')} />

  {#if missing}
    <Card>
      <CardContent>
        <EmptyState
          icon={SearchXIcon}
          headingLevel="h1"
          heading={t('parts.missing.heading')}
          description={t('parts.missing.note')}
        >
          {#snippet action()}
            <Button
              variant="secondary"
              onclick={() => {
                navigation.openFolder(folder);
              }}
            >
              {t('parts.missing.back')}
            </Button>
          {/snippet}
        </EmptyState>
      </CardContent>
    </Card>
  {:else if fault && part === undefined}
    <FaultAlert {fault} />
  {:else}
    <DetailHeader
      title={part?.name ?? ''}
      loading={part === undefined}
      {media}
      {titleAside}
      {meta}
      {actions}
    />

    <Card>
      <CardContent>
        {#if part}
          <DetailTabs
            {tabs}
            active={tab}
            onChange={(next: string) => {
              tab = next;
            }}
          />
        {/if}
      </CardContent>
    </Card>
  {/if}

  <div
    class="pointer-events-none absolute inset-0 hidden place-content-center justify-items-center gap-y-2 rounded-md border-2 border-dashed border-primary bg-secondary p-6 text-center text-secondary-foreground group-data-dropping/drop:grid group-data-dropping/drop:animate-fade-in"
    aria-hidden="true"
  >
    <CloudUploadIcon class="size-8 text-primary" />
    <p class="text-base font-medium">
      {t('parts.part.drop.hint', { name: part?.name ?? '' })}
    </p>
    <p class="max-w-sm text-sm text-muted-foreground">
      {t('parts.part.drop.note')}
    </p>
  </div>
</div>

{#snippet media()}
  <PartPicture url={pictureUrl(files, part?.picture)} class="size-24" />
{/snippet}

{#snippet titleAside()}
  {#if latest}
    <Badge variant="outline" numeric>
      {t('parts.version.short', { number: latest.number })}
    </Badge>
  {/if}
{/snippet}

{#snippet meta()}
  {#if part?.summary}
    <p class="text-sm text-muted-foreground">{part.summary}</p>
  {/if}
  {#if part && part.tags.length > 0}
    <ul class="flex flex-wrap gap-1" aria-label={t('parts.column.tags')}>
      {#each part.tags as tag (tag)}
        <li>
          <Chip>
            {#snippet child({ props })}
              <FolderLink
                {...props}
                spot={{ ...TOP, tag }}
                label={t('parts.tags.all', { tag })}
              >
                {tag}
              </FolderLink>
            {/snippet}
            {tag}
          </Chip>
        </li>
      {/each}
    </ul>
  {/if}
{/snippet}

{#snippet actions()}
  {#if printFile && mayPrint}
    <Button size="lg" data-part="print" onclick={() => print(printFile)}>
      <PlayIcon aria-hidden="true" />
      {t('parts.action.print')}
    </Button>
  {/if}
  {#if mayUpload}
    <Button
      size="lg"
      variant={printFile && mayPrint ? 'secondary' : 'default'}
      data-part="new-version"
      onclick={() => {
        versioning = true;
      }}
    >
      <FilePlus2Icon aria-hidden="true" />
      {t('parts.action.newVersion')}
    </Button>
  {/if}
  {#if part && allowed.write}
    <RowActions name={part.name}>
      <DropdownMenuItem
        onSelect={() => {
          editing = true;
        }}
      >
        <PencilLineIcon aria-hidden="true" />
        {t('parts.part.edit')}
      </DropdownMenuItem>
      {#if mayDelete}
        <DropdownMenuItem
          variant="destructive"
          data-part="delete-part"
          onSelect={() => {
            deleting = true;
          }}
        >
          <Trash2Icon aria-hidden="true" />
          {t('parts.part.delete.action')}
        </DropdownMenuItem>
      {/if}
    </RowActions>
  {/if}
{/snippet}

{#snippet overview()}
  <PartOverview />
{/snippet}

{#snippet filesPanel()}
  <PartFiles
    version={latest}
    {mayPrint}
    onPrint={print}
    onNewVersion={mayUpload
      ? () => {
          versioning = true;
        }
      : undefined}
  />
{/snippet}

{#snippet versions()}
  <PartVersions {mayPrint} onPrint={print} />
{/snippet}

{#snippet comments()}
  <PartComments />
{/snippet}

{#if part}
  <PartDialog
    open={editing}
    folder={part.folder}
    {part}
    onClose={() => {
      editing = false;
    }}
  />

  <VersionDialog
    open={versioning}
    {part}
    {dropped}
    onClose={() => {
      versioning = false;
      dropped = [];
    }}
    onSaved={() => {
      tab = 'files';
    }}
  />
{/if}

<ConfirmDialog
  open={deleting}
  onOpenChange={(open: boolean) => {
    deleting = open;
  }}
  title={t('parts.part.delete.title', { name: part?.name ?? '' })}
  description={t('parts.part.delete.note')}
  confirmLabel={t('parts.action.delete')}
  onConfirm={() => void remove()}
/>

<PrintDialog
  open={printing !== undefined}
  onOpenChange={(next: boolean) => {
    if (!next) printing = undefined;
  }}
  file={sheetFile}
  devices={host.fleet.devices}
  {transitions}
  onPrint={(request: PrintRequest) => files.print(request)}
  onPrinted={(printers: readonly string[]) => {
    // A print to one printer lands on that printer. A print to several stays
    // on the part, because no one printer's page shows them all.
    const [only] = printers;
    if (printers.length === 1 && only !== undefined) {
      navigation.leave(printerHref(only));
    }
  }}
/>
