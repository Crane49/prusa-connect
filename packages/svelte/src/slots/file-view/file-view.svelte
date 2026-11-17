<!-- What a file holds, on the file's page: a module's view for the file's
     type, or the core's own part (ADR-20261027-wvj rules 16 to 18). Each state
     is drawn: no view for the type draws the core's part, a view still
     loading draws a placeholder, a view that is not available draws the
     core's part with one line saying so, and a registered view draws itself. -->
<script lang="ts">
  import { onMount } from 'svelte';

  import { t } from '../../i18n';
  import { Skeleton } from '../../primitives';
  import { getSlotContext } from '../context';
  import { matchesFileRef } from '../file-types';
  import type { FileViewProps } from './types';

  let { fileRef, fallback, registry, loader }: FileViewProps = $props();

  const context = getSlotContext();
  const registryInUse = $derived(registry ?? context?.registry);
  const loaderInUse = $derived(loader ?? context?.loader);

  const forThisFile = <T extends { fileTypes?: readonly string[] | undefined }>(
    rows: readonly T[],
  ): T[] => rows.filter((row) => matchesFileRef(row.fileTypes, fileRef));

  const rows = $derived(
    forThisFile(registryInUse?.sections('file-view') ?? []),
  );
  const declared = $derived(
    forThisFile(registryInUse?.declaredSections('file-view') ?? []),
  );
  const refused = $derived(
    forThisFile(registryInUse?.refusedSections('file-view') ?? []),
  );

  // A view the manifest declared and the bundle has not registered yet.
  const waiting = $derived(
    rows.length === 0 &&
      declared.length > 0 &&
      loaderInUse !== undefined &&
      !loaderInUse.ready,
  );

  // The module that should have drawn here and did not, for the one line.
  const missing = $derived.by(() => {
    if (rows.length > 0 || waiting) return undefined;
    const entry = declared[0] ?? refused[0];
    if (entry === undefined) return undefined;

    return registryInUse?.moduleTitle(entry.namespace) ?? entry.namespace;
  });

  // The module whose view threw, for the file it threw on. Another file on
  // the same page starts clean.
  let thrown = $state<{ fileRef: string; module: string } | undefined>(
    undefined,
  );
  const failed = $derived(
    thrown?.fileRef === fileRef ? thrown.module : undefined,
  );

  // Activation is lazy (ADR-20260911-4p6 rule 25): the bundle loads when its
  // first slot first renders.
  onMount(() => {
    void loaderInUse?.activate();
  });

  const dropped = (namespace: string, error: unknown) => {
    thrown = {
      fileRef,
      module: registryInUse?.moduleTitle(namespace) ?? namespace,
    };
    registryInUse?.refuse({
      namespace,
      reason: 'threw',
      detail: error instanceof Error ? error.message : String(error),
    });
  };
</script>

{#snippet unavailable(module: string)}
  <p class="text-xs text-muted-foreground" data-file-view="unavailable">
    {t('fileView.unavailable', { module })}
  </p>
  {@render fallback()}
{/snippet}

{#if failed !== undefined}
  {@render unavailable(failed)}
{:else if rows.length > 0}
  {#each rows as row (row.namespace + '/' + row.id)}
    {@const Row = row.component}
    <svelte:boundary
      onerror={(error) => {
        dropped(row.namespace, error);
      }}
    >
      <section
        class="flex flex-col gap-4"
        data-slot="file-view"
        data-module={row.namespace}
        data-section={row.id}
      >
        <!-- The Contents tab already names the part, so only a screen reader
             reads the module's title (ADR-20261029-dgv rule 3). -->
        <h2 class="sr-only">{row.title}</h2>
        <Row {fileRef} />
      </section>
    </svelte:boundary>
  {/each}
{:else if waiting}
  <section
    class="flex flex-col gap-4"
    data-slot="file-view"
    data-file-view="loading"
    aria-busy="true"
  >
    <span class="sr-only">{t('fileView.loading')}</span>
    <Skeleton class="h-5 w-40" />
    <Skeleton class="h-20 w-full" />
  </section>
{:else if missing !== undefined}
  {@render unavailable(missing)}
{:else}
  {@render fallback()}
{/if}
