<!--
  The suggestions and the conflicts of an extension, as its manifest wrote
  them (ADR-20261027-e6k). A suggestion names a module and why, and installs
  nothing. A conflict names releases of a module the extension refuses to run
  beside, and says whether this server has one.
-->
<script lang="ts">
  import { translateFor } from '@prusa/connect-ui/i18n';
  import { Badge } from '@prusa/connect-ui/primitives';

  import { APP_NAMESPACE } from '../../i18n';
  import {
    conflictStandingOf,
    type ModuleRequirement,
    shortNameOf,
  } from '../dependencies';

  let {
    suggests,
    conflicts,
  }: {
    suggests: readonly ModuleRequirement[];
    conflicts: readonly ModuleRequirement[];
  } = $props();

  const t = translateFor(APP_NAMESPACE);
</script>

{#if suggests.length > 0}
  <div class="flex flex-col gap-1" data-part="dependencies-suggests">
    <p class="font-medium">
      {t('app.settings.extensions.dependencies.suggests')}
    </p>
    <ul class="flex flex-col gap-2">
      {#each suggests as row (row.module)}
        <li class="flex flex-col gap-0.5" data-part="suggestion">
          <span class="font-medium">{shortNameOf(row.module ?? '')}</span>
          {#if (row.reason ?? '') !== ''}
            <span class="text-muted-foreground">{row.reason}</span>
          {/if}
          <code class="text-xs text-muted-foreground">{row.module}</code>
        </li>
      {/each}
    </ul>
  </div>
{/if}

{#if conflicts.length > 0}
  <div class="flex flex-col gap-1" data-part="dependencies-conflicts">
    <p class="font-medium">
      {t('app.settings.extensions.dependencies.conflicts')}
    </p>
    <ul class="flex flex-col gap-2">
      {#each conflicts as row (row.module)}
        {@const standing = conflictStandingOf(row)}
        <li class="flex flex-col gap-0.5" data-part="conflict">
          <span class="flex flex-wrap items-center gap-2">
            <span class="font-medium">{shortNameOf(row.module ?? '')}</span>
            <code class="text-xs text-muted-foreground">{row.version}</code>
            <Badge
              variant={standing === 'clear' ? 'secondary' : 'warning'}
              data-part="conflict-standing"
            >
              {t(`app.settings.extensions.dependencies.conflict.${standing}`, {
                installed: row.installed ?? '',
              })}
            </Badge>
          </span>
          <code class="text-xs text-muted-foreground">{row.module}</code>
        </li>
      {/each}
    </ul>
  </div>
{/if}
