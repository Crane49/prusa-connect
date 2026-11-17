<!--
  A folder of one storage, picked by walking it: where the ticked entries move
  to on the Storage page, and where a file sent from a printer page is kept.

  It walks one folder at a time rather than drawing a tree: the server answers
  one directory per request, and a tree would ask for every folder of a storage
  to draw a picker a reader steps through in two clicks.
-->
<script lang="ts">
  import FolderIcon from '@lucide/svelte/icons/folder';

  import { describeFault, t } from '../../i18n';
  import {
    Button,
    Dialog,
    DialogClose,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
    FaultAlert,
    FolderTrail,
  } from '../../primitives';
  import { trailOf } from './presentation';
  import type { StorageRow } from './types';

  let {
    open,
    onOpenChange,
    title,
    description,
    confirmLabel,
    root,
    current,
    at,
    folders,
    fault,
    onWalk,
    onConfirm,
  }: {
    open: boolean;
    onOpenChange: (open: boolean) => void;
    title: string;
    description: string;
    /** What the button that takes the folder says. */
    confirmLabel: string;
    /** What the storage calls itself, which leads the trail. */
    root: string;
    /**
     * A folder the confirm refuses, such as the one the entries being moved
     * are already in. Absent, every folder can be taken.
     */
    current?: string | undefined;
    /** The folder the picker is showing. The host opens it at the storage root. */
    at: string;
    /** The folders directly inside `at`. */
    folders: readonly Pick<StorageRow, 'name' | 'path'>[];
    /** Why the folders inside `at` could not be listed. */
    fault?: unknown;
    /** Asks the host to show the folders inside another folder. */
    onWalk: (path: string) => void;
    onConfirm: (path: string) => void;
  } = $props();

  const crumbs = $derived(trailOf(root, at));

  const refusal = $derived(
    fault === undefined ? undefined : describeFault(fault),
  );
</script>

<Dialog {open} {onOpenChange}>
  <DialogContent>
    <DialogHeader>
      <DialogTitle>{title}</DialogTitle>
      <DialogDescription>{description}</DialogDescription>
    </DialogHeader>
    <FolderTrail {crumbs} label={t('storage.folders.trail')} onOpen={onWalk} />
    <FaultAlert fault={refusal} />
    <div class="flex max-h-64 flex-col overflow-y-auto">
      {#each folders as folder (folder.path)}
        <Button
          variant="ghost"
          class="justify-start"
          onclick={() => {
            onWalk(folder.path);
          }}
        >
          <FolderIcon aria-hidden="true" />
          {folder.name}
        </Button>
      {:else}
        <p class="text-sm text-muted-foreground">
          {t('storage.folders.empty')}
        </p>
      {/each}
    </div>
    <DialogFooter>
      <DialogClose>
        {#snippet child({ props })}
          <Button {...props} variant="secondary"
            >{t('primitives.cancel')}</Button
          >
        {/snippet}
      </DialogClose>
      <Button
        disabled={current !== undefined && at === current}
        onclick={() => {
          onConfirm(at);
        }}
      >
        {confirmLabel}
      </Button>
    </DialogFooter>
  </DialogContent>
</Dialog>
