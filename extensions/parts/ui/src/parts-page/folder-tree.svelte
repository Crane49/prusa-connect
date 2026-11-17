<!--
  The folder tree: every folder of the catalogue, under one entry for the top.

  The whole tree comes with the overview, so walking it costs no request. Any
  folder with folders inside it, the top included, opens and closes with the
  chevron beside it, and the header opens or closes them all at once. The
  folders above the one on screen open by themselves, so the reader always
  sees where they are. What the person closed is kept for them.

  Files dropped onto a row become parts of that folder. A person who may
  write makes a new folder from the header, inside the folder on screen.
-->
<script lang="ts">
  import ChevronRightIcon from '@lucide/svelte/icons/chevron-right';
  import ChevronsDownUpIcon from '@lucide/svelte/icons/chevrons-down-up';
  import ChevronsUpDownIcon from '@lucide/svelte/icons/chevrons-up-down';
  import FolderPlusIcon from '@lucide/svelte/icons/folder-plus';
  import LibraryIcon from '@lucide/svelte/icons/library';
  import { Button } from '@prusa/connect-ui/primitives';

  import { TOP } from '../navigation.svelte';
  import { heldSession } from '../session';
  import { fileDrop } from './file-drop';
  import FolderLink from './folder-link.svelte';
  import FolderNode from './folder-node.svelte';
  import {
    CHILDREN,
    COUNT,
    GLYPH_CURRENT,
    LINE,
    ROW,
    ROW_CURRENT,
    ROW_IDLE,
  } from './tree-row';

  let { onNewFolder }: { onNewFolder: () => void } = $props();

  const { catalogue, navigation, tree, drop, host } = heldSession();
  const t = host.t;

  /** The top of the tree, which the tree state keys like any folder. */
  const ROOT = 0;

  const here = $derived(navigation.searching ? -1 : navigation.spot.folder);
  const top = $derived(catalogue.childrenOf(ROOT));
  const open = $derived(tree.isOpen(ROOT, here, here > 0));
  // The folders a "close all" closes: every one with folders inside it. The
  // top stays open, so closing all still shows the first level. The store
  // works them out once per overview, so a large tree costs one pass.
  const branches = $derived(catalogue.branches);
  // Whether the header offers to close or to open: only the folders drawn
  // count, so a closed folder deleted since does not flip the button.
  const allOpen = $derived(tree.openAmong([ROOT, ...branches]));
  // Every part of the catalogue: the ones at the top and the ones in each
  // folder, which the overview counts folder by folder.
  const total = $derived(
    catalogue.folders.reduce(
      (sum, folder) => sum + (folder.parts ?? 0),
      catalogue.overview?.root_parts ?? 0,
    ),
  );
</script>

<nav aria-label={t('parts.tree.label')} class="grid gap-y-1">
  <div class="flex min-h-8 items-center justify-between gap-2">
    <h2
      class="text-xs font-medium tracking-wide text-muted-foreground uppercase"
    >
      {t('parts.tree.heading')}
    </h2>
    <span class="flex items-center gap-1">
      {#if catalogue.allowed.write}
        <Button
          variant="ghost"
          size="icon-sm"
          label={t('parts.action.newFolder')}
          onclick={onNewFolder}
        >
          <FolderPlusIcon aria-hidden="true" />
        </Button>
      {/if}
      {#if branches.length > 0}
        <Button
          variant="ghost"
          size="icon-sm"
          data-part="tree-all"
          label={allOpen
            ? t('parts.tree.collapseAll')
            : t('parts.tree.expandAll')}
          onclick={() => {
            if (allOpen) tree.closeAll(branches, here);
            else tree.openAll();
          }}
        >
          {#if allOpen}
            <ChevronsDownUpIcon aria-hidden="true" />
          {:else}
            <ChevronsUpDownIcon aria-hidden="true" />
          {/if}
        </Button>
      {/if}
    </span>
  </div>
  <ul class="grid" data-part="folder-tree">
    <li>
      <div
        class={LINE}
        data-part="folder-row"
        {@attach fileDrop((files) => {
          void drop.drop(ROOT, files);
        }, drop.allowed)}
      >
        {#if top.length > 0}
          <Button
            variant="ghost"
            size="icon-sm"
            aria-expanded={open}
            label={open
              ? t('parts.tree.collapse', { name: t('parts.tree.all') })
              : t('parts.tree.expand', { name: t('parts.tree.all') })}
            onclick={() => {
              tree.toggle(ROOT, here, here > 0);
            }}
          >
            <ChevronRightIcon
              aria-hidden="true"
              class={[
                'size-4 transition-transform motion-reduce:transition-none',
                open && 'rotate-90',
              ]}
            />
          </Button>
        {:else}
          <span class="size-7 shrink-0"></span>
        {/if}
        <FolderLink
          spot={TOP}
          current={here === ROOT}
          class={[ROW, here === ROOT ? ROW_CURRENT : ROW_IDLE]}
        >
          <LibraryIcon
            aria-hidden="true"
            class={here === ROOT
              ? GLYPH_CURRENT
              : 'size-4 shrink-0 text-muted-foreground'}
          />
          <span class="min-w-0 flex-1 truncate">{t('parts.tree.all')}</span>
          <span class={COUNT}>{total}</span>
        </FolderLink>
      </div>
      {#if open && top.length > 0}
        <ul class={CHILDREN}>
          {#each top as folder (folder.id)}
            <FolderNode {folder} {here} />
          {/each}
        </ul>
      {/if}
    </li>
  </ul>
</nav>
