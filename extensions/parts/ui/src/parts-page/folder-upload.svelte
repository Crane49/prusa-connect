<!--
  The zone that makes parts of the folder on screen from files, by drop or by
  pick: the Storage screen's dropzone, at the head of the folder column.

  A pick is the same as dropping files onto the listing, for a person who
  cannot drag: a keyboard, a screen reader, a touch screen. What is taken goes
  the way a drop goes, into the list of dropped files above the listing.
-->
<script lang="ts">
  import { FileDropzone } from '@prusa/connect-ui/primitives';

  import { heldSession } from '../session';

  let { folder }: { folder: number } = $props();

  const { catalogue, drop } = heldSession();

  // An empty list is the server saying the storage takes any extension.
  const accept = $derived.by(() => {
    const allowed = catalogue.storage?.allowed_extensions ?? [];

    return allowed.length === 0 ? undefined : allowed.join(',');
  });
</script>

{#if drop.allowed}
  <FileDropzone
    {accept}
    data-part="folder-upload"
    onFiles={(files: File[]) => void drop.drop(folder, files)}
  />
{/if}
