<!--
  One folder of the tree, and the folders inside it.

  The depth is drawn by nesting: each level hangs off a guide line under its
  parent's chevron, so no number is handed to the styles.

  Files dropped onto the row become parts of this folder.
-->
<script lang="ts">
  import ChevronRightIcon from '@lucide/svelte/icons/chevron-right';
  import FolderIcon from '@lucide/svelte/icons/folder';
  import FolderOpenIcon from '@lucide/svelte/icons/folder-open';
  import { Button } from '@prusa/connect-ui/primitives';

  import { TOP } from '../navigation.svelte';
  import type { Folder } from '../parts';
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

  let {
    folder,
    here,
  }: {
    folder: Folder;
    /** The folder on screen, and -1 while a search spans every folder. */
    here: number;
  } = $props();

  const { catalogue, tree, drop, host } = heldSession();
  const t = host.t;

  const children = $derived(catalogue.childrenOf(folder.id));
  const current = $derived(here === folder.id);
  const holdsHere = $derived(
    here > 0 &&
      !current &&
      catalogue.trailOf(here).some((row) => row.id === folder.id),
  );
  const open = $derived(tree.isOpen(folder.id, here, holdsHere));
</script>

<li>
  <div
    class={LINE}
    data-part="folder-row"
    {@attach fileDrop((files) => {
      void drop.drop(folder.id, files);
    }, drop.allowed)}
  >
    {#if children.length > 0}
      <Button
        variant="ghost"
        size="icon-sm"
        aria-expanded={open}
        label={open
          ? t('parts.tree.collapse', { name: folder.name })
          : t('parts.tree.expand', { name: folder.name })}
        onclick={() => {
          tree.toggle(folder.id, here, holdsHere);
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
      spot={{ ...TOP, folder: folder.id }}
      {current}
      class={[ROW, current ? ROW_CURRENT : ROW_IDLE]}
    >
      {#if current}
        <FolderOpenIcon aria-hidden="true" class={GLYPH_CURRENT} />
      {:else if open && children.length > 0}
        <FolderOpenIcon
          aria-hidden="true"
          class="size-4 shrink-0 text-muted-foreground"
        />
      {:else}
        <FolderIcon
          aria-hidden="true"
          class="size-4 shrink-0 text-muted-foreground"
        />
      {/if}
      <span class="min-w-0 flex-1 truncate">{folder.name}</span>
      {#if (folder.parts ?? 0) > 0}
        <span class={COUNT}>{folder.parts}</span>
      {/if}
    </FolderLink>
  </div>
  {#if open && children.length > 0}
    <ul class={CHILDREN}>
      {#each children as child (child.id)}
        <FolderNode folder={child} {here} />
      {/each}
    </ul>
  {/if}
</li>
