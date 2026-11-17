<script lang="ts">
  import { onMount } from 'svelte';

  import { t } from '../../i18n';
  import { getSlotContext } from '../context';
  import type { ModulePageProps } from './types';

  let {
    namespace,
    path,
    registry,
    loader,
    fallback,
    class: className,
  }: ModulePageProps = $props();

  const context = getSlotContext();
  const registryInUse = $derived(registry ?? context?.registry);
  const loaderInUse = $derived(loader ?? context?.loader);

  const page = $derived(
    registryInUse
      ?.pages()
      .find((row) => row.namespace === namespace && row.path === path),
  );

  // The bundle is imported when its page first opens (ADR-20260911-4p6 rule 25).
  onMount(() => {
    void loaderInUse?.activate();
  });

  const dropped = (error: unknown) => {
    registryInUse?.refuse({
      namespace,
      reason: 'threw',
      detail: error instanceof Error ? error.message : String(error),
    });
  };
</script>

{#if page}
  {@const Page = page.component}
  <!-- The page renders inside a boundary, so a module that throws costs its
       own screen and not the app (ADR-20260911-4p6 rule 26). -->
  <svelte:boundary onerror={dropped}>
    <div class={className} data-slot="page" data-module={namespace}>
      <Page />
    </div>
    {#snippet failed()}
      <p class="text-sm text-muted-foreground">{t('modulePage.failed')}</p>
    {/snippet}
  </svelte:boundary>
{:else}
  {@render fallback?.()}
{/if}
