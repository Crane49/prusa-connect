<!--
  A file page bookmarked before the address named its storage. The old
  address carried the file's path in its query and no storage.
-->
<script lang="ts">
  import { PATHS, storageFileHref } from '../routes';
  import type { AppContext } from '../screens';
  import StorageFiles from './storage.svelte';

  let { ctx }: { ctx: AppContext } = $props();

  const path = $derived(
    new URLSearchParams(ctx.search).get('path')?.replace(/^\/+/, ''),
  );

  const storage = $derived(ctx.storage);

  // The pattern names no storage, so the storage page is told the name.
  const named = $derived<AppContext>({ ...ctx, params: { storage: 'file' } });

  // The answer of this screen's own read of the storages. The store's error
  // is shared, so an older error from a move or a delete must not decide.
  let listed = $state<readonly string[] | 'failed' | undefined>(undefined);

  $effect(() => {
    if (path === undefined || path === '') return;

    void storage.load().then(() => {
      listed =
        storage.error === undefined
          ? storage.storages.map((row) => row.name)
          : 'failed';
    });
  });

  // One storage is the only place the file can be. With more, the address
  // does not say which, so the reader chooses on the storage page.
  const wanted = $derived.by(() => {
    if (path === undefined) return undefined;
    // A path that names nothing is no file to open. A dot segment names no
    // stored file either, and the browser would rewrite it in the address.
    if (path === '') return PATHS.storage;
    if (path.split('/').some((part) => part === '.' || part === '..')) {
      return PATHS.storage;
    }
    if (listed === undefined) return undefined;
    if (listed === 'failed' || listed.length !== 1) return PATHS.storage;

    const [only = ''] = listed;

    return storageFileHref(only, path);
  });

  $effect(() => {
    if (wanted !== undefined) ctx.go(wanted, { replace: true });
  });
</script>

<!-- Without a path this is the address of a storage named file. -->
{#if path === undefined}
  <StorageFiles ctx={named} />
{/if}
