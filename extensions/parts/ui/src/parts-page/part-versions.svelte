<!--
  The history of the part, newest first: each version with its note, who
  wrote it and when, and the files it was at.

  A version's files do not change, so the history reads the way the part
  went. The note can be rewritten, because a note is a person's explanation
  and they may explain better later. Deleting a version deletes the files no
  other version holds, and the confirmation says so.
-->
<script lang="ts">
  import DownloadIcon from '@lucide/svelte/icons/download';
  import HistoryIcon from '@lucide/svelte/icons/history';
  import PencilLineIcon from '@lucide/svelte/icons/pencil-line';
  import PrinterIcon from '@lucide/svelte/icons/printer';
  import Trash2Icon from '@lucide/svelte/icons/trash-2';
  import { formatBytes, formatDateTime } from '@prusa/connect-ui/i18n';
  import {
    Badge,
    Button,
    ConfirmDialog,
    DropdownMenuItem,
    EmptyState,
    RowActions,
    Textarea,
  } from '@prusa/connect-ui/primitives';

  import { glyphOf, isPrintable, MAX_NOTE } from '../presentation';
  import type { PartFile, Version } from '../parts';
  import { heldSession } from '../session';
  import { personOf } from './person';

  let {
    mayPrint,
    onPrint,
  }: { mayPrint: boolean; onPrint: (file: PartFile) => void } = $props();

  const { catalogue, host, files } = heldSession();
  const t = host.t;

  let editing = $state<{ number: number; note: string } | undefined>(undefined);
  let deleting = $state<Version | undefined>(undefined);

  const part = $derived(catalogue.detail);
  const newestFirst = $derived([...(part?.versions ?? [])].reverse());
  const newest = $derived(part?.versions.at(-1)?.number);

  const saveNote = async (): Promise<void> => {
    if (part === undefined || editing === undefined) return;
    if (
      await catalogue.changeVersion(
        part.id,
        editing.number,
        editing.note.trim(),
      )
    ) {
      editing = undefined;
    }
  };

  const remove = (): void => {
    const version = deleting;
    deleting = undefined;
    if (part === undefined || version === undefined) return;
    void catalogue.deleteVersion(part.id, version.number);
  };
</script>

{#if newestFirst.length === 0}
  <EmptyState
    icon={HistoryIcon}
    headingLevel="h2"
    heading={t('parts.versions.none.heading')}
    description={t('parts.versions.none.note')}
  />
{:else}
  <ol class="grid gap-y-4" data-part="versions">
    {#each newestFirst as version (version.number)}
      <li
        class="grid gap-y-3 rounded-sm border border-border p-4"
        data-part="version"
      >
        <div class="flex flex-wrap items-center gap-2">
          <h3 class="text-sm font-medium">
            {t('parts.version.long', { number: version.number })}
          </h3>
          {#if version.number === newest}
            <Badge variant="info">{t('parts.versions.newest')}</Badge>
          {/if}
          <span class="text-xs text-muted-foreground">
            {t('parts.fact.when', {
              when: formatDateTime(version.created),
              who: personOf(version.created_by, catalogue.who, t),
            })}
          </span>
          {#if catalogue.allowed.write}
            <span class="ms-auto">
              <RowActions
                name={t('parts.version.long', { number: version.number })}
              >
                <DropdownMenuItem
                  onSelect={() => {
                    editing = {
                      number: version.number,
                      note: version.note ?? '',
                    };
                  }}
                >
                  <PencilLineIcon aria-hidden="true" />
                  {t('parts.versions.editNote')}
                </DropdownMenuItem>
                {#if catalogue.allowed.upload}
                  <DropdownMenuItem
                    variant="destructive"
                    onSelect={() => {
                      deleting = version;
                    }}
                  >
                    <Trash2Icon aria-hidden="true" />
                    {t('parts.versions.delete.action')}
                  </DropdownMenuItem>
                {/if}
              </RowActions>
            </span>
          {/if}
        </div>

        {#if editing?.number === version.number}
          <Textarea
            aria-label={t('parts.versions.note')}
            rows={3}
            data-byte-limit={MAX_NOTE}
            bind:value={editing.note}
          />
          <div class="flex gap-2">
            <Button
              size="sm"
              disabled={catalogue.busy}
              onclick={() => void saveNote()}
            >
              {t('parts.action.save')}
            </Button>
            <Button
              size="sm"
              variant="secondary"
              onclick={() => {
                editing = undefined;
              }}
            >
              {t('parts.action.cancel')}
            </Button>
          </div>
        {:else if version.note}
          <p class="text-sm whitespace-pre-line">{version.note}</p>
        {/if}

        <ul class="grid gap-y-1" aria-label={t('parts.versions.files')}>
          {#each version.files as file (`${file.storage}/${file.path}`)}
            {@const Glyph = glyphOf(file.name)}
            <li class="flex min-w-0 items-center gap-2 text-sm">
              <Glyph
                aria-hidden="true"
                class="size-4 shrink-0 text-muted-foreground"
              />
              <span class="min-w-0 flex-1 truncate">{file.name}</span>
              <span class="font-mono text-xs text-muted-foreground"
                >{formatBytes(file.size)}</span
              >
              {#if mayPrint && isPrintable(file.name)}
                <Button
                  variant="ghost"
                  size="icon-sm"
                  label={t('parts.files.printFor', { name: file.name })}
                  onclick={() => {
                    onPrint(file);
                  }}
                >
                  <PrinterIcon aria-hidden="true" />
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
            </li>
          {/each}
        </ul>
      </li>
    {/each}
  </ol>
{/if}

<ConfirmDialog
  open={deleting !== undefined}
  onOpenChange={(open: boolean) => {
    if (!open) deleting = undefined;
  }}
  title={t('parts.versions.delete.title', { number: deleting?.number ?? 0 })}
  description={t('parts.versions.delete.note')}
  confirmLabel={t('parts.action.delete')}
  onConfirm={remove}
/>
