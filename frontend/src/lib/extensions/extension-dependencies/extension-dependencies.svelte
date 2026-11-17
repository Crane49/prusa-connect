<!--
  The modules one extension depends on, and the installed ones that depend on
  it (ADR-20261027-e6k). The keys are Composer's: require, suggest and
  conflict. A module another installed module requires cannot be removed or
  switched off, so the list of those modules says why. When the page has read
  them, it also says what keeps the extension installed and how its release
  stands against the index.
-->
<script lang="ts">
  import { translateFor } from '@prusa/connect-ui/i18n';

  import { APP_NAMESPACE } from '../../i18n';
  import type {
    OutdatedCheck,
    WhyCheck,
  } from '../../stores/module-catalogue.svelte';
  import { type ExtensionDependencies, shortNameOf } from '../dependencies';
  import DependencyNotes from './dependency-notes.svelte';
  import DependencyRows from './dependency-rows.svelte';
  import DependencyStanding from './dependency-standing.svelte';

  let {
    dependencies,
    namespace = '',
    why,
    outdated,
  }: {
    dependencies: ExtensionDependencies;
    /** The extension's namespace, which picks its row out of `outdated`. */
    namespace?: string;
    why?: WhyCheck | undefined;
    outdated?: OutdatedCheck | undefined;
  } = $props();

  const t = translateFor(APP_NAMESPACE);
</script>

<div class="flex flex-col gap-3 text-sm" data-part="dependencies">
  {#if dependencies.core !== ''}
    <p data-part="dependencies-core">
      {t('app.settings.extensions.dependencies.core', {
        constraint: dependencies.core,
      })}
    </p>
  {/if}

  {#if dependencies.requires.length > 0}
    <DependencyRows rows={dependencies.requires} />
  {/if}

  <DependencyNotes
    suggests={dependencies.suggests}
    conflicts={dependencies.conflicts}
  />

  {#if dependencies.requiredBy.length > 0}
    <div class="flex flex-col gap-1" data-part="dependencies-required-by">
      <p class="font-medium">
        {t('app.settings.extensions.dependencies.requiredBy')}
      </p>
      <ul class="flex flex-col gap-1">
        {#each dependencies.requiredBy as dependant (dependant.instance)}
          <li class="flex flex-wrap items-center gap-2">
            <span class="font-medium">{shortNameOf(dependant.module)}</span>
            <code class="text-xs text-muted-foreground"
              >{dependant.version}</code
            >
          </li>
        {/each}
      </ul>
      <p class="text-xs text-muted-foreground">
        {t('app.settings.extensions.dependencies.requiredBy.note')}
      </p>
    </div>
  {/if}

  <DependencyStanding {why} {outdated} {namespace} />
</div>
