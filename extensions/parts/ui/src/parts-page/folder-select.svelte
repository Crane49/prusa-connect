<!--
  A choice of one folder, as the tree is: the top first, then every folder
  under its parent, indented by its depth.

  A folder being moved cannot go into itself or into a folder inside it, so
  that subtree is left out of the choice rather than refused after it.
-->
<script lang="ts">
  import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
  } from '@prusa/connect-ui/primitives';

  import type { Folder } from '../parts';
  import { heldSession } from '../session';

  let {
    id,
    value,
    onValue,
    exclude,
  }: {
    id: string;
    value: number;
    onValue: (folder: number) => void;
    /** A folder whose subtree is not offered, for a folder being moved. */
    exclude?: number | undefined;
  } = $props();

  const { catalogue, host } = heldSession();
  const t = host.t;

  interface Choice {
    value: string;
    label: string;
    depth: number;
  }

  /** The tree, walked depth first, so a folder follows its parent. */
  const choices = $derived.by((): Choice[] => {
    const out: Choice[] = [
      { value: '0', label: t('parts.tree.all'), depth: 0 },
    ];
    const walk = (parent: number, depth: number): void => {
      for (const folder of catalogue.childrenOf(parent)) {
        if (folder.id === exclude) continue;
        out.push({ value: String(folder.id), label: folder.name, depth });
        walk(folder.id, depth + 1);
      }
    };
    walk(0, 1);

    return out;
  });

  const chosen = (folder: Folder | undefined): string =>
    folder?.name ?? t('parts.tree.all');
</script>

<Select
  value={String(value)}
  items={choices.map((choice) => ({
    value: choice.value,
    label: choice.label,
  }))}
  onValueChange={(next: string) => {
    onValue(Number(next));
  }}
>
  <SelectTrigger {id} class="w-full">
    <SelectValue placeholder={chosen(catalogue.folder(value))} />
  </SelectTrigger>
  <SelectContent>
    {#each choices as choice (choice.value)}
      <SelectItem value={choice.value} label={choice.label}>
        <span class="folder-depth" style="--depth: {choice.depth}"></span>
        {choice.label}
      </SelectItem>
    {/each}
  </SelectContent>
</Select>

<style>
  .folder-depth {
    display: inline-block;
    inline-size: calc(var(--depth) * var(--spacing) * 3);
  }
</style>
