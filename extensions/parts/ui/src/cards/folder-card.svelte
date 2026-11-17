<!--
  A dashboard card of one folder of the catalogue: its newest parts, or its
  parts by name, with a link to the folder on the module's page.
-->
<script lang="ts">
  import type { CardProps } from '@prusa/connect-ui/slots';
  import { onMount } from 'svelte';

  import { heldSession } from '../session';
  import CardFrame from './card-frame.svelte';
  import FolderParts from './folder-parts.svelte';
  import { folderCardSettings, folderHref } from './settings';

  let { config, title, size, editing }: CardProps = $props();

  const { host, catalogue } = heldSession();
  const t = host.t;

  const settings = $derived(folderCardSettings(config));
  // The parts are read again when any setting changes.
  const read = $derived(JSON.stringify(settings));

  // The tree names the folder in the heading. The page reads it too, so
  // whichever comes first reads it for both.
  onMount(() => {
    void catalogue.frame();
  });

  const folderName = $derived(
    settings.folder === 0
      ? t('parts.tree.all')
      : (catalogue.folder(settings.folder)?.name ??
          t('parts.dashboard.folder.name')),
  );
</script>

<CardFrame
  part="parts-folder-card"
  heading={title.trim() === '' ? folderName : title}
  href={editing
    ? undefined
    : folderHref(host.pageHref('parts'), settings.folder)}
  link={t('parts.dashboard.folder.open')}
>
  {#key read}
    <FolderParts {settings} {size} {editing} />
  {/key}
</CardFrame>
