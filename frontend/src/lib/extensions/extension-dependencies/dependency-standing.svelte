<!--
  What keeps an extension installed, and its releases against the index, as
  the server answers why and outdated (ADR-20261027-e6k rule 20). Each part
  draws its loading and refused state on its own, so a slow index never
  hides the answer to why.
-->
<script lang="ts">
  import { translateFor } from '@prusa/connect-ui/i18n';
  import { Badge, Skeleton } from '@prusa/connect-ui/primitives';

  import { APP_NAMESPACE } from '../../i18n';
  import { outdatedOf, shortNameOf } from '../dependencies';
  import type {
    OutdatedCheck,
    WhyCheck,
  } from '../../stores/module-catalogue.svelte';

  let {
    why,
    outdated,
    namespace,
  }: {
    why: WhyCheck | undefined;
    outdated: OutdatedCheck | undefined;
    namespace: string;
  } = $props();

  const t = translateFor(APP_NAMESPACE);

  const reasons = $derived(why?.state === 'ready' ? why.reasons : []);
  const row = $derived(
    outdated?.state === 'ready'
      ? outdatedOf(outdated.rows, namespace)
      : undefined,
  );

  // A reason as a person reads it: who keeps the extension, by its short
  // name, and the range it needs. The server's sentence is the fallback.
  const OPERATOR = 'the operator';
  const reasonText = (reason: {
    by: string;
    constraint?: string;
    text: string;
  }): string => {
    if (reason.by === OPERATOR) {
      return t('app.settings.extensions.dependencies.why.operator');
    }
    const version = / (\S+) requires /u.exec(reason.text)?.[1];
    if (version === undefined || (reason.constraint ?? '') === '') {
      return reason.text;
    }

    return t('app.settings.extensions.dependencies.why.requiredBy', {
      name: shortNameOf(reason.by),
      version,
      constraint: reason.constraint ?? '',
    });
  };

  const VARIANT = {
    'up-to-date': 'secondary',
    'update-possible': 'default',
    blocked: 'warning',
  } as const;
</script>

{#if why !== undefined}
  <div class="flex flex-col gap-1" data-part="dependencies-why">
    <p class="font-medium">
      {t('app.settings.extensions.dependencies.why')}
    </p>
    {#if why.state === 'loading'}
      <Skeleton
        class="h-4 w-40"
        role="status"
        aria-label={t('app.settings.extensions.dependencies.why.loading')}
      />
    {:else if why.state === 'refused'}
      <p class="text-muted-foreground">
        {t('app.settings.extensions.dependencies.why.refused')}
      </p>
    {:else if reasons.length === 0}
      <p class="text-muted-foreground">
        {t('app.settings.extensions.dependencies.why.none')}
      </p>
    {:else}
      <ul class="flex flex-col gap-1">
        {#each reasons as reason (reason.text)}
          <li>{reasonText(reason)}</li>
        {/each}
      </ul>
    {/if}
  </div>
{/if}

{#if outdated !== undefined}
  <div class="flex flex-col gap-1" data-part="dependencies-outdated">
    <p class="font-medium">
      {t('app.settings.extensions.dependencies.outdated')}
    </p>
    {#if outdated.state === 'loading'}
      <Skeleton
        class="h-4 w-40"
        role="status"
        aria-label={t('app.settings.extensions.dependencies.outdated.loading')}
      />
    {:else if outdated.state === 'refused'}
      <p class="text-muted-foreground">
        {t('app.settings.extensions.dependencies.outdated.refused')}
      </p>
    {:else if row === undefined}
      <p class="text-muted-foreground">
        {t('app.settings.extensions.dependencies.outdated.none')}
      </p>
    {:else}
      <span class="flex flex-wrap items-center gap-2">
        <span
          >{t('app.settings.extensions.dependencies.outdated.installed', {
            version: row.installed,
          })}</span
        >
        {#if (row.allowed ?? '') !== ''}
          <span
            >{t('app.settings.extensions.dependencies.outdated.allowed', {
              version: row.allowed ?? '',
            })}</span
          >
        {/if}
        {#if (row.latest ?? '') !== ''}
          <span
            >{t('app.settings.extensions.dependencies.outdated.latest', {
              version: row.latest ?? '',
            })}</span
          >
        {/if}
        <Badge variant={VARIANT[row.status]} data-part="outdated-status">
          {t(`app.settings.extensions.dependencies.outdated.${row.status}`)}
        </Badge>
      </span>
    {/if}
  </div>
{/if}
