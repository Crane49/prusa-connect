<!--
  The files of the part's newest version, as rows: the picture or the glyph of
  its kind, the name, what it is, how big it is, and what can be done with it.

  A print file is printed from its row. Any file is downloaded from its name,
  which is a plain link the browser fetches with the session the page already
  has. A picture or a print file with a plate inside it can stand for the part
  in the listing, and the one that does is marked.
-->
<script lang="ts">
  import DownloadIcon from '@lucide/svelte/icons/download';
  import FilePlus2Icon from '@lucide/svelte/icons/file-plus-2';
  import ImageIcon from '@lucide/svelte/icons/image';
  import PrinterIcon from '@lucide/svelte/icons/printer';
  import { formatBytes } from '@prusa/connect-ui/i18n';
  import {
    ABOVE_ROW_LINK,
    Badge,
    Button,
    ClippedName,
    EmptyState,
    FileThumbnail,
    Table,
    TableBody,
    TableCaption,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
    THUMBNAIL_ROW_MIN_HEIGHT,
  } from '@prusa/connect-ui/primitives';

  import {
    canCover,
    fileThumbnail,
    glyphOf,
    isPrintable,
    KIND_KEY,
    kindOf,
  } from '../presentation';
  import type { PartFile, Version } from '../parts';
  import { heldSession } from '../session';

  let {
    version,
    mayPrint,
    onPrint,
    onNewVersion,
  }: {
    version: Version | undefined;
    mayPrint: boolean;
    onPrint: (file: PartFile) => void;
    /** Absent for a person who may not upload. */
    onNewVersion?: (() => void) | undefined;
  } = $props();

  const { catalogue, host, files } = heldSession();
  const t = host.t;

  const part = $derived(catalogue.detail);
  const cover = $derived(part?.picture?.path);

  const useAsPicture = (file: PartFile): void => {
    if (part === undefined) return;
    void catalogue.changePart(part.id, { cover: file.path });
  };
</script>

{#if version === undefined}
  <EmptyState
    icon={FilePlus2Icon}
    headingLevel="h2"
    heading={t('parts.files.none.heading')}
    description={onNewVersion
      ? t('parts.files.none.note')
      : t('parts.files.none.viewer')}
  >
    {#snippet action()}
      {#if onNewVersion}
        <Button onclick={onNewVersion}>
          <FilePlus2Icon aria-hidden="true" />
          {t('parts.action.newVersion')}
        </Button>
      {/if}
    {/snippet}
  </EmptyState>
{:else}
  <div class="grid gap-y-3">
    <p class="text-sm text-muted-foreground">
      {t('parts.files.of', { number: version.number })}
    </p>
    <Table data-part="files">
      <TableCaption class="sr-only"
        >{t('parts.files.of', { number: version.number })}</TableCaption
      >
      <TableHeader>
        <TableRow>
          <TableHead>{t('parts.column.file')}</TableHead>
          <TableHead class="hidden @xl/parts:table-cell"
            >{t('parts.column.kind')}</TableHead
          >
          <TableHead>{t('parts.column.size')}</TableHead>
          <TableHead class="sr-only">{t('parts.column.actions')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {#each version.files as file (`${file.storage}/${file.path}`)}
          <TableRow class={THUMBNAIL_ROW_MIN_HEIGHT} data-part="file">
            <TableCell class="max-w-80">
              <div class="flex items-center gap-3">
                <FileThumbnail
                  name={file.name}
                  thumbnailUrl={catalogue.allowed.files
                    ? fileThumbnail(files, file)
                    : undefined}
                  glyph={glyphOf(file.name)}
                />
                <span class="grid min-w-0">
                  {#if catalogue.allowed.files}
                    <a
                      href={files.contentUrl(file.storage, file.path)}
                      download={file.name}
                      class="min-w-0 underline-offset-4 hover:underline"
                    >
                      <ClippedName name={file.name} />
                    </a>
                  {:else}
                    <ClippedName name={file.name} class="min-w-0" />
                  {/if}
                  {#if cover === file.path}
                    <span class="text-xs text-muted-foreground"
                      >{t('parts.files.isPicture')}</span
                    >
                  {/if}
                </span>
              </div>
            </TableCell>
            <TableCell class="hidden @xl/parts:table-cell">
              <Badge variant="outline">{t(KIND_KEY[kindOf(file.name)])}</Badge>
            </TableCell>
            <TableCell class="font-mono text-xs whitespace-nowrap"
              >{formatBytes(file.size)}</TableCell
            >
            <TableCell class="text-end whitespace-nowrap">
              {#if mayPrint && isPrintable(file.name)}
                <Button
                  variant="ghost"
                  size="icon-sm"
                  class={ABOVE_ROW_LINK}
                  label={t('parts.files.printFor', { name: file.name })}
                  onclick={() => {
                    onPrint(file);
                  }}
                >
                  <PrinterIcon aria-hidden="true" />
                </Button>
              {/if}
              {#if catalogue.allowed.write && canCover(file.name) && cover !== file.path}
                <Button
                  variant="ghost"
                  size="icon-sm"
                  label={t('parts.files.useAsPicture', {
                    name: file.name,
                  })}
                  disabled={catalogue.busy}
                  onclick={() => {
                    useAsPicture(file);
                  }}
                >
                  <ImageIcon aria-hidden="true" />
                </Button>
              {/if}
              {#if catalogue.allowed.files}
                <Button
                  variant="ghost"
                  size="icon-sm"
                  label={t('parts.files.downloadFor', { name: file.name })}
                >
                  {#snippet child({ props })}
                    <a
                      {...props}
                      href={files.contentUrl(file.storage, file.path)}
                      download={file.name}
                    >
                      <DownloadIcon aria-hidden="true" />
                    </a>
                  {/snippet}
                </Button>
              {/if}
            </TableCell>
          </TableRow>
        {/each}
      </TableBody>
    </Table>
  </div>
{/if}
