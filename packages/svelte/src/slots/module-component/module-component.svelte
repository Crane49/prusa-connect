<!-- Another module's component inside a module's own page: a placeholder
     while it loads, the component once it is there, and the importing
     module's fallback when it is not available (ADR-20261027-wvj rule 24).
     The component stands under its own module's name, so that module's
     styles reach it and the importing module's do not (ADR-20261029-zzg). -->
<script lang="ts" generics="Props extends Record<string, unknown>">
  import { t } from '../../i18n';
  import { Skeleton } from '../../primitives';
  import { ownerOf } from '../component-owner';
  import { ComponentUnavailable } from '../host';
  import type { ModuleComponentProps } from './types';

  let { load, props, fallback }: ModuleComponentProps<Props> = $props();

  // A rejection that is not the host's own refusal, such as a bundle that
  // threw, reads as a module that failed.
  const unavailableOf = (thrown: unknown): ComponentUnavailable =>
    thrown instanceof ComponentUnavailable
      ? thrown
      : new ComponentUnavailable('failed', '', '');
</script>

{#snippet unavailable(why: ComponentUnavailable)}
  {#if fallback}
    {@render fallback(why)}
  {:else}
    <p
      class="text-sm text-muted-foreground"
      data-module-component="unavailable"
    >
      {t('moduleComponent.unavailable')}
    </p>
  {/if}
{/snippet}

{#await load()}
  <div data-module-component="loading" aria-busy="true">
    <span class="sr-only">{t('moduleComponent.loading')}</span>
    <Skeleton class="h-20 w-full" />
  </div>
{:then Loaded}
  {@const owner = ownerOf(Loaded)}
  <svelte:boundary>
    {#if owner === undefined}
      <Loaded {...props} />
    {:else}
      <!-- The owner's styles reach only markup under its own name. -->
      <div data-module={owner} class="contents">
        <Loaded {...props} />
      </div>
    {/if}
    {#snippet failed(error)}
      {@render unavailable(unavailableOf(error))}
    {/snippet}
  </svelte:boundary>
{:catch error}
  {@render unavailable(unavailableOf(error))}
{/await}
